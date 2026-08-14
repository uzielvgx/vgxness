package hooks

import (
	"context"
	"time"
)

type correlationKey struct{}

// WithCorrelationID adds trusted correlation metadata to a composition context.
// Validation is deliberately deferred to event materialization; this does not grant authority.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey{}, id)
}

func correlationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationKey{}).(string)
	return id
}

type hiddenValueContext struct {
	deadline func() (time.Time, bool)
	done     <-chan struct{}
	err      func() error
}

func (hiddenValueContext) Value(any) any { return nil }

func (ctx hiddenValueContext) Deadline() (time.Time, bool) { return ctx.deadline() }
func (ctx hiddenValueContext) Done() <-chan struct{}       { return ctx.done }
func (ctx hiddenValueContext) Err() error                  { return ctx.err() }

func listenerContext(ctx context.Context) context.Context {
	return hiddenValueContext{deadline: ctx.Deadline, done: ctx.Done(), err: ctx.Err}
}
