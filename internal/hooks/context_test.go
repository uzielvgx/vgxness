package hooks

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestListenerContextPreservesCancellationButHidesValues(t *testing.T) {
	type key struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "hidden"))
	defer cancel()
	ctx = WithCorrelationID(ctx, "correlation-1")
	listener := listenerContext(ctx)

	if got := listener.Value(key{}); got != nil {
		t.Errorf("Value() = %v, want nil", got)
	}
	if got := correlationID(ctx); got != "correlation-1" {
		t.Errorf("correlationID() = %q", got)
	}
	cancel()
	select {
	case <-listener.Done():
	case <-time.After(time.Second):
		t.Fatal("listener context did not preserve cancellation")
	}
	if listener.Err() != context.Canceled {
		t.Errorf("Err() = %v, want %v", listener.Err(), context.Canceled)
	}
}

func TestCorrectionListenerContextHasNoRecoverableParent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	listener := listenerContext(ctx)
	typ := reflect.TypeOf(listener)
	contextType := reflect.TypeFor[context.Context]()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Anonymous || field.Type.Implements(contextType) {
			t.Errorf("listener field %q exposes parent context", field.Name)
		}
	}
	if deadline, ok := listener.Deadline(); !ok || deadline.IsZero() {
		t.Error("listener context did not preserve deadline")
	}
}

func TestCorrectionWithCorrelationIDDefersValidationToMaterialization(t *testing.T) {
	ctx := WithCorrelationID(context.Background(), "invalid\ncorrelation")
	if got := correlationID(ctx); got != "invalid\ncorrelation" {
		t.Fatalf("correlationID() = %q", got)
	}
	draft, err := NewChangeCreated("project-1", "change-1", "apply", "active", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newEvent(draft, "event-1", time.Now(), 1, correlationID(ctx)); err == nil {
		t.Error("newEvent accepted deferred invalid correlation")
	}
}
