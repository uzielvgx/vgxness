package pi

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

type processHandle interface {
	CloseInput() error
	Wait(context.Context) error
	TerminateTree() error
	KillTree() error
}

type processLauncher interface {
	Start(context.Context, string, []string, io.Reader, io.Writer, io.Writer) (processHandle, error)
}

type terminalKind string

const (
	terminalCompleted terminalKind = "completed"
	terminalCancelled terminalKind = "cancelled"
	terminalFailed    terminalKind = "failed"
)

type terminalRecord struct {
	Kind terminalKind
	Err  error
}

type terminalGate struct {
	mu     sync.Mutex
	record terminalRecord
	ok     bool
}

func (g *terminalGate) Commit(r terminalRecord) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.ok {
		return false
	}
	g.record, g.ok = r, true
	return true
}
func (g *terminalGate) Result() (terminalRecord, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.record, g.ok
}

type processLifecycle struct {
	launcher processLauncher
	gate     *terminalGate
	grace    time.Duration
	process  processHandle
}

func newProcessLifecycle(l processLauncher, g *terminalGate, grace time.Duration) *processLifecycle {
	return &processLifecycle{launcher: l, gate: g, grace: grace}
}
func (l *processLifecycle) Start(ctx context.Context, name string, args []string, in io.Reader, out, errOut io.Writer) error {
	if err := ctx.Err(); err != nil {
		l.gate.Commit(terminalRecord{Kind: terminalFailed, Err: err})
		return err
	}
	p, err := l.launcher.Start(ctx, name, args, in, gatedWriter{l.gate, out}, gatedWriter{l.gate, errOut})
	l.process = p
	if err != nil {
		if p != nil {
			_ = p.TerminateTree()
			if l.waitWithGrace(context.Background()) != nil {
				_ = p.KillTree()
				_ = l.waitWithGrace(context.Background())
			}
		}
		l.gate.Commit(terminalRecord{Kind: terminalFailed, Err: err})
		return err
	}
	if p == nil {
		err := errors.New("process launcher returned nil process")
		l.gate.Commit(terminalRecord{Kind: terminalFailed, Err: err})
		return err
	}
	l.process = p
	return nil
}
func (l *processLifecycle) Complete(ctx context.Context) error {
	closeErr := l.process.CloseInput()
	waitErr := l.process.Wait(ctx)
	err := errors.Join(closeErr, waitErr)
	l.gate.Commit(terminalRecord{Kind: terminalCompleted, Err: err})
	return err
}
func (l *processLifecycle) Cancel(ctx context.Context) error {
	closeErr := l.process.CloseInput()
	if l.waitWithGrace(ctx) == nil {
		l.gate.Commit(terminalRecord{Kind: terminalCancelled, Err: closeErr})
		return closeErr
	}
	if err := l.process.TerminateTree(); err != nil {
		err = errors.Join(closeErr, err)
		l.gate.Commit(terminalRecord{Kind: terminalFailed, Err: err})
		return err
	}
	if l.waitWithGrace(ctx) == nil {
		l.gate.Commit(terminalRecord{Kind: terminalCancelled, Err: closeErr})
		return closeErr
	}
	if err := l.process.KillTree(); err != nil {
		err = errors.Join(closeErr, err)
		l.gate.Commit(terminalRecord{Kind: terminalFailed, Err: err})
		return err
	}
	err := l.waitWithGrace(ctx)
	err = errors.Join(closeErr, err)
	l.gate.Commit(terminalRecord{Kind: terminalCancelled, Err: err})
	return err
}

func (l *processLifecycle) waitWithGrace(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, l.grace)
	defer cancel()
	return l.process.Wait(ctx)
}

type gatedWriter struct {
	gate *terminalGate
	dst  io.Writer
}

func (w gatedWriter) Write(p []byte) (int, error) {
	w.gate.mu.Lock()
	defer w.gate.mu.Unlock()
	if w.gate.ok {
		return len(p), nil
	}
	if w.dst == nil {
		return len(p), nil
	}
	return w.dst.Write(p)
}
