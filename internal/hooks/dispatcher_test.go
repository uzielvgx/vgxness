package hooks

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDispatcherRunsHandlersInRegistrationOrder(t *testing.T) {
	var order []int
	dispatcher := newTestDispatcher(t,
		func(context.Context, Event) error { order = append(order, 1); return nil },
		func(context.Context, Event) error { order = append(order, 2); return nil },
	)

	diagnostics := dispatcher.Dispatch(context.Background(), validTaskStarted())
	if len(diagnostics) != 0 || len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("order=%v diagnostics=%v", order, diagnostics)
	}
}

func TestDispatcherSuppressesDuplicateIdentityUnderConcurrency(t *testing.T) {
	var calls atomic.Int32
	dispatcher := newTestDispatcher(t, func(context.Context, Event) error {
		calls.Add(1)
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	event := validTaskStarted()
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			dispatcher.Dispatch(context.Background(), event)
		}()
	}
	wait.Wait()
	if calls.Load() != 1 {
		t.Fatalf("handler calls=%d", calls.Load())
	}
}

func TestDispatcherTimesOutHandlerAndContinues(t *testing.T) {
	release := make(chan struct{})
	var second atomic.Bool
	var blockedCalls atomic.Int32
	dispatcher, err := New(Options{HandlerTimeout: 5 * time.Millisecond, MaxDepth: 4, DedupeCapacity: 16},
		func(context.Context, Event) error { blockedCalls.Add(1); <-release; return nil },
		func(context.Context, Event) error { second.Store(true); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	defer close(release)
	diagnostics := dispatcher.Dispatch(context.Background(), validTaskStarted())
	if !second.Load() || len(diagnostics) != 1 || diagnostics[0].Kind != DiagnosticTimeout || diagnostics[0].Handler != 0 {
		t.Fatalf("second=%v diagnostics=%v", second.Load(), diagnostics)
	}
	for index := range 32 {
		another := validTaskStarted()
		another.Meta.ID = "event-started-again-" + strconv.Itoa(index)
		dispatcher.Dispatch(context.Background(), another)
	}
	if blockedCalls.Load() > 4 {
		t.Fatalf("timed-out handler exceeded bounded in-flight calls: %d", blockedCalls.Load())
	}
}

func TestDispatcherRecoversPanicAndError(t *testing.T) {
	var completed atomic.Bool
	dispatcher := newTestDispatcher(t,
		func(context.Context, Event) error { panic("secret panic") },
		func(context.Context, Event) error { return errors.New("secret error") },
		func(context.Context, Event) error { completed.Store(true); return nil },
	)
	diagnostics := dispatcher.Dispatch(context.Background(), validTaskStarted())
	if !completed.Load() || len(diagnostics) != 2 || diagnostics[0].Kind != DiagnosticPanic || diagnostics[1].Kind != DiagnosticError {
		t.Fatalf("completed=%v diagnostics=%v", completed.Load(), diagnostics)
	}
}

func TestDispatcherBoundsRecursiveDispatch(t *testing.T) {
	var dispatcher *Dispatcher
	var calls atomic.Int32
	handler := func(ctx context.Context, event Event) error {
		calls.Add(1)
		nested := event.(TaskStarted)
		nested.Meta.ID += "-nested"
		dispatcher.Dispatch(ctx, nested)
		return nil
	}
	var err error
	dispatcher, err = New(Options{HandlerTimeout: time.Second, MaxDepth: 3, DedupeCapacity: 16}, handler)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher.Dispatch(context.Background(), validTaskStarted())
	if calls.Load() != 3 {
		t.Fatalf("recursive handler calls=%d", calls.Load())
	}
}

func TestDispatcherValidatesOptionsAndEvents(t *testing.T) {
	invalidOptions := []Options{
		{HandlerTimeout: -time.Second},
		{MaxDepth: -1},
		{DedupeCapacity: -1},
		{HandlerTimeout: 6 * time.Second},
		{MaxDepth: 17},
		{DedupeCapacity: 65_537},
	}
	for _, options := range invalidOptions {
		if _, err := New(options); err == nil {
			t.Errorf("accepted options %+v", options)
		}
	}
	var calls atomic.Int32
	dispatcher := newTestDispatcher(t, func(context.Context, Event) error { calls.Add(1); return nil })
	var typedNil *TaskStarted
	invalid := []Event{
		typedNil,
		TaskStarted{},
		TaskStarted{Meta: Metadata{ID: "event with spaces", At: time.Now()}, RunID: "run", TaskID: "task", Mode: ModeForeground},
		TaskSucceeded{Meta: validMetadata("event-success"), RunID: "run", TaskID: "task", Mode: ModeForeground, ResultDigest: "bad"},
		CandidateFrozen{Meta: validMetadata("candidate"), TicketID: "ticket", TaskID: "task", ManifestDigest: digest, ArtifactDigests: make([]string, MaxArtifactDigests+1)},
		ValidationCompleted{Meta: validMetadata("validation"), TicketID: "ticket", ReceiptDigest: digest, Operation: "shell", Success: true},
		DeliveryInstalled{Meta: validMetadata("delivery"), ReceiptID: "receipt", ReceiptDigest: digest, ChangeCount: -1},
	}
	for index, event := range invalid {
		diagnostics := dispatcher.Dispatch(context.Background(), event)
		if len(diagnostics) != 1 || diagnostics[0].Kind != DiagnosticInvalid {
			t.Errorf("invalid event %d diagnostics=%v", index, diagnostics)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid events reached handler %d times", calls.Load())
	}
}

func TestDispatcherClonesMutablePayloadForEachHandler(t *testing.T) {
	event := CandidateFrozen{
		Meta: validMetadata("candidate-frozen"), TicketID: "ticket", TaskID: "task",
		ManifestDigest: digest, ArtifactDigests: []string{digest, otherDigest}, ChangeCount: 2,
	}
	var observed string
	dispatcher := newTestDispatcher(t,
		func(_ context.Context, event Event) error {
			event.(CandidateFrozen).ArtifactDigests[0] = otherDigest
			return nil
		},
		func(_ context.Context, event Event) error {
			observed = event.(CandidateFrozen).ArtifactDigests[0]
			return nil
		},
	)
	dispatcher.Dispatch(context.Background(), event)
	if observed != digest || event.ArtifactDigests[0] != digest {
		t.Fatalf("observed=%q original=%q", observed, event.ArtifactDigests[0])
	}
}

func TestNilAndEmptyDispatcherAreSafe(t *testing.T) {
	var nilDispatcher *Dispatcher
	if diagnostics := nilDispatcher.Dispatch(context.Background(), validTaskStarted()); diagnostics != nil {
		t.Fatalf("nil dispatcher diagnostics=%v", diagnostics)
	}
	empty := newTestDispatcher(t)
	if diagnostics := empty.Dispatch(context.Background(), validTaskStarted()); len(diagnostics) != 0 {
		t.Fatalf("empty dispatcher diagnostics=%v", diagnostics)
	}
}

const (
	digest      = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	otherDigest = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
)

func newTestDispatcher(t *testing.T, handlers ...Handler) *Dispatcher {
	t.Helper()
	dispatcher, err := New(Options{HandlerTimeout: time.Second, MaxDepth: 4, DedupeCapacity: 64}, handlers...)
	if err != nil {
		t.Fatal(err)
	}
	return dispatcher
}

func validMetadata(id string) Metadata {
	return Metadata{ID: id, At: time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)}
}

func validTaskStarted() TaskStarted {
	return TaskStarted{Meta: validMetadata("event-started"), RunID: "run-1", TaskID: "task-1", Mode: ModeForeground}
}
