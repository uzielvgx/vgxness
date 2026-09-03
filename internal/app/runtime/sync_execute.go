package runtime

import (
	"context"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
)

// AutoSyncProjectResult records a schema-v1 recommendation and the selected
// foreground or resume dispatcher attempt.
type AutoSyncProjectResult struct {
	SchemaVersion int
	Plan          SyncProjectPlan
	// Attempted means the selected foreground or resume dispatcher was invoked,
	// not that remote I/O occurred.
	Attempted  bool
	Sync       memory.SyncResult
	Transition memory.SyncProjectTransitionResult
}

// AutoSyncProject plans the current project state before considering a bounded
// foreground synchronization or the active transition selected by its plan.
func (runtime Memory) AutoSyncProject(ctx context.Context, opts config.Options) (AutoSyncProjectResult, error) {
	return executeSyncProject(ctx, opts, runtime.PlanSyncProject, runtime.Sync, func(ctx context.Context, opts config.Options, mode memory.SyncProjectTransitionMode, identity int64) (memory.SyncProjectTransitionResult, error) {
		return runtime.resumeSyncProjectTransition(ctx, opts, opts.ProjectDir, mode, identity)
	})
}

type syncProjectPlanner func(context.Context, config.Options) (SyncProjectPlan, error)
type syncProjectDispatcher func(context.Context, config.Options) (memory.SyncResult, error)
type syncProjectResumeDispatcher func(context.Context, config.Options, memory.SyncProjectTransitionMode, int64) (memory.SyncProjectTransitionResult, error)

func executeSyncProject(ctx context.Context, opts config.Options, planProject syncProjectPlanner, dispatch syncProjectDispatcher, resume syncProjectResumeDispatcher) (AutoSyncProjectResult, error) {
	plan, err := planProject(ctx, opts)
	if err != nil {
		return AutoSyncProjectResult{}, err
	}
	if !validSyncExecutionPlan(plan) {
		return AutoSyncProjectResult{}, memory.ErrCorrupt
	}
	result := AutoSyncProjectResult{SchemaVersion: 1, Plan: plan}
	switch plan.Action {
	case SyncPlanActionNoOp, SyncPlanActionBlockedManual:
		return result, nil
	case SyncPlanActionResumeTransition:
		if err := ctx.Err(); err != nil {
			return result, err
		}
		result.Attempted = true
		result.Transition, err = resume(ctx, opts, plan.TransitionMode, plan.transitionIdentity)
		return result, err
	case SyncPlanActionForegroundSync:
		if err := ctx.Err(); err != nil {
			return result, err
		}
		result.Attempted = true
		result.Sync, err = dispatch(ctx, opts)
		return result, err
	default:
		return AutoSyncProjectResult{}, memory.ErrCorrupt
	}
}

func validSyncExecutionPlan(plan SyncProjectPlan) bool {
	if plan.SchemaVersion != 1 {
		return false
	}
	switch plan.Action {
	case SyncPlanActionNoOp:
		return plan.Reason == SyncPlanReasonRemoteAbsent && plan.TransitionMode == ""
	case SyncPlanActionForegroundSync:
		return plan.Reason == SyncPlanReasonBoundActive && plan.TransitionMode == ""
	case SyncPlanActionResumeTransition:
		return plan.Reason == SyncPlanReasonActiveTransition && plan.transitionIdentity > 0 && (plan.TransitionMode == memory.SyncProjectTransitionReseedSource || plan.TransitionMode == memory.SyncProjectTransitionRejoinMerge)
	case SyncPlanActionBlockedManual:
		if plan.TransitionMode != "" {
			return false
		}
		switch plan.Reason {
		case SyncPlanReasonUnsupportedTopologySchema, SyncPlanReasonInvalidLocalTopology, SyncPlanReasonInvalidRemoteState, SyncPlanReasonBindingMismatch, SyncPlanReasonCursorHistoryMismatch, SyncPlanReasonCursorAheadRemote, SyncPlanReasonRemoteAbsentBound:
			return true
		default:
			return false
		}
	default:
		return false
	}
}
