package orchestrator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/vgxness/vgxness/internal/navigator"
)

func schedulerPlan(t *testing.T, tasks ...navigator.Task) navigator.Plan {
	t.Helper()
	plan, err := navigator.PlanRequest(context.Background(), navigator.Request{
		Kind: navigator.RequestKind, SchemaVersion: navigator.SchemaVersion, Goal: "Inspect independent boundaries",
		AcceptanceCriteria: []string{}, CandidateTasks: tasks, PolicyVersion: "bridge-balanced-v1", MaxParallel: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func schedulerRead(id string, dependencies ...string) navigator.Task {
	return navigator.Task{
		TaskID: id, Capability: navigator.CapabilityExplore, Operation: navigator.OperationReadFiles, Goal: "Inspect " + id,
		AcceptanceCriteria: []string{}, DependsOn: dependencies, Continuity: navigator.ContinuityIsolated,
	}
}

func binding(task, child string) NativeTaskBinding {
	return NativeTaskBinding{TaskID: task, ParentSessionID: "ses-parent", ChildSessionID: child, TicketID: "ticket-" + task}
}

func completed(value NativeTaskBinding) NativeTaskOutcome {
	return NativeTaskOutcome{
		NativeTaskBinding: value, Status: TaskCompleted, MessageID: "msg-" + value.TaskID,
		ResultID: "result-" + value.TaskID, Result: json.RawMessage(`{"kind":"agent.result","taskId":"` + value.TaskID + `"}`),
	}
}

type testTicketAuthority struct {
	mu                      sync.Mutex
	currentLeases           map[string]ScheduleLease
	currentClaimKeys        map[string]string
	claimReplay             map[string]ScheduleLease
	nextEpoch               map[string]uint64
	allowed                 map[ticketUse]struct{}
	admittedTickets         map[string]ticketUse
	admittedChildren        map[string]ticketUse
	terminalTickets         map[string]string
	terminalOutcomes        map[string]NativeTaskOutcome
	plans                   map[string]navigator.Plan
	logicalSlots            map[taskSlot]ticketUse
	dispatchResults         map[string]NativeDispatchResult
	dispatching             map[string]bool
	unresolvedDispatch      map[string]int
	admissionReplay         map[string]WaveAdmissionResult
	terminalReplay          map[string]NativeTicketVerdict
	joinReplay              map[string]JoinResult
	revoked                 map[string]struct{}
	expired                 map[string]struct{}
	terminalUnavailable     bool
	claimCommitThenError    bool
	admitCommitThenError    bool
	admitCrashAfterPrepared bool
	terminalCommitThenError bool
	claimErrorDelivered     bool
	admitErrorDelivered     bool
	preparedErrorDelivered  bool
	terminalErrorDelivered  bool
	admitResponseCommitted  chan struct{}
	admitResponseRelease    chan struct{}
	admitResponseSignalled  bool
	joinLinearizing         chan struct{}
	joinRelease             chan struct{}
	joinSignalled           bool
	joinResponseMutator     func(JoinResult) JoinResult
	claimCalls              int
	dispatchTimeout         time.Duration
}

type ticketUse struct {
	ScheduleID, PlanID, WaveID, TaskID, ParentSessionID, ChildSessionID, TicketID string
}

type taskSlot struct {
	ScheduleID, PlanID, WaveID, TaskID string
}

func authorityFor(plan navigator.Plan, bindings ...NativeTaskBinding) *testTicketAuthority {
	authority := &testTicketAuthority{
		currentLeases: make(map[string]ScheduleLease), claimReplay: make(map[string]ScheduleLease),
		currentClaimKeys: make(map[string]string), nextEpoch: make(map[string]uint64),
		allowed: make(map[ticketUse]struct{}, len(bindings)), admittedTickets: make(map[string]ticketUse),
		admittedChildren: make(map[string]ticketUse), terminalTickets: make(map[string]string),
		terminalOutcomes: make(map[string]NativeTaskOutcome),
		plans:            make(map[string]navigator.Plan),
		logicalSlots:     make(map[taskSlot]ticketUse), dispatchResults: make(map[string]NativeDispatchResult),
		dispatching: make(map[string]bool), unresolvedDispatch: make(map[string]int), admissionReplay: make(map[string]WaveAdmissionResult),
		terminalReplay: make(map[string]NativeTicketVerdict), joinReplay: make(map[string]JoinResult),
		revoked: make(map[string]struct{}), expired: make(map[string]struct{}),
		dispatchTimeout: time.Second,
	}
	authority.allow("schedule-1", plan, bindings...)
	return authority
}

func scheduleIdentity(scheduleID, ownerID string) ScheduleIdentity {
	return ScheduleIdentity{ScheduleID: scheduleID, OwnerID: ownerID, ParentSessionID: "ses-parent"}
}

func (authority *testTicketAuthority) AcquireSchedule(ctx context.Context, claim ScheduleClaim) (ScheduleLease, error) {
	if err := ctx.Err(); err != nil {
		return ScheduleLease{Verdict: ScheduleLeaseUnavailable}, err
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	authority.claimCalls++
	expectedKey, err := scheduleClaimIdempotencyKey(claim)
	if err != nil || claim.IdempotencyKey != expectedKey {
		return ScheduleLease{Verdict: ScheduleLeaseRejected}, nil
	}
	if authority.dispatching[claim.ScheduleID] || authority.unresolvedDispatch[claim.ScheduleID] != 0 {
		return ScheduleLease{Verdict: ScheduleLeaseUnavailable}, nil
	}
	if replay, ok := authority.claimReplay[claim.IdempotencyKey]; ok {
		if authority.currentClaimKeys[claim.ScheduleID] != claim.IdempotencyKey {
			return ScheduleLease{Verdict: ScheduleLeaseRejected}, nil
		}
		replay.Checkpoint = authority.checkpointLocked(claim.ScheduleID, claim.PlanID, claim.ParentSessionID)
		return replay, nil
	}
	if current, ok := authority.currentLeases[claim.ScheduleID]; ok {
		if current.PlanID != claim.PlanID || current.ParentSessionID != claim.ParentSessionID || current.OwnerID == claim.OwnerID {
			return ScheduleLease{Verdict: ScheduleLeaseRejected}, nil
		}
	}
	authority.nextEpoch[claim.ScheduleID]++
	lease := ScheduleLease{
		Verdict: ScheduleLeaseGranted, ScheduleID: claim.ScheduleID, PlanID: claim.PlanID,
		ParentSessionID: claim.ParentSessionID, OwnerID: claim.OwnerID, Epoch: authority.nextEpoch[claim.ScheduleID],
		Checkpoint: authority.checkpointLocked(claim.ScheduleID, claim.PlanID, claim.ParentSessionID),
	}
	authority.currentLeases[claim.ScheduleID] = lease
	authority.currentClaimKeys[claim.ScheduleID] = claim.IdempotencyKey
	authority.claimReplay[claim.IdempotencyKey] = lease
	if authority.claimCommitThenError && !authority.claimErrorDelivered {
		authority.claimErrorDelivered = true
		return ScheduleLease{Verdict: ScheduleLeaseUnavailable}, errors.New("schedule claim response lost after commit")
	}
	return lease, nil
}

func (authority *testTicketAuthority) ValidateLease(ctx context.Context, ref ScheduleLeaseRef) (NativeTicketVerdict, error) {
	if err := ctx.Err(); err != nil {
		return NativeTicketUnavailable, err
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	lease, ok := authority.currentLeases[ref.ScheduleID]
	if !ok || lease.Verdict != ScheduleLeaseGranted || lease.ScheduleID != ref.ScheduleID || lease.PlanID != ref.PlanID || lease.ParentSessionID != ref.ParentSessionID || lease.OwnerID != ref.OwnerID || lease.Epoch != ref.Epoch {
		return NativeTicketFenced, nil
	}
	return NativeTicketAccepted, nil
}

func (authority *testTicketAuthority) checkpointLocked(scheduleID, planID, parentSessionID string) ScheduleCheckpoint {
	checkpoint := ScheduleCheckpoint{ScheduleID: scheduleID, PlanID: planID, ParentSessionID: parentSessionID}
	for _, use := range authority.admittedTickets {
		if use.ScheduleID != scheduleID || use.PlanID != planID || use.ParentSessionID != parentSessionID {
			continue
		}
		dispatchResult, ok := authority.dispatchResults[use.TicketID]
		if !ok {
			dispatchResult = NativeDispatchResult{Status: NativeDispatchUncertain, Failure: "native dispatch state missing"}
		}
		item := NativeTaskCheckpoint{
			NativeTaskBinding: NativeTaskBinding{
				TaskID: use.TaskID, ParentSessionID: use.ParentSessionID,
				ChildSessionID: use.ChildSessionID, TicketID: use.TicketID,
			},
			DispatchStatus: dispatchResult.Status, Status: TaskRunning,
		}
		switch {
		case authority.ticketRevokedLocked(use.TicketID):
			item.Status, item.Failure = TaskFailed, "native ticket authority revoked"
		case authority.ticketExpiredLocked(use.TicketID):
			item.Status, item.Failure = TaskFailed, "native ticket authority expired"
		case dispatchResult.Status == NativeDispatchNotStarted || dispatchResult.Status == NativeDispatchUncertain:
			item.Status, item.Failure = TaskFailed, dispatchResult.Failure
		case dispatchResult.Status != NativeDispatchConfirmed:
			item.Status, item.DispatchStatus, item.Failure = TaskFailed, NativeDispatchUncertain, "native dispatch state invalid"
		case authority.terminalOutcomes[use.TicketID].TaskID != "":
			outcome := authority.terminalOutcomes[use.TicketID]
			item.Status, item.MessageID, item.ResultID = outcome.Status, outcome.MessageID, outcome.ResultID
			item.Result = append(json.RawMessage(nil), outcome.Result...)
			item.Failure = outcome.Failure
		}
		checkpoint.Tasks = append(checkpoint.Tasks, item)
	}
	sort.Slice(checkpoint.Tasks, func(i, j int) bool { return checkpoint.Tasks[i].TaskID < checkpoint.Tasks[j].TaskID })
	return checkpoint
}

func (authority *testTicketAuthority) ticketRevokedLocked(ticketID string) bool {
	_, ok := authority.revoked[ticketID]
	return ok
}

func (authority *testTicketAuthority) ticketExpiredLocked(ticketID string) bool {
	_, ok := authority.expired[ticketID]
	return ok
}

func (authority *testTicketAuthority) allow(scheduleID string, plan navigator.Plan, bindings ...NativeTaskBinding) {
	authority.mu.Lock()
	defer authority.mu.Unlock()
	authority.plans[plan.PlanID] = plan
	for _, item := range bindings {
		authority.allowed[ticketUseFor(scheduleID, plan, item)] = struct{}{}
	}
}

func ticketUseFor(scheduleID string, plan navigator.Plan, binding NativeTaskBinding) ticketUse {
	waveID := ""
	for _, wave := range plan.Waves {
		for _, taskID := range wave.TaskIDs {
			if taskID == binding.TaskID {
				waveID = wave.WaveID
			}
		}
	}
	return ticketUse{scheduleID, plan.PlanID, waveID, binding.TaskID, binding.ParentSessionID, binding.ChildSessionID, binding.TicketID}
}

func ticketUseFromAdmission(scheduleID, planID, waveID string, binding NativeTaskBinding) ticketUse {
	return ticketUse{scheduleID, planID, waveID, binding.TaskID, binding.ParentSessionID, binding.ChildSessionID, binding.TicketID}
}

func (authority *testTicketAuthority) AdmitWave(ctx context.Context, admission WaveAdmission, dispatch NativeTaskDispatch) (WaveAdmissionResult, error) {
	if err := ctx.Err(); err != nil {
		return WaveAdmissionResult{Verdict: NativeTicketUnavailable}, err
	}
	if dispatch == nil {
		return WaveAdmissionResult{Verdict: NativeTicketRevoked}, nil
	}
	authority.mu.Lock()
	expectedKey, err := waveAdmissionIdempotencyKey(admission)
	if err != nil || admission.IdempotencyKey != expectedKey {
		authority.mu.Unlock()
		return WaveAdmissionResult{Verdict: NativeTicketRevoked}, nil
	}
	lease, leaseOK := authority.currentLeases[admission.ScheduleID]
	if !leaseOK || lease.Verdict != ScheduleLeaseGranted || lease.ScheduleID != admission.ScheduleID || lease.PlanID != admission.PlanID || lease.OwnerID != admission.OwnerID || lease.Epoch != admission.ScheduleEpoch {
		authority.mu.Unlock()
		return WaveAdmissionResult{Verdict: NativeTicketFenced}, nil
	}
	if authority.dispatching[admission.ScheduleID] {
		authority.mu.Unlock()
		return WaveAdmissionResult{Verdict: NativeTicketUnavailable}, nil
	}
	plan, planOK := authority.plans[admission.PlanID]
	expectedPrerequisites, prerequisitesOK := prerequisiteTaskIDs(plan, admission.WaveID)
	if !planOK || !prerequisitesOK || !slices.Equal(admission.PrerequisiteTaskIDs, expectedPrerequisites) {
		authority.mu.Unlock()
		return WaveAdmissionResult{Verdict: NativeTicketRevoked}, nil
	}
	checkpoint := authority.checkpointLocked(admission.ScheduleID, admission.PlanID, lease.ParentSessionID)
	checkpointTasks := make(map[string]NativeTaskCheckpoint, len(checkpoint.Tasks))
	for _, item := range checkpoint.Tasks {
		checkpointTasks[item.TaskID] = item
	}
	for _, taskID := range admission.PrerequisiteTaskIDs {
		item, ok := checkpointTasks[taskID]
		if !ok || item.Status != TaskCompleted || item.DispatchStatus != NativeDispatchConfirmed {
			authority.mu.Unlock()
			return WaveAdmissionResult{Verdict: NativeTicketRevoked, Checkpoint: checkpoint}, nil
		}
	}
	for _, binding := range admission.Bindings {
		if binding.ParentSessionID != lease.ParentSessionID {
			authority.mu.Unlock()
			return WaveAdmissionResult{Verdict: NativeTicketRevoked}, nil
		}
		if _, revoked := authority.revoked[binding.TicketID]; revoked {
			authority.recordRejectedAdmissionLocked(admission, "native ticket authority revoked")
			checkpoint := authority.checkpointLocked(admission.ScheduleID, admission.PlanID, lease.ParentSessionID)
			authority.mu.Unlock()
			return WaveAdmissionResult{Verdict: NativeTicketRevoked, Checkpoint: checkpoint}, nil
		}
		if _, expired := authority.expired[binding.TicketID]; expired {
			authority.recordRejectedAdmissionLocked(admission, "native ticket authority expired")
			checkpoint := authority.checkpointLocked(admission.ScheduleID, admission.PlanID, lease.ParentSessionID)
			authority.mu.Unlock()
			return WaveAdmissionResult{Verdict: NativeTicketExpired, Checkpoint: checkpoint}, nil
		}
	}
	if replay, ok := authority.admissionReplay[admission.IdempotencyKey]; ok {
		replay.Checkpoint = authority.checkpointLocked(admission.ScheduleID, admission.PlanID, lease.ParentSessionID)
		authority.mu.Unlock()
		return replay, nil
	}
	uses := make([]ticketUse, len(admission.Bindings))
	for index, binding := range admission.Bindings {
		uses[index] = ticketUseFromAdmission(admission.ScheduleID, admission.PlanID, admission.WaveID, binding)
		_, allowed := authority.allowed[uses[index]]
		_, ticketConsumed := authority.admittedTickets[binding.TicketID]
		_, childConsumed := authority.admittedChildren[binding.ChildSessionID]
		slot := taskSlot{admission.ScheduleID, admission.PlanID, admission.WaveID, binding.TaskID}
		_, slotConsumed := authority.logicalSlots[slot]
		if !allowed || ticketConsumed || childConsumed || slotConsumed {
			authority.mu.Unlock()
			return WaveAdmissionResult{Verdict: NativeTicketRevoked}, nil
		}
	}
	for _, use := range uses {
		authority.admittedTickets[use.TicketID] = use
		authority.admittedChildren[use.ChildSessionID] = use
		authority.logicalSlots[taskSlot{use.ScheduleID, use.PlanID, use.WaveID, use.TaskID}] = use
		authority.dispatchResults[use.TicketID] = NativeDispatchResult{Status: NativeDispatchUncertain, Failure: "native dispatch interrupted before durable result"}
	}
	authority.admissionReplay[admission.IdempotencyKey] = WaveAdmissionResult{
		Verdict:    NativeTicketAccepted,
		Checkpoint: authority.checkpointLocked(admission.ScheduleID, admission.PlanID, lease.ParentSessionID),
	}
	if authority.admitCrashAfterPrepared && !authority.preparedErrorDelivered {
		authority.preparedErrorDelivered = true
		authority.mu.Unlock()
		return WaveAdmissionResult{Verdict: NativeTicketUnavailable}, errors.New("authority interrupted after durable admission preparation")
	}
	authority.dispatching[admission.ScheduleID] = true
	for _, binding := range admission.Bindings {
		authority.mu.Unlock()
		result := NativeDispatchResult{Status: NativeDispatchNotStarted, Failure: "native dispatch context cancelled before invocation"}
		var lateResult <-chan NativeDispatchResult
		if ctx.Err() == nil {
			dispatchContext, cancelDispatch := context.WithTimeout(ctx, authority.dispatchTimeout)
			resultChannel := make(chan NativeDispatchResult, 1)
			go func() { resultChannel <- dispatch(dispatchContext, binding) }()
			select {
			case result = <-resultChannel:
			case <-dispatchContext.Done():
				result = NativeDispatchResult{Status: NativeDispatchUncertain, Failure: "native dispatch exceeded its authority deadline"}
				lateResult = resultChannel
			}
			cancelDispatch()
		}
		if !validDispatchResult(result) {
			result = NativeDispatchResult{Status: NativeDispatchUncertain, Failure: "native dispatch returned an invalid classification"}
		}
		authority.mu.Lock()
		authority.dispatchResults[binding.TicketID] = result
		if lateResult != nil {
			authority.unresolvedDispatch[admission.ScheduleID]++
			go authority.awaitLateDispatch(admission.ScheduleID, lateResult)
		}
	}
	delete(authority.dispatching, admission.ScheduleID)
	result := WaveAdmissionResult{
		Verdict:    NativeTicketAccepted,
		Checkpoint: authority.checkpointLocked(admission.ScheduleID, admission.PlanID, lease.ParentSessionID),
	}
	authority.admissionReplay[admission.IdempotencyKey] = result
	if authority.admitResponseCommitted != nil && !authority.admitResponseSignalled {
		authority.admitResponseSignalled = true
		close(authority.admitResponseCommitted)
		release := authority.admitResponseRelease
		authority.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-release:
		}
		authority.mu.Lock()
	}
	if authority.admitCommitThenError && !authority.admitErrorDelivered {
		authority.admitErrorDelivered = true
		authority.mu.Unlock()
		return WaveAdmissionResult{Verdict: NativeTicketUnavailable}, errors.New("admission response lost after commit")
	}
	authority.mu.Unlock()
	return result, nil
}

func (authority *testTicketAuthority) awaitLateDispatch(scheduleID string, result <-chan NativeDispatchResult) {
	<-result
	authority.mu.Lock()
	defer authority.mu.Unlock()
	authority.unresolvedDispatch[scheduleID]--
	if authority.unresolvedDispatch[scheduleID] == 0 {
		delete(authority.unresolvedDispatch, scheduleID)
	}
}

func (authority *testTicketAuthority) recordRejectedAdmissionLocked(admission WaveAdmission, failure string) {
	for _, binding := range admission.Bindings {
		use := ticketUseFromAdmission(admission.ScheduleID, admission.PlanID, admission.WaveID, binding)
		if _, allowed := authority.allowed[use]; !allowed {
			continue
		}
		if admitted, exists := authority.admittedTickets[binding.TicketID]; exists && admitted != use {
			continue
		}
		if admitted, exists := authority.admittedChildren[binding.ChildSessionID]; exists && admitted != use {
			continue
		}
		slot := taskSlot{use.ScheduleID, use.PlanID, use.WaveID, use.TaskID}
		if admitted, exists := authority.logicalSlots[slot]; exists && admitted != use {
			continue
		}
		authority.admittedTickets[use.TicketID] = use
		authority.admittedChildren[use.ChildSessionID] = use
		authority.logicalSlots[slot] = use
		if _, exists := authority.dispatchResults[use.TicketID]; !exists {
			authority.dispatchResults[use.TicketID] = NativeDispatchResult{Status: NativeDispatchNotStarted, Failure: failure}
		}
	}
}

func prerequisiteTaskIDs(plan navigator.Plan, waveID string) ([]string, bool) {
	prerequisites := []string{}
	for _, wave := range plan.Waves {
		if wave.WaveID == waveID {
			sort.Strings(prerequisites)
			return prerequisites, true
		}
		prerequisites = append(prerequisites, wave.TaskIDs...)
	}
	return nil, false
}

func validDispatchResult(result NativeDispatchResult) bool {
	failure := strings.TrimSpace(result.Failure)
	switch result.Status {
	case NativeDispatchConfirmed:
		return result.Failure == ""
	case NativeDispatchNotStarted, NativeDispatchUncertain:
		return failure != "" && utf8.RuneCountInString(result.Failure) <= 2048
	default:
		return false
	}
}

func (authority *testTicketAuthority) AcceptTerminal(ctx context.Context, acceptance TerminalAcceptance) (NativeTicketVerdict, error) {
	if err := ctx.Err(); err != nil {
		return NativeTicketUnavailable, err
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	expectedKey, err := terminalAcceptanceIdempotencyKey(acceptance)
	if err != nil || acceptance.IdempotencyKey != expectedKey {
		return NativeTicketRevoked, nil
	}
	lease, leaseOK := authority.currentLeases[acceptance.ScheduleID]
	if !leaseOK || lease.Verdict != ScheduleLeaseGranted || lease.ScheduleID != acceptance.ScheduleID || lease.PlanID != acceptance.PlanID || lease.OwnerID != acceptance.OwnerID || lease.Epoch != acceptance.ScheduleEpoch || lease.ParentSessionID != acceptance.Outcome.ParentSessionID {
		return NativeTicketFenced, nil
	}
	if _, revoked := authority.revoked[acceptance.Outcome.TicketID]; revoked {
		return NativeTicketRevoked, nil
	}
	if _, expired := authority.expired[acceptance.Outcome.TicketID]; expired {
		return NativeTicketExpired, nil
	}
	if replay, ok := authority.terminalReplay[acceptance.IdempotencyKey]; ok {
		return replay, nil
	}
	use := ticketUseFromAdmission(acceptance.ScheduleID, acceptance.PlanID, acceptance.WaveID, acceptance.Outcome.NativeTaskBinding)
	if authority.terminalUnavailable {
		return NativeTicketUnavailable, nil
	}
	admitted, ok := authority.admittedTickets[use.TicketID]
	childAdmission, childOK := authority.admittedChildren[use.ChildSessionID]
	_, consumed := authority.terminalTickets[use.TicketID]
	dispatchResult := authority.dispatchResults[use.TicketID]
	if !ok || !childOK || admitted != use || childAdmission != use || consumed || dispatchResult.Status != NativeDispatchConfirmed {
		return NativeTicketRevoked, nil
	}
	authority.terminalTickets[use.TicketID] = acceptance.IdempotencyKey
	authority.terminalOutcomes[use.TicketID] = NativeTaskOutcome{
		NativeTaskBinding: acceptance.Outcome.NativeTaskBinding, Status: acceptance.Outcome.Status,
		MessageID: acceptance.Outcome.MessageID, ResultID: acceptance.Outcome.ResultID,
		Result: append(json.RawMessage(nil), acceptance.Outcome.Result...), Failure: acceptance.Outcome.Failure,
	}
	authority.terminalReplay[acceptance.IdempotencyKey] = NativeTicketAccepted
	if authority.terminalCommitThenError && !authority.terminalErrorDelivered {
		authority.terminalErrorDelivered = true
		return NativeTicketUnavailable, errors.New("terminal response lost after commit")
	}
	return NativeTicketAccepted, nil
}

func (authority *testTicketAuthority) AcceptJoin(ctx context.Context, acceptance JoinAcceptance) (JoinAcceptanceResult, error) {
	if err := ctx.Err(); err != nil {
		return JoinAcceptanceResult{Verdict: NativeTicketUnavailable}, err
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	expectedKey, err := joinAcceptanceIdempotencyKey(acceptance)
	if err != nil || acceptance.IdempotencyKey != expectedKey {
		return JoinAcceptanceResult{Verdict: NativeTicketRevoked}, nil
	}
	lease, ok := authority.currentLeases[acceptance.ScheduleID]
	if !ok || lease.Verdict != ScheduleLeaseGranted || lease.PlanID != acceptance.PlanID || lease.ParentSessionID != acceptance.ParentSessionID || lease.OwnerID != acceptance.OwnerID || lease.Epoch != acceptance.ScheduleEpoch {
		return JoinAcceptanceResult{Verdict: NativeTicketFenced}, nil
	}
	plan, ok := authority.plans[acceptance.PlanID]
	if !ok {
		return JoinAcceptanceResult{Verdict: NativeTicketRevoked}, nil
	}
	lease.Checkpoint = authority.checkpointLocked(acceptance.ScheduleID, acceptance.PlanID, acceptance.ParentSessionID)
	status, _, tasks, err := restoreCheckpoint(plan, ScheduleIdentity{
		ScheduleID: acceptance.ScheduleID, OwnerID: acceptance.OwnerID, ParentSessionID: acceptance.ParentSessionID,
	}, lease)
	if err != nil || status != ScheduleCompleted && status != ScheduleFailed && status != ScheduleCancelled {
		return JoinAcceptanceResult{Verdict: NativeTicketRevoked}, nil
	}
	authoritative, err := buildJoinResult(ctx, plan, status, tasks)
	if err != nil {
		return JoinAcceptanceResult{Verdict: NativeTicketRevoked}, nil
	}
	candidateData, candidateErr := json.Marshal(acceptance.Candidate)
	authoritativeData, authoritativeErr := json.Marshal(authoritative)
	if candidateErr != nil || authoritativeErr != nil || !bytes.Equal(candidateData, authoritativeData) {
		return JoinAcceptanceResult{Verdict: NativeTicketRevoked}, nil
	}
	if authority.joinLinearizing != nil && !authority.joinSignalled {
		authority.joinSignalled = true
		close(authority.joinLinearizing)
		<-authority.joinRelease
	}
	if replay, ok := authority.joinReplay[acceptance.IdempotencyKey]; ok {
		return JoinAcceptanceResult{Verdict: NativeTicketAccepted, Join: authority.joinResponse(replay)}, nil
	}
	authority.joinReplay[acceptance.IdempotencyKey] = cloneJoinResult(authoritative)
	return JoinAcceptanceResult{Verdict: NativeTicketAccepted, Join: authority.joinResponse(authoritative)}, nil
}

func (authority *testTicketAuthority) joinResponse(result JoinResult) JoinResult {
	result = cloneJoinResult(result)
	if authority.joinResponseMutator != nil {
		return authority.joinResponseMutator(result)
	}
	return result
}

func cloneJoinResult(result JoinResult) JoinResult {
	result.Tasks = append([]JoinedTask(nil), result.Tasks...)
	return result
}

func (authority *testTicketAuthority) revoke(ticketID string) {
	authority.mu.Lock()
	defer authority.mu.Unlock()
	authority.revoked[ticketID] = struct{}{}
}

func (authority *testTicketAuthority) expire(ticketID string) {
	authority.mu.Lock()
	defer authority.mu.Unlock()
	authority.expired[ticketID] = struct{}{}
}

func (authority *testTicketAuthority) setTerminalUnavailable(value bool) {
	authority.mu.Lock()
	defer authority.mu.Unlock()
	authority.terminalUnavailable = value
}

type reentrantAuthority struct {
	delegate *testTicketAuthority
	onAdmit  func()
}

type cancelFirstAcquireAuthority struct {
	delegate *testTicketAuthority
	once     sync.Once
	started  chan struct{}
}

type delayedAcquireAuthority struct {
	NativeTicketAuthority
	owner     string
	once      sync.Once
	committed chan struct{}
	release   chan struct{}
}

func (authority *delayedAcquireAuthority) AcquireSchedule(ctx context.Context, claim ScheduleClaim) (ScheduleLease, error) {
	lease, err := authority.NativeTicketAuthority.AcquireSchedule(ctx, claim)
	if err == nil && claim.OwnerID == authority.owner {
		authority.once.Do(func() {
			close(authority.committed)
			<-authority.release
		})
	}
	return lease, err
}

func (authority *cancelFirstAcquireAuthority) AcquireSchedule(ctx context.Context, claim ScheduleClaim) (ScheduleLease, error) {
	blocked := false
	authority.once.Do(func() {
		blocked = true
		close(authority.started)
	})
	if blocked {
		<-ctx.Done()
		return ScheduleLease{Verdict: ScheduleLeaseUnavailable}, ctx.Err()
	}
	return authority.delegate.AcquireSchedule(ctx, claim)
}

func (authority *cancelFirstAcquireAuthority) ValidateLease(ctx context.Context, ref ScheduleLeaseRef) (NativeTicketVerdict, error) {
	return authority.delegate.ValidateLease(ctx, ref)
}

func (authority *cancelFirstAcquireAuthority) AdmitWave(ctx context.Context, admission WaveAdmission, dispatch NativeTaskDispatch) (WaveAdmissionResult, error) {
	return authority.delegate.AdmitWave(ctx, admission, dispatch)
}

func (authority *cancelFirstAcquireAuthority) AcceptTerminal(ctx context.Context, acceptance TerminalAcceptance) (NativeTicketVerdict, error) {
	return authority.delegate.AcceptTerminal(ctx, acceptance)
}

func (authority *cancelFirstAcquireAuthority) AcceptJoin(ctx context.Context, acceptance JoinAcceptance) (JoinAcceptanceResult, error) {
	return authority.delegate.AcceptJoin(ctx, acceptance)
}

type emptyCheckpointAuthority struct {
	delegate *testTicketAuthority
}

func (authority *emptyCheckpointAuthority) AcquireSchedule(ctx context.Context, claim ScheduleClaim) (ScheduleLease, error) {
	lease, err := authority.delegate.AcquireSchedule(ctx, claim)
	lease.Checkpoint = ScheduleCheckpoint{}
	return lease, err
}

func (authority *emptyCheckpointAuthority) ValidateLease(ctx context.Context, ref ScheduleLeaseRef) (NativeTicketVerdict, error) {
	return authority.delegate.ValidateLease(ctx, ref)
}

func (authority *emptyCheckpointAuthority) AdmitWave(ctx context.Context, admission WaveAdmission, dispatch NativeTaskDispatch) (WaveAdmissionResult, error) {
	return authority.delegate.AdmitWave(ctx, admission, dispatch)
}

func (authority *emptyCheckpointAuthority) AcceptTerminal(ctx context.Context, acceptance TerminalAcceptance) (NativeTicketVerdict, error) {
	return authority.delegate.AcceptTerminal(ctx, acceptance)
}

func (authority *emptyCheckpointAuthority) AcceptJoin(ctx context.Context, acceptance JoinAcceptance) (JoinAcceptanceResult, error) {
	return authority.delegate.AcceptJoin(ctx, acceptance)
}

func (authority *reentrantAuthority) AcquireSchedule(ctx context.Context, claim ScheduleClaim) (ScheduleLease, error) {
	return authority.delegate.AcquireSchedule(ctx, claim)
}

func (authority *reentrantAuthority) ValidateLease(ctx context.Context, ref ScheduleLeaseRef) (NativeTicketVerdict, error) {
	return authority.delegate.ValidateLease(ctx, ref)
}

func (authority *reentrantAuthority) AdmitWave(ctx context.Context, admission WaveAdmission, dispatch NativeTaskDispatch) (WaveAdmissionResult, error) {
	if authority.onAdmit != nil {
		authority.onAdmit()
	}
	return authority.delegate.AdmitWave(ctx, admission, dispatch)
}

func (authority *reentrantAuthority) AcceptTerminal(ctx context.Context, acceptance TerminalAcceptance) (NativeTicketVerdict, error) {
	return authority.delegate.AcceptTerminal(ctx, acceptance)
}

func (authority *reentrantAuthority) AcceptJoin(ctx context.Context, acceptance JoinAcceptance) (JoinAcceptanceResult, error) {
	return authority.delegate.AcceptJoin(ctx, acceptance)
}

func newScheduler(t *testing.T, plan navigator.Plan, bindings ...NativeTaskBinding) *WaveScheduler {
	t.Helper()
	scheduler, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"), authorityFor(plan, bindings...))
	if err != nil {
		t.Fatal(err)
	}
	return scheduler
}

func openScheduler(ctx context.Context, plan navigator.Plan, expectedPlanID string, identity ScheduleIdentity, authority NativeTicketAuthority) (*WaveScheduler, error) {
	factory, err := NewSchedulerFactory(authority)
	if err != nil {
		return nil, err
	}
	return factory.Open(ctx, plan, expectedPlanID, identity)
}

func schedulerFactory(t *testing.T, authority NativeTicketAuthority) *SchedulerFactory {
	t.Helper()
	factory, err := NewSchedulerFactory(authority)
	if err != nil {
		t.Fatal(err)
	}
	return factory
}

func successfulDispatch(ctx context.Context, _ NativeTaskBinding) NativeDispatchResult {
	if err := ctx.Err(); err != nil {
		return NativeDispatchResult{Status: NativeDispatchNotStarted, Failure: err.Error()}
	}
	return NativeDispatchResult{Status: NativeDispatchConfirmed}
}

func TestWaveSchedulerRequiresDistinctNativeChildrenAndJoinsParallelWave(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"), schedulerRead("task-b"))
	a, b := binding("task-a", "ses-child-a"), binding("task-b", "ses-child-b")
	scheduler := newScheduler(t, plan, a, b)
	wave, ok := scheduler.NextWave()
	if !ok || wave.Mode != "parallel" {
		t.Fatalf("wave=%#v ok=%t", wave, ok)
	}
	if err := scheduler.StartWave(context.Background(), wave.WaveID, []NativeTaskBinding{a, b}, successfulDispatch); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Record(context.Background(), completed(a)); err != nil || scheduler.Status() != ScheduleRunning {
		t.Fatalf("first completion err=%v status=%s", err, scheduler.Status())
	}
	if err := scheduler.Record(context.Background(), completed(b)); err != nil || scheduler.Status() != ScheduleCompleted {
		t.Fatalf("second completion err=%v status=%s", err, scheduler.Status())
	}
	join, err := scheduler.Join(context.Background())
	if err != nil || join.Status != "completed" || join.Completed != 2 || len(join.Tasks) != 2 {
		t.Fatalf("join=%#v err=%v", join, err)
	}
	expected := sha256.Sum256(completed(a).Result)
	if join.Tasks[0].ResultDigest != "sha256-"+hex.EncodeToString(expected[:]) {
		t.Fatalf("result digest was not derived from accepted bytes: %#v", join.Tasks[0])
	}
}

func TestWaveSchedulerRejectsDetachedOrReusedNativeSessions(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"), schedulerRead("task-b"))
	for name, bindings := range map[string][]NativeTaskBinding{
		"detached":      {binding("task-a", "ses-parent"), binding("task-b", "ses-child-b")},
		"reused child":  {binding("task-a", "ses-child"), binding("task-b", "ses-child")},
		"mixed parents": {binding("task-a", "ses-child-a"), {TaskID: "task-b", ParentSessionID: "ses-other", ChildSessionID: "ses-child-b", TicketID: "ticket-b"}},
	} {
		t.Run(name, func(t *testing.T) {
			scheduler := newScheduler(t, plan, bindings...)
			if err := scheduler.StartWave(context.Background(), "wave-1", bindings, successfulDispatch); !errors.Is(err, ErrInvalidSchedule) {
				t.Fatalf("unsafe bindings accepted: %v", err)
			}
		})
	}
}

func TestWaveSchedulerStopsDependentWavesAfterPartialFailure(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"), schedulerRead("task-b"), schedulerRead("task-c", "task-a", "task-b"))
	a, b := binding("task-a", "ses-child-a"), binding("task-b", "ses-child-b")
	c := binding("task-c", "ses-child-c")
	scheduler := newScheduler(t, plan, a, b, c)
	if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a, b}, successfulDispatch); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Record(context.Background(), completed(a)); err != nil {
		t.Fatal(err)
	}
	failure := NativeTaskOutcome{NativeTaskBinding: b, Status: TaskFailed, Failure: "provider unavailable"}
	if err := scheduler.Record(context.Background(), failure); err != nil || scheduler.Status() != ScheduleFailed {
		t.Fatalf("failure err=%v status=%s", err, scheduler.Status())
	}
	if _, ok := scheduler.NextWave(); ok {
		t.Fatal("dependent wave became runnable after failure")
	}
	join, err := scheduler.Join(context.Background())
	if err != nil || join.Status != "partial" || join.Completed != 1 || join.Failed != 1 {
		t.Fatalf("join=%#v err=%v", join, err)
	}
}

func TestWaveSchedulerRejectsTamperedWaveTopology(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"), schedulerRead("task-b"))
	plan.Waves[0].TaskIDs = []string{"task-a"}
	if _, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"), authorityFor(plan)); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("tampered topology accepted: %v", err)
	}
}

func TestWaveSchedulerRejectsAnyContentMutationUnderStalePlanIdentity(t *testing.T) {
	original := schedulerPlan(t, schedulerRead("task-a"), schedulerRead("task-b"))
	mutations := map[string]func(*navigator.Plan){
		"task goal":       func(plan *navigator.Plan) { plan.Tasks[0].Goal = "mutated" },
		"write operation": func(plan *navigator.Plan) { plan.Tasks[0].Operation = navigator.OperationWriteFiles },
		"decision":        func(plan *navigator.Plan) { plan.Decision = "sequential" },
		"rationale":       func(plan *navigator.Plan) { plan.Rationale = "caller supplied" },
		"plan id":         func(plan *navigator.Plan) { plan.PlanID = "plan-stale" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			var plan navigator.Plan
			if err := json.Unmarshal(data, &plan); err != nil {
				t.Fatal(err)
			}
			mutate(&plan)
			if _, err := openScheduler(context.Background(), plan, original.PlanID, scheduleIdentity("schedule-1", "owner-1"), authorityFor(original)); !errors.Is(err, ErrInvalidSchedule) {
				t.Fatalf("tampered plan accepted: %v", err)
			}
		})
	}
}

func TestWaveSchedulerPinsTrustedParentAndPreparedTicketsAcrossWaves(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"), schedulerRead("task-b", "task-a"))
	a, b := binding("task-a", "ses-child-a"), binding("task-b", "ses-child-b")
	scheduler := newScheduler(t, plan, a, b)
	if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Record(context.Background(), completed(a)); err != nil {
		t.Fatal(err)
	}
	wrongParent := b
	wrongParent.ParentSessionID = "ses-other"
	if err := scheduler.StartWave(context.Background(), "wave-2", []NativeTaskBinding{wrongParent}, successfulDispatch); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("parent switch accepted: %v", err)
	}
	forgedTicket := b
	forgedTicket.TicketID = "ticket-forged"
	if err := scheduler.StartWave(context.Background(), "wave-2", []NativeTaskBinding{forgedTicket}, successfulDispatch); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("unprepared ticket accepted: %v", err)
	}
	if scheduler.Status() != SchedulePending {
		t.Fatalf("untrusted rejection mutated durable schedule: %s", scheduler.Status())
	}
	if err := scheduler.StartWave(context.Background(), "wave-2", []NativeTaskBinding{b}, successfulDispatch); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Record(context.Background(), completed(b)); err != nil {
		t.Fatal(err)
	}
	join, err := scheduler.Join(context.Background())
	if err != nil || join.Status != "completed" || join.Completed != 2 {
		t.Fatalf("join=%#v err=%v", join, err)
	}
}

func TestWaveSchedulerTerminalizesPermanentTicketRevocation(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority := authorityFor(plan, a)
	scheduler, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"), authority)
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
		t.Fatal(err)
	}
	authority.revoke(a.TicketID)
	if err := scheduler.Record(context.Background(), completed(a)); err != nil {
		t.Fatalf("revocation was not terminalized: %v", err)
	}
	if scheduler.Status() != ScheduleFailed {
		t.Fatalf("revoked outcome did not fail schedule: %s", scheduler.Status())
	}
	join, err := scheduler.Join(context.Background())
	if err != nil || join.Status != "failed" || join.Failed != 1 || join.Tasks[0].Failure != "native ticket authority revoked" {
		t.Fatalf("join=%#v err=%v", join, err)
	}
}

func TestWaveSchedulerTerminalizesExpiredTicket(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority := authorityFor(plan, a)
	scheduler, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"), authority)
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
		t.Fatal(err)
	}
	authority.expire(a.TicketID)
	if err := scheduler.Record(context.Background(), completed(a)); err != nil || scheduler.Status() != ScheduleFailed {
		t.Fatalf("expiration err=%v status=%s", err, scheduler.Status())
	}
	join, err := scheduler.Join(context.Background())
	if err != nil || join.Tasks[0].Failure != "native ticket authority expired" {
		t.Fatalf("join=%#v err=%v", join, err)
	}
}

func TestWaveSchedulerKeepsTransientAuthorityFailureRetryable(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority := authorityFor(plan, a)
	scheduler, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"), authority)
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
		t.Fatal(err)
	}
	authority.setTerminalUnavailable(true)
	if err := scheduler.Record(context.Background(), completed(a)); !errors.Is(err, ErrAuthorityUnavailable) || scheduler.Status() != ScheduleRunning {
		t.Fatalf("transient failure err=%v status=%s", err, scheduler.Status())
	}
	authority.setTerminalUnavailable(false)
	if err := scheduler.Record(context.Background(), completed(a)); err != nil || scheduler.Status() != ScheduleCompleted {
		t.Fatalf("retry err=%v status=%s", err, scheduler.Status())
	}
}

func TestWaveSchedulerReplaysAdmissionAfterCommittedResponseLoss(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"), schedulerRead("task-b"))
	a, b := binding("task-a", "ses-child-a"), binding("task-b", "ses-child-b")
	authority := authorityFor(plan, a, b)
	authority.admitCommitThenError = true
	scheduler, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"), authority)
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a, b}, successfulDispatch); !errors.Is(err, ErrAuthorityUnavailable) || scheduler.Status() != SchedulePending {
		t.Fatalf("lost admission response err=%v status=%s", err, scheduler.Status())
	}
	if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{b, a}, successfulDispatch); err != nil || scheduler.Status() != ScheduleRunning {
		t.Fatalf("idempotent admission replay err=%v status=%s", err, scheduler.Status())
	}
}

func TestWaveSchedulerReconstructsPreparedAdmissionAfterAuthorityInterruption(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority := authorityFor(plan, a)
	authority.admitCrashAfterPrepared = true
	scheduler, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"), authority)
	if err != nil {
		t.Fatal(err)
	}
	dispatches := 0
	dispatch := func(ctx context.Context, binding NativeTaskBinding) NativeDispatchResult {
		dispatches++
		return successfulDispatch(ctx, binding)
	}
	if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, dispatch); !errors.Is(err, ErrAuthorityUnavailable) || scheduler.Status() != SchedulePending {
		t.Fatalf("prepared interruption err=%v status=%s", err, scheduler.Status())
	}
	if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, dispatch); !errors.Is(err, ErrNativeDispatch) || scheduler.Status() != ScheduleFailed {
		t.Fatalf("prepared replay err=%v status=%s", err, scheduler.Status())
	}
	if dispatches != 0 {
		t.Fatalf("prepared uncertain admission redispatched %d times", dispatches)
	}
	join, err := scheduler.Join(context.Background())
	if err != nil || join.Tasks[0].DispatchStatus != NativeDispatchUncertain {
		t.Fatalf("join=%#v err=%v", join, err)
	}
}

func TestWaveSchedulerFailsUncertainNativeDispatchClosedWithoutRepeat(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority := authorityFor(plan, a)
	scheduler, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"), authority)
	if err != nil {
		t.Fatal(err)
	}
	dispatchCount := 0
	uncertainDispatch := func(context.Context, NativeTaskBinding) NativeDispatchResult {
		dispatchCount++
		return NativeDispatchResult{Status: NativeDispatchUncertain, Failure: "native dispatch response lost"}
	}
	if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, uncertainDispatch); !errors.Is(err, ErrNativeDispatch) || scheduler.Status() != ScheduleFailed {
		t.Fatalf("uncertain dispatch err=%v", err)
	}
	join, err := scheduler.Join(context.Background())
	if err != nil || join.Failed != 1 || join.Tasks[0].Failure != "native dispatch response lost" {
		t.Fatalf("join=%#v err=%v", join, err)
	}
	if dispatchCount != 1 {
		t.Fatalf("uncertain native dispatch repeated %d times", dispatchCount)
	}
}

func TestWaveSchedulerPreservesConfirmedTasksAfterPartialDispatchFailure(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"), schedulerRead("task-b"))
	a, b := binding("task-a", "ses-child-a"), binding("task-b", "ses-child-b")
	scheduler := newScheduler(t, plan, a, b)
	dispatch := func(_ context.Context, item NativeTaskBinding) NativeDispatchResult {
		if item.TaskID == b.TaskID {
			return NativeDispatchResult{Status: NativeDispatchNotStarted, Failure: "native adapter rejected create before send"}
		}
		return NativeDispatchResult{Status: NativeDispatchConfirmed}
	}
	if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a, b}, dispatch); !errors.Is(err, ErrNativeDispatch) || scheduler.Status() != ScheduleRunning {
		t.Fatalf("partial dispatch err=%v status=%s", err, scheduler.Status())
	}
	if err := scheduler.Record(context.Background(), completed(a)); err != nil || scheduler.Status() != ScheduleFailed {
		t.Fatalf("confirmed task terminal err=%v status=%s", err, scheduler.Status())
	}
	join, err := scheduler.Join(context.Background())
	if err != nil || join.Completed != 1 || join.Failed != 1 || join.Status != "partial" {
		t.Fatalf("join=%#v err=%v", join, err)
	}
}

func TestWaveSchedulerExposesUncertainDispatchClassification(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	scheduler := newScheduler(t, plan, a)
	if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, func(context.Context, NativeTaskBinding) NativeDispatchResult {
		return NativeDispatchResult{Status: NativeDispatchUncertain, Failure: "native create response was lost"}
	}); !errors.Is(err, ErrNativeDispatch) {
		t.Fatalf("uncertain dispatch err=%v", err)
	}
	join, err := scheduler.Join(context.Background())
	if err != nil || join.Tasks[0].DispatchStatus != NativeDispatchUncertain {
		t.Fatalf("join=%#v err=%v", join, err)
	}
}

func TestWaveSchedulerBoundsHungDispatchAndFencesTakeoverUntilReturn(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority := authorityFor(plan, a)
	authority.dispatchTimeout = 20 * time.Millisecond
	factory := schedulerFactory(t, authority)
	old, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-old"))
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	dispatchReturned := make(chan struct{})
	start := time.Now()
	err = old.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, func(context.Context, NativeTaskBinding) NativeDispatchResult {
		defer close(dispatchReturned)
		<-release
		return NativeDispatchResult{Status: NativeDispatchConfirmed}
	})
	if !errors.Is(err, ErrNativeDispatch) || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("hung dispatch err=%v elapsed=%s", err, time.Since(start))
	}
	if _, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-current")); !errors.Is(err, ErrAuthorityUnavailable) {
		t.Fatalf("takeover admitted while callback remained live: %v", err)
	}
	close(release)
	select {
	case <-dispatchReturned:
	case <-time.After(time.Second):
		t.Fatal("test dispatch goroutine did not exit")
	}
	var current *WaveScheduler
	deadline := time.Now().Add(time.Second)
	for err != nil && time.Now().Before(deadline) {
		current, err = factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-current"))
		if err != nil {
			time.Sleep(time.Millisecond)
		}
	}
	if err != nil || current.Status() != ScheduleFailed {
		t.Fatalf("takeover status=%v err=%v", current.Status(), err)
	}
	join, err := current.Join(context.Background())
	if err != nil || join.Tasks[0].DispatchStatus != NativeDispatchUncertain {
		t.Fatalf("join=%#v err=%v", join, err)
	}
}

func TestWaveSchedulerRequiresAuthorityBoundInitialCheckpoint(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority := &emptyCheckpointAuthority{delegate: authorityFor(plan, a)}
	if _, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"), authority); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("unbound initial checkpoint accepted: %v", err)
	}
}

func TestWaveSchedulerReplaysCommittedScheduleClaimWithoutMintingNewEpoch(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority := authorityFor(plan, a)
	authority.claimCommitThenError = true
	factory := schedulerFactory(t, authority)
	identity := scheduleIdentity("schedule-1", "owner-recovery")
	if _, err := factory.Open(context.Background(), plan, plan.PlanID, identity); !errors.Is(err, ErrAuthorityUnavailable) {
		t.Fatalf("lost claim response err=%v", err)
	}
	scheduler, err := factory.Open(context.Background(), plan, plan.PlanID, identity)
	if err != nil {
		t.Fatal(err)
	}
	if scheduler.scheduleEpoch != 1 {
		t.Fatalf("claim replay minted epoch %d, want 1", scheduler.scheduleEpoch)
	}
	if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerFactoryReturnsOneLiveHandleForSameOwner(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority := authorityFor(plan, a)
	factory := schedulerFactory(t, authority)
	identity := scheduleIdentity("schedule-1", "owner-one")

	const callers = 24
	results := make(chan *WaveScheduler, callers)
	errorsFound := make(chan error, callers)
	var group sync.WaitGroup
	for range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			scheduler, err := factory.Open(context.Background(), plan, plan.PlanID, identity)
			results <- scheduler
			errorsFound <- err
		}()
	}
	group.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first *WaveScheduler
	for scheduler := range results {
		if first == nil {
			first = scheduler
		} else if scheduler != first {
			t.Fatal("same owner produced multiple live scheduler handles")
		}
	}
	if authority.claimCalls != 1 {
		t.Fatalf("authority claims=%d want=1", authority.claimCalls)
	}
	dispatchCount := 0
	dispatch := func(ctx context.Context, _ NativeTaskBinding) NativeDispatchResult {
		dispatchCount++
		return successfulDispatch(ctx, a)
	}
	if err := first.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, dispatch); err != nil {
		t.Fatal(err)
	}
	if err := first.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, dispatch); !errors.Is(err, ErrScheduleState) {
		t.Fatalf("duplicate handle admitted work twice: %v", err)
	}
	if dispatchCount != 1 {
		t.Fatalf("dispatch count=%d want=1", dispatchCount)
	}
}

func TestSchedulerFactoryWaiterRetriesAfterLeaderContextCancellation(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority := &cancelFirstAcquireAuthority{delegate: authorityFor(plan, a), started: make(chan struct{})}
	factory := schedulerFactory(t, authority)
	identity := scheduleIdentity("schedule-1", "owner-one")
	leaderContext, cancelLeader := context.WithCancel(context.Background())
	leaderDone := make(chan error, 1)
	go func() {
		_, err := factory.Open(leaderContext, plan, plan.PlanID, identity)
		leaderDone <- err
	}()
	<-authority.started
	waiterDone := make(chan error, 1)
	go func() {
		_, err := factory.Open(context.Background(), plan, plan.PlanID, identity)
		waiterDone <- err
	}()
	cancelLeader()
	if err := <-leaderDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader err=%v", err)
	}
	select {
	case err := <-waiterDone:
		if err != nil {
			t.Fatalf("waiter inherited leader cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter did not retry after leader cancellation")
	}
}

func TestSchedulerFactoriesDoNotShareAuthorityByCallerSuppliedIdentity(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	firstAuthority := authorityFor(plan, binding("task-a", "ses-child-a"))
	secondAuthority := authorityFor(plan, binding("task-a", "ses-child-b"))
	firstFactory, err := NewSchedulerFactory(firstAuthority)
	if err != nil {
		t.Fatal(err)
	}
	secondFactory, err := NewSchedulerFactory(secondAuthority)
	if err != nil {
		t.Fatal(err)
	}
	if firstFactory == secondFactory || firstFactory.authority != firstAuthority || secondFactory.authority != secondAuthority {
		t.Fatal("factories substituted a caller's authority")
	}
}

func TestAuthorityLogicalSlotRejectsAlternateBindingAcrossFactories(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	b := binding("task-a", "ses-child-b")
	b.TicketID = "ticket-task-a-alternate"
	authority := authorityFor(plan, a, b)
	firstFactory, secondFactory := schedulerFactory(t, authority), schedulerFactory(t, authority)
	identity := scheduleIdentity("schedule-1", "owner-one")
	first, err := firstFactory.Open(context.Background(), plan, plan.PlanID, identity)
	if err != nil {
		t.Fatal(err)
	}
	second, err := secondFactory.Open(context.Background(), plan, plan.PlanID, identity)
	if err != nil {
		t.Fatal(err)
	}
	firstDispatches, secondDispatches := 0, 0
	if err := first.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, func(ctx context.Context, binding NativeTaskBinding) NativeDispatchResult {
		firstDispatches++
		return successfulDispatch(ctx, binding)
	}); err != nil {
		t.Fatal(err)
	}
	if err := second.StartWave(context.Background(), "wave-1", []NativeTaskBinding{b}, func(ctx context.Context, binding NativeTaskBinding) NativeDispatchResult {
		secondDispatches++
		return successfulDispatch(ctx, binding)
	}); !errors.Is(err, ErrInvalidSchedule) || second.Status() != SchedulePending {
		t.Fatalf("alternate logical slot binding err=%v status=%s", err, second.Status())
	}
	if firstDispatches != 1 || secondDispatches != 0 {
		t.Fatalf("dispatches first=%d second=%d", firstDispatches, secondDispatches)
	}
}

func TestWaveSchedulerTakeoverAdoptsCommittedAdmission(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority := authorityFor(plan, a)
	authority.admitCommitThenError = true
	factory := schedulerFactory(t, authority)
	old, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-old"))
	if err != nil {
		t.Fatal(err)
	}
	if err := old.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); !errors.Is(err, ErrAuthorityUnavailable) {
		t.Fatalf("lost admission response err=%v", err)
	}
	current, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-current"))
	if err != nil {
		t.Fatal(err)
	}
	if current.Status() != ScheduleRunning || current.currentWave != 0 || current.tasks[a.TaskID].Status != TaskRunning {
		t.Fatalf("checkpoint was not adopted: status=%s wave=%d task=%#v", current.Status(), current.currentWave, current.tasks[a.TaskID])
	}
	if err := current.Record(context.Background(), completed(a)); err != nil || current.Status() != ScheduleCompleted {
		t.Fatalf("adopted task completion err=%v status=%s", err, current.Status())
	}
}

func TestWaveSchedulerTakeoverAdoptsCommittedTerminal(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority := authorityFor(plan, a)
	factory := schedulerFactory(t, authority)
	old, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-old"))
	if err != nil {
		t.Fatal(err)
	}
	if err := old.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
		t.Fatal(err)
	}
	authority.terminalCommitThenError = true
	if err := old.Record(context.Background(), completed(a)); !errors.Is(err, ErrAuthorityUnavailable) {
		t.Fatalf("lost terminal response err=%v", err)
	}
	current, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-current"))
	if err != nil {
		t.Fatal(err)
	}
	if current.Status() != ScheduleCompleted {
		t.Fatalf("terminal checkpoint status=%s", current.Status())
	}
	join, err := current.Join(context.Background())
	if err != nil || join.Completed != 1 || join.Tasks[0].ResultDigest == "" {
		t.Fatalf("join=%#v err=%v", join, err)
	}
}

func TestWaveSchedulerTakeoverAdvancesFromCompletedCheckpointWave(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"), schedulerRead("task-b", "task-a"))
	a, b := binding("task-a", "ses-child-a"), binding("task-b", "ses-child-b")
	authority := authorityFor(plan, a, b)
	factory := schedulerFactory(t, authority)
	old, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-old"))
	if err != nil {
		t.Fatal(err)
	}
	if err := old.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
		t.Fatal(err)
	}
	if err := old.Record(context.Background(), completed(a)); err != nil || old.Status() != SchedulePending {
		t.Fatalf("first wave err=%v status=%s", err, old.Status())
	}
	current, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-current"))
	if err != nil || current.Status() != SchedulePending || current.currentWave != 1 {
		t.Fatalf("checkpoint status=%v wave=%d err=%v", current.Status(), current.currentWave, err)
	}
	if err := current.StartWave(context.Background(), "wave-2", []NativeTaskBinding{b}, successfulDispatch); err != nil {
		t.Fatal(err)
	}
	if err := current.Record(context.Background(), completed(b)); err != nil || current.Status() != ScheduleCompleted {
		t.Fatalf("second wave err=%v status=%s", err, current.Status())
	}
}

func TestWaveSchedulerRejectsDependentDispatchAfterPrerequisiteInvalidation(t *testing.T) {
	for name, invalidate := range map[string]func(*testTicketAuthority, string){
		"revoked": func(authority *testTicketAuthority, ticket string) { authority.revoke(ticket) },
		"expired": func(authority *testTicketAuthority, ticket string) { authority.expire(ticket) },
	} {
		t.Run(name, func(t *testing.T) {
			plan := schedulerPlan(t, schedulerRead("task-a"), schedulerRead("task-b", "task-a"))
			a, b := binding("task-a", "ses-child-a"), binding("task-b", "ses-child-b")
			authority := authorityFor(plan, a, b)
			scheduler, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"), authority)
			if err != nil {
				t.Fatal(err)
			}
			if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
				t.Fatal(err)
			}
			if err := scheduler.Record(context.Background(), completed(a)); err != nil || scheduler.Status() != SchedulePending {
				t.Fatalf("first wave err=%v status=%s", err, scheduler.Status())
			}
			invalidate(authority, a.TicketID)
			dispatches := 0
			err = scheduler.StartWave(context.Background(), "wave-2", []NativeTaskBinding{b}, func(ctx context.Context, binding NativeTaskBinding) NativeDispatchResult {
				dispatches++
				return successfulDispatch(ctx, binding)
			})
			if !errors.Is(err, ErrInvalidSchedule) || scheduler.Status() != ScheduleFailed || dispatches != 0 {
				t.Fatalf("dependent admission err=%v status=%s dispatches=%d", err, scheduler.Status(), dispatches)
			}
			join, err := scheduler.Join(context.Background())
			if err != nil || join.Failed != 1 || join.Completed != 0 || join.Tasks[0].Failure != "native ticket authority "+name {
				t.Fatalf("join=%#v err=%v", join, err)
			}
		})
	}
}

func TestWaveSchedulerTakeoverPreservesPartiallyTerminalWave(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"), schedulerRead("task-b"))
	a, b := binding("task-a", "ses-child-a"), binding("task-b", "ses-child-b")
	authority := authorityFor(plan, a, b)
	factory := schedulerFactory(t, authority)
	old, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-old"))
	if err != nil {
		t.Fatal(err)
	}
	if err := old.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a, b}, successfulDispatch); err != nil {
		t.Fatal(err)
	}
	failed := NativeTaskOutcome{NativeTaskBinding: a, Status: TaskFailed, Failure: "provider unavailable"}
	if err := old.Record(context.Background(), failed); err != nil || old.Status() != ScheduleRunning {
		t.Fatalf("partial terminal err=%v status=%s", err, old.Status())
	}
	current, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-current"))
	if err != nil || current.Status() != ScheduleRunning || current.tasks[a.TaskID].Status != TaskFailed || current.tasks[b.TaskID].Status != TaskRunning {
		t.Fatalf("checkpoint status=%v a=%#v b=%#v err=%v", current.Status(), current.tasks[a.TaskID], current.tasks[b.TaskID], err)
	}
	if err := current.Record(context.Background(), completed(b)); err != nil || current.Status() != ScheduleFailed {
		t.Fatalf("remaining terminal err=%v status=%s", err, current.Status())
	}
	join, err := current.Join(context.Background())
	if err != nil || join.Status != "partial" || join.Completed != 1 || join.Failed != 1 {
		t.Fatalf("join=%#v err=%v", join, err)
	}
}

func TestWaveSchedulerDispatchLinearizesBeforeTakeover(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority := authorityFor(plan, a)
	authority.admitResponseCommitted = make(chan struct{})
	authority.admitResponseRelease = make(chan struct{})
	factory := schedulerFactory(t, authority)
	old, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-old"))
	if err != nil {
		t.Fatal(err)
	}
	dispatched := make(chan struct{}, 2)
	dispatch := func(ctx context.Context, _ NativeTaskBinding) NativeDispatchResult {
		dispatched <- struct{}{}
		return successfulDispatch(ctx, a)
	}
	startDone := make(chan error, 1)
	go func() { startDone <- old.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, dispatch) }()
	<-authority.admitResponseCommitted
	current, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-current"))
	if err != nil || current.Status() != ScheduleRunning {
		t.Fatalf("takeover scheduler status=%v err=%v", current.Status(), err)
	}
	close(authority.admitResponseRelease)
	if err := <-startDone; !errors.Is(err, ErrScheduleState) {
		t.Fatalf("superseded delayed response err=%v", err)
	}
	if len(dispatched) != 1 {
		t.Fatalf("native dispatch count=%d want=1", len(dispatched))
	}
	if err := current.Record(context.Background(), completed(a)); err != nil {
		t.Fatalf("current owner could not accept dispatched result: %v", err)
	}
}

func TestAuthorityDispatchCallbackCanReenterWithoutGlobalDeadlock(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority := authorityFor(plan, a)
	scheduler, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"), authority)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, func(ctx context.Context, item NativeTaskBinding) NativeDispatchResult {
			verdict, err := authority.ValidateLease(ctx, scheduler.leaseRef())
			if err != nil || verdict != NativeTicketAccepted {
				return NativeDispatchResult{Status: NativeDispatchNotStarted, Failure: "lease validation failed during dispatch"}
			}
			return successfulDispatch(ctx, item)
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("dispatch callback deadlocked while re-entering authority")
	}
}

func TestWaveSchedulerDoesNotReactivateSupersededClaimReplay(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority := authorityFor(plan, a)
	authority.claimCommitThenError = true
	factory := schedulerFactory(t, authority)
	oldIdentity := scheduleIdentity("schedule-1", "owner-old")
	if _, err := factory.Open(context.Background(), plan, plan.PlanID, oldIdentity); !errors.Is(err, ErrAuthorityUnavailable) {
		t.Fatalf("lost claim response err=%v", err)
	}
	current, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-current"))
	if err != nil || current.scheduleEpoch != 2 {
		t.Fatalf("takeover scheduler=%#v err=%v", current, err)
	}
	if _, err := factory.Open(context.Background(), plan, plan.PlanID, oldIdentity); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("superseded claim replay reactivated: %v", err)
	}
}

func TestSchedulerFactoryRejectsLateLowerEpochResponse(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	delegate := authorityFor(plan, a)
	authority := &delayedAcquireAuthority{
		NativeTicketAuthority: delegate, owner: "owner-old", committed: make(chan struct{}), release: make(chan struct{}),
	}
	factory := schedulerFactory(t, authority)
	oldResult := make(chan error, 1)
	go func() {
		_, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-old"))
		oldResult <- err
	}()
	<-authority.committed
	current, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-current"))
	if err != nil || current.scheduleEpoch != 2 {
		t.Fatalf("current scheduler=%#v err=%v", current, err)
	}
	close(authority.release)
	if err := <-oldResult; !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("late lower epoch response became current: %v", err)
	}
	if err := current.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
		t.Fatalf("higher epoch scheduler was superseded: %v", err)
	}
}

func TestWaveSchedulerFencesSupersededScheduleOwner(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority := authorityFor(plan, a)
	factory := schedulerFactory(t, authority)
	first, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-2"))
	if err != nil {
		t.Fatal(err)
	}
	if first.scheduleEpoch != 1 || second.scheduleEpoch != 2 {
		t.Fatalf("epochs first=%d second=%d", first.scheduleEpoch, second.scheduleEpoch)
	}
	if err := first.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); !errors.Is(err, ErrScheduleState) {
		t.Fatalf("superseded owner admitted work: %v", err)
	}
	if err := second.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
		t.Fatalf("current owner could not admit work: %v", err)
	}
}

func TestWaveSchedulerFencesTerminalReplayFromSupersededOwner(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority := authorityFor(plan, a)
	factory := schedulerFactory(t, authority)
	first, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := first.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-2")); err != nil {
		t.Fatal(err)
	}
	if err := first.Record(context.Background(), completed(a)); !errors.Is(err, ErrScheduleState) || first.Status() != ScheduleFailed {
		t.Fatalf("stale terminal was not fenced err=%v status=%s", err, first.Status())
	}
	join, err := first.Join(context.Background())
	if !errors.Is(err, ErrScheduleState) || len(join.Tasks) != 0 {
		t.Fatalf("join=%#v err=%v", join, err)
	}
}

func TestWaveSchedulerJoinLinearizesAtomicallyBeforeCrossFactoryTakeover(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority := authorityFor(plan, a)
	authority.joinLinearizing = make(chan struct{})
	authority.joinRelease = make(chan struct{})
	oldFactory, takeoverFactory := schedulerFactory(t, authority), schedulerFactory(t, authority)
	old, err := oldFactory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-old"))
	if err != nil {
		t.Fatal(err)
	}
	if err := old.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
		t.Fatal(err)
	}
	if err := old.Record(context.Background(), completed(a)); err != nil {
		t.Fatal(err)
	}
	joinDone := make(chan error, 1)
	go func() {
		_, err := old.Join(context.Background())
		joinDone <- err
	}()
	<-authority.joinLinearizing
	takeoverDone := make(chan error, 1)
	go func() {
		_, err := takeoverFactory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-current"))
		takeoverDone <- err
	}()
	select {
	case err := <-takeoverDone:
		t.Fatalf("takeover crossed atomic join boundary: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(authority.joinRelease)
	if err := <-joinDone; err != nil {
		t.Fatalf("linearized join err=%v", err)
	}
	if err := <-takeoverDone; err != nil {
		t.Fatalf("takeover err=%v", err)
	}
	if _, err := old.Join(context.Background()); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("stale cross-factory join published: %v", err)
	}
}

func TestWaveSchedulerRejectsMismatchedAndDetachesAcceptedJoin(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	scheduler := newScheduler(t, plan, a)
	authority := scheduler.authority.(*testTicketAuthority)
	authority.joinResponseMutator = func(result JoinResult) JoinResult {
		result.Tasks[0].TaskID = "task-forged"
		return result
	}
	if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Record(context.Background(), completed(a)); err != nil {
		t.Fatal(err)
	}
	if _, err := scheduler.Join(context.Background()); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("mismatched authority join accepted: %v", err)
	}
	authority.joinResponseMutator = nil
	first, err := scheduler.Join(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first.Tasks[0].TaskID = "task-forged"
	second, err := scheduler.Join(context.Background())
	if err != nil || second.Tasks[0].TaskID != a.TaskID {
		t.Fatalf("authority replay was externally mutated: join=%#v err=%v", second, err)
	}
}

func TestWaveSchedulerJoinRejectsTerminalInvalidatedAfterLocalCompletion(t *testing.T) {
	for name, invalidate := range map[string]func(*testTicketAuthority, string){
		"revoked": func(authority *testTicketAuthority, ticket string) { authority.revoke(ticket) },
		"expired": func(authority *testTicketAuthority, ticket string) { authority.expire(ticket) },
	} {
		t.Run(name, func(t *testing.T) {
			plan := schedulerPlan(t, schedulerRead("task-a"))
			a := binding("task-a", "ses-child-a")
			authority := authorityFor(plan, a)
			scheduler, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"), authority)
			if err != nil {
				t.Fatal(err)
			}
			if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
				t.Fatal(err)
			}
			if err := scheduler.Record(context.Background(), completed(a)); err != nil {
				t.Fatal(err)
			}
			invalidate(authority, a.TicketID)
			if _, err := scheduler.Join(context.Background()); !errors.Is(err, ErrInvalidSchedule) {
				t.Fatalf("invalidated terminal join err=%v", err)
			}
		})
	}
}

func TestWaveSchedulerRechecksTicketValidityBeforeAdmissionReplay(t *testing.T) {
	for name, invalidate := range map[string]func(*testTicketAuthority, string){
		"revoked": func(authority *testTicketAuthority, ticket string) { authority.revoke(ticket) },
		"expired": func(authority *testTicketAuthority, ticket string) { authority.expire(ticket) },
	} {
		t.Run(name, func(t *testing.T) {
			plan := schedulerPlan(t, schedulerRead("task-a"))
			a := binding("task-a", "ses-child-a")
			authority := authorityFor(plan, a)
			authority.admitCommitThenError = true
			scheduler, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"), authority)
			if err != nil {
				t.Fatal(err)
			}
			if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); !errors.Is(err, ErrAuthorityUnavailable) {
				t.Fatalf("lost admission response err=%v", err)
			}
			invalidate(authority, a.TicketID)
			if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); !errors.Is(err, ErrInvalidSchedule) || scheduler.Status() != ScheduleFailed {
				t.Fatalf("invalid ticket replay err=%v status=%s", err, scheduler.Status())
			}
			if _, err := scheduler.Join(context.Background()); err != nil {
				t.Fatalf("permanent rejection was not joinable: %v", err)
			}
		})
	}
}

func TestWaveSchedulerRechecksTicketValidityBeforeTerminalReplay(t *testing.T) {
	for name, invalidate := range map[string]func(*testTicketAuthority, string){
		"revoked": func(authority *testTicketAuthority, ticket string) { authority.revoke(ticket) },
		"expired": func(authority *testTicketAuthority, ticket string) { authority.expire(ticket) },
	} {
		t.Run(name, func(t *testing.T) {
			plan := schedulerPlan(t, schedulerRead("task-a"))
			a := binding("task-a", "ses-child-a")
			authority := authorityFor(plan, a)
			scheduler, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"), authority)
			if err != nil {
				t.Fatal(err)
			}
			if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
				t.Fatal(err)
			}
			authority.terminalCommitThenError = true
			if err := scheduler.Record(context.Background(), completed(a)); !errors.Is(err, ErrAuthorityUnavailable) {
				t.Fatalf("lost terminal response err=%v", err)
			}
			invalidate(authority, a.TicketID)
			if err := scheduler.Record(context.Background(), completed(a)); err != nil || scheduler.Status() != ScheduleFailed {
				t.Fatalf("invalid terminal replay err=%v status=%s", err, scheduler.Status())
			}
			join, err := scheduler.Join(context.Background())
			if err != nil || join.Tasks[0].Failure != "native ticket authority "+name {
				t.Fatalf("join=%#v err=%v", join, err)
			}
		})
	}
}

func TestWaveSchedulerTakeoverCheckpointAppliesCurrentTerminalInvalidation(t *testing.T) {
	for name, invalidate := range map[string]func(*testTicketAuthority, string){
		"revoked": func(authority *testTicketAuthority, ticket string) { authority.revoke(ticket) },
		"expired": func(authority *testTicketAuthority, ticket string) { authority.expire(ticket) },
	} {
		t.Run(name, func(t *testing.T) {
			plan := schedulerPlan(t, schedulerRead("task-a"))
			a := binding("task-a", "ses-child-a")
			authority := authorityFor(plan, a)
			factory := schedulerFactory(t, authority)
			old, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-old"))
			if err != nil {
				t.Fatal(err)
			}
			if err := old.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
				t.Fatal(err)
			}
			authority.terminalCommitThenError = true
			if err := old.Record(context.Background(), completed(a)); !errors.Is(err, ErrAuthorityUnavailable) {
				t.Fatalf("lost terminal response err=%v", err)
			}
			invalidate(authority, a.TicketID)
			current, err := factory.Open(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-current"))
			if err != nil || current.Status() != ScheduleFailed {
				t.Fatalf("takeover status=%v err=%v", current.Status(), err)
			}
			join, err := current.Join(context.Background())
			if err != nil || join.Completed != 0 || join.Failed != 1 || join.Tasks[0].Failure != "native ticket authority "+name {
				t.Fatalf("join=%#v err=%v", join, err)
			}
		})
	}
}

func TestAuthorityIdempotencyKeysBindScheduleOwnerAndEpoch(t *testing.T) {
	claimA := ScheduleClaim{ScheduleID: "schedule-1", PlanID: "plan-1", ParentSessionID: "ses-parent", OwnerID: "owner-1"}
	claimB := claimA
	claimB.OwnerID = "owner-2"
	keyA, err := scheduleClaimIdempotencyKey(claimA)
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := scheduleClaimIdempotencyKey(claimB)
	if err != nil || keyA == keyB {
		t.Fatalf("claim keys a=%q b=%q err=%v", keyA, keyB, err)
	}
	admissionA := WaveAdmission{
		ScheduleID: "schedule-1", OwnerID: "owner-1", ScheduleEpoch: 1, PlanID: "plan-1", WaveID: "wave-1",
		Bindings: []NativeTaskBinding{binding("task-a", "ses-child-a")},
	}
	admissionB := admissionA
	admissionB.ScheduleEpoch = 2
	keyA, err = waveAdmissionIdempotencyKey(admissionA)
	if err != nil {
		t.Fatal(err)
	}
	keyB, err = waveAdmissionIdempotencyKey(admissionB)
	if err != nil || keyA == keyB {
		t.Fatalf("admission keys a=%q b=%q err=%v", keyA, keyB, err)
	}
}

func TestWaveSchedulerReplaysTerminalAfterCommittedResponseLoss(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority := authorityFor(plan, a)
	scheduler, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"), authority)
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
		t.Fatal(err)
	}
	authority.terminalCommitThenError = true
	if err := scheduler.Record(context.Background(), completed(a)); !errors.Is(err, ErrAuthorityUnavailable) || scheduler.Status() != ScheduleRunning {
		t.Fatalf("lost terminal response err=%v status=%s", err, scheduler.Status())
	}
	if err := scheduler.Record(context.Background(), completed(a)); err != nil || scheduler.Status() != ScheduleCompleted {
		t.Fatalf("idempotent terminal replay err=%v status=%s", err, scheduler.Status())
	}
}

func TestWaveSchedulerPreventsTicketReplayAcrossPlansAndSchedulers(t *testing.T) {
	planA := schedulerPlan(t, schedulerRead("task-a"))
	taskB := schedulerRead("task-a")
	taskB.Goal = "Inspect an alternative boundary"
	planB := schedulerPlan(t, taskB)
	if planA.PlanID == planB.PlanID {
		t.Fatal("test plans unexpectedly share identity")
	}
	a := binding("task-a", "ses-child-a")
	authority := authorityFor(planA, a)
	authority.allow("schedule-2", planB, a)
	first, err := openScheduler(context.Background(), planA, planA.PlanID, scheduleIdentity("schedule-1", "owner-1"), authority)
	if err != nil {
		t.Fatal(err)
	}
	second, err := openScheduler(context.Background(), planB, planB.PlanID, scheduleIdentity("schedule-2", "owner-2"), authority)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
		t.Fatal(err)
	}
	if err := second.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("cross-plan ticket replay accepted: %v", err)
	}
}

func TestWaveSchedulerPreventsChildReplayWithDifferentTickets(t *testing.T) {
	planA := schedulerPlan(t, schedulerRead("task-a"))
	taskB := schedulerRead("task-a")
	taskB.Goal = "Inspect another boundary"
	planB := schedulerPlan(t, taskB)
	a := binding("task-a", "ses-child-shared")
	b := a
	b.TicketID = "ticket-task-b"
	authority := authorityFor(planA, a)
	authority.allow("schedule-2", planB, b)
	first, err := openScheduler(context.Background(), planA, planA.PlanID, scheduleIdentity("schedule-1", "owner-1"), authority)
	if err != nil {
		t.Fatal(err)
	}
	second, err := openScheduler(context.Background(), planB, planB.PlanID, scheduleIdentity("schedule-2", "owner-2"), authority)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
		t.Fatal(err)
	}
	if err := second.StartWave(context.Background(), "wave-1", []NativeTaskBinding{b}, successfulDispatch); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("same child with different ticket replay accepted: %v", err)
	}
}

func TestWaveSchedulerDoesNotHoldMutexDuringAuthorityCall(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority := &reentrantAuthority{delegate: authorityFor(plan, a)}
	scheduler, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"), authority)
	if err != nil {
		t.Fatal(err)
	}
	authority.onAdmit = func() { _ = scheduler.Status() }
	done := make(chan error, 1)
	go func() {
		done <- scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("authority callback deadlocked on scheduler mutex")
	}
}

func TestWaveSchedulerValidatesResultBytesAndFailureRuneBound(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	for name, outcome := range map[string]NativeTaskOutcome{
		"invalid json": {NativeTaskBinding: a, Status: TaskCompleted, MessageID: "msg-a", ResultID: "result-a", Result: json.RawMessage(`{`)},
		"oversized":    {NativeTaskBinding: a, Status: TaskCompleted, MessageID: "msg-a", ResultID: "result-a", Result: json.RawMessage(`"` + strings.Repeat("x", maxNativeResultBytes) + `"`)},
	} {
		t.Run(name, func(t *testing.T) {
			scheduler := newScheduler(t, plan, a)
			if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
				t.Fatal(err)
			}
			if err := scheduler.Record(context.Background(), outcome); !errors.Is(err, ErrInvalidSchedule) {
				t.Fatalf("invalid result accepted: %v", err)
			}
		})
	}

	scheduler := newScheduler(t, plan, a)
	if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Record(context.Background(), NativeTaskOutcome{NativeTaskBinding: a, Status: TaskFailed, Failure: strings.Repeat("€", 2048)}); err != nil {
		t.Fatalf("2048-rune failure rejected: %v", err)
	}

	scheduler = newScheduler(t, plan, a)
	if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, successfulDispatch); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Record(context.Background(), NativeTaskOutcome{NativeTaskBinding: a, Status: TaskFailed, Failure: strings.Repeat("€", 2049)}); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("2049-rune failure accepted: %v", err)
	}
}

func TestWaveSchedulerAcceptsConcurrentNativeCompletionsSafely(t *testing.T) {
	plan := schedulerPlan(t, schedulerRead("task-a"), schedulerRead("task-b"), schedulerRead("task-c"), schedulerRead("task-d"))
	bindings := []NativeTaskBinding{
		binding("task-a", "ses-child-a"), binding("task-b", "ses-child-b"),
		binding("task-c", "ses-child-c"), binding("task-d", "ses-child-d"),
	}
	scheduler := newScheduler(t, plan, bindings...)
	if err := scheduler.StartWave(context.Background(), "wave-1", bindings, successfulDispatch); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	errorsFound := make(chan error, len(bindings))
	for _, item := range bindings {
		item := item
		group.Add(1)
		go func() {
			defer group.Done()
			errorsFound <- scheduler.Record(context.Background(), completed(item))
		}()
	}
	group.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	if scheduler.Status() != ScheduleCompleted {
		t.Fatalf("status=%s", scheduler.Status())
	}
}
