package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	"unicode"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/chronicle"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
)

const continuityMemoryLimit = 3

const memoryCandidateConfidence = 0.8

var durableMemoryCandidateTypes = map[string]struct{}{
	"decision": {}, "architecture": {}, "preference": {}, "convention": {}, "discovery": {},
	"bugfix": {}, "constraint": {}, "learning": {}, "summary": {},
}

type MemoryRuntime interface {
	Save(context.Context, config.Options, memory.SaveRequest) (memory.MemoryResult, error)
	Search(context.Context, config.Options, memory.SearchRequest) ([]memory.MemoryResult, error)
	Get(context.Context, config.Options, memory.GetRequest) (memory.MemoryResult, error)
	ResolveProject(context.Context, config.Options, string) (string, error)
}

type taskMemoryState struct {
	project           string
	workspace         string
	retrievedMemories []memory.MemoryResult
}

type continuityState struct {
	mode              bridge.ContinuityMode
	runID             string
	project           string
	workspace         string
	store             *chronicle.SnapshotStore
	log               *chronicle.EventLog
	snapshot          runSnapshot
	expected          *chronicle.CurrentRun
	staged            chronicle.CurrentRun
	selectionID       string
	decisionID        string
	preflightID       string
	previousCapsule   json.RawMessage
	retrievedMemories []memory.MemoryResult
}

type continuityOutcome struct {
	capsuleID    string
	stateVersion int
	memoryRefs   []string
}

type executionIDs struct {
	selectionID string
	decisionID  string
	preflightID string
	packetID    string
	loopID      string
}

type runSnapshot struct {
	SchemaVersion         string              `json:"schemaVersion"`
	ID                    string              `json:"id"`
	Project               string              `json:"project"`
	Goal                  string              `json:"goal"`
	Status                string              `json:"status"`
	StorageMode           string              `json:"storageMode"`
	ArtifactBackend       string              `json:"artifactBackend"`
	OrchestratorSelection json.RawMessage     `json:"orchestratorSelection"`
	RoutingDecision       json.RawMessage     `json:"routingDecision"`
	SDDPreflight          json.RawMessage     `json:"sddPreflight"`
	CreatedAt             string              `json:"createdAt"`
	UpdatedAt             string              `json:"updatedAt"`
	Phases                []runPhase          `json:"phases"`
	Artifacts             []json.RawMessage   `json:"artifacts"`
	MemoryWrites          []runMemoryWrite    `json:"memoryWrites"`
	Decisions             []json.RawMessage   `json:"decisions"`
	Tasks                 []runTask           `json:"tasks"`
	Cancellations         []json.RawMessage   `json:"cancellations"`
	Results               []json.RawMessage   `json:"results"`
	Capsules              []continuityCapsule `json:"capsules"`
	Validations           []json.RawMessage   `json:"validations"`
}

func (service *Service) executionIdentities(state *continuityState) (executionIDs, error) {
	ids := executionIDs{}
	var err error
	if state != nil && (state.mode == bridge.ContinuityContinue || state.mode == bridge.ContinuityFinish) {
		ids.selectionID, ids.decisionID, ids.preflightID = state.selectionID, state.decisionID, state.preflightID
	} else {
		if ids.selectionID, err = service.newID("selection"); err != nil {
			return executionIDs{}, fmt.Errorf("%w: selection identity", bridge.ErrExecution)
		}
		if ids.decisionID, err = service.newID("decision"); err != nil {
			return executionIDs{}, fmt.Errorf("%w: decision identity", bridge.ErrExecution)
		}
		if ids.preflightID, err = service.newID("preflight"); err != nil {
			return executionIDs{}, fmt.Errorf("%w: preflight identity", bridge.ErrExecution)
		}
		if state != nil {
			state.selectionID, state.decisionID, state.preflightID = ids.selectionID, ids.decisionID, ids.preflightID
		}
	}
	if ids.packetID, err = service.newID("packet"); err != nil {
		return executionIDs{}, fmt.Errorf("%w: packet identity", bridge.ErrExecution)
	}
	if ids.loopID, err = service.newID("loop"); err != nil {
		return executionIDs{}, fmt.Errorf("%w: loop identity", bridge.ErrExecution)
	}
	return ids, nil
}

type runPhase struct {
	Name         string   `json:"name"`
	Agent        string   `json:"agent,omitempty"`
	Status       string   `json:"status"`
	StartedAt    string   `json:"startedAt,omitempty"`
	Artifacts    []string `json:"artifacts"`
	MemoryWrites []string `json:"memoryWrites"`
	Validations  []string `json:"validations"`
}

type runTask struct {
	TaskID          string `json:"taskId"`
	Phase           string `json:"phase"`
	AgentID         string `json:"agentId"`
	Status          string `json:"status"`
	ContextPacketID string `json:"contextPacketId"`
	LoopID          string `json:"loopId,omitempty"`
	ResultID        string `json:"resultId,omitempty"`
}

type runMemoryWrite struct {
	ID         string         `json:"id"`
	Backend    string         `json:"backend"`
	TopicKey   string         `json:"topicKey,omitempty"`
	Type       string         `json:"type,omitempty"`
	Phase      string         `json:"phase,omitempty"`
	Provenance map[string]any `json:"provenance"`
	At         string         `json:"at"`
}

type continuityCapsule struct {
	Kind          string             `json:"kind"`
	SchemaVersion string             `json:"schemaVersion"`
	CapsuleID     string             `json:"capsuleId"`
	RunID         string             `json:"runId"`
	StateVersion  int                `json:"stateVersion"`
	Status        string             `json:"status"`
	DecisionIDs   []string           `json:"decisionIds"`
	TaskStates    []capsuleTaskState `json:"taskStates"`
	ArtifactRefs  []json.RawMessage  `json:"artifactRefs"`
	NextActions   []string           `json:"nextActions"`
	Provenance    map[string]any     `json:"provenance"`
}

type capsuleTaskState struct {
	TaskID   string `json:"taskId"`
	Status   string `json:"status"`
	ResultID string `json:"resultId,omitempty"`
}

type agentResult struct {
	ResultID         string            `json:"resultId"`
	TaskID           string            `json:"taskId"`
	Status           string            `json:"status"`
	Summary          string            `json:"summary"`
	NextRecommended  string            `json:"nextRecommended"`
	MemoryCandidates []memoryCandidate `json:"memoryCandidates,omitempty"`
}

type memoryCandidate struct {
	Type       string  `json:"type"`
	Title      string  `json:"title"`
	Content    string  `json:"content"`
	TopicKey   string  `json:"topicKey"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}

func (service *Service) openContinuity(ctx context.Context, paths config.Paths, root string, input bridge.DispatchRequest) (*continuityState, error) {
	if input.Continuity == bridge.ContinuitySingle {
		return nil, nil
	}
	state := &continuityState{mode: input.Continuity, workspace: root}
	if input.Continuity == bridge.ContinuityStart {
		if _, present, err := chronicle.ReadCurrent(ctx, paths.CurrentRun); err != nil {
			return nil, fmt.Errorf("%w: inspect active run", bridge.ErrExecution)
		} else if present {
			return nil, fmt.Errorf("%w: another continuity run is active", bridge.ErrDenied)
		}
		runID, err := service.newID("run")
		if err != nil {
			return nil, fmt.Errorf("%w: run identity", bridge.ErrExecution)
		}
		state.runID = runID
	} else {
		state.runID = input.RunID
	}
	store, err := chronicle.NewSnapshotStore(paths.Root, state.runID)
	if err != nil {
		return nil, fmt.Errorf("%w: continuity store", bridge.ErrExecution)
	}
	log, err := chronicle.NewEventLog(paths.Root, state.runID)
	if err != nil {
		return nil, fmt.Errorf("%w: continuity log", bridge.ErrExecution)
	}
	state.store, state.log = store, log
	if input.Continuity == bridge.ContinuityContinue || input.Continuity == bridge.ContinuityFinish {
		recovered, recoverErr := store.Recover(ctx)
		if recoverErr != nil || !recovered.CurrentPresent || recovered.RunID != input.RunID {
			return nil, fmt.Errorf("%w: recover active run", bridge.ErrExecution)
		}
		if recovered.Current.Status == "running" || recovered.Current.Status == "recovering" {
			return nil, fmt.Errorf("%w: active phase requires recovery before continuation", bridge.ErrDenied)
		}
		if json.Unmarshal(recovered.Run, &state.snapshot) != nil {
			return nil, fmt.Errorf("%w: decode active run", bridge.ErrExecution)
		}
		state.expected = &recovered.Current
		state.selectionID = rawIdentity(state.snapshot.OrchestratorSelection, "selectionId")
		state.decisionID = rawIdentity(state.snapshot.RoutingDecision, "decisionId")
		state.preflightID = rawIdentity(state.snapshot.SDDPreflight, "preflightId")
		if state.selectionID == "" || state.decisionID == "" || state.preflightID == "" {
			return nil, fmt.Errorf("%w: incomplete active run identities", bridge.ErrExecution)
		}
		if len(state.snapshot.Capsules) > 0 {
			state.previousCapsule, _ = json.Marshal(state.snapshot.Capsules[len(state.snapshot.Capsules)-1])
		}
	}
	if service.memory == nil {
		return nil, fmt.Errorf("%w: continuity memory", bridge.ErrUnavailable)
	}
	taskMemory, err := service.openTaskMemory(ctx, root, input.Goal)
	if err != nil {
		return nil, err
	}
	if taskMemory == nil {
		return nil, fmt.Errorf("%w: continuity memory", bridge.ErrUnavailable)
	}
	state.project, state.retrievedMemories = taskMemory.project, taskMemory.retrievedMemories
	return state, nil
}

func (service *Service) stageContinuity(ctx context.Context, state *continuityState, input bridge.DispatchRequest, taskID, packetID, loopID string) error {
	if state == nil {
		return nil
	}
	now := service.now().UTC().Format(time.RFC3339Nano)
	if state.mode == bridge.ContinuityStart {
		state.snapshot = newRunSnapshot(state.runID, state.project, input.Goal, now, state.selectionID, state.decisionID, state.preflightID, input.Operation)
		for _, event := range []struct {
			typeName string
			fields   map[string]any
		}{
			{"run.started", map[string]any{"data": map[string]any{"continuity": "start"}}},
			{"orchestrator.selected", map[string]any{"selectionId": state.selectionID}},
			{"routing.decided", map[string]any{"decisionId": state.decisionID}},
			{"preflight.completed", map[string]any{"preflightId": state.preflightID}},
			{"phase.started", map[string]any{"phase": "apply", "agent": agentID, "data": map[string]any{"continuity": true}}},
		} {
			if _, err := service.appendContinuityEvent(ctx, state.log, event.typeName, event.fields); err != nil {
				return err
			}
		}
	}
	state.snapshot.Tasks = append(state.snapshot.Tasks, runTask{
		TaskID: taskID, Phase: "apply", AgentID: agentID, Status: "pending", ContextPacketID: packetID, LoopID: loopID,
	})
	events, err := state.log.Read(ctx)
	if err != nil || len(events) == 0 {
		return fmt.Errorf("%w: read continuity events", bridge.ErrExecution)
	}
	state.snapshot.Status = "running"
	state.snapshot.UpdatedAt = now
	current := chronicle.CurrentRun{
		SchemaVersion: "1", ID: state.runID, Project: state.snapshot.Project, Goal: state.snapshot.Goal, Status: "running", Phase: "apply",
		SelectionID: state.selectionID, DecisionID: state.decisionID, PreflightID: state.preflightID, TaskID: taskID,
		LastEventID: events[len(events)-1].ID, ArtifactIDs: artifactIDs(state.snapshot.Artifacts), StorageMode: state.snapshot.StorageMode,
		StartedAt: state.snapshot.CreatedAt, UpdatedAt: now,
	}
	if len(state.snapshot.Capsules) > 0 {
		current.CapsuleID = state.snapshot.Capsules[len(state.snapshot.Capsules)-1].CapsuleID
	}
	document, err := json.Marshal(state.snapshot)
	if err != nil {
		return fmt.Errorf("%w: encode continuity snapshot", bridge.ErrExecution)
	}
	if state.expected == nil {
		err = state.store.WriteActive(ctx, document, current)
	} else {
		err = state.store.WriteActiveContinuation(ctx, document, current, *state.expected)
	}
	if err != nil {
		return fmt.Errorf("%w: stage continuity snapshot", bridge.ErrExecution)
	}
	state.staged = current
	return nil
}

func (service *Service) openTaskMemory(ctx context.Context, workspace, goal string) (*taskMemoryState, error) {
	if service == nil || service.memory == nil {
		return nil, nil
	}
	options := service.taskMemoryOptions(workspace)
	project, err := service.memory.ResolveProject(ctx, options, workspace)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve task memory project", bridge.ErrExecution)
	}
	found, err := service.memory.Search(ctx, options, memory.SearchRequest{
		Query: memoryQuery(goal), Project: project, Scope: memory.ScopeProject, Limit: continuityMemoryLimit, MatchAny: true,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: task memory search", bridge.ErrExecution)
	}
	hydrated := make([]memory.MemoryResult, 0, len(found))
	for _, candidate := range found {
		item, getErr := service.memory.Get(ctx, options, memory.GetRequest{ID: candidate.ID, Project: project, Scope: memory.ScopeProject})
		if getErr != nil {
			if errors.Is(getErr, memory.ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("%w: hydrate task memory", bridge.ErrExecution)
		}
		hydrated = append(hydrated, item)
	}
	return &taskMemoryState{project: project, workspace: workspace, retrievedMemories: hydrated}, nil
}

func taskMemoryFromContinuity(state *continuityState) *taskMemoryState {
	if state == nil {
		return nil
	}
	return &taskMemoryState{project: state.project, workspace: state.workspace, retrievedMemories: append([]memory.MemoryResult(nil), state.retrievedMemories...)}
}

func (service *Service) completeTaskMemory(ctx context.Context, state *taskMemoryState, input bridge.DispatchRequest, runID, taskID string, resultData json.RawMessage, failed bool) ([]string, error) {
	if state == nil {
		return nil, nil
	}
	result := agentResult{TaskID: taskID, Status: "failed", Summary: "The bounded task failed before producing a valid result.", NextRecommended: "Retry after inspecting the blocker."}
	if !failed {
		if err := json.Unmarshal(resultData, &result); err != nil || result.ResultID == "" || result.TaskID != taskID {
			return nil, fmt.Errorf("%w: task memory result", bridge.ErrExecution)
		}
	}
	topicKey := "task/" + runID + "/" + taskID
	retrievedIDs := memoryIDs(state.retrievedMemories)
	request := memory.SaveRequest{
		Title:      boundedText("Task result: "+input.Goal, 256),
		Content:    continuityMemoryContent(runID, taskID, input.Goal, result),
		Project:    state.project,
		Scope:      memory.ScopeProject,
		Type:       "task-result",
		TopicKey:   topicKey,
		Session:    runID,
		References: retrievedIDs,
	}
	if containsSensitiveMaterial(request.Title + "\n" + request.Content) {
		return retrievedIDs, nil
	}
	saved, err := service.findTaskMemory(ctx, state, topicKey, "task-result")
	if err == nil && saved.ID == "" {
		saved, err = service.memory.Save(ctx, service.taskMemoryOptions(state.workspace), request)
		if err != nil {
			saved, _ = service.findTaskMemory(ctx, state, topicKey, "task-result")
			if saved.ID != "" {
				err = nil
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("%w: task memory save", bridge.ErrExecution)
	}
	if saved.ID == "" || saved.Project != state.project || saved.Scope != memory.ScopeProject || saved.Type != "task-result" || saved.TopicKey != topicKey || saved.Session != runID {
		return nil, fmt.Errorf("%w: task memory identity", bridge.ErrExecution)
	}
	refs := append(retrievedIDs, saved.ID)
	if !failed {
		candidateRefs, candidateErr := service.persistMemoryCandidates(ctx, state, runID, result, saved)
		if candidateErr != nil {
			return nil, candidateErr
		}
		refs = append(refs, candidateRefs...)
	}
	return refs, nil
}

func (service *Service) persistMemoryCandidates(ctx context.Context, state *taskMemoryState, runID string, result agentResult, taskResult memory.MemoryResult) ([]string, error) {
	if len(result.MemoryCandidates) == 0 {
		return nil, nil
	}
	options := service.taskMemoryOptions(state.workspace)
	refs := make([]string, 0, len(result.MemoryCandidates))
	for _, candidate := range result.MemoryCandidates {
		candidate.Type = strings.TrimSpace(candidate.Type)
		candidate.Title = boundedText(candidate.Title, 256)
		candidate.Content = boundedText(candidate.Content, 3000)
		candidate.TopicKey = strings.TrimSpace(candidate.TopicKey)
		candidate.Reason = boundedText(candidate.Reason, 512)
		if !validMemoryCandidate(candidate) {
			continue
		}
		topicKey := "agent/" + candidate.Type + "/" + candidate.TopicKey
		content := memoryCandidateContent(candidate)
		existing, err := service.findMemoryCandidate(ctx, state, topicKey, candidate.Type)
		if err != nil {
			return nil, fmt.Errorf("%w: memory candidate lookup", bridge.ErrExecution)
		}
		saveState := memory.StateActive
		if existing.ID != "" {
			if existing.Content == content {
				refs = append(refs, existing.ID)
				continue
			}
			if strings.HasPrefix(existing.Content, "Proposed update: "+content+" | Previous value: ") {
				refs = append(refs, existing.ID)
				continue
			}
			saveState = memory.StateNeedsReview
			previous := existing.Content
			if _, prior, ok := strings.Cut(previous, " | Previous value: "); ok && strings.HasPrefix(previous, "Proposed update: ") {
				previous = prior
			}
			content = boundedText("Proposed update: "+content+" | Previous value: "+previous, 4096)
		}
		saved, err := service.memory.Save(ctx, options, memory.SaveRequest{
			Title: candidate.Title, Content: content, Project: state.project, Scope: memory.ScopeProject,
			Type: candidate.Type, TopicKey: topicKey, Session: runID, State: saveState, References: []string{taskResult.ID},
		})
		if err != nil {
			return nil, fmt.Errorf("%w: memory candidate save", bridge.ErrExecution)
		}
		if saved.ID == "" || saved.TopicKey != topicKey || saved.State != saveState {
			return nil, fmt.Errorf("%w: memory candidate identity", bridge.ErrExecution)
		}
		refs = append(refs, saved.ID)
	}
	return refs, nil
}

func (service *Service) findMemoryCandidate(ctx context.Context, state *taskMemoryState, topicKey, typeName string) (memory.MemoryResult, error) {
	items, err := service.memory.Search(ctx, service.taskMemoryOptions(state.workspace), memory.SearchRequest{
		Query: "candidate", Project: state.project, Scope: memory.ScopeProject, Type: typeName, TopicKey: topicKey,
		States: []memory.State{memory.StateActive, memory.StateNeedsReview}, Limit: 2,
	})
	if err != nil {
		return memory.MemoryResult{}, err
	}
	if len(items) == 0 {
		return memory.MemoryResult{}, nil
	}
	if len(items) != 1 || items[0].TopicKey != topicKey {
		return memory.MemoryResult{}, fmt.Errorf("ambiguous memory candidate topic")
	}
	return service.memory.Get(ctx, service.taskMemoryOptions(state.workspace), memory.GetRequest{
		ID: items[0].ID, Project: state.project, Scope: memory.ScopeProject,
	})
}

func validMemoryCandidate(candidate memoryCandidate) bool {
	if _, ok := durableMemoryCandidateTypes[candidate.Type]; !ok || candidate.Confidence < memoryCandidateConfidence {
		return false
	}
	if candidate.Title == "" || candidate.Content == "" || candidate.Reason == "" || !validCandidateTopic(candidate.TopicKey) {
		return false
	}
	return !containsSensitiveMaterial(candidate.Title + "\n" + candidate.Content + "\n" + candidate.Reason)
}

func containsSensitiveMaterial(value string) bool {
	combined := strings.ToLower(value)
	compact := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '"' || r == '\'' {
			return -1
		}
		return r
	}, combined)
	for _, marker := range []string{
		"-----begin private key", "-----begin rsa private key", "-----begin openssh private key",
		"-----begin ec private key", "-----begin dsa private key", "authorization: bearer ",
		"sk-proj-", "xoxb-", "aws_secret_access_key",
	} {
		if strings.Contains(combined, marker) {
			return true
		}
	}
	for _, marker := range []string{
		"password=", "password:", "passwd=", "passwd:", "secret=", "secret:",
		"api_key=", "api_key:", "apikey=", "apikey:", "token=", "token:", "ghp_",
	} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

func validCandidateTopic(value string) bool {
	runes := []rune(value)
	if len(runes) == 0 || len(runes) > 200 || !unicode.IsLetter(runes[0]) && !unicode.IsNumber(runes[0]) {
		return false
	}
	for _, r := range runes[1:] {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '.' && r != '_' && r != ':' && r != '/' && r != '-' {
			return false
		}
	}
	return true
}

func memoryCandidateContent(candidate memoryCandidate) string {
	return fmt.Sprintf(
		"VGXNESS memory candidate | Type: %s | Reason: %s | Content: %s",
		candidate.Type, candidate.Reason, candidate.Content,
	)
}

func (service *Service) findTaskMemory(ctx context.Context, state *taskMemoryState, topicKey, typeName string) (memory.MemoryResult, error) {
	items, err := service.memory.Search(ctx, service.taskMemoryOptions(state.workspace), memory.SearchRequest{
		Query: "task", Project: state.project, Scope: memory.ScopeProject, Type: typeName, TopicKey: topicKey, Limit: 2,
	})
	if err != nil {
		return memory.MemoryResult{}, err
	}
	if len(items) == 0 {
		return memory.MemoryResult{}, nil
	}
	if len(items) != 1 || items[0].TopicKey != topicKey {
		return memory.MemoryResult{}, fmt.Errorf("ambiguous task memory topic")
	}
	return items[0], nil
}

func (service *Service) taskMemoryOptions(workspace string) config.Options {
	return config.Options{StorageRoot: service.storageRoot, ProjectDir: workspace}
}

func (service *Service) completeContinuity(ctx context.Context, state *continuityState, input bridge.DispatchRequest, taskID string, resultData json.RawMessage, failed bool) (continuityOutcome, error) {
	return service.completeContinuityWithFailureStatus(ctx, state, input, taskID, resultData, failed, string(chronicle.TaskFailed))
}

func (service *Service) completeUnstartedContinuity(ctx context.Context, state *continuityState, input bridge.DispatchRequest, taskID string) (continuityOutcome, error) {
	return service.completeContinuityWithFailureStatus(ctx, state, input, taskID, nil, true, string(chronicle.TaskPending))
}

func (service *Service) completeContinuityWithFailureStatus(ctx context.Context, state *continuityState, input bridge.DispatchRequest, taskID string, resultData json.RawMessage, failed bool, failureStatus string) (continuityOutcome, error) {
	if state == nil {
		return continuityOutcome{}, nil
	}
	if failureStatus != string(chronicle.TaskFailed) && failureStatus != string(chronicle.TaskPending) {
		return continuityOutcome{}, fmt.Errorf("%w: continuity failure status", bridge.ErrExecution)
	}
	result := agentResult{TaskID: taskID, Status: "failed", Summary: "The bounded phase failed before producing a valid result.", NextRecommended: "Retry the bounded phase after inspecting the blocker."}
	if !failed {
		if err := json.Unmarshal(resultData, &result); err != nil || result.ResultID == "" || result.TaskID != taskID {
			return continuityOutcome{}, fmt.Errorf("%w: continuity result", bridge.ErrExecution)
		}
		if !containsRawIdentity(state.snapshot.Results, "resultId", result.ResultID) {
			state.snapshot.Results = append(state.snapshot.Results, append(json.RawMessage(nil), resultData...))
		}
	}
	task := findTask(state.snapshot.Tasks, taskID)
	if task == nil {
		return continuityOutcome{}, fmt.Errorf("%w: continuity task", bridge.ErrExecution)
	}
	if !failed && result.Status == "success" {
		task.Status, task.ResultID = "completed", result.ResultID
	} else {
		task.Status = failureStatus
	}
	topicKey := "run/" + state.runID + "/" + taskID
	memoryRequest := memory.SaveRequest{
		Title:   boundedText("Run continuity: "+state.snapshot.Goal, 256),
		Content: "Continuity | " + continuityMemoryContent(state.runID, taskID, input.Goal, result),
		Project: state.project, Scope: memory.ScopeProject, Type: "continuity", TopicKey: topicKey, Session: state.runID,
		References: memoryIDs(state.retrievedMemories),
	}
	memoryResult, err := service.findContinuityMemory(ctx, state, topicKey)
	if err == nil && memoryResult.ID == "" {
		memoryResult, err = service.memory.Save(ctx, service.continuityMemoryOptions(state), memoryRequest)
		if err != nil {
			// Save may have committed before its caller observed the error.
			memoryResult, _ = service.findContinuityMemory(ctx, state, topicKey)
			if memoryResult.ID != "" {
				err = nil
			}
		}
	}
	if err != nil {
		return continuityOutcome{}, fmt.Errorf("%w: continuity memory save", bridge.ErrExecution)
	}
	if memoryResult.ID == "" || memoryResult.TopicKey != topicKey || memoryResult.Project != state.project || memoryResult.Scope != memory.ScopeProject || memoryResult.Type != "continuity" || memoryResult.Session != state.runID {
		return continuityOutcome{}, fmt.Errorf("%w: continuity memory identity", bridge.ErrExecution)
	}
	if err := service.failContinuity("after-memory-commit"); err != nil {
		return continuityOutcome{}, err
	}
	nowTime := memoryResult.CreatedAt
	if nowTime.IsZero() {
		nowTime = service.now().UTC()
	}
	now := nowTime.UTC().Format(time.RFC3339Nano)
	memoryWrite := runMemoryWrite{
		ID: memoryResult.ID, Backend: "memory", TopicKey: memoryResult.TopicKey, Type: memoryResult.Type, Phase: "apply", At: now,
		Provenance: map[string]any{"producer": "vgxness-controlplane", "createdAt": now, "runId": state.runID, "phase": "apply", "agentId": agentID},
	}
	if !containsMemoryWrite(state.snapshot.MemoryWrites, memoryWrite.ID) {
		state.snapshot.MemoryWrites = append(state.snapshot.MemoryWrites, memoryWrite)
	}
	state.snapshot.Phases[0].MemoryWrites = appendUnique(state.snapshot.Phases[0].MemoryWrites, memoryWrite.ID)
	if _, err := service.appendCompletionEvent(ctx, state, taskID, "memory.written", map[string]any{"memoryWrite": memoryWrite.ID}); err != nil {
		return continuityOutcome{}, err
	}
	if err := service.failContinuity("after-memory-written"); err != nil {
		return continuityOutcome{}, err
	}
	capsuleID := completionIdentity("capsule", state.runID, taskID)
	artifactID := completionIdentity("artifact", state.runID, taskID)
	artifact := map[string]any{
		"kind": "artifact.reference", "schemaVersion": "1", "provider": "chronicle", "id": artifactID, "artifactType": "continuity-capsule",
		"uri":        "chronicle://runs/" + state.runID + "/capsules/" + capsuleID,
		"provenance": map[string]any{"producer": "vgxness", "createdAt": now, "runId": state.runID, "phase": "apply", "agentId": agentID},
	}
	artifactData, _ := json.Marshal(artifact)
	if !containsRawIdentity(state.snapshot.Artifacts, "id", artifactID) {
		state.snapshot.Artifacts = append(state.snapshot.Artifacts, artifactData)
	}
	state.snapshot.Phases[0].Artifacts = appendUnique(state.snapshot.Phases[0].Artifacts, artifactID)
	capsuleStatus, runStatus := "active", "paused"
	if failed || result.Status != "success" {
		capsuleStatus, runStatus = "blocked", "blocked"
	} else if state.mode == bridge.ContinuityFinish {
		capsuleStatus, runStatus = "terminal", "completed"
	}
	stateVersion := len(state.snapshot.Capsules) + 1
	if existing := findCapsule(state.snapshot.Capsules, capsuleID); existing != nil {
		stateVersion = existing.StateVersion
	}
	capsule := continuityCapsule{
		Kind: "continuity.capsule", SchemaVersion: "1", CapsuleID: capsuleID, RunID: state.runID,
		StateVersion: stateVersion, Status: capsuleStatus, DecisionIDs: []string{state.decisionID},
		TaskStates: capsuleTaskStates(state.snapshot.Tasks), ArtifactRefs: []json.RawMessage{}, NextActions: []string{result.NextRecommended},
		Provenance: map[string]any{"producer": "vgxness", "createdAt": now, "runId": state.runID, "phase": "apply", "agentId": agentID},
	}
	if findCapsule(state.snapshot.Capsules, capsuleID) == nil {
		state.snapshot.Capsules = append(state.snapshot.Capsules, capsule)
	}
	capsuleEvent, err := service.appendCompletionEvent(ctx, state, taskID, "capsule.written", map[string]any{"capsuleId": capsuleID, "artifact": artifact})
	if err != nil {
		return continuityOutcome{}, err
	}
	if err := service.failContinuity("after-capsule-written"); err != nil {
		return continuityOutcome{}, err
	}
	state.snapshot.Status = runStatus
	state.snapshot.UpdatedAt = now
	if runStatus == "completed" {
		state.snapshot.Phases[0].Status = "completed"
	}
	current := state.staged
	current.Status, current.ResultID, current.CapsuleID = runStatus, result.ResultID, capsuleID
	current.LastEventID, current.ArtifactIDs, current.UpdatedAt = capsuleEvent.ID, artifactIDs(state.snapshot.Artifacts), now
	document, err := json.Marshal(state.snapshot)
	if err != nil {
		return continuityOutcome{}, fmt.Errorf("%w: encode completed continuity", bridge.ErrExecution)
	}
	if runStatus == "completed" {
		runEvent, err := service.appendCompletionEvent(ctx, state, taskID, "run.completed", map[string]any{"artifact": artifact})
		if err != nil {
			return continuityOutcome{}, err
		}
		current.LastEventID = runEvent.ID
		if err := service.failContinuity("before-snapshot-publication"); err != nil {
			return continuityOutcome{}, err
		}
		document, err = json.Marshal(state.snapshot)
		if err != nil {
			return continuityOutcome{}, fmt.Errorf("%w: encode terminal continuity", bridge.ErrExecution)
		}
		if err := state.store.Finalize(ctx, document); err != nil {
			return continuityOutcome{}, fmt.Errorf("%w: finalize continuity snapshot: %v", bridge.ErrExecution, err)
		}
	} else {
		if err := service.failContinuity("before-snapshot-publication"); err != nil {
			return continuityOutcome{}, err
		}
		if err := state.store.WriteActiveContinuation(ctx, document, current, state.staged); err != nil {
			published, present, readErr := chronicle.ReadCurrent(ctx, state.store.CurrentPath())
			if readErr != nil || !present || published.ID != state.runID || published.TaskID != taskID || published.CapsuleID != capsuleID {
				return continuityOutcome{}, fmt.Errorf("%w: publish continuity snapshot: %v", bridge.ErrExecution, err)
			}
		}
	}
	if err := service.failContinuity("after-snapshot-publication"); err != nil {
		return continuityOutcome{}, err
	}
	refs := make([]string, 0, len(state.retrievedMemories)+1)
	for _, item := range state.retrievedMemories {
		refs = append(refs, item.ID)
	}
	refs = append(refs, memoryResult.ID)
	return continuityOutcome{capsuleID: capsuleID, stateVersion: capsule.StateVersion, memoryRefs: refs}, nil
}

func (service *Service) findContinuityMemory(ctx context.Context, state *continuityState, topicKey string) (memory.MemoryResult, error) {
	options := service.continuityMemoryOptions(state)
	paths, err := config.PathsFor(options)
	if err != nil {
		return memory.MemoryResult{}, err
	}
	if _, err := os.Stat(paths.Database); errors.Is(err, os.ErrNotExist) {
		return memory.MemoryResult{}, nil
	} else if err != nil {
		return memory.MemoryResult{}, err
	}
	items, err := service.memory.Search(ctx, options, memory.SearchRequest{
		Query: "continuity", Project: state.project, Scope: memory.ScopeProject, Type: "continuity", TopicKey: topicKey, Limit: 2,
	})
	if err != nil {
		return memory.MemoryResult{}, err
	}
	if len(items) == 0 {
		return memory.MemoryResult{}, nil
	}
	if len(items) != 1 || items[0].TopicKey != topicKey {
		return memory.MemoryResult{}, fmt.Errorf("ambiguous continuity memory topic")
	}
	return items[0], nil
}

func (service *Service) continuityMemoryOptions(state *continuityState) config.Options {
	return service.taskMemoryOptions(state.workspace)
}

func (service *Service) appendCompletionEvent(ctx context.Context, state *continuityState, taskID, typeName string, fields map[string]any) (chronicle.Event, error) {
	id := completionIdentity("event-"+typeName, state.runID, taskID)
	events, err := state.log.Read(ctx)
	if err != nil {
		return chronicle.Event{}, fmt.Errorf("%w: read completion evidence", bridge.ErrExecution)
	}
	for _, event := range events {
		if event.ID == id {
			var evidence struct {
				TaskID string `json:"taskId"`
			}
			if event.Type != typeName || json.Unmarshal(event.Raw, &evidence) != nil || evidence.TaskID != taskID {
				return chronicle.Event{}, fmt.Errorf("%w: conflicting completion evidence", bridge.ErrExecution)
			}
			var document map[string]json.RawMessage
			if json.Unmarshal(event.Raw, &document) != nil {
				return chronicle.Event{}, fmt.Errorf("%w: conflicting completion evidence", bridge.ErrExecution)
			}
			for key, expected := range fields {
				encoded, marshalErr := json.Marshal(expected)
				if marshalErr != nil || !sameJSON(document[key], encoded) {
					return chronicle.Event{}, fmt.Errorf("%w: conflicting completion evidence", bridge.ErrExecution)
				}
			}
			return event, nil
		}
	}
	document := map[string]any{"schemaVersion": "1", "eventId": id, "runId": state.runID, "taskId": taskID, "at": service.now().UTC().Format(time.RFC3339Nano), "type": typeName}
	for key, value := range fields {
		document[key] = value
	}
	data, err := json.Marshal(document)
	if err != nil {
		return chronicle.Event{}, fmt.Errorf("%w: encode continuity event", bridge.ErrExecution)
	}
	event, err := state.log.Append(ctx, data)
	if err != nil {
		return chronicle.Event{}, fmt.Errorf("%w: append continuity event", bridge.ErrExecution)
	}
	return event, nil
}

func sameJSON(left, right []byte) bool {
	var leftValue, rightValue any
	return json.Unmarshal(left, &leftValue) == nil && json.Unmarshal(right, &rightValue) == nil && reflect.DeepEqual(leftValue, rightValue)
}

func (service *Service) failContinuity(step string) error {
	if service.continuityFault == nil {
		return nil
	}
	if err := service.continuityFault(step); err != nil {
		return fmt.Errorf("%w: injected continuity failure at %s: %v", bridge.ErrExecution, step, err)
	}
	return nil
}

func completionIdentity(kind, runID, taskID string) string {
	digest := sha256.Sum256([]byte("continuity/v1\x00" + kind + "\x00" + runID + "\x00" + taskID))
	return kind + "-" + fmt.Sprintf("%x", digest[:12])
}

func containsMemoryWrite(items []runMemoryWrite, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func containsRawIdentity(items []json.RawMessage, field, id string) bool {
	for _, item := range items {
		if rawIdentity(item, field) == id {
			return true
		}
	}
	return false
}

func appendUnique(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func findCapsule(items []continuityCapsule, id string) *continuityCapsule {
	for index := range items {
		if items[index].CapsuleID == id {
			return &items[index]
		}
	}
	return nil
}

func (service *Service) appendContinuityEvent(ctx context.Context, log *chronicle.EventLog, typeName string, fields map[string]any) (chronicle.Event, error) {
	eventID, err := service.newID("event")
	if err != nil {
		return chronicle.Event{}, fmt.Errorf("%w: event identity", bridge.ErrExecution)
	}
	document := map[string]any{
		"schemaVersion": "1", "eventId": eventID, "runId": logRunID(log), "at": service.now().UTC().Format(time.RFC3339Nano), "type": typeName,
	}
	for key, value := range fields {
		document[key] = value
	}
	data, err := json.Marshal(document)
	if err != nil {
		return chronicle.Event{}, fmt.Errorf("%w: encode continuity event", bridge.ErrExecution)
	}
	event, err := log.Append(ctx, data)
	if err != nil {
		return chronicle.Event{}, fmt.Errorf("%w: append continuity event", bridge.ErrExecution)
	}
	return event, nil
}

func newRunSnapshot(runID, project, goal, now, selectionID, decisionID, preflightID string, operation bridge.Operation) runSnapshot {
	risk := "low"
	if operation != bridge.ReadFiles {
		risk = "medium"
	}
	selection, _ := json.Marshal(map[string]any{
		"kind": "orchestrator.selection", "schemaVersion": "1", "selectionId": selectionID,
		"needs":      []any{map[string]any{"capability": capabilityID, "version": "1"}},
		"candidates": []any{map[string]any{"provider": "opencode", "capabilities": []any{map[string]any{"capability": capabilityID, "version": "1", "constraints": map[string]any{}}}, "eligible": true, "reasons": []any{}}},
		"status":     "selected", "selectedProvider": "opencode", "policyVersion": "bridge-balanced-v1", "rationale": "exact bounded provider is eligible", "decidedAt": now,
	})
	routing, _ := json.Marshal(map[string]any{
		"kind": "routing.decision", "schemaVersion": "1", "decisionId": decisionID, "inputRefs": []any{}, "difficulty": "medium", "risk": risk,
		"candidates": []any{agentID}, "selectedAgent": agentID, "route": "apply", "rationale": "continue one bounded foreground phase", "policyVersion": "bridge-balanced-v1", "sdd": "skipped", "decidedAt": now,
	})
	preflight, _ := json.Marshal(map[string]any{
		"kind": "sdd.preflight", "schemaVersion": "1", "preflightId": preflightID, "mode": "off", "backend": "none", "status": "not-run", "artifactAccess": false, "checkedAt": now,
	})
	return runSnapshot{
		SchemaVersion: "1", ID: runID, Project: project, Goal: strings.TrimSpace(goal), Status: "running", StorageMode: "user-global", ArtifactBackend: "hybrid",
		OrchestratorSelection: selection, RoutingDecision: routing, SDDPreflight: preflight, CreatedAt: now, UpdatedAt: now,
		Phases:    []runPhase{{Name: "apply", Agent: agentID, Status: "running", StartedAt: now, Artifacts: []string{}, MemoryWrites: []string{}, Validations: []string{}}},
		Artifacts: []json.RawMessage{}, MemoryWrites: []runMemoryWrite{}, Decisions: []json.RawMessage{}, Tasks: []runTask{}, Cancellations: []json.RawMessage{}, Results: []json.RawMessage{}, Capsules: []continuityCapsule{}, Validations: []json.RawMessage{},
	}
}

func memoryQuery(goal string) string {
	seen := map[string]bool{}
	reserved := map[string]bool{
		"and": true, "or": true, "not": true, "near": true,
		"a": true, "an": true, "the": true, "to": true, "of": true, "in": true, "on": true, "for": true, "with": true, "from": true, "is": true, "are": true, "be": true, "this": true, "that": true,
		"un": true, "una": true, "el": true, "la": true, "los": true, "las": true, "de": true, "del": true, "y": true, "o": true, "en": true, "para": true, "con": true, "por": true, "que": true,
	}
	terms := make([]string, 0, 8)
	for _, field := range strings.FieldsFunc(goal, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' }) {
		field = strings.ToLower(field)
		if field == "" || seen[field] || reserved[field] {
			continue
		}
		seen[field] = true
		terms = append(terms, field)
		if len(terms) == 8 {
			break
		}
	}
	if len(terms) == 0 {
		return "continuity"
	}
	return strings.Join(terms, " ")
}

func continuityMemoryContent(runID, taskID, goal string, result agentResult) string {
	return fmt.Sprintf("Run: %s | Task: %s | Goal: %s | Status: %s | Summary: %s | Next: %s", runID, taskID, boundedText(goal, 1024), result.Status, boundedText(result.Summary, 1800), boundedText(result.NextRecommended, 900))
}

func boundedText(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}

func memoryContext(items []memory.MemoryResult) []map[string]any {
	contextItems := make([]map[string]any, 0, len(items))
	remaining := 4096
	for _, item := range items {
		content := []rune(item.Content)
		if len(content) > remaining {
			content = content[:remaining]
		}
		remaining -= len(content)
		contextItems = append(contextItems, map[string]any{"id": item.ID, "title": item.Title, "type": item.Type, "content": string(content)})
		if remaining == 0 {
			break
		}
	}
	return contextItems
}

func memoryIDs(items []memory.MemoryResult) []string {
	ids := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.ID == "" {
			continue
		}
		if _, exists := seen[item.ID]; exists {
			continue
		}
		seen[item.ID] = struct{}{}
		ids = append(ids, item.ID)
	}
	return ids
}

func capsuleTaskStates(tasks []runTask) []capsuleTaskState {
	states := make([]capsuleTaskState, len(tasks))
	for i, task := range tasks {
		states[i] = capsuleTaskState{TaskID: task.TaskID, Status: task.Status, ResultID: task.ResultID}
	}
	return states
}

func artifactIDs(artifacts []json.RawMessage) []string {
	ids := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		if id := rawIdentity(artifact, "id"); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func rawIdentity(document json.RawMessage, field string) string {
	var value map[string]any
	if json.Unmarshal(document, &value) != nil {
		return ""
	}
	identity, _ := value[field].(string)
	return identity
}

func findTask(tasks []runTask, taskID string) *runTask {
	for i := range tasks {
		if tasks[i].TaskID == taskID {
			return &tasks[i]
		}
	}
	return nil
}

func logRunID(log *chronicle.EventLog) string {
	base := filepath.Base(log.Path())
	return strings.TrimSuffix(base, filepath.Ext(base))
}
