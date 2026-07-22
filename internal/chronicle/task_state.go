package chronicle

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrIllegalTaskTransition means an event would move a task or cancellation
// through an order that Chronicle cannot reproduce safely.
var ErrIllegalTaskTransition = errors.New("illegal task transition")

// TaskStatus is the provider-neutral lifecycle recorded in run snapshots.
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskBlocked   TaskStatus = "blocked"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskSkipped   TaskStatus = "skipped"
	TaskCancelled TaskStatus = "cancelled"
)

// TaskMode distinguishes manager-advancing work from advisory background work.
type TaskMode string

const (
	TaskForeground TaskMode = "foreground"
	TaskBackground TaskMode = "background"
)

// TaskEventState is the lifecycle state reproducible from Chronicle events.
// Pending, blocked, skipped, and cancelled may also be represented by snapshot
// evidence because the v1 event contract has no dedicated event for them.
type TaskEventState struct {
	Status   TaskStatus
	Mode     TaskMode
	ResultID string
}

// CanTransitionTask reports whether the snapshot lifecycle permits a move.
func CanTransitionTask(from, to TaskStatus) bool {
	switch from {
	case TaskPending:
		return to == TaskRunning || to == TaskBlocked || to == TaskSkipped || to == TaskCancelled
	case TaskRunning:
		return to == TaskBlocked || to == TaskCompleted || to == TaskFailed || to == TaskCancelled
	case TaskBlocked:
		return to == TaskRunning || to == TaskFailed || to == TaskSkipped || to == TaskCancelled
	default:
		return false
	}
}

// ValidateTaskTransition rejects unknown, repeated, or terminal transitions.
func ValidateTaskTransition(from, to TaskStatus) error {
	if !validTaskStatus(from) || !validTaskStatus(to) || !CanTransitionTask(from, to) {
		return fmt.Errorf("%w: task status change is not permitted", ErrIllegalTaskTransition)
	}
	return nil
}

// DeriveTaskStates replays task lifecycle events without mutating Chronicle.
func DeriveTaskStates(events []Event) (map[string]TaskEventState, error) {
	projection, err := deriveLifecycle(events)
	if err != nil {
		return nil, err
	}
	states := make(map[string]TaskEventState, len(projection.tasks))
	for id, state := range projection.tasks {
		states[id] = state
	}
	return states, nil
}

type cancellationEventState string

const (
	cancellationRequested cancellationEventState = "requested"
	cancellationCompleted cancellationEventState = "completed"
)

type lifecycleProjection struct {
	tasks         map[string]TaskEventState
	cancellations map[string]cancellationEventState
}

func deriveLifecycle(events []Event) (lifecycleProjection, error) {
	projection := lifecycleProjection{
		tasks:         make(map[string]TaskEventState),
		cancellations: make(map[string]cancellationEventState),
	}
	for _, event := range events {
		if err := projection.apply(event); err != nil {
			return lifecycleProjection{}, err
		}
	}
	return projection, nil
}

func (p lifecycleProjection) apply(event Event) error {
	var refs struct {
		TaskID         string `json:"taskId"`
		CancellationID string `json:"cancellationId"`
		ResultID       string `json:"resultId"`
	}
	switch event.Type {
	case "task.started", "task.completed", "task.failed",
		"background.started", "background.completed", "background.failed",
		"cancellation.requested", "cancellation.completed":
		if err := json.Unmarshal(event.Raw, &refs); err != nil {
			return fmt.Errorf("%w: malformed lifecycle event", ErrIllegalTaskTransition)
		}
	default:
		return nil
	}

	switch event.Type {
	case "cancellation.requested":
		if _, exists := p.cancellations[refs.CancellationID]; exists {
			return fmt.Errorf("%w: cancellation was already requested", ErrIllegalTaskTransition)
		}
		p.cancellations[refs.CancellationID] = cancellationRequested
		return nil
	case "cancellation.completed":
		if p.cancellations[refs.CancellationID] != cancellationRequested {
			return fmt.Errorf("%w: cancellation completed before its request", ErrIllegalTaskTransition)
		}
		p.cancellations[refs.CancellationID] = cancellationCompleted
		return nil
	}

	mode := TaskForeground
	if len(event.Type) >= len("background") && event.Type[:len("background")] == "background" {
		mode = TaskBackground
	}
	state, exists := p.tasks[refs.TaskID]
	if event.Type == "task.started" || event.Type == "background.started" {
		if exists {
			return fmt.Errorf("%w: task started more than once", ErrIllegalTaskTransition)
		}
		p.tasks[refs.TaskID] = TaskEventState{Status: TaskRunning, Mode: mode}
		return nil
	}
	if !exists || state.Status != TaskRunning {
		return fmt.Errorf("%w: task reached a terminal event before starting", ErrIllegalTaskTransition)
	}
	if state.Mode != mode {
		return fmt.Errorf("%w: task mixed foreground and background events", ErrIllegalTaskTransition)
	}
	if event.Type == "task.completed" || event.Type == "background.completed" {
		state.Status = TaskCompleted
		state.ResultID = refs.ResultID
	} else {
		state.Status = TaskFailed
	}
	p.tasks[refs.TaskID] = state
	return nil
}

func validTaskStatus(status TaskStatus) bool {
	switch status {
	case TaskPending, TaskRunning, TaskBlocked, TaskCompleted, TaskFailed, TaskSkipped, TaskCancelled:
		return true
	default:
		return false
	}
}
