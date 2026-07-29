package opencodebackup

import (
	"context"
	"errors"
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
	engine.syncPublishedSnapshot = func(string) error { return syncErr }
	snapshot, err := engine.Create(context.Background(), ModeFull)
	if !errors.Is(err, syncErr) || snapshot.Manifest.SnapshotID == "" || snapshot.Directory == "" {
		t.Fatalf("Create()=%+v, %v", snapshot, err)
	}
	verified, verifyErr := engine.Verify(context.Background(), snapshot.Manifest.SnapshotID)
	if verifyErr != nil || verified.Manifest.SnapshotID != snapshot.Manifest.SnapshotID {
		t.Fatalf("retained snapshot did not verify: %+v, %v", verified, verifyErr)
	}
}

func TestCollectChecksCancellationDuringDiscovery(t *testing.T) {
	root := canonicalCorrectionPath(t, t.TempDir())
	engine := &Engine{sourceRoot: root, managedPaths: []string{"a", "b"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := engine.collect(ctx, ModeManaged); !errors.Is(err, context.Canceled) {
		t.Fatalf("managed collect error=%v", err)
	}
	if _, err := engine.collect(ctx, ModeFull); !errors.Is(err, context.Canceled) {
		t.Fatalf("full collect error=%v", err)
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
