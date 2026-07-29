//go:build !windows

package opencode

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunVersionBoundsInheritedOutputPipes(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n(sleep 3) &\nprintf '1.18.4\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := runVersion(ctx, executable, t.TempDir())
	if elapsed := time.Since(started); err == nil || elapsed >= 2*time.Second {
		t.Fatalf("inherited output pipes were not bounded: elapsed=%s err=%v", elapsed, err)
	}
}
