package orchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDurableTicketAuthorityRecoversAcrossInstancesAndTakeover(t *testing.T) {
	root := t.TempDir()
	plan := schedulerPlan(t, schedulerRead("task-a"), schedulerRead("task-b"))
	a, b := binding("task-a", "ses-child-a"), binding("task-b", "ses-child-b")
	authority, err := NewDurableTicketAuthority(root, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.RegisterPlan(context.Background(), "schedule-1", plan, []NativeTaskBinding{a, b}); err != nil {
		t.Fatal(err)
	}
	first, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"), authority)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a, b}, successfulDispatch); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewDurableTicketAuthority(root, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	second, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-2"), reopened)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status() != ScheduleRunning {
		t.Fatalf("takeover did not adopt running checkpoint: %s", second.Status())
	}
	if err := second.Record(context.Background(), completed(a)); err != nil {
		t.Fatal(err)
	}
	if err := second.Record(context.Background(), completed(b)); err != nil {
		t.Fatal(err)
	}
	join, err := second.Join(context.Background())
	if err != nil || join.Status != "completed" || join.Completed != 2 {
		t.Fatalf("join=%#v err=%v", join, err)
	}

	thirdAuthority, err := NewDurableTicketAuthority(root, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	third, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-3"), thirdAuthority)
	if err != nil {
		t.Fatal(err)
	}
	if third.Status() != ScheduleCompleted {
		t.Fatalf("completed checkpoint was not durable: %s", third.Status())
	}
	replayed, err := third.Join(context.Background())
	if err != nil || replayed.PlanID != join.PlanID || replayed.Completed != join.Completed {
		t.Fatalf("replayed join=%#v err=%v", replayed, err)
	}
}

func TestDurableTicketAuthorityFencesTakeoverUntilLateCallbackTerminates(t *testing.T) {
	root := t.TempDir()
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority, err := NewDurableTicketAuthority(root, 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.RegisterPlan(context.Background(), "schedule-1", plan, []NativeTaskBinding{a}); err != nil {
		t.Fatal(err)
	}
	scheduler, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"), authority)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	dispatch := func(context.Context, NativeTaskBinding) NativeDispatchResult {
		<-release
		return NativeDispatchResult{Status: NativeDispatchConfirmed}
	}
	if err := scheduler.StartWave(context.Background(), "wave-1", []NativeTaskBinding{a}, dispatch); !errors.Is(err, ErrNativeDispatch) {
		t.Fatalf("timed out dispatch err=%v", err)
	}

	reopened, err := NewDurableTicketAuthority(root, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-2"), reopened); !errors.Is(err, ErrAuthorityUnavailable) {
		t.Fatalf("takeover was not fenced while callback lived: %v", err)
	}
	if err := reopened.ResolveUncertain(context.Background(), "schedule-1"); !errors.Is(err, ErrAuthorityUnavailable) {
		t.Fatalf("live callback was cleared as crash-left: %v", err)
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err = openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-2"), reopened)
		if err == nil {
			break
		}
		if !errors.Is(err, ErrAuthorityUnavailable) || time.Now().After(deadline) {
			t.Fatalf("takeover did not recover after callback termination: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDurableTicketAuthorityRejectsCrossScheduleTicketAlias(t *testing.T) {
	root := t.TempDir()
	plan := schedulerPlan(t, schedulerRead("task-a"))
	authority, err := NewDurableTicketAuthority(root, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	a := binding("task-a", "ses-child-a")
	if err := authority.RegisterPlan(context.Background(), "schedule-1", plan, []NativeTaskBinding{a}); err != nil {
		t.Fatal(err)
	}
	alias := a
	alias.ChildSessionID = "ses-child-other"
	if err := authority.RegisterPlan(context.Background(), "schedule-2", plan, []NativeTaskBinding{alias}); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("cross-schedule ticket alias accepted: %v", err)
	}
}

func TestDurableTicketAuthorityFailsClosedOnCorruptState(t *testing.T) {
	root := t.TempDir()
	plan := schedulerPlan(t, schedulerRead("task-a"))
	a := binding("task-a", "ses-child-a")
	authority, err := NewDurableTicketAuthority(root, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.RegisterPlan(context.Background(), "schedule-1", plan, []NativeTaskBinding{a}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "delegation-authority", "state.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openScheduler(context.Background(), plan, plan.PlanID, scheduleIdentity("schedule-1", "owner-1"), authority); !errors.Is(err, ErrAuthorityUnavailable) {
		t.Fatalf("corrupt authority did not fail closed: %v", err)
	}
}
