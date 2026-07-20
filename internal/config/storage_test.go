package config

import (
	"context"
	"github.com/vgxness/vgxness/internal/testutil"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestResolveStorageRoot_ProjectOverrideWins(t *testing.T) {
	base, explicit := t.TempDir(), filepath.Join(t.TempDir(), "explicit")
	paths, err := Prepare(context.Background(), Options{StorageRoot: explicit, ProjectDir: filepath.Join(base, "project"), ProjectLocal: true, HomeDir: filepath.Join(base, "home")})
	testutil.NoError(t, err)
	testutil.Require(t, paths.Root == explicit && paths.Database == filepath.Join(explicit, "memory.db"), "unexpected paths: %+v", paths)
}

func TestOpen_InvalidRootLeavesNoPartialState(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "blocked")
	testutil.NoError(t, os.WriteFile(blocked, []byte("file"), 0o600))
	child := filepath.Join(blocked, "child")
	_, err := Prepare(context.Background(), Options{StorageRoot: child})
	testutil.Require(t, err != nil, "expected invalid root")
	_, err = os.Stat(child)
	testutil.Require(t, err != nil, "partial root remains")
}

func TestPrepare_ProjectLocalRejectsSymlink(t *testing.T) {
	base, target := t.TempDir(), t.TempDir()
	testutil.NoError(t, os.Symlink(target, filepath.Join(base, ".vgxness")))
	_, err := Prepare(context.Background(), Options{ProjectDir: base, ProjectLocal: true})
	testutil.Require(t, err != nil, "expected project-local symlink rejection")
}

func TestCleanupCreatedRoot_DoesNotDeleteConcurrentContent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	testutil.NoError(t, os.Mkdir(root, 0o700))
	ready := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		testutil.NoError(t, os.WriteFile(filepath.Join(root, "owned-elsewhere"), []byte("keep"), 0o600))
		close(ready)
	}()
	<-ready
	cleanupCreatedRoot(root)
	wg.Wait()
	_, err := os.Stat(filepath.Join(root, "owned-elsewhere"))
	testutil.NoError(t, err)
}
