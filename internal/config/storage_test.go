package config

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/vgxness/vgxness/internal/testutil"
)

func TestResolveStorageRoot_ProjectOverrideWins(t *testing.T) {
	base, explicit := t.TempDir(), filepath.Join(t.TempDir(), "explicit")
	paths, err := Prepare(context.Background(), Options{StorageRoot: explicit, ProjectDir: filepath.Join(base, "project"), ProjectLocal: true, HomeDir: filepath.Join(base, "home")})
	testutil.NoError(t, err)
	explicit, err = filepath.EvalSymlinks(explicit)
	testutil.NoError(t, err)
	testutil.Require(t, paths.Root == explicit && paths.Database == filepath.Join(explicit, "memory.db") && paths.LegacyDatabase == "", "unexpected paths: %+v", paths)
}

func TestPrepare_DefaultUsesUnifiedDatabaseAndProjectOperationalRoot(t *testing.T) {
	home, project := t.TempDir(), filepath.Join(t.TempDir(), "project")
	testutil.NoError(t, os.MkdirAll(project, 0o700))
	paths, err := Prepare(context.Background(), Options{HomeDir: home, ProjectDir: project})
	testutil.NoError(t, err)
	home, err = filepath.EvalSymlinks(home)
	testutil.NoError(t, err)
	testutil.Require(t, filepath.Dir(paths.Root) == filepath.Join(home, ".vgxness", "projects"), "unexpected project root: %+v", paths)
	testutil.Require(t, paths.Database == filepath.Join(home, ".vgxness", "memory.db"), "database is not unified: %+v", paths)
	testutil.Require(t, paths.LegacyDatabase == filepath.Join(paths.Root, "memory.db"), "legacy path missing: %+v", paths)
}

func TestPrepare_DefaultUsesExistingWorkspaceCaseOnCaseInsensitiveFilesystem(t *testing.T) {
	parent := t.TempDir()
	workspace := filepath.Join(parent, "Development")
	testutil.NoError(t, os.Mkdir(workspace, 0o700))
	misspelled := filepath.Join(parent, "development")
	if _, err := os.Stat(misspelled); err != nil {
		t.Skip("filesystem is case-sensitive")
	}
	home := t.TempDir()
	stored, err := Prepare(context.Background(), Options{HomeDir: home, ProjectDir: workspace})
	testutil.NoError(t, err)
	resolved, err := Prepare(context.Background(), Options{HomeDir: home, ProjectDir: misspelled})
	testutil.NoError(t, err)
	testutil.Require(t, resolved.Root == stored.Root, "case spelling changed root: %q != %q", resolved.Root, stored.Root)
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

func TestPrepare_RejectsAncestorSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on some Windows hosts")
	}
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	testutil.NoError(t, os.Symlink(target, link))
	_, err := Prepare(context.Background(), Options{StorageRoot: filepath.Join(link, "storage")})
	testutil.Require(t, err != nil, "expected ancestor symlink rejection")
}

func TestPrepare_MakesDedicatedRootOwnerPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose POSIX permission bits")
	}
	root := filepath.Join(t.TempDir(), "storage")
	testutil.NoError(t, os.Mkdir(root, 0o755))
	_, err := Prepare(context.Background(), Options{StorageRoot: root})
	testutil.NoError(t, err)
	info, err := os.Stat(root)
	testutil.NoError(t, err)
	testutil.Require(t, info.Mode().Perm() == 0o700, "root mode = %o, want 0700", info.Mode().Perm())
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
