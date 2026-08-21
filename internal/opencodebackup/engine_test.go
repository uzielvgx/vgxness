package opencodebackup_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/opencodebackup"
)

func TestCreateManagedAndFullInclusion(t *testing.T) {
	source := t.TempDir()
	writeFile(t, source, "opencode.json", "config", 0o644)
	writeFile(t, source, ".hidden", "hidden", 0o600)
	writeFile(t, source, "nested/agent.md", "agent", 0o755)

	t.Run("managed", func(t *testing.T) {
		engine, backup := newEngine(t, source, []string{"missing.json", "opencode.json", "nested/agent.md"})
		snapshot, err := engine.Create(context.Background(), opencodebackup.ModeManaged)
		if err != nil {
			t.Fatal(err)
		}
		assertEntryPaths(t, snapshot.Manifest.Entries, []string{"nested/agent.md", "opencode.json"})
		assertPrivateSnapshot(t, backup, snapshot.Manifest)
		if runtime.GOOS != "windows" && snapshot.Manifest.Entries[0].Mode != 0o755 {
			t.Fatalf("source permission metadata = %#o, want 0755", snapshot.Manifest.Entries[0].Mode)
		}
	})

	t.Run("full", func(t *testing.T) {
		engine, backup := newEngine(t, source, nil)
		snapshot, err := engine.Create(context.Background(), opencodebackup.ModeFull)
		if err != nil {
			t.Fatal(err)
		}
		assertEntryPaths(t, snapshot.Manifest.Entries, []string{".hidden", "nested/agent.md", "opencode.json"})
		assertPrivateSnapshot(t, backup, snapshot.Manifest)
	})
}

func TestNewValidatesModesRootsAndManagedPaths(t *testing.T) {
	source := t.TempDir()
	backup := t.TempDir()

	for name, options := range map[string]opencodebackup.Options{
		"backup below source": {SourceRoot: source, BackupRoot: filepath.Join(source, "backups")},
		"source below backup": {SourceRoot: filepath.Join(backup, "source"), BackupRoot: backup},
		"traversal":           {SourceRoot: source, BackupRoot: backup, ManagedPaths: []string{"../secret"}},
		"absolute":            {SourceRoot: source, BackupRoot: backup, ManagedPaths: []string{filepath.Join(source, "file")}},
		"duplicate":           {SourceRoot: source, BackupRoot: backup, ManagedPaths: []string{"a", "a"}},
		"non canonical":       {SourceRoot: source, BackupRoot: backup, ManagedPaths: []string{"a/../b"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := opencodebackup.New(options); !errors.Is(err, opencodebackup.ErrInvalid) {
				t.Fatalf("New() error = %v, want ErrInvalid", err)
			}
		})
	}

	engine, _ := newEngine(t, source, nil)
	if _, err := engine.Create(context.Background(), opencodebackup.Mode("other")); !errors.Is(err, opencodebackup.ErrInvalid) {
		t.Fatalf("Create invalid mode error = %v", err)
	}
}

func TestDefaultBackupRoot(t *testing.T) {
	home := canonicalPath(t, t.TempDir())
	source := canonicalPath(t, t.TempDir())
	writeFile(t, source, "opencode.json", "config", 0o600)
	engine, err := opencodebackup.New(opencodebackup.Options{SourceRoot: source, HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := engine.Create(context.Background(), opencodebackup.ModeFull)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".local", "share", "vgxness", "backups", "opencode", snapshot.Manifest.SnapshotID)
	if snapshot.Directory != want {
		t.Fatalf("snapshot directory = %q, want %q", snapshot.Directory, want)
	}
}

func TestCreateRejectsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation commonly requires elevated Windows privileges")
	}
	t.Run("managed file", func(t *testing.T) {
		source := t.TempDir()
		writeFile(t, source, "target", "secret", 0o600)
		if err := os.Symlink("target", filepath.Join(source, "link")); err != nil {
			t.Fatal(err)
		}
		engine, _ := newEngine(t, source, []string{"link"})
		if _, err := engine.Create(context.Background(), opencodebackup.ModeManaged); !errors.Is(err, opencodebackup.ErrUnsupported) {
			t.Fatalf("Create() error = %v, want ErrUnsupported", err)
		}
	})

	t.Run("full directory", func(t *testing.T) {
		source := t.TempDir()
		outside := t.TempDir()
		writeFile(t, outside, "secret", "secret", 0o600)
		if err := os.Symlink(outside, filepath.Join(source, "linked-dir")); err != nil {
			t.Fatal(err)
		}
		engine, _ := newEngine(t, source, nil)
		if _, err := engine.Create(context.Background(), opencodebackup.ModeFull); !errors.Is(err, opencodebackup.ErrUnsupported) {
			t.Fatalf("Create() error = %v, want ErrUnsupported", err)
		}
	})
}

func TestCreateAvoidsReservationOnCancellationAndRetainsIncompleteFailure(t *testing.T) {
	t.Run("cancelled", func(t *testing.T) {
		source := t.TempDir()
		writeFile(t, source, "file", strings.Repeat("x", 1024), 0o600)
		engine, backup := newEngine(t, source, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := engine.Create(ctx, opencodebackup.ModeFull); !errors.Is(err, context.Canceled) {
			t.Fatalf("Create() error = %v, want context cancellation", err)
		}
		assertDirectoryEmpty(t, backup)
	})

	t.Run("copy failure", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows does not enforce Unix read permission bits")
		}
		source := t.TempDir()
		writeFile(t, source, "a-readable", "ok", 0o600)
		writeFile(t, source, "z-unreadable", "no", 0o000)
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(source, "z-unreadable"), 0o600) })
		engine, backup := newEngine(t, source, nil)
		if _, err := engine.Create(context.Background(), opencodebackup.ModeFull); err == nil {
			t.Fatal("Create() unexpectedly succeeded")
		}
		summaries, listErr := engine.List(context.Background())
		if listErr != nil || len(summaries) != 0 {
			t.Fatalf("List() = %v, %v", summaries, listErr)
		}
		entries, readErr := os.ReadDir(backup)
		if readErr != nil || len(entries) != 1 || !entries[0].IsDir() {
			t.Fatalf("failed reservation missing: %v, %v", entries, readErr)
		}
		if _, verifyErr := engine.Verify(context.Background(), entries[0].Name()); !errors.Is(verifyErr, opencodebackup.ErrCorrupt) {
			t.Fatalf("Verify() error = %v, want ErrCorrupt", verifyErr)
		}
	})
}

func TestCreateRejectsOversizedSourceFile(t *testing.T) {
	source := t.TempDir()
	file, err := os.Create(filepath.Join(source, "oversized"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(opencodebackup.MaxFileSize + 1); err != nil {
		_ = file.Close()
		t.Skipf("filesystem does not support a sparse bounded-size test file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	engine, backup := newEngine(t, source, nil)
	if _, err := engine.Create(context.Background(), opencodebackup.ModeFull); !errors.Is(err, opencodebackup.ErrInvalid) {
		t.Fatalf("Create() error = %v, want ErrInvalid", err)
	}
	assertDirectoryEmpty(t, backup)
}

func TestVerifyRejectsTamperingAndMalformedManifests(t *testing.T) {
	newSnapshot := func(t *testing.T) (*opencodebackup.Engine, string, opencodebackup.Snapshot) {
		t.Helper()
		source := t.TempDir()
		writeFile(t, source, "config.json", "original", 0o600)
		engine, backup := newEngine(t, source, nil)
		snapshot, err := engine.Create(context.Background(), opencodebackup.ModeFull)
		if err != nil {
			t.Fatal(err)
		}
		return engine, backup, snapshot
	}

	t.Run("payload", func(t *testing.T) {
		engine, _, snapshot := newSnapshot(t)
		payload := filepath.Join(snapshot.Directory, "files", "config.json")
		if err := os.WriteFile(payload, []byte("tampered"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Verify(context.Background(), snapshot.Manifest.SnapshotID); !errors.Is(err, opencodebackup.ErrCorrupt) {
			t.Fatalf("Verify() error = %v, want ErrCorrupt", err)
		}
	})

	t.Run("unknown manifest field", func(t *testing.T) {
		engine, _, snapshot := newSnapshot(t)
		manifest := filepath.Join(snapshot.Directory, "manifest.json")
		data, err := os.ReadFile(manifest)
		if err != nil {
			t.Fatal(err)
		}
		data = []byte(strings.Replace(string(data), "{", `{"unknown":true,`, 1))
		if err := os.WriteFile(manifest, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Verify(context.Background(), snapshot.Manifest.SnapshotID); !errors.Is(err, opencodebackup.ErrCorrupt) {
			t.Fatalf("Verify() error = %v, want ErrCorrupt", err)
		}
	})

	t.Run("trailing JSON", func(t *testing.T) {
		engine, _, snapshot := newSnapshot(t)
		manifest := filepath.Join(snapshot.Directory, "manifest.json")
		file, err := os.OpenFile(manifest, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.WriteString("\n{}")
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatalf("tamper manifest: write=%v close=%v", writeErr, closeErr)
		}
		if _, err := engine.Verify(context.Background(), snapshot.Manifest.SnapshotID); !errors.Is(err, opencodebackup.ErrCorrupt) {
			t.Fatalf("Verify() error = %v, want ErrCorrupt", err)
		}
	})

	t.Run("extra payload", func(t *testing.T) {
		engine, _, snapshot := newSnapshot(t)
		writeFile(t, filepath.Join(snapshot.Directory, "files"), "extra", "extra", 0o600)
		if _, err := engine.Verify(context.Background(), snapshot.Manifest.SnapshotID); !errors.Is(err, opencodebackup.ErrCorrupt) {
			t.Fatalf("Verify() error = %v, want ErrCorrupt", err)
		}
	})

	t.Run("extra payload directory", func(t *testing.T) {
		engine, _, snapshot := newSnapshot(t)
		if err := os.Mkdir(filepath.Join(snapshot.Directory, "files", "extra-dir"), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Verify(context.Background(), snapshot.Manifest.SnapshotID); !errors.Is(err, opencodebackup.ErrCorrupt) {
			t.Fatalf("Verify() error = %v, want ErrCorrupt", err)
		}
	})

	t.Run("permissive manifest", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows does not expose Unix permission bits")
		}
		engine, _, snapshot := newSnapshot(t)
		manifest := filepath.Join(snapshot.Directory, "manifest.json")
		if err := os.Chmod(manifest, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Verify(context.Background(), snapshot.Manifest.SnapshotID); !errors.Is(err, opencodebackup.ErrCorrupt) {
			t.Fatalf("Verify() error = %v, want ErrCorrupt", err)
		}
	})
}

func TestListIsVerifiedAndNewestFirst(t *testing.T) {
	source := t.TempDir()
	writeFile(t, source, "config", "one", 0o600)
	engine, _ := newEngine(t, source, nil)
	first, err := engine.Create(context.Background(), opencodebackup.ModeFull)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, source, "config", "two", 0o600)
	second, err := engine.Create(context.Background(), opencodebackup.ModeFull)
	if err != nil {
		t.Fatal(err)
	}

	summaries, err := engine.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := []string{summaries[0].SnapshotID, summaries[1].SnapshotID}, []string{second.Manifest.SnapshotID, first.Manifest.SnapshotID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("List IDs = %v, want %v", got, want)
	}

	payload := filepath.Join(first.Directory, "files", "config")
	if err := os.WriteFile(payload, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.List(context.Background()); !errors.Is(err, opencodebackup.ErrCorrupt) {
		t.Fatalf("List() error = %v, want ErrCorrupt", err)
	}
}

func TestListDoesNotCreateBackupRootAfterCancellation(t *testing.T) {
	source := t.TempDir()
	engine, backup := newEngine(t, source, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := engine.List(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("List() error = %v, want context cancellation", err)
	}
	if _, err := os.Lstat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled List created backup root: %v", err)
	}
}

func TestVerifyDoesNotCreateBackupRootAfterCancellation(t *testing.T) {
	source := t.TempDir()
	engine, backup := newEngine(t, source, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := engine.Verify(ctx, "20260820T000000.000000000Z-0123456789abcdef"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify() error = %v, want context cancellation", err)
	}
	if _, err := os.Lstat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled Verify created backup root: %v", err)
	}
}

func TestMergeRestore(t *testing.T) {
	source := t.TempDir()
	writeFile(t, source, "missing", "snapshot missing", 0o644)
	writeFile(t, source, "identical", "same", 0o644)
	writeFile(t, source, "conflict", "snapshot conflict", 0o644)
	engine, _ := newEngine(t, source, nil)
	snapshot, err := engine.Create(context.Background(), opencodebackup.ModeFull)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(source, "missing")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, source, "conflict", "destination conflict", 0o666)
	writeFile(t, source, "unrelated", "leave me", 0o600)

	preview, err := engine.PreviewRestore(context.Background(), snapshot.Manifest.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(preview.Missing, []string{"missing"}) || !reflect.DeepEqual(preview.Identical, []string{"identical"}) || !reflect.DeepEqual(preview.Conflicts, []string{"conflict"}) || preview.SHA256 == "" {
		t.Fatalf("unexpected preview: %+v", preview)
	}

	result, err := engine.Restore(context.Background(), opencodebackup.RestoreRequest{SnapshotID: snapshot.Manifest.SnapshotID, PreviewSHA256: preview.SHA256})
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 1 || result.Identical != 1 || result.Replaced != 0 || !reflect.DeepEqual(result.Unresolved, []string{"conflict"}) {
		t.Fatalf("unexpected restore result: %+v", result)
	}
	assertFile(t, filepath.Join(source, "missing"), "snapshot missing", 0o600)
	assertFile(t, filepath.Join(source, "conflict"), "destination conflict", 0o666)
	assertFile(t, filepath.Join(source, "unrelated"), "leave me", 0o600)

	if _, err := engine.Restore(context.Background(), opencodebackup.RestoreRequest{SnapshotID: snapshot.Manifest.SnapshotID, PreviewSHA256: preview.SHA256, ReplaceConflicts: []string{"conflict"}}); !errors.Is(err, opencodebackup.ErrUnsupported) {
		t.Fatalf("conflict replacement error = %v, want ErrUnsupported", err)
	}

	fresh, err := engine.PreviewRestore(context.Background(), snapshot.Manifest.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	result, err = engine.Restore(context.Background(), opencodebackup.RestoreRequest{SnapshotID: snapshot.Manifest.SnapshotID, PreviewSHA256: fresh.SHA256, ReplaceConflicts: []string{"conflict"}})
	if !errors.Is(err, opencodebackup.ErrUnsupported) || result.Replaced != 0 {
		t.Fatalf("replacement was not rejected: result=%+v err=%v", result, err)
	}
	assertFile(t, filepath.Join(source, "conflict"), "destination conflict", 0o666)
	assertFile(t, filepath.Join(source, "unrelated"), "leave me", 0o600)

	retryPreview, err := engine.PreviewRestore(context.Background(), snapshot.Manifest.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := engine.Restore(context.Background(), opencodebackup.RestoreRequest{SnapshotID: snapshot.Manifest.SnapshotID, PreviewSHA256: retryPreview.SHA256})
	if err != nil {
		t.Fatal(err)
	}
	if retry.Created != 0 || retry.Replaced != 0 || retry.Identical != 2 || !reflect.DeepEqual(retry.Unresolved, []string{"conflict"}) {
		t.Fatalf("unexpected retry result: %+v", retry)
	}
}

func TestRestoreRejectsUnselectedAndUnsafeConflictRequests(t *testing.T) {
	source := t.TempDir()
	writeFile(t, source, "file", "snapshot", 0o600)
	engine, _ := newEngine(t, source, nil)
	snapshot, err := engine.Create(context.Background(), opencodebackup.ModeFull)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, source, "file", "changed", 0o600)
	preview, err := engine.PreviewRestore(context.Background(), snapshot.Manifest.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	for name, paths := range map[string][]string{
		"not a conflict": {"other"},
		"traversal":      {"../file"},
		"duplicate":      {"file", "file"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := engine.Restore(context.Background(), opencodebackup.RestoreRequest{SnapshotID: snapshot.Manifest.SnapshotID, PreviewSHA256: preview.SHA256, ReplaceConflicts: paths})
			if !errors.Is(err, opencodebackup.ErrUnsupported) {
				t.Fatalf("Restore() error = %v, want ErrUnsupported", err)
			}
		})
	}

	if runtime.GOOS == "windows" {
		t.Skip("symlink creation commonly requires elevated Windows privileges")
	}
	if err := os.Remove(filepath.Join(source, "file")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-target", filepath.Join(source, "file")); err != nil {
		t.Fatal(err)
	}
	preview, err = engine.PreviewRestore(context.Background(), snapshot.Manifest.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Restore(context.Background(), opencodebackup.RestoreRequest{SnapshotID: snapshot.Manifest.SnapshotID, PreviewSHA256: preview.SHA256, ReplaceConflicts: []string{"file"}}); !errors.Is(err, opencodebackup.ErrUnsupported) {
		t.Fatalf("symlink Restore() error = %v, want ErrUnsupported", err)
	}
	info, err := os.Lstat(filepath.Join(source, "file"))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink conflict was modified: info=%v err=%v", info, err)
	}

	if err := os.Remove(filepath.Join(source, "file")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(source, "file"), 0o700); err != nil {
		t.Fatal(err)
	}
	preview, err = engine.PreviewRestore(context.Background(), snapshot.Manifest.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Restore(context.Background(), opencodebackup.RestoreRequest{SnapshotID: snapshot.Manifest.SnapshotID, PreviewSHA256: preview.SHA256, ReplaceConflicts: []string{"file"}}); !errors.Is(err, opencodebackup.ErrUnsupported) {
		t.Fatalf("directory Restore() error = %v, want ErrUnsupported", err)
	}
	info, err = os.Lstat(filepath.Join(source, "file"))
	if err != nil || !info.IsDir() {
		t.Fatalf("directory conflict was modified: info=%v err=%v", info, err)
	}
}

func TestRestoreCanBeBoundedToExplicitSnapshotPaths(t *testing.T) {
	source := t.TempDir()
	writeFile(t, source, "managed", "managed snapshot", 0o600)
	writeFile(t, source, "unrelated", "unrelated snapshot", 0o600)
	engine, _ := newEngine(t, source, nil)
	snapshot, err := engine.Create(context.Background(), opencodebackup.ModeFull)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(source, "managed")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(source, "unrelated")); err != nil {
		t.Fatal(err)
	}
	preview, err := engine.PreviewRestore(context.Background(), snapshot.Manifest.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Restore(context.Background(), opencodebackup.RestoreRequest{
		SnapshotID: snapshot.Manifest.SnapshotID, PreviewSHA256: preview.SHA256, IncludePaths: []string{"managed"},
	})
	if err != nil || result.Created != 1 {
		t.Fatalf("Restore() = %+v, %v", result, err)
	}
	assertFile(t, filepath.Join(source, "managed"), "managed snapshot", 0o600)
	if _, err := os.Lstat(filepath.Join(source, "unrelated")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("bounded restore created unrelated file: %v", err)
	}
}

func newEngine(t *testing.T, source string, managed []string) (*opencodebackup.Engine, string) {
	t.Helper()
	source = canonicalPath(t, source)
	backup := filepath.Join(canonicalPath(t, t.TempDir()), "backup")
	engine, err := opencodebackup.New(opencodebackup.Options{SourceRoot: source, BackupRoot: backup, ManagedPaths: managed})
	if err != nil {
		t.Fatal(err)
	}
	return engine, backup
}

func canonicalPath(t *testing.T, value string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(value)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func writeFile(t *testing.T, root, relative, contents string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func assertEntryPaths(t *testing.T, entries []opencodebackup.Entry, want []string) {
	t.Helper()
	got := make([]string, len(entries))
	for index, entry := range entries {
		got[index] = entry.Path
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entry paths = %v, want %v", got, want)
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("entry paths are not sorted: %v", got)
	}
}

func assertPrivateSnapshot(t *testing.T, backup string, manifest opencodebackup.Manifest) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	assertMode(t, backup, 0o700)
	snapshot := filepath.Join(backup, manifest.SnapshotID)
	assertMode(t, snapshot, 0o700)
	assertMode(t, filepath.Join(snapshot, "manifest.json"), 0o600)
	assertMode(t, filepath.Join(snapshot, "files"), 0o700)
	for _, entry := range manifest.Entries {
		assertMode(t, filepath.Join(snapshot, "files", filepath.FromSlash(entry.Path)), 0o600)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %#o, want %#o", path, got, want)
	}
}

func assertDirectoryEmpty(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("directory contains temporary/published entries: %v", entries)
	}
}

func assertFile(t *testing.T, path, want string, mode os.FileMode) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s contents = %q, want %q", path, data, want)
	}
	if runtime.GOOS != "windows" {
		assertMode(t, path, mode)
	}
}
