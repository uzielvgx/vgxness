//go:build windows

package launcher

import (
	"os"
	"os/exec"
)

func replaceProcess(path string, args, environment []string) error {
	command := exec.Command(path, args[1:]...)
	command.Env = environment
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	err := command.Run()
	if exitError, ok := err.(*exec.ExitError); ok {
		return &childExitError{code: exitError.ExitCode()}
	}
	return err
}

func binaryName() string { return "vgxness.exe" }
