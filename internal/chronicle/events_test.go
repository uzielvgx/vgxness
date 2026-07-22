package chronicle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/contracts"
)

func TestEventLogAppendAndRead(t *testing.T) {
	root := t.TempDir()
	log, err := NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	document := validEvent("event-1", "run-1", "hello\nworld")

	appended, err := log.Append(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	if appended.ID != "event-1" || appended.RunID != "run-1" || appended.Type != "run.started" {
		t.Fatalf("unexpected appended event: %#v", appended)
	}

	events, err := log.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != appended.ID {
		t.Fatalf("unexpected events: %#v", events)
	}
	stored, err := os.ReadFile(log.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) == 0 || stored[len(stored)-1] != '\n' {
		t.Fatalf("event log is not newline terminated: %q", stored)
	}
	if string(stored) == string(document) {
		t.Fatal("event was not compacted into one JSONL record")
	}
}

func TestEventLogInvalidEventDoesNotMutateStorage(t *testing.T) {
	root := t.TempDir()
	log, err := NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	invalid := []byte(`{"schemaVersion":"1","eventId":"event-1","runId":"run-1","at":"2026-07-21T12:00:00Z","type":"run.started"}`)

	_, err = log.Append(context.Background(), invalid)
	if !errors.Is(err, contracts.ErrInvalid) {
		t.Fatalf("expected contract error, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "logs")); !os.IsNotExist(err) {
		t.Fatalf("invalid event mutated storage: %v", err)
	}
}

func TestEventLogCanonicalWriterRejectsLegacyArtifact(t *testing.T) {
	root := t.TempDir()
	log, err := NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"schemaVersion":"1","eventId":"event-1","runId":"run-1","at":"2026-07-21T12:00:00Z","type":"run.completed","artifact":"summary.md"}`)

	if _, err := log.Append(context.Background(), legacy); !errors.Is(err, contracts.ErrInvalid) {
		t.Fatalf("expected canonical contract error, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "logs")); !os.IsNotExist(err) {
		t.Fatalf("legacy artifact mutated storage: %v", err)
	}
}

func TestEventLogRejectsRunMismatchBeforeMutation(t *testing.T) {
	root := t.TempDir()
	log, err := NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}

	_, err = log.Append(context.Background(), validEvent("event-1", "run-2", "mismatch"))
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "logs")); !os.IsNotExist(err) {
		t.Fatalf("run mismatch mutated storage: %v", err)
	}
}

func TestEventLogRejectsDuplicateWithoutMutation(t *testing.T) {
	root := t.TempDir()
	log, err := NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Append(context.Background(), validEvent("event-1", "run-1", "first")); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(log.Path())
	if err != nil {
		t.Fatal(err)
	}

	_, err = log.Append(context.Background(), validEvent("event-1", "run-1", "duplicate"))
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected duplicate conflict, got %v", err)
	}
	after, err := os.ReadFile(log.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("duplicate event changed the log")
	}
}

func TestEventLogReportsIncompleteFinalRecord(t *testing.T) {
	root := t.TempDir()
	log, err := NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	partial := validEvent("event-1", "run-1", "partial")
	if err := os.WriteFile(log.Path(), partial, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := log.Read(context.Background()); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected corrupt read, got %v", err)
	}
	before, err := os.ReadFile(log.Path())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Append(context.Background(), validEvent("event-2", "run-1", "next")); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected corrupt append, got %v", err)
	}
	after, err := os.ReadFile(log.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("append changed a partial log")
	}
}

func TestEventLogReportsInvalidCompleteRecord(t *testing.T) {
	root := t.TempDir()
	log, err := NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(log.Path(), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := log.Read(context.Background()); !errors.Is(err, ErrCorrupt) || !errors.Is(err, contracts.ErrInvalid) {
		t.Fatalf("expected corrupt contract error, got %v", err)
	}
}

func TestEventLogRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	log, err := NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, log.Path()); err != nil {
		t.Fatal(err)
	}

	if _, err := log.Append(context.Background(), validEvent("event-1", "run-1", "blocked")); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected corrupt symlink error, got %v", err)
	}
	contents, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "unchanged" {
		t.Fatalf("symlink target changed: %q", contents)
	}
}

func TestEventLogReadRejectsSymlinkedLogsDirectory(t *testing.T) {
	root := t.TempDir()
	log, err := NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "run-1.jsonl"), append(validEvent("event-1", "run-1", "external"), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, "logs")); err != nil {
		t.Fatal(err)
	}

	if _, err := log.Read(context.Background()); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected corrupt directory error, got %v", err)
	}
}

func TestEventLogConcurrentAppends(t *testing.T) {
	root := t.TempDir()
	const count = 24
	start := make(chan struct{})
	errorsCh := make(chan error, count)
	var group sync.WaitGroup
	for index := 0; index < count; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			log, err := NewEventLog(root, "run-1")
			if err != nil {
				errorsCh <- err
				return
			}
			<-start
			_, err = log.Append(context.Background(), validEvent(fmt.Sprintf("event-%d", index), "run-1", "concurrent"))
			errorsCh <- err
		}(index)
	}
	close(start)
	group.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	log, err := NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	events, err := log.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != count {
		t.Fatalf("got %d events, want %d", len(events), count)
	}
	seen := make(map[string]bool, count)
	for _, event := range events {
		seen[event.ID] = true
	}
	if len(seen) != count {
		t.Fatalf("got %d unique IDs, want %d", len(seen), count)
	}
}

func TestEventLogCancelledContextDoesNotMutateStorage(t *testing.T) {
	root := t.TempDir()
	log, err := NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := log.Append(ctx, validEvent("event-1", "run-1", "cancelled")); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "logs")); !os.IsNotExist(err) {
		t.Fatalf("cancelled append mutated storage: %v", err)
	}
}

func TestEventLogLockWaitHonorsContext(t *testing.T) {
	root := t.TempDir()
	log, err := NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	locked, err := os.OpenFile(log.Path(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer locked.Close()
	if err := lockFile(context.Background(), locked, lockExclusive); err != nil {
		t.Fatal(err)
	}
	defer unlockFile(locked)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if _, err := log.Append(ctx, validEvent("event-1", "run-1", "locked")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected lock timeout, got %v", err)
	}
	info, err := locked.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("lock timeout changed file size to %d", info.Size())
	}
}

func TestEventLogReadMissingIsEmptyAndNonMutating(t *testing.T) {
	root := t.TempDir()
	log, err := NewEventLog(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	events, err := log.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if events != nil {
		t.Fatalf("expected nil events, got %#v", events)
	}
	if _, err := os.Lstat(filepath.Join(root, "logs")); !os.IsNotExist(err) {
		t.Fatalf("read created storage: %v", err)
	}
}

func TestNewEventLogRejectsUnsafeRunIDs(t *testing.T) {
	root := t.TempDir()
	for _, runID := range []string{"", "../run", "run/child", strings.Repeat("a", 241)} {
		if _, err := NewEventLog(root, runID); err == nil {
			t.Fatalf("expected %q to be rejected", runID)
		}
	}
}

func validEvent(eventID, runID, message string) []byte {
	return []byte(fmt.Sprintf(`{
  "schemaVersion": "1",
  "eventId": %q,
  "runId": %q,
  "at": %q,
  "type": "run.started",
  "message": %q,
  "data": {}
}`, eventID, runID, time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC).Format(time.RFC3339), message))
}
