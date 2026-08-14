package hooks

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrNilListener = errors.New("hook listener is nil")
	ErrNilContext  = errors.New("hook context is nil")
)

// Listener receives a sanitized context and an immutable lifecycle event.
// It runs synchronously on the caller's goroutine.
type Listener func(context.Context, Event) error

// ListenerError classifies a listener failure without changing its error identity.
type ListenerError struct {
	Index int
	Err   error
}

func (e ListenerError) Error() string { return "hook listener failed" }
func (e ListenerError) Unwrap() error { return e.Err }

// PanicError identifies a listener panic without retaining its value. Error text
// intentionally redacts listener and panic diagnostics at this trust boundary.
type PanicError struct{ Index int }

func (e PanicError) Error() string { return "hook listener panicked" }

// Dispatcher synchronously distributes events to an in-process listener snapshot.
type Dispatcher struct {
	mu        sync.RWMutex
	listeners []Listener
}

// Register appends listener to the deterministic dispatch order.
func (d *Dispatcher) Register(listener Listener) error {
	if listener == nil {
		return ErrNilListener
	}
	d.mu.Lock()
	d.listeners = append(d.listeners, listener)
	d.mu.Unlock()
	return nil
}

// Dispatch calls the registration snapshot in order. Cancellation is cooperative:
// it is checked before each listener, and no listener is preempted or timed out.
func (d *Dispatcher) Dispatch(ctx context.Context, event Event) error {
	if ctx == nil {
		return ErrNilContext
	}
	d.mu.RLock()
	listeners := append([]Listener(nil), d.listeners...)
	d.mu.RUnlock()
	if len(listeners) == 0 {
		return nil
	}

	listenerCtx := listenerContext(ctx)
	var failures []error
	for index, listener := range listeners {
		if err := listenerCtx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		if err := callListener(listener, listenerCtx, event, index); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func callListener(listener Listener, ctx context.Context, event Event, index int) (failure error) {
	returned := false
	defer func() {
		if !returned {
			// recover may return nil for panic(nil) when panicnil=1 is enabled.
			recover()
			failure = PanicError{Index: index}
		}
	}()
	if err := listener(ctx, event); err != nil {
		returned = true
		return ListenerError{Index: index, Err: err}
	}
	returned = true
	return nil
}
