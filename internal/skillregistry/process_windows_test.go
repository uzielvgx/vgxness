//go:build windows

package skillregistry

import (
	"errors"
	"os"
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

// TestAliveFromWindowsQueryClassification pins the conservative rule without
// native process calls: only a missing PID or a completed non-STILL_ACTIVE
// query proves death; access-denied and query errors count as alive.
func TestAliveFromWindowsQueryClassification(t *testing.T) {
	cases := []struct {
		name     string
		openErr  error
		exitErr  error
		exitCode uint32
		want     bool
	}{
		{"running", nil, nil, stillActive, true},
		{"exited", nil, nil, 0, false},
		{"missing", windows.ERROR_INVALID_PARAMETER, nil, 0, false},
		{"access-denied", windows.ERROR_ACCESS_DENIED, nil, 0, true},
		{"open-unknown", errors.New("injected open failure"), nil, 0, true},
		{"query-failure", nil, errors.New("injected query failure"), 0, true},
	}
	for _, test := range cases {
		if got := aliveFromWindowsQuery(test.openErr, test.exitErr, test.exitCode); got != test.want {
			t.Fatalf("%s: alive=%t want %t", test.name, got, test.want)
		}
	}
}

// TestProcessAliveRecognizesDeadAndLiveOnWindows exercises the native Windows
// owner probe: a live PID is alive and a reaped PID is dead. It runs only on
// Windows so the Unix build never assumes this result.
func TestProcessAliveRecognizesDeadAndLiveOnWindows(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Fatal("the current process must be reported alive")
	}
	if processAlive(0) {
		t.Fatal("pid 0 must not be reported alive")
	}
	command := exec.Command("cmd", "/c", "exit", "0")
	if err := command.Start(); err != nil {
		t.Skipf("cannot start a helper process: %v", err)
	}
	pid := command.Process.Pid
	if !processAlive(pid) {
		t.Fatalf("a running helper pid %d was reported dead", pid)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("helper wait: %v", err)
	}
	if processAlive(pid) {
		t.Fatalf("a reaped helper pid %d was reported alive", pid)
	}
}
