package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/chronicle"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/gatekeeper"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/providers"
	"github.com/vgxness/vgxness/internal/registry"
)

type fakeAdapter struct {
	descriptor providers.Descriptor
	health     providers.Health
	healthFn   func(context.Context) providers.Health
	runs       int
	last       providers.Invocation
}

func (adapter *fakeAdapter) Descriptor() providers.Descriptor { return adapter.descriptor }
func (adapter *fakeAdapter) Health(ctx context.Context) providers.Health {
	if adapter.healthFn != nil {
		return adapter.healthFn(ctx)
	}
	return adapter.health
}
func (adapter *fakeAdapter) Run(_ context.Context, invocation providers.Invocation) ([]byte, error) {
	adapter.runs++
	adapter.last = invocation
	return json.Marshal(map[string]any{
		"kind": "agent.result", "schemaVersion": "1", "resultId": "result-" + invocation.WorkUnitID, "taskId": invocation.WorkUnitID,
		"agentId": invocation.Agent.ID, "status": "success", "summary": "bounded work completed", "artifacts": []any{},
		"nextRecommended": "inspect the receipt", "risks": []any{}, "errors": []any{},
	})
}

type sqliteContinuityMemory struct{}

func (sqliteContinuityMemory) Save(ctx context.Context, opts config.Options, request memory.SaveRequest) (memory.MemoryResult, error) {
	store, err := memory.Open(ctx, filepath.Join(opts.StorageRoot, "memory.db"), nil)
	if err != nil {
		return memory.MemoryResult{}, err
	}
	defer store.Close()
	return memory.NewMemoryService(store, "vgxness-controlplane", nil).Save(ctx, request)
}

func (sqliteContinuityMemory) Search(ctx context.Context, opts config.Options, request memory.SearchRequest) ([]memory.MemoryResult, error) {
	store, err := memory.OpenRead(ctx, filepath.Join(opts.StorageRoot, "memory.db"))
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return memory.NewMemoryService(store, "vgxness-controlplane", nil).Search(ctx, request)
}

func TestServiceStatusAndDispatchTraverseBoundedRuntime(t *testing.T) {
	workspace := t.TempDir()
	canonical, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	adapter := &fakeAdapter{
		descriptor: providers.Descriptor{
			Reference: registry.ProviderReference{Provider: "opencode", ID: "primary", Version: "1"},
			Source:    registry.SourceReference{Provider: "filesystem", ID: "opencode", Path: "/test/opencode"}, InterfaceVersion: "1",
			Capabilities: []registry.Capability{{Capability: capabilityID, Version: "1"}},
		},
		health: providers.Health{Status: gatekeeper.AdapterHealthy, CheckedAt: now},
	}
	sequence := 0
	service := New(Options{
		StorageRoot: filepath.Join(t.TempDir(), "storage"), Now: func() time.Time { return now },
		AdapterFactory: func(root string) (providers.Adapter, error) {
			if root != canonical {
				t.Fatalf("factory workspace=%q want %q", root, canonical)
			}
			return adapter, nil
		},
		NewID: func(prefix string) (string, error) { sequence++; return prefix + "-" + string(rune('a'+sequence)), nil },
	})
	status, err := service.Status(context.Background(), workspace)
	if err != nil || !status.OK || status.Bridge != "healthy" || adapter.runs != 0 {
		t.Fatalf("unexpected status: %#v err=%v runs=%d", status, err, adapter.runs)
	}
	if _, err := runtimeRegistry(context.Background(), canonical, "openai/gpt-5.6-sol"); err != nil {
		t.Fatalf("runtime registry: %v", err)
	}
	response, err := service.Dispatch(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, Model: "openai/gpt-5.6-sol", Operation: bridge.WriteFiles, Goal: "Update one bounded file", AcceptanceCriteria: []string{"valid result"},
	})
	if err != nil {
		t.Fatalf("dispatch: %v runs=%d last=%#v", err, adapter.runs, adapter.last)
	}
	if !response.OK || response.Status != "completed" || response.Receipt == nil || response.Receipt.Decision != "allow" || response.Receipt.EventCount != 3 {
		t.Fatalf("unexpected response: %#v", response)
	}
	if adapter.runs != 1 || adapter.last.Operation != gatekeeper.WriteFiles || len(adapter.last.AuthorizedOperations) != 2 || adapter.last.Agent.Model != "openai/gpt-5.6-sol" {
		t.Fatalf("unexpected bounded invocation: %#v", adapter.last)
	}
	var result struct {
		TaskID  string `json:"taskId"`
		AgentID string `json:"agentId"`
	}
	if json.Unmarshal(response.Result, &result) != nil || result.TaskID != response.TaskID || result.AgentID != agentID {
		t.Fatalf("result identity mismatch: %s", response.Result)
	}
}

func TestServiceRejectsUnsupportedOperationBeforeAdapterExecution(t *testing.T) {
	service := New(Options{AdapterFactory: func(string) (providers.Adapter, error) {
		t.Fatal("invalid request reached adapter factory")
		return nil, nil
	}})
	_, err := service.Dispatch(context.Background(), t.TempDir(), bridge.DispatchRequest{ProtocolVersion: "1", Model: "openai/gpt-5.6-sol", Operation: "run-command", Goal: "execute"})
	if err == nil {
		t.Fatal("unsupported operation was accepted")
	}
}

func TestServicePersistsAndRecoversContinuityWithCuratedMemory(t *testing.T) {
	workspace := t.TempDir()
	storage := filepath.Join(t.TempDir(), "storage")
	now := time.Now().UTC()
	adapter := &fakeAdapter{
		descriptor: providers.Descriptor{
			Reference: registry.ProviderReference{Provider: "opencode", ID: "primary", Version: "1"},
			Source:    registry.SourceReference{Provider: "filesystem", ID: "opencode", Path: "/test/opencode"}, InterfaceVersion: "1",
			Capabilities: []registry.Capability{{Capability: capabilityID, Version: "1"}},
		},
		health: providers.Health{Status: gatekeeper.AdapterHealthy, CheckedAt: now},
	}
	service := New(Options{
		StorageRoot: storage, Memory: sqliteContinuityMemory{},
		AdapterFactory: func(string) (providers.Adapter, error) { return adapter, nil },
	})
	started, err := service.Dispatch(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Inspect the continuity\narchitecture and state", Continuity: bridge.ContinuityStart,
	})
	if err != nil || !started.OK || started.RunID == "" || started.CapsuleID == "" || started.StateVersion != 1 || len(started.MemoryRefs) != 1 {
		t.Fatalf("start continuity: response=%#v err=%v", started, err)
	}
	runsAfterStart := adapter.runs
	if _, err := service.Dispatch(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Start a competing run", Continuity: bridge.ContinuityStart,
	}); !errors.Is(err, bridge.ErrDenied) || adapter.runs != runsAfterStart {
		t.Fatalf("competing start was not denied before execution: err=%v runs=%d", err, adapter.runs)
	}
	firstCapsule := started.CapsuleID
	continued, err := service.Dispatch(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, Model: "openai/gpt-5.6-sol", Operation: bridge.WriteFiles,
		Goal: "Inspect the continuity\narchitecture and state", Continuity: bridge.ContinuityContinue, RunID: started.RunID,
	})
	if err != nil || !continued.OK || continued.RunID != started.RunID || continued.CapsuleID == "" || continued.CapsuleID == firstCapsule || continued.StateVersion != 2 || len(continued.MemoryRefs) != 2 {
		t.Fatalf("continue continuity: response=%#v err=%v", continued, err)
	}
	var packet struct {
		Context struct {
			Inputs struct {
				Continuity struct {
					Mode            bridge.ContinuityMode `json:"mode"`
					RunID           string                `json:"runId"`
					PreviousCapsule continuityCapsule     `json:"previousCapsule"`
				} `json:"continuity"`
				MemoryContext []map[string]any `json:"memoryContext"`
			} `json:"inputs"`
		} `json:"context"`
	}
	if err := json.Unmarshal(adapter.last.Packet, &packet); err != nil {
		t.Fatal(err)
	}
	if packet.Context.Inputs.Continuity.Mode != bridge.ContinuityContinue || packet.Context.Inputs.Continuity.RunID != started.RunID || packet.Context.Inputs.Continuity.PreviousCapsule.CapsuleID != firstCapsule || len(packet.Context.Inputs.MemoryContext) != 1 {
		t.Fatalf("continuation packet lost bounded context: %#v", packet.Context.Inputs)
	}
	store, err := chronicle.NewSnapshotStore(storage, started.RunID)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := store.Recover(context.Background())
	if err != nil || !recovered.CurrentPresent || recovered.Status != "paused" || recovered.Current.CapsuleID != continued.CapsuleID {
		t.Fatalf("recover continuity: state=%#v err=%v", recovered, err)
	}
	var snapshot runSnapshot
	if json.Unmarshal(recovered.Run, &snapshot) != nil || len(snapshot.Tasks) != 2 || len(snapshot.Results) != 2 || len(snapshot.MemoryWrites) != 2 || len(snapshot.Capsules) != 2 {
		t.Fatalf("unexpected recovered snapshot: %#v", snapshot)
	}
	finished, err := service.Dispatch(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Inspect the continuity\narchitecture and state", Continuity: bridge.ContinuityFinish, RunID: started.RunID,
	})
	if err != nil || !finished.OK || finished.StateVersion != 3 || len(finished.MemoryRefs) != 3 {
		t.Fatalf("finish continuity: response=%#v err=%v", finished, err)
	}
	recovered, err = store.Recover(context.Background())
	if err != nil || recovered.CurrentPresent || recovered.Status != "completed" {
		t.Fatalf("recover terminal continuity: state=%#v err=%v", recovered, err)
	}
	if json.Unmarshal(recovered.Run, &snapshot) != nil || len(snapshot.Tasks) != 3 || len(snapshot.Capsules) != 3 || snapshot.Status != "completed" {
		t.Fatalf("unexpected terminal snapshot: %#v", snapshot)
	}
}

func TestContinuityCompletionRetriesEveryDurableBoundary(t *testing.T) {
	for _, boundary := range []string{
		"after-memory-commit",
		"after-memory-written",
		"after-capsule-written",
		"before-snapshot-publication",
		"after-snapshot-publication",
	} {
		t.Run(boundary, func(t *testing.T) {
			workspace := t.TempDir()
			storage := filepath.Join(t.TempDir(), "storage")
			fired := false
			service := New(Options{
				StorageRoot: storage,
				Memory:      sqliteContinuityMemory{},
				ContinuityFault: func(step string) error {
					if step == boundary && !fired {
						fired = true
						return errors.New("boundary failure")
					}
					return nil
				},
			})
			paths, err := config.Prepare(context.Background(), config.Options{StorageRoot: storage, ProjectDir: workspace})
			if err != nil {
				t.Fatal(err)
			}
			input := bridge.DispatchRequest{Goal: "finish replayable continuity", Continuity: bridge.ContinuityStart, Operation: bridge.WriteFiles}
			state, err := service.openContinuity(context.Background(), paths, workspace, input)
			if err != nil {
				t.Fatal(err)
			}
			ids, err := service.executionIdentities(state)
			if err != nil {
				t.Fatal(err)
			}
			const taskID = "task-replay"
			if err := service.stageContinuity(context.Background(), state, input, taskID, ids.packetID, ids.loopID); err != nil {
				t.Fatal(err)
			}
			if _, err := service.appendContinuityEvent(context.Background(), state.log, "task.started", map[string]any{"taskId": taskID, "phase": "apply", "agent": agentID}); err != nil {
				t.Fatal(err)
			}
			if _, err := service.appendContinuityEvent(context.Background(), state.log, "task.completed", map[string]any{"taskId": taskID, "resultId": "result-task-replay"}); err != nil {
				t.Fatal(err)
			}
			state.mode = bridge.ContinuityFinish
			// Preserve only the published staging projection. The retry below is
			// intentionally reconstructed from this baseline and durable stores,
			// rather than relying on mutations retained by the first call.
			stagedData, err := json.Marshal(state.snapshot)
			if err != nil {
				t.Fatal(err)
			}
			var stagedSnapshot runSnapshot
			if err := json.Unmarshal(stagedData, &stagedSnapshot); err != nil {
				t.Fatal(err)
			}
			result, _ := json.Marshal(map[string]any{
				"kind": "agent.result", "schemaVersion": "1", "resultId": "result-task-replay", "taskId": taskID,
				"agentId": agentID, "status": "success", "summary": "completed once", "artifacts": []any{},
				"nextRecommended": "done", "risks": []any{}, "errors": []any{},
			})
			if _, err := service.completeContinuity(context.Background(), state, input, taskID, result, false); err == nil || !fired {
				t.Fatalf("first completion did not fail at %s: %v", boundary, err)
			}
			replay := &continuityState{
				mode: bridge.ContinuityFinish, runID: state.runID, project: state.project,
				store: state.store, log: state.log, snapshot: stagedSnapshot, staged: state.staged,
				decisionID: state.decisionID,
			}
			outcome, err := service.completeContinuity(context.Background(), replay, input, taskID, result, false)
			if err != nil || outcome.capsuleID != completionIdentity("capsule", state.runID, taskID) {
				t.Fatalf("retry outcome=%#v err=%v", outcome, err)
			}
			events, err := state.log.Read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			counts := map[string]int{}
			for _, event := range events {
				counts[event.Type]++
			}
			if counts["memory.written"] != 1 || counts["capsule.written"] != 1 || counts["run.completed"] != 1 {
				t.Fatalf("duplicate completion evidence: %#v", counts)
			}
			memories, err := service.memory.Search(context.Background(), config.Options{StorageRoot: storage}, memory.SearchRequest{
				Query: "continuity", Project: filepath.Base(workspace), Scope: memory.ScopeProject,
				Type: "continuity", TopicKey: "run/" + state.runID + "/" + taskID, Limit: 2,
			})
			if err != nil || len(memories) != 1 {
				t.Fatalf("memory count=%d err=%v", len(memories), err)
			}
			recovered, err := state.store.Recover(context.Background())
			if err != nil || recovered.Status != "completed" || recovered.CurrentPresent {
				t.Fatalf("recovery=%#v err=%v", recovered, err)
			}
		})
	}
}

func TestServiceReviewChangesInjectsGitEvidenceWithoutCommandPermission(t *testing.T) {
	workspace := t.TempDir()
	canonical, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	adapter := &fakeAdapter{
		descriptor: providers.Descriptor{
			Reference: registry.ProviderReference{Provider: "opencode", ID: "primary", Version: "1"},
			Source:    registry.SourceReference{Provider: "filesystem", ID: "opencode", Path: "/test/opencode"}, InterfaceVersion: "1",
			Capabilities: []registry.Capability{{Capability: capabilityID, Version: "1"}},
		},
		health: providers.Health{Status: gatekeeper.AdapterHealthy, CheckedAt: now},
	}
	service := New(Options{
		StorageRoot: filepath.Join(t.TempDir(), "storage"), Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return adapter, nil },
		GitInspector: func(_ context.Context, root string) (GitEvidence, error) {
			if root != canonical {
				t.Fatalf("Git workspace=%q want %q", root, canonical)
			}
			return GitEvidence{StatusShort: " M file.go\n", WorktreeDiff: "diff --git a/file.go b/file.go\n"}, nil
		},
	})
	response, err := service.Dispatch(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, Model: "openai/gpt-5.6-sol", Operation: bridge.ReviewChanges, Goal: "Review current changes",
	})
	if err != nil || !response.OK {
		t.Fatalf("review dispatch: response=%#v err=%v", response, err)
	}
	if adapter.last.Operation != gatekeeper.ReadFiles || adapter.last.Agent.Permissions.MayRunCommands || len(adapter.last.AuthorizedOperations) != 1 {
		t.Fatalf("review broadened permissions: %#v", adapter.last)
	}
	var packet struct {
		Context struct {
			Inputs struct {
				Operation bridge.Operation `json:"operation"`
				Git       GitEvidence      `json:"git"`
			} `json:"inputs"`
		} `json:"context"`
	}
	if err := json.Unmarshal(adapter.last.Packet, &packet); err != nil {
		t.Fatal(err)
	}
	if packet.Context.Inputs.Operation != bridge.ReviewChanges || packet.Context.Inputs.Git.StatusShort != " M file.go\n" || packet.Context.Inputs.Git.WorktreeDiff == "" {
		t.Fatalf("missing bounded Git evidence: %#v", packet)
	}
}

func TestServiceStatusBoundsStuckProviderProbe(t *testing.T) {
	adapter := &fakeAdapter{healthFn: func(ctx context.Context) providers.Health {
		<-ctx.Done()
		return providers.Health{Status: gatekeeper.AdapterUnavailable, CheckedAt: time.Now().UTC()}
	}}
	service := New(Options{
		StatusTimeout:  10 * time.Millisecond,
		AdapterFactory: func(string) (providers.Adapter, error) { return adapter, nil },
	})
	started := time.Now()
	_, err := service.Status(context.Background(), t.TempDir())
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatalf("status probe was not bounded: err=%v elapsed=%v", err, time.Since(started))
	}
}
