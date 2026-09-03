package runtime

import (
	"reflect"
	"testing"

	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/syncservice"
)

const (
	planProjectID = "11111111-1111-4111-8111-111111111111"
	planHistoryID = "22222222-2222-4222-8222-222222222222"
)

func TestPlanSyncProjectOrderedMatrix(t *testing.T) {
	validBound := func() memory.SyncProjectTopology {
		return memory.SyncProjectTopology{
			SchemaVersion: 1, PortableProjectID: planProjectID,
			WorkspaceProjectID: "local-project", WorkspacePortableProjectID: planProjectID, BoundProjectID: "local-project",
		}
	}
	active := syncservice.ProjectState{Status: syncservice.ProjectStateActive, HasHistory: true, HistoryGeneration: planHistoryID, Watermark: 4}
	cases := []struct {
		name     string
		topology memory.SyncProjectTopology
		remote   syncservice.ProjectState
		want     SyncProjectPlan
	}{
		{"unsupported schema", memory.SyncProjectTopology{SchemaVersion: 2, PortableProjectID: planProjectID}, syncservice.ProjectState{}, SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionBlockedManual, Reason: SyncPlanReasonUnsupportedTopologySchema}},
		{"invalid local precedes remote", memory.SyncProjectTopology{SchemaVersion: 1, PortableProjectID: "bad"}, syncservice.ProjectState{Status: "bad"}, SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionBlockedManual, Reason: SyncPlanReasonInvalidLocalTopology}},
		{"invalid remote", memory.SyncProjectTopology{SchemaVersion: 1, PortableProjectID: planProjectID}, syncservice.ProjectState{Status: "bad"}, SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionBlockedManual, Reason: SyncPlanReasonInvalidRemoteState}},
		{"unbound absent", memory.SyncProjectTopology{SchemaVersion: 1, PortableProjectID: planProjectID}, syncservice.ProjectState{Status: syncservice.ProjectStateAbsent}, SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionNoOp, Reason: SyncPlanReasonRemoteAbsent}},
		{"unbound workspace root absent", memory.SyncProjectTopology{SchemaVersion: 1, PortableProjectID: planProjectID, WorkspaceProjectID: "local-project"}, syncservice.ProjectState{Status: syncservice.ProjectStateAbsent}, SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionNoOp, Reason: SyncPlanReasonRemoteAbsent}},
		{"bound absent blocks", validBound(), syncservice.ProjectState{Status: syncservice.ProjectStateAbsent}, SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionBlockedManual, Reason: SyncPlanReasonRemoteAbsentBound}},
		{"active foreground", validBound(), active, SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionForegroundSync, Reason: SyncPlanReasonBoundActive}},
		{"active completed transition foreground", func() memory.SyncProjectTopology {
			v := validBound()
			v.HasTransition = true
			v.Transition = memory.SyncProjectTransitionResult{SchemaVersion: 1, Mode: memory.SyncProjectTransitionRejoinMerge, Status: memory.SyncProjectTransitionCompleted, TransitionIdentity: 1}
			return v
		}(), active, SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionForegroundSync, Reason: SyncPlanReasonBoundActive}},
		{"resume pulling", func() memory.SyncProjectTopology {
			v := validBound()
			v.HasTransition = true
			v.Transition = memory.SyncProjectTransitionResult{SchemaVersion: 1, Mode: memory.SyncProjectTransitionRejoinMerge, Status: memory.SyncProjectTransitionPulling, TransitionIdentity: 1}
			return v
		}(), active, SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionResumeTransition, Reason: SyncPlanReasonActiveTransition, TransitionMode: memory.SyncProjectTransitionRejoinMerge, transitionIdentity: 1}},
		{"resume publishing", func() memory.SyncProjectTopology {
			v := validBound()
			v.HasTransition = true
			v.Transition = memory.SyncProjectTransitionResult{SchemaVersion: 1, Mode: memory.SyncProjectTransitionReseedSource, Status: memory.SyncProjectTransitionPublishing, TransitionIdentity: 1}
			return v
		}(), active, SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionResumeTransition, Reason: SyncPlanReasonActiveTransition, TransitionMode: memory.SyncProjectTransitionReseedSource, transitionIdentity: 1}},
		{"resume pulling before remote absent", func() memory.SyncProjectTopology {
			v := validBound()
			v.HasTransition = true
			v.Transition = memory.SyncProjectTransitionResult{SchemaVersion: 1, Mode: memory.SyncProjectTransitionRejoinMerge, Status: memory.SyncProjectTransitionPulling, TransitionIdentity: 1}
			return v
		}(), syncservice.ProjectState{Status: syncservice.ProjectStateAbsent}, SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionResumeTransition, Reason: SyncPlanReasonActiveTransition, TransitionMode: memory.SyncProjectTransitionRejoinMerge, transitionIdentity: 1}},
		{"resume publishing before cursor mismatch", func() memory.SyncProjectTopology {
			v := validBound()
			v.HasCursor = true
			v.Cursor = syncservice.Cursor{HistoryID: "33333333-3333-4333-8333-333333333333", Position: 1, Watermark: 1}
			v.HasTransition = true
			v.Transition = memory.SyncProjectTransitionResult{SchemaVersion: 1, Mode: memory.SyncProjectTransitionReseedSource, Status: memory.SyncProjectTransitionPublishing, TransitionIdentity: 1}
			return v
		}(), active, SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionResumeTransition, Reason: SyncPlanReasonActiveTransition, TransitionMode: memory.SyncProjectTransitionReseedSource, transitionIdentity: 1}},
		{"resume pulling before cursor ahead", func() memory.SyncProjectTopology {
			v := validBound()
			v.HasCursor = true
			v.Cursor = syncservice.Cursor{HistoryID: planHistoryID, Position: 5, Watermark: 5}
			v.HasTransition = true
			v.Transition = memory.SyncProjectTransitionResult{SchemaVersion: 1, Mode: memory.SyncProjectTransitionRejoinMerge, Status: memory.SyncProjectTransitionPulling, TransitionIdentity: 1}
			return v
		}(), active, SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionResumeTransition, Reason: SyncPlanReasonActiveTransition, TransitionMode: memory.SyncProjectTransitionRejoinMerge, transitionIdentity: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PlanSyncProject(tc.topology, tc.remote); got != tc.want {
				t.Fatalf("PlanSyncProject() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestPlanSyncProjectRejectsCorruptionAndCursorEdges(t *testing.T) {
	bound := memory.SyncProjectTopology{SchemaVersion: 1, PortableProjectID: planProjectID, WorkspaceProjectID: "local-project", WorkspacePortableProjectID: planProjectID, BoundProjectID: "local-project"}
	active := syncservice.ProjectState{Status: syncservice.ProjectStateActive, HasHistory: true, HistoryGeneration: planHistoryID, Watermark: 4}
	cases := []struct {
		name     string
		topology memory.SyncProjectTopology
		remote   syncservice.ProjectState
		reason   SyncPlanReason
	}{
		{"partial binding", memory.SyncProjectTopology{SchemaVersion: 1, PortableProjectID: planProjectID, BoundProjectID: "local-project"}, active, SyncPlanReasonBindingMismatch},
		{"mismatched workspace portable", memory.SyncProjectTopology{SchemaVersion: 1, PortableProjectID: planProjectID, WorkspaceProjectID: "local-project", WorkspacePortableProjectID: "33333333-3333-4333-8333-333333333333", BoundProjectID: "local-project"}, active, SyncPlanReasonBindingMismatch},
		{"other workspace portable", memory.SyncProjectTopology{SchemaVersion: 1, PortableProjectID: planProjectID, WorkspaceProjectID: "local-project", WorkspacePortableProjectID: "33333333-3333-4333-8333-333333333333"}, active, SyncPlanReasonBindingMismatch},
		{"workspace portable without root", memory.SyncProjectTopology{SchemaVersion: 1, PortableProjectID: planProjectID, WorkspacePortableProjectID: planProjectID}, active, SyncPlanReasonInvalidLocalTopology},
		{"uppercase workspace portable", memory.SyncProjectTopology{SchemaVersion: 1, PortableProjectID: planProjectID, WorkspaceProjectID: "local-project", WorkspacePortableProjectID: "11111111-1111-4111-8111-11111111111A"}, active, SyncPlanReasonInvalidLocalTopology},
		{"cursor without binding", memory.SyncProjectTopology{SchemaVersion: 1, PortableProjectID: planProjectID, HasCursor: true, Cursor: syncservice.Cursor{HistoryID: planHistoryID}}, active, SyncPlanReasonInvalidLocalTopology},
		{"invalid cursor", func() memory.SyncProjectTopology {
			v := bound
			v.HasCursor = true
			v.Cursor = syncservice.Cursor{HistoryID: "bad"}
			return v
		}(), active, SyncPlanReasonInvalidLocalTopology},
		{"uppercase cursor history", func() memory.SyncProjectTopology {
			v := bound
			v.HasCursor = true
			v.Cursor = syncservice.Cursor{HistoryID: "22222222-2222-4222-8222-22222222222A"}
			return v
		}(), active, SyncPlanReasonInvalidLocalTopology},
		{"history mismatch", func() memory.SyncProjectTopology {
			v := bound
			v.HasCursor = true
			v.Cursor = syncservice.Cursor{HistoryID: "33333333-3333-4333-8333-333333333333", Position: 1, Watermark: 1}
			return v
		}(), active, SyncPlanReasonCursorHistoryMismatch},
		{"cursor ahead", func() memory.SyncProjectTopology {
			v := bound
			v.HasCursor = true
			v.Cursor = syncservice.Cursor{HistoryID: planHistoryID, Position: 5, Watermark: 5}
			return v
		}(), active, SyncPlanReasonCursorAheadRemote},
		{"cursor watermark ahead", func() memory.SyncProjectTopology {
			v := bound
			v.HasCursor = true
			v.Cursor = syncservice.Cursor{HistoryID: planHistoryID, Position: 4, Watermark: 5}
			return v
		}(), active, SyncPlanReasonCursorAheadRemote},
		{"invalid transition", func() memory.SyncProjectTopology {
			v := bound
			v.HasTransition = true
			v.Transition = memory.SyncProjectTransitionResult{SchemaVersion: 1, Mode: "bad", Status: memory.SyncProjectTransitionPulling}
			return v
		}(), active, SyncPlanReasonInvalidLocalTopology},
		{"transition without binding", memory.SyncProjectTopology{SchemaVersion: 1, PortableProjectID: planProjectID, HasTransition: true, Transition: memory.SyncProjectTransitionResult{SchemaVersion: 1, Mode: memory.SyncProjectTransitionRejoinMerge, Status: memory.SyncProjectTransitionPulling}}, active, SyncPlanReasonInvalidLocalTopology},
		{"remote history corrupt", bound, syncservice.ProjectState{Status: syncservice.ProjectStateActive, HasHistory: true, HistoryGeneration: "bad"}, SyncPlanReasonInvalidRemoteState},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PlanSyncProject(tc.topology, tc.remote)
			if got.Action != SyncPlanActionBlockedManual || got.Reason != tc.reason || got.TransitionMode != "" {
				t.Fatalf("PlanSyncProject() = %#v", got)
			}
		})
	}
}

func TestPlanSyncProjectIsDeterministicAndDoesNotMutateInputs(t *testing.T) {
	topology := memory.SyncProjectTopology{SchemaVersion: 1, PortableProjectID: planProjectID, WorkspaceProjectID: "local-project", WorkspacePortableProjectID: planProjectID, BoundProjectID: "local-project", HasCursor: true, Cursor: syncservice.Cursor{HistoryID: planHistoryID, Position: 4, Watermark: 4}}
	remote := syncservice.ProjectState{Status: syncservice.ProjectStateActive, HasHistory: true, HistoryGeneration: planHistoryID, Watermark: 4}
	beforeTopology, beforeRemote := topology, remote
	first := PlanSyncProject(topology, remote)
	if second := PlanSyncProject(topology, remote); second != first {
		t.Fatalf("non-deterministic result: %#v then %#v", first, second)
	}
	if !reflect.DeepEqual(topology, beforeTopology) || !reflect.DeepEqual(remote, beforeRemote) {
		t.Fatal("PlanSyncProject mutated an input")
	}
}

func TestSyncPlanSchemaValues(t *testing.T) {
	if SyncPlanActionNoOp != "no_op" || SyncPlanActionForegroundSync != "foreground_sync" || SyncPlanActionResumeTransition != "resume_transition" || SyncPlanActionBlockedManual != "blocked_manual" {
		t.Fatal("sync plan action values changed")
	}
	if SyncPlanReasonUnsupportedTopologySchema != "unsupported_topology_schema" || SyncPlanReasonInvalidLocalTopology != "invalid_local_topology" || SyncPlanReasonInvalidRemoteState != "invalid_remote_state" || SyncPlanReasonBindingMismatch != "binding_mismatch" || SyncPlanReasonCursorHistoryMismatch != "cursor_history_mismatch" || SyncPlanReasonCursorAheadRemote != "cursor_ahead_remote" || SyncPlanReasonRemoteAbsent != "remote_absent" || SyncPlanReasonRemoteAbsentBound != "remote_absent_bound" || SyncPlanReasonBoundActive != "bound_active" || SyncPlanReasonActiveTransition != "active_transition" {
		t.Fatal("sync plan reason values changed")
	}
}
