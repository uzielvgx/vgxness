package hooks

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestDispatcherRegistrationOwnershipAndOrder(t *testing.T) {
	d := NewForTest(fixedClock, ids("event-1", "event-2"))
	var got []string
	first := func(context.Context, Event) error { got = append(got, "first"); return nil }
	second := func(context.Context, Event) error { got = append(got, "second"); return nil }
	if err := d.Register("one", first, NameChangeCreated); err != nil {
		t.Fatal(err)
	}
	if err := d.Register("two", second, NameChangeCreated); err != nil {
		t.Fatal(err)
	}
	if err := d.Register("one", second, NameChangeCreated, NameChangeTransitioned); err != nil {
		t.Fatal(err)
	}
	d.Emit(context.Background(), testDraft(t))
	if want := []string{"first", "second"}; !same(got, want) {
		t.Errorf("delivery = %v, want %v", got, want)
	}
	got = nil
	d.Emit(context.Background(), testTransition(t))
	if want := []string{"first"}; !same(got, want) {
		t.Errorf("added name handler = %v, want %v", got, want)
	}
}

func TestDispatcherRejectsInvalidRegistrationWithoutPartialMutation(t *testing.T) {
	d := New()
	listener := func(context.Context, Event) error { return nil }
	for _, registration := range []struct {
		id       ListenerID
		listener Listener
		names    []Name
		want     error
	}{
		{"", listener, []Name{NameChangeCreated}, ErrInvalidListenerID},
		{"bad\n", listener, []Name{NameChangeCreated}, ErrInvalidListenerID},
		{ListenerID(string(make([]byte, 129))), listener, []Name{NameChangeCreated}, ErrInvalidListenerID},
		{"one", nil, []Name{NameChangeCreated}, ErrNilListener},
		{"one", listener, nil, ErrInvalidName},
		{"one", listener, []Name{"unknown"}, ErrInvalidName},
	} {
		if err := d.Register(registration.id, registration.listener, registration.names...); !errors.Is(err, registration.want) {
			t.Errorf("Register(%q) = %v, want %v", registration.id, err, registration.want)
		}
	}
	if d.Unregister("one") {
		t.Error("invalid registration was retained")
	}
}

func TestDispatcherRejectsRawNameOverflowBeforeDeduplication(t *testing.T) {
	d := New()
	names := make([]Name, maxNamesPerListener+1)
	for i := range names {
		names[i] = NameChangeCreated
	}
	if err := d.Register("one", func(context.Context, Event) error { return nil }, names...); !errors.Is(err, ErrListenerNameLimit) {
		t.Fatalf("Register() = %v, want ErrListenerNameLimit", err)
	}
	if d.Unregister("one") {
		t.Error("oversized registration was retained")
	}
}

func TestDispatcherContainsSealingPanicsAndResetsDelivery(t *testing.T) {
	for _, source := range []string{"event ID", "clock", "correlation"} {
		t.Run(source, func(t *testing.T) {
			d := NewForTest(fixedClock, ids("event-1"))
			var events []Event
			if err := d.Register("one", func(_ context.Context, event Event) error { events = append(events, event); return nil }, NameChangeCreated); err != nil {
				t.Fatal(err)
			}
			switch source {
			case "event ID":
				d.eventID = func() (string, error) { panic("secret") }
			case "clock":
				d.clock = func() time.Time { panic("secret") }
			case "correlation":
				if emitPanics(d, panicValueContext{}, testDraft(t)) {
					t.Error("sealing panic escaped Emit")
				}
				d.eventID = ids("event-1")
				d.Emit(context.Background(), testDraft(t))
				if len(events) != 1 || events[0].Sequence() != 1 {
					t.Fatalf("events = %#v", events)
				}
				return
			}
			if emitPanics(d, context.Background(), testDraft(t)) {
				t.Error("sealing panic escaped Emit")
			}
			d.eventID = ids("event-1")
			d.clock = fixedClock
			d.Emit(context.Background(), testDraft(t))
			if len(events) != 1 || events[0].Sequence() != 1 {
				t.Fatalf("events = %#v", events)
			}
		})
	}
}

func TestDispatcherEmitSealsEventAndSuppressesInvalidInputs(t *testing.T) {
	d := NewForTest(fixedClock, ids("event-1", "event-2"))
	var events []Event
	if err := d.Register("one", func(_ context.Context, event Event) error { events = append(events, event); return nil }, NameChangeCreated); err != nil {
		t.Fatal(err)
	}
	ctx := WithCorrelationID(context.Background(), "correlation-1")
	d.Emit(ctx, testDraft(t))
	if len(events) != 1 || events[0].EventID() != "event-1" || events[0].OccurredAt() != fixedClock().UTC() || events[0].Sequence() != 1 || events[0].CorrelationID() != "correlation-1" {
		t.Fatalf("sealed event = %#v", events)
	}
	d.Emit(nil, testDraft(t))
	d.Emit(context.Background(), Draft{})
	if len(events) != 1 {
		t.Errorf("invalid emits delivered %d events", len(events))
	}
}

func TestDispatcherFiltersExactNamesAndSnapshotsMutation(t *testing.T) {
	d := NewForTest(fixedClock, ids("event-1", "event-2"))
	var got []string
	if err := d.Register("change", func(context.Context, Event) error {
		got = append(got, "change")
		_ = d.Register("later", func(context.Context, Event) error { got = append(got, "later"); return nil }, NameChangeCreated)
		_ = d.Unregister("transition")
		return nil
	}, NameChangeCreated); err != nil {
		t.Fatal(err)
	}
	if err := d.Register("transition", func(context.Context, Event) error { got = append(got, "transition"); return nil }, NameChangeTransitioned); err != nil {
		t.Fatal(err)
	}
	d.Emit(context.Background(), testDraft(t))
	if want := []string{"change"}; !same(got, want) {
		t.Fatalf("first emit = %v, want %v", got, want)
	}
	got = nil
	d.Emit(context.Background(), testDraft(t))
	if want := []string{"change", "later"}; !same(got, want) {
		t.Errorf("next emit = %v, want %v", got, want)
	}
}

func TestDispatcherSuppressesNestedAndConcurrentEmitsWithoutClose(t *testing.T) {
	d := NewForTest(fixedClock, ids("event-1", "event-2"))
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	if err := d.Register("one", func(context.Context, Event) error {
		calls.Add(1)
		d.Emit(context.Background(), testDraft(t))
		close(started)
		<-release
		return nil
	}, NameChangeCreated); err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() { d.Emit(context.Background(), testDraft(t)); close(finished) }()
	<-started
	returned := make(chan struct{})
	go func() { d.Emit(context.Background(), testDraft(t)); close(returned) }()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("concurrent Emit waited")
	}
	close(release)
	<-finished
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

func TestDispatcherCloseIsImmediateAndNonPreemptive(t *testing.T) {
	d := NewForTest(fixedClock, ids("event-1"))
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var second atomic.Bool
	if err := d.Register("one", func(context.Context, Event) error { close(started); <-release; return nil }, NameChangeCreated); err != nil {
		t.Fatal(err)
	}
	if err := d.Register("two", func(context.Context, Event) error { second.Store(true); return nil }, NameChangeCreated); err != nil {
		t.Fatal(err)
	}
	go func() { d.Emit(context.Background(), testDraft(t)); close(done) }()
	<-started
	returned := make(chan struct{})
	go func() { d.Close(); close(returned) }()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("Close waited for listener")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("active listener did not finish")
	}
	if second.Load() {
		t.Error("Close did not prevent next listener")
	}
	d.Emit(context.Background(), testDraft(t))
}

func TestDispatcherSuppressesSealingFailuresWithoutAdvancingSequence(t *testing.T) {
	clock := fixedClock
	ids := ids("event-1", "event-2", "event-3", "event-4")
	d := NewForTest(clock, func() (string, error) { return ids() })
	var events []Event
	if err := d.Register("one", func(_ context.Context, event Event) error { events = append(events, event); return nil }, NameChangeCreated); err != nil {
		t.Fatal(err)
	}
	badID := NewForTest(fixedClock, func() (string, error) { return "", errors.New("no ID") })
	if err := badID.Register("one", func(context.Context, Event) error { t.Error("generation failure delivered"); return nil }, NameChangeCreated); err != nil {
		t.Fatal(err)
	}
	badID.Emit(context.Background(), testDraft(t))
	d.Emit(WithCorrelationID(context.Background(), "bad\n"), testDraft(t))
	d.clock = func() time.Time { return time.Time{} }
	d.Emit(context.Background(), testDraft(t))
	d.clock = fixedClock
	d.Emit(context.Background(), testDraft(t))
	if len(events) != 1 || events[0].Sequence() != 1 {
		t.Fatalf("events = %#v", events)
	}
	d.sequence = ^uint64(0)
	d.Emit(context.Background(), testDraft(t))
	if len(events) != 1 {
		t.Error("overflow delivered event")
	}
}

func TestDispatcherZeroListenersDoesNotSealAndContextPreservesSignals(t *testing.T) {
	var idCalls, clockCalls atomic.Int64
	d := NewForTest(func() time.Time { clockCalls.Add(1); return fixedClock() }, func() (string, error) { idCalls.Add(1); return "event-1", nil })
	d.Emit(context.Background(), testDraft(t))
	if idCalls.Load() != 0 || clockCalls.Load() != 0 {
		t.Error("zero listener emit sealed event")
	}
	type key struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "hidden"))
	defer cancel()
	seen := make(chan context.Context, 1)
	if err := d.Register("one", func(listener context.Context, _ Event) error {
		seen <- listener
		return errors.New("secret listener error")
	}, NameChangeCreated); err != nil {
		t.Fatal(err)
	}
	d.Emit(ctx, testDraft(t))
	listener := <-seen
	if listener.Value(key{}) != nil {
		t.Error("context value leaked")
	}
	cancel()
	select {
	case <-listener.Done():
	case <-time.After(time.Second):
		t.Error("cancellation not preserved")
	}
}

func TestDispatcherSnapshotSingleFlightAndClose(t *testing.T) {
	d := NewForTest(fixedClock, ids("event-1", "event-2"))
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int64
	if err := d.Register("one", func(context.Context, Event) error {
		calls.Add(1)
		close(started)
		d.Emit(context.Background(), testDraft(t))
		<-release
		return nil
	}, NameChangeCreated); err != nil {
		t.Fatal(err)
	}
	if err := d.Register("two", func(context.Context, Event) error { calls.Add(1); return nil }, NameChangeCreated); err != nil {
		t.Fatal(err)
	}
	go d.Emit(context.Background(), testDraft(t))
	<-started
	d.Emit(context.Background(), testDraft(t))
	d.Close()
	if err := d.Register("three", func(context.Context, Event) error { return nil }, NameChangeCreated); !errors.Is(err, ErrClosed) {
		t.Errorf("Register after Close = %v", err)
	}
	close(release)
	deadline := time.After(time.Second)
	for {
		if calls.Load() == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("calls = %d, want 1", calls.Load())
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestDispatcherSanitizesContextAndContainsPanics(t *testing.T) {
	type key struct{}
	d := NewForTest(fixedClock, ids("event-1"))
	called := false
	if err := d.Register("one", func(ctx context.Context, _ Event) error {
		if ctx.Value(key{}) != nil {
			t.Error("context value leaked")
		}
		panic(nil)
	}, NameChangeCreated); err != nil {
		t.Fatal(err)
	}
	if err := d.Register("two", func(context.Context, Event) error { called = true; return errors.New("secret") }, NameChangeCreated); err != nil {
		t.Fatal(err)
	}
	d.Emit(context.WithValue(context.Background(), key{}, "hidden"), testDraft(t))
	if !called {
		t.Error("later listener was not isolated from panic")
	}
}

func TestDispatcherUnregisterAndLimits(t *testing.T) {
	d := New()
	listener := func(context.Context, Event) error { return nil }
	if err := d.Register("one", listener, NameChangeCreated); err != nil {
		t.Fatal(err)
	}
	if !d.Unregister("one") || d.Unregister("one") {
		t.Error("Unregister() result mismatch")
	}
	for i := 0; i < 64; i++ {
		if err := d.Register(ListenerID(fmt.Sprintf("listener-%d", i)), listener, NameChangeCreated); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Register("overflow", listener, NameChangeCreated); !errors.Is(err, ErrListenerLimit) {
		t.Errorf("listener limit = %v", err)
	}
	var calls atomic.Int64
	d2 := NewForTest(fixedClock, ids("event-1"))
	for i := 0; i < 64; i++ {
		if err := d2.Register(ListenerID(fmt.Sprintf("id-%d", i)), func(context.Context, Event) error { calls.Add(1); return nil }, NameChangeCreated); err != nil {
			t.Fatal(err)
		}
	}
	if err := d2.Register("overflow", listener, NameChangeCreated); !errors.Is(err, ErrListenerLimit) {
		t.Fatal(err)
	}
	d2.Emit(context.Background(), testDraft(t))
	if got := calls.Load(); got != 64 {
		t.Errorf("limit mutated registration: %d listeners", got)
	}
}

func TestDispatcherAllKnownNamesAreUniqueAndLimitIsDocumented(t *testing.T) {
	// Hooks V1 has 11 closed names, so the 16-name limit cannot be reached without widening it.
	names := []Name{NameChangeCreated, NameRevisionAccepted, NameChangeTransitioned, NameProjectionRecorded, NameMemorySaved, NameMemoryForgotten, NameMemorySyncCompleted, NameIntegrationPreviewCompleted, NameIntegrationInstallCompleted, NameIntegrationStatusCompleted, NameIntegrationUninstallCompleted}
	seen := make(map[Name]bool, len(names))
	for _, name := range names {
		if seen[name] || !knownName(name) {
			t.Errorf("invalid known name %q", name)
		}
		seen[name] = true
	}
}

func fixedClock() time.Time {
	return time.Date(2026, 8, 14, 12, 0, 0, 0, time.FixedZone("offset", -6*60*60))
}
func ids(values ...string) func() (string, error) {
	var next int
	return func() (string, error) {
		if next >= len(values) {
			return "", errors.New("exhausted")
		}
		value := values[next]
		next++
		return value, nil
	}
}
func testDraft(t *testing.T) Draft {
	t.Helper()
	d, err := NewChangeCreated("project-1", "change-1", "apply", "active", 1)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func testTransition(t *testing.T) Draft {
	t.Helper()
	d, err := NewChangeTransitioned("project-1", "change-1", "apply", "active", 1)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func same(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func emitPanics(d *Dispatcher, ctx context.Context, draft Draft) (panicked bool) {
	defer func() { panicked = recover() != nil }()
	d.Emit(ctx, draft)
	return false
}

type panicValueContext struct{}

func (panicValueContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (panicValueContext) Done() <-chan struct{}       { return nil }
func (panicValueContext) Err() error                  { return nil }
func (panicValueContext) Value(any) any               { panic("secret") }
