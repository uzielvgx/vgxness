//go:build windows

package controlplane

import (
	"os"
	"os/exec"
	"time"
)

func configureNativeValidationProcess(command *exec.Cmd) {
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		return command.Process.Kill()
	}
	command.WaitDelay = 2 * time.Second
}
