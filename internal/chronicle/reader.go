package chronicle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

var ErrCorrupt = errors.New("corrupt")

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
	CancellationID string   `json:"cancellationId"`
	ResultID       string   `json:"resultId"`
	CapsuleID      string   `json:"capsuleId"`
	LastEventID    string   `json:"lastEventId"`
	ArtifactIDs    []string `json:"artifactIds"`
	StorageMode    string   `json:"storageMode"`
	StartedAt      string   `json:"startedAt"`
	UpdatedAt      string   `json:"updatedAt"`
	RunFile        string   `json:"runFile"`
	LogFile        string   `json:"logFile"`
}

func ReadCurrent(ctx context.Context, path string) (CurrentRun, bool, error) {
	if err := ctx.Err(); err != nil {
		return CurrentRun{}, false, err
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return CurrentRun{}, false, nil
	}
	if err != nil {
		return CurrentRun{}, false, fmt.Errorf("read Chronicle: %w", err)
	}
	defer f.Close()
	var run CurrentRun
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&run); err != nil || decoder.Decode(&struct{}{}) != io.EOF || !valid(run) {
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
	return run.SchemaVersion == "1" && run.ID != "" && run.Project != "" && run.Goal != "" && run.Phase != "" && run.SelectionID != "" && run.DecisionID != "" && run.PreflightID != "" && run.TaskID != "" && run.LastEventID != "" && run.ArtifactIDs != nil && run.StorageMode != "" && run.StartedAt != "" && run.UpdatedAt != ""
}
