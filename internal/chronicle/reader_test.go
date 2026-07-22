package chronicle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/contracts"
	"github.com/vgxness/vgxness/internal/testutil"
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

func currentRunJSON(t *testing.T, changes map[string]any) []byte {
	t.Helper()
	run := map[string]any{
		"schemaVersion": "1", "id": "run-1", "project": "vgxness", "goal": "test",
		"status": "running", "phase": "apply", "selectionId": "selection-1", "decisionId": "decision-1",
		"preflightId": "preflight-1", "taskId": "task-1", "lastEventId": "event-1", "artifactIds": []string{},
		"storageMode": "project-local", "startedAt": "2026-07-20T12:00:00Z", "updatedAt": "2026-07-20T12:01:00Z",
	}
	for key, value := range changes {
		run[key] = value
	}
	data, err := json.Marshal(run)
	testutil.NoError(t, err)
	return data
}

func TestChronicle_AcceptsValidRegularFileAndStorageModes(t *testing.T) {
	for _, mode := range []string{"project-local", "user-global"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "current-run.json")
			testutil.NoError(t, os.WriteFile(path, currentRunJSON(t, map[string]any{"storageMode": mode}), 0o600))
			run, present, err := ReadCurrent(context.Background(), path)
			testutil.Require(t, err == nil && present && run.StorageMode == mode, "run=%+v present=%v err=%v", run, present, err)
		})
	}
}

func TestChronicle_RejectsUnsafeOrInvalidCurrentRun(t *testing.T) {
	t.Run("child symlink", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target.json")
		testutil.NoError(t, os.WriteFile(target, currentRunJSON(t, nil), 0o600))
		path := filepath.Join(dir, "current-run.json")
		testutil.NoError(t, os.Symlink(target, path))
		_, _, err := ReadCurrent(context.Background(), path)
		testutil.Require(t, errors.Is(err, ErrCorrupt), "expected symlink corruption, got %v", err)
	})
	for name, changes := range map[string]map[string]any{
		"unknown field": {"unexpected": true}, "invalid status": {"status": "done"}, "invalid storage": {"storageMode": "global"},
		"empty optional path": {"runFile": ""}, "empty optional id": {"resultId": ""},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "current-run.json")
			testutil.NoError(t, os.WriteFile(path, currentRunJSON(t, changes), 0o600))
			_, _, err := ReadCurrent(context.Background(), path)
			testutil.Require(t, errors.Is(err, ErrCorrupt), "expected corruption, got %v", err)
		})
	}
}

func TestChronicle_RejectsInvalidTemporalAndArtifactMetadata(t *testing.T) {
	for name, changes := range map[string]map[string]any{
		"invalid startedAt":     {"startedAt": "yesterday"},
		"invalid updatedAt":     {"updatedAt": "later"},
		"decreasing timestamps": {"updatedAt": "2026-07-20T11:59:59Z"},
		"blank artifact":        {"artifactIds": []string{""}},
		"duplicate artifact":    {"artifactIds": []string{"artifact-1", "artifact-1"}},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "current-run.json")
			testutil.NoError(t, os.WriteFile(path, currentRunJSON(t, changes), 0o600))
			_, _, err := ReadCurrent(context.Background(), path)
			testutil.Require(t, errors.Is(err, ErrCorrupt), "expected corruption, got %v", err)
		})
	}
}

func TestChronicle_ReportsSchemaFailureWithoutLosingCorruptionIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "current-run.json")
	testutil.NoError(t, os.WriteFile(path, currentRunJSON(t, map[string]any{"startedAt": "yesterday"}), 0o600))
	_, _, err := ReadCurrent(context.Background(), path)
	var failure *contracts.ContractError
	testutil.Require(t, errors.Is(err, ErrCorrupt) && errors.Is(err, contracts.ErrInvalid) && errors.As(err, &failure), "expected joined contract corruption, got %v", err)
	testutil.Require(t, failure.SchemaURI == contracts.CurrentRunSchemaURI && failure.Pointer == "/startedAt" && !failure.Recoverable, "unexpected failure: %+v", failure)
}

func TestChronicle_RejectsOversizedCurrentRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "current-run.json")
	testutil.NoError(t, os.WriteFile(path, bytes.Repeat([]byte("x"), int(maxCurrentRunBytes)+1), 0o600))
	_, _, err := ReadCurrent(context.Background(), path)
	testutil.Require(t, errors.Is(err, ErrCorrupt), "expected oversized corruption, got %v", err)
}
