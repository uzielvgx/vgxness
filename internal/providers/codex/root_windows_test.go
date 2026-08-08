//go:build windows

package codex

import (
	"context"
	"errors"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
)

func TestOpenRootFailsClosedOnWindows(t *testing.T) {
	r, err := OpenRoot(context.Background(), integration.Options{ConfigDir: t.TempDir()}, true)
	if r != nil || !errors.Is(err, integration.ErrInvalid) {
		t.Fatalf("OpenRoot() = %v, %v, want ErrInvalid", r, err)
	}
}
