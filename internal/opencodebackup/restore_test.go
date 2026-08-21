package opencodebackup

import (
	"context"
	"errors"
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

func TestRestoreMissingRetainsReservedDestinationOnCopyFailure(t *testing.T) {
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
	if !created || !errors.Is(err, context.Canceled) {
		t.Fatalf("restoreMissing() = %v, %v; want retained destination and cancellation", created, err)
	}
	if _, err := refs.source.Lstat("file"); err != nil {
		t.Fatalf("reserved destination was removed: %v", err)
	}
}
