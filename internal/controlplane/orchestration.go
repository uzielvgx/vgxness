package controlplane

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/navigator"
	"github.com/vgxness/vgxness/internal/orchestrator"
)

const (
	orchestrationDocumentVersion = 1
	orchestrationDocumentLimit   = 16 << 20
	orchestrationDispatchTimeout = 45 * time.Second
	dependencyEvidenceTextLimit  = 8 << 10
	dependencyEvidenceJSONLimit  = 16 << 10
	orchestrationTaskGoalLimit   = 24 << 10
)

type orchestrationDocument struct {
	Version          int                                  `json:"version"`
	Workspace        string                               `json:"workspace"`
	OrchestrationID  string                               `json:"orchestrationId"`
	ScheduleID       string                               `json:"scheduleId"`
	OwnerID          string                               `json:"ownerId"`
	Model            string                               `json:"model"`
	ParentSessionID  string                               `json:"parentSessionId"`
	ParentMessageID  string                               `json:"parentMessageId"`
	Plan             navigator.Plan                       `json:"plan"`
	Status           string                               `json:"status"`
	CurrentWave      int                                  `json:"currentWave"`
	CreatedAt        string                               `json:"createdAt"`
	UpdatedAt        string                               `json:"updatedAt"`
	Join             json.RawMessage                      `json:"join,omitempty"`
	PreparedBindings map[string]string                    `json:"preparedBindings"`
	ClaimTokens      map[string]string                    `json:"claimTokens,omitempty"`
	Results          map[string]json.RawMessage           `json:"results"`
	EditArtifacts    map[string]bridge.NativeEditArtifact `json:"editArtifacts,omitempty"`
}

func (service *Service) PlanOrchestration(ctx context.Context, workspace string, request bridge.OrchestratePlanRequest) (bridge.Response, error) {
	if bridge.ValidateOrchestratePlan(request) != nil {
		return bridge.Response{}, bridge.ErrInvalid
	}
	root, err := canonicalWorkspace(ctx, workspace)
	if err != nil {
		return bridge.Response{}, err
	}
	paths, err := config.Prepare(ctx, config.Options{StorageRoot: service.storageRoot, ProjectDir: root})
	if err != nil {
		return bridge.Response{}, fmt.Errorf("%w: orchestration storage", bridge.ErrExecution)
	}
	plan, err := navigator.PlanRequest(ctx, navigator.Request{
		Kind: navigator.RequestKind, SchemaVersion: navigator.SchemaVersion,
		Goal: request.Input.Goal, AcceptanceCriteria: append([]string(nil), request.Input.AcceptanceCriteria...),
		CandidateTasks: append([]navigator.Task(nil), request.CandidateTasks...), PolicyVersion: "bridge-balanced-v1",
		MaxParallel: navigator.DefaultMaxParallel,
	})
	if err != nil {
		return bridge.Response{}, fmt.Errorf("%w: navigator proposal: %v", bridge.ErrDenied, err)
	}
	orchestrationID, err := service.newID("orchestration")
	if err != nil {
		return bridge.Response{}, fmt.Errorf("%w: orchestration identity", bridge.ErrExecution)
	}
	scheduleID, err := service.newID("schedule")
	if err != nil {
		return bridge.Response{}, fmt.Errorf("%w: schedule identity", bridge.ErrExecution)
	}
	ownerID, err := service.newID("owner")
	if err != nil {
		return bridge.Response{}, fmt.Errorf("%w: owner identity", bridge.ErrExecution)
	}
	now := service.now().UTC().Format(time.RFC3339Nano)
	document := orchestrationDocument{
		Version: orchestrationDocumentVersion, Workspace: root, OrchestrationID: orchestrationID,
		ScheduleID: scheduleID, OwnerID: ownerID, Model: request.Model,
		ParentSessionID: request.ParentSessionID, ParentMessageID: request.ParentMessageID,
		Plan: plan, Status: string(orchestrator.SchedulePending), CreatedAt: now, UpdatedAt: now,
		PreparedBindings: make(map[string]string),
		ClaimTokens:      make(map[string]string),
		Results:          make(map[string]json.RawMessage),
		EditArtifacts:    make(map[string]bridge.NativeEditArtifact),
	}
	if data, marshalErr := json.Marshal(document); marshalErr != nil || len(data) > orchestrationDocumentLimit {
		return bridge.Response{}, fmt.Errorf("%w: approved orchestration exceeds its durable bound", bridge.ErrDenied)
	}
	authority, err := orchestrator.NewDurableTicketAuthority(paths.Root, orchestrationDispatchTimeout)
	if err != nil || authority.RegisterPlan(ctx, scheduleID, plan, nil) != nil {
		return bridge.Response{}, fmt.Errorf("%w: initialize delegation authority", bridge.ErrExecution)
	}
	if err := createOrchestrationDocument(paths.Root, document); err != nil {
		return bridge.Response{}, fmt.Errorf("%w: persist approved orchestration", bridge.ErrExecution)
	}
	return orchestrationResponse(root, document, nil), nil
}

func (service *Service) PrepareOrchestrationWave(ctx context.Context, workspace string, request bridge.OrchestrateWaveRequest) (bridge.Response, error) {
	if bridge.ValidateOrchestrateWave(request) != nil {
		return bridge.Response{}, bridge.ErrInvalid
	}
	root, paths, document, release, err := service.openOrchestration(ctx, workspace, request.OrchestrationID)
	if err != nil {
		return bridge.Response{}, err
	}
	defer release()
	if document.OwnerID != request.OwnerID || document.Status == "cancelled" || len(document.Join) != 0 {
		return bridge.Response{}, bridge.ErrDenied
	}
	authority, scheduler, err := openDurableScheduler(ctx, paths.Root, document)
	if err != nil {
		return bridge.Response{}, err
	}
	checkpoint, err := authority.Snapshot(ctx, document.ScheduleID, document.Plan.PlanID, document.ParentSessionID)
	if err != nil {
		return bridge.Response{}, normalizeOrchestrationError(err)
	}
	checkpointByTask := make(map[string]orchestrator.NativeTaskCheckpoint, len(checkpoint.Tasks))
	for _, item := range checkpoint.Tasks {
		checkpointByTask[item.TaskID] = item
	}
	replayed := make([]bridge.OrchestrationPreparedTask, 0, len(request.Bindings))
	replayEligible := true
	for _, binding := range request.Bindings {
		item, ok := checkpointByTask[binding.TaskID]
		storedClaimToken := document.ClaimTokens[binding.TaskID]
		if !ok || item.TicketID != binding.TicketID || item.ChildSessionID != binding.ChildSessionID || storedClaimToken != "" && storedClaimToken != orchestrationClaimTokenDigest(binding.ClaimToken) {
			replayEligible = false
			break
		}
		if item.DispatchStatus != orchestrator.NativeDispatchConfirmed || item.Status != orchestrator.TaskRunning {
			continue
		}
		native, readErr := readNativeTicket(paths.Root, binding.TicketID)
		prepared, preparedOK := orchestrationPreparedReplay(native)
		if readErr != nil || !preparedOK {
			replayEligible = false
			break
		}
		replayed = append(replayed, bridge.OrchestrationPreparedTask{TaskID: binding.TaskID, ChildSessionID: binding.ChildSessionID, Prepared: prepared})
	}
	if replayEligible && len(replayed) > 0 && scheduler.Status() == orchestrator.ScheduleRunning {
		sort.Slice(replayed, func(i, j int) bool { return replayed[i].TaskID < replayed[j].TaskID })
		document.Status = string(scheduler.Status())
		for _, binding := range request.Bindings {
			document.ClaimTokens[binding.TaskID] = orchestrationClaimTokenDigest(binding.ClaimToken)
		}
		document.UpdatedAt = service.now().UTC().Format(time.RFC3339Nano)
		if err := writeOrchestrationDocument(paths.Root, document); err != nil {
			return bridge.Response{}, fmt.Errorf("%w: repair prepared orchestration replay", bridge.ErrExecution)
		}
		return orchestrationResponse(root, document, replayed), nil
	}
	wave, ok := scheduler.NextWave()
	if !ok || len(wave.TaskIDs) != len(request.Bindings) {
		return bridge.Response{}, bridge.ErrDenied
	}
	bindingsByTask := make(map[string]bridge.OrchestrationBinding, len(request.Bindings))
	for _, binding := range request.Bindings {
		bindingsByTask[binding.TaskID] = binding
	}
	nativeBindings := make([]orchestrator.NativeTaskBinding, 0, len(wave.TaskIDs))
	for _, taskID := range wave.TaskIDs {
		binding, exists := bindingsByTask[taskID]
		if !exists {
			return bridge.Response{}, bridge.ErrDenied
		}
		nativeBindings = append(nativeBindings, orchestrator.NativeTaskBinding{
			TaskID: taskID, ParentSessionID: document.ParentSessionID,
			ChildSessionID: binding.ChildSessionID, TicketID: binding.TicketID,
		})
	}
	if err := authority.RegisterPlan(ctx, document.ScheduleID, document.Plan, nativeBindings); err != nil {
		return bridge.Response{}, fmt.Errorf("%w: register native wave", bridge.ErrDenied)
	}
	prepared := make(map[string]bridge.PreparedDispatch, len(nativeBindings))
	tasks := tasksByID(document.Plan)
	dispatch := func(dispatchCtx context.Context, binding orchestrator.NativeTaskBinding) orchestrator.NativeDispatchResult {
		task, exists := tasks[binding.TaskID]
		if !exists {
			return orchestrator.NativeDispatchResult{Status: orchestrator.NativeDispatchNotStarted, Failure: "planned task identity is unavailable"}
		}
		goal, goalErr := orchestrationTaskGoal(task, document.Results)
		if goalErr != nil {
			return orchestrator.NativeDispatchResult{Status: orchestrator.NativeDispatchNotStarted, Failure: "validated dependency evidence exceeds the bounded task context"}
		}
		operation := bridge.Operation(task.Operation)
		response, prepareErr := service.Prepare(dispatchCtx, root, bridge.DispatchRequest{
			ProtocolVersion: bridge.ProtocolVersion, TicketID: binding.TicketID, Model: document.Model,
			Operation: operation, Goal: goal, AcceptanceCriteria: append([]string(nil), task.AcceptanceCriteria...),
			ParentSessionID: document.ParentSessionID, ParentMessageID: document.ParentMessageID, ChildSessionID: binding.ChildSessionID,
		})
		if prepareErr != nil || !response.OK || response.Prepared == nil {
			_, _ = service.Fail(context.WithoutCancel(dispatchCtx), root, bridge.NativeFailureRequest{
				ProtocolVersion: bridge.ProtocolVersion, TicketID: binding.TicketID,
				ParentSessionID: document.ParentSessionID, ChildSessionID: binding.ChildSessionID,
				Category: "native-subagent-failed",
			})
			return orchestrator.NativeDispatchResult{
				Status:  orchestrator.NativeDispatchNotStarted,
				Failure: nativeOrchestrationPreparationFailure(prepareErr, response),
			}
		}
		prepared[binding.TaskID] = *response.Prepared
		return orchestrator.NativeDispatchResult{Status: orchestrator.NativeDispatchConfirmed}
	}
	startErr := scheduler.StartWave(ctx, wave.WaveID, nativeBindings, dispatch)
	if startErr != nil && !errors.Is(startErr, orchestrator.ErrNativeDispatch) {
		return bridge.Response{}, normalizeOrchestrationError(startErr)
	}
	document.Status = string(scheduler.Status())
	document.CurrentWave = wave.Index
	document.UpdatedAt = service.now().UTC().Format(time.RFC3339Nano)
	for _, binding := range request.Bindings {
		document.ClaimTokens[binding.TaskID] = orchestrationClaimTokenDigest(binding.ClaimToken)
	}
	for taskID, item := range prepared {
		document.PreparedBindings[taskID] = item.TicketID
	}
	if err := writeOrchestrationDocument(paths.Root, document); err != nil {
		return bridge.Response{}, fmt.Errorf("%w: persist prepared orchestration wave", bridge.ErrExecution)
	}
	items := make([]bridge.OrchestrationPreparedTask, 0, len(prepared))
	for taskID, item := range prepared {
		items = append(items, bridge.OrchestrationPreparedTask{TaskID: taskID, ChildSessionID: bindingsByTask[taskID].ChildSessionID, Prepared: item})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].TaskID < items[j].TaskID })
	return orchestrationResponse(root, document, items), nil
}

func nativeOrchestrationPreparationFailure(err error, response bridge.Response) string {
	if err == nil {
		if !response.OK || response.Prepared == nil {
			return "native ticket preparation returned an invalid response"
		}
		return "native ticket preparation failed"
	}
	var staged *nativePreparationError
	if errors.As(err, &staged) && staged.stage == nativePreparationStageEditWorkspace {
		if errors.Is(err, errNativeSourceWorktreeDirty) {
			return "native ticket preparation denied: source worktree is not clean"
		}
		if errors.Is(err, bridge.ErrDenied) {
			return "native ticket preparation denied during isolated edit workspace setup"
		}
		if errors.Is(err, bridge.ErrUnavailable) {
			return "native ticket preparation unavailable during isolated edit workspace setup"
		}
		return "native ticket preparation failed during isolated edit workspace setup"
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "native ticket preparation was interrupted"
	case errors.Is(err, bridge.ErrInvalid):
		return "native ticket preparation rejected an invalid request"
	case errors.Is(err, bridge.ErrDenied):
		return "native ticket preparation was denied by policy"
	case errors.Is(err, bridge.ErrUnavailable):
		return "native ticket preparation is unavailable"
	default:
		return "native ticket preparation failed"
	}
}

func (service *Service) RecordOrchestrationTerminal(ctx context.Context, workspace string, request bridge.OrchestrateTerminalRequest) (bridge.Response, error) {
	if bridge.ValidateOrchestrateTerminal(request) != nil {
		return bridge.Response{}, bridge.ErrInvalid
	}
	root, paths, document, release, err := service.openOrchestration(ctx, workspace, request.OrchestrationID)
	if err != nil {
		return bridge.Response{}, err
	}
	defer release()
	if document.OwnerID != request.OwnerID || document.Status == "cancelled" || len(document.Join) != 0 {
		return bridge.Response{}, bridge.ErrDenied
	}
	native, err := readNativeTicket(paths.Root, request.TicketID)
	if err != nil || native.Input.ParentSessionID != document.ParentSessionID || native.Input.ChildSessionID != request.ChildSessionID {
		return bridge.Response{}, bridge.ErrDenied
	}
	if request.Status == "completed" {
		if native.State != "completed" || native.Response == nil || !bytes.Equal(native.Response.Result, request.Result) {
			return bridge.Response{}, bridge.ErrDenied
		}
	} else if native.State != "failed" {
		return bridge.Response{}, bridge.ErrDenied
	}
	if native.TerminalStatus != "" && native.TerminalStatus != request.Status {
		return bridge.Response{}, bridge.ErrDenied
	}
	if request.Status != "completed" && native.TerminalFailure != "" && native.TerminalFailure != request.Failure {
		return bridge.Response{}, bridge.ErrDenied
	}
	authority, scheduler, err := openDurableScheduler(ctx, paths.Root, document)
	if err != nil {
		return bridge.Response{}, err
	}
	checkpoint, err := authority.Snapshot(ctx, document.ScheduleID, document.Plan.PlanID, document.ParentSessionID)
	if err != nil {
		return bridge.Response{}, normalizeOrchestrationError(err)
	}
	for _, item := range checkpoint.Tasks {
		if item.TaskID != request.TaskID || item.TicketID != request.TicketID || item.ChildSessionID != request.ChildSessionID {
			continue
		}
		legacyFailureReplay := native.TerminalStatus == "" && native.State == "failed" && item.Status == orchestrator.TaskFailed &&
			(request.Status == "failed" || request.Status == "cancelled")
		if string(item.Status) != request.Status && !legacyFailureReplay {
			continue
		}
		exact := request.Status == "completed" && item.MessageID == request.MessageID && item.ResultID == request.ResultID && bytes.Equal(item.Result, request.Result) ||
			request.Status != "completed" && (item.Failure == request.Failure || legacyFailureReplay)
		if !exact {
			return bridge.Response{}, bridge.ErrDenied
		}
		document.Status = string(scheduler.Status())
		captureNativeEditArtifact(&document, request.TaskID, native.Response)
		document.UpdatedAt = service.now().UTC().Format(time.RFC3339Nano)
		if err := writeOrchestrationDocument(paths.Root, document); err != nil {
			return bridge.Response{}, fmt.Errorf("%w: repair orchestration terminal projection", bridge.ErrExecution)
		}
		return orchestrationResponse(root, document, nil), nil
	}
	status := orchestrator.TaskExecutionStatus(request.Status)
	outcome := orchestrator.NativeTaskOutcome{
		NativeTaskBinding: orchestrator.NativeTaskBinding{
			TaskID: request.TaskID, ParentSessionID: document.ParentSessionID,
			ChildSessionID: request.ChildSessionID, TicketID: request.TicketID,
		},
		Status: status, MessageID: request.MessageID, ResultID: request.ResultID,
		Result: append(json.RawMessage(nil), request.Result...), Failure: request.Failure,
	}
	if err := scheduler.Record(ctx, outcome); err != nil {
		return bridge.Response{}, normalizeOrchestrationError(err)
	}
	document.Status = string(scheduler.Status())
	if request.Status == "completed" {
		document.Results[request.TaskID] = append(json.RawMessage(nil), request.Result...)
		captureNativeEditArtifact(&document, request.TaskID, native.Response)
	}
	document.UpdatedAt = service.now().UTC().Format(time.RFC3339Nano)
	if err := writeOrchestrationDocument(paths.Root, document); err != nil {
		return bridge.Response{}, fmt.Errorf("%w: persist orchestration terminal", bridge.ErrExecution)
	}
	return orchestrationResponse(root, document, nil), nil
}

func (service *Service) JoinOrchestration(ctx context.Context, workspace string, request bridge.OrchestrateReferenceRequest) (bridge.Response, error) {
	if bridge.ValidateOrchestrateReference(request) != nil {
		return bridge.Response{}, bridge.ErrInvalid
	}
	root, paths, document, release, err := service.openOrchestration(ctx, workspace, request.OrchestrationID)
	if err != nil {
		return bridge.Response{}, err
	}
	defer release()
	if request.OwnerID == "" || document.OwnerID != request.OwnerID || document.Status == "cancelled" {
		return bridge.Response{}, bridge.ErrDenied
	}
	if len(document.Join) == 0 {
		authority, scheduler, openErr := openDurableScheduler(ctx, paths.Root, document)
		if openErr != nil {
			return bridge.Response{}, openErr
		}
		if err := service.reconcileNativeTerminals(ctx, paths.Root, &document, authority, scheduler); err != nil {
			return bridge.Response{}, err
		}
		join, joinErr := scheduler.Join(ctx)
		if joinErr != nil {
			return bridge.Response{}, normalizeOrchestrationError(joinErr)
		}
		document.Join, err = json.Marshal(join)
		if err != nil {
			return bridge.Response{}, bridge.ErrExecution
		}
		document.Status = string(scheduler.Status())
		document.UpdatedAt = service.now().UTC().Format(time.RFC3339Nano)
		if err := writeOrchestrationDocument(paths.Root, document); err != nil {
			return bridge.Response{}, fmt.Errorf("%w: persist orchestration join", bridge.ErrExecution)
		}
	}
	return orchestrationResponse(root, document, nil), nil
}

func (service *Service) StatusOrchestration(ctx context.Context, workspace string, request bridge.OrchestrateReferenceRequest) (bridge.Response, error) {
	if bridge.ValidateOrchestrateReference(request) != nil {
		return bridge.Response{}, bridge.ErrInvalid
	}
	root, paths, document, release, err := service.openOrchestration(ctx, workspace, request.OrchestrationID)
	if err != nil {
		return bridge.Response{}, err
	}
	defer release()
	if request.OwnerID != "" && document.OwnerID != request.OwnerID {
		return bridge.Response{}, bridge.ErrDenied
	}
	var prepared []bridge.OrchestrationPreparedTask
	if document.Status != "cancelled" && len(document.Join) == 0 {
		authority, scheduler, openErr := openDurableScheduler(ctx, paths.Root, document)
		if openErr != nil {
			return bridge.Response{}, openErr
		}
		if err := service.reconcileNativeTerminals(ctx, paths.Root, &document, authority, scheduler); err != nil {
			return bridge.Response{}, err
		}
		document.Status = string(scheduler.Status())
		document.UpdatedAt = service.now().UTC().Format(time.RFC3339Nano)
		if err := writeOrchestrationDocument(paths.Root, document); err != nil {
			return bridge.Response{}, bridge.ErrExecution
		}
		if request.ClaimToken != "" && document.Status == string(orchestrator.ScheduleRunning) {
			if document.ClaimTokens[request.TaskID] != orchestrationClaimTokenDigest(request.ClaimToken) {
				return bridge.Response{}, bridge.ErrDenied
			}
			prepared, err = runningOrchestrationPrepared(ctx, paths.Root, document, authority, request.TaskID, request.ChildSessionID)
			if err != nil {
				return bridge.Response{}, err
			}
		}
	}
	return orchestrationResponse(root, document, prepared), nil
}

// ResumeOrchestration transfers the durable schedule to a fresh owner epoch.
// It never re-dispatches a task: accepted terminals and prepared/running native
// children are reconstructed from the authority checkpoint, while uncertain
// dispatches remain fenced for explicit resolution.
func (service *Service) ResumeOrchestration(ctx context.Context, workspace string, request bridge.OrchestrateReferenceRequest) (bridge.Response, error) {
	if bridge.ValidateOrchestrateReference(request) != nil || request.OwnerID == "" {
		return bridge.Response{}, bridge.ErrInvalid
	}
	root, paths, document, release, err := service.openOrchestration(ctx, workspace, request.OrchestrationID)
	if err != nil {
		return bridge.Response{}, err
	}
	defer release()
	if document.OwnerID != request.OwnerID || document.Status == "cancelled" || len(document.Join) != 0 {
		return bridge.Response{}, bridge.ErrDenied
	}
	authority, err := orchestrator.NewDurableTicketAuthority(paths.Root, orchestrationDispatchTimeout)
	if err != nil {
		return bridge.Response{}, bridge.ErrUnavailable
	}
	checkpoint, err := authority.Snapshot(ctx, document.ScheduleID, document.Plan.PlanID, document.ParentSessionID)
	if err != nil {
		return bridge.Response{}, normalizeOrchestrationError(err)
	}
	for _, item := range checkpoint.Tasks {
		if item.Status == orchestrator.TaskRunning {
			return bridge.Response{}, fmt.Errorf("%w: native tasks are still running", bridge.ErrDenied)
		}
	}
	if err := authority.ResolveUncertain(ctx, document.ScheduleID); err != nil {
		return bridge.Response{}, normalizeOrchestrationError(err)
	}
	checkpoint, err = authority.Snapshot(ctx, document.ScheduleID, document.Plan.PlanID, document.ParentSessionID)
	if err != nil {
		return bridge.Response{}, normalizeOrchestrationError(err)
	}
	for _, item := range checkpoint.Tasks {
		if item.DispatchStatus != orchestrator.NativeDispatchUncertain {
			continue
		}
		native, readErr := readNativeTicket(paths.Root, item.TicketID)
		if prepared, ok := orchestrationPreparedReplay(native); readErr == nil && ok && prepared.TicketID == item.TicketID {
			if err := authority.ConfirmPreparedDispatch(ctx, document.ScheduleID, item.TicketID); err != nil {
				return bridge.Response{}, normalizeOrchestrationError(err)
			}
		}
	}
	_, current, err := openDurableScheduler(ctx, paths.Root, document)
	if err != nil {
		return bridge.Response{}, err
	}
	if err := service.reconcileNativeTerminals(ctx, paths.Root, &document, authority, current); err != nil {
		return bridge.Response{}, err
	}
	checkpoint, err = authority.Snapshot(ctx, document.ScheduleID, document.Plan.PlanID, document.ParentSessionID)
	if err != nil {
		return bridge.Response{}, normalizeOrchestrationError(err)
	}
	for _, item := range checkpoint.Tasks {
		if item.Status == orchestrator.TaskRunning {
			return bridge.Response{}, fmt.Errorf("%w: native tasks are still running", bridge.ErrDenied)
		}
	}
	ownerID, err := service.newID("owner")
	if err != nil {
		return bridge.Response{}, fmt.Errorf("%w: successor owner identity", bridge.ErrExecution)
	}
	successor := document
	successor.OwnerID = ownerID
	_, scheduler, err := openDurableScheduler(ctx, paths.Root, successor)
	if err != nil {
		return bridge.Response{}, err
	}
	successor.Status = string(scheduler.Status())
	successor.UpdatedAt = service.now().UTC().Format(time.RFC3339Nano)
	if err := writeOrchestrationDocument(paths.Root, successor); err != nil {
		return bridge.Response{}, fmt.Errorf("%w: persist orchestration takeover", bridge.ErrExecution)
	}
	return orchestrationResponse(root, successor, nil), nil
}

func (service *Service) CancelOrchestration(ctx context.Context, workspace string, request bridge.OrchestrateReferenceRequest) (bridge.Response, error) {
	if bridge.ValidateOrchestrateReference(request) != nil {
		return bridge.Response{}, bridge.ErrInvalid
	}
	root, paths, document, release, err := service.openOrchestration(ctx, workspace, request.OrchestrationID)
	if err != nil {
		return bridge.Response{}, err
	}
	defer release()
	if request.OwnerID == "" || request.OwnerID != document.OwnerID || len(document.Join) != 0 {
		return bridge.Response{}, bridge.ErrDenied
	}
	authority, scheduler, err := openDurableScheduler(ctx, paths.Root, document)
	if err != nil {
		return bridge.Response{}, err
	}
	if err := service.reconcileNativeTerminals(ctx, paths.Root, &document, authority, scheduler); err != nil {
		return bridge.Response{}, err
	}
	if status := scheduler.Status(); status == orchestrator.ScheduleCompleted || status == orchestrator.ScheduleFailed || status == orchestrator.ScheduleCancelled {
		join, joinErr := scheduler.Join(ctx)
		if joinErr != nil {
			return bridge.Response{}, normalizeOrchestrationError(joinErr)
		}
		document.Join, _ = json.Marshal(join)
		document.Status = string(status)
	} else {
		checkpoint, snapshotErr := authority.Snapshot(ctx, document.ScheduleID, document.Plan.PlanID, document.ParentSessionID)
		if snapshotErr != nil {
			return bridge.Response{}, normalizeOrchestrationError(snapshotErr)
		}
		cancelledTasks := 0
		for _, item := range checkpoint.Tasks {
			if item.Status != orchestrator.TaskRunning {
				continue
			}
			cancelledTasks++
			if _, failErr := service.Fail(context.WithoutCancel(ctx), root, bridge.NativeFailureRequest{
				ProtocolVersion: bridge.ProtocolVersion, TicketID: item.TicketID, ParentSessionID: document.ParentSessionID,
				ChildSessionID: item.ChildSessionID, Category: "native-subagent-cancelled",
			}); failErr != nil {
				return bridge.Response{}, fmt.Errorf("%w: cancel native task", bridge.ErrDenied)
			}
			if recordErr := scheduler.Record(context.WithoutCancel(ctx), orchestrator.NativeTaskOutcome{
				NativeTaskBinding: item.NativeTaskBinding, Status: orchestrator.TaskCancelled, Failure: "native orchestration was cancelled",
			}); recordErr != nil {
				return bridge.Response{}, normalizeOrchestrationError(recordErr)
			}
		}
		if scheduler.Status() == orchestrator.ScheduleCancelled {
			join, joinErr := scheduler.Join(context.WithoutCancel(ctx))
			if joinErr != nil {
				return bridge.Response{}, normalizeOrchestrationError(joinErr)
			}
			document.Join, _ = json.Marshal(join)
		}
		document.Status = string(scheduler.Status())
		if cancelledTasks == 0 {
			document.Status = "cancelled"
		}
	}
	document.UpdatedAt = service.now().UTC().Format(time.RFC3339Nano)
	if err := writeOrchestrationDocument(paths.Root, document); err != nil {
		return bridge.Response{}, fmt.Errorf("%w: persist orchestration cancellation", bridge.ErrExecution)
	}
	return orchestrationResponse(root, document, nil), nil
}

func openDurableScheduler(ctx context.Context, storageRoot string, document orchestrationDocument) (*orchestrator.DurableTicketAuthority, *orchestrator.WaveScheduler, error) {
	authority, err := orchestrator.NewDurableTicketAuthority(storageRoot, orchestrationDispatchTimeout)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: delegation authority", bridge.ErrExecution)
	}
	factory, err := orchestrator.NewSchedulerFactory(authority)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: scheduler factory", bridge.ErrExecution)
	}
	scheduler, err := factory.Open(ctx, document.Plan, document.Plan.PlanID, orchestrator.ScheduleIdentity{
		ScheduleID: document.ScheduleID, OwnerID: document.OwnerID, ParentSessionID: document.ParentSessionID,
	})
	if err != nil {
		return nil, nil, normalizeOrchestrationError(err)
	}
	return authority, scheduler, nil
}

func orchestrationResponse(root string, document orchestrationDocument, prepared []bridge.OrchestrationPreparedTask) bridge.Response {
	nextWave := document.CurrentWave
	if document.Status == string(orchestrator.SchedulePending) && len(document.PreparedBindings) > 0 {
		nextWave++
	}
	return bridge.Response{
		ProtocolVersion: bridge.ProtocolVersion, OK: true, Bridge: "healthy", Provider: "opencode", Workspace: root,
		Status: document.Status, Result: finalOrchestrationResult(document), Orchestration: &bridge.OrchestrationView{
			OrchestrationID: document.OrchestrationID, ScheduleID: document.ScheduleID, OwnerID: document.OwnerID,
			ParentSessionID: document.ParentSessionID, Status: document.Status, CurrentWave: document.CurrentWave, NextWave: nextWave, Plan: document.Plan,
			Prepared: prepared, EditArtifacts: cloneNativeEditArtifacts(document.EditArtifacts), Join: append(json.RawMessage(nil), document.Join...),
		},
	}
}

func finalOrchestrationResult(document orchestrationDocument) json.RawMessage {
	if document.Status != string(orchestrator.ScheduleCompleted) {
		return nil
	}
	dependedOn := make(map[string]bool, len(document.Plan.Tasks))
	for _, task := range document.Plan.Tasks {
		for _, dependency := range task.DependsOn {
			dependedOn[dependency] = true
		}
	}
	terminalTaskID := ""
	for _, task := range document.Plan.Tasks {
		if dependedOn[task.TaskID] {
			continue
		}
		if terminalTaskID != "" {
			return nil
		}
		terminalTaskID = task.TaskID
	}
	result := document.Results[terminalTaskID]
	if len(result) == 0 || len(result) > bridge.MaxOrchestrationResultBytes {
		return nil
	}
	return append(json.RawMessage(nil), result...)
}

func tasksByID(plan navigator.Plan) map[string]navigator.Task {
	result := make(map[string]navigator.Task, len(plan.Tasks))
	for _, task := range plan.Tasks {
		result[task.TaskID] = task
	}
	return result
}

func reconcileOrchestrationDocument(document *orchestrationDocument, checkpoint orchestrator.ScheduleCheckpoint) bool {
	changed := false
	for _, item := range checkpoint.Tasks {
		if document.PreparedBindings[item.TaskID] != item.TicketID {
			document.PreparedBindings[item.TaskID] = item.TicketID
			changed = true
		}
		if item.Status == orchestrator.TaskCompleted && !bytes.Equal(document.Results[item.TaskID], item.Result) {
			document.Results[item.TaskID] = append(json.RawMessage(nil), item.Result...)
			changed = true
		}
	}
	return changed
}

func orchestrationPreparedReplay(document nativeTicketDocument) (bridge.PreparedDispatch, bool) {
	prepared := document.Coordinator.Prepared
	if document.State != "prepared" || prepared.Invocation.ExecutionID == "" || prepared.Invocation.Prompt.System == "" {
		return bridge.PreparedDispatch{}, false
	}
	return bridge.PreparedDispatch{
		TicketID: document.TicketID, ExecutionID: prepared.Invocation.ExecutionID, Agent: nativeAgentFor(document.Input.Operation),
		Model: document.Input.Model, Prompt: prepared.Invocation.Prompt.System, PromptSHA256: prepared.PromptSHA256, Deadline: document.Deadline,
		PromptRef: bridge.PromptReceipt{ID: prepared.PromptRef.ID, Version: prepared.PromptRef.Version, SHA256: prepared.PromptSHA256},
	}, true
}

func runningOrchestrationPrepared(ctx context.Context, storageRoot string, document orchestrationDocument, authority *orchestrator.DurableTicketAuthority, taskID, childSessionID string) ([]bridge.OrchestrationPreparedTask, error) {
	checkpoint, err := authority.Snapshot(ctx, document.ScheduleID, document.Plan.PlanID, document.ParentSessionID)
	if err != nil {
		return nil, normalizeOrchestrationError(err)
	}
	items := make([]bridge.OrchestrationPreparedTask, 0)
	for _, item := range checkpoint.Tasks {
		if item.TaskID != taskID || item.ChildSessionID != childSessionID || item.Status != orchestrator.TaskRunning || item.DispatchStatus != orchestrator.NativeDispatchConfirmed {
			continue
		}
		native, readErr := readNativeTicket(storageRoot, item.TicketID)
		prepared, ok := orchestrationPreparedReplay(native)
		if readErr != nil || !ok || native.Input.ChildSessionID != item.ChildSessionID {
			continue
		}
		items = append(items, bridge.OrchestrationPreparedTask{
			TaskID: item.TaskID, ChildSessionID: item.ChildSessionID, Prepared: prepared,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].TaskID < items[j].TaskID })
	return items, nil
}

func orchestrationClaimTokenDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func normalizeOrchestrationError(err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, orchestrator.ErrAuthorityUnavailable), errors.Is(err, orchestrator.ErrCoordinatorBusy):
		return fmt.Errorf("%w: %v", bridge.ErrUnavailable, err)
	case errors.Is(err, orchestrator.ErrInvalidSchedule), errors.Is(err, orchestrator.ErrScheduleState), errors.Is(err, orchestrator.ErrNativeDispatch):
		return fmt.Errorf("%w: %v", bridge.ErrDenied, err)
	default:
		return fmt.Errorf("%w: orchestration", bridge.ErrExecution)
	}
}

func orchestrationDirectory(root string) string { return filepath.Join(root, "orchestration-plans") }

func orchestrationPath(root, orchestrationID string) (string, error) {
	if orchestrationID == "" || filepath.Base(orchestrationID) != orchestrationID || strings.ContainsAny(orchestrationID, "/\\\x00") {
		return "", bridge.ErrInvalid
	}
	return filepath.Join(orchestrationDirectory(root), orchestrationID+".json"), nil
}

func createOrchestrationDocument(root string, document orchestrationDocument) error {
	directory := orchestrationDirectory(root)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(directory); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return bridge.ErrExecution
	}
	path, err := orchestrationPath(root, document.OrchestrationID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(document)
	if err != nil || len(data) > orchestrationDocumentLimit {
		return bridge.ErrExecution
	}
	file, err := os.CreateTemp(directory, ".orchestration-*.tmp")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return bridge.ErrExecution
	}
	if err := os.Link(temporary, path); err != nil {
		return err
	}
	return syncNativeDirectory(directory)
}

func readOrchestrationDocument(root, orchestrationID string) (orchestrationDocument, error) {
	path, err := orchestrationPath(root, orchestrationID)
	if err != nil {
		return orchestrationDocument{}, err
	}
	data, err := readBoundedControlPlaneFile(path, orchestrationDocumentLimit)
	if err != nil {
		return orchestrationDocument{}, bridge.ErrDenied
	}
	var document orchestrationDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&document) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		document.Version != orchestrationDocumentVersion || document.OrchestrationID != orchestrationID ||
		navigator.ValidatePlan(context.Background(), document.Plan) != nil || document.PreparedBindings == nil || document.Results == nil {
		return orchestrationDocument{}, bridge.ErrDenied
	}
	if document.ClaimTokens == nil {
		document.ClaimTokens = make(map[string]string)
	}
	return document, nil
}

func orchestrationTaskGoal(task navigator.Task, results map[string]json.RawMessage) (string, error) {
	if len(task.DependsOn) == 0 {
		return task.Goal, nil
	}
	dependencies := make(map[string]orchestrationDependencyEvidence, len(task.DependsOn))
	textBudget := dependencyEvidenceTextLimit / len(task.DependsOn)
	for _, dependency := range task.DependsOn {
		result, ok := results[dependency]
		if !ok || len(result) == 0 {
			return "", bridge.ErrDenied
		}
		evidence, err := compactOrchestrationDependency(result, textBudget)
		if err != nil {
			return "", err
		}
		dependencies[dependency] = evidence
	}
	data, err := json.Marshal(dependencies)
	if err != nil || len(data) > dependencyEvidenceJSONLimit {
		return "", bridge.ErrDenied
	}
	goal := task.Goal + "\n\nValidated dependency evidence (bounded JSON):\n" + string(data)
	if len(goal) > orchestrationTaskGoalLimit {
		return "", bridge.ErrDenied
	}
	return goal, nil
}

type orchestrationDependencyEvidence struct {
	Status          string   `json:"status"`
	Summary         string   `json:"summary"`
	NextRecommended string   `json:"nextRecommended"`
	Risks           []string `json:"risks,omitempty"`
	Truncated       bool     `json:"truncated,omitempty"`
}

func compactOrchestrationDependency(raw json.RawMessage, budget int) (orchestrationDependencyEvidence, error) {
	var result struct {
		Status          string   `json:"status"`
		Summary         string   `json:"summary"`
		NextRecommended string   `json:"nextRecommended"`
		Risks           []string `json:"risks"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Status == "" || strings.TrimSpace(result.Summary) == "" || strings.TrimSpace(result.NextRecommended) == "" {
		return orchestrationDependencyEvidence{}, bridge.ErrDenied
	}
	if budget < 256 {
		return orchestrationDependencyEvidence{}, bridge.ErrDenied
	}
	summaryBudget := budget * 5 / 8
	nextBudget := budget / 8
	riskBudget := budget - summaryBudget - nextBudget
	evidence := orchestrationDependencyEvidence{
		Status:          result.Status,
		Summary:         boundedOrchestrationText(result.Summary, summaryBudget),
		NextRecommended: boundedOrchestrationText(result.NextRecommended, nextBudget),
	}
	for _, risk := range result.Risks {
		if len(evidence.Risks) == 2 {
			break
		}
		evidence.Risks = append(evidence.Risks, boundedOrchestrationText(risk, riskBudget/2))
	}
	evidence.Truncated = evidence.Summary != strings.TrimSpace(result.Summary) || evidence.NextRecommended != strings.TrimSpace(result.NextRecommended) || len(evidence.Risks) != len(result.Risks)
	for index := range evidence.Risks {
		if evidence.Risks[index] != strings.TrimSpace(result.Risks[index]) {
			evidence.Truncated = true
		}
	}
	return evidence, nil
}

func boundedOrchestrationText(value string, limit int) string {
	value = strings.TrimSpace(strings.Map(func(char rune) rune {
		if unicode.IsControl(char) {
			return ' '
		}
		return char
	}, value))
	if len(value) <= limit {
		return value
	}
	end := limit
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return strings.TrimSpace(value[:end])
}

func writeOrchestrationDocument(root string, document orchestrationDocument) error {
	path, err := orchestrationPath(root, document.OrchestrationID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(document)
	if err != nil || len(data) > orchestrationDocumentLimit {
		return bridge.ErrExecution
	}
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".orchestration-*.tmp")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return bridge.ErrExecution
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	return syncNativeDirectory(directory)
}

func (service *Service) openOrchestration(ctx context.Context, workspace, orchestrationID string) (string, config.Paths, orchestrationDocument, func(), error) {
	root, err := canonicalWorkspace(ctx, workspace)
	if err != nil {
		return "", config.Paths{}, orchestrationDocument{}, nil, err
	}
	paths, err := config.PathsFor(config.Options{StorageRoot: service.storageRoot, ProjectDir: root})
	if err != nil {
		return "", config.Paths{}, orchestrationDocument{}, nil, err
	}
	path, err := orchestrationPath(paths.Root, orchestrationID)
	if err != nil {
		return "", config.Paths{}, orchestrationDocument{}, nil, err
	}
	lock, err := acquireBoundedControlPlaneLock(ctx, path+".lock")
	if err != nil {
		return "", config.Paths{}, orchestrationDocument{}, nil, bridge.ErrUnavailable
	}
	document, err := readOrchestrationDocument(paths.Root, orchestrationID)
	if err != nil || document.Workspace != root {
		lock.Release()
		if err == nil {
			err = bridge.ErrDenied
		}
		return "", config.Paths{}, orchestrationDocument{}, nil, err
	}
	authority, authorityErr := orchestrator.NewDurableTicketAuthority(paths.Root, orchestrationDispatchTimeout)
	if authorityErr != nil {
		lock.Release()
		return "", config.Paths{}, orchestrationDocument{}, nil, bridge.ErrUnavailable
	}
	checkpoint, checkpointErr := authority.Snapshot(ctx, document.ScheduleID, document.Plan.PlanID, document.ParentSessionID)
	if checkpointErr != nil {
		lock.Release()
		return "", config.Paths{}, orchestrationDocument{}, nil, normalizeOrchestrationError(checkpointErr)
	}
	if reconcileOrchestrationDocument(&document, checkpoint) {
		document.UpdatedAt = service.now().UTC().Format(time.RFC3339Nano)
		if err := writeOrchestrationDocument(paths.Root, document); err != nil {
			lock.Release()
			return "", config.Paths{}, orchestrationDocument{}, nil, bridge.ErrExecution
		}
	}
	return root, paths, document, lock.Release, nil
}

func (service *Service) reconcileNativeTerminals(ctx context.Context, storageRoot string, document *orchestrationDocument, authority *orchestrator.DurableTicketAuthority, scheduler *orchestrator.WaveScheduler) error {
	checkpoint, err := authority.Snapshot(ctx, document.ScheduleID, document.Plan.PlanID, document.ParentSessionID)
	if err != nil {
		return normalizeOrchestrationError(err)
	}
	for _, item := range checkpoint.Tasks {
		if item.Status != orchestrator.TaskRunning || item.DispatchStatus != orchestrator.NativeDispatchConfirmed {
			continue
		}
		native, readErr := readNativeTicket(storageRoot, item.TicketID)
		if readErr != nil || native.Input.ChildSessionID != item.ChildSessionID {
			continue
		}
		if native.Response == nil && native.State == "prepared" && service.now().UTC().After(nativeCompletionDeadline(native.Deadline)) {
			_, _ = service.Fail(context.WithoutCancel(ctx), document.Workspace, bridge.NativeFailureRequest{
				ProtocolVersion: bridge.ProtocolVersion, TicketID: item.TicketID,
				ParentSessionID: document.ParentSessionID, ChildSessionID: item.ChildSessionID,
				Category: "native-subagent-deadline",
			})
			native, readErr = readNativeTicket(storageRoot, item.TicketID)
		}
		if readErr != nil || native.Response == nil {
			continue
		}
		outcome := orchestrator.NativeTaskOutcome{
			NativeTaskBinding: orchestrator.NativeTaskBinding{TaskID: item.TaskID, ParentSessionID: document.ParentSessionID, ChildSessionID: item.ChildSessionID, TicketID: item.TicketID},
		}
		switch native.State {
		case "completed":
			messageID := native.CompletionMessageID
			if messageID == "" {
				messageID = "message-" + item.TicketID
			}
			outcome.Status, outcome.MessageID, outcome.ResultID, outcome.Result = orchestrator.TaskCompleted, messageID, "result-"+item.TicketID, append(json.RawMessage(nil), native.Response.Result...)
		case "failed":
			outcome.Status, outcome.Failure = orchestrator.TaskFailed, native.TerminalFailure
			if outcome.Failure == "" {
				outcome.Failure = "native ticket completed with failure before orchestration acknowledgement"
			}
			if native.TerminalStatus == "cancelled" {
				outcome.Status = orchestrator.TaskCancelled
			}
		default:
			continue
		}
		if err := scheduler.Record(ctx, outcome); err != nil {
			return normalizeOrchestrationError(err)
		}
		if outcome.Status == orchestrator.TaskCompleted {
			document.Results[item.TaskID] = append(json.RawMessage(nil), outcome.Result...)
			captureNativeEditArtifact(document, item.TaskID, native.Response)
		}
	}
	return nil
}

func captureNativeEditArtifact(document *orchestrationDocument, taskID string, response *bridge.Response) {
	if document == nil || response == nil || response.EditArtifact == nil {
		return
	}
	if document.EditArtifacts == nil {
		document.EditArtifacts = make(map[string]bridge.NativeEditArtifact)
	}
	document.EditArtifacts[taskID] = cloneNativeEditArtifact(*response.EditArtifact)
}

func cloneNativeEditArtifacts(input map[string]bridge.NativeEditArtifact) map[string]bridge.NativeEditArtifact {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]bridge.NativeEditArtifact, len(input))
	for taskID, artifact := range input {
		output[taskID] = cloneNativeEditArtifact(artifact)
	}
	return output
}

func cloneNativeEditArtifact(input bridge.NativeEditArtifact) bridge.NativeEditArtifact {
	input.Changes = append([]bridge.NativeEditResult(nil), input.Changes...)
	return input
}

var _ bridge.OrchestrationRuntime = (*Service)(nil)
