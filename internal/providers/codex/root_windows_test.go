//go:build windows

package codex

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
)

func TestOpenRootUsesCurrentUserDirectoryOnWindows(t *testing.T) {
	r, err := OpenRoot(context.Background(), integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.Mkdir("agents"); err != nil {
		t.Fatal(err)
	}
}
