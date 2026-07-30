package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
)

func TestMemoryReadOnlyResolveProjectDoesNotCreateAbsentStore(t *testing.T) {
	storageRoot := filepath.Join(t.TempDir(), "absent-store")
	_, err := NewMemory("cli", true).ResolveProject(context.Background(), config.Options{StorageRoot: storageRoot}, t.TempDir())
	if !errors.Is(err, memory.ErrCorrupt) {
		t.Fatalf("resolve project error = %v, want corrupt absent-store error", err)
	}
	if _, err := os.Stat(storageRoot); !os.IsNotExist(err) {
		t.Fatalf("read-only resolve created storage root: %v", err)
	}
}
