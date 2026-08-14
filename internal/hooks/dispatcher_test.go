package hooks

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDispatcherZeroListenersIsNoOp(t *testing.T) {
	var dispatcher Dispatcher
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := dispatcher.Dispatch(ctx, testEvent(t)); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
}

func TestDispatcherRejectsNilContextBeforeZeroListenerNoOp(t *testing.T) {
	var dispatcher Dispatcher
	if err := dispatcher.Dispatch(nil, testEvent(t)); !errors.Is(err, ErrNilContext) {
		t.Fatalf("Dispatch(nil) = %v, want ErrNilContext", err)
	}
}

func TestDispatcherDeliversInRegistrationOrderWithSanitizedContext(t *testing.T) {
	type key struct{}
	var dispatcher Dispatcher
	var got []int
	if err := dispatcher.Register(func(ctx context.Context, event Event) error {
		if ctx.Value(key{}) != nil {
			t.Error("listener received parent value")
		}
		if event.Name() != NameChangeCreated {
			t.Errorf("event name = %q", event.Name())
		}
		got = append(got, 1)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.Register(func(context.Context, Event) error { got = append(got, 2); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.Dispatch(context.WithValue(context.Background(), key{}, "secret"), testEvent(t)); err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 2}; !reflect.DeepEqual(got, want) {
		t.Errorf("delivery order = %v, want %v", got, want)
	}
}

func TestDispatcherAggregatesErrorsAndClassifiesPanics(t *testing.T) {
	first := errors.New("first")
	second := errors.New("second")
	var dispatcher Dispatcher
	for _, listener := range []Listener{
		func(context.Context, Event) error { return first },
		func(context.Context, Event) error { panic("secret panic value") },
		func(context.Context, Event) error { return second },
	} {
		if err := dispatcher.Register(listener); err != nil {
			t.Fatal(err)
		}
	}
	err := dispatcher.Dispatch(context.Background(), testEvent(t))
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("Dispatch() = %v, missing listener errors", err)
	}
	if got := err.Error(); got == "" || strings.Contains(got, "secret panic value") {
		t.Errorf("Dispatch() leaked panic value: %q", got)
	}
	var panicFailure PanicError
	if !errors.As(err, &panicFailure) {
		t.Errorf("Dispatch() = %v, missing PanicError", err)
	}
}

func TestDispatcherClassifiesNilPanic(t *testing.T) {
	var dispatcher Dispatcher
	if err := dispatcher.Register(func(context.Context, Event) error { panic(nil) }); err != nil {
		t.Fatal(err)
	}
	err := dispatcher.Dispatch(context.Background(), testEvent(t))
	var panicFailure PanicError
	if !errors.As(err, &panicFailure) {
		t.Fatalf("Dispatch() = %v, want PanicError", err)
	}
}

func TestDispatcherStopsBeforeNextListenerOnCancellation(t *testing.T) {
	var dispatcher Dispatcher
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := 0
	if err := dispatcher.Register(func(context.Context, Event) error { called++; cancel(); return errors.New("first") }); err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.Register(func(context.Context, Event) error { called++; return nil }); err != nil {
		t.Fatal(err)
	}
	err := dispatcher.Dispatch(ctx, testEvent(t))
	if called != 1 {
		t.Errorf("listener calls = %d, want 1", called)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Dispatch() = %v, want context cancellation", err)
	}
}

func TestDispatcherSnapshotIsSafeDuringRegistration(t *testing.T) {
	var dispatcher Dispatcher
	var called atomic.Int64
	if err := dispatcher.Register(func(context.Context, Event) error {
		called.Add(1)
		return dispatcher.Register(func(context.Context, Event) error { called.Add(10); return nil })
	}); err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.Dispatch(context.Background(), testEvent(t)); err != nil {
		t.Fatal(err)
	}
	if got := called.Load(); got != 1 {
		t.Fatalf("snapshot called = %d, want 1", got)
	}
	var wait sync.WaitGroup
	event := testEvent(t)
	for range 20 {
		wait.Add(2)
		go func() { defer wait.Done(); _ = dispatcher.Register(func(context.Context, Event) error { return nil }) }()
		go func() { defer wait.Done(); _ = dispatcher.Dispatch(context.Background(), event) }()
	}
	wait.Wait()
}

func TestDispatcherRejectsNilListener(t *testing.T) {
	var dispatcher Dispatcher
	if err := dispatcher.Register(nil); !errors.Is(err, ErrNilListener) {
		t.Errorf("Register(nil) = %v", err)
	}
}

func testEvent(t *testing.T) Event {
	t.Helper()
	draft, err := NewChangeCreated("project-1", "change-1", "apply", "active", 1)
	if err != nil {
		t.Fatal(err)
	}
	event, err := newEvent(draft, "event-1", time.Now(), 1, "")
	if err != nil {
		t.Fatal(err)
	}
	return event
}
