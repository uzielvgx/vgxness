package syncservice

import "testing"

func TestLocallyRetryableKeepsOnlyDeviceIdentityRejections(t *testing.T) {
	for _, tc := range []struct {
		result Result
		want   bool
	}{
		{Result{Disposition: DispositionRejected, Code: "revoked"}, true},
		{Result{Disposition: DispositionRejected, Code: "invalid_device"}, true},
		{Result{Disposition: DispositionRejected, Code: "invalid_input"}, false},
		{Result{Disposition: DispositionRejected, Code: "stale_base"}, false},
		{Result{Disposition: DispositionConflict, Code: "revoked"}, false},
	} {
		got := tc.result.LocallyRetryable()
		if got.Retryable != tc.want || got.Terminal() == tc.want || tc.result.Retryable {
			t.Fatalf("%+v: retryable=%v terminal=%v", tc.result, got.Retryable, got.Terminal())
		}
	}
}
