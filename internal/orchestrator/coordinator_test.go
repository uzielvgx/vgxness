package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/chronicle"
	"github.com/vgxness/vgxness/internal/gatekeeper"
	"github.com/vgxness/vgxness/internal/hooks"
	"github.com/vgxness/vgxness/internal/providers"
)

type fakeRunner struct {
	mu      sync.Mutex
	run     func(context.Context, providers.Request) (providers.Receipt, error)
	prepare func(context.Context, providers.Request) (providers.Prepared, error)
}

func (f *fakeRunner) Prepare(ctx context.Context, request providers.Request) (providers.Prepared, error) {
	f.mu.Lock()
	prepare := f.prepare
	f.mu.Unlock()
	if prepare != nil {
		return prepare(ctx, request)
	}
	return providers.Prepared{}, nil
}

func (f *fakeRunner) Accept(context.Context, providers.Prepared, []byte) (providers.Receipt, error) {
	return providers.Receipt{}, nil
}

func (f *fakeRunner) Run(ctx context.Context, request providers.Request) (providers.Receipt, error) {
	f.mu.Lock()
	run := f.run
	f.mu.Unlock()
	return run(ctx, request)
}

func TestCoordinatorRecordsSuccessfulForegroundLifecycle(t *testing.T) {
	coordinator, log, runner := testCoordinator(t, Limits{MaxIterations: 3, MaxBackground: 1, MaxDuration: time.Second, CleanupTimeout: time.Second})
	runner.run = func(context.Context, providers.Request) (providers.Receipt, error) {
		return providers.Receipt{ExecutionID: "execution-1", Result: resultDocumentFor(t, "success")}, nil
	}

	receipt, err := coordinator.Run(context.Background(), testRequest(t, chronicle.TaskForeground, nil))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != chronicle.TaskCompleted || receipt.Provider == nil {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
	assertEventTypes(t, receipt.Events, "task.started", "task.completed", "result.accepted")
	events, err := log.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	states, err := chronicle.DeriveTaskStates(events)
	if err != nil {
		t.Fatal(err)
	}
	if states["work-1"].Status != chronicle.TaskCompleted || states["work-1"].ResultID != "result-1" {
		t.Fatalf("unexpected durable task state: %+v", states["work-1"])
	}
}

func TestCoordinatorDispatchesOnlyCommittedLifecycleEvents(t *testing.T) {
	log, err := chronicle.NewEventLog(t.TempDir(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{run: func(context.Context, providers.Request) (providers.Receipt, error) {
		return providers.Receipt{ExecutionID: "execution-1", Result: resultDocumentFor(t, "success")}, nil
	}}
	var observed []hooks.Event
	dispatcher, err := hooks.New(hooks.Options{},
		func(ctx context.Context, event hooks.Event) error {
			persisted, readErr := log.Read(ctx)
			if readErr != nil {
				t.Fatalf("read committed event: %v", readErr)
			}
			found := false
			for _, item := range persisted {
				if item.ID == hookEventID(event) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("hook observed event before Chronicle commit: %#v", event)
			}
			observed = append(observed, event)
			return nil
		},
		func(context.Context, hooks.Event) error { panic("observer failure") },
	)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := New(log, runner, Limits{MaxIterations: 3, MaxBackground: 1, MaxDuration: time.Second, CleanupTimeout: time.Second}, WithDispatcher(dispatcher))
	if err != nil {
		t.Fatal(err)
	}
	configureIDs(coordinator)
	receipt, err := coordinator.Run(context.Background(), testRequest(t, chronicle.TaskForeground, nil))
	if err != nil || receipt.Status != chronicle.TaskCompleted {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	if len(observed) != 2 || observed[0].Name() != hooks.TaskStartedName || observed[1].Name() != hooks.TaskSucceededName ||
		hookEventID(observed[0]) != receipt.Events[0].ID || hookEventID(observed[1]) != receipt.Events[1].ID {
		t.Fatalf("observed=%#v receipt=%#v", observed, receipt.Events)
	}
}

func TestCoordinatorSlowSuccessHookCannotPreemptResultAccepted(t *testing.T) {
	log, err := chronicle.NewEventLog(t.TempDir(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := hooks.New(hooks.Options{HandlerTimeout: 100 * time.Millisecond}, func(ctx context.Context, event hooks.Event) error {
		if event.Name() == hooks.TaskSucceededName {
			<-ctx.Done()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{run: func(context.Context, providers.Request) (providers.Receipt, error) {
		return providers.Receipt{ExecutionID: "execution-1", Result: resultDocumentFor(t, "success")}, nil
	}}
	coordinator, err := New(log, runner, Limits{MaxIterations: 3, MaxBackground: 1, MaxDuration: time.Second, CleanupTimeout: 50 * time.Millisecond}, WithDispatcher(dispatcher))
	if err != nil {
		t.Fatal(err)
	}
	configureIDs(coordinator)
	receipt, err := coordinator.Run(context.Background(), testRequest(t, chronicle.TaskForeground, nil))
	if err != nil || receipt.Status != chronicle.TaskCompleted {
		t.Fatalf("slow observer changed success: receipt=%+v err=%v", receipt, err)
	}
	events, err := log.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertEventTypes(t, events, "task.started", "task.completed", "result.accepted")
}

func TestCoordinatorSlowStartHookCannotExpireNativePrepare(t *testing.T) {
	log, err := chronicle.NewEventLog(t.TempDir(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := hooks.New(hooks.Options{HandlerTimeout: 200 * time.Millisecond}, func(ctx context.Context, event hooks.Event) error {
		if event.Name() == hooks.TaskStartedName {
			<-ctx.Done()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{prepare: func(ctx context.Context, _ providers.Request) (providers.Prepared, error) {
		if err := ctx.Err(); err != nil {
			return providers.Prepared{}, err
		}
		return providers.Prepared{}, nil
	}}
	coordinator, err := New(log, runner, Limits{MaxIterations: 3, MaxBackground: 1, MaxDuration: time.Second, CleanupTimeout: time.Second}, WithDispatcher(dispatcher))
	if err != nil {
		t.Fatal(err)
	}
	configureIDs(coordinator)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, receipt, err := coordinator.StartNative(ctx, testRequest(t, chronicle.TaskForeground, nil))
	if err != nil || receipt.Status != chronicle.TaskRunning {
		t.Fatalf("slow start observer changed prepare: receipt=%+v err=%v", receipt, err)
	}
}

func TestCoordinatorTaskFailureWithoutExitCodeUsesUnknownSentinel(t *testing.T) {
	log, err := chronicle.NewEventLog(t.TempDir(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	var observed hooks.TaskFailed
	dispatcher, err := hooks.New(hooks.Options{}, func(_ context.Context, event hooks.Event) error {
		if failed, ok := event.(hooks.TaskFailed); ok {
			observed = failed
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{run: func(context.Context, providers.Request) (providers.Receipt, error) {
		return providers.Receipt{}, &providers.Failure{Category: providers.FailureUnavailable, Recoverable: true}
	}}
	coordinator, err := New(log, runner, Limits{MaxIterations: 3, MaxBackground: 1, MaxDuration: time.Second, CleanupTimeout: time.Second}, WithDispatcher(dispatcher))
	if err != nil {
		t.Fatal(err)
	}
	configureIDs(coordinator)
	_, _ = coordinator.Run(context.Background(), testRequest(t, chronicle.TaskForeground, nil))
	if observed.Meta.ID == "" || observed.ExitCode != -1 {
		t.Fatalf("task failure exit code=%d event=%#v", observed.ExitCode, observed)
	}
	coordinator.dispatchTaskEvents(context.Background(), []chronicle.Event{{
		ID: "explicit-zero", RunID: "run-1", At: time.Now().UTC().Format(time.RFC3339Nano), Type: "task.failed",
		Raw: json.RawMessage(`{"taskId":"work-1","failure":{"exitCode":0}}`),
	}})
	if observed.Meta.ID != "explicit-zero" || observed.ExitCode != 0 {
		t.Fatalf("explicit zero exit code was not retained: %#v", observed)
	}
}

func TestCoordinatorDoesNotDispatchUncommittedStart(t *testing.T) {
	log, err := chronicle.NewEventLog(t.TempDir(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(log.Path(), 0o700); err != nil {
		t.Fatal(err)
	}
	var calls int
	dispatcher, err := hooks.New(hooks.Options{}, func(context.Context, hooks.Event) error { calls++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := New(log, &fakeRunner{}, Limits{MaxIterations: 1, MaxBackground: 0, MaxDuration: time.Second, CleanupTimeout: time.Second}, WithDispatcher(dispatcher))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := coordinator.StartNative(context.Background(), testRequest(t, chronicle.TaskForeground, nil)); err == nil {
		t.Fatal("expected Chronicle append failure")
	}
	if calls != 0 {
		t.Fatalf("uncommitted event dispatched %d times", calls)
	}
}

func hookEventID(event hooks.Event) string {
	switch event := event.(type) {
	case hooks.TaskStarted:
		return event.Meta.ID
	case hooks.TaskSucceeded:
		return event.Meta.ID
	case hooks.TaskFailed:
		return event.Meta.ID
	default:
		return ""
	}
}

func TestCoordinatorRecordsStructuredNonSuccessAndProviderFailure(t *testing.T) {
	t.Run("structured result", func(t *testing.T) {
		coordinator, _, runner := testCoordinator(t, Limits{MaxIterations: 3, MaxBackground: 1, MaxDuration: time.Second, CleanupTimeout: time.Second})
		runner.run = func(context.Context, providers.Request) (providers.Receipt, error) {
			return providers.Receipt{Result: resultDocumentFor(t, "blocked")}, nil
		}
		receipt, err := coordinator.Run(context.Background(), testRequest(t, chronicle.TaskBackground, nil))
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Status != chronicle.TaskFailed {
			t.Fatalf("unexpected status: %s", receipt.Status)
		}
		assertEventTypes(t, receipt.Events, "background.started", "background.failed", "result.accepted")
		assertFailureCategory(t, receipt.Events[1], "agent-blocked")
		assertFailureNextSafeAction(t, receipt.Events[1], "resolve the reported blocker before scheduling another bounded attempt")
	})

	t.Run("provider failure", func(t *testing.T) {
		coordinator, _, runner := testCoordinator(t, Limits{MaxIterations: 3, MaxBackground: 1, MaxDuration: time.Second, CleanupTimeout: time.Second})
		runner.run = func(context.Context, providers.Request) (providers.Receipt, error) {
			return providers.Receipt{}, &providers.Failure{Category: providers.FailureUnavailable, Recoverable: true}
		}
		receipt, err := coordinator.Run(context.Background(), testRequest(t, chronicle.TaskForeground, nil))
		var failure *providers.Failure
		if !errors.As(err, &failure) || receipt.Status != chronicle.TaskFailed {
			t.Fatalf("expected categorized provider failure, receipt=%+v err=%v", receipt, err)
		}
		assertEventTypes(t, receipt.Events, "task.started", "task.failed")
		assertFailureCategory(t, receipt.Events[1], "provider-unavailable")
	})

	t.Run("prompt composition failure", func(t *testing.T) {
		coordinator, _, runner := testCoordinator(t, Limits{MaxIterations: 3, MaxBackground: 1, MaxDuration: time.Second, CleanupTimeout: time.Second})
		runner.run = func(context.Context, providers.Request) (providers.Receipt, error) {
			return providers.Receipt{}, providers.ErrInvalidPrompt
		}
		receipt, err := coordinator.Run(context.Background(), testRequest(t, chronicle.TaskForeground, nil))
		if !errors.Is(err, providers.ErrInvalidPrompt) || receipt.Status != chronicle.TaskFailed {
			t.Fatalf("expected prompt composition failure, receipt=%+v err=%v", receipt, err)
		}
		assertEventTypes(t, receipt.Events, "task.started", "task.failed")
		assertFailureCategory(t, receipt.Events[1], "invalid-prompt-composition")
	})
}

func TestNativeReplayRequiresExactFailedResultAndFailureCategory(t *testing.T) {
	t.Run("failed result identity", func(t *testing.T) {
		coordinator, _, _ := testCoordinator(t, Limits{MaxIterations: 3, MaxBackground: 1, MaxDuration: time.Second, CleanupTimeout: time.Second})
		request := testRequest(t, chronicle.TaskForeground, nil)
		packet, err := decodePacket(context.Background(), request.Packet)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := coordinator.appendStarted(context.Background(), packet, request); err != nil {
			t.Fatal(err)
		}
		digest := nativeEvidenceDigest(json.RawMessage(`{"resultId":"result-1","taskId":"work-1","status":"blocked"}`))
		if _, err := coordinator.appendFailed(context.Background(), packet, request.Mode, map[string]any{"category": "agent-blocked", "resultId": "result-1", "digest": digest, "nextSafeAction": "resolve blocker"}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := coordinator.replayNativeResult(context.Background(), resultDocument{ResultID: "result-2", TaskID: "work-1", Status: "blocked"}, digest); !errors.Is(err, ErrDurability) {
			t.Fatalf("mismatched result replay error=%v", err)
		}
		if _, _, err := coordinator.replayNativeResult(context.Background(), resultDocument{ResultID: "result-1", TaskID: "work-1", Status: "blocked"}, nativeEvidenceDigest("altered")); !errors.Is(err, ErrDurability) {
			t.Fatalf("mismatched content replay error=%v", err)
		}
		events, ok, err := coordinator.replayNativeResult(context.Background(), resultDocument{ResultID: "result-1", TaskID: "work-1", Status: "blocked"}, digest)
		if err != nil || !ok || len(events) != 2 || events[1].Type != "result.accepted" {
			t.Fatalf("exact replay events=%v ok=%v err=%v", events, ok, err)
		}
	})

	t.Run("host failure category", func(t *testing.T) {
		coordinator, _, _ := testCoordinator(t, Limits{MaxIterations: 3, MaxBackground: 1, MaxDuration: time.Second, CleanupTimeout: time.Second})
		request := testRequest(t, chronicle.TaskForeground, nil)
		packet, err := decodePacket(context.Background(), request.Packet)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := coordinator.appendStarted(context.Background(), packet, request); err != nil {
			t.Fatal(err)
		}
		digest := nativeEvidenceDigest("run-1", "work-1", request.Mode, "native-subagent-failed")
		if _, err := coordinator.appendFailed(context.Background(), packet, request.Mode, map[string]any{"category": "native-subagent-failed", "digest": digest, "nextSafeAction": "retry"}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := coordinator.replayNativeFailure(context.Background(), "work-1", "native-subagent-deadline", digest); !errors.Is(err, ErrDurability) {
			t.Fatalf("mismatched category replay error=%v", err)
		}
		if _, _, err := coordinator.replayNativeFailure(context.Background(), "work-1", "native-subagent-failed", nativeEvidenceDigest("altered")); !errors.Is(err, ErrDurability) {
			t.Fatalf("mismatched digest replay error=%v", err)
		}
		if events, ok, err := coordinator.replayNativeFailure(context.Background(), "work-1", "native-subagent-failed", digest); err != nil || !ok || len(events) != 1 {
			t.Fatalf("exact failure replay events=%v ok=%v err=%v", events, ok, err)
		}
	})
}

func TestStartNativeReportsPendingWhenStartEventWasNotCommitted(t *testing.T) {
	coordinator, log, _ := testCoordinator(t, Limits{MaxIterations: 3, MaxBackground: 1, MaxDuration: time.Second, CleanupTimeout: time.Second})
	if err := os.MkdirAll(log.Path(), 0o700); err != nil {
		t.Fatal(err)
	}
	_, receipt, err := coordinator.StartNative(context.Background(), testRequest(t, chronicle.TaskForeground, nil))
	if err == nil || receipt.Status != chronicle.TaskPending || len(receipt.Events) != 0 {
		t.Fatalf("uncommitted start was reported as active: receipt=%+v err=%v", receipt, err)
	}
}

func TestNewRejectsTypedNilRunner(t *testing.T) {
	log, err := chronicle.NewEventLog(t.TempDir(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	var runner *fakeRunner
	_, err = New(log, runner, Limits{MaxIterations: 1, MaxBackground: 0, MaxDuration: time.Second, CleanupTimeout: time.Second})
	if !errors.Is(err, ErrInvalidCoordinator) {
		t.Fatalf("expected typed-nil runner rejection, got %v", err)
	}
}

func TestCoordinatorRecordsCancellationAndDeadlineTermination(t *testing.T) {
	t.Run("caller cancellation", func(t *testing.T) {
		coordinator, log, runner := testCoordinator(t, Limits{MaxIterations: 3, MaxBackground: 1, MaxDuration: time.Second, CleanupTimeout: time.Second})
		entered := make(chan struct{})
		runner.run = func(ctx context.Context, _ providers.Request) (providers.Receipt, error) {
			close(entered)
			<-ctx.Done()
			return providers.Receipt{}, ctx.Err()
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		var receipt Receipt
		var runErr error
		go func() {
			receipt, runErr = coordinator.Run(ctx, testRequest(t, chronicle.TaskForeground, nil))
			close(done)
		}()
		<-entered
		cancel()
		<-done
		if !errors.Is(runErr, context.Canceled) || receipt.Status != chronicle.TaskCancelled || receipt.CancellationID == "" {
			t.Fatalf("unexpected cancellation: receipt=%+v err=%v", receipt, runErr)
		}
		assertEventTypes(t, receipt.Events, "task.started", "cancellation.requested", "cancellation.completed", "loop.terminated")
		events, err := log.Read(context.Background())
		if err != nil || len(events) != 4 {
			t.Fatalf("cancellation evidence was not durable: events=%d err=%v", len(events), err)
		}
	})

	t.Run("expired packet", func(t *testing.T) {
		coordinator, _, runner := testCoordinator(t, Limits{MaxIterations: 3, MaxBackground: 1, MaxDuration: time.Second, CleanupTimeout: time.Second})
		called := false
		runner.run = func(context.Context, providers.Request) (providers.Receipt, error) {
			called = true
			return providers.Receipt{}, nil
		}
		past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
		receipt, err := coordinator.Run(context.Background(), testRequest(t, chronicle.TaskForeground, func(packet map[string]any) {
			packet["loop"].(map[string]any)["deadline"] = past
		}))
		if !errors.Is(err, ErrLoopTerminated) || called || receipt.Status != chronicle.TaskSkipped {
			t.Fatalf("expired packet executed: receipt=%+v called=%v err=%v", receipt, called, err)
		}
		assertEventTypes(t, receipt.Events, "loop.terminated")
	})
}

func TestCoordinatorEnforcesCrossInstanceSlots(t *testing.T) {
	root := t.TempDir()
	log, err := chronicle.NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	runner := &fakeRunner{run: func(context.Context, providers.Request) (providers.Receipt, error) {
		close(entered)
		<-release
		return providers.Receipt{Result: resultDocumentFor(t, "success")}, nil
	}}
	limits := Limits{MaxIterations: 3, MaxBackground: 1, MaxDuration: time.Second, CleanupTimeout: time.Second}
	first := mustCoordinator(t, log, runner, limits)
	second := mustCoordinator(t, log, runner, limits)
	configureIDs(first)
	configureIDs(second)

	done := make(chan error)
	go func() {
		_, runErr := first.Run(context.Background(), testRequest(t, chronicle.TaskForeground, nil))
		done <- runErr
	}()
	<-entered
	if _, err := second.Run(context.Background(), testRequest(t, chronicle.TaskForeground, func(packet map[string]any) {
		packet["executionId"] = "execution-2"
		packet["context"].(map[string]any)["taskId"] = "work-2"
	})); !errors.Is(err, ErrCoordinatorBusy) {
		t.Fatalf("expected foreground slot exhaustion, got %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCoordinatorEnforcesBackgroundCapacity(t *testing.T) {
	root := t.TempDir()
	log, err := chronicle.NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	runner := &fakeRunner{run: func(context.Context, providers.Request) (providers.Receipt, error) {
		close(entered)
		<-release
		return providers.Receipt{Result: resultDocumentFor(t, "success")}, nil
	}}
	limits := Limits{MaxIterations: 3, MaxBackground: 1, MaxDuration: time.Second, CleanupTimeout: time.Second}
	first := mustCoordinator(t, log, runner, limits)
	second := mustCoordinator(t, log, runner, limits)
	configureIDs(first)
	configureIDs(second)
	firstRequest := testRequest(t, chronicle.TaskBackground, nil)
	secondRequest := testRequest(t, chronicle.TaskBackground, func(packet map[string]any) {
		packet["executionId"] = "execution-2"
		packet["context"].(map[string]any)["taskId"] = "work-2"
	})

	done := make(chan error)
	go func() {
		_, runErr := first.Run(context.Background(), firstRequest)
		done <- runErr
	}()
	<-entered
	if _, err := second.Run(context.Background(), secondRequest); !errors.Is(err, ErrCoordinatorBusy) {
		t.Fatalf("expected background slot exhaustion, got %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCoordinatorRejectsRequestedBudgetAboveCeiling(t *testing.T) {
	coordinator, _, runner := testCoordinator(t, Limits{MaxIterations: 1, MaxBackground: 0, MaxDuration: time.Second, CleanupTimeout: time.Second})
	called := false
	runner.run = func(context.Context, providers.Request) (providers.Receipt, error) {
		called = true
		return providers.Receipt{}, nil
	}
	receipt, err := coordinator.Run(context.Background(), testRequest(t, chronicle.TaskForeground, nil))
	if !errors.Is(err, ErrLoopTerminated) || called {
		t.Fatalf("over-budget packet executed: receipt=%+v called=%v err=%v", receipt, called, err)
	}
	assertEventTypes(t, receipt.Events, "loop.terminated")
	var body struct {
		Data struct {
			TerminalReason string `json:"terminalReason"`
		} `json:"data"`
	}
	if err := json.Unmarshal(receipt.Events[0].Raw, &body); err != nil || body.Data.TerminalReason != "budget_exhausted" {
		t.Fatalf("unexpected loop evidence: %s err=%v", receipt.Events[0].Raw, err)
	}
}

func testCoordinator(t *testing.T, limits Limits) (*Coordinator, *chronicle.EventLog, *fakeRunner) {
	t.Helper()
	log, err := chronicle.NewEventLog(t.TempDir(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	coordinator := mustCoordinator(t, log, runner, limits)
	configureIDs(coordinator)
	return coordinator, log, runner
}

func mustCoordinator(t *testing.T, log *chronicle.EventLog, runner ProviderRunner, limits Limits) *Coordinator {
	t.Helper()
	coordinator, err := New(log, runner, limits)
	if err != nil {
		t.Fatal(err)
	}
	return coordinator
}

func configureIDs(coordinator *Coordinator) {
	var sequence int
	coordinator.newID = func() (string, error) {
		sequence++
		return fmt.Sprintf("event-%d", sequence), nil
	}
}

func testRequest(t *testing.T, mode chronicle.TaskMode, mutate func(map[string]any)) providers.Request {
	t.Helper()
	packet := map[string]any{
		"kind": "execution.packet", "schemaVersion": "1", "executionId": "execution-1", "selectionId": "selection-1", "decisionId": "decision-1",
		"context": map[string]any{
			"kind": "context.packet", "schemaVersion": "1", "packetId": "packet-1", "runId": "run-1", "taskId": "work-1", "phase": "apply", "goal": "implement",
			"scope": map[string]any{"included": []any{"/workspace"}, "excluded": []any{}}, "inputs": map[string]any{},
			"allowedPaths": []any{"/workspace"}, "allowedTools": []any{"shell"}, "artifactRefs": []any{}, "skillRefs": []any{},
			"acceptanceCriteria": []any{"tests pass"}, "approvalState": "not-required",
			"returnContract": "https://vgxness.dev/schemas/execution.schema.json#/$defs/agentResult",
		},
		"loop":           map[string]any{"kind": "loop.control", "schemaVersion": "1", "loopId": "loop-1", "loopType": "agent", "maxIterations": 2, "currentIteration": 0, "terminal": false},
		"languagePolicy": map[string]any{"kind": "language.policy", "schemaVersion": "1", "userFacing": "match-user", "technicalArtifacts": "english", "subagentInstructions": "english"},
	}
	if mutate != nil {
		mutate(packet)
	}
	data, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	contextPacket := packet["context"].(map[string]any)
	executionID := packet["executionId"].(string)
	taskID := contextPacket["taskId"].(string)
	return providers.Request{
		Mode: mode, Packet: data,
		Authorization: gatekeeper.Request{
			AgentID: "forge", CorrelationID: executionID,
			WorkUnit: gatekeeper.WorkUnit{ID: taskID},
		},
	}
}

func resultDocumentFor(t *testing.T, status string) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"kind": "agent.result", "schemaVersion": "1", "resultId": "result-1", "taskId": "work-1", "agentId": "forge",
		"status": status, "summary": "finished", "artifacts": []any{}, "nextRecommended": "inspect the result", "risks": []any{}, "errors": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertEventTypes(t *testing.T, events []chronicle.Event, expected ...string) {
	t.Helper()
	if len(events) != len(expected) {
		t.Fatalf("event count: got %d want %d: %+v", len(events), len(expected), events)
	}
	for index, eventType := range expected {
		if events[index].Type != eventType {
			t.Fatalf("event %d: got %s want %s", index, events[index].Type, eventType)
		}
	}
}

func assertFailureCategory(t *testing.T, event chronicle.Event, expected string) {
	t.Helper()
	var body struct {
		Failure struct {
			Category string `json:"category"`
		} `json:"failure"`
	}
	if err := json.Unmarshal(event.Raw, &body); err != nil || body.Failure.Category != expected {
		t.Fatalf("failure category: got %q want %q err=%v", body.Failure.Category, expected, err)
	}
}

func assertFailureNextSafeAction(t *testing.T, event chronicle.Event, expected string) {
	t.Helper()
	var body struct {
		Failure struct {
			NextSafeAction string `json:"nextSafeAction"`
		} `json:"failure"`
	}
	if err := json.Unmarshal(event.Raw, &body); err != nil || body.Failure.NextSafeAction != expected {
		t.Fatalf("next safe action: got %q want %q err=%v", body.Failure.NextSafeAction, expected, err)
	}
}
