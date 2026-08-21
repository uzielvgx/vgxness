package opencodebackup

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRestoreMissingKeepsConcurrentDestination(t *testing.T) {
	engine, snapshot := correctionSnapshot(t, "file", "snapshot bytes")
	if err := os.Remove(engine.sourceRoot + "/file"); err != nil {
		t.Fatal(err)
	}
	refs, err := engine.openRestoreRefs(context.Background(), snapshot.Manifest.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	defer refs.Close()
	if err := refs.source.WriteFile("file", []byte("concurrent winner"), 0o600); err != nil {
		t.Fatal(err)
	}
	created, err := engine.restoreMissing(context.Background(), refs, snapshot.Manifest.Entries[0])
	if created || !errors.Is(err, ErrConflict) {
		t.Fatalf("restoreMissing() = %v, %v; want conflict without publication", created, err)
	}
	data, err := refs.source.ReadFile("file")
	if err != nil || string(data) != "concurrent winner" {
		t.Fatalf("concurrent destination changed: %q, %v", data, err)
	}
	if matches, err := filepath.Glob(filepath.Join(engine.sourceRoot, ".vgxness-restore-*")); err != nil || len(matches) != 0 {
		t.Fatalf("restore temporary files = %v, %v; want none", matches, err)
	}
}

func TestSnapshotDoesNotExposeDirectory(t *testing.T) {
	if _, exists := reflect.TypeOf(Snapshot{}).FieldByName("Directory"); exists {
		t.Fatal("Snapshot exposes a pathname authority")
	}
}

func TestRestoreMissingRemovesReservedDestinationOnCopyFailure(t *testing.T) {
	engine, snapshot := correctionSnapshot(t, "file", "snapshot bytes")
	if err := os.Remove(engine.sourceRoot + "/file"); err != nil {
		t.Fatal(err)
	}
	refs, err := engine.openRestoreRefs(context.Background(), snapshot.Manifest.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	defer refs.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	created, err := engine.restoreMissing(ctx, refs, snapshot.Manifest.Entries[0])
	if created || !errors.Is(err, context.Canceled) {
		t.Fatalf("restoreMissing() = %v, %v; want cleaned destination and cancellation", created, err)
	}
	if _, err := refs.source.Lstat("file"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reserved destination was retained: %v", err)
	}
}

func TestRestoreRetriesAfterPrePublicationCopyFailure(t *testing.T) {
	engine, snapshot := correctionSnapshot(t, "file", "snapshot bytes")
	if err := os.Remove(filepath.Join(engine.sourceRoot, "file")); err != nil {
		t.Fatal(err)
	}
	preview, err := engine.PreviewRestore(context.Background(), snapshot.Manifest.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	copyErr := errors.New("copy failure")
	engine.copySnapshotEntry = func(_ context.Context, _ *os.Root, _ Entry, output io.Writer) error {
		_, _ = output.Write([]byte("partial"))
		return copyErr
	}
	result, err := engine.Restore(context.Background(), RestoreRequest{SnapshotID: snapshot.Manifest.SnapshotID, PreviewSHA256: preview.SHA256})
	if !errors.Is(err, copyErr) || result.Created != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(engine.sourceRoot, "file")); !os.IsNotExist(err) {
		t.Fatalf("failed restore retained destination: %v", err)
	}
	if names, err := filepath.Glob(filepath.Join(engine.sourceRoot, ".vgxness-restore-*")); err != nil || len(names) != 0 {
		t.Fatalf("staging=%v err=%v", names, err)
	}
	engine.copySnapshotEntry = copySnapshotEntry
	result, err = engine.Restore(context.Background(), RestoreRequest{SnapshotID: snapshot.Manifest.SnapshotID, PreviewSHA256: preview.SHA256})
	if err != nil || result.Created != 1 {
		t.Fatalf("retry result=%+v err=%v", result, err)
	}
}

func TestRestoreDoesNotOverwriteConcurrentFinalAtPublish(t *testing.T) {
	engine, snapshot := correctionSnapshot(t, "file", "snapshot bytes")
	if err := os.Remove(filepath.Join(engine.sourceRoot, "file")); err != nil {
		t.Fatal(err)
	}
	preview, err := engine.PreviewRestore(context.Background(), snapshot.Manifest.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	engine.beforeRestorePublish = func(root *os.Root, _ string) error { return root.WriteFile("file", []byte("concurrent"), 0o600) }
	result, err := engine.Restore(context.Background(), RestoreRequest{SnapshotID: snapshot.Manifest.SnapshotID, PreviewSHA256: preview.SHA256})
	if !errors.Is(err, ErrConflict) || result.Created != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	data, err := os.ReadFile(filepath.Join(engine.sourceRoot, "file"))
	if err != nil || string(data) != "concurrent" {
		t.Fatalf("final=%q err=%v", data, err)
	}
	if names, err := filepath.Glob(filepath.Join(engine.sourceRoot, ".vgxness-restore-*")); err != nil || len(names) != 0 {
		t.Fatalf("staging=%v err=%v", names, err)
	}
}
