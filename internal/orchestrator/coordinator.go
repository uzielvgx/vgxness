package orchestrator

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/chronicle"
	"github.com/vgxness/vgxness/internal/contracts"
	"github.com/vgxness/vgxness/internal/hooks"
	"github.com/vgxness/vgxness/internal/providers"
)

var (
	ErrInvalidCoordinator = errors.New("invalid coordinator")
	ErrCoordinatorBusy    = errors.New("coordinator capacity exhausted")
	ErrLoopTerminated     = errors.New("execution loop terminated")
	ErrDurability         = errors.New("coordination evidence is incomplete")
)

// Limits are hard local ceilings. Execution packets may request less capacity,
// but never more than these process-independent bounds.
type Limits struct {
	MaxIterations  int
	MaxBackground  int
	MaxDuration    time.Duration
	CleanupTimeout time.Duration
}

// ProviderRunner is implemented by providers.Runner. Keeping this boundary
// narrow makes the coordinator testable without creating another provider path.
type ProviderRunner interface {
	Run(context.Context, providers.Request) (providers.Receipt, error)
}

var _ ProviderRunner = (*providers.Runner)(nil)

type NativeProviderRunner interface {
	Prepare(context.Context, providers.Request) (providers.Prepared, error)
	Accept(context.Context, providers.Prepared, []byte) (providers.Receipt, error)
}

var _ NativeProviderRunner = (*providers.Runner)(nil)

// NativeTicket binds the durable coordinator identity to a provider
// invocation that OpenCode will execute as a native child session.
type NativeTicket struct {
	RunID    string
	TaskID   string
	Mode     chronicle.TaskMode
	Request  providers.Request
	Prepared providers.Prepared
}

// Receipt describes what Chronicle durably observed around one provider run.
// Provider may be present even when a later Chronicle append failed.
type Receipt struct {
	Status         chronicle.TaskStatus
	Events         []chronicle.Event
	Provider       *providers.Receipt
	CancellationID string
}

type Coordinator struct {
	log      *chronicle.EventLog
	runner   ProviderRunner
	limits   Limits
	runID    string
	lockRoot string
	now      func() time.Time
	newID    func() (string, error)
	dispatch *hooks.Dispatcher
}

type Option func(*Coordinator) error

// WithDispatcher adds best-effort post-Chronicle lifecycle notifications.
func WithDispatcher(dispatcher *hooks.Dispatcher) Option {
	return func(coordinator *Coordinator) error {
		coordinator.dispatch = dispatcher
		return nil
	}
}

type FileLock struct{ slot heldSlot }

func AcquireFileLock(path string) (FileLock, error) {
	slot, err := tryLock(path)
	return FileLock{slot: slot}, err
}
func (lock FileLock) Release() { lock.slot.release() }

// New creates a bounded coordinator for the run owned by log.
func New(log *chronicle.EventLog, runner ProviderRunner, limits Limits, options ...Option) (*Coordinator, error) {
	if log == nil || nilInterface(runner) || limits.MaxIterations < 1 || limits.MaxBackground < 0 || limits.MaxDuration <= 0 || limits.CleanupTimeout <= 0 {
		return nil, ErrInvalidCoordinator
	}
	base := filepath.Base(log.Path())
	extension := filepath.Ext(base)
	runID := base[:len(base)-len(extension)]
	if extension != ".jsonl" || runID == "" {
		return nil, ErrInvalidCoordinator
	}
	root := filepath.Dir(filepath.Dir(log.Path()))
	coordinator := &Coordinator{
		log: log, runner: runner, limits: limits, runID: runID,
		lockRoot: filepath.Join(root, "coordination-locks"), now: time.Now, newID: randomID,
	}
	for _, option := range options {
		if option == nil || option(coordinator) != nil {
			return nil, ErrInvalidCoordinator
		}
	}
	return coordinator, nil
}

// StartNative validates, authorizes, composes, and records a task start without
// running a provider process. The returned ticket is completed by a later
// bridge call after OpenCode executes a native child session.
func (c *Coordinator) StartNative(ctx context.Context, request providers.Request) (ticket NativeTicket, receipt Receipt, resultErr error) {
	if err := ctx.Err(); err != nil {
		return NativeTicket{}, Receipt{}, err
	}
	packet, err := decodePacket(ctx, request.Packet)
	if err != nil {
		return NativeTicket{}, Receipt{}, err
	}
	if packet.Context.RunID != c.runID || packet.Context.TaskID != request.Authorization.WorkUnit.ID || packet.ExecutionID != request.Authorization.CorrelationID {
		return NativeTicket{}, Receipt{}, fmt.Errorf("%w: execution identity mismatch", ErrInvalidCoordinator)
	}
	if request.Mode != chronicle.TaskForeground && request.Mode != chronicle.TaskBackground {
		return NativeTicket{}, Receipt{}, fmt.Errorf("%w: unknown task mode", ErrInvalidCoordinator)
	}
	if reason := c.loopTermination(packet); reason != "" {
		return NativeTicket{}, Receipt{Status: chronicle.TaskSkipped}, ErrLoopTerminated
	}
	runner, ok := c.runner.(NativeProviderRunner)
	if !ok {
		return NativeTicket{}, Receipt{}, ErrInvalidCoordinator
	}
	slot, err := c.acquireSlot(request.Mode)
	if err != nil {
		return NativeTicket{}, Receipt{}, err
	}
	defer func() {
		slot.release()
		c.dispatchTaskEvents(context.WithoutCancel(ctx), receipt.Events)
	}()
	receipt = Receipt{Status: chronicle.TaskPending}
	started, err := c.appendStarted(ctx, packet, request)
	if err != nil {
		return NativeTicket{}, receipt, err
	}
	receipt.Status = chronicle.TaskRunning
	receipt.Events = append(receipt.Events, started)
	prepared, err := runner.Prepare(ctx, request)
	if err != nil {
		cleanup, cancelCleanup := c.cleanupContext(ctx)
		defer cancelCleanup()
		failed, appendErr := c.appendFailed(cleanup, packet, request.Mode, failureEvidence(err))
		if appendErr != nil {
			return NativeTicket{}, receipt, errors.Join(err, fmt.Errorf("%w: %v", ErrDurability, appendErr))
		}
		receipt.Status = chronicle.TaskFailed
		receipt.Events = append(receipt.Events, failed)
		return NativeTicket{}, receipt, err
	}
	return NativeTicket{RunID: c.runID, TaskID: packet.Context.TaskID, Mode: request.Mode, Request: request, Prepared: prepared}, receipt, nil
}

// CompleteNative accepts one native child result and records the same terminal
// Chronicle evidence as the synchronous provider path.
func (c *Coordinator) CompleteNative(ctx context.Context, ticket NativeTicket, resultData []byte) (receipt Receipt, resultErr error) {
	packet, err := decodePacket(ctx, ticket.Request.Packet)
	if err != nil || ticket.RunID != c.runID || ticket.TaskID != packet.Context.TaskID || ticket.Mode != ticket.Request.Mode {
		return Receipt{}, fmt.Errorf("%w: native ticket identity mismatch", ErrInvalidCoordinator)
	}
	runner, ok := c.runner.(NativeProviderRunner)
	if !ok {
		return Receipt{}, ErrInvalidCoordinator
	}
	receipt = Receipt{Status: chronicle.TaskRunning}
	defer func() {
		c.dispatchTaskEvents(context.WithoutCancel(ctx), receipt.Events)
	}()
	providerReceipt, runErr := runner.Accept(ctx, ticket.Prepared, resultData)
	resultDigest := nativeEvidenceDigest(resultData)
	cleanup, cancelCleanup := c.cleanupContext(ctx)
	defer cancelCleanup()
	if runErr != nil {
		evidence := failureEvidence(runErr)
		evidence["digest"] = resultDigest
		failed, appendErr := c.recordNativeFailure(cleanup, packet, ticket.Mode, evidence)
		if appendErr != nil {
			return receipt, errors.Join(runErr, fmt.Errorf("%w: %v", ErrDurability, appendErr))
		}
		receipt.Status = chronicle.TaskFailed
		receipt.Events = append(receipt.Events, failed...)
		return receipt, runErr
	}
	receipt.Provider = &providerReceipt
	result, err := decodeResult(cleanup, providerReceipt.Result)
	if err != nil || result.TaskID != packet.Context.TaskID {
		if err == nil {
			err = fmt.Errorf("%w: result identity mismatch", providers.ErrInvalidResult)
		}
		evidence := failureEvidence(err)
		evidence["digest"] = resultDigest
		failed, appendErr := c.recordNativeFailure(cleanup, packet, ticket.Mode, evidence)
		if appendErr != nil {
			return receipt, errors.Join(err, fmt.Errorf("%w: %v", ErrDurability, appendErr))
		}
		receipt.Status = chronicle.TaskFailed
		receipt.Events = append(receipt.Events, failed...)
		return receipt, err
	}
	if replay, ok, replayErr := c.replayNativeResult(cleanup, result, resultDigest); replayErr != nil {
		return receipt, replayErr
	} else if ok {
		receipt.Status, receipt.Events = chronicle.TaskFailed, replay
		if result.Status == "success" {
			receipt.Status = chronicle.TaskCompleted
		}
		return receipt, nil
	}
	if result.Status == "success" {
		terminal, appendErr := c.appendCompleted(cleanup, packet, ticket.Mode, result.ResultID, resultDigest)
		if appendErr != nil {
			return receipt, fmt.Errorf("%w: %v", ErrDurability, appendErr)
		}
		receipt.Status = chronicle.TaskCompleted
		receipt.Events = append(receipt.Events, terminal)
	} else {
		terminal, appendErr := c.appendFailed(cleanup, packet, ticket.Mode, map[string]any{
			"category": "agent-" + result.Status, "resultId": result.ResultID, "digest": resultDigest, "nextSafeAction": resultSafeAction(result.Status),
		})
		if appendErr != nil {
			return receipt, fmt.Errorf("%w: %v", ErrDurability, appendErr)
		}
		receipt.Status = chronicle.TaskFailed
		receipt.Events = append(receipt.Events, terminal)
	}
	accepted, err := c.appendEvent(cleanup, map[string]any{"type": "result.accepted", "resultId": result.ResultID, "resultDigest": resultDigest, "taskId": packet.Context.TaskID})
	if err != nil {
		return receipt, fmt.Errorf("%w: %v", ErrDurability, err)
	}
	receipt.Events = append(receipt.Events, accepted)
	return receipt, nil
}

func (c *Coordinator) replayNativeResult(ctx context.Context, result resultDocument, resultDigest string) ([]chronicle.Event, bool, error) {
	events, err := c.log.Read(ctx)
	if err != nil {
		return nil, false, err
	}
	states, err := chronicle.DeriveTaskStates(events)
	if err != nil {
		return nil, false, err
	}
	state, exists := states[result.TaskID]
	if !exists || state.Status == chronicle.TaskRunning {
		return nil, false, nil
	}
	expected := map[bool]chronicle.TaskStatus{true: chronicle.TaskCompleted, false: chronicle.TaskFailed}[result.Status == "success"]
	if state.Status != expected || expected == chronicle.TaskCompleted && state.ResultID != result.ResultID {
		return nil, false, ErrDurability
	}
	replay, accepted, terminalMatched := make([]chronicle.Event, 0, 2), false, false
	for _, event := range events {
		var refs struct {
			TaskID       string `json:"taskId"`
			ResultID     string `json:"resultId"`
			ResultDigest string `json:"resultDigest"`
			Failure      struct {
				Category string `json:"category"`
				Digest   string `json:"digest"`
			} `json:"failure"`
		}
		_ = json.Unmarshal(event.Raw, &refs)
		completed := event.Type == "task.completed" || event.Type == "background.completed"
		failed := event.Type == "task.failed" || event.Type == "background.failed"
		terminalMatches := refs.TaskID == result.TaskID && (completed && result.Status == "success" && refs.ResultID == result.ResultID && refs.ResultDigest == resultDigest || failed && result.Status != "success" && refs.ResultID == result.ResultID && refs.Failure.Category == "agent-"+result.Status && refs.Failure.Digest == resultDigest)
		if terminalMatches {
			replay = append(replay, event)
			terminalMatched = true
		}
		if event.Type == "result.accepted" && refs.TaskID == result.TaskID && refs.ResultID == result.ResultID && refs.ResultDigest == resultDigest {
			replay, accepted = append(replay, event), true
		}
	}
	if !terminalMatched {
		return nil, false, ErrDurability
	}
	if !accepted {
		event, appendErr := c.appendEvent(ctx, map[string]any{"type": "result.accepted", "resultId": result.ResultID, "resultDigest": resultDigest, "taskId": result.TaskID})
		if appendErr != nil {
			return nil, false, appendErr
		}
		replay = append(replay, event)
	}
	return replay, true, nil
}

func (c *Coordinator) recordNativeFailure(ctx context.Context, packet packetDocument, mode chronicle.TaskMode, evidence map[string]any) ([]chronicle.Event, error) {
	category, _ := evidence["category"].(string)
	digest, _ := evidence["digest"].(string)
	if category == "" || digest == "" {
		return nil, ErrDurability
	}
	if replay, ok, err := c.replayNativeFailure(ctx, packet.Context.TaskID, category, digest); err != nil || ok {
		return replay, err
	}
	failed, err := c.appendFailed(ctx, packet, mode, evidence)
	if err != nil {
		return nil, err
	}
	return []chronicle.Event{failed}, nil
}

func (c *Coordinator) replayNativeFailure(ctx context.Context, taskID, category, digest string) ([]chronicle.Event, bool, error) {
	events, err := c.log.Read(ctx)
	if err != nil {
		return nil, false, err
	}
	states, err := chronicle.DeriveTaskStates(events)
	if err != nil {
		return nil, false, err
	}
	state, exists := states[taskID]
	if !exists || state.Status == chronicle.TaskRunning {
		return nil, false, nil
	}
	if state.Status != chronicle.TaskFailed {
		return nil, false, ErrDurability
	}
	replay := make([]chronicle.Event, 0, 1)
	for _, event := range events {
		var refs struct {
			TaskID  string `json:"taskId"`
			Failure struct {
				Category string `json:"category"`
				Digest   string `json:"digest"`
			} `json:"failure"`
		}
		_ = json.Unmarshal(event.Raw, &refs)
		if refs.TaskID == taskID && refs.Failure.Category == category && refs.Failure.Digest == digest && (event.Type == "task.failed" || event.Type == "background.failed") {
			replay = append(replay, event)
		}
	}
	if len(replay) == 0 {
		return nil, false, ErrDurability
	}
	return replay, true, nil
}

// FailNative records a host-side child-session failure for a started ticket.
func (c *Coordinator) FailNative(ctx context.Context, ticket NativeTicket, category string) (receipt Receipt, resultErr error) {
	packet, err := decodePacket(ctx, ticket.Request.Packet)
	if err != nil || ticket.RunID != c.runID || ticket.TaskID != packet.Context.TaskID || ticket.Mode != ticket.Request.Mode {
		return Receipt{}, fmt.Errorf("%w: native ticket identity mismatch", ErrInvalidCoordinator)
	}
	if category == "" {
		category = "native-subagent-failed"
	}
	digest := nativeEvidenceDigest(ticket.RunID, ticket.TaskID, ticket.Mode, category)
	cleanup, cancelCleanup := c.cleanupContext(ctx)
	defer func() {
		c.dispatchTaskEvents(context.WithoutCancel(ctx), receipt.Events)
	}()
	defer cancelCleanup()
	failed, err := c.recordNativeFailure(cleanup, packet, ticket.Mode, map[string]any{
		"category": category, "digest": digest, "nextSafeAction": "Retry the bounded task after checking the native OpenCode subagent.",
	})
	if err != nil {
		return Receipt{Status: chronicle.TaskRunning}, fmt.Errorf("%w: %v", ErrDurability, err)
	}
	return Receipt{Status: chronicle.TaskFailed, Events: failed}, nil
}

// Run executes one foreground or read-only background task. Foreground capacity
// is one slot; background capacity is Limits.MaxBackground slots. Slot files use
// platform file locks so separate VGXNESS processes cannot exceed the same run's bounds.
func (c *Coordinator) Run(ctx context.Context, request providers.Request) (receipt Receipt, resultErr error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	packet, err := decodePacket(ctx, request.Packet)
	if err != nil {
		return Receipt{}, err
	}
	if packet.Context.RunID != c.runID || packet.Context.TaskID != request.Authorization.WorkUnit.ID || packet.ExecutionID != request.Authorization.CorrelationID {
		return Receipt{}, fmt.Errorf("%w: execution identity mismatch", ErrInvalidCoordinator)
	}
	if request.Mode != chronicle.TaskForeground && request.Mode != chronicle.TaskBackground {
		return Receipt{}, fmt.Errorf("%w: unknown task mode", ErrInvalidCoordinator)
	}
	if reason := c.loopTermination(packet); reason != "" {
		receipt := Receipt{Status: chronicle.TaskSkipped}
		cleanup, cancelCleanup := c.cleanupContext(ctx)
		defer cancelCleanup()
		event, appendErr := c.appendLoopTerminated(cleanup, packet, reason)
		if appendErr != nil {
			return receipt, errors.Join(ErrLoopTerminated, fmt.Errorf("%w: %v", ErrDurability, appendErr))
		}
		receipt.Events = append(receipt.Events, event)
		return receipt, ErrLoopTerminated
	}

	slot, err := c.acquireSlot(request.Mode)
	if err != nil {
		return Receipt{}, err
	}
	defer func() {
		slot.release()
		c.dispatchTaskEvents(context.WithoutCancel(ctx), receipt.Events)
	}()

	receipt = Receipt{Status: chronicle.TaskRunning}
	started, err := c.appendStarted(ctx, packet, request)
	if err != nil {
		return receipt, err
	}
	receipt.Events = append(receipt.Events, started)

	runCtx, cancel := c.executionContext(ctx, packet)
	providerReceipt, runErr := c.runner.Run(runCtx, request)
	runContextErr := runCtx.Err()
	cancel()
	if runErr == nil {
		receipt.Provider = &providerReceipt
	}
	if contextError(runErr) || runContextErr != nil {
		cause := runErr
		if runContextErr != nil {
			cause = runContextErr
		}
		return c.recordCancellation(ctx, packet, request.Mode, receipt, cause)
	}
	cleanup, cancelCleanup := c.cleanupContext(ctx)
	defer cancelCleanup()
	if runErr != nil {
		failed, appendErr := c.appendFailed(cleanup, packet, request.Mode, failureEvidence(runErr))
		if appendErr != nil {
			return receipt, errors.Join(runErr, fmt.Errorf("%w: %v", ErrDurability, appendErr))
		}
		receipt.Status = chronicle.TaskFailed
		receipt.Events = append(receipt.Events, failed)
		return receipt, runErr
	}

	result, err := decodeResult(cleanup, providerReceipt.Result)
	resultDigest := nativeEvidenceDigest(providerReceipt.Result)
	if err != nil {
		failed, appendErr := c.appendFailed(cleanup, packet, request.Mode, failureEvidence(err))
		if appendErr != nil {
			return receipt, errors.Join(err, fmt.Errorf("%w: %v", ErrDurability, appendErr))
		}
		receipt.Status = chronicle.TaskFailed
		receipt.Events = append(receipt.Events, failed)
		return receipt, err
	}
	if result.TaskID != packet.Context.TaskID {
		err = fmt.Errorf("%w: result identity mismatch", providers.ErrInvalidResult)
		failed, appendErr := c.appendFailed(cleanup, packet, request.Mode, failureEvidence(err))
		if appendErr != nil {
			return receipt, errors.Join(err, fmt.Errorf("%w: %v", ErrDurability, appendErr))
		}
		receipt.Status = chronicle.TaskFailed
		receipt.Events = append(receipt.Events, failed)
		return receipt, err
	}

	if result.Status == "success" {
		terminal, appendErr := c.appendCompleted(cleanup, packet, request.Mode, result.ResultID, resultDigest)
		if appendErr != nil {
			return receipt, fmt.Errorf("%w: %v", ErrDurability, appendErr)
		}
		receipt.Status = chronicle.TaskCompleted
		receipt.Events = append(receipt.Events, terminal)
	} else {
		terminal, appendErr := c.appendFailed(cleanup, packet, request.Mode, map[string]any{
			"category": "agent-" + result.Status, "digest": resultDigest, "nextSafeAction": resultSafeAction(result.Status),
		})
		if appendErr != nil {
			return receipt, fmt.Errorf("%w: %v", ErrDurability, appendErr)
		}
		receipt.Status = chronicle.TaskFailed
		receipt.Events = append(receipt.Events, terminal)
	}
	accepted, err := c.appendEvent(cleanup, map[string]any{
		"type": "result.accepted", "resultId": result.ResultID, "resultDigest": resultDigest, "taskId": packet.Context.TaskID,
	})
	if err != nil {
		return receipt, fmt.Errorf("%w: %v", ErrDurability, err)
	}
	receipt.Events = append(receipt.Events, accepted)
	return receipt, nil
}

type packetDocument struct {
	ExecutionID string `json:"executionId"`
	Context     struct {
		RunID  string `json:"runId"`
		TaskID string `json:"taskId"`
		Phase  string `json:"phase"`
	} `json:"context"`
	Loop struct {
		LoopID           string `json:"loopId"`
		MaxIterations    int    `json:"maxIterations"`
		CurrentIteration int    `json:"currentIteration"`
		Deadline         string `json:"deadline"`
		Terminal         bool   `json:"terminal"`
	} `json:"loop"`
}

type resultDocument struct {
	ResultID string `json:"resultId"`
	TaskID   string `json:"taskId"`
	Status   string `json:"status"`
}

func decodePacket(ctx context.Context, document []byte) (packetDocument, error) {
	if err := contracts.Validate(ctx, contracts.ExecutionSchemaURI+"#/$defs/executionPacket", document, false); err != nil {
		return packetDocument{}, err
	}
	var packet packetDocument
	if err := json.Unmarshal(document, &packet); err != nil {
		return packetDocument{}, ErrInvalidCoordinator
	}
	return packet, nil
}

func decodeResult(ctx context.Context, document []byte) (resultDocument, error) {
	if err := contracts.Validate(ctx, contracts.ExecutionSchemaURI+"#/$defs/agentResult", document, false); err != nil {
		return resultDocument{}, fmt.Errorf("%w: provider result contract", providers.ErrInvalidResult)
	}
	var result resultDocument
	if err := json.Unmarshal(document, &result); err != nil {
		return resultDocument{}, providers.ErrInvalidResult
	}
	return result, nil
}

func (c *Coordinator) loopTermination(packet packetDocument) string {
	if packet.Loop.Terminal || packet.Loop.CurrentIteration >= packet.Loop.MaxIterations || packet.Loop.MaxIterations > c.limits.MaxIterations {
		return "budget_exhausted"
	}
	if packet.Loop.Deadline != "" {
		deadline, err := time.Parse(time.RFC3339Nano, packet.Loop.Deadline)
		if err != nil || !deadline.After(c.now()) {
			return "deadline_exceeded"
		}
	}
	return ""
}

func (c *Coordinator) executionContext(parent context.Context, packet packetDocument) (context.Context, context.CancelFunc) {
	deadline := c.now().Add(c.limits.MaxDuration)
	if packet.Loop.Deadline != "" {
		packetDeadline, err := time.Parse(time.RFC3339Nano, packet.Loop.Deadline)
		if err == nil && packetDeadline.Before(deadline) {
			deadline = packetDeadline
		}
	}
	return context.WithDeadline(parent, deadline)
}

func (c *Coordinator) appendStarted(ctx context.Context, packet packetDocument, request providers.Request) (chronicle.Event, error) {
	typeName := "task.started"
	if request.Mode == chronicle.TaskBackground {
		typeName = "background.started"
	}
	fields := map[string]any{
		"type": typeName, "taskId": packet.Context.TaskID, "agent": request.Authorization.AgentID,
		"phase": packet.Context.Phase, "loopId": packet.Loop.LoopID,
	}
	return c.appendEvent(ctx, fields)
}

func (c *Coordinator) appendCompleted(ctx context.Context, packet packetDocument, mode chronicle.TaskMode, resultID, resultDigest string) (chronicle.Event, error) {
	typeName := "task.completed"
	if mode == chronicle.TaskBackground {
		typeName = "background.completed"
	}
	return c.appendEvent(ctx, map[string]any{"type": typeName, "taskId": packet.Context.TaskID, "resultId": resultID, "resultDigest": resultDigest})
}

func (c *Coordinator) appendFailed(ctx context.Context, packet packetDocument, mode chronicle.TaskMode, failure map[string]any) (chronicle.Event, error) {
	typeName := "task.failed"
	if mode == chronicle.TaskBackground {
		typeName = "background.failed"
	}
	fields := map[string]any{"type": typeName, "taskId": packet.Context.TaskID}
	cleanFailure := make(map[string]any, len(failure))
	for key, value := range failure {
		if key == "resultId" {
			fields["resultId"] = value
			continue
		}
		cleanFailure[key] = value
	}
	fields["failure"] = cleanFailure
	return c.appendEvent(ctx, fields)
}

func (c *Coordinator) dispatchTaskEvents(ctx context.Context, events []chronicle.Event) {
	for _, event := range events {
		var evidence struct {
			TaskID       string `json:"taskId"`
			ResultID     string `json:"resultId"`
			ResultDigest string `json:"resultDigest"`
			Failure      struct {
				Digest   string `json:"digest"`
				ExitCode *int   `json:"exitCode"`
			} `json:"failure"`
		}
		if json.Unmarshal(event.Raw, &evidence) != nil {
			continue
		}
		meta := hooks.Metadata{ID: event.ID, At: hookTime(event.At)}
		mode := chronicle.TaskForeground
		if strings.HasPrefix(event.Type, "background.") {
			mode = chronicle.TaskBackground
		}
		switch event.Type {
		case "task.started", "background.started":
			c.dispatch.Dispatch(ctx, hooks.TaskStarted{Meta: meta, RunID: event.RunID, TaskID: evidence.TaskID, Mode: hookMode(mode)})
		case "task.completed", "background.completed":
			c.dispatch.Dispatch(ctx, hooks.TaskSucceeded{
				Meta: meta, RunID: event.RunID, TaskID: evidence.TaskID, Mode: hookMode(mode),
				ResultID: evidence.ResultID, ResultDigest: hookDigest(evidence.ResultDigest),
			})
		case "task.failed", "background.failed":
			exitCode := -1
			if evidence.Failure.ExitCode != nil {
				exitCode = *evidence.Failure.ExitCode
			}
			c.dispatch.Dispatch(ctx, hooks.TaskFailed{
				Meta: meta, RunID: event.RunID, TaskID: evidence.TaskID, Mode: hookMode(mode), ResultID: evidence.ResultID,
				FailureDigest: hookDigest(evidence.Failure.Digest), ExitCode: exitCode,
			})
		}
	}
}

func hookMode(mode chronicle.TaskMode) hooks.Mode {
	if mode == chronicle.TaskBackground {
		return hooks.ModeBackground
	}
	return hooks.ModeForeground
}

func hookTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed.UTC()
}

func hookDigest(value string) string {
	value = strings.TrimPrefix(value, "sha256:")
	return strings.TrimPrefix(value, "sha256-")
}

func (c *Coordinator) appendLoopTerminated(ctx context.Context, packet packetDocument, reason string) (chronicle.Event, error) {
	return c.appendEvent(ctx, map[string]any{
		"type": "loop.terminated", "loopId": packet.Loop.LoopID,
		"data": map[string]any{"terminalReason": reason, "currentIteration": packet.Loop.CurrentIteration, "maxIterations": packet.Loop.MaxIterations},
	})
}

func (c *Coordinator) recordCancellation(parent context.Context, packet packetDocument, mode chronicle.TaskMode, receipt Receipt, cause error) (Receipt, error) {
	cancellationID, err := c.newID()
	if err != nil {
		return receipt, errors.Join(cause, fmt.Errorf("%w: cancellation identity", ErrDurability))
	}
	receipt.CancellationID = cancellationID
	cleanup, cancelCleanup := c.cleanupContext(parent)
	defer cancelCleanup()
	targetKind := "task"
	if mode == chronicle.TaskBackground {
		targetKind = "background-task"
	}
	requested, err := c.appendEvent(cleanup, map[string]any{
		"type": "cancellation.requested", "cancellationId": cancellationID,
		"taskId": packet.Context.TaskID, "data": map[string]any{"targetKind": targetKind, "targetId": packet.Context.TaskID, "reason": cancellationReason(cause)},
	})
	if err != nil {
		return receipt, errors.Join(cause, fmt.Errorf("%w: %v", ErrDurability, err))
	}
	receipt.Events = append(receipt.Events, requested)
	completed, err := c.appendEvent(cleanup, map[string]any{
		"type": "cancellation.completed", "cancellationId": cancellationID,
		"taskId": packet.Context.TaskID, "data": map[string]any{"targetKind": targetKind, "targetId": packet.Context.TaskID},
	})
	if err != nil {
		return receipt, errors.Join(cause, fmt.Errorf("%w: %v", ErrDurability, err))
	}
	receipt.Events = append(receipt.Events, completed)
	receipt.Status = chronicle.TaskCancelled
	reason := "cancelled"
	if errors.Is(cause, context.DeadlineExceeded) {
		reason = "deadline_exceeded"
	}
	terminated, err := c.appendLoopTerminated(cleanup, packet, reason)
	if err != nil {
		return receipt, errors.Join(cause, fmt.Errorf("%w: %v", ErrDurability, err))
	}
	receipt.Events = append(receipt.Events, terminated)
	return receipt, cause
}

func (c *Coordinator) appendEvent(ctx context.Context, fields map[string]any) (chronicle.Event, error) {
	id, err := c.newID()
	if err != nil {
		return chronicle.Event{}, err
	}
	document := map[string]any{
		"schemaVersion": "1", "eventId": id, "runId": c.runID,
		"at": c.now().UTC().Format(time.RFC3339Nano),
	}
	for key, value := range fields {
		document[key] = value
	}
	data, err := json.Marshal(document)
	if err != nil {
		return chronicle.Event{}, err
	}
	return c.log.Append(ctx, data)
}

func failureEvidence(err error) map[string]any {
	category := "provider-unavailable"
	next := "retry through the bounded coordinator after provider health recovers"
	var failure *providers.Failure
	var decision *providers.DecisionError
	switch {
	case errors.As(err, &decision) && errors.Is(err, providers.ErrApprovalRequired):
		category, next = "approval-required", "obtain the required approval before retrying"
	case errors.As(err, &decision):
		category, next = "policy-denied", "adjust the work unit to satisfy the active policy"
	case errors.As(err, &failure):
		category = "provider-" + string(failure.Category)
		if !failure.Recoverable {
			next = "select another compatible provider or revise the work unit"
		}
	case errors.Is(err, providers.ErrInvalidPrompt):
		category, next = "invalid-prompt-composition", "repair the exact prompt registry entry before retrying"
	case errors.Is(err, providers.ErrInvalidPacket):
		category, next = "invalid-execution-packet", "rebuild the packet from validated orchestration evidence"
	case errors.Is(err, providers.ErrInvalidResult):
		category, next = "invalid-provider-result", "quarantine the result and inspect the provider adapter"
	}
	return map[string]any{"category": category, "nextSafeAction": next}
}

func nativeEvidenceDigest(parts ...any) string {
	hash := sha256.New()
	for _, part := range parts {
		data, _ := json.Marshal(part)
		_, _ = hash.Write(data)
		_, _ = hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func resultSafeAction(status string) string {
	switch status {
	case "blocked":
		return "resolve the reported blocker before scheduling another bounded attempt"
	case "needs_followup":
		return "schedule a new bounded work unit for the required follow-up"
	case "unsupported":
		return "select a provider and agent that support the required capability"
	default:
		return "inspect the structured result before scheduling another bounded attempt"
	}
}

func cancellationReason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline exceeded"
	}
	return "cancellation requested"
}

func contextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func (c *Coordinator) cleanupContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), c.limits.CleanupTimeout)
}

func (c *Coordinator) acquireSlot(mode chronicle.TaskMode) (heldSlot, error) {
	if err := ensurePrivateDirectory(c.lockRoot); err != nil {
		return heldSlot{}, err
	}
	if mode == chronicle.TaskForeground {
		return tryLock(filepath.Join(c.lockRoot, c.runID+".foreground.lock"))
	}
	for index := 0; index < c.limits.MaxBackground; index++ {
		slot, err := tryLock(filepath.Join(c.lockRoot, fmt.Sprintf("%s.background.%d.lock", c.runID, index)))
		if err == nil {
			return slot, nil
		}
		if !errors.Is(err, ErrCoordinatorBusy) {
			return heldSlot{}, err
		}
	}
	return heldSlot{}, ErrCoordinatorBusy
}

func randomID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "coord-" + hex.EncodeToString(value[:]), nil
}
