package chronicle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/vgxness/vgxness/internal/contracts"
)

var ErrCorrupt = errors.New("corrupt")

const maxCurrentRunBytes int64 = 1 << 20

type CurrentRun struct {
	SchemaVersion  string   `json:"schemaVersion"`
	ID             string   `json:"id"`
	Project        string   `json:"project"`
	Goal           string   `json:"goal"`
	Status         string   `json:"status"`
	Phase          string   `json:"phase"`
	SelectionID    string   `json:"selectionId"`
	DecisionID     string   `json:"decisionId"`
	PreflightID    string   `json:"preflightId"`
	TaskID         string   `json:"taskId"`
	CancellationID string   `json:"cancellationId,omitempty"`
	ResultID       string   `json:"resultId,omitempty"`
	CapsuleID      string   `json:"capsuleId,omitempty"`
	LastEventID    string   `json:"lastEventId"`
	ArtifactIDs    []string `json:"artifactIds"`
	StorageMode    string   `json:"storageMode"`
	StartedAt      string   `json:"startedAt"`
	UpdatedAt      string   `json:"updatedAt"`
	RunFile        string   `json:"runFile,omitempty"`
	LogFile        string   `json:"logFile,omitempty"`
}

func ReadCurrent(ctx context.Context, path string) (CurrentRun, bool, error) {
	if err := ctx.Err(); err != nil {
		return CurrentRun{}, false, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return CurrentRun{}, false, nil
	}
	if err != nil {
		return CurrentRun{}, false, fmt.Errorf("read Chronicle: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return CurrentRun{}, false, fmt.Errorf("%w: current run must be a regular file", ErrCorrupt)
	}
	f, err := os.Open(path)
	if err != nil {
		return CurrentRun{}, false, fmt.Errorf("read Chronicle: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCurrentRunBytes+1))
	if err != nil {
		return CurrentRun{}, false, fmt.Errorf("read Chronicle: %w", err)
	}
	if int64(len(data)) > maxCurrentRunBytes {
		return CurrentRun{}, false, fmt.Errorf("%w: current run exceeds size limit", ErrCorrupt)
	}
	if err := contracts.Validate(ctx, contracts.CurrentRunSchemaURI, data, false); err != nil {
		return CurrentRun{}, false, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	var run CurrentRun
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var optional struct {
		CancellationID *string `json:"cancellationId"`
		ResultID       *string `json:"resultId"`
		CapsuleID      *string `json:"capsuleId"`
		RunFile        *string `json:"runFile"`
		LogFile        *string `json:"logFile"`
	}
	optionalInvalid := json.Unmarshal(data, &optional) != nil
	for _, value := range []*string{optional.CancellationID, optional.ResultID, optional.CapsuleID, optional.RunFile, optional.LogFile} {
		optionalInvalid = optionalInvalid || value != nil && *value == ""
	}
	if err := decoder.Decode(&run); err != nil || decoder.Decode(&struct{}{}) != io.EOF || optionalInvalid || !valid(run) {
		return CurrentRun{}, false, fmt.Errorf("%w: malformed current run", ErrCorrupt)
	}
	return run, true, nil
}

func valid(run CurrentRun) bool {
	switch run.Status {
	case "running", "paused", "blocked", "recovering":
	default:
		return false
	}
	if run.StorageMode != "project-local" && run.StorageMode != "user-global" {
		return false
	}
	started, startErr := time.Parse(time.RFC3339, run.StartedAt)
	updated, updateErr := time.Parse(time.RFC3339, run.UpdatedAt)
	if startErr != nil || updateErr != nil || updated.Before(started) {
		return false
	}
	seen := make(map[string]struct{}, len(run.ArtifactIDs))
	for _, id := range run.ArtifactIDs {
		if id == "" {
			return false
		}
		if _, duplicate := seen[id]; duplicate {
			return false
		}
		seen[id] = struct{}{}
	}
	return run.SchemaVersion == "1" && run.ID != "" && run.Project != "" && run.Goal != "" && run.Phase != "" && run.SelectionID != "" && run.DecisionID != "" && run.PreflightID != "" && run.TaskID != "" && run.LastEventID != "" && run.ArtifactIDs != nil
}
