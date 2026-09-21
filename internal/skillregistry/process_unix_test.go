//go:build unix

package skillregistry

import (
	"errors"
	"syscall"
	"testing"
)

// TestAliveFromKillErrorClassification pins the conservative rule: only ESRCH
// proves absence; EPERM and every unknown error count as alive.
func TestAliveFromKillErrorClassification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"success", nil, true},
		{"eperm", syscall.EPERM, true},
		{"esrch", syscall.ESRCH, false},
		{"unknown", errors.New("injected unknown failure"), true},
	}
	for _, test := range cases {
		if got := aliveFromKillError(test.err); got != test.want {
			t.Fatalf("%s: alive=%t want %t", test.name, got, test.want)
		}
	}
}
