package chronicle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/contracts"
)

// RecoveryState is a validated view of Chronicle's operational sources.
type RecoveryState struct {
	Run            json.RawMessage
	RunID          string
	Status         string
	Current        CurrentRun
	CurrentPresent bool
	Events         []Event
}

type runIdentity struct {
	SchemaVersion   string `json:"schemaVersion"`
	ID              string `json:"id"`
	Project         string `json:"project"`
	Goal            string `json:"goal"`
	Status          string `json:"status"`
	StorageMode     string `json:"storageMode"`
	ArtifactBackend string `json:"artifactBackend"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
	Selection       struct {
		ID               string                      `json:"selectionId"`
		Status           string                      `json:"status"`
		SelectedProvider string                      `json:"selectedProvider"`
		Needs            []capabilityIdentity        `json:"needs"`
		Candidates       []providerCandidateIdentity `json:"candidates"`
	} `json:"orchestratorSelection"`
	Routing struct {
		ID            string   `json:"decisionId"`
		Candidates    []string `json:"candidates"`
		SelectedAgent string   `json:"selectedAgent"`
	} `json:"routingDecision"`
	Preflight struct {
		ID string `json:"preflightId"`
	} `json:"sddPreflight"`
	Phases []struct {
		Name         string   `json:"name"`
		Agent        string   `json:"agent"`
		Status       string   `json:"status"`
		Artifacts    []string `json:"artifacts"`
		MemoryWrites []string `json:"memoryWrites"`
		Validations  []string `json:"validations"`
	} `json:"phases"`
	Artifacts    []json.RawMessage `json:"artifacts"`
	MemoryWrites []struct {
		ID string `json:"id"`
	} `json:"memoryWrites"`
	Decisions []struct {
		ID string `json:"decisionId"`
	} `json:"decisions"`
	Tasks []struct {
		ID             string `json:"taskId"`
		Phase          string `json:"phase"`
		AgentID        string `json:"agentId"`
		Status         string `json:"status"`
		ResultID       string `json:"resultId"`
		CancellationID string `json:"cancellationId"`
	} `json:"tasks"`
	Cancellations []struct {
		ID         string `json:"cancellationId"`
		TargetKind string `json:"targetKind"`
		TargetID   string `json:"targetId"`
		Status     string `json:"status"`
	} `json:"cancellations"`
	Results []struct {
		ID     string `json:"resultId"`
		TaskID string `json:"taskId"`
	} `json:"results"`
	Capsules []struct {
		ID    string `json:"capsuleId"`
		RunID string `json:"runId"`
	} `json:"capsules"`
	Validations []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Phase  string `json:"phase"`
	} `json:"validations"`
	Raw json.RawMessage `json:"-"`
}

type capabilityIdentity struct {
	Capability  string          `json:"capability"`
	Version     string          `json:"version"`
	Constraints json.RawMessage `json:"constraints"`
}

type providerCandidateIdentity struct {
	Provider     string               `json:"provider"`
	Capabilities []capabilityIdentity `json:"capabilities"`
	Eligible     bool                 `json:"eligible"`
}

// Recover validates and assembles the full snapshot, current projection, and
// event log. It also completes a terminal snapshot whose pointer removal was
// interrupted after the terminal snapshot became durable.
func (s *SnapshotStore) Recover(ctx context.Context) (RecoveryState, error) {
	var recovered RecoveryState
	err := s.withStateLock(ctx, lockExclusive, func() error {
		return s.withEvents(ctx, lockExclusive, func(events []Event) error {
			current, present, err := ReadCurrent(ctx, s.currentPath)
			if err != nil {
				return err
			}
			if present {
				run, data, readErr := s.readCurrentRunSnapshot(ctx, current)
				if readErr != nil {
					return readErr
				}
				if activeStatus(run.Status) {
					activeErr := validateEventReferences(run, events)
					if activeErr == nil {
						activeErr = validateActiveProjection(run, current, s.runID)
					}
					if activeErr == nil && (len(events) == 0 || current.LastEventID != events[len(events)-1].ID) {
						activeErr = fmt.Errorf("%w: current snapshot is not at the event-log head", ErrInconsistent)
					}
					if activeErr == nil {
						recovered = recoveryState(run, data, current, true, events)
						return nil
					}
					terminalRun, terminalData, terminalErr := s.readRunSnapshot(ctx)
					if terminalErr != nil || !terminalStatus(terminalRun.Status) {
						return activeErr
					}
					if err := s.completeTerminalCommit(terminalRun, current, events); err != nil {
						return activeErr
					}
					recovered = recoveryState(terminalRun, terminalData, CurrentRun{}, false, events)
					return nil
				}
				if !terminalStatus(run.Status) {
					return fmt.Errorf("%w: snapshot status is not recoverable", ErrInconsistent)
				}
				if err := s.completeTerminalCommit(run, current, events); err != nil {
					return err
				}
				recovered = recoveryState(run, data, CurrentRun{}, false, events)
				return nil
			}

			run, data, err := s.readRunSnapshot(ctx)
			if err != nil {
				return err
			}
			if err := validateEventReferences(run, events); err != nil {
				return err
			}
			if !terminalStatus(run.Status) {
				return fmt.Errorf("%w: active snapshot has no current pointer", ErrInconsistent)
			}
			if err := validateTerminalHead(run, events); err != nil {
				return err
			}
			recovered = recoveryState(run, data, CurrentRun{}, false, events)
			return nil
		})
	})
	return recovered, err
}

func recoveryState(run runIdentity, data []byte, current CurrentRun, present bool, events []Event) RecoveryState {
	return RecoveryState{
		Run:            append(json.RawMessage(nil), data...),
		RunID:          run.ID,
		Status:         run.Status,
		Current:        current,
		CurrentPresent: present,
		Events:         append([]Event(nil), events...),
	}
}

func (s *SnapshotStore) completeTerminalCommit(run runIdentity, current CurrentRun, events []Event) error {
	if err := validateEventReferences(run, events); err != nil {
		return err
	}
	if err := validateTerminalHead(run, events); err != nil {
		return err
	}
	if err := validateFinalizingPointer(run, current); err != nil {
		return err
	}
	if err := s.removeFile(s.currentPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("complete terminal snapshot commit: %w", err)
	}
	return syncDirectory(s.root)
}

func (s *SnapshotStore) readRunSnapshot(ctx context.Context) (runIdentity, []byte, error) {
	return s.readRunSnapshotAt(ctx, s.runPath, "")
}

func (s *SnapshotStore) readCurrentRunSnapshot(ctx context.Context, current CurrentRun) (runIdentity, []byte, error) {
	if !canonicalCurrentPaths(current, s.runID) {
		return runIdentity{}, nil, fmt.Errorf("%w: current storage references are not canonical", ErrInconsistent)
	}
	if current.RunFile == "" || current.RunFile == stableRunFile(s.runID) {
		return s.readRunSnapshot(ctx)
	}
	path := filepath.Join(s.root, filepath.FromSlash(current.RunFile))
	return s.readRunSnapshotAt(ctx, path, current.RunFile)
}

func (s *SnapshotStore) readRunSnapshotAt(ctx context.Context, path, reference string) (runIdentity, []byte, error) {
	if err := validateDirectory(filepath.Join(s.root, "runs"), "Chronicle runs directory"); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return runIdentity{}, nil, fmt.Errorf("%w: run snapshot is missing", ErrInconsistent)
		}
		return runIdentity{}, nil, err
	}
	data, err := readRegularFile(path, maxRunSnapshotBytes)
	if errors.Is(err, os.ErrNotExist) {
		return runIdentity{}, nil, fmt.Errorf("%w: run snapshot is missing", ErrInconsistent)
	}
	if err != nil {
		return runIdentity{}, nil, err
	}
	if reference != "" && reference != stableRunFile(s.runID) {
		expected, ok := activeRunDigest(reference, s.runID)
		actual := sha256.Sum256(data)
		if !ok || !bytes.Equal(expected, actual[:]) {
			return runIdentity{}, nil, fmt.Errorf("%w: active snapshot digest mismatch", ErrCorrupt)
		}
	}
	if err := contracts.Validate(ctx, contracts.RunSchemaURI, data, false); err != nil {
		return runIdentity{}, nil, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	run, err := decodeRunIdentity(data)
	if err != nil {
		return runIdentity{}, nil, fmt.Errorf("%w: malformed run snapshot", ErrCorrupt)
	}
	if err := validateRunIdentity(run); err != nil {
		return runIdentity{}, nil, err
	}
	if run.ID != s.runID {
		return runIdentity{}, nil, fmt.Errorf("%w: run snapshot identity mismatch", ErrInconsistent)
	}
	return run, data, nil
}

func decodeRunIdentity(data []byte) (runIdentity, error) {
	var run runIdentity
	if err := json.Unmarshal(data, &run); err != nil {
		return runIdentity{}, err
	}
	run.Raw = append(json.RawMessage(nil), data...)
	return run, nil
}

func validateRunIdentity(run runIdentity) error {
	created, createdErr := time.Parse(time.RFC3339, run.CreatedAt)
	updated, updatedErr := time.Parse(time.RFC3339, run.UpdatedAt)
	if createdErr != nil || updatedErr != nil || updated.Before(created) {
		return fmt.Errorf("%w: invalid run snapshot timestamps", ErrInconsistent)
	}
	sets, err := identitySets(run)
	if err != nil {
		return err
	}
	for _, capsule := range run.Capsules {
		if capsule.RunID != run.ID {
			return fmt.Errorf("%w: capsule belongs to another run", ErrInconsistent)
		}
	}
	for _, task := range run.Tasks {
		if _, ok := sets.phases[task.Phase]; !ok {
			return fmt.Errorf("%w: task phase is missing from run snapshot", ErrInconsistent)
		}
		if !optionalIdentityExists(task.ResultID, sets.results) || !optionalIdentityExists(task.CancellationID, sets.cancellations) {
			return fmt.Errorf("%w: task result or cancellation is missing", ErrInconsistent)
		}
	}
	if run.ArtifactBackend == "none" && len(run.Artifacts) != 0 {
		return fmt.Errorf("%w: artifact backend none contains artifacts", ErrInconsistent)
	}
	if run.Selection.Status == "selected" {
		eligible := false
		for _, candidate := range run.Selection.Candidates {
			eligible = eligible || candidate.Provider == run.Selection.SelectedProvider && candidate.Eligible && candidateSatisfiesNeeds(candidate, run.Selection.Needs)
		}
		if !eligible {
			return fmt.Errorf("%w: selected provider is not an eligible candidate", ErrInconsistent)
		}
	}
	if !sliceContains(run.Routing.Candidates, run.Routing.SelectedAgent) {
		return fmt.Errorf("%w: selected route agent is not a candidate", ErrInconsistent)
	}
	for _, phase := range run.Phases {
		if !allIdentitiesExist(phase.Artifacts, sets.artifacts) || !allIdentitiesExist(phase.MemoryWrites, sets.memoryWrites) || !allIdentitiesExist(phase.Validations, sets.validations) {
			return fmt.Errorf("%w: phase references missing snapshot state", ErrInconsistent)
		}
	}
	for _, result := range run.Results {
		if !optionalIdentityExists(result.TaskID, sets.tasks) {
			return fmt.Errorf("%w: result task is missing from run snapshot", ErrInconsistent)
		}
	}
	for _, cancellation := range run.Cancellations {
		if cancellation.TargetKind == "run" && cancellation.TargetID != run.ID || cancellation.TargetKind != "run" && !optionalIdentityExists(cancellation.TargetID, sets.tasks) {
			return fmt.Errorf("%w: cancellation target is missing from run snapshot", ErrInconsistent)
		}
	}
	for _, validation := range run.Validations {
		if !optionalIdentityExists(validation.Phase, sets.phases) {
			return fmt.Errorf("%w: validation phase is missing from run snapshot", ErrInconsistent)
		}
	}
	if len(sets.phases) != len(run.Phases) || len(sets.artifacts) != len(run.Artifacts) || len(sets.memoryWrites) != len(run.MemoryWrites) || len(sets.decisions) != len(run.Decisions) || len(sets.tasks) != len(run.Tasks) || len(sets.cancellations) != len(run.Cancellations) || len(sets.results) != len(run.Results) || len(sets.capsules) != len(run.Capsules) || len(sets.validations) != len(run.Validations) {
		return fmt.Errorf("%w: duplicate snapshot identity", ErrInconsistent)
	}
	return nil
}

func candidateSatisfiesNeeds(candidate providerCandidateIdentity, needs []capabilityIdentity) bool {
	for _, need := range needs {
		matched := false
		for _, capability := range candidate.Capabilities {
			matched = matched || capability.Capability == need.Capability && capability.Version == need.Version && constraintsContain(capability.Constraints, need.Constraints)
		}
		if !matched {
			return false
		}
	}
	return true
}

func constraintsContain(candidateData, needData json.RawMessage) bool {
	if len(needData) == 0 || string(needData) == "null" {
		return true
	}
	var candidate, need map[string]any
	if json.Unmarshal(candidateData, &candidate) != nil || json.Unmarshal(needData, &need) != nil {
		return false
	}
	for key, value := range need {
		if !reflect.DeepEqual(candidate[key], value) {
			return false
		}
	}
	return true
}

func validateActiveProjection(run runIdentity, current CurrentRun, expectedRunID string) error {
	if !activeStatus(run.Status) || current.ID != expectedRunID || run.ID != expectedRunID {
		return fmt.Errorf("%w: active run identity or status mismatch", ErrInconsistent)
	}
	if current.SchemaVersion != run.SchemaVersion || current.Project != run.Project || current.Goal != run.Goal || current.Status != run.Status || current.StorageMode != run.StorageMode || current.StartedAt != run.CreatedAt || current.UpdatedAt != run.UpdatedAt {
		return fmt.Errorf("%w: current projection differs from run snapshot", ErrInconsistent)
	}
	if current.SelectionID != run.Selection.ID || current.DecisionID != run.Routing.ID || current.PreflightID != run.Preflight.ID {
		return fmt.Errorf("%w: orchestration identity mismatch", ErrInconsistent)
	}
	sets, err := identitySets(run)
	if err != nil {
		return err
	}
	if _, ok := sets.phases[current.Phase]; !ok {
		return fmt.Errorf("%w: current phase is missing from run snapshot", ErrInconsistent)
	}
	if _, ok := sets.tasks[current.TaskID]; !ok {
		return fmt.Errorf("%w: current task is missing from run snapshot", ErrInconsistent)
	}
	if !taskMatches(run, current.TaskID, current.Phase, "") {
		return fmt.Errorf("%w: current task and phase disagree", ErrInconsistent)
	}
	if !optionalIdentityExists(current.CancellationID, sets.cancellations) || !optionalIdentityExists(current.ResultID, sets.results) || !optionalIdentityExists(current.CapsuleID, sets.capsules) {
		return fmt.Errorf("%w: current reference is missing from run snapshot", ErrInconsistent)
	}
	if !sameIdentitySet(current.ArtifactIDs, sets.artifacts) {
		return fmt.Errorf("%w: current artifact identities differ from run snapshot", ErrInconsistent)
	}
	if !canonicalCurrentPaths(current, expectedRunID) {
		return fmt.Errorf("%w: current storage references are not canonical", ErrInconsistent)
	}
	return nil
}

func validateFinalizingPointer(run runIdentity, current CurrentRun) error {
	if !activeStatus(current.Status) || current.ID != run.ID || current.SchemaVersion != run.SchemaVersion || current.Project != run.Project || current.Goal != run.Goal || current.StorageMode != run.StorageMode || current.StartedAt != run.CreatedAt {
		return fmt.Errorf("%w: current pointer differs from terminal snapshot", ErrInconsistent)
	}
	if current.SelectionID != run.Selection.ID || current.DecisionID != run.Routing.ID || current.PreflightID != run.Preflight.ID {
		return fmt.Errorf("%w: current orchestration identity mismatch", ErrInconsistent)
	}
	sets, err := identitySets(run)
	if err != nil {
		return err
	}
	if !optionalIdentityExists(current.Phase, sets.phases) || !optionalIdentityExists(current.TaskID, sets.tasks) || !optionalIdentityExists(current.CancellationID, sets.cancellations) || !optionalIdentityExists(current.ResultID, sets.results) || !optionalIdentityExists(current.CapsuleID, sets.capsules) {
		return fmt.Errorf("%w: current pointer references missing terminal state", ErrInconsistent)
	}
	if !taskMatches(run, current.TaskID, current.Phase, "") || !canonicalCurrentPaths(current, run.ID) {
		return fmt.Errorf("%w: current task, phase, or storage path disagrees with terminal snapshot", ErrInconsistent)
	}
	for _, artifactID := range current.ArtifactIDs {
		if !optionalIdentityExists(artifactID, sets.artifacts) {
			return fmt.Errorf("%w: current artifact is missing from terminal snapshot", ErrInconsistent)
		}
	}
	currentUpdated, currentErr := time.Parse(time.RFC3339, current.UpdatedAt)
	runUpdated, runErr := time.Parse(time.RFC3339, run.UpdatedAt)
	if currentErr != nil || runErr != nil || currentUpdated.After(runUpdated) {
		return fmt.Errorf("%w: current pointer is newer than terminal snapshot", ErrInconsistent)
	}
	return nil
}

type runIdentitySets struct {
	phases        map[string]struct{}
	artifacts     map[string]struct{}
	memoryWrites  map[string]struct{}
	decisions     map[string]struct{}
	tasks         map[string]struct{}
	cancellations map[string]struct{}
	results       map[string]struct{}
	capsules      map[string]struct{}
	validations   map[string]struct{}
}

func identitySets(run runIdentity) (runIdentitySets, error) {
	sets := runIdentitySets{
		phases: map[string]struct{}{}, artifacts: map[string]struct{}{}, memoryWrites: map[string]struct{}{}, decisions: map[string]struct{}{}, tasks: map[string]struct{}{}, cancellations: map[string]struct{}{}, results: map[string]struct{}{}, capsules: map[string]struct{}{}, validations: map[string]struct{}{},
	}
	for _, phase := range run.Phases {
		sets.phases[phase.Name] = struct{}{}
	}
	for _, artifact := range run.Artifacts {
		id, err := referenceIdentity(artifact)
		if err != nil {
			return runIdentitySets{}, fmt.Errorf("%w: malformed artifact identity", ErrInconsistent)
		}
		sets.artifacts[id] = struct{}{}
	}
	for _, item := range run.MemoryWrites {
		sets.memoryWrites[item.ID] = struct{}{}
	}
	for _, item := range run.Decisions {
		sets.decisions[item.ID] = struct{}{}
	}
	for _, item := range run.Tasks {
		sets.tasks[item.ID] = struct{}{}
	}
	for _, item := range run.Cancellations {
		sets.cancellations[item.ID] = struct{}{}
	}
	for _, item := range run.Results {
		sets.results[item.ID] = struct{}{}
	}
	for _, item := range run.Capsules {
		sets.capsules[item.ID] = struct{}{}
	}
	for _, item := range run.Validations {
		sets.validations[item.ID] = struct{}{}
	}
	return sets, nil
}

func validateEventReferences(run runIdentity, events []Event) error {
	if len(events) == 0 {
		return fmt.Errorf("%w: event log is empty", ErrInconsistent)
	}
	lifecycle, err := deriveLifecycle(events)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInconsistent, err)
	}
	sets, err := identitySets(run)
	if err != nil {
		return err
	}
	seen := runIdentitySets{
		phases: map[string]struct{}{}, artifacts: map[string]struct{}{}, memoryWrites: map[string]struct{}{}, decisions: map[string]struct{}{}, tasks: map[string]struct{}{}, cancellations: map[string]struct{}{}, results: map[string]struct{}{}, capsules: map[string]struct{}{}, validations: map[string]struct{}{},
	}
	selectionSeen, routingSeen, preflightSeen := false, false, false
	for _, event := range events {
		var refs struct {
			Phase          string          `json:"phase"`
			Agent          string          `json:"agent"`
			SelectionID    string          `json:"selectionId"`
			DecisionID     string          `json:"decisionId"`
			PreflightID    string          `json:"preflightId"`
			TaskID         string          `json:"taskId"`
			CancellationID string          `json:"cancellationId"`
			ResultID       string          `json:"resultId"`
			CapsuleID      string          `json:"capsuleId"`
			Artifact       json.RawMessage `json:"artifact"`
			MemoryWrite    json.RawMessage `json:"memoryWrite"`
			Validation     json.RawMessage `json:"validation"`
		}
		if err := json.Unmarshal(event.Raw, &refs); err != nil {
			return fmt.Errorf("%w: malformed event references", ErrCorrupt)
		}
		if refs.SelectionID != "" && refs.SelectionID != run.Selection.ID || refs.PreflightID != "" && refs.PreflightID != run.Preflight.ID {
			return fmt.Errorf("%w: event orchestration identity mismatch", ErrInconsistent)
		}
		selectionSeen = selectionSeen || refs.SelectionID == run.Selection.ID
		routingSeen = routingSeen || refs.DecisionID == run.Routing.ID
		preflightSeen = preflightSeen || refs.PreflightID == run.Preflight.ID
		recordIdentity(refs.Phase, seen.phases)
		recordIdentity(refs.TaskID, seen.tasks)
		recordIdentity(refs.CancellationID, seen.cancellations)
		recordIdentity(refs.ResultID, seen.results)
		recordIdentity(refs.CapsuleID, seen.capsules)
		if !optionalIdentityExists(refs.Phase, sets.phases) || refs.DecisionID != "" && refs.DecisionID != run.Routing.ID && !optionalIdentityExists(refs.DecisionID, sets.decisions) || !optionalIdentityExists(refs.TaskID, sets.tasks) || !optionalIdentityExists(refs.CancellationID, sets.cancellations) || !optionalIdentityExists(refs.ResultID, sets.results) || !optionalIdentityExists(refs.CapsuleID, sets.capsules) {
			return fmt.Errorf("%w: event reference is missing from run snapshot", ErrInconsistent)
		}
		if !phaseMatches(run, refs.Phase, refs.Agent) || !taskMatches(run, refs.TaskID, refs.Phase, refs.Agent) {
			return fmt.Errorf("%w: event phase or agent differs from run snapshot", ErrInconsistent)
		}
		for _, reference := range []struct {
			raw      json.RawMessage
			expected map[string]struct{}
			seen     map[string]struct{}
		}{{refs.Artifact, sets.artifacts, seen.artifacts}, {refs.MemoryWrite, sets.memoryWrites, seen.memoryWrites}, {refs.Validation, sets.validations, seen.validations}} {
			raw := string(reference.raw)
			if raw == "" || raw == "null" {
				continue
			}
			id, err := referenceIdentity([]byte(raw))
			if err != nil {
				return fmt.Errorf("%w: malformed event reference", ErrInconsistent)
			}
			if _, ok := reference.expected[id]; !ok {
				return fmt.Errorf("%w: event reference is missing from run snapshot", ErrInconsistent)
			}
			reference.seen[id] = struct{}{}
		}
	}
	if !selectionSeen || !routingSeen || !preflightSeen {
		return fmt.Errorf("%w: snapshot orchestration identities are not backed by events", ErrInconsistent)
	}
	for _, phase := range run.Phases {
		if phase.Status != "pending" && !optionalIdentityExists(phase.Name, seen.phases) {
			return fmt.Errorf("%w: phase snapshot is not backed by an event", ErrInconsistent)
		}
	}
	for _, task := range run.Tasks {
		if task.Status != "pending" && !optionalIdentityExists(task.ID, seen.tasks) {
			return fmt.Errorf("%w: task snapshot is not backed by an event", ErrInconsistent)
		}
	}
	for _, identities := range []struct {
		name     string
		expected map[string]struct{}
		seen     map[string]struct{}
	}{{"artifact", sets.artifacts, seen.artifacts}, {"memory write", sets.memoryWrites, seen.memoryWrites}, {"cancellation", sets.cancellations, seen.cancellations}, {"result", sets.results, seen.results}, {"capsule", sets.capsules, seen.capsules}} {
		if !isSubset(identities.expected, identities.seen) {
			return fmt.Errorf("%w: %s snapshot is not backed by an event", ErrInconsistent, identities.name)
		}
	}
	for _, validation := range run.Validations {
		if validation.Status != "pending" && !optionalIdentityExists(validation.ID, seen.validations) {
			return fmt.Errorf("%w: validation snapshot is not backed by an event", ErrInconsistent)
		}
	}
	if err := validateTaskSnapshotStates(run, lifecycle); err != nil {
		return err
	}
	return nil
}

func validateTaskSnapshotStates(run runIdentity, lifecycle lifecycleProjection) error {
	for _, cancellation := range run.Cancellations {
		state, present := lifecycle.cancellations[cancellation.ID]
		if !present {
			return fmt.Errorf("%w: cancellation snapshot has no request event", ErrInconsistent)
		}
		if cancellation.Status == "completed" && state != cancellationCompleted {
			return fmt.Errorf("%w: completed cancellation has no completion event", ErrInconsistent)
		}
		if cancellation.Status != "completed" && state == cancellationCompleted {
			return fmt.Errorf("%w: cancellation event advanced beyond its snapshot", ErrInconsistent)
		}
		if cancellation.TargetKind == "run" && cancellation.Status == "completed" && run.Status != "cancelled" {
			return fmt.Errorf("%w: completed run cancellation has a non-cancelled snapshot", ErrInconsistent)
		}
		if cancellation.TargetKind == "task" || cancellation.TargetKind == "background-task" {
			matched := false
			for _, task := range run.Tasks {
				if task.ID != cancellation.TargetID {
					continue
				}
				matched = true
				if task.CancellationID != cancellation.ID {
					return fmt.Errorf("%w: task does not own its cancellation", ErrInconsistent)
				}
				taskState, started := lifecycle.tasks[task.ID]
				if started && (cancellation.TargetKind == "task") != (taskState.Mode == TaskForeground) {
					return fmt.Errorf("%w: cancellation target kind differs from task mode", ErrInconsistent)
				}
				if cancellation.Status == "completed" && task.Status != string(TaskCancelled) {
					return fmt.Errorf("%w: completed task cancellation has a non-cancelled snapshot", ErrInconsistent)
				}
			}
			if !matched {
				return fmt.Errorf("%w: cancellation target task is missing", ErrInconsistent)
			}
		}
	}

	for _, task := range run.Tasks {
		state, present := lifecycle.tasks[task.ID]
		switch TaskStatus(task.Status) {
		case TaskPending, TaskSkipped:
			if present {
				return fmt.Errorf("%w: inactive task has lifecycle events", ErrInconsistent)
			}
		case TaskRunning:
			if !present || state.Status != TaskRunning {
				return fmt.Errorf("%w: running task lacks a start event", ErrInconsistent)
			}
		case TaskBlocked:
			if present && state.Status != TaskRunning {
				return fmt.Errorf("%w: blocked task has a terminal event", ErrInconsistent)
			}
		case TaskCompleted:
			if !present || state.Status != TaskCompleted || task.ResultID == "" || task.ResultID != state.ResultID || !resultMatchesTask(run, task.ResultID, task.ID) {
				return fmt.Errorf("%w: completed task disagrees with its result event", ErrInconsistent)
			}
		case TaskFailed:
			if !present || state.Status != TaskFailed {
				return fmt.Errorf("%w: failed task lacks a failure event", ErrInconsistent)
			}
		case TaskCancelled:
			if present && state.Status != TaskRunning {
				return fmt.Errorf("%w: cancelled task already reached another terminal state", ErrInconsistent)
			}
			if !completedCancellationCoversTask(run, lifecycle, task.ID, task.CancellationID, state.Mode, present) {
				return fmt.Errorf("%w: cancelled task lacks completed cancellation evidence", ErrInconsistent)
			}
		default:
			return fmt.Errorf("%w: unknown task status", ErrInconsistent)
		}
		if task.Status != string(TaskCompleted) && task.ResultID != "" {
			return fmt.Errorf("%w: non-completed task carries a result", ErrInconsistent)
		}
	}
	return nil
}

func resultMatchesTask(run runIdentity, resultID, taskID string) bool {
	for _, result := range run.Results {
		if result.ID == resultID && result.TaskID == taskID {
			return true
		}
	}
	return false
}

func completedCancellationCoversTask(run runIdentity, lifecycle lifecycleProjection, taskID, taskCancellationID string, mode TaskMode, started bool) bool {
	for _, cancellation := range run.Cancellations {
		if cancellation.Status != "completed" || lifecycle.cancellations[cancellation.ID] != cancellationCompleted {
			continue
		}
		if cancellation.TargetKind == "run" && cancellation.TargetID == run.ID && (taskCancellationID == "" || taskCancellationID == cancellation.ID) {
			return true
		}
		if cancellation.ID != taskCancellationID || cancellation.TargetID != taskID {
			continue
		}
		if cancellation.TargetKind == "task" && (!started || mode == TaskForeground) {
			return true
		}
		if cancellation.TargetKind == "background-task" && (!started || mode == TaskBackground) {
			return true
		}
	}
	return false
}

func optionalIdentityExists(value string, set map[string]struct{}) bool {
	if value == "" {
		return true
	}
	_, ok := set[value]
	return ok
}

func allIdentitiesExist(values []string, set map[string]struct{}) bool {
	for _, value := range values {
		if !optionalIdentityExists(value, set) {
			return false
		}
	}
	return true
}

func sliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func canonicalCurrentPaths(current CurrentRun, runID string) bool {
	expectedRunFile := stableRunFile(runID)
	expectedLogFile := "logs/" + runID + ".jsonl"
	validRunFile := current.RunFile == "" || current.RunFile == expectedRunFile
	if !validRunFile {
		_, validRunFile = activeRunDigest(current.RunFile, runID)
	}
	return validRunFile && (current.LogFile == "" || current.LogFile == expectedLogFile)
}

func stableRunFile(runID string) string { return "runs/" + runID + ".json" }

func activeRunDigest(reference, runID string) ([]byte, bool) {
	prefix := "runs/" + runID + "."
	if !strings.HasPrefix(reference, prefix) || !strings.HasSuffix(reference, ".json") {
		return nil, false
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(reference, prefix), ".json")
	if len(raw) != sha256.Size*2 || raw != strings.ToLower(raw) {
		return nil, false
	}
	digest, err := hex.DecodeString(raw)
	return digest, err == nil && len(digest) == sha256.Size
}

func phaseMatches(run runIdentity, phase, agent string) bool {
	if phase == "" {
		return true
	}
	for _, candidate := range run.Phases {
		if candidate.Name == phase {
			return agent == "" || candidate.Agent == "" || candidate.Agent == agent
		}
	}
	return false
}

func taskMatches(run runIdentity, taskID, phase, agent string) bool {
	if taskID == "" {
		return true
	}
	for _, task := range run.Tasks {
		if task.ID == taskID {
			return (phase == "" || task.Phase == phase) && (agent == "" || task.AgentID == agent)
		}
	}
	return false
}

func recordIdentity(value string, set map[string]struct{}) {
	if value != "" {
		set[value] = struct{}{}
	}
}

func isSubset(expected, actual map[string]struct{}) bool {
	for value := range expected {
		if _, ok := actual[value]; !ok {
			return false
		}
	}
	return true
}

func referenceIdentity(raw []byte) (string, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil && text != "" {
		return text, nil
	}
	var object struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return "", err
	}
	if object.ID != "" {
		return object.ID, nil
	}
	if object.Path != "" {
		return object.Path, nil
	}
	return "", errors.New("reference has no identity")
}

func sameIdentitySet(values []string, expected map[string]struct{}) bool {
	if len(values) != len(expected) {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := expected[value]; !ok {
			return false
		}
		seen[value] = struct{}{}
	}
	return len(seen) == len(expected)
}

func activeStatus(status string) bool {
	switch status {
	case "running", "paused", "blocked", "recovering":
		return true
	default:
		return false
	}
}

func terminalStatus(status string) bool {
	return status == "completed" || status == "failed" || status == "cancelled"
}

func validateTerminalHead(run runIdentity, events []Event) error {
	if len(events) == 0 {
		return fmt.Errorf("%w: terminal snapshot has no events", ErrInconsistent)
	}
	want := map[string]string{"completed": "run.completed", "failed": "run.failed", "cancelled": "cancellation.completed"}[run.Status]
	if events[len(events)-1].Type != want {
		return fmt.Errorf("%w: terminal snapshot disagrees with final event", ErrInconsistent)
	}
	if run.Status == "cancelled" {
		var terminal struct {
			CancellationID string `json:"cancellationId"`
		}
		if err := json.Unmarshal(events[len(events)-1].Raw, &terminal); err != nil {
			return fmt.Errorf("%w: malformed terminal cancellation", ErrCorrupt)
		}
		matched := false
		for _, cancellation := range run.Cancellations {
			matched = matched || cancellation.ID == terminal.CancellationID && cancellation.TargetKind == "run" && cancellation.TargetID == run.ID && cancellation.Status == "completed"
		}
		if !matched {
			return fmt.Errorf("%w: terminal cancellation does not close the run", ErrInconsistent)
		}
	}
	return nil
}

func containsEvent(events []Event, id string) bool {
	for _, event := range events {
		if event.ID == id {
			return true
		}
	}
	return false
}
