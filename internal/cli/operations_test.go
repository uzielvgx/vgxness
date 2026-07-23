package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/inspection"
)

type fakeOperationsRuntime struct {
	*fakeBridgeRuntime
	inventory    bridge.OperationalInventory
	inventoryErr error
	prune        bridge.OperationalPruneResult
	pruneErr     error
	request      bridge.OperationalPruneRequest
	workspace    string
}

func (runtime *fakeOperationsRuntime) OperationalInventory(_ context.Context, workspace string, _ bridge.OperationalInventoryRequest) (bridge.OperationalInventory, error) {
	runtime.workspace = workspace
	return runtime.inventory, runtime.inventoryErr
}

func (runtime *fakeOperationsRuntime) PruneOperations(_ context.Context, workspace string, request bridge.OperationalPruneRequest) (bridge.OperationalPruneResult, error) {
	runtime.workspace = workspace
	runtime.request = request
	return runtime.prune, runtime.pruneErr
}

func TestDeepDoctorReportsOperationalHealth(t *testing.T) {
	workspace := t.TempDir()
	for _, test := range []struct {
		name      string
		inventory bridge.OperationalInventory
		code      int
		want      string
	}{
		{
			name: "healthy", code: 0, want: "doctor=healthy",
			inventory: bridge.OperationalInventory{Health: "healthy", Orchestrations: []bridge.OrchestrationSummary{}, NativeTickets: []bridge.NativeTicketSummary{}},
		},
		{
			name: "degraded", code: 1, want: "finding=warning/orchestration_stale",
			inventory: bridge.OperationalInventory{
				Health:         "degraded",
				Orchestrations: []bridge.OrchestrationSummary{{ID: "orchestration-1", Status: "running"}},
				NativeTickets:  []bridge.NativeTicketSummary{},
				Findings: []bridge.OperationalFinding{{
					Severity: "warning", Code: "orchestration_stale", Subject: "orchestration-1", Message: "stale",
				}},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime := &fakeOperationsRuntime{fakeBridgeRuntime: &fakeBridgeRuntime{}, inventory: test.inventory}
			inspector := &fakeInspector{result: inspection.Result{Root: "/storage", Database: "/storage/memory.db", Migration: 1}}
			var stdout, stderr bytes.Buffer
			code := RunControlPlaneRuntime(context.Background(), []string{"doctor", "--deep", "--workspace", workspace},
				strings.NewReader(""), &stdout, &stderr, inspector, nil, nil, runtime)
			if code != test.code || !strings.Contains(stdout.String(), test.want) || stderr.Len() != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestMaintenancePruneDefaultsToDryRun(t *testing.T) {
	workspace := t.TempDir()
	runtime := &fakeOperationsRuntime{
		fakeBridgeRuntime: &fakeBridgeRuntime{},
		prune:             bridge.OperationalPruneResult{Workspace: workspace, Applied: false, Candidates: []bridge.OperationalPruneCandidate{}},
	}
	var stdout, stderr bytes.Buffer
	code := runMaintenance(context.Background(), []string{"prune", "--workspace", workspace}, &stdout, &stderr, runtime)
	if code != 0 || runtime.request.Apply || runtime.request.OlderThanSeconds != int64((30*24*time.Hour)/time.Second) {
		t.Fatalf("code=%d request=%#v stdout=%q stderr=%q", code, runtime.request, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"applied":false`) || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestMaintenancePruneReportsPartialRemovalOnFailure(t *testing.T) {
	workspace := t.TempDir()
	runtime := &fakeOperationsRuntime{
		fakeBridgeRuntime: &fakeBridgeRuntime{},
		prune: bridge.OperationalPruneResult{
			Workspace: workspace, Applied: true,
			Removed: []bridge.OperationalPruneCandidate{{Kind: "orchestration", ID: "old"}},
		},
		pruneErr: bridge.ErrExecution,
	}
	var stdout, stderr bytes.Buffer
	code := runMaintenance(context.Background(), []string{"prune", "--workspace", workspace, "--apply"}, &stdout, &stderr, runtime)
	if code != 1 || !strings.Contains(stdout.String(), `"id":"old"`) || !strings.Contains(stderr.String(), "execution") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestOrchestrationListUsesOperationalInventory(t *testing.T) {
	workspace := t.TempDir()
	runtime := &fakeOperationsRuntime{
		fakeBridgeRuntime: &fakeBridgeRuntime{},
		inventory: bridge.OperationalInventory{
			Health:         "healthy",
			Orchestrations: []bridge.OrchestrationSummary{{ID: "orchestration-1", Status: "completed"}},
		},
	}
	var stdout, stderr bytes.Buffer
	code := runOrchestration(context.Background(), []string{"list", "--workspace", workspace}, &stdout, &stderr, runtime)
	if code != 0 || !strings.Contains(stdout.String(), `"id":"orchestration-1"`) || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
