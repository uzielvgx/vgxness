package runtime

import (
	"regexp"

	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/syncservice"
)

// SyncPlanAction identifies the sole recommendation produced by PlanSyncProject.
type SyncPlanAction string

const (
	SyncPlanActionNoOp             SyncPlanAction = "no_op"
	SyncPlanActionForegroundSync   SyncPlanAction = "foreground_sync"
	SyncPlanActionResumeTransition SyncPlanAction = "resume_transition"
	SyncPlanActionBlockedManual    SyncPlanAction = "blocked_manual"
)

// SyncPlanReason records why a sync recommendation was selected.
type SyncPlanReason string

const (
	SyncPlanReasonUnsupportedTopologySchema SyncPlanReason = "unsupported_topology_schema"
	SyncPlanReasonInvalidLocalTopology      SyncPlanReason = "invalid_local_topology"
	SyncPlanReasonInvalidRemoteState        SyncPlanReason = "invalid_remote_state"
	SyncPlanReasonBindingMismatch           SyncPlanReason = "binding_mismatch"
	SyncPlanReasonCursorHistoryMismatch     SyncPlanReason = "cursor_history_mismatch"
	SyncPlanReasonCursorAheadRemote         SyncPlanReason = "cursor_ahead_remote"
	SyncPlanReasonRemoteAbsent              SyncPlanReason = "remote_absent"
	SyncPlanReasonRemoteAbsentBound         SyncPlanReason = "remote_absent_bound"
	SyncPlanReasonBoundActive               SyncPlanReason = "bound_active"
	SyncPlanReasonActiveTransition          SyncPlanReason = "active_transition"
)

// SyncProjectPlan is a schema-v1 side-effect-free sync recommendation.
type SyncProjectPlan struct {
	SchemaVersion      int
	Action             SyncPlanAction
	Reason             SyncPlanReason
	TransitionMode     memory.SyncProjectTransitionMode
	transitionIdentity int64
}

var canonicalSyncPlanUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// PlanSyncProject derives a fail-closed recommendation from supplied snapshots.
// It neither reads nor changes state, and callers must revalidate any action.
func PlanSyncProject(local memory.SyncProjectTopology, remote syncservice.ProjectState) SyncProjectPlan {
	blocked := func(reason SyncPlanReason) SyncProjectPlan {
		return SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionBlockedManual, Reason: reason}
	}
	if local.SchemaVersion != 1 {
		return blocked(SyncPlanReasonUnsupportedTopologySchema)
	}
	if !validSyncPlanTopology(local) {
		return blocked(SyncPlanReasonInvalidLocalTopology)
	}
	if syncservice.ValidateProjectState(remote) != nil {
		return blocked(SyncPlanReasonInvalidRemoteState)
	}
	bound := local.BoundProjectID != ""
	if bound && (local.WorkspaceProjectID != local.BoundProjectID || local.WorkspacePortableProjectID != local.PortableProjectID) {
		return blocked(SyncPlanReasonBindingMismatch)
	}
	if local.HasTransition && local.Transition.Status != memory.SyncProjectTransitionCompleted {
		return SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionResumeTransition, Reason: SyncPlanReasonActiveTransition, TransitionMode: local.Transition.Mode, transitionIdentity: local.Transition.TransitionIdentity}
	}
	if !bound {
		if local.WorkspacePortableProjectID != "" {
			return blocked(SyncPlanReasonBindingMismatch)
		}
		if remote.Status == syncservice.ProjectStateAbsent {
			return SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionNoOp, Reason: SyncPlanReasonRemoteAbsent}
		}
		return blocked(SyncPlanReasonBindingMismatch)
	}
	if remote.Status == syncservice.ProjectStateAbsent {
		return blocked(SyncPlanReasonRemoteAbsentBound)
	}
	if local.HasCursor {
		if local.Cursor.HistoryID != remote.HistoryGeneration {
			return blocked(SyncPlanReasonCursorHistoryMismatch)
		}
		if local.Cursor.Position > remote.Watermark || local.Cursor.Watermark > remote.Watermark {
			return blocked(SyncPlanReasonCursorAheadRemote)
		}
	}
	return SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionForegroundSync, Reason: SyncPlanReasonBoundActive}
}

func validSyncPlanTopology(value memory.SyncProjectTopology) bool {
	if !canonicalSyncPlanUUID.MatchString(value.PortableProjectID) {
		return false
	}
	bound := value.BoundProjectID != ""
	if value.WorkspacePortableProjectID != "" && (value.WorkspaceProjectID == "" || !canonicalSyncPlanUUID.MatchString(value.WorkspacePortableProjectID)) {
		return false
	}
	if !bound && (value.HasCursor || value.HasTransition) {
		return false
	}
	if value.HasCursor && (syncservice.ValidateCursor(value.Cursor) != nil || !canonicalSyncPlanUUID.MatchString(value.Cursor.HistoryID) || value.Cursor.Watermark < value.Cursor.Position) {
		return false
	}
	if !value.HasCursor && value.Cursor != (syncservice.Cursor{}) {
		return false
	}
	if !value.HasTransition {
		return value.Transition == (memory.SyncProjectTransitionResult{})
	}
	transition := value.Transition
	if transition.SchemaVersion != 1 || (transition.Mode != memory.SyncProjectTransitionReseedSource && transition.Mode != memory.SyncProjectTransitionRejoinMerge) || (transition.Status != memory.SyncProjectTransitionPulling && transition.Status != memory.SyncProjectTransitionPublishing && transition.Status != memory.SyncProjectTransitionCompleted) || (transition.Mode == memory.SyncProjectTransitionReseedSource && transition.Status == memory.SyncProjectTransitionPulling) {
		return false
	}
	return transition.TransitionIdentity > 0 && transition.Projects == 0 && transition.Sessions == 0 && transition.Observations == 0 && transition.Queued == 0
}
