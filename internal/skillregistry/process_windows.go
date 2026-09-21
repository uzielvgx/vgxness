//go:build windows

package skillregistry

import (
	"errors"

	"golang.org/x/sys/windows"
)

// stillActive is the exit code GetExitCodeProcess reports for a running process
// (Win32 STILL_ACTIVE). x/sys/windows does not export it, so it is named here.
const stillActive = 259

// processAlive reports whether a PID is still running on Windows. A succeeded
// query with a non-STILL_ACTIVE exit code is provably dead; ERROR_INVALID_PARAMETER
// means the PID does not name a process. Every other outcome, including access
// denied for a protected process and any query failure, fails closed as alive.
func processAlive(pid int) bool {
	if pid <= 0 || int64(pid) > int64(^uint32(0)) {
		// No live Windows process can hold a PID outside the uint32 range, so
		// out-of-range values are provably dead rather than truncated into a
		// possibly live PID.
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return aliveFromWindowsQuery(err, nil, 0)
	}
	defer windows.CloseHandle(handle)
	var code uint32
	queryErr := windows.GetExitCodeProcess(handle, &code)
	return aliveFromWindowsQuery(nil, queryErr, code)
}

// aliveFromWindowsQuery is the pure liveness decision for Windows, split out so
// alive/dead/unknown is testable without native process calls.
func aliveFromWindowsQuery(openErr, exitErr error, exitCode uint32) bool {
	if openErr != nil {
		// A missing PID is dead; access denied or any other open failure is
		// unknown and fails closed as alive.
		return !errors.Is(openErr, windows.ERROR_INVALID_PARAMETER)
	}
	if exitErr != nil {
		return true
	}
	return exitCode == stillActive
}
