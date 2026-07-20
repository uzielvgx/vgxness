package chronicle

import (
	"context"
	"errors"
	"github.com/vgxness/vgxness/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChronicle_RestartReadOnlyPresentOrAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "current-run.json")
	_, present, err := ReadCurrent(context.Background(), path)
	testutil.Require(t, err == nil && !present, "absence: %v %v", present, err)
	data := []byte(`{"schemaVersion":"1","id":"run-1","project":"vgxness","goal":"test","status":"running","phase":"apply","selectionId":"selection-1","decisionId":"decision-1","preflightId":"preflight-1","taskId":"task-1","lastEventId":"event-1","artifactIds":[],"storageMode":"project-local","startedAt":"2026-07-20T12:00:00Z","updatedAt":"2026-07-20T12:01:00Z"}`)
	testutil.NoError(t, os.WriteFile(path, data, 0o600))
	for range 2 {
		run, present, err := ReadCurrent(context.Background(), path)
		testutil.Require(t, err == nil && present && run.ID == "run-1", "read: %+v %v %v", run, present, err)
	}
	after, _ := os.ReadFile(path)
	testutil.Require(t, string(data) == string(after), "inspection mutated Chronicle")
}

func TestChronicle_RejectsMissingAndUnknownFields(t *testing.T) {
	for name, data := range map[string]string{
		"missing": `{"schemaVersion":"1","id":"run-1","status":"running","phase":"apply"}`,
		"unknown": `{"schemaVersion":"1","id":"run-1","project":"p","goal":"g","status":"running","phase":"apply","selectionId":"s","decisionId":"d","preflightId":"p","taskId":"t","lastEventId":"e","artifactIds":[],"storageMode":"project-local","startedAt":"2026-07-20T12:00:00Z","updatedAt":"2026-07-20T12:01:00Z","unknown":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "current-run.json")
			testutil.NoError(t, os.WriteFile(path, []byte(data), 0o600))
			_, _, err := ReadCurrent(context.Background(), path)
			testutil.Require(t, errors.Is(err, ErrCorrupt), "expected corrupt, got %v", err)
		})
	}
}

func TestChronicle_AcceptsOnlyCurrentStatuses(t *testing.T) {
	data := `{"schemaVersion":"1","id":"run-1","project":"p","goal":"g","status":"running","phase":"apply","selectionId":"s","decisionId":"d","preflightId":"p","taskId":"t","lastEventId":"e","artifactIds":[],"storageMode":"project-local","startedAt":"2026-07-20T12:00:00Z","updatedAt":"2026-07-20T12:01:00Z"}`
	for _, status := range []string{"running", "paused", "blocked", "recovering", "completed", "invalid"} {
		t.Run(status, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "current-run.json")
			testutil.NoError(t, os.WriteFile(path, []byte(strings.Replace(data, "running", status, 1)), 0o600))
			_, present, err := ReadCurrent(context.Background(), path)
			valid := status != "completed" && status != "invalid"
			testutil.Require(t, valid == (err == nil && present), "status %q: present=%v err=%v", status, present, err)
			if !valid {
				testutil.Require(t, errors.Is(err, ErrCorrupt), "expected corrupt, got %v", err)
			}
		})
	}
}
