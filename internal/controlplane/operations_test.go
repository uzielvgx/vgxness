package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/navigator"
)

func TestOperationalInventoryReportsStaleAndExpiredWork(t *testing.T) {
	workspace := operationalTestWorkspace(t)
	storage := filepath.Join(t.TempDir(), "storage")
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	service := New(Options{StorageRoot: storage, Now: func() time.Time { return now }})
	plan := operationalTestPlan(t)
	createOperationalOrchestration(t, storage, orchestrationDocument{
		Version: orchestrationDocumentVersion, Workspace: workspace, OrchestrationID: "orchestration-stale",
		Plan: plan, Status: "pending", CreatedAt: now.Add(-3 * time.Hour).Format(time.RFC3339),
		UpdatedAt: now.Add(-2 * time.Hour).Format(time.RFC3339), PreparedBindings: map[string]string{}, Results: map[string]json.RawMessage{},
	})
	createOperationalTicket(t, storage, nativeTicketDocument{
		SchemaVersion: nativeTicketVersion, TicketID: "ticket-expired", Workspace: workspace,
		TaskID: "task-1", RunID: "run-1", State: "prepared", Deadline: now.Add(-time.Minute).Format(time.RFC3339),
	}, now.Add(-2*time.Hour))

	inventory, err := service.OperationalInventory(context.Background(), workspace, bridge.OperationalInventoryRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Health != "degraded" || len(inventory.Orchestrations) != 1 || len(inventory.NativeTickets) != 1 {
		t.Fatalf("inventory=%#v", inventory)
	}
	codes := make(map[string]bool)
	for _, finding := range inventory.Findings {
		codes[finding.Code] = true
	}
	if !codes["orchestration_stale"] || !codes["native_ticket_expired"] {
		t.Fatalf("findings=%#v", inventory.Findings)
	}
}

func TestPruneOperationsIsDryRunByDefaultAndProtectsReferencedTickets(t *testing.T) {
	workspace := operationalTestWorkspace(t)
	storage := filepath.Join(t.TempDir(), "storage")
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	old := now.Add(-40 * 24 * time.Hour)
	service := New(Options{StorageRoot: storage, Now: func() time.Time { return now }})
	plan := operationalTestPlan(t)
	createOperationalOrchestration(t, storage, orchestrationDocument{
		Version: orchestrationDocumentVersion, Workspace: workspace, OrchestrationID: "orchestration-old",
		Plan: plan, Status: "completed", CreatedAt: old.Add(-time.Hour).Format(time.RFC3339),
		UpdatedAt: old.Format(time.RFC3339), PreparedBindings: map[string]string{"task-1": "ticket-prunable"}, Results: map[string]json.RawMessage{},
	})
	createOperationalOrchestration(t, storage, orchestrationDocument{
		Version: orchestrationDocumentVersion, Workspace: workspace, OrchestrationID: "orchestration-active",
		Plan: plan, Status: "pending", CreatedAt: old.Add(-time.Hour).Format(time.RFC3339),
		UpdatedAt: old.Format(time.RFC3339), PreparedBindings: map[string]string{"task-1": "ticket-protected"}, Results: map[string]json.RawMessage{},
	})
	createOperationalTicket(t, storage, nativeTicketDocument{
		SchemaVersion: nativeTicketVersion, TicketID: "ticket-prunable", Workspace: workspace,
		TaskID: "task-1", RunID: "run-1", State: "completed", Deadline: old.Format(time.RFC3339),
	}, old)
	createOperationalTicket(t, storage, nativeTicketDocument{
		SchemaVersion: nativeTicketVersion, TicketID: "ticket-protected", Workspace: workspace,
		TaskID: "task-1", RunID: "run-2", State: "completed", Deadline: old.Format(time.RFC3339),
	}, old)
	createOperationalTicket(t, storage, nativeTicketDocument{
		SchemaVersion: nativeTicketVersion, TicketID: "ticket-young", Workspace: workspace,
		TaskID: "task-1", RunID: "run-3", State: "completed", Deadline: now.Format(time.RFC3339),
	}, now)
	createOperationalTicket(t, storage, nativeTicketDocument{
		SchemaVersion: nativeTicketVersion, TicketID: "ticket-leased", Workspace: workspace,
		TaskID: "task-1", RunID: "run-4", State: "completed", Deadline: old.Format(time.RFC3339),
	}, old)
	lease, _ := json.Marshal(nativeLease{SchemaVersion: nativeTicketVersion, TicketID: "ticket-leased", Deadline: now.Add(time.Hour).Format(time.RFC3339)})
	if err := os.WriteFile(nativeLeasePath(storage), lease, 0o600); err != nil {
		t.Fatal(err)
	}

	dryRun, err := service.PruneOperations(context.Background(), workspace, bridge.OperationalPruneRequest{OlderThanSeconds: int64((30 * 24 * time.Hour) / time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if dryRun.Applied || len(dryRun.Candidates) != 2 || len(dryRun.Removed) != 0 {
		t.Fatalf("dry-run=%#v", dryRun)
	}
	if dryRun.Candidates[0].Kind != "orchestration" || dryRun.Candidates[1].Kind != "native-ticket" {
		t.Fatalf("unsafe prune order=%#v", dryRun.Candidates)
	}
	requireOperationalFile(t, orchestrationDirectory(storage), "orchestration-old.json", true)
	requireOperationalFile(t, nativeTicketDirectory(storage), "ticket-prunable.json", true)

	applied, err := service.PruneOperations(context.Background(), workspace, bridge.OperationalPruneRequest{
		OlderThanSeconds: int64((30 * 24 * time.Hour) / time.Second), Apply: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Applied || len(applied.Removed) != 2 {
		t.Fatalf("applied=%#v", applied)
	}
	requireOperationalFile(t, orchestrationDirectory(storage), "orchestration-old.json", false)
	requireOperationalFile(t, nativeTicketDirectory(storage), "ticket-prunable.json", false)
	requireOperationalFile(t, orchestrationDirectory(storage), "orchestration-active.json", true)
	requireOperationalFile(t, nativeTicketDirectory(storage), "ticket-protected.json", true)
	requireOperationalFile(t, nativeTicketDirectory(storage), "ticket-young.json", true)
	requireOperationalFile(t, nativeTicketDirectory(storage), "ticket-leased.json", true)
	requireOperationalFile(t, orchestrationDirectory(storage), "orchestration-old.json.lock", true)
	requireOperationalFile(t, nativeTicketDirectory(storage), "ticket-prunable.json.lock", true)
}

func TestPruneOperationsRejectsUnsafeRetentionAndCorruptInventory(t *testing.T) {
	workspace := operationalTestWorkspace(t)
	storage := filepath.Join(t.TempDir(), "storage")
	service := New(Options{StorageRoot: storage})
	for _, seconds := range []int64{1, int64((11 * 365 * 24 * time.Hour) / time.Second), 1 << 62} {
		if _, err := service.PruneOperations(context.Background(), workspace, bridge.OperationalPruneRequest{OlderThanSeconds: seconds}); !errors.Is(err, bridge.ErrInvalid) {
			t.Fatalf("seconds=%d err=%v", seconds, err)
		}
	}
	if err := os.MkdirAll(nativeTicketDirectory(storage), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nativeTicketDirectory(storage), "ticket-corrupt.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("ticket-corrupt.json", filepath.Join(nativeTicketDirectory(storage), "ticket-link.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := readNativeTicket(storage, "ticket-link"); !errors.Is(err, bridge.ErrDenied) {
		t.Fatalf("symlink read err=%v", err)
	}
	if _, err := service.PruneOperations(context.Background(), workspace, bridge.OperationalPruneRequest{
		OlderThanSeconds: int64((30 * 24 * time.Hour) / time.Second), Apply: true,
	}); !errors.Is(err, bridge.ErrDenied) {
		t.Fatalf("err=%v", err)
	}
}

func operationalTestPlan(t *testing.T) navigator.Plan {
	t.Helper()
	plan, err := navigator.PlanRequest(context.Background(), navigator.Request{
		Kind: navigator.RequestKind, SchemaVersion: navigator.SchemaVersion,
		Goal: "Inspect operational state", AcceptanceCriteria: []string{"Return verified findings"},
		PolicyVersion: "operations-test-v1", MaxParallel: navigator.DefaultMaxParallel,
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func operationalTestWorkspace(t *testing.T) string {
	t.Helper()
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}

func createOperationalOrchestration(t *testing.T, root string, document orchestrationDocument) {
	t.Helper()
	if err := createOrchestrationDocument(root, document); err != nil {
		t.Fatal(err)
	}
}

func createOperationalTicket(t *testing.T, root string, document nativeTicketDocument, modified time.Time) {
	t.Helper()
	if err := createNativeTicket(root, document); err != nil {
		t.Fatal(err)
	}
	path, err := nativeTicketPath(root, document.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
}

func requireOperationalFile(t *testing.T, directory, name string, exists bool) {
	t.Helper()
	_, err := os.Lstat(filepath.Join(directory, name))
	if exists && err != nil {
		t.Fatalf("%s should exist: %v", name, err)
	}
	if !exists && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s should be absent: %v", name, err)
	}
}
