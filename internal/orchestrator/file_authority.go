package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vgxness/vgxness/internal/navigator"
)

const (
	durableAuthorityVersion  = 1
	durableAuthorityMaxBytes = 32 << 20
)

// DurableTicketAuthority is the production, cross-process implementation of
// NativeTicketAuthority. Every state transition is serialized through one
// workspace-owned lock and published by an fsync-backed atomic replacement.
type DurableTicketAuthority struct {
	directory       string
	statePath       string
	lockPath        string
	dispatchTimeout time.Duration
}

type durableAuthorityState struct {
	Version          int                             `json:"version"`
	Schedules        map[string]durableSchedule      `json:"schedules"`
	ClaimReplay      map[string]ScheduleLease        `json:"claimReplay"`
	Allowed          map[string]durableTicketUse     `json:"allowed"`
	AdmittedTickets  map[string]durableTicketUse     `json:"admittedTickets"`
	AdmittedChildren map[string]durableTicketUse     `json:"admittedChildren"`
	LogicalSlots     map[string]durableTicketUse     `json:"logicalSlots"`
	DispatchResults  map[string]NativeDispatchResult `json:"dispatchResults"`
	AdmissionReplay  map[string]WaveAdmissionResult  `json:"admissionReplay"`
	TerminalTickets  map[string]string               `json:"terminalTickets"`
	TerminalOutcomes map[string]NativeTaskOutcome    `json:"terminalOutcomes"`
	TerminalReplay   map[string]NativeTicketVerdict  `json:"terminalReplay"`
	JoinReplay       map[string]JoinResult           `json:"joinReplay"`
	Revoked          map[string]bool                 `json:"revoked"`
	Expired          map[string]bool                 `json:"expired"`
	ActiveCallbacks  map[string]map[string]bool      `json:"activeCallbacks"`
}

type durableSchedule struct {
	Plan            navigator.Plan `json:"plan"`
	Lease           ScheduleLease  `json:"lease"`
	CurrentClaimKey string         `json:"currentClaimKey,omitempty"`
	NextEpoch       uint64         `json:"nextEpoch"`
}

type durableTicketUse struct {
	ScheduleID      string `json:"scheduleId"`
	PlanID          string `json:"planId"`
	WaveID          string `json:"waveId"`
	TaskID          string `json:"taskId"`
	ParentSessionID string `json:"parentSessionId"`
	ChildSessionID  string `json:"childSessionId"`
	TicketID        string `json:"ticketId"`
}

func NewDurableTicketAuthority(root string, dispatchTimeout time.Duration) (*DurableTicketAuthority, error) {
	if !filepath.IsAbs(root) || dispatchTimeout <= 0 {
		return nil, ErrInvalidSchedule
	}
	directory := filepath.Join(filepath.Clean(root), "delegation-authority")
	if err := ensurePrivateAuthorityDirectory(directory); err != nil {
		return nil, err
	}
	return &DurableTicketAuthority{
		directory: directory, statePath: filepath.Join(directory, "state.json"),
		lockPath: filepath.Join(directory, "state.lock"), dispatchTimeout: dispatchTimeout,
	}, nil
}

// RegisterPlan durably creates an exact plan registration and incrementally
// authorizes each wave's native bindings before that wave executes. Both the
// initial plan-only call and later binding calls are exact-replay safe.
func (authority *DurableTicketAuthority) RegisterPlan(ctx context.Context, scheduleID string, plan navigator.Plan, bindings []NativeTaskBinding) error {
	if authority == nil || !validIdentity(scheduleID) || navigator.ValidatePlan(ctx, plan) != nil {
		return ErrInvalidSchedule
	}
	uses := make([]durableTicketUse, len(bindings))
	seenTickets := make(map[string]struct{}, len(bindings))
	seenChildren := make(map[string]struct{}, len(bindings))
	seenTasks := make(map[string]struct{}, len(bindings))
	for index, binding := range bindings {
		use, ok := durableUseFor(scheduleID, plan, binding)
		if !ok || binding.ParentSessionID == "" {
			return ErrInvalidSchedule
		}
		if _, duplicate := seenTickets[binding.TicketID]; duplicate {
			return ErrInvalidSchedule
		}
		if _, duplicate := seenChildren[binding.ChildSessionID]; duplicate {
			return ErrInvalidSchedule
		}
		if _, duplicate := seenTasks[binding.TaskID]; duplicate {
			return ErrInvalidSchedule
		}
		seenTickets[binding.TicketID] = struct{}{}
		seenChildren[binding.ChildSessionID] = struct{}{}
		seenTasks[binding.TaskID] = struct{}{}
		uses[index] = use
	}
	return authority.transact(ctx, func(state *durableAuthorityState) (bool, error) {
		changed := false
		if existing, ok := state.Schedules[scheduleID]; ok {
			left, leftErr := json.Marshal(existing.Plan)
			right, rightErr := json.Marshal(plan)
			if leftErr != nil || rightErr != nil || !bytes.Equal(left, right) {
				return false, ErrInvalidSchedule
			}
		} else {
			state.Schedules[scheduleID] = durableSchedule{Plan: plan}
			changed = true
		}
		for _, use := range uses {
			key := durableUseKey(use)
			if existing, ok := state.Allowed[key]; ok {
				if existing != use {
					return false, ErrInvalidSchedule
				}
				continue
			}
			if existing, ok := state.AdmittedTickets[use.TicketID]; ok && existing != use {
				return false, ErrInvalidSchedule
			}
			if existing, ok := state.AdmittedChildren[use.ChildSessionID]; ok && existing != use {
				return false, ErrInvalidSchedule
			}
			for _, allowed := range state.Allowed {
				if allowed.TicketID == use.TicketID || allowed.ChildSessionID == use.ChildSessionID || durableSlotKey(allowed) == durableSlotKey(use) {
					if allowed != use {
						return false, ErrInvalidSchedule
					}
				}
			}
			state.Allowed[key] = use
			changed = true
		}
		return changed, nil
	})
}

func (authority *DurableTicketAuthority) AcquireSchedule(ctx context.Context, claim ScheduleClaim) (lease ScheduleLease, err error) {
	lease.Verdict = ScheduleLeaseUnavailable
	err = authority.transact(ctx, func(state *durableAuthorityState) (bool, error) {
		expectedKey, keyErr := scheduleClaimIdempotencyKey(claim)
		if keyErr != nil || claim.IdempotencyKey != expectedKey {
			lease = ScheduleLease{Verdict: ScheduleLeaseRejected}
			return false, nil
		}
		schedule, ok := state.Schedules[claim.ScheduleID]
		if !ok || schedule.Plan.PlanID != claim.PlanID {
			lease = ScheduleLease{Verdict: ScheduleLeaseRejected}
			return false, nil
		}
		if len(state.ActiveCallbacks[claim.ScheduleID]) != 0 {
			lease = ScheduleLease{Verdict: ScheduleLeaseUnavailable}
			return false, nil
		}
		if replay, ok := state.ClaimReplay[claim.IdempotencyKey]; ok {
			if schedule.CurrentClaimKey != claim.IdempotencyKey {
				lease = ScheduleLease{Verdict: ScheduleLeaseRejected}
				return false, nil
			}
			replay.Checkpoint = durableCheckpoint(state, claim.ScheduleID, claim.PlanID, claim.ParentSessionID)
			lease = replay
			return false, nil
		}
		if current := schedule.Lease; current.Verdict == ScheduleLeaseGranted {
			if current.PlanID != claim.PlanID || current.ParentSessionID != claim.ParentSessionID || current.OwnerID == claim.OwnerID {
				lease = ScheduleLease{Verdict: ScheduleLeaseRejected}
				return false, nil
			}
		}
		schedule.NextEpoch++
		lease = ScheduleLease{
			Verdict: ScheduleLeaseGranted, ScheduleID: claim.ScheduleID, PlanID: claim.PlanID,
			ParentSessionID: claim.ParentSessionID, OwnerID: claim.OwnerID, Epoch: schedule.NextEpoch,
			Checkpoint: durableCheckpoint(state, claim.ScheduleID, claim.PlanID, claim.ParentSessionID),
		}
		schedule.Lease = lease
		schedule.CurrentClaimKey = claim.IdempotencyKey
		state.Schedules[claim.ScheduleID] = schedule
		state.ClaimReplay[claim.IdempotencyKey] = lease
		return true, nil
	})
	return lease, err
}

func (authority *DurableTicketAuthority) ValidateLease(ctx context.Context, ref ScheduleLeaseRef) (verdict NativeTicketVerdict, err error) {
	verdict = NativeTicketUnavailable
	err = authority.transact(ctx, func(state *durableAuthorityState) (bool, error) {
		schedule, ok := state.Schedules[ref.ScheduleID]
		if !ok || !sameDurableLease(schedule.Lease, ref) {
			verdict = NativeTicketFenced
			return false, nil
		}
		verdict = NativeTicketAccepted
		return false, nil
	})
	return verdict, err
}

// Snapshot returns the current durable checkpoint for projection repair. It
// grants no execution authority and is safe to call after response loss.
func (authority *DurableTicketAuthority) Snapshot(ctx context.Context, scheduleID, planID, parentSessionID string) (checkpoint ScheduleCheckpoint, err error) {
	err = authority.transact(ctx, func(state *durableAuthorityState) (bool, error) {
		schedule, ok := state.Schedules[scheduleID]
		if !ok || schedule.Plan.PlanID != planID || schedule.Lease.Verdict == ScheduleLeaseGranted && schedule.Lease.ParentSessionID != parentSessionID {
			return false, ErrInvalidSchedule
		}
		checkpoint = durableCheckpoint(state, scheduleID, planID, parentSessionID)
		return false, nil
	})
	return checkpoint, err
}

func (authority *DurableTicketAuthority) AdmitWave(ctx context.Context, admission WaveAdmission, dispatch NativeTaskDispatch) (result WaveAdmissionResult, err error) {
	result.Verdict = NativeTicketUnavailable
	if dispatch == nil {
		return WaveAdmissionResult{Verdict: NativeTicketRevoked}, nil
	}
	callbackLocks := make(map[string]FileLock, len(admission.Bindings))
	defer func() {
		for _, lock := range callbackLocks {
			lock.Release()
		}
	}()
	for _, binding := range admission.Bindings {
		lock, lockErr := AcquireFileLock(filepath.Join(authority.directory, "callback-"+binding.TicketID+".lock"))
		if lockErr != nil {
			return WaveAdmissionResult{Verdict: NativeTicketUnavailable}, nil
		}
		callbackLocks[binding.TicketID] = lock
	}
	prepared := false
	err = authority.transact(ctx, func(state *durableAuthorityState) (bool, error) {
		expectedKey, keyErr := waveAdmissionIdempotencyKey(admission)
		if keyErr != nil || admission.IdempotencyKey != expectedKey {
			result = WaveAdmissionResult{Verdict: NativeTicketRevoked}
			return false, nil
		}
		schedule, ok := state.Schedules[admission.ScheduleID]
		if !ok || !sameAdmissionLease(schedule.Lease, admission) {
			result = WaveAdmissionResult{Verdict: NativeTicketFenced}
			return false, nil
		}
		expectedPrerequisites, prerequisitesOK := durablePrerequisites(schedule.Plan, admission.WaveID)
		if !prerequisitesOK || !slices.Equal(admission.PrerequisiteTaskIDs, expectedPrerequisites) {
			result = WaveAdmissionResult{Verdict: NativeTicketRevoked}
			return false, nil
		}
		checkpoint := durableCheckpoint(state, admission.ScheduleID, admission.PlanID, schedule.Lease.ParentSessionID)
		checkpointTasks := make(map[string]NativeTaskCheckpoint, len(checkpoint.Tasks))
		for _, item := range checkpoint.Tasks {
			checkpointTasks[item.TaskID] = item
		}
		for _, taskID := range admission.PrerequisiteTaskIDs {
			item, exists := checkpointTasks[taskID]
			if !exists || item.Status != TaskCompleted || item.DispatchStatus != NativeDispatchConfirmed {
				result = WaveAdmissionResult{Verdict: NativeTicketRevoked, Checkpoint: checkpoint}
				return false, nil
			}
		}
		for _, binding := range admission.Bindings {
			if binding.ParentSessionID != schedule.Lease.ParentSessionID {
				result = WaveAdmissionResult{Verdict: NativeTicketRevoked}
				return false, nil
			}
			if state.Revoked[binding.TicketID] || state.Expired[binding.TicketID] {
				durableRecordRejectedAdmission(state, admission, durableTicketFailure(state, binding.TicketID))
				result = WaveAdmissionResult{Verdict: NativeTicketRevoked, Checkpoint: durableCheckpoint(state, admission.ScheduleID, admission.PlanID, schedule.Lease.ParentSessionID)}
				if state.Expired[binding.TicketID] {
					result.Verdict = NativeTicketExpired
				}
				return true, nil
			}
		}
		if replay, ok := state.AdmissionReplay[admission.IdempotencyKey]; ok {
			replay.Checkpoint = durableCheckpoint(state, admission.ScheduleID, admission.PlanID, schedule.Lease.ParentSessionID)
			result = replay
			return false, nil
		}
		uses := make([]durableTicketUse, len(admission.Bindings))
		for index, binding := range admission.Bindings {
			use := durableUseFromAdmission(admission, binding)
			if allowed, ok := state.Allowed[durableUseKey(use)]; !ok || allowed != use {
				result = WaveAdmissionResult{Verdict: NativeTicketRevoked}
				return false, nil
			}
			if _, consumed := state.AdmittedTickets[use.TicketID]; consumed {
				result = WaveAdmissionResult{Verdict: NativeTicketRevoked}
				return false, nil
			}
			if _, consumed := state.AdmittedChildren[use.ChildSessionID]; consumed {
				result = WaveAdmissionResult{Verdict: NativeTicketRevoked}
				return false, nil
			}
			if _, consumed := state.LogicalSlots[durableSlotKey(use)]; consumed {
				result = WaveAdmissionResult{Verdict: NativeTicketRevoked}
				return false, nil
			}
			uses[index] = use
		}
		for _, use := range uses {
			state.AdmittedTickets[use.TicketID] = use
			state.AdmittedChildren[use.ChildSessionID] = use
			state.LogicalSlots[durableSlotKey(use)] = use
			state.DispatchResults[use.TicketID] = NativeDispatchResult{Status: NativeDispatchUncertain, Failure: "native dispatch interrupted before durable result"}
		}
		if state.ActiveCallbacks[admission.ScheduleID] == nil {
			state.ActiveCallbacks[admission.ScheduleID] = make(map[string]bool)
		}
		for _, use := range uses {
			state.ActiveCallbacks[admission.ScheduleID][use.TicketID] = true
		}
		result = WaveAdmissionResult{Verdict: NativeTicketAccepted, Checkpoint: durableCheckpoint(state, admission.ScheduleID, admission.PlanID, schedule.Lease.ParentSessionID)}
		state.AdmissionReplay[admission.IdempotencyKey] = result
		prepared = true
		return true, nil
	})
	if err != nil || !prepared {
		return result, err
	}

	for _, binding := range admission.Bindings {
		dispatchResult := NativeDispatchResult{Status: NativeDispatchNotStarted, Failure: "native dispatch context cancelled before invocation"}
		var late <-chan NativeDispatchResult
		if ctx.Err() == nil {
			dispatchContext, cancel := context.WithTimeout(ctx, authority.dispatchTimeout)
			response := make(chan NativeDispatchResult, 1)
			go func(item NativeTaskBinding) { response <- dispatch(dispatchContext, item) }(binding)
			select {
			case dispatchResult = <-response:
			case <-dispatchContext.Done():
				dispatchResult = NativeDispatchResult{Status: NativeDispatchUncertain, Failure: "native dispatch exceeded its authority deadline"}
				late = response
			}
			cancel()
		}
		if !durableDispatchResultValid(dispatchResult) {
			dispatchResult = NativeDispatchResult{Status: NativeDispatchUncertain, Failure: "native dispatch returned an invalid classification"}
		}
		if late != nil {
			lock := callbackLocks[binding.TicketID]
			delete(callbackLocks, binding.TicketID)
			go authority.awaitLateDispatch(admission.ScheduleID, binding.TicketID, late, lock)
		}
		if updateErr := authority.recordDispatchResult(context.WithoutCancel(ctx), admission.ScheduleID, binding.TicketID, dispatchResult, late == nil); updateErr != nil {
			return WaveAdmissionResult{Verdict: NativeTicketUnavailable}, updateErr
		}
		if late == nil {
			callbackLocks[binding.TicketID].Release()
			delete(callbackLocks, binding.TicketID)
		}
	}
	err = authority.transact(context.WithoutCancel(ctx), func(state *durableAuthorityState) (bool, error) {
		schedule, ok := state.Schedules[admission.ScheduleID]
		if !ok || !sameAdmissionLease(schedule.Lease, admission) {
			result = WaveAdmissionResult{Verdict: NativeTicketFenced}
			return false, nil
		}
		result = WaveAdmissionResult{Verdict: NativeTicketAccepted, Checkpoint: durableCheckpoint(state, admission.ScheduleID, admission.PlanID, schedule.Lease.ParentSessionID)}
		state.AdmissionReplay[admission.IdempotencyKey] = result
		return true, nil
	})
	return result, err
}

func (authority *DurableTicketAuthority) AcceptTerminal(ctx context.Context, acceptance TerminalAcceptance) (verdict NativeTicketVerdict, err error) {
	verdict = NativeTicketUnavailable
	err = authority.transact(ctx, func(state *durableAuthorityState) (bool, error) {
		expectedKey, keyErr := terminalAcceptanceIdempotencyKey(acceptance)
		if keyErr != nil || acceptance.IdempotencyKey != expectedKey {
			verdict = NativeTicketRevoked
			return false, nil
		}
		schedule, ok := state.Schedules[acceptance.ScheduleID]
		if !ok || !sameTerminalLease(schedule.Lease, acceptance) {
			verdict = NativeTicketFenced
			return false, nil
		}
		if state.Revoked[acceptance.Outcome.TicketID] {
			verdict = NativeTicketRevoked
			return false, nil
		}
		if state.Expired[acceptance.Outcome.TicketID] {
			verdict = NativeTicketExpired
			return false, nil
		}
		if replay, ok := state.TerminalReplay[acceptance.IdempotencyKey]; ok {
			verdict = replay
			return false, nil
		}
		use := durableUseFromTerminal(acceptance)
		admitted, ticketOK := state.AdmittedTickets[use.TicketID]
		child, childOK := state.AdmittedChildren[use.ChildSessionID]
		_, consumed := state.TerminalTickets[use.TicketID]
		if !ticketOK || !childOK || admitted != use || child != use || consumed || state.DispatchResults[use.TicketID].Status != NativeDispatchConfirmed {
			verdict = NativeTicketRevoked
			return false, nil
		}
		state.TerminalTickets[use.TicketID] = acceptance.IdempotencyKey
		outcome := acceptance.Outcome
		outcome.Result = append(json.RawMessage(nil), outcome.Result...)
		state.TerminalOutcomes[use.TicketID] = outcome
		state.TerminalReplay[acceptance.IdempotencyKey] = NativeTicketAccepted
		verdict = NativeTicketAccepted
		return true, nil
	})
	return verdict, err
}

func (authority *DurableTicketAuthority) AcceptJoin(ctx context.Context, acceptance JoinAcceptance) (result JoinAcceptanceResult, err error) {
	result.Verdict = NativeTicketUnavailable
	err = authority.transact(ctx, func(state *durableAuthorityState) (bool, error) {
		expectedKey, keyErr := joinAcceptanceIdempotencyKey(acceptance)
		if keyErr != nil || acceptance.IdempotencyKey != expectedKey {
			result = JoinAcceptanceResult{Verdict: NativeTicketRevoked}
			return false, nil
		}
		schedule, ok := state.Schedules[acceptance.ScheduleID]
		if !ok || !sameJoinLease(schedule.Lease, acceptance) {
			result = JoinAcceptanceResult{Verdict: NativeTicketFenced}
			return false, nil
		}
		lease := schedule.Lease
		lease.Checkpoint = durableCheckpoint(state, acceptance.ScheduleID, acceptance.PlanID, acceptance.ParentSessionID)
		status, _, tasks, restoreErr := restoreCheckpoint(schedule.Plan, ScheduleIdentity{
			ScheduleID: acceptance.ScheduleID, OwnerID: acceptance.OwnerID, ParentSessionID: acceptance.ParentSessionID,
		}, lease)
		if restoreErr != nil || status != ScheduleCompleted && status != ScheduleFailed && status != ScheduleCancelled {
			result = JoinAcceptanceResult{Verdict: NativeTicketRevoked}
			return false, nil
		}
		authoritative, buildErr := buildJoinResult(ctx, schedule.Plan, status, tasks)
		candidateData, candidateErr := json.Marshal(acceptance.Candidate)
		authoritativeData, authoritativeErr := json.Marshal(authoritative)
		if buildErr != nil || candidateErr != nil || authoritativeErr != nil || !bytes.Equal(candidateData, authoritativeData) {
			result = JoinAcceptanceResult{Verdict: NativeTicketRevoked}
			return false, nil
		}
		if replay, ok := state.JoinReplay[acceptance.IdempotencyKey]; ok {
			result = JoinAcceptanceResult{Verdict: NativeTicketAccepted, Join: cloneDurableJoin(replay)}
			return false, nil
		}
		state.JoinReplay[acceptance.IdempotencyKey] = cloneDurableJoin(authoritative)
		result = JoinAcceptanceResult{Verdict: NativeTicketAccepted, Join: cloneDurableJoin(authoritative)}
		return true, nil
	})
	return result, err
}

func (authority *DurableTicketAuthority) RevokeTicket(ctx context.Context, ticketID string) error {
	return authority.markTicket(ctx, ticketID, true)
}

func (authority *DurableTicketAuthority) ExpireTicket(ctx context.Context, ticketID string) error {
	return authority.markTicket(ctx, ticketID, false)
}

// ResolveUncertain releases a crash-left callback fence only after the caller
// has performed an explicit external audit. It never changes the persisted
// uncertain task classification or causes automatic redelivery.
func (authority *DurableTicketAuthority) ResolveUncertain(ctx context.Context, scheduleID string) error {
	if !validIdentity(scheduleID) {
		return ErrInvalidSchedule
	}
	var recovered []FileLock
	defer func() {
		for _, lock := range recovered {
			lock.Release()
		}
	}()
	return authority.transact(ctx, func(state *durableAuthorityState) (bool, error) {
		if _, ok := state.Schedules[scheduleID]; !ok {
			return false, ErrInvalidSchedule
		}
		if len(state.ActiveCallbacks[scheduleID]) == 0 {
			return false, nil
		}
		for ticketID := range state.ActiveCallbacks[scheduleID] {
			lock, err := AcquireFileLock(filepath.Join(authority.directory, "callback-"+ticketID+".lock"))
			if err != nil {
				return false, ErrAuthorityUnavailable
			}
			recovered = append(recovered, lock)
		}
		delete(state.ActiveCallbacks, scheduleID)
		return true, nil
	})
}

func (authority *DurableTicketAuthority) ConfirmPreparedDispatch(ctx context.Context, scheduleID, ticketID string) error {
	if !validIdentity(scheduleID) || !validIdentity(ticketID) {
		return ErrInvalidSchedule
	}
	return authority.transact(ctx, func(state *durableAuthorityState) (bool, error) {
		use, ok := state.AdmittedTickets[ticketID]
		if !ok || use.ScheduleID != scheduleID || state.ActiveCallbacks[scheduleID][ticketID] {
			return false, ErrInvalidSchedule
		}
		current := state.DispatchResults[ticketID]
		if current.Status == NativeDispatchConfirmed {
			return false, nil
		}
		if current.Status != NativeDispatchUncertain {
			return false, ErrInvalidSchedule
		}
		state.DispatchResults[ticketID] = NativeDispatchResult{Status: NativeDispatchConfirmed}
		return true, nil
	})
}

func (authority *DurableTicketAuthority) markTicket(ctx context.Context, ticketID string, revoked bool) error {
	if !validIdentity(ticketID) {
		return ErrInvalidSchedule
	}
	return authority.transact(ctx, func(state *durableAuthorityState) (bool, error) {
		if _, ok := state.AdmittedTickets[ticketID]; !ok {
			return false, ErrInvalidSchedule
		}
		if revoked {
			if state.Revoked[ticketID] {
				return false, nil
			}
			state.Revoked[ticketID] = true
		} else {
			if state.Expired[ticketID] {
				return false, nil
			}
			state.Expired[ticketID] = true
		}
		return true, nil
	})
}

func (authority *DurableTicketAuthority) recordDispatchResult(ctx context.Context, scheduleID, ticketID string, result NativeDispatchResult, callbackFinished bool) error {
	return authority.transact(ctx, func(state *durableAuthorityState) (bool, error) {
		use, ok := state.AdmittedTickets[ticketID]
		if !ok || use.ScheduleID != scheduleID {
			return false, ErrInvalidSchedule
		}
		state.DispatchResults[ticketID] = result
		if callbackFinished && state.ActiveCallbacks[scheduleID][ticketID] {
			delete(state.ActiveCallbacks[scheduleID], ticketID)
		}
		return true, nil
	})
}

func (authority *DurableTicketAuthority) awaitLateDispatch(scheduleID, ticketID string, result <-chan NativeDispatchResult, lock FileLock) {
	defer lock.Release()
	resolved := <-result
	if !durableDispatchResultValid(resolved) {
		resolved = NativeDispatchResult{Status: NativeDispatchUncertain, Failure: "native dispatch returned an invalid late classification"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = authority.recordDispatchResult(ctx, scheduleID, ticketID, resolved, true)
}

func (authority *DurableTicketAuthority) transact(ctx context.Context, action func(*durableAuthorityState) (bool, error)) error {
	if authority == nil || action == nil {
		return ErrInvalidSchedule
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	lock, err := authority.acquire(ctx)
	if err != nil {
		return err
	}
	defer lock.Release()
	state, err := authority.readState()
	if err != nil {
		return err
	}
	changed, err := action(&state)
	if err != nil || !changed {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return authority.writeState(state)
}

func (authority *DurableTicketAuthority) acquire(ctx context.Context) (FileLock, error) {
	for {
		lock, err := AcquireFileLock(authority.lockPath)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, ErrCoordinatorBusy) {
			return FileLock{}, fmt.Errorf("acquire delegation authority: %w", err)
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return FileLock{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func (authority *DurableTicketAuthority) readState() (durableAuthorityState, error) {
	data, err := os.ReadFile(authority.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return newDurableAuthorityState(), nil
	}
	if err != nil || len(data) == 0 || len(data) > durableAuthorityMaxBytes {
		return durableAuthorityState{}, fmt.Errorf("read delegation authority: %w", ErrDurability)
	}
	var state durableAuthorityState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil || state.Version != durableAuthorityVersion || !state.valid() {
		return durableAuthorityState{}, fmt.Errorf("decode delegation authority: %w", ErrDurability)
	}
	return state, nil
}

func (authority *DurableTicketAuthority) writeState(state durableAuthorityState) error {
	data, err := json.Marshal(state)
	if err != nil || len(data) > durableAuthorityMaxBytes {
		return fmt.Errorf("encode delegation authority: %w", ErrDurability)
	}
	file, err := os.CreateTemp(authority.directory, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("create delegation authority: %w", err)
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
		return fmt.Errorf("persist delegation authority: %w", ErrDurability)
	}
	if err := os.Rename(temporary, authority.statePath); err != nil {
		return fmt.Errorf("publish delegation authority: %w", err)
	}
	if err := syncAuthorityDirectory(authority.directory); err != nil {
		return fmt.Errorf("sync delegation authority: %w", err)
	}
	return nil
}

func newDurableAuthorityState() durableAuthorityState {
	return durableAuthorityState{
		Version: durableAuthorityVersion, Schedules: make(map[string]durableSchedule), ClaimReplay: make(map[string]ScheduleLease),
		Allowed: make(map[string]durableTicketUse), AdmittedTickets: make(map[string]durableTicketUse), AdmittedChildren: make(map[string]durableTicketUse),
		LogicalSlots: make(map[string]durableTicketUse), DispatchResults: make(map[string]NativeDispatchResult), AdmissionReplay: make(map[string]WaveAdmissionResult),
		TerminalTickets: make(map[string]string), TerminalOutcomes: make(map[string]NativeTaskOutcome), TerminalReplay: make(map[string]NativeTicketVerdict),
		JoinReplay: make(map[string]JoinResult), Revoked: make(map[string]bool), Expired: make(map[string]bool), ActiveCallbacks: make(map[string]map[string]bool),
	}
}

func (state durableAuthorityState) valid() bool {
	return state.Schedules != nil && state.ClaimReplay != nil && state.Allowed != nil && state.AdmittedTickets != nil && state.AdmittedChildren != nil &&
		state.LogicalSlots != nil && state.DispatchResults != nil && state.AdmissionReplay != nil && state.TerminalTickets != nil && state.TerminalOutcomes != nil &&
		state.TerminalReplay != nil && state.JoinReplay != nil && state.Revoked != nil && state.Expired != nil && state.ActiveCallbacks != nil
}

func ensurePrivateAuthorityDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("delegation authority directory: %w", ErrDurability)
	}
	return nil
}

func durableCheckpoint(state *durableAuthorityState, scheduleID, planID, parentSessionID string) ScheduleCheckpoint {
	checkpoint := ScheduleCheckpoint{ScheduleID: scheduleID, PlanID: planID, ParentSessionID: parentSessionID}
	for _, use := range state.AdmittedTickets {
		if use.ScheduleID != scheduleID || use.PlanID != planID || use.ParentSessionID != parentSessionID {
			continue
		}
		dispatch, ok := state.DispatchResults[use.TicketID]
		if !ok {
			dispatch = NativeDispatchResult{Status: NativeDispatchUncertain, Failure: "native dispatch state missing"}
		}
		item := NativeTaskCheckpoint{
			NativeTaskBinding: NativeTaskBinding{TaskID: use.TaskID, ParentSessionID: use.ParentSessionID, ChildSessionID: use.ChildSessionID, TicketID: use.TicketID},
			DispatchStatus:    dispatch.Status, Status: TaskRunning,
		}
		switch {
		case state.Revoked[use.TicketID]:
			item.Status, item.Failure = TaskFailed, "native ticket authority revoked"
		case state.Expired[use.TicketID]:
			item.Status, item.Failure = TaskFailed, "native ticket authority expired"
		case dispatch.Status == NativeDispatchNotStarted || dispatch.Status == NativeDispatchUncertain:
			item.Status, item.Failure = TaskFailed, dispatch.Failure
		case dispatch.Status != NativeDispatchConfirmed:
			item.Status, item.DispatchStatus, item.Failure = TaskFailed, NativeDispatchUncertain, "native dispatch state invalid"
		case state.TerminalOutcomes[use.TicketID].TaskID != "":
			outcome := state.TerminalOutcomes[use.TicketID]
			item.Status, item.MessageID, item.ResultID = outcome.Status, outcome.MessageID, outcome.ResultID
			item.Result = append(json.RawMessage(nil), outcome.Result...)
			item.Failure = outcome.Failure
		}
		checkpoint.Tasks = append(checkpoint.Tasks, item)
	}
	sort.Slice(checkpoint.Tasks, func(i, j int) bool { return checkpoint.Tasks[i].TaskID < checkpoint.Tasks[j].TaskID })
	return checkpoint
}

func durableUseFor(scheduleID string, plan navigator.Plan, binding NativeTaskBinding) (durableTicketUse, bool) {
	if !validIdentity(binding.TaskID) || !validIdentity(binding.ParentSessionID) || !validIdentity(binding.ChildSessionID) || !validIdentity(binding.TicketID) {
		return durableTicketUse{}, false
	}
	for _, wave := range plan.Waves {
		for _, taskID := range wave.TaskIDs {
			if taskID == binding.TaskID {
				return durableTicketUse{ScheduleID: scheduleID, PlanID: plan.PlanID, WaveID: wave.WaveID, TaskID: binding.TaskID, ParentSessionID: binding.ParentSessionID, ChildSessionID: binding.ChildSessionID, TicketID: binding.TicketID}, true
			}
		}
	}
	return durableTicketUse{}, false
}

func durableUseFromAdmission(admission WaveAdmission, binding NativeTaskBinding) durableTicketUse {
	return durableTicketUse{ScheduleID: admission.ScheduleID, PlanID: admission.PlanID, WaveID: admission.WaveID, TaskID: binding.TaskID, ParentSessionID: binding.ParentSessionID, ChildSessionID: binding.ChildSessionID, TicketID: binding.TicketID}
}

func durableUseFromTerminal(acceptance TerminalAcceptance) durableTicketUse {
	binding := acceptance.Outcome.NativeTaskBinding
	return durableTicketUse{ScheduleID: acceptance.ScheduleID, PlanID: acceptance.PlanID, WaveID: acceptance.WaveID, TaskID: binding.TaskID, ParentSessionID: binding.ParentSessionID, ChildSessionID: binding.ChildSessionID, TicketID: binding.TicketID}
}

func durableUseKey(use durableTicketUse) string {
	return strings.Join([]string{use.ScheduleID, use.PlanID, use.WaveID, use.TaskID, use.ParentSessionID, use.ChildSessionID, use.TicketID}, "\x00")
}

func durableSlotKey(use durableTicketUse) string {
	return strings.Join([]string{use.ScheduleID, use.PlanID, use.WaveID, use.TaskID}, "\x00")
}

func durablePrerequisites(plan navigator.Plan, waveID string) ([]string, bool) {
	var prerequisites []string
	for _, wave := range plan.Waves {
		if wave.WaveID == waveID {
			sort.Strings(prerequisites)
			return prerequisites, true
		}
		prerequisites = append(prerequisites, wave.TaskIDs...)
	}
	return nil, false
}

func durableRecordRejectedAdmission(state *durableAuthorityState, admission WaveAdmission, failure string) {
	for _, binding := range admission.Bindings {
		use := durableUseFromAdmission(admission, binding)
		if allowed, ok := state.Allowed[durableUseKey(use)]; !ok || allowed != use {
			continue
		}
		if existing, ok := state.AdmittedTickets[use.TicketID]; ok && existing != use {
			continue
		}
		if existing, ok := state.AdmittedChildren[use.ChildSessionID]; ok && existing != use {
			continue
		}
		if existing, ok := state.LogicalSlots[durableSlotKey(use)]; ok && existing != use {
			continue
		}
		state.AdmittedTickets[use.TicketID] = use
		state.AdmittedChildren[use.ChildSessionID] = use
		state.LogicalSlots[durableSlotKey(use)] = use
		if _, ok := state.DispatchResults[use.TicketID]; !ok {
			state.DispatchResults[use.TicketID] = NativeDispatchResult{Status: NativeDispatchNotStarted, Failure: failure}
		}
	}
}

func durableTicketFailure(state *durableAuthorityState, ticketID string) string {
	if state.Expired[ticketID] {
		return "native ticket authority expired"
	}
	return "native ticket authority revoked"
}

func durableDispatchResultValid(result NativeDispatchResult) bool {
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

func sameDurableLease(lease ScheduleLease, ref ScheduleLeaseRef) bool {
	return lease.Verdict == ScheduleLeaseGranted && lease.ScheduleID == ref.ScheduleID && lease.PlanID == ref.PlanID && lease.ParentSessionID == ref.ParentSessionID && lease.OwnerID == ref.OwnerID && lease.Epoch == ref.Epoch
}

func sameAdmissionLease(lease ScheduleLease, admission WaveAdmission) bool {
	return lease.Verdict == ScheduleLeaseGranted && lease.ScheduleID == admission.ScheduleID && lease.PlanID == admission.PlanID && lease.OwnerID == admission.OwnerID && lease.Epoch == admission.ScheduleEpoch
}

func sameTerminalLease(lease ScheduleLease, acceptance TerminalAcceptance) bool {
	return lease.Verdict == ScheduleLeaseGranted && lease.ScheduleID == acceptance.ScheduleID && lease.PlanID == acceptance.PlanID && lease.OwnerID == acceptance.OwnerID && lease.Epoch == acceptance.ScheduleEpoch && lease.ParentSessionID == acceptance.Outcome.ParentSessionID
}

func sameJoinLease(lease ScheduleLease, acceptance JoinAcceptance) bool {
	return lease.Verdict == ScheduleLeaseGranted && lease.ScheduleID == acceptance.ScheduleID && lease.PlanID == acceptance.PlanID && lease.ParentSessionID == acceptance.ParentSessionID && lease.OwnerID == acceptance.OwnerID && lease.Epoch == acceptance.ScheduleEpoch
}

func cloneDurableJoin(result JoinResult) JoinResult {
	result.Tasks = append([]JoinedTask(nil), result.Tasks...)
	return result
}

var _ NativeTicketAuthority = (*DurableTicketAuthority)(nil)
