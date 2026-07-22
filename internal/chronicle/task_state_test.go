package chronicle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskTransitionTable(t *testing.T) {
	tests := []struct {
		from, to TaskStatus
		allowed  bool
	}{
		{TaskPending, TaskRunning, true},
		{TaskPending, TaskSkipped, true},
		{TaskRunning, TaskCompleted, true},
		{TaskRunning, TaskFailed, true},
		{TaskRunning, TaskCancelled, true},
		{TaskBlocked, TaskRunning, true},
		{TaskCompleted, TaskRunning, false},
		{TaskFailed, TaskRunning, false},
		{TaskPending, TaskCompleted, false},
		{TaskRunning, TaskRunning, false},
	}
	for _, test := range tests {
		err := ValidateTaskTransition(test.from, test.to)
		if test.allowed && err != nil {
			t.Fatalf("%s -> %s should be legal: %v", test.from, test.to, err)
		}
		if !test.allowed && !errors.Is(err, ErrIllegalTaskTransition) {
			t.Fatalf("%s -> %s should be illegal: %v", test.from, test.to, err)
		}
	}
}

func TestTaskTransitionErrorDoesNotEchoRejectedState(t *testing.T) {
	err := ValidateTaskTransition(TaskStatus(strings.Repeat("sensitive", 100)), TaskCompleted)
	if !errors.Is(err, ErrIllegalTaskTransition) {
		t.Fatalf("expected illegal transition, got %v", err)
	}
	if strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("transition error disclosed rejected state: %v", err)
	}
}

func TestEventLogDerivesLegalTaskLifecycle(t *testing.T) {
	root := t.TempDir()
	log, err := NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	appendLifecycleEvent(t, log, "event-1", "task.started", `,"taskId":"task-1","phase":"apply","agent":"forge"`)
	appendLifecycleEvent(t, log, "event-2", "task.completed", `,"taskId":"task-1","resultId":"result-1"`)

	events, err := log.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	states, err := DeriveTaskStates(events)
	if err != nil {
		t.Fatal(err)
	}
	if state := states["task-1"]; state.Status != TaskCompleted || state.Mode != TaskForeground || state.ResultID != "result-1" {
		t.Fatalf("unexpected task state: %+v", state)
	}
}

func TestEventLogRejectsIllegalTaskOrderWithoutMutation(t *testing.T) {
	root := t.TempDir()
	log, err := NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	document := lifecycleEvent("event-1", "task.completed", `,"taskId":"task-1","resultId":"result-1"`)

	if _, err := log.Append(context.Background(), document); !errors.Is(err, ErrConflict) || !errors.Is(err, ErrIllegalTaskTransition) {
		t.Fatalf("expected illegal transition conflict, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "logs")); !os.IsNotExist(err) {
		t.Fatalf("illegal first event mutated storage: %v", err)
	}
}

func TestEventLogRejectsRestartAndModeMixing(t *testing.T) {
	root := t.TempDir()
	log, err := NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	appendLifecycleEvent(t, log, "event-1", "task.started", `,"taskId":"task-1","phase":"apply","agent":"forge"`)
	before, err := os.ReadFile(log.Path())
	if err != nil {
		t.Fatal(err)
	}
	for index, document := range [][]byte{
		lifecycleEvent("event-2", "task.started", `,"taskId":"task-1","phase":"apply","agent":"forge"`),
		lifecycleEvent("event-3", "background.completed", `,"taskId":"task-1","resultId":"result-1"`),
	} {
		if _, err := log.Append(context.Background(), document); !errors.Is(err, ErrIllegalTaskTransition) {
			t.Fatalf("case %d: expected illegal transition, got %v", index, err)
		}
	}
	after, err := os.ReadFile(log.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("rejected lifecycle event changed the log")
	}
}

func TestEventLogRequiresCancellationRequest(t *testing.T) {
	root := t.TempDir()
	log, err := NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	completed := lifecycleEvent("event-1", "cancellation.completed", `,"cancellationId":"cancel-1"`)
	if _, err := log.Append(context.Background(), completed); !errors.Is(err, ErrIllegalTaskTransition) {
		t.Fatalf("expected completion-before-request rejection, got %v", err)
	}
	appendLifecycleEvent(t, log, "event-2", "cancellation.requested", `,"cancellationId":"cancel-1"`)
	appendLifecycleEvent(t, log, "event-3", "cancellation.completed", `,"cancellationId":"cancel-1"`)
}

func TestEventLogReadRejectsIllegalLifecycleHistory(t *testing.T) {
	root := t.TempDir()
	log, err := NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	data := append(lifecycleEvent("event-1", "task.completed", `,"taskId":"task-1","resultId":"result-1"`), '\n')
	if err := os.WriteFile(log.Path(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Read(context.Background()); !errors.Is(err, ErrCorrupt) || !errors.Is(err, ErrIllegalTaskTransition) {
		t.Fatalf("expected corrupt lifecycle history, got %v", err)
	}
}

func appendLifecycleEvent(t *testing.T, log *EventLog, eventID, eventType, fields string) {
	t.Helper()
	if _, err := log.Append(context.Background(), lifecycleEvent(eventID, eventType, fields)); err != nil {
		t.Fatal(err)
	}
}

func lifecycleEvent(eventID, eventType, fields string) []byte {
	return []byte(fmt.Sprintf(`{"schemaVersion":"1","eventId":%q,"runId":"run-1","at":"2026-07-21T12:00:00Z","type":%q%s}`, eventID, eventType, fields))
}
