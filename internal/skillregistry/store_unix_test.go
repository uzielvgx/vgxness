//go:build unix

package skillregistry

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A swapped FIFO must be rejected as non-regular without blocking the reader.
func TestReadCacheRejectsFIFOWithoutBlocking(t *testing.T) {
	directory := t.TempDir()
	fifo := filepath.Join(directory, "skill-registry.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	options := Options{Workspace: directory, CachePath: fifo}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, usable, err := New().Load(context.Background(), options); err != nil || usable {
			t.Errorf("FIFO cache must be unusable: usable=%t err=%v", usable, err)
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("reading a FIFO cache blocked")
	}
	if _, err := os.Stat(fifo); err != nil {
		t.Fatalf("fifo disappeared: %v", err)
	}
}
