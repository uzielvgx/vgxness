//go:build !windows

package codegraph

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// configureProcessTree gives each CodeGraph query its own process group so a
// cancelled or expired ticket also terminates descendants of the CLI process.
func configureProcessTree(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = 2 * time.Second
}
