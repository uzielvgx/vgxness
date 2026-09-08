//go:build !windows

package pi

import (
	"context"
	"io"
	"os/exec"
	"sync"
	"syscall"
)

type platformProcessLauncher struct{}

var newPlatformProcessLauncher processLauncher = platformProcessLauncher{}

func (platformProcessLauncher) Start(ctx context.Context, name string, args []string, in io.Reader, out, errOut io.Writer) (processHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, errOut
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &platformProcess{cmd: cmd}, nil
}

type platformProcess struct {
	cmd      *exec.Cmd
	waitOnce sync.Once
	waitDone chan struct{}
	waitErr  error
}

func (p *platformProcess) CloseInput() error {
	if c, ok := p.cmd.Stdin.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (p *platformProcess) Wait(ctx context.Context) error {
	p.waitOnce.Do(func() {
		p.waitDone = make(chan struct{})
		go func() {
			p.waitErr = p.cmd.Wait()
			close(p.waitDone)
		}()
	})
	select {
	case <-p.waitDone:
		return p.waitErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *platformProcess) TerminateTree() error {
	return syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
}
func (p *platformProcess) KillTree() error { return syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL) }
