package chronicle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/vgxness/vgxness/internal/contracts"
)

func TestSnapshotStoreWriteActiveAndRecover(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	latest := appendActiveHistory(t, log, "run-1")
	current := activeCurrent(latest)
	document := runSnapshot(t, "running", nil)

	if err := store.WriteActive(context.Background(), document, current); err != nil {
		t.Fatal(err)
	}
	if current.RunFile != "" || current.LogFile != "" {
		t.Fatal("WriteActive mutated the caller's current snapshot")
	}
	written, present, err := ReadCurrent(context.Background(), store.CurrentPath())
	if err != nil || !present {
		t.Fatalf("read current: present=%v err=%v", present, err)
	}
	if written.RunFile != activeRunFile("run-1", mustReadableJSON(t, document)) || written.LogFile != "logs/run-1.jsonl" {
		t.Fatalf("unexpected storage references: %+v", written)
	}
	recovered, err := store.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recovered.RunID != "run-1" || recovered.Status != "running" || !recovered.CurrentPresent || len(recovered.Events) != 6 || recovered.Events[len(recovered.Events)-1].ID != latest {
		t.Fatalf("unexpected recovery state: %+v", recovered)
	}
	if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(written.RunFile))); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("run snapshot permissions: info=%v err=%v", info, err)
	}
	if _, err := os.Lstat(store.RunPath()); !os.IsNotExist(err) {
		t.Fatalf("active write published terminal path: %v", err)
	}
}

func TestSnapshotStoreInvalidInputDoesNotMutateSnapshots(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	latest := appendActiveHistory(t, log, "run-1")

	if err := store.WriteActive(context.Background(), []byte(`{}`), activeCurrent(latest)); !errors.Is(err, contracts.ErrInvalid) {
		t.Fatalf("expected contract failure, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "runs")); !os.IsNotExist(err) {
		t.Fatalf("invalid snapshot created runs storage: %v", err)
	}
	if _, err := os.Lstat(store.CurrentPath()); !os.IsNotExist(err) {
		t.Fatalf("invalid snapshot created current pointer: %v", err)
	}
}

func TestSnapshotStoreRejectsProjectionMismatchBeforeMutation(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	appendActiveHistory(t, log, "run-1")
	current := activeCurrent("missing-event")

	if err := store.WriteActive(context.Background(), runSnapshot(t, "running", nil), current); !errors.Is(err, ErrInconsistent) {
		t.Fatalf("expected inconsistency, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "runs")); !os.IsNotExist(err) {
		t.Fatalf("mismatch created runs storage: %v", err)
	}
}

func TestSnapshotStoreRejectsIneligibleSelectedProvider(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	latest := appendActiveHistory(t, log, "run-1")
	var document map[string]any
	if err := json.Unmarshal(runSnapshot(t, "running", nil), &document); err != nil {
		t.Fatal(err)
	}
	selection := document["orchestratorSelection"].(map[string]any)
	selection["candidates"].([]any)[0].(map[string]any)["eligible"] = false
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.WriteActive(context.Background(), data, activeCurrent(latest)); !errors.Is(err, ErrInconsistent) {
		t.Fatalf("expected semantic inconsistency, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "runs")); !os.IsNotExist(err) {
		t.Fatalf("ineligible selection mutated snapshots: %v", err)
	}
}

func TestSnapshotStoreRejectsSelectedProviderCapabilityMismatch(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	latest := appendActiveHistory(t, log, "run-1")
	var document map[string]any
	if err := json.Unmarshal(runSnapshot(t, "running", nil), &document); err != nil {
		t.Fatal(err)
	}
	selection := document["orchestratorSelection"].(map[string]any)
	candidate := selection["candidates"].([]any)[0].(map[string]any)
	candidate["capabilities"].([]any)[0].(map[string]any)["version"] = "2"
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.WriteActive(context.Background(), data, activeCurrent(latest)); !errors.Is(err, ErrInconsistent) {
		t.Fatalf("expected capability inconsistency, got %v", err)
	}
}

func TestSnapshotStorePreservesLastValidFilesOnRejectedUpdate(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	latest := appendActiveHistory(t, log, "run-1")
	if err := store.WriteActive(context.Background(), runSnapshot(t, "running", nil), activeCurrent(latest)); err != nil {
		t.Fatal(err)
	}
	beforeCurrent := mustRead(t, store.CurrentPath())
	beforePointer, _, err := ReadCurrent(context.Background(), store.CurrentPath())
	if err != nil {
		t.Fatal(err)
	}
	activePath := filepath.Join(root, filepath.FromSlash(beforePointer.RunFile))
	beforeRun := mustRead(t, activePath)

	if err := store.WriteActive(context.Background(), []byte(`{"schemaVersion":"1"}`), activeCurrent(latest)); err == nil {
		t.Fatal("expected rejected update")
	}
	if !bytes.Equal(beforeRun, mustRead(t, activePath)) || !bytes.Equal(beforeCurrent, mustRead(t, store.CurrentPath())) {
		t.Fatal("rejected update changed the last valid snapshots")
	}
}

func TestSnapshotStoreCompletesCommitAfterCancellationPointOfNoReturn(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	latest := appendActiveHistory(t, log, "run-1")
	ctx, cancel := context.WithCancel(context.Background())
	original := store.writeJSON
	store.writeJSON = func(writeCtx context.Context, path string, data []byte, limit int64, schemaURI, expectedID string) error {
		err := original(writeCtx, path, data, limit, schemaURI, expectedID)
		if err == nil && schemaURI == contracts.RunSchemaURI {
			cancel()
		}
		return err
	}

	if err := store.WriteActive(ctx, runSnapshot(t, "running", nil), activeCurrent(latest)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Recover(context.Background()); err != nil {
		t.Fatalf("cancelled commit left inconsistent state: %v", err)
	}
}

func TestSnapshotStoreCrashBeforeActivePointerCommitPreservesPreviousSnapshot(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	latest := appendActiveHistory(t, log, "run-1")
	if err := store.WriteActive(context.Background(), runSnapshot(t, "running", nil), activeCurrent(latest)); err != nil {
		t.Fatal(err)
	}
	beforeCurrent := mustRead(t, store.CurrentPath())
	previous, present, err := ReadCurrent(context.Background(), store.CurrentPath())
	if err != nil || !present {
		t.Fatalf("read previous pointer: present=%v err=%v", present, err)
	}

	updatedDocument := activeRunSnapshotAt(t, "2026-07-21T12:01:10Z")
	updatedCurrent := activeCurrent(latest)
	updatedCurrent.UpdatedAt = "2026-07-21T12:01:10Z"
	stagedPath := filepath.Join(root, filepath.FromSlash(activeRunFile("run-1", mustReadableJSON(t, updatedDocument))))
	crashErr := errors.New("simulated crash before pointer commit")
	original := store.writeJSON
	store.writeJSON = func(writeCtx context.Context, path string, data []byte, limit int64, schemaURI, expectedID string) error {
		if path == store.CurrentPath() {
			return crashErr
		}
		return original(writeCtx, path, data, limit, schemaURI, expectedID)
	}

	if err := store.WriteActive(context.Background(), updatedDocument, updatedCurrent); !errors.Is(err, crashErr) {
		t.Fatalf("expected simulated crash, got %v", err)
	}
	if !bytes.Equal(beforeCurrent, mustRead(t, store.CurrentPath())) {
		t.Fatal("failed active commit changed the authoritative pointer")
	}
	if _, err := os.Stat(stagedPath); err != nil {
		t.Fatalf("immutable staged snapshot missing: %v", err)
	}

	restarted, err := NewSnapshotStore(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := restarted.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Current.RunFile != previous.RunFile || recovered.Current.UpdatedAt != previous.UpdatedAt {
		t.Fatalf("recovery did not preserve previous committed state: %+v", recovered.Current)
	}
}

func TestSnapshotStoreRejectsActiveSnapshotDigestMismatch(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	latest := appendActiveHistory(t, log, "run-1")
	if err := store.WriteActive(context.Background(), runSnapshot(t, "running", nil), activeCurrent(latest)); err != nil {
		t.Fatal(err)
	}
	current, present, err := ReadCurrent(context.Background(), store.CurrentPath())
	if err != nil || !present {
		t.Fatalf("read current: present=%v err=%v", present, err)
	}
	path := filepath.Join(root, filepath.FromSlash(current.RunFile))
	if err := os.WriteFile(path, mustReadableJSON(t, activeRunSnapshotAt(t, "2026-07-21T12:01:10Z")), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Recover(context.Background()); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected digest corruption, got %v", err)
	}
}

func TestSnapshotStoreRejectsPermissiveImmutableSnapshot(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	latest := appendActiveHistory(t, log, "run-1")
	document := runSnapshot(t, "running", nil)
	data := mustReadableJSON(t, document)
	if err := os.Mkdir(filepath.Join(root, "runs"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(activeRunFile("run-1", data)))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := store.WriteActive(context.Background(), document, activeCurrent(latest)); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected permissive immutable snapshot rejection, got %v", err)
	}
	if _, err := os.Lstat(store.CurrentPath()); !os.IsNotExist(err) {
		t.Fatalf("permissive snapshot published a pointer: %v", err)
	}
}

func TestSnapshotStoreCanonicalWriterRejectsLegacyArtifact(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	latest := appendActiveHistory(t, log, "run-1")
	var document map[string]any
	if err := json.Unmarshal(runSnapshot(t, "running", nil), &document); err != nil {
		t.Fatal(err)
	}
	document["artifactBackend"] = "filesystem"
	document["artifacts"] = []any{"legacy.md"}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.WriteActive(context.Background(), data, activeCurrent(latest)); !errors.Is(err, contracts.ErrInvalid) {
		t.Fatalf("expected canonical contract error, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "runs")); !os.IsNotExist(err) {
		t.Fatalf("legacy artifact mutated snapshots: %v", err)
	}
}

func TestSnapshotStoreRejectsEventReferenceMissingFromSnapshot(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	appendActiveHistory(t, log, "run-1")
	appendSnapshotEvent(t, log, []byte(`{"schemaVersion":"1","eventId":"event-2","runId":"run-1","at":"2026-07-21T12:01:00Z","type":"task.started","taskId":"task-2","phase":"apply","agent":"forge"}`))

	if err := store.WriteActive(context.Background(), runSnapshot(t, "running", nil), activeCurrent("event-2")); !errors.Is(err, ErrInconsistent) {
		t.Fatalf("expected missing-reference inconsistency, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "runs")); !os.IsNotExist(err) {
		t.Fatalf("inconsistent event created runs storage: %v", err)
	}
}

func TestSnapshotStoreRejectsCompletedCancellationForRunningTask(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	appendActiveHistory(t, log, "run-1")
	appendSnapshotEvent(t, log, []byte(`{"schemaVersion":"1","eventId":"event-cancel-request","runId":"run-1","at":"2026-07-21T12:01:00Z","type":"cancellation.requested","cancellationId":"cancel-task-1"}`))
	appendSnapshotEvent(t, log, []byte(`{"schemaVersion":"1","eventId":"event-cancel-complete","runId":"run-1","at":"2026-07-21T12:01:10Z","type":"cancellation.completed","cancellationId":"cancel-task-1"}`))
	var document map[string]any
	if err := json.Unmarshal(runSnapshot(t, "running", nil), &document); err != nil {
		t.Fatal(err)
	}
	document["tasks"].([]any)[0].(map[string]any)["cancellationId"] = "cancel-task-1"
	document["cancellations"] = []any{map[string]any{
		"kind": "execution.cancellation", "schemaVersion": "1", "cancellationId": "cancel-task-1", "targetKind": "task", "targetId": "task-1",
		"status": "completed", "requestedAt": "2026-07-21T12:01:00Z", "completedAt": "2026-07-21T12:01:10Z",
	}}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.WriteActive(context.Background(), data, activeCurrent("event-cancel-complete")); !errors.Is(err, ErrInconsistent) {
		t.Fatalf("expected task cancellation inconsistency, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "runs")); !os.IsNotExist(err) {
		t.Fatalf("inconsistent cancellation mutated snapshots: %v", err)
	}
}

func TestSnapshotStoreRecoveryStopsOnProjectionMismatch(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	latest := appendActiveHistory(t, log, "run-1")
	if err := store.WriteActive(context.Background(), runSnapshot(t, "running", nil), activeCurrent(latest)); err != nil {
		t.Fatal(err)
	}
	current, present, err := ReadCurrent(context.Background(), store.CurrentPath())
	if err != nil || !present {
		t.Fatalf("read current: present=%v err=%v", present, err)
	}
	current.TaskID = "task-2"
	data, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.CurrentPath(), data, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Recover(context.Background()); !errors.Is(err, ErrInconsistent) {
		t.Fatalf("expected fail-closed recovery, got %v", err)
	}
}

func TestSnapshotStoreFinalizeAndRecoverTerminalRun(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	latest := appendActiveHistory(t, log, "run-1")
	if err := store.WriteActive(context.Background(), runSnapshot(t, "running", nil), activeCurrent(latest)); err != nil {
		t.Fatal(err)
	}
	appendSnapshotEvent(t, log, []byte(`{"schemaVersion":"1","eventId":"event-task-completed","runId":"run-1","at":"2026-07-21T12:01:30Z","type":"task.completed","taskId":"task-1","resultId":"result-1"}`))
	artifact := canonicalArtifact()
	completed := map[string]any{
		"schemaVersion": "1", "eventId": "event-2", "runId": "run-1", "at": "2026-07-21T12:02:00Z", "type": "run.completed", "artifact": artifact,
	}
	eventData, err := json.Marshal(completed)
	if err != nil {
		t.Fatal(err)
	}
	appendSnapshotEvent(t, log, eventData)

	if err := store.Finalize(context.Background(), runSnapshot(t, "completed", []any{artifact})); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(store.CurrentPath()); !os.IsNotExist(err) {
		t.Fatalf("terminal current pointer still exists: %v", err)
	}
	recovered, err := store.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != "completed" || recovered.CurrentPresent || len(recovered.Events) != 8 {
		t.Fatalf("unexpected terminal recovery: %+v", recovered)
	}
}

func TestSnapshotStoreRecoveryCompletesCrashedTerminalPointerRemoval(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	latest := appendActiveHistory(t, log, "run-1")
	if err := store.WriteActive(context.Background(), runSnapshot(t, "running", nil), activeCurrent(latest)); err != nil {
		t.Fatal(err)
	}
	appendSnapshotEvent(t, log, []byte(`{"schemaVersion":"1","eventId":"event-task-completed","runId":"run-1","at":"2026-07-21T12:01:30Z","type":"task.completed","taskId":"task-1","resultId":"result-1"}`))
	artifact := canonicalArtifact()
	completed := map[string]any{
		"schemaVersion": "1", "eventId": "event-2", "runId": "run-1", "at": "2026-07-21T12:02:00Z", "type": "run.completed", "artifact": artifact,
	}
	eventData, err := json.Marshal(completed)
	if err != nil {
		t.Fatal(err)
	}
	appendSnapshotEvent(t, log, eventData)
	crashErr := errors.New("simulated crash before pointer removal")
	store.removeFile = func(string) error { return crashErr }

	if err := store.Finalize(context.Background(), runSnapshot(t, "completed", []any{artifact})); !errors.Is(err, crashErr) {
		t.Fatalf("expected simulated crash, got %v", err)
	}
	if _, err := os.Stat(store.CurrentPath()); err != nil {
		t.Fatalf("crashed finalization removed pointer: %v", err)
	}
	if _, err := os.Stat(store.RunPath()); err != nil {
		t.Fatalf("crashed finalization did not stage terminal snapshot: %v", err)
	}

	restarted, err := NewSnapshotStore(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := restarted.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != "completed" || recovered.CurrentPresent {
		t.Fatalf("unexpected recovered terminal state: %+v", recovered)
	}
	if _, err := os.Lstat(store.CurrentPath()); !os.IsNotExist(err) {
		t.Fatalf("recovery did not commit pointer removal: %v", err)
	}
}

func TestSnapshotStoreFinalizeCancelledRunRequiresRunCancellation(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	latest := appendActiveHistory(t, log, "run-1")
	if err := store.WriteActive(context.Background(), runSnapshot(t, "running", nil), activeCurrent(latest)); err != nil {
		t.Fatal(err)
	}
	appendSnapshotEvent(t, log, []byte(`{"schemaVersion":"1","eventId":"event-cancellation-requested","runId":"run-1","at":"2026-07-21T12:01:30Z","type":"cancellation.requested","cancellationId":"cancellation-1"}`))
	appendSnapshotEvent(t, log, []byte(`{"schemaVersion":"1","eventId":"event-cancelled","runId":"run-1","at":"2026-07-21T12:02:00Z","type":"cancellation.completed","cancellationId":"cancellation-1"}`))

	if err := store.Finalize(context.Background(), cancelledRunSnapshot(t)); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != "cancelled" || recovered.CurrentPresent || len(recovered.Events) != 8 {
		t.Fatalf("unexpected cancelled recovery: %+v", recovered)
	}
}

func TestSnapshotStoreRejectsSnapshotSymlink(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	latest := appendActiveHistory(t, log, "run-1")
	if err := os.Mkdir(filepath.Join(root, "runs"), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target.json")
	if err := os.WriteFile(target, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	document := runSnapshot(t, "running", nil)
	activePath := filepath.Join(root, filepath.FromSlash(activeRunFile("run-1", mustReadableJSON(t, document))))
	if err := os.Symlink(target, activePath); err != nil {
		t.Fatal(err)
	}

	if err := store.WriteActive(context.Background(), document, activeCurrent(latest)); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected symlink corruption, got %v", err)
	}
	if string(mustRead(t, target)) != "unchanged" {
		t.Fatal("snapshot symlink target changed")
	}
}

func TestSnapshotStoreRejectsCurrentPointerSymlinkBeforeRunWrite(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	latest := appendActiveHistory(t, log, "run-1")
	target := filepath.Join(root, "target-current.json")
	if err := os.WriteFile(target, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, store.CurrentPath()); err != nil {
		t.Fatal(err)
	}

	if err := store.WriteActive(context.Background(), runSnapshot(t, "running", nil), activeCurrent(latest)); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected current symlink corruption, got %v", err)
	}
	if string(mustRead(t, target)) != "unchanged" {
		t.Fatal("current symlink target changed")
	}
	if _, err := os.Lstat(store.RunPath()); !os.IsNotExist(err) {
		t.Fatalf("current symlink allowed a run snapshot write: %v", err)
	}
}

func TestSnapshotStoreRecoveryRejectsSymlinkedRunsDirectory(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	latest := appendActiveHistory(t, log, "run-1")
	if err := store.WriteActive(context.Background(), runSnapshot(t, "running", nil), activeCurrent(latest)); err != nil {
		t.Fatal(err)
	}
	runs := filepath.Join(root, "runs")
	backup := filepath.Join(root, "runs-backup")
	if err := os.Rename(runs, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(backup, runs); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Recover(context.Background()); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected symlinked runs corruption, got %v", err)
	}
}

func TestSnapshotStoreDoesNotReplaceAnotherCurrentRun(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	latest := appendActiveHistory(t, log, "run-1")
	other := activeCurrent("event-other")
	other.ID = "run-2"
	data, err := prepareCurrentSnapshot(context.Background(), other)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.CurrentPath(), data, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := store.WriteActive(context.Background(), runSnapshot(t, "running", nil), activeCurrent(latest)); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ownership conflict, got %v", err)
	}
	current, present, err := ReadCurrent(context.Background(), store.CurrentPath())
	if err != nil || !present || current.ID != "run-2" {
		t.Fatalf("other current pointer changed: %+v present=%v err=%v", current, present, err)
	}
}

func TestSnapshotStoreContinuationRejectsStaleActiveTask(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	latest := appendActiveHistory(t, log, "run-1")
	original := activeCurrent(latest)
	if err := store.WriteActive(context.Background(), runSnapshot(t, "running", nil), original); err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(runSnapshot(t, "running", nil), &document); err != nil {
		t.Fatal(err)
	}
	document["tasks"] = append(document["tasks"].([]any), map[string]any{
		"taskId": "task-2", "phase": "apply", "agentId": "forge", "status": "pending", "contextPacketId": "packet-2",
	})
	updatedDocument, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	updated := original
	updated.TaskID = "task-2"
	if err := store.WriteActiveContinuation(context.Background(), updatedDocument, updated, original); err != nil {
		t.Fatalf("publish continuation: %v", err)
	}
	if err := store.WriteActiveContinuation(context.Background(), updatedDocument, updated, original); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale continuation was not rejected: %v", err)
	}
}

func TestSnapshotStoreSerializesCurrentOwnershipAcrossRuns(t *testing.T) {
	root := t.TempDir()
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for _, runID := range []string{"run-1", "run-2"} {
		runID := runID
		log, err := NewEventLog(root, runID)
		if err != nil {
			t.Fatal(err)
		}
		eventID := appendActiveHistory(t, log, runID)
		store, err := NewSnapshotStore(root, runID)
		if err != nil {
			t.Fatal(err)
		}
		current := activeCurrent(eventID)
		current.ID = runID
		document := runSnapshotForRun(t, runID)
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			results <- store.WriteActive(context.Background(), document, current)
		}()
	}
	close(start)
	group.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatalf("unexpected concurrent write error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestSnapshotStoreRecoveryAcceptsOptionalStorageReferences(t *testing.T) {
	root := t.TempDir()
	log, store := newSnapshotTestStore(t, root)
	latest := appendActiveHistory(t, log, "run-1")
	if err := store.WriteActive(context.Background(), runSnapshot(t, "running", nil), activeCurrent(latest)); err != nil {
		t.Fatal(err)
	}
	var current map[string]any
	if err := json.Unmarshal(mustRead(t, store.CurrentPath()), &current); err != nil {
		t.Fatal(err)
	}
	activePath := filepath.Join(root, filepath.FromSlash(current["runFile"].(string)))
	if err := os.WriteFile(store.RunPath(), mustRead(t, activePath), 0o600); err != nil {
		t.Fatal(err)
	}
	delete(current, "runFile")
	delete(current, "logFile")
	data, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.CurrentPath(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Recover(context.Background()); err != nil {
		t.Fatalf("optional storage references should recover: %v", err)
	}
}

func newSnapshotTestStore(t *testing.T, root string) (*EventLog, *SnapshotStore) {
	t.Helper()
	log, err := NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewSnapshotStore(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	return log, store
}

func appendSnapshotEvent(t *testing.T, log *EventLog, data []byte) {
	t.Helper()
	if _, err := log.Append(context.Background(), data); err != nil {
		t.Fatal(err)
	}
}

func appendActiveHistory(t *testing.T, log *EventLog, runID string) string {
	t.Helper()
	events := [][]byte{
		validEvent("event-run", runID, "started"),
		[]byte(fmt.Sprintf(`{"schemaVersion":"1","eventId":"event-selection","runId":%q,"at":"2026-07-21T12:00:10Z","type":"orchestrator.selected","selectionId":"selection-1"}`, runID)),
		[]byte(fmt.Sprintf(`{"schemaVersion":"1","eventId":"event-routing","runId":%q,"at":"2026-07-21T12:00:20Z","type":"routing.decided","decisionId":"decision-1"}`, runID)),
		[]byte(fmt.Sprintf(`{"schemaVersion":"1","eventId":"event-preflight","runId":%q,"at":"2026-07-21T12:00:30Z","type":"preflight.completed","preflightId":"preflight-1"}`, runID)),
		[]byte(fmt.Sprintf(`{"schemaVersion":"1","eventId":"event-phase","runId":%q,"at":"2026-07-21T12:00:40Z","type":"phase.started","phase":"apply","agent":"forge","data":{}}`, runID)),
		[]byte(fmt.Sprintf(`{"schemaVersion":"1","eventId":"event-task","runId":%q,"at":"2026-07-21T12:00:50Z","type":"task.started","taskId":"task-1","phase":"apply","agent":"forge"}`, runID)),
	}
	for _, event := range events {
		appendSnapshotEvent(t, log, event)
	}
	return "event-task"
}

func activeCurrent(lastEventID string) CurrentRun {
	return CurrentRun{
		SchemaVersion: "1", ID: "run-1", Project: "vgxness", Goal: "test snapshots", Status: "running", Phase: "apply",
		SelectionID: "selection-1", DecisionID: "decision-1", PreflightID: "preflight-1", TaskID: "task-1", LastEventID: lastEventID,
		ArtifactIDs: []string{}, StorageMode: "project-local", StartedAt: "2026-07-21T12:00:00Z", UpdatedAt: "2026-07-21T12:01:00Z",
	}
}

func runSnapshot(t *testing.T, status string, artifacts []any) []byte {
	t.Helper()
	if artifacts == nil {
		artifacts = []any{}
	}
	phaseStatus, taskStatus, updatedAt, backend := "running", "running", "2026-07-21T12:01:00Z", "none"
	if status == "completed" {
		phaseStatus, taskStatus, updatedAt, backend = "completed", "completed", "2026-07-21T12:02:00Z", "filesystem"
	}
	document := map[string]any{
		"schemaVersion": "1", "id": "run-1", "project": "vgxness", "goal": "test snapshots", "status": status,
		"storageMode": "project-local", "artifactBackend": backend,
		"orchestratorSelection": map[string]any{
			"kind": "orchestrator.selection", "schemaVersion": "1", "selectionId": "selection-1",
			"needs":      []any{map[string]any{"capability": "workflow", "version": "1"}},
			"candidates": []any{map[string]any{"provider": "opencode", "capabilities": []any{map[string]any{"capability": "workflow", "version": "1", "constraints": map[string]any{}}}, "eligible": true, "reasons": []any{}}},
			"status":     "selected", "selectedProvider": "opencode", "policyVersion": "1", "rationale": "eligible", "decidedAt": "2026-07-21T12:00:00Z",
		},
		"routingDecision": map[string]any{
			"kind": "routing.decision", "schemaVersion": "1", "decisionId": "decision-1", "inputRefs": []any{}, "difficulty": "medium", "risk": "medium",
			"candidates": []any{"forge"}, "selectedAgent": "forge", "route": "apply", "rationale": "implementation", "policyVersion": "1", "sdd": "skipped", "decidedAt": "2026-07-21T12:00:00Z",
		},
		"sddPreflight": map[string]any{
			"kind": "sdd.preflight", "schemaVersion": "1", "preflightId": "preflight-1", "mode": "off", "backend": "none", "status": "not-run", "artifactAccess": false, "checkedAt": "2026-07-21T12:00:00Z",
		},
		"createdAt": "2026-07-21T12:00:00Z", "updatedAt": updatedAt,
		"phases":    []any{map[string]any{"name": "apply", "agent": "forge", "status": phaseStatus, "startedAt": "2026-07-21T12:00:00Z", "artifacts": []any{}, "memoryWrites": []any{}, "validations": []any{}}},
		"artifacts": artifacts, "memoryWrites": []any{}, "decisions": []any{},
		"tasks":         []any{map[string]any{"taskId": "task-1", "phase": "apply", "agentId": "forge", "status": taskStatus, "contextPacketId": "packet-1"}},
		"cancellations": []any{}, "results": []any{}, "capsules": []any{}, "validations": []any{},
	}
	if status == "completed" {
		document["tasks"].([]any)[0].(map[string]any)["resultId"] = "result-1"
		document["results"] = []any{map[string]any{
			"kind": "agent.result", "schemaVersion": "1", "resultId": "result-1", "taskId": "task-1", "agentId": "forge", "status": "success",
			"summary": "completed", "artifacts": []any{}, "nextRecommended": "none", "risks": []any{}, "errors": []any{},
		}}
	}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func runSnapshotForRun(t *testing.T, runID string) []byte {
	t.Helper()
	return bytes.ReplaceAll(runSnapshot(t, "running", nil), []byte("run-1"), []byte(runID))
}

func activeRunSnapshotAt(t *testing.T, updatedAt string) []byte {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(runSnapshot(t, "running", nil), &document); err != nil {
		t.Fatal(err)
	}
	document["updatedAt"] = updatedAt
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func cancelledRunSnapshot(t *testing.T) []byte {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(runSnapshot(t, "running", nil), &document); err != nil {
		t.Fatal(err)
	}
	document["status"] = "cancelled"
	document["updatedAt"] = "2026-07-21T12:02:00Z"
	document["phases"].([]any)[0].(map[string]any)["status"] = "cancelled"
	document["tasks"].([]any)[0].(map[string]any)["status"] = "cancelled"
	document["cancellations"] = []any{map[string]any{
		"kind": "execution.cancellation", "schemaVersion": "1", "cancellationId": "cancellation-1", "targetKind": "run", "targetId": "run-1", "status": "completed", "requestedAt": "2026-07-21T12:01:30Z", "completedAt": "2026-07-21T12:02:00Z",
	}}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func canonicalArtifact() map[string]any {
	return map[string]any{
		"kind": "artifact.reference", "schemaVersion": "1", "provider": "filesystem", "id": "artifact-1", "artifactType": "summary", "path": "artifacts/summary.md",
		"provenance": map[string]any{"producer": "vgxness", "createdAt": "2026-07-21T12:02:00Z", "runId": "run-1"},
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustReadableJSON(t *testing.T, document []byte) []byte {
	t.Helper()
	data, err := readableJSON(document)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
