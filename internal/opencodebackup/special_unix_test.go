//go:build unix

package opencodebackup_test

import (
	"context"
	"errors"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/vgxness/vgxness/internal/opencodebackup"
)

func TestCreateRejectsSpecialFiles(t *testing.T) {
	source := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(source, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, _ := newEngine(t, source, nil)
	if _, err := engine.Create(context.Background(), opencodebackup.ModeFull); !errors.Is(err, opencodebackup.ErrUnsupported) {
		t.Fatalf("Create() error = %v, want ErrUnsupported", err)
	}
}
