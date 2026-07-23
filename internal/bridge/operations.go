package bridge

import "context"

type OperationsRuntime interface {
	Runtime
	OperationalInventory(context.Context, string, OperationalInventoryRequest) (OperationalInventory, error)
	PruneOperations(context.Context, string, OperationalPruneRequest) (OperationalPruneResult, error)
}

type OperationalInventoryRequest struct {
	StorageRoot  string `json:"storageRoot,omitempty"`
	ProjectLocal bool   `json:"projectLocal,omitempty"`
}

type OperationalInventory struct {
	Workspace      string                 `json:"workspace"`
	StorageRoot    string                 `json:"storageRoot"`
	Health         string                 `json:"health"`
	CurrentRun     *CurrentRunSummary     `json:"currentRun,omitempty"`
	Orchestrations []OrchestrationSummary `json:"orchestrations"`
	NativeTickets  []NativeTicketSummary  `json:"nativeTickets"`
	Findings       []OperationalFinding   `json:"findings"`
}

type CurrentRunSummary struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Phase     string `json:"phase"`
	UpdatedAt string `json:"updatedAt"`
}

type OrchestrationSummary struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	CurrentWave int    `json:"currentWave"`
	Tasks       int    `json:"tasks"`
	Waves       int    `json:"waves"`
	UpdatedAt   string `json:"updatedAt"`
}

type NativeTicketSummary struct {
	ID         string `json:"id"`
	State      string `json:"state"`
	TaskID     string `json:"taskId"`
	RunID      string `json:"runId"`
	Deadline   string `json:"deadline"`
	ModifiedAt string `json:"modifiedAt"`
}

type OperationalFinding struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Subject  string `json:"subject"`
	Message  string `json:"message"`
}

type OperationalPruneRequest struct {
	OlderThanSeconds int64  `json:"olderThanSeconds"`
	Apply            bool   `json:"apply"`
	StorageRoot      string `json:"storageRoot,omitempty"`
	ProjectLocal     bool   `json:"projectLocal,omitempty"`
}

type OperationalPruneCandidate struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
}

type OperationalPruneResult struct {
	Workspace   string                      `json:"workspace"`
	StorageRoot string                      `json:"storageRoot"`
	Cutoff      string                      `json:"cutoff"`
	Applied     bool                        `json:"applied"`
	Candidates  []OperationalPruneCandidate `json:"candidates"`
	Removed     []OperationalPruneCandidate `json:"removed"`
}
