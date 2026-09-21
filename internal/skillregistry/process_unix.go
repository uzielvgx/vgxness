//go:build unix

package skillregistry

import (
	"errors"
	"syscall"
)

// processAlive reports whether a PID is still running. A successful signal
// probe or EPERM (the process exists but is not signalable) means alive; ESRCH
// proves absence; every other result is unknown and fails closed as alive.
func processAlive(pid int) bool {
	return aliveFromKillError(syscall.Kill(pid, 0))
}

// aliveFromKillError is the pure liveness decision, split out so the
// alive/dead/unknown classification is testable without sending signals.
func aliveFromKillError(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	if errors.Is(err, syscall.ESRCH) {
		return false
	}
	return true
}
