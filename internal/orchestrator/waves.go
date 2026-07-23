package orchestrator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/vgxness/vgxness/internal/contracts"
	"github.com/vgxness/vgxness/internal/navigator"
)

var (
	ErrInvalidSchedule      = errors.New("invalid delegation schedule")
	ErrScheduleState        = errors.New("illegal delegation schedule transition")
	ErrAuthorityUnavailable = errors.New("native ticket authority unavailable")
	ErrNativeDispatch       = errors.New("native task dispatch did not complete")
	nativeIdentity          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

type ScheduleStatus string
type TaskExecutionStatus string

const (
	SchedulePending   ScheduleStatus = "pending"
	ScheduleRunning   ScheduleStatus = "running"
	ScheduleCompleted ScheduleStatus = "completed"
	ScheduleFailed    ScheduleStatus = "failed"
	ScheduleCancelled ScheduleStatus = "cancelled"

	TaskPending   TaskExecutionStatus = "pending"
	TaskRunning   TaskExecutionStatus = "running"
	TaskCompleted TaskExecutionStatus = "completed"
	TaskFailed    TaskExecutionStatus = "failed"
	TaskCancelled TaskExecutionStatus = "cancelled"

	maxNativeResultBytes = 2 << 20
)

type NativeTaskBinding struct {
	TaskID          string
	ParentSessionID string
	ChildSessionID  string
	TicketID        string
}

type NativeTaskOutcome struct {
	NativeTaskBinding
	Status    TaskExecutionStatus
	MessageID string
	ResultID  string
	Result    json.RawMessage
	Failure   string
}

type NativeTicketVerdict string
type ScheduleLeaseVerdict string

const (
	NativeTicketAccepted    NativeTicketVerdict = "accepted"
	NativeTicketFenced      NativeTicketVerdict = "fenced"
	NativeTicketRevoked     NativeTicketVerdict = "revoked"
	NativeTicketExpired     NativeTicketVerdict = "expired"
	NativeTicketUnavailable NativeTicketVerdict = "unavailable"

	ScheduleLeaseGranted     ScheduleLeaseVerdict = "granted"
	ScheduleLeaseRejected    ScheduleLeaseVerdict = "rejected"
	ScheduleLeaseUnavailable ScheduleLeaseVerdict = "unavailable"
)

// ScheduleIdentity is minted by the trusted OpenCode adapter. OwnerID must be
// collision-resistant and unique for every live scheduler incarnation. It may
// be reused only to resolve an uncertain AcquireSchedule response before a
// scheduler was returned; recovery after successful construction uses a new
// owner so the authority advances the epoch and fences the prior instance.
type ScheduleIdentity struct {
	ScheduleID      string
	OwnerID         string
	ParentSessionID string
}

type ScheduleClaim struct {
	IdempotencyKey  string
	ScheduleID      string
	PlanID          string
	ParentSessionID string
	OwnerID         string
}

type ScheduleLease struct {
	Verdict         ScheduleLeaseVerdict
	ScheduleID      string
	PlanID          string
	ParentSessionID string
	OwnerID         string
	Epoch           uint64
	Checkpoint      ScheduleCheckpoint
}

type NativeTaskCheckpoint struct {
	NativeTaskBinding
	DispatchStatus NativeDispatchStatus
	Status         TaskExecutionStatus
	MessageID      string
	ResultID       string
	Result         json.RawMessage
	Failure        string
}

type ScheduleCheckpoint struct {
	ScheduleID      string
	PlanID          string
	ParentSessionID string
	Tasks           []NativeTaskCheckpoint
}

type NativeDispatchStatus string

const (
	NativeDispatchConfirmed  NativeDispatchStatus = "confirmed"
	NativeDispatchNotStarted NativeDispatchStatus = "not_started"
	NativeDispatchUncertain  NativeDispatchStatus = "uncertain"
)

// A confirmed result must have an empty Failure. Not-started and uncertain
// results must contain a trimmed, non-empty failure of at most 2048 runes.
// Invalid combinations are authority protocol errors and must be persisted as
// uncertain rather than guessed or redispatched.
type NativeDispatchResult struct {
	Status  NativeDispatchStatus
	Failure string
}

// NativeTaskDispatch is the trusted OpenCode adapter's per-ticket native
// create/send boundary. Before invoking it, the authority must durably reserve
// the logical schedule/plan/wave/task slot and persist an uncertain dispatch
// marker. The callback classifies that one binding as confirmed, definitely not
// started, or uncertain. Authorities must never invoke the same logical slot
// again unless a separate reconciliation protocol has proved that no native
// side effect occurred. This foundation fails not-started and uncertain tasks
// closed and leaves any later recovery to an explicit audited successor run.
type NativeTaskDispatch func(context.Context, NativeTaskBinding) NativeDispatchResult

type WaveAdmission struct {
	IdempotencyKey      string
	ScheduleID          string
	OwnerID             string
	ScheduleEpoch       uint64
	PlanID              string
	WaveID              string
	PrerequisiteTaskIDs []string
	Bindings            []NativeTaskBinding
}

type WaveAdmissionResult struct {
	Verdict    NativeTicketVerdict
	Checkpoint ScheduleCheckpoint
}

type ScheduleLeaseRef struct {
	ScheduleID      string
	PlanID          string
	ParentSessionID string
	OwnerID         string
	Epoch           uint64
}

type TerminalAcceptance struct {
	IdempotencyKey string
	ScheduleID     string
	OwnerID        string
	ScheduleEpoch  uint64
	PlanID         string
	WaveID         string
	Outcome        NativeTaskOutcome
}

type JoinAcceptance struct {
	IdempotencyKey  string
	ScheduleID      string
	OwnerID         string
	ScheduleEpoch   uint64
	PlanID          string
	ParentSessionID string
	Candidate       JoinResult
}

// JoinAcceptanceResult is the authority-linearized publication snapshot.
// AcceptJoin must atomically fence the owner/epoch, rebuild the current
// revocation-aware terminal checkpoint, require exact Candidate equality, and
// replay the same immutable Join for the same idempotency key.
type JoinAcceptanceResult struct {
	Verdict NativeTicketVerdict
	Join    JoinResult
}

// NativeTicketAuthority owns the durable fencing boundary for native OpenCode
// child sessions. AcquireSchedule must bind a schedule permanently to its plan
// and parent, return the same live lease for an exact claim replay, and issue a
// greater nonzero epoch when a different unique owner takes over. A superseded owner must be
// rejected by ValidateLease, AdmitWave, AcceptTerminal, and AcceptJoin. AdmitWave must
// reserve each logical schedule/plan/wave/task slot durably before dispatch,
// reject alternate bindings for a reserved slot across processes, persist the
// callback's per-task dispatch status, and return a current checkpoint. All
// mutating methods must validate their IdempotencyKey and recheck the current lease plus
// ticket validity before replaying an accepted operation. Checkpoints are
// mandatory on every granted lease, including the initial empty lease, and must
// reflect current revocation/expiry state. Nil-error verdicts are definitive; an
// unavailable verdict is retryable. A non-nil error means the result is
// uncertain and requires replay with the same immutable key and payload.
// ValidateLease is a read-only freshness probe and is deliberately exempt from
// idempotency keys; authoritative publication uses AcceptJoin instead. Dispatch
// deadlines bound the admission call, not an uncooperative native side effect:
// persist uncertain, ignore late results, and keep takeover fenced until the
// callback terminates or proof-backed reconciliation resolves it. Implementations
// must honor context; calls occur outside scheduler locks.
type NativeTicketAuthority interface {
	AcquireSchedule(context.Context, ScheduleClaim) (ScheduleLease, error)
	ValidateLease(context.Context, ScheduleLeaseRef) (NativeTicketVerdict, error)
	AdmitWave(context.Context, WaveAdmission, NativeTaskDispatch) (WaveAdmissionResult, error)
	AcceptTerminal(context.Context, TerminalAcceptance) (NativeTicketVerdict, error)
	AcceptJoin(context.Context, JoinAcceptance) (JoinAcceptanceResult, error)
}

type JoinedTask struct {
	TaskID          string               `json:"taskId"`
	Status          TaskExecutionStatus  `json:"status"`
	DispatchStatus  NativeDispatchStatus `json:"dispatchStatus"`
	ParentSessionID string               `json:"parentSessionId"`
	ChildSessionID  string               `json:"childSessionId"`
	TicketID        string               `json:"ticketId"`
	MessageID       string               `json:"messageId,omitempty"`
	ResultID        string               `json:"resultId,omitempty"`
	ResultDigest    string               `json:"resultDigest,omitempty"`
	Failure         string               `json:"failure,omitempty"`
}

type JoinResult struct {
	Kind          string       `json:"kind"`
	SchemaVersion string       `json:"schemaVersion"`
	PlanID        string       `json:"planId"`
	Status        string       `json:"status"`
	Completed     int          `json:"completed"`
	Failed        int          `json:"failed"`
	Cancelled     int          `json:"cancelled"`
	Tasks         []JoinedTask `json:"tasks"`
}

type WaveScheduler struct {
	mu                sync.Mutex
	plan              navigator.Plan
	status            ScheduleStatus
	currentWave       int
	tasks             map[string]JoinedTask
	scheduleID        string
	ownerID           string
	scheduleEpoch     uint64
	parentID          string
	authority         NativeTicketAuthority
	superseded        bool
	admissionInFlight bool
	terminalInFlight  map[string]struct{}
}

type schedulerOpen struct {
	done      chan struct{}
	scheduler *WaveScheduler
	err       error
}

// SchedulerFactory is the mandatory process-local construction boundary. One
// factory is owned by the composition root for one NativeTicketAuthority. It
// singleflights schedule/owner opens and returns the same scheduler pointer for
// an exact duplicate, so an idempotent lease replay cannot create two live
// execution handles in this process.
type SchedulerFactory struct {
	mu         sync.Mutex
	authority  NativeTicketAuthority
	schedulers map[string]*WaveScheduler
	current    map[string]*WaveScheduler
	opening    map[string]*schedulerOpen
}

func NewSchedulerFactory(authority NativeTicketAuthority) (*SchedulerFactory, error) {
	if authority == nil {
		return nil, ErrInvalidSchedule
	}
	return &SchedulerFactory{
		authority: authority, schedulers: make(map[string]*WaveScheduler),
		current: make(map[string]*WaveScheduler), opening: make(map[string]*schedulerOpen),
	}, nil
}

func (factory *SchedulerFactory) Open(ctx context.Context, plan navigator.Plan, expectedPlanID string, identity ScheduleIdentity) (*WaveScheduler, error) {
	if factory == nil || factory.authority == nil {
		return nil, ErrInvalidSchedule
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := identity.ScheduleID + "\x00" + identity.OwnerID
	for {
		factory.mu.Lock()
		if scheduler := factory.schedulers[key]; scheduler != nil {
			factory.mu.Unlock()
			if !schedulerMatches(scheduler, plan, expectedPlanID, identity) {
				return nil, ErrInvalidSchedule
			}
			return scheduler, nil
		}
		if pending := factory.opening[key]; pending != nil {
			done := pending.done
			factory.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-done:
				if pending.err != nil {
					if ctx.Err() == nil && (errors.Is(pending.err, context.Canceled) || errors.Is(pending.err, context.DeadlineExceeded)) {
						continue
					}
					return nil, pending.err
				}
				if !schedulerMatches(pending.scheduler, plan, expectedPlanID, identity) {
					return nil, ErrInvalidSchedule
				}
				return pending.scheduler, nil
			}
		}
		pending := &schedulerOpen{done: make(chan struct{})}
		factory.opening[key] = pending
		factory.mu.Unlock()

		scheduler, err := newWaveScheduler(ctx, plan, expectedPlanID, identity, factory.authority)
		var previous, stale *WaveScheduler
		factory.mu.Lock()
		if err == nil {
			previous = factory.current[identity.ScheduleID]
			if previous != nil && previous.scheduleEpoch >= scheduler.scheduleEpoch {
				stale, scheduler, err = scheduler, nil, ErrInvalidSchedule
			} else {
				factory.schedulers[key] = scheduler
				factory.current[identity.ScheduleID] = scheduler
			}
		}
		pending.scheduler, pending.err = scheduler, err
		delete(factory.opening, key)
		close(pending.done)
		factory.mu.Unlock()
		if stale != nil {
			stale.markSuperseded()
		} else if previous != nil && previous != scheduler {
			previous.markSuperseded()
		}
		return scheduler, err
	}
}

func schedulerMatches(scheduler *WaveScheduler, plan navigator.Plan, expectedPlanID string, identity ScheduleIdentity) bool {
	if scheduler == nil || expectedPlanID != scheduler.plan.PlanID || plan.PlanID != expectedPlanID ||
		identity.ScheduleID != scheduler.scheduleID || identity.OwnerID != scheduler.ownerID || identity.ParentSessionID != scheduler.parentID {
		return false
	}
	candidate, err := json.Marshal(plan)
	if err != nil {
		return false
	}
	frozen, err := json.Marshal(scheduler.plan)
	return err == nil && bytes.Equal(candidate, frozen)
}

func newWaveScheduler(ctx context.Context, plan navigator.Plan, expectedPlanID string, identity ScheduleIdentity, authority NativeTicketAuthority) (*WaveScheduler, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if plan.PlanID != expectedPlanID || !validIdentity(expectedPlanID) || !validIdentity(identity.ScheduleID) || !validIdentity(identity.OwnerID) || !validIdentity(identity.ParentSessionID) || authority == nil {
		return nil, ErrInvalidSchedule
	}
	data, err := json.Marshal(plan)
	if err != nil {
		return nil, ErrInvalidSchedule
	}
	if err := contracts.Validate(ctx, contracts.OrchestrationURI+"#/$defs/delegationPlan", data, false); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSchedule, err)
	}
	var frozen navigator.Plan
	if err := json.Unmarshal(data, &frozen); err != nil {
		return nil, ErrInvalidSchedule
	}
	if err := navigator.ValidatePlan(ctx, frozen); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSchedule, err)
	}
	if err := validatePlanTopology(frozen); err != nil {
		return nil, err
	}
	claim := ScheduleClaim{
		ScheduleID: identity.ScheduleID, PlanID: frozen.PlanID,
		ParentSessionID: identity.ParentSessionID, OwnerID: identity.OwnerID,
	}
	claimKey, err := scheduleClaimIdempotencyKey(claim)
	if err != nil {
		return nil, ErrInvalidSchedule
	}
	claim.IdempotencyKey = claimKey
	lease, authorityErr := authority.AcquireSchedule(ctx, claim)
	if authorityErr != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %v", ErrAuthorityUnavailable, authorityErr)
	}
	switch lease.Verdict {
	case ScheduleLeaseGranted:
		if lease.ScheduleID != identity.ScheduleID || lease.PlanID != frozen.PlanID || lease.ParentSessionID != identity.ParentSessionID || lease.OwnerID != identity.OwnerID || lease.Epoch == 0 {
			return nil, ErrInvalidSchedule
		}
	case ScheduleLeaseUnavailable:
		return nil, ErrAuthorityUnavailable
	case ScheduleLeaseRejected:
		return nil, ErrInvalidSchedule
	default:
		return nil, ErrInvalidSchedule
	}
	status, currentWave, tasks, err := restoreCheckpoint(frozen, identity, lease)
	if err != nil {
		return nil, err
	}
	return &WaveScheduler{
		plan: frozen, status: status, currentWave: currentWave, tasks: tasks,
		scheduleID: identity.ScheduleID, ownerID: identity.OwnerID, scheduleEpoch: lease.Epoch,
		parentID: identity.ParentSessionID, authority: authority, terminalInFlight: make(map[string]struct{}),
	}, nil
}

func (scheduler *WaveScheduler) leaseRef() ScheduleLeaseRef {
	return ScheduleLeaseRef{
		ScheduleID: scheduler.scheduleID, PlanID: scheduler.plan.PlanID,
		ParentSessionID: scheduler.parentID, OwnerID: scheduler.ownerID, Epoch: scheduler.scheduleEpoch,
	}
}

func (scheduler *WaveScheduler) markSuperseded() {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	scheduler.superseded = true
	if scheduler.status == SchedulePending || scheduler.status == ScheduleRunning {
		for taskID, execution := range scheduler.tasks {
			if execution.Status == TaskRunning || execution.Status == TaskPending {
				execution.Status = TaskFailed
				execution.Failure = "schedule owner superseded"
				scheduler.tasks[taskID] = execution
			}
		}
		scheduler.status = ScheduleFailed
	}
}

func restoreCheckpoint(plan navigator.Plan, identity ScheduleIdentity, lease ScheduleLease) (ScheduleStatus, int, map[string]JoinedTask, error) {
	checkpoint := lease.Checkpoint
	if checkpoint.ScheduleID != identity.ScheduleID || checkpoint.PlanID != plan.PlanID || checkpoint.ParentSessionID != identity.ParentSessionID {
		return ScheduleFailed, 0, nil, ErrInvalidSchedule
	}
	planTasks := make(map[string]navigator.Task, len(plan.Tasks))
	waveByTask := make(map[string]int, len(plan.Tasks))
	for _, task := range plan.Tasks {
		planTasks[task.TaskID] = task
	}
	for waveIndex, wave := range plan.Waves {
		for _, taskID := range wave.TaskIDs {
			waveByTask[taskID] = waveIndex
		}
	}
	tasks := make(map[string]JoinedTask, len(plan.Tasks))
	children, tickets := make(map[string]struct{}), make(map[string]struct{})
	checkpointWaveCounts := make(map[int]int)
	for _, item := range checkpoint.Tasks {
		if _, ok := planTasks[item.TaskID]; !ok || item.ParentSessionID != identity.ParentSessionID ||
			!validIdentity(item.ChildSessionID) || !validIdentity(item.TicketID) || item.ParentSessionID == item.ChildSessionID {
			return ScheduleFailed, 0, nil, ErrInvalidSchedule
		}
		if _, duplicate := tasks[item.TaskID]; duplicate {
			return ScheduleFailed, 0, nil, ErrInvalidSchedule
		}
		if _, duplicate := children[item.ChildSessionID]; duplicate {
			return ScheduleFailed, 0, nil, ErrInvalidSchedule
		}
		children[item.ChildSessionID] = struct{}{}
		if _, duplicate := tickets[item.TicketID]; duplicate {
			return ScheduleFailed, 0, nil, ErrInvalidSchedule
		}
		tickets[item.TicketID] = struct{}{}
		execution := JoinedTask{
			TaskID: item.TaskID, Status: TaskRunning, DispatchStatus: item.DispatchStatus, ParentSessionID: item.ParentSessionID,
			ChildSessionID: item.ChildSessionID, TicketID: item.TicketID,
		}
		switch item.Status {
		case TaskRunning:
			if item.DispatchStatus != NativeDispatchConfirmed || item.MessageID != "" || item.ResultID != "" || len(item.Result) != 0 || item.Failure != "" {
				return ScheduleFailed, 0, nil, ErrInvalidSchedule
			}
		case TaskCompleted, TaskFailed, TaskCancelled:
			if item.Status != TaskFailed && item.DispatchStatus != NativeDispatchConfirmed {
				return ScheduleFailed, 0, nil, ErrInvalidSchedule
			}
			if item.Status == TaskFailed && item.DispatchStatus != NativeDispatchConfirmed && item.DispatchStatus != NativeDispatchNotStarted && item.DispatchStatus != NativeDispatchUncertain {
				return ScheduleFailed, 0, nil, ErrInvalidSchedule
			}
			outcome := NativeTaskOutcome{
				NativeTaskBinding: item.NativeTaskBinding, Status: item.Status, MessageID: item.MessageID,
				ResultID: item.ResultID, Result: append(json.RawMessage(nil), item.Result...), Failure: item.Failure,
			}
			var err error
			execution, err = stageTerminal(execution, outcome)
			if err != nil {
				return ScheduleFailed, 0, nil, err
			}
		default:
			return ScheduleFailed, 0, nil, ErrInvalidSchedule
		}
		tasks[item.TaskID] = execution
		checkpointWaveCounts[waveByTask[item.TaskID]]++
	}
	for waveIndex, wave := range plan.Waves {
		count := checkpointWaveCounts[waveIndex]
		if count == 0 {
			for later := waveIndex + 1; later < len(plan.Waves); later++ {
				if checkpointWaveCounts[later] != 0 {
					return ScheduleFailed, 0, nil, ErrInvalidSchedule
				}
			}
			return SchedulePending, waveIndex, tasks, nil
		}
		if count != len(wave.TaskIDs) {
			return ScheduleFailed, 0, nil, ErrInvalidSchedule
		}
		failed, cancelled, running := false, false, false
		for _, taskID := range wave.TaskIDs {
			switch tasks[taskID].Status {
			case TaskRunning:
				running = true
			case TaskFailed:
				failed = true
			case TaskCancelled:
				cancelled = true
			}
		}
		if failed || cancelled || running {
			for later := waveIndex + 1; later < len(plan.Waves); later++ {
				if checkpointWaveCounts[later] != 0 {
					return ScheduleFailed, 0, nil, ErrInvalidSchedule
				}
			}
			switch {
			case running:
				return ScheduleRunning, waveIndex, tasks, nil
			case failed:
				return ScheduleFailed, waveIndex, tasks, nil
			case cancelled:
				return ScheduleCancelled, waveIndex, tasks, nil
			}
		}
	}
	return ScheduleCompleted, len(plan.Waves), tasks, nil
}

func (scheduler *WaveScheduler) Status() ScheduleStatus {
	if scheduler == nil {
		return ScheduleFailed
	}
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	return scheduler.status
}

func (scheduler *WaveScheduler) validateLease(ctx context.Context) (NativeTicketVerdict, error) {
	verdict, err := scheduler.authority.ValidateLease(ctx, scheduler.leaseRef())
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return NativeTicketUnavailable, ctxErr
		}
		return NativeTicketUnavailable, fmt.Errorf("%w: %v", ErrAuthorityUnavailable, err)
	}
	return verdict, nil
}

func (scheduler *WaveScheduler) NextWave() (navigator.Wave, bool) {
	if scheduler == nil {
		return navigator.Wave{}, false
	}
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	if scheduler.status != SchedulePending || scheduler.currentWave >= len(scheduler.plan.Waves) {
		return navigator.Wave{}, false
	}
	wave := scheduler.plan.Waves[scheduler.currentWave]
	wave.TaskIDs = append([]string(nil), wave.TaskIDs...)
	return wave, true
}

// StartWave atomically admits every planned task and asks durable authority to
// invoke the per-task dispatch boundary while this schedule epoch is current.
// The caller cannot send native work after this method returns; all child
// create/send side effects must occur through dispatch. Confirmed tasks become
// running. Definitely-not-started and uncertain tasks fail closed with their
// dispatch state in the returned authority checkpoint.
func (scheduler *WaveScheduler) StartWave(ctx context.Context, waveID string, bindings []NativeTaskBinding, dispatch NativeTaskDispatch) error {
	if scheduler == nil {
		return ErrScheduleState
	}
	if dispatch == nil {
		return ErrInvalidSchedule
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	scheduler.mu.Lock()
	if scheduler.status != SchedulePending || scheduler.currentWave >= len(scheduler.plan.Waves) || scheduler.admissionInFlight {
		scheduler.mu.Unlock()
		return ErrScheduleState
	}
	wave := scheduler.plan.Waves[scheduler.currentWave]
	if wave.WaveID != waveID || len(bindings) != len(wave.TaskIDs) {
		scheduler.mu.Unlock()
		return ErrInvalidSchedule
	}
	expected := make(map[string]struct{}, len(wave.TaskIDs))
	for _, taskID := range wave.TaskIDs {
		expected[taskID] = struct{}{}
	}
	children, tickets := map[string]struct{}{}, map[string]struct{}{}
	for _, prior := range scheduler.tasks {
		children[prior.ChildSessionID] = struct{}{}
		tickets[prior.TicketID] = struct{}{}
	}
	staged := make(map[string]JoinedTask, len(bindings))
	for _, binding := range bindings {
		if _, ok := expected[binding.TaskID]; !ok || binding.ParentSessionID != scheduler.parentID || !validIdentity(binding.ChildSessionID) || !validIdentity(binding.TicketID) || binding.ParentSessionID == binding.ChildSessionID {
			scheduler.mu.Unlock()
			return ErrInvalidSchedule
		}
		if _, duplicate := staged[binding.TaskID]; duplicate {
			scheduler.mu.Unlock()
			return ErrInvalidSchedule
		}
		if _, duplicate := children[binding.ChildSessionID]; duplicate {
			scheduler.mu.Unlock()
			return ErrInvalidSchedule
		}
		children[binding.ChildSessionID] = struct{}{}
		if _, duplicate := tickets[binding.TicketID]; duplicate {
			scheduler.mu.Unlock()
			return ErrInvalidSchedule
		}
		tickets[binding.TicketID] = struct{}{}
		staged[binding.TaskID] = JoinedTask{
			TaskID: binding.TaskID, Status: TaskRunning, ParentSessionID: binding.ParentSessionID,
			ChildSessionID: binding.ChildSessionID, TicketID: binding.TicketID,
		}
	}
	admission := WaveAdmission{
		ScheduleID: scheduler.scheduleID, OwnerID: scheduler.ownerID, ScheduleEpoch: scheduler.scheduleEpoch,
		PlanID: scheduler.plan.PlanID, WaveID: wave.WaveID, Bindings: append([]NativeTaskBinding(nil), bindings...),
	}
	for index := 0; index < scheduler.currentWave; index++ {
		admission.PrerequisiteTaskIDs = append(admission.PrerequisiteTaskIDs, scheduler.plan.Waves[index].TaskIDs...)
	}
	sort.Strings(admission.PrerequisiteTaskIDs)
	sort.Slice(admission.Bindings, func(i, j int) bool { return admission.Bindings[i].TaskID < admission.Bindings[j].TaskID })
	key, keyErr := waveAdmissionIdempotencyKey(admission)
	if keyErr != nil {
		scheduler.mu.Unlock()
		return ErrInvalidSchedule
	}
	admission.IdempotencyKey = key
	scheduler.admissionInFlight = true
	scheduler.mu.Unlock()

	result, authorityErr := scheduler.authority.AdmitWave(ctx, admission, dispatch)
	if authorityErr == nil && result.Verdict == NativeTicketAccepted {
		current, validateErr := scheduler.validateLease(ctx)
		if validateErr != nil {
			authorityErr = validateErr
		} else if current != NativeTicketAccepted {
			result.Verdict = current
		}
	}
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	scheduler.admissionInFlight = false
	if scheduler.status != SchedulePending || scheduler.currentWave >= len(scheduler.plan.Waves) || scheduler.plan.Waves[scheduler.currentWave].WaveID != waveID {
		return ErrScheduleState
	}
	if authorityErr != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return fmt.Errorf("%w: %v", ErrAuthorityUnavailable, authorityErr)
	}
	switch result.Verdict {
	case NativeTicketAccepted:
	case NativeTicketUnavailable:
		return ErrAuthorityUnavailable
	case NativeTicketFenced, NativeTicketRevoked, NativeTicketExpired:
		if result.Checkpoint.ScheduleID != "" {
			lease := ScheduleLease{
				Verdict: ScheduleLeaseGranted, ScheduleID: scheduler.scheduleID, PlanID: scheduler.plan.PlanID,
				ParentSessionID: scheduler.parentID, OwnerID: scheduler.ownerID, Epoch: scheduler.scheduleEpoch,
				Checkpoint: result.Checkpoint,
			}
			status, currentWave, restored, checkpointErr := restoreCheckpoint(scheduler.plan, ScheduleIdentity{
				ScheduleID: scheduler.scheduleID, OwnerID: scheduler.ownerID, ParentSessionID: scheduler.parentID,
			}, lease)
			if checkpointErr == nil && (status == ScheduleFailed || status == ScheduleCancelled) {
				scheduler.tasks, scheduler.status, scheduler.currentWave = restored, status, currentWave
				return ErrInvalidSchedule
			}
		}
		return ErrInvalidSchedule
	default:
		return ErrInvalidSchedule
	}
	lease := ScheduleLease{
		Verdict: ScheduleLeaseGranted, ScheduleID: scheduler.scheduleID, PlanID: scheduler.plan.PlanID,
		ParentSessionID: scheduler.parentID, OwnerID: scheduler.ownerID, Epoch: scheduler.scheduleEpoch,
		Checkpoint: result.Checkpoint,
	}
	status, currentWave, restored, err := restoreCheckpoint(scheduler.plan, ScheduleIdentity{
		ScheduleID: scheduler.scheduleID, OwnerID: scheduler.ownerID, ParentSessionID: scheduler.parentID,
	}, lease)
	if err != nil {
		return err
	}
	dispatchFailed := false
	for _, item := range result.Checkpoint.Tasks {
		if item.DispatchStatus == NativeDispatchNotStarted || item.DispatchStatus == NativeDispatchUncertain {
			dispatchFailed = true
		}
	}
	for taskID, expectedExecution := range staged {
		execution, ok := restored[taskID]
		if !ok || execution.ParentSessionID != expectedExecution.ParentSessionID || execution.ChildSessionID != expectedExecution.ChildSessionID || execution.TicketID != expectedExecution.TicketID {
			return ErrInvalidSchedule
		}
	}
	scheduler.tasks, scheduler.status, scheduler.currentWave = restored, status, currentWave
	if dispatchFailed || status == ScheduleFailed || status == ScheduleCancelled {
		return ErrNativeDispatch
	}
	if status != ScheduleRunning {
		return ErrInvalidSchedule
	}
	return nil
}

func (scheduler *WaveScheduler) Record(ctx context.Context, outcome NativeTaskOutcome) error {
	if scheduler == nil {
		return ErrScheduleState
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	outcome.Result = append(json.RawMessage(nil), outcome.Result...)
	scheduler.mu.Lock()
	if scheduler.status != ScheduleRunning || scheduler.currentWave >= len(scheduler.plan.Waves) {
		scheduler.mu.Unlock()
		return ErrScheduleState
	}
	execution, ok := scheduler.tasks[outcome.TaskID]
	if _, inFlight := scheduler.terminalInFlight[outcome.TaskID]; inFlight || !ok || execution.Status != TaskRunning || execution.ParentSessionID != outcome.ParentSessionID || execution.ChildSessionID != outcome.ChildSessionID || execution.TicketID != outcome.TicketID {
		scheduler.mu.Unlock()
		return ErrInvalidSchedule
	}
	staged, err := stageTerminal(execution, outcome)
	if err != nil {
		scheduler.mu.Unlock()
		return err
	}
	waveID := scheduler.plan.Waves[scheduler.currentWave].WaveID
	scheduler.terminalInFlight[outcome.TaskID] = struct{}{}
	scheduler.mu.Unlock()

	acceptance := TerminalAcceptance{
		ScheduleID: scheduler.scheduleID, OwnerID: scheduler.ownerID, ScheduleEpoch: scheduler.scheduleEpoch,
		PlanID: scheduler.plan.PlanID, WaveID: waveID, Outcome: outcome,
	}
	key, keyErr := terminalAcceptanceIdempotencyKey(acceptance)
	if keyErr != nil {
		scheduler.mu.Lock()
		delete(scheduler.terminalInFlight, outcome.TaskID)
		scheduler.mu.Unlock()
		return ErrInvalidSchedule
	}
	acceptance.IdempotencyKey = key
	verdict, authorityErr := scheduler.authority.AcceptTerminal(ctx, acceptance)
	if authorityErr == nil && verdict == NativeTicketAccepted {
		current, validateErr := scheduler.validateLease(ctx)
		if validateErr != nil {
			authorityErr = validateErr
		} else if current != NativeTicketAccepted {
			verdict = current
		}
	}
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	delete(scheduler.terminalInFlight, outcome.TaskID)
	execution, ok = scheduler.tasks[outcome.TaskID]
	if scheduler.status != ScheduleRunning || scheduler.currentWave >= len(scheduler.plan.Waves) || scheduler.plan.Waves[scheduler.currentWave].WaveID != waveID || !ok || execution.Status != TaskRunning || execution.ParentSessionID != outcome.ParentSessionID || execution.ChildSessionID != outcome.ChildSessionID || execution.TicketID != outcome.TicketID {
		return ErrScheduleState
	}
	if authorityErr != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return fmt.Errorf("%w: %v", ErrAuthorityUnavailable, authorityErr)
	}
	switch verdict {
	case NativeTicketAccepted:
		execution = staged
	case NativeTicketFenced, NativeTicketRevoked, NativeTicketExpired:
		execution.Status = TaskFailed
		execution.MessageID, execution.ResultID, execution.ResultDigest = "", "", ""
		execution.Failure = "native ticket authority " + string(verdict)
	case NativeTicketUnavailable:
		return ErrAuthorityUnavailable
	default:
		return ErrInvalidSchedule
	}
	scheduler.tasks[outcome.TaskID] = execution
	return scheduler.advanceIfWaveTerminal()
}

func waveAdmissionIdempotencyKey(admission WaveAdmission) (string, error) {
	admission.IdempotencyKey = ""
	return nativeTicketIdempotencyKey("wave-admission", admission)
}

func scheduleClaimIdempotencyKey(claim ScheduleClaim) (string, error) {
	claim.IdempotencyKey = ""
	return nativeTicketIdempotencyKey("schedule-claim", claim)
}

func terminalAcceptanceIdempotencyKey(acceptance TerminalAcceptance) (string, error) {
	acceptance.IdempotencyKey = ""
	return nativeTicketIdempotencyKey("terminal-acceptance", acceptance)
}

func joinAcceptanceIdempotencyKey(acceptance JoinAcceptance) (string, error) {
	acceptance.IdempotencyKey = ""
	return nativeTicketIdempotencyKey("join-acceptance", acceptance)
}

func nativeTicketIdempotencyKey(kind string, payload any) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte(kind))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(data)
	return "sha256-" + hex.EncodeToString(digest.Sum(nil)), nil
}

func stageTerminal(execution JoinedTask, outcome NativeTaskOutcome) (JoinedTask, error) {
	switch outcome.Status {
	case TaskCompleted:
		if !validIdentity(outcome.MessageID) || !validIdentity(outcome.ResultID) || len(outcome.Result) == 0 || len(outcome.Result) > maxNativeResultBytes || !json.Valid(outcome.Result) || outcome.Failure != "" {
			return JoinedTask{}, ErrInvalidSchedule
		}
		digest := sha256.Sum256(outcome.Result)
		execution.MessageID, execution.ResultID = outcome.MessageID, outcome.ResultID
		execution.ResultDigest = "sha256-" + hex.EncodeToString(digest[:])
	case TaskFailed, TaskCancelled:
		failure := strings.TrimSpace(outcome.Failure)
		if failure == "" || utf8.RuneCountInString(outcome.Failure) > 2048 || outcome.MessageID != "" || outcome.ResultID != "" || len(outcome.Result) != 0 {
			return JoinedTask{}, ErrInvalidSchedule
		}
		execution.Failure = failure
	default:
		return JoinedTask{}, ErrInvalidSchedule
	}
	execution.Status = outcome.Status
	return execution, nil
}

func (scheduler *WaveScheduler) Join(ctx context.Context) (JoinResult, error) {
	if scheduler == nil {
		return JoinResult{}, ErrScheduleState
	}
	scheduler.mu.Lock()
	if scheduler.superseded || scheduler.status != ScheduleCompleted && scheduler.status != ScheduleFailed && scheduler.status != ScheduleCancelled {
		scheduler.mu.Unlock()
		return JoinResult{}, ErrScheduleState
	}
	result, err := buildJoinResult(ctx, scheduler.plan, scheduler.status, scheduler.tasks)
	scheduler.mu.Unlock()
	if err != nil {
		return JoinResult{}, err
	}
	acceptance := JoinAcceptance{
		ScheduleID: scheduler.scheduleID, OwnerID: scheduler.ownerID, ScheduleEpoch: scheduler.scheduleEpoch,
		PlanID: scheduler.plan.PlanID, ParentSessionID: scheduler.parentID, Candidate: result,
	}
	key, err := joinAcceptanceIdempotencyKey(acceptance)
	if err != nil {
		return JoinResult{}, ErrInvalidSchedule
	}
	acceptance.IdempotencyKey = key
	accepted, authorityErr := scheduler.authority.AcceptJoin(ctx, acceptance)
	if authorityErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return JoinResult{}, ctxErr
		}
		return JoinResult{}, fmt.Errorf("%w: %v", ErrAuthorityUnavailable, authorityErr)
	}
	switch accepted.Verdict {
	case NativeTicketAccepted:
	case NativeTicketUnavailable:
		return JoinResult{}, ErrAuthorityUnavailable
	default:
		return JoinResult{}, ErrInvalidSchedule
	}
	data, err := json.Marshal(accepted.Join)
	if err != nil {
		return JoinResult{}, ErrInvalidSchedule
	}
	candidateData, err := json.Marshal(result)
	if err != nil || !bytes.Equal(data, candidateData) {
		return JoinResult{}, ErrInvalidSchedule
	}
	if err := contracts.Validate(ctx, contracts.OrchestrationURI+"#/$defs/delegationJoin", data, false); err != nil {
		return JoinResult{}, fmt.Errorf("%w: authority join: %v", ErrInvalidSchedule, err)
	}
	var detached JoinResult
	if err := json.Unmarshal(data, &detached); err != nil {
		return JoinResult{}, ErrInvalidSchedule
	}
	return detached, nil
}

func buildJoinResult(ctx context.Context, plan navigator.Plan, status ScheduleStatus, tasks map[string]JoinedTask) (JoinResult, error) {
	joined := make([]JoinedTask, 0, len(tasks))
	result := JoinResult{Kind: "delegation.join", SchemaVersion: navigator.SchemaVersion, PlanID: plan.PlanID}
	for _, execution := range tasks {
		if execution.Status == TaskRunning || execution.Status == TaskPending {
			continue
		}
		joined = append(joined, execution)
		switch execution.Status {
		case TaskCompleted:
			result.Completed++
		case TaskFailed:
			result.Failed++
		case TaskCancelled:
			result.Cancelled++
		}
	}
	sort.Slice(joined, func(i, j int) bool { return joined[i].TaskID < joined[j].TaskID })
	result.Tasks = joined
	switch {
	case status == ScheduleCompleted:
		result.Status = "completed"
	case result.Completed > 0:
		result.Status = "partial"
	case status == ScheduleCancelled:
		result.Status = "cancelled"
	default:
		result.Status = "failed"
	}
	data, err := json.Marshal(result)
	if err != nil {
		return JoinResult{}, ErrInvalidSchedule
	}
	if err := contracts.Validate(ctx, contracts.OrchestrationURI+"#/$defs/delegationJoin", data, false); err != nil {
		return JoinResult{}, fmt.Errorf("%w: generated join: %v", ErrInvalidSchedule, err)
	}
	return result, nil
}

func (scheduler *WaveScheduler) advanceIfWaveTerminal() error {
	wave := scheduler.plan.Waves[scheduler.currentWave]
	failed, cancelled := false, false
	for _, taskID := range wave.TaskIDs {
		execution, ok := scheduler.tasks[taskID]
		if !ok || execution.Status == TaskRunning {
			return nil
		}
		failed = failed || execution.Status == TaskFailed
		cancelled = cancelled || execution.Status == TaskCancelled
	}
	if failed {
		scheduler.status = ScheduleFailed
		return nil
	}
	if cancelled {
		scheduler.status = ScheduleCancelled
		return nil
	}
	scheduler.currentWave++
	if scheduler.currentWave == len(scheduler.plan.Waves) {
		scheduler.status = ScheduleCompleted
	} else {
		scheduler.status = SchedulePending
	}
	return nil
}

func validatePlanTopology(plan navigator.Plan) error {
	tasks := make(map[string]navigator.Task, len(plan.Tasks))
	for _, task := range plan.Tasks {
		if _, duplicate := tasks[task.TaskID]; duplicate {
			return ErrInvalidSchedule
		}
		tasks[task.TaskID] = task
	}
	waveByTask := make(map[string]int, len(tasks))
	for index, wave := range plan.Waves {
		if wave.Index != index || wave.WaveID != fmt.Sprintf("wave-%d", index+1) || wave.Mode != "sequential" && wave.Mode != "parallel" || wave.Mode == "parallel" != (len(wave.TaskIDs) > 1) || len(wave.TaskIDs) > plan.MaxParallel {
			return ErrInvalidSchedule
		}
		for _, taskID := range wave.TaskIDs {
			task, ok := tasks[taskID]
			if !ok {
				return ErrInvalidSchedule
			}
			if _, duplicate := waveByTask[taskID]; duplicate {
				return ErrInvalidSchedule
			}
			if wave.Mode == "parallel" && !navigator.IsParallelSafeTask(task) {
				return ErrInvalidSchedule
			}
			waveByTask[taskID] = index
		}
	}
	if len(waveByTask) != len(tasks) {
		return ErrInvalidSchedule
	}
	for taskID, task := range tasks {
		for _, dependency := range task.DependsOn {
			dependencyWave, ok := waveByTask[dependency]
			if !ok || dependencyWave >= waveByTask[taskID] {
				return ErrInvalidSchedule
			}
		}
	}
	return nil
}

func validIdentity(value string) bool {
	return value != "" && len(value) <= 240 && nativeIdentity.MatchString(value)
}
