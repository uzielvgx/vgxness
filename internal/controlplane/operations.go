package controlplane

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/chronicle"
	"github.com/vgxness/vgxness/internal/config"
)

const (
	operationalStaleAge      = time.Hour
	operationalDocumentLimit = 10_000
	minimumPruneRetention    = 24 * time.Hour
	maximumPruneRetention    = 10 * 365 * 24 * time.Hour
)

type operationalSnapshot struct {
	inventory      bridge.OperationalInventory
	orchestrations map[string]orchestrationDocument
	tickets        map[string]nativeTicketDocument
	ticketModified map[string]time.Time
	leasedTickets  map[string]struct{}
	hasErrors      bool
}

func (service *Service) OperationalInventory(ctx context.Context, workspace string, request bridge.OperationalInventoryRequest) (bridge.OperationalInventory, error) {
	snapshot, err := service.scanOperationalState(ctx, workspace, request)
	return snapshot.inventory, err
}

func (service *Service) scanOperationalState(ctx context.Context, workspace string, request bridge.OperationalInventoryRequest) (operationalSnapshot, error) {
	root, err := canonicalWorkspace(ctx, workspace)
	if err != nil {
		return operationalSnapshot{}, err
	}
	storageRoot := service.storageRoot
	if request.StorageRoot != "" {
		storageRoot = request.StorageRoot
	}
	paths, err := config.PathsFor(config.Options{StorageRoot: storageRoot, ProjectDir: root, ProjectLocal: request.ProjectLocal})
	if err != nil {
		return operationalSnapshot{}, fmt.Errorf("%w: operational storage", bridge.ErrExecution)
	}
	snapshot := operationalSnapshot{
		inventory: bridge.OperationalInventory{
			Workspace: root, StorageRoot: paths.Root, Health: "healthy",
			Orchestrations: []bridge.OrchestrationSummary{},
			NativeTickets:  []bridge.NativeTicketSummary{},
			Findings:       []bridge.OperationalFinding{},
		},
		orchestrations: make(map[string]orchestrationDocument),
		tickets:        make(map[string]nativeTicketDocument),
		ticketModified: make(map[string]time.Time),
		leasedTickets:  make(map[string]struct{}),
	}
	now := service.now().UTC()
	run, present, runErr := chronicle.ReadCurrent(ctx, paths.CurrentRun)
	if runErr != nil {
		if ctx.Err() != nil {
			return snapshot, ctx.Err()
		}
		snapshot.addFinding("error", "current_run_corrupt", "current-run", "Chronicle current-run state could not be verified")
	} else if present {
		snapshot.inventory.CurrentRun = &bridge.CurrentRunSummary{ID: run.ID, Status: run.Status, Phase: run.Phase, UpdatedAt: run.UpdatedAt}
		updated, parseErr := time.Parse(time.RFC3339, run.UpdatedAt)
		switch {
		case parseErr != nil:
			snapshot.addFinding("error", "current_run_time_invalid", run.ID, "Chronicle update time is invalid")
		case run.Status != "running":
			snapshot.addFinding("warning", "current_run_attention", run.ID, "Chronicle current run is "+run.Status)
		case now.Sub(updated) > operationalStaleAge:
			snapshot.addFinding("warning", "current_run_stale", run.ID, "Chronicle current run has not advanced within one hour")
		}
	}
	snapshot.scanOrchestrations(ctx, paths.Root, root, now)
	snapshot.scanNativeTickets(ctx, paths.Root, root, now)
	snapshot.scanNativeLeases(paths.Root, now)
	if ctx.Err() != nil {
		return snapshot, ctx.Err()
	}
	sort.Slice(snapshot.inventory.Orchestrations, func(i, j int) bool {
		return snapshot.inventory.Orchestrations[i].ID < snapshot.inventory.Orchestrations[j].ID
	})
	sort.Slice(snapshot.inventory.NativeTickets, func(i, j int) bool {
		return snapshot.inventory.NativeTickets[i].ID < snapshot.inventory.NativeTickets[j].ID
	})
	sort.Slice(snapshot.inventory.Findings, func(i, j int) bool {
		if snapshot.inventory.Findings[i].Severity != snapshot.inventory.Findings[j].Severity {
			return snapshot.inventory.Findings[i].Severity < snapshot.inventory.Findings[j].Severity
		}
		if snapshot.inventory.Findings[i].Code != snapshot.inventory.Findings[j].Code {
			return snapshot.inventory.Findings[i].Code < snapshot.inventory.Findings[j].Code
		}
		return snapshot.inventory.Findings[i].Subject < snapshot.inventory.Findings[j].Subject
	})
	return snapshot, nil
}

func (snapshot *operationalSnapshot) scanNativeLeases(storageRoot string, now time.Time) {
	for _, path := range nativeLeasePaths(storageRoot) {
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			snapshot.addFinding("error", "native_lease_unreadable", filepath.Base(path), "Native execution lease could not be inspected")
			continue
		}
		lease, err := readNativeLeasePath(path)
		if err != nil {
			snapshot.addFinding("error", "native_lease_corrupt", filepath.Base(path), "Native execution lease could not be verified")
			continue
		}
		snapshot.leasedTickets[lease.TicketID] = struct{}{}
		if _, exists := snapshot.tickets[lease.TicketID]; !exists {
			snapshot.addFinding("warning", "native_lease_orphan", lease.TicketID, "Native execution lease has no readable ticket")
		}
		if nativeLeaseReclaimable(path, now) {
			snapshot.addFinding("warning", "native_lease_reclaimable", lease.TicketID, "Native execution lease is eligible for recovery")
		}
	}
}

func (snapshot *operationalSnapshot) scanOrchestrations(ctx context.Context, storageRoot, workspace string, now time.Time) {
	directory := orchestrationDirectory(storageRoot)
	for _, id := range snapshot.documentIDs(ctx, directory, "orchestration") {
		if ctx.Err() != nil {
			return
		}
		document, err := readOrchestrationDocument(storageRoot, id)
		if err != nil || document.Workspace != workspace {
			snapshot.addFinding("error", "orchestration_corrupt", id, "Orchestration document could not be verified")
			continue
		}
		updated, timeErr := time.Parse(time.RFC3339, document.UpdatedAt)
		if timeErr != nil || !validOrchestrationStatus(document.Status) {
			snapshot.addFinding("error", "orchestration_invalid", id, "Orchestration status or update time is invalid")
			continue
		}
		snapshot.orchestrations[id] = document
		snapshot.inventory.Orchestrations = append(snapshot.inventory.Orchestrations, bridge.OrchestrationSummary{
			ID: id, Status: document.Status, CurrentWave: document.CurrentWave,
			Tasks: len(document.Plan.Tasks), Waves: len(document.Plan.Waves), UpdatedAt: document.UpdatedAt,
		})
		if !terminalOrchestrationStatus(document.Status) && now.Sub(updated) > operationalStaleAge {
			snapshot.addFinding("warning", "orchestration_stale", id, "Non-terminal orchestration has not advanced within one hour")
		}
	}
}

func (snapshot *operationalSnapshot) scanNativeTickets(ctx context.Context, storageRoot, workspace string, now time.Time) {
	directory := nativeTicketDirectory(storageRoot)
	for _, id := range snapshot.documentIDs(ctx, directory, "native ticket") {
		if ctx.Err() != nil {
			return
		}
		path, pathErr := nativeTicketPath(storageRoot, id)
		info, statErr := os.Lstat(path)
		if pathErr != nil || statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			snapshot.addFinding("error", "native_ticket_corrupt", id, "Native ticket file could not be verified")
			continue
		}
		document, err := readNativeTicket(storageRoot, id)
		if err != nil || document.Workspace != workspace || !validNativeTicketState(document.State) {
			snapshot.addFinding("error", "native_ticket_corrupt", id, "Native ticket document could not be verified")
			continue
		}
		deadline, deadlineErr := time.Parse(time.RFC3339, document.Deadline)
		if deadlineErr != nil {
			snapshot.addFinding("error", "native_ticket_deadline_invalid", id, "Native ticket deadline is invalid")
			continue
		}
		modified := info.ModTime().UTC()
		snapshot.tickets[id] = document
		snapshot.ticketModified[id] = modified
		snapshot.inventory.NativeTickets = append(snapshot.inventory.NativeTickets, bridge.NativeTicketSummary{
			ID: id, State: document.State, TaskID: document.TaskID, RunID: document.RunID,
			Deadline: document.Deadline, ModifiedAt: modified.Format(time.RFC3339),
		})
		if !terminalNativeTicketState(document.State) && now.After(deadline) {
			snapshot.addFinding("warning", "native_ticket_expired", id, "Non-terminal native ticket is past its deadline")
		}
	}
}

func (snapshot *operationalSnapshot) documentIDs(ctx context.Context, directory, kind string) []string {
	if ctx.Err() != nil {
		return nil
	}
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		snapshot.addFinding("error", "operational_directory_invalid", kind, "Operational directory could not be verified")
		return nil
	}
	opened, err := os.Open(directory)
	if err != nil {
		snapshot.addFinding("error", "operational_directory_unreadable", kind, "Operational directory could not be read")
		return nil
	}
	defer opened.Close()
	openedInfo, statErr := opened.Stat()
	if statErr != nil || !os.SameFile(info, openedInfo) || !openedInfo.IsDir() {
		snapshot.addFinding("error", "operational_directory_changed", kind, "Operational directory changed during inspection")
		return nil
	}
	entries, readErr := opened.ReadDir(operationalDocumentLimit + 1)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		snapshot.addFinding("error", "operational_directory_unreadable", kind, "Operational directory could not be read")
		return nil
	}
	if len(entries) > operationalDocumentLimit {
		snapshot.addFinding("error", "operational_inventory_limit", kind, "Operational directory exceeds the 10000-document inspection limit")
		entries = entries[:operationalDocumentLimit]
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".json") && !strings.HasPrefix(name, ".") {
			ids = append(ids, strings.TrimSuffix(name, ".json"))
		}
	}
	sort.Strings(ids)
	return ids
}

func (snapshot *operationalSnapshot) addFinding(severity, code, subject, message string) {
	snapshot.inventory.Health = "degraded"
	if severity == "error" {
		snapshot.hasErrors = true
	}
	snapshot.inventory.Findings = append(snapshot.inventory.Findings, bridge.OperationalFinding{
		Severity: severity, Code: code, Subject: subject, Message: message,
	})
}

func (service *Service) PruneOperations(ctx context.Context, workspace string, request bridge.OperationalPruneRequest) (bridge.OperationalPruneResult, error) {
	if request.OlderThanSeconds < int64(minimumPruneRetention/time.Second) ||
		request.OlderThanSeconds > int64(maximumPruneRetention/time.Second) {
		return bridge.OperationalPruneResult{}, bridge.ErrInvalid
	}
	retention := time.Duration(request.OlderThanSeconds) * time.Second
	snapshot, err := service.scanOperationalState(ctx, workspace, bridge.OperationalInventoryRequest{
		StorageRoot: request.StorageRoot, ProjectLocal: request.ProjectLocal,
	})
	if err != nil {
		return bridge.OperationalPruneResult{}, err
	}
	if snapshot.hasErrors {
		return bridge.OperationalPruneResult{}, fmt.Errorf("%w: operational inventory contains errors", bridge.ErrDenied)
	}
	cutoff := service.now().UTC().Add(-retention)
	result := bridge.OperationalPruneResult{
		Workspace: snapshot.inventory.Workspace, StorageRoot: snapshot.inventory.StorageRoot,
		Cutoff: cutoff.Format(time.RFC3339), Applied: request.Apply,
		Candidates: []bridge.OperationalPruneCandidate{}, Removed: []bridge.OperationalPruneCandidate{},
	}
	orchestrationCandidates := make(map[string]orchestrationDocument)
	for id, document := range snapshot.orchestrations {
		updated, _ := time.Parse(time.RFC3339, document.UpdatedAt)
		if terminalOrchestrationStatus(document.Status) && updated.Before(cutoff) {
			orchestrationCandidates[id] = document
			result.Candidates = append(result.Candidates, bridge.OperationalPruneCandidate{Kind: "orchestration", ID: id, Timestamp: document.UpdatedAt})
		}
	}
	protectedTickets := make(map[string]struct{})
	for id, document := range snapshot.orchestrations {
		if _, pruned := orchestrationCandidates[id]; pruned {
			continue
		}
		for _, ticketID := range document.PreparedBindings {
			protectedTickets[ticketID] = struct{}{}
		}
	}
	for id, document := range snapshot.tickets {
		modified := snapshot.ticketModified[id]
		_, protected := protectedTickets[id]
		_, leased := snapshot.leasedTickets[id]
		if terminalNativeTicketState(document.State) && modified.Before(cutoff) && !protected && !leased {
			result.Candidates = append(result.Candidates, bridge.OperationalPruneCandidate{Kind: "native-ticket", ID: id, Timestamp: modified.Format(time.RFC3339)})
		}
	}
	sort.Slice(result.Candidates, func(i, j int) bool {
		if result.Candidates[i].Kind != result.Candidates[j].Kind {
			return pruneKindOrder(result.Candidates[i].Kind) < pruneKindOrder(result.Candidates[j].Kind)
		}
		return result.Candidates[i].ID < result.Candidates[j].ID
	})
	if !request.Apply {
		return result, nil
	}
	var refreshedTickets map[string]struct{}
	for _, candidate := range result.Candidates {
		if candidate.Kind == "native-ticket" && refreshedTickets == nil {
			refreshed, refreshErr := service.scanOperationalState(ctx, workspace, bridge.OperationalInventoryRequest{
				StorageRoot: request.StorageRoot, ProjectLocal: request.ProjectLocal,
			})
			if refreshErr != nil {
				return result, refreshErr
			}
			if refreshed.hasErrors {
				return result, fmt.Errorf("%w: operational inventory changed during prune", bridge.ErrDenied)
			}
			refreshedTickets = prunableTicketIDs(refreshed, cutoff)
		}
		var removeErr error
		var removed bool
		switch candidate.Kind {
		case "orchestration":
			removed, removeErr = service.removePrunableOrchestration(ctx, snapshot.inventory.StorageRoot, snapshot.inventory.Workspace, candidate.ID, cutoff)
		case "native-ticket":
			if _, stillPrunable := refreshedTickets[candidate.ID]; !stillPrunable {
				return result, fmt.Errorf("%w: native ticket protection changed during prune", bridge.ErrDenied)
			}
			removed, removeErr = service.removePrunableNativeTicket(ctx, snapshot.inventory.StorageRoot, snapshot.inventory.Workspace, candidate.ID, cutoff)
		}
		if removed {
			result.Removed = append(result.Removed, candidate)
		}
		if removeErr != nil {
			return result, removeErr
		}
	}
	return result, nil
}

func prunableTicketIDs(snapshot operationalSnapshot, cutoff time.Time) map[string]struct{} {
	protected := make(map[string]struct{})
	for _, document := range snapshot.orchestrations {
		for _, ticketID := range document.PreparedBindings {
			protected[ticketID] = struct{}{}
		}
	}
	result := make(map[string]struct{})
	for id, document := range snapshot.tickets {
		_, referenced := protected[id]
		_, leased := snapshot.leasedTickets[id]
		if terminalNativeTicketState(document.State) && snapshot.ticketModified[id].Before(cutoff) && !referenced && !leased {
			result[id] = struct{}{}
		}
	}
	return result
}

func (service *Service) removePrunableOrchestration(ctx context.Context, root, workspace, id string, cutoff time.Time) (bool, error) {
	path, err := orchestrationPath(root, id)
	if err != nil {
		return false, err
	}
	lock, err := acquireBoundedControlPlaneLock(ctx, path+".lock")
	if err != nil {
		return false, fmt.Errorf("%w: orchestration is busy", bridge.ErrUnavailable)
	}
	defer lock.Release()
	document, err := readOrchestrationDocument(root, id)
	updated, timeErr := time.Parse(time.RFC3339, document.UpdatedAt)
	if err != nil || document.Workspace != workspace || timeErr != nil ||
		!terminalOrchestrationStatus(document.Status) || !updated.Before(cutoff) {
		return false, fmt.Errorf("%w: orchestration changed during prune", bridge.ErrDenied)
	}
	if err := os.Remove(path); err != nil {
		return false, fmt.Errorf("%w: remove orchestration", bridge.ErrExecution)
	}
	if err := syncNativeDirectory(filepath.Dir(path)); err != nil {
		return true, fmt.Errorf("%w: sync orchestration directory", bridge.ErrExecution)
	}
	return true, nil
}

func (service *Service) removePrunableNativeTicket(ctx context.Context, root, workspace, id string, cutoff time.Time) (bool, error) {
	path, err := nativeTicketPath(root, id)
	if err != nil {
		return false, err
	}
	lock, err := acquireBoundedControlPlaneLock(ctx, path+".lock")
	if err != nil {
		return false, fmt.Errorf("%w: native ticket is busy", bridge.ErrUnavailable)
	}
	defer lock.Release()
	info, statErr := os.Lstat(path)
	document, readErr := readNativeTicket(root, id)
	if statErr != nil || readErr != nil || document.Workspace != workspace ||
		!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		!terminalNativeTicketState(document.State) || !info.ModTime().UTC().Before(cutoff) {
		return false, fmt.Errorf("%w: native ticket changed during prune", bridge.ErrDenied)
	}
	for _, leasePath := range nativeLeasePaths(root) {
		lease, leaseErr := readNativeLeasePath(leasePath)
		if errors.Is(leaseErr, os.ErrNotExist) {
			continue
		}
		if leaseErr == nil && lease.TicketID == id {
			return false, fmt.Errorf("%w: native ticket acquired a lease during prune", bridge.ErrDenied)
		}
		if leaseErr != nil {
			if _, statErr := os.Lstat(leasePath); !errors.Is(statErr, os.ErrNotExist) {
				return false, fmt.Errorf("%w: native lease changed during prune", bridge.ErrDenied)
			}
		}
	}
	if err := os.Remove(path); err != nil {
		return false, fmt.Errorf("%w: remove native ticket", bridge.ErrExecution)
	}
	if err := syncNativeDirectory(filepath.Dir(path)); err != nil {
		return true, fmt.Errorf("%w: sync native ticket directory", bridge.ErrExecution)
	}
	return true, nil
}

func validOrchestrationStatus(status string) bool {
	switch status {
	case "pending", "running", "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func terminalOrchestrationStatus(status string) bool {
	return status == "completed" || status == "failed" || status == "cancelled"
}

func validNativeTicketState(state string) bool {
	switch state {
	case "preparing", "prepared", "completed", "failed":
		return true
	default:
		return false
	}
}

func terminalNativeTicketState(state string) bool {
	return state == "completed" || state == "failed"
}

func pruneKindOrder(kind string) int {
	if kind == "orchestration" {
		return 0
	}
	return 1
}

var _ bridge.OperationsRuntime = (*Service)(nil)
