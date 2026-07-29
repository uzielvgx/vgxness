package opencode

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
)

func TestProberStatuses(t *testing.T) {
	workspace := t.TempDir()
	tests := []struct {
		name       string
		workspace  string
		version    string
		lookErr    error
		runErr     error
		wantStatus integration.HandshakeStatus
		wantOK     bool
	}{
		{name: "healthy minimum", workspace: workspace, version: "1.18.4", wantStatus: integration.HandshakeHealthy, wantOK: true},
		{name: "healthy newer", workspace: workspace, version: "1.99.0", wantStatus: integration.HandshakeHealthy, wantOK: true},
		{name: "older", workspace: workspace, version: "1.18.3", wantStatus: integration.HandshakeIncompatible},
		{name: "wrong major", workspace: workspace, version: "2.0.0", wantStatus: integration.HandshakeIncompatible},
		{name: "malformed", workspace: workspace, version: "OpenCode", wantStatus: integration.HandshakeIncompatible},
		{name: "missing executable", workspace: workspace, lookErr: errors.New("missing"), wantStatus: integration.HandshakeUnavailable},
		{name: "execution failure", workspace: workspace, runErr: errors.New("failed"), wantStatus: integration.HandshakeUnavailable},
		{name: "relative workspace", workspace: ".", version: "1.18.4", wantStatus: integration.HandshakeUnavailable},
		{name: "missing workspace", workspace: filepath.Join(workspace, "missing"), version: "1.18.4", wantStatus: integration.HandshakeUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prober := &Prober{
				lookPath: func(string) (string, error) { return "/bin/opencode", test.lookErr },
				run:      func(context.Context, string, string) ([]byte, error) { return []byte(test.version), test.runErr },
			}
			result, err := prober.Probe(context.Background(), test.workspace)
			if err != nil || result.Status != test.wantStatus || result.OK != test.wantOK {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestProberHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := NewProber("").Probe(ctx, t.TempDir())
	if !errors.Is(err, context.Canceled) || result.Status != integration.HandshakeUnavailable {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestProberCancelsRunningVersionCheck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	prober := &Prober{
		lookPath: func(string) (string, error) { return "/bin/opencode", nil },
		run: func(ctx context.Context, _, _ string) ([]byte, error) {
			cancel()
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	result, err := prober.Probe(ctx, t.TempDir())
	if !errors.Is(err, context.Canceled) || result.Status != integration.HandshakeUnavailable {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
