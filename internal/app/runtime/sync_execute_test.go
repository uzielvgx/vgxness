package runtime

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
)

func TestExecuteSyncProject(t *testing.T) {
	opts := config.Options{ProjectDir: "C:\\project"}
	foreground := SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionForegroundSync, Reason: SyncPlanReasonBoundActive}
	noOp := SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionNoOp, Reason: SyncPlanReasonRemoteAbsent}
	blocked := SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionBlockedManual, Reason: SyncPlanReasonBindingMismatch}
	resume := SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionResumeTransition, Reason: SyncPlanReasonActiveTransition, TransitionMode: memory.SyncProjectTransitionRejoinMerge, transitionIdentity: 1}
	reseedResume := SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionResumeTransition, Reason: SyncPlanReasonActiveTransition, TransitionMode: memory.SyncProjectTransitionReseedSource, transitionIdentity: 2}
	syncResult := memory.SyncResult{Status: memory.SyncStatusDisabled, Pushed: 2}
	transitionResult := memory.SyncProjectTransitionResult{SchemaVersion: 1, Mode: memory.SyncProjectTransitionRejoinMerge, Status: memory.SyncProjectTransitionPulling}
	planErr := errors.New("plan failed")
	syncErr := errors.New("sync failed")

	for _, test := range []struct {
		name            string
		ctx             context.Context
		plan            SyncProjectPlan
		planErr         error
		syncErr         error
		resumeErr       error
		want            AutoSyncProjectResult
		wantErr         error
		wantCalls       int
		wantResumeCalls int
	}{
		{name: "planner error", ctx: context.Background(), planErr: planErr, wantErr: planErr},
		{name: "no op", ctx: context.Background(), plan: noOp, want: AutoSyncProjectResult{SchemaVersion: 1, Plan: noOp}},
		{name: "blocked", ctx: context.Background(), plan: blocked, want: AutoSyncProjectResult{SchemaVersion: 1, Plan: blocked}},
		{name: "cancelled resume", ctx: cancelledContext(), plan: resume, want: AutoSyncProjectResult{SchemaVersion: 1, Plan: resume}, wantErr: context.Canceled},
		{name: "resume", ctx: context.Background(), plan: resume, want: AutoSyncProjectResult{SchemaVersion: 1, Plan: resume, Attempted: true, Transition: transitionResult}, wantResumeCalls: 1},
		{name: "reseed source resume", ctx: context.Background(), plan: reseedResume, want: AutoSyncProjectResult{SchemaVersion: 1, Plan: reseedResume, Attempted: true, Transition: transitionResult}, wantResumeCalls: 1},
		{name: "resume error preserves result", ctx: context.Background(), plan: resume, resumeErr: syncErr, want: AutoSyncProjectResult{SchemaVersion: 1, Plan: resume, Attempted: true, Transition: transitionResult}, wantErr: syncErr, wantResumeCalls: 1},
		{name: "invalid plan", ctx: context.Background(), plan: SyncProjectPlan{SchemaVersion: 2, Action: SyncPlanActionForegroundSync, Reason: SyncPlanReasonBoundActive}, wantErr: memory.ErrCorrupt},
		{name: "unknown action", ctx: context.Background(), plan: SyncProjectPlan{SchemaVersion: 1, Action: "unknown", Reason: SyncPlanReasonBoundActive}, wantErr: memory.ErrCorrupt},
		{name: "impossible plan shape", ctx: context.Background(), plan: SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionForegroundSync, Reason: SyncPlanReasonBoundActive, TransitionMode: memory.SyncProjectTransitionRejoinMerge}, wantErr: memory.ErrCorrupt},
		{name: "foreground wrong reason", ctx: context.Background(), plan: SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionForegroundSync, Reason: SyncPlanReasonRemoteAbsent}, wantErr: memory.ErrCorrupt},
		{name: "no op wrong reason", ctx: context.Background(), plan: SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionNoOp, Reason: SyncPlanReasonBoundActive}, wantErr: memory.ErrCorrupt},
		{name: "resume wrong reason", ctx: context.Background(), plan: SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionResumeTransition, Reason: SyncPlanReasonBoundActive, TransitionMode: memory.SyncProjectTransitionRejoinMerge}, wantErr: memory.ErrCorrupt},
		{name: "resume unknown mode", ctx: context.Background(), plan: SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionResumeTransition, Reason: SyncPlanReasonActiveTransition, TransitionMode: "unknown"}, wantErr: memory.ErrCorrupt},
		{name: "blocked non-blocking reason", ctx: context.Background(), plan: SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionBlockedManual, Reason: SyncPlanReasonBoundActive}, wantErr: memory.ErrCorrupt},
		{name: "blocked unknown reason", ctx: context.Background(), plan: SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionBlockedManual, Reason: "unknown"}, wantErr: memory.ErrCorrupt},
		{name: "cancelled foreground", ctx: cancelledContext(), plan: foreground, want: AutoSyncProjectResult{SchemaVersion: 1, Plan: foreground}, wantErr: context.Canceled},
		{name: "foreground success", ctx: context.Background(), plan: foreground, want: AutoSyncProjectResult{SchemaVersion: 1, Plan: foreground, Attempted: true, Sync: syncResult}, wantCalls: 1},
		{name: "foreground error preserves result", ctx: context.Background(), plan: foreground, syncErr: syncErr, want: AutoSyncProjectResult{SchemaVersion: 1, Plan: foreground, Attempted: true, Sync: syncResult}, wantErr: syncErr, wantCalls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			plannerCalls, dispatcherCalls, resumeCalls := 0, 0, 0
			got, err := executeSyncProject(test.ctx, opts, func(ctx context.Context, gotOpts config.Options) (SyncProjectPlan, error) {
				plannerCalls++
				if ctx != test.ctx || !reflect.DeepEqual(gotOpts, opts) {
					t.Fatal("planner inputs changed")
				}
				return test.plan, test.planErr
			}, func(ctx context.Context, gotOpts config.Options) (memory.SyncResult, error) {
				dispatcherCalls++
				if ctx != test.ctx || !reflect.DeepEqual(gotOpts, opts) {
					t.Fatal("dispatcher inputs changed")
				}
				return syncResult, test.syncErr
			}, func(ctx context.Context, gotOpts config.Options, mode memory.SyncProjectTransitionMode, identity int64) (memory.SyncProjectTransitionResult, error) {
				resumeCalls++
				if ctx != test.ctx || !reflect.DeepEqual(gotOpts, opts) || mode != test.plan.TransitionMode || identity != test.plan.transitionIdentity {
					t.Fatal("resume inputs changed")
				}
				return transitionResult, test.resumeErr
			})
			if plannerCalls != 1 || dispatcherCalls != test.wantCalls || resumeCalls != test.wantResumeCalls {
				t.Fatalf("calls = planner %d, dispatcher %d, resume %d; want 1, %d, %d", plannerCalls, dispatcherCalls, resumeCalls, test.wantCalls, test.wantResumeCalls)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("result = %#v; want %#v", got, test.want)
			}
			if test.wantErr == nil {
				if err != nil {
					t.Fatalf("error = %v; want nil", err)
				}
			} else if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v; want %v", err, test.wantErr)
			}
		})
	}
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
