package opencodebackup

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreMissingPublishesCompleteFileWithoutOverwritingRaceWinner(t *testing.T) {
	engine, snapshot := correctionSnapshot(t, "file", "snapshot bytes")
	if err := os.Remove(filepath.Join(engine.sourceRoot, "file")); err != nil {
		t.Fatal(err)
	}
	preview, err := engine.PreviewRestore(context.Background(), snapshot.Manifest.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	engine.publishRestoreFile = func(_, destination string) error {
		if err := os.WriteFile(destination, []byte("concurrent winner"), 0o600); err != nil {
			return err
		}
		return os.ErrExist
	}
	result, err := engine.Restore(context.Background(), RestoreRequest{SnapshotID: snapshot.Manifest.SnapshotID, PreviewSHA256: preview.SHA256})
	if !errors.Is(err, ErrConflict) || result.Created != 0 {
		t.Fatalf("Restore()=%+v, %v", result, err)
	}
	data, readErr := os.ReadFile(filepath.Join(engine.sourceRoot, "file"))
	if readErr != nil || string(data) != "concurrent winner" {
		t.Fatalf("race winner changed: %q, %v", data, readErr)
	}
	if matches, _ := filepath.Glob(filepath.Join(engine.sourceRoot, ".vgxness-restore-*")); len(matches) != 0 {
		t.Fatalf("restore temporary files leaked: %v", matches)
	}
}

func TestRestoreReturnsPublishedPartialResultOnDurabilityFailure(t *testing.T) {
	engine, snapshot := correctionSnapshot(t, "file", "snapshot bytes")
	if err := os.Remove(filepath.Join(engine.sourceRoot, "file")); err != nil {
		t.Fatal(err)
	}
	preview, err := engine.PreviewRestore(context.Background(), snapshot.Manifest.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	durabilityErr := errors.New("injected restore directory sync failure")
	engine.syncRestoreDirectory = func(string) error { return durabilityErr }
	result, err := engine.Restore(context.Background(), RestoreRequest{SnapshotID: snapshot.Manifest.SnapshotID, PreviewSHA256: preview.SHA256})
	if !errors.Is(err, durabilityErr) || result.Created != 1 {
		t.Fatalf("Restore()=%+v, %v", result, err)
	}
	data, readErr := os.ReadFile(filepath.Join(engine.sourceRoot, "file"))
	if readErr != nil || string(data) != "snapshot bytes" {
		t.Fatalf("complete published file was not retained: %q, %v", data, readErr)
	}
}

func TestRestoreCancellationReturnsAccumulatedResult(t *testing.T) {
	source := t.TempDir()
	backup := filepath.Join(t.TempDir(), "backup")
	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	engine, err := New(Options{SourceRoot: canonicalCorrectionPath(t, source), BackupRoot: canonicalCorrectionPath(t, filepath.Dir(backup)) + string(filepath.Separator) + filepath.Base(backup)})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := engine.Create(context.Background(), ModeFull)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if err := os.Remove(filepath.Join(source, name)); err != nil {
			t.Fatal(err)
		}
	}
	preview, err := engine.PreviewRestore(context.Background(), snapshot.Manifest.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	engine.syncRestoreDirectory = func(directory string) error {
		calls++
		if calls == 1 {
			cancel()
		}
		return syncDirectory(directory)
	}
	result, err := engine.Restore(ctx, RestoreRequest{SnapshotID: snapshot.Manifest.SnapshotID, PreviewSHA256: preview.SHA256})
	if !errors.Is(err, context.Canceled) || result.Created != 1 {
		t.Fatalf("Restore()=%+v, %v", result, err)
	}
}

func TestCreateReturnsRetainedSnapshotOnPublicationSyncFailure(t *testing.T) {
	source := canonicalCorrectionPath(t, t.TempDir())
	backup := filepath.Join(canonicalCorrectionPath(t, t.TempDir()), "backup")
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := New(Options{SourceRoot: source, BackupRoot: backup})
	if err != nil {
		t.Fatal(err)
	}
	syncErr := errors.New("injected publication sync failure")
	engine.syncBackupRoot = func(*os.Root) error { return syncErr }
	snapshot, err := engine.Create(context.Background(), ModeFull)
	if !errors.Is(err, syncErr) || snapshot.Manifest.SnapshotID == "" || snapshot.Directory == "" {
		t.Fatalf("Create()=%+v, %v", snapshot, err)
	}
	verified, verifyErr := engine.Verify(context.Background(), snapshot.Manifest.SnapshotID)
	if verifyErr != nil || verified.Manifest.SnapshotID != snapshot.Manifest.SnapshotID {
		t.Fatalf("retained snapshot did not verify: %+v, %v", verified, verifyErr)
	}
}

func TestCreateRejectsBackupRootReplacementDuringFinalSync(t *testing.T) {
	source := canonicalCorrectionPath(t, t.TempDir())
	backup := filepath.Join(canonicalCorrectionPath(t, t.TempDir()), "backup")
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := New(Options{SourceRoot: source, BackupRoot: backup})
	if err != nil {
		t.Fatal(err)
	}
	engine.syncBackupRoot = func(*os.Root) error {
		if err := os.Rename(backup, backup+"-old"); err != nil {
			return err
		}
		return os.Mkdir(backup, 0o700)
	}
	snapshot, err := engine.Create(context.Background(), ModeFull)
	if !errors.Is(err, ErrConflict) || snapshot.Directory != "" {
		t.Fatalf("Create() = %+v, %v; want empty snapshot, ErrConflict", snapshot, err)
	}
}

func TestCreateRejectsSnapshotReplacementDuringFinalSync(t *testing.T) {
	source := canonicalCorrectionPath(t, t.TempDir())
	backup := filepath.Join(canonicalCorrectionPath(t, t.TempDir()), "backup")
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := New(Options{SourceRoot: source, BackupRoot: backup})
	if err != nil {
		t.Fatal(err)
	}
	engine.syncBackupRoot = func(root *os.Root) error {
		entries, err := fs.ReadDir(root.FS(), ".")
		if err != nil || len(entries) != 1 {
			return errors.Join(err, ErrConflict)
		}
		id := entries[0].Name()
		if err := root.Rename(id, "replaced"); err != nil {
			return err
		}
		return root.Mkdir(id, 0o700)
	}
	snapshot, err := engine.Create(context.Background(), ModeFull)
	if !errors.Is(err, ErrConflict) || snapshot.Directory != "" {
		t.Fatalf("Create() = %+v, %v; want empty snapshot, ErrConflict", snapshot, err)
	}
}

func TestCollectChecksCancellationDuringDiscovery(t *testing.T) {
	root := canonicalCorrectionPath(t, t.TempDir())
	engine := &Engine{sourceRoot: root, managedPaths: []string{"a", "b"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := engine.collect(ctx, nil, ModeManaged); !errors.Is(err, context.Canceled) {
		t.Fatalf("managed collect error=%v", err)
	}
	if _, err := engine.collect(ctx, nil, ModeFull); !errors.Is(err, context.Canceled) {
		t.Fatalf("full collect error=%v", err)
	}
}

func TestListSkipsIncompleteReservationsWithoutDeletingThem(t *testing.T) {
	for name, mark := range map[string]bool{"empty": false, "marked": true} {
		t.Run(name, func(t *testing.T) {
			source := canonicalCorrectionPath(t, t.TempDir())
			backup := filepath.Join(canonicalCorrectionPath(t, t.TempDir()), "backup")
			engine, err := New(Options{SourceRoot: source, BackupRoot: backup})
			if err != nil {
				t.Fatal(err)
			}
			anchor, err := engine.backupRootAnchor()
			if err != nil {
				t.Fatal(err)
			}
			root, err := anchor.open()
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			id := "20260820T000000.000000000Z-0123456789abcdef"
			if err := root.Mkdir(id, 0o700); err != nil {
				t.Fatal(err)
			}
			if mark {
				snapshot, err := root.OpenRoot(id)
				if err != nil {
					t.Fatal(err)
				}
				file, err := snapshot.OpenFile(".incomplete", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
				if err == nil {
					err = file.Sync()
				}
				if closeErr := file.Close(); err == nil {
					err = closeErr
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if summaries, err := engine.List(context.Background()); err != nil || len(summaries) != 0 {
				t.Fatalf("List() = %v, %v", summaries, err)
			}
			if _, err := engine.Verify(context.Background(), id); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("Verify() error=%v, want ErrCorrupt", err)
			}
			if info, err := root.Lstat(id); err != nil || !info.IsDir() {
				t.Fatalf("incomplete reservation removed: %v, %v", info, err)
			}
		})
	}
}

func TestManifestRejectsExplicitEmptyLauncherPaths(t *testing.T) {
	for _, field := range []string{"launcherPath", "manifestPath"} {
		document := `{"schemaVersion":"1","snapshotId":"20260729T120000.000000000Z-0123456789abcdef","createdAt":"2026-07-29T12:00:00Z","mode":"managed","sourceRoot":"/root","entries":[],"totalBytes":0,"launcher":{"version":"1","managedBy":"vgxness","` + field + `":""}}`
		if err := requireManifestFields([]byte(document)); err == nil {
			t.Fatalf("explicit empty %s was accepted", field)
		}
	}
}

func TestNewRejectsPhysicalRootAliasesAndOverlap(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(source, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	for name, backup := range map[string]string{"same alias": alias, "child through alias": filepath.Join(alias, "backup")} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(Options{SourceRoot: source, BackupRoot: backup}); !errors.Is(err, ErrInvalid) {
				t.Fatalf("New() error=%v, want ErrInvalid", err)
			}
		})
	}
}

func TestCreateRejectsPinnedSourceReplacement(t *testing.T) {
	parent := t.TempDir()
	source, backup := filepath.Join(parent, "source"), filepath.Join(parent, "backup")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := New(Options{SourceRoot: source, BackupRoot: backup})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(source, filepath.Join(parent, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Create(context.Background(), ModeFull); !errors.Is(err, ErrConflict) {
		t.Fatalf("Create() error=%v, want ErrConflict", err)
	}
}

func TestPreviewRejectsPinnedSourceReplacement(t *testing.T) {
	parent := t.TempDir()
	source, backup := filepath.Join(parent, "source"), filepath.Join(parent, "backup")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := New(Options{SourceRoot: source, BackupRoot: backup})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := engine.Create(context.Background(), ModeFull)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(source, filepath.Join(parent, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.PreviewRestore(context.Background(), snapshot.Manifest.SnapshotID); !errors.Is(err, ErrConflict) {
		t.Fatalf("PreviewRestore() error=%v, want ErrConflict", err)
	}
}

func TestOpenRestoreRefsCancellationDoesNotCreateBackupRoot(t *testing.T) {
	source := canonicalCorrectionPath(t, t.TempDir())
	backup := filepath.Join(canonicalCorrectionPath(t, t.TempDir()), "backup")
	engine, err := New(Options{SourceRoot: source, BackupRoot: backup})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if refs, err := engine.openRestoreRefs(ctx, "20260820T000000.000000000Z-0123456789abcdef"); refs != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("openRestoreRefs() = %v, %v; want nil, cancellation", refs, err)
	}
	if _, err := os.Lstat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled ref opening created backup root: %v", err)
	}
}

func correctionSnapshot(t *testing.T, name, contents string) (*Engine, Snapshot) {
	t.Helper()
	source := canonicalCorrectionPath(t, t.TempDir())
	backup := filepath.Join(canonicalCorrectionPath(t, t.TempDir()), "backup")
	if err := os.WriteFile(filepath.Join(source, name), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := New(Options{SourceRoot: source, BackupRoot: backup})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := engine.Create(context.Background(), ModeFull)
	if err != nil {
		t.Fatal(err)
	}
	return engine, snapshot
}

func canonicalCorrectionPath(t *testing.T, value string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(value)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(resolved, string(filepath.Separator))
}
