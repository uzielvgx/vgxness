//go:build unix

package skillregistry

import (
	"errors"
	"syscall"
)

// processAlive reports whether a PID is still running. EPERM means the process
// exists but is not signalable, which still counts as alive.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
