//go:build windows

package opencode

import (
	"os"
	"os/exec"
	"time"
)

func supportsDescendantCancellation() bool { return false }

func configureProcessTree(command *exec.Cmd) {
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		return command.Process.Kill()
	}
	command.WaitDelay = 2 * time.Second
}
