package navigator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/vgxness/vgxness/internal/contracts"
)

const (
	SchemaVersion      = "1"
	RequestKind        = "delegation.request"
	PlanKind           = "delegation.plan"
	DefaultMaxParallel = 4
	MaxTasks           = 16
)

var (
	ErrInvalidRequest  = errors.New("invalid delegation request")
	ErrUnsupportedTask = errors.New("unsupported delegated task")
	ErrCyclicPlan      = errors.New("cyclic delegation plan")
)

type Capability string
type Operation string
type Continuity string

const (
	CapabilityExplore   Capability = "explore"
	CapabilityVerify    Capability = "verify"
	CapabilityReview    Capability = "review"
	CapabilityImplement Capability = "implement"

	OperationReadFiles        Operation = "read-files"
	OperationAnalyzeStructure Operation = "analyze-structure"
	OperationReviewChanges    Operation = "review-changes"
	OperationWriteFiles       Operation = "write-files"

	ContinuityIsolated Continuity = "isolated"
	ContinuityLinked   Continuity = "linked"
)

type Task struct {
	TaskID             string     `json:"taskId"`
	Capability         Capability `json:"capability"`
	Operation          Operation  `json:"operation"`
	Goal               string     `json:"goal"`
	AcceptanceCriteria []string   `json:"acceptanceCriteria"`
	DependsOn          []string   `json:"dependsOn"`
	Continuity         Continuity `json:"continuity"`
}

type Request struct {
	Kind               string   `json:"kind"`
	SchemaVersion      string   `json:"schemaVersion"`
	Goal               string   `json:"goal"`
	AcceptanceCriteria []string `json:"acceptanceCriteria"`
	CandidateTasks     []Task   `json:"candidateTasks"`
	PolicyVersion      string   `json:"policyVersion"`
	MaxParallel        int      `json:"maxParallel"`
}

type Wave struct {
	WaveID  string   `json:"waveId"`
	Index   int      `json:"index"`
	Mode    string   `json:"mode"`
	TaskIDs []string `json:"taskIds"`
}

type Plan struct {
	Kind          string `json:"kind"`
	SchemaVersion string `json:"schemaVersion"`
	PlanID        string `json:"planId"`
	RequestDigest string `json:"requestDigest"`
	Decision      string `json:"decision"`
	Rationale     string `json:"rationale"`
	PolicyVersion string `json:"policyVersion"`
	MaxParallel   int    `json:"maxParallel"`
	Tasks         []Task `json:"tasks"`
	Waves         []Wave `json:"waves"`
}

// PlanRequest validates an advisory decomposition and computes the only legal
// execution waves. Candidate tasks never carry agent identities or wave
// placement: Registry resolves exact agents later and this planner owns all
// concurrency decisions.
func PlanRequest(ctx context.Context, request Request) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	request = normalizedRequest(request)
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return Plan{}, fmt.Errorf("%w: encode request", ErrInvalidRequest)
	}
	if err := contracts.Validate(ctx, contracts.OrchestrationURI+"#/$defs/delegationRequest", requestJSON, false); err != nil {
		return Plan{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}

	tasks := append([]Task(nil), request.CandidateTasks...)
	if len(tasks) == 0 {
		tasks = []Task{{
			TaskID: "task-1", Capability: CapabilityExplore, Operation: OperationReadFiles,
			Goal: request.Goal, AcceptanceCriteria: append([]string{}, request.AcceptanceCriteria...),
			DependsOn: []string{}, Continuity: ContinuityIsolated,
		}}
	}
	if err := validateTasks(tasks); err != nil {
		return Plan{}, err
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].TaskID < tasks[j].TaskID })
	waves, err := buildWaves(tasks, request.MaxParallel)
	if err != nil {
		return Plan{}, err
	}
	requestHash := sha256.Sum256(requestJSON)
	requestDigest := "sha256-" + hex.EncodeToString(requestHash[:])
	decision := "sequential"
	if len(tasks) == 1 {
		decision = "single"
	} else {
		for _, wave := range waves {
			if len(wave.TaskIDs) > 1 {
				decision = "parallel"
				break
			}
		}
	}
	plan := Plan{
		Kind: PlanKind, SchemaVersion: SchemaVersion,
		RequestDigest: requestDigest, Decision: decision, Rationale: rationale(decision, len(tasks), len(waves), request.MaxParallel),
		PolicyVersion: request.PolicyVersion, MaxParallel: request.MaxParallel, Tasks: tasks, Waves: waves,
	}
	plan.PlanID, err = contentBoundPlanID(plan)
	if err != nil {
		return Plan{}, err
	}
	if err := ValidatePlan(ctx, plan); err != nil {
		return Plan{}, fmt.Errorf("%w: generated plan: %v", ErrInvalidRequest, err)
	}
	return plan, nil
}

// ValidatePlan re-derives the deterministic execution decision and content
// identity. Runtime admission must call it so a stale plan ID cannot authorize
// modified tasks, policy, rationale, or waves.
func ValidatePlan(ctx context.Context, plan Plan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("%w: encode plan", ErrInvalidRequest)
	}
	if err := contracts.Validate(ctx, contracts.OrchestrationURI+"#/$defs/delegationPlan", planJSON, false); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}

	canonicalTasks := canonicalTasks(plan.Tasks)
	if !reflect.DeepEqual(plan.Tasks, canonicalTasks) {
		return fmt.Errorf("%w: non-canonical task order", ErrInvalidRequest)
	}
	if err := validateTasks(canonicalTasks); err != nil {
		return err
	}
	expectedWaves, err := buildWaves(canonicalTasks, plan.MaxParallel)
	if err != nil {
		return err
	}
	decision := decisionFor(canonicalTasks, expectedWaves)
	if plan.Kind != PlanKind || plan.SchemaVersion != SchemaVersion || plan.Decision != decision || plan.Rationale != rationale(decision, len(canonicalTasks), len(expectedWaves), plan.MaxParallel) || !reflect.DeepEqual(plan.Waves, expectedWaves) {
		return fmt.Errorf("%w: non-canonical execution plan", ErrInvalidRequest)
	}
	expectedID, err := contentBoundPlanID(plan)
	if err != nil {
		return err
	}
	if plan.PlanID != expectedID {
		return fmt.Errorf("%w: plan identity mismatch", ErrInvalidRequest)
	}
	return nil
}

func canonicalTasks(input []Task) []Task {
	tasks := make([]Task, len(input))
	for index, task := range input {
		task.AcceptanceCriteria = append([]string{}, task.AcceptanceCriteria...)
		task.DependsOn = append([]string{}, task.DependsOn...)
		sort.Strings(task.DependsOn)
		tasks[index] = task
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].TaskID < tasks[j].TaskID })
	return tasks
}

func decisionFor(tasks []Task, waves []Wave) string {
	if len(tasks) == 1 {
		return "single"
	}
	for _, wave := range waves {
		if len(wave.TaskIDs) > 1 {
			return "parallel"
		}
	}
	return "sequential"
}

// IsParallelSafeTask is the canonical eligibility check shared by planning
// and runtime topology validation.
func IsParallelSafeTask(task Task) bool {
	return (task.Operation == OperationReadFiles || task.Operation == OperationAnalyzeStructure) &&
		task.Continuity == ContinuityIsolated
}

func contentBoundPlanID(plan Plan) (string, error) {
	plan.PlanID = ""
	data, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("%w: encode plan identity", ErrInvalidRequest)
	}
	digest := sha256.Sum256(data)
	return "plan-" + hex.EncodeToString(digest[:]), nil
}

func normalizedRequest(request Request) Request {
	request.AcceptanceCriteria = append([]string{}, request.AcceptanceCriteria...)
	request.CandidateTasks = canonicalTasks(request.CandidateTasks)
	return request
}

func validateTasks(tasks []Task) error {
	if len(tasks) == 0 || len(tasks) > MaxTasks {
		return ErrInvalidRequest
	}
	byID := make(map[string]Task, len(tasks))
	for _, task := range tasks {
		if _, exists := byID[task.TaskID]; exists {
			return fmt.Errorf("%w: duplicate task identity", ErrInvalidRequest)
		}
		byID[task.TaskID] = task
		switch task.Operation {
		case OperationReadFiles, OperationAnalyzeStructure:
			if task.Capability != CapabilityExplore && task.Capability != CapabilityVerify {
				return fmt.Errorf("%w: read task capability", ErrInvalidRequest)
			}
		case OperationReviewChanges:
			if task.Capability != CapabilityReview {
				return fmt.Errorf("%w: review task capability", ErrInvalidRequest)
			}
		case OperationWriteFiles:
			return fmt.Errorf("%w: native edit broker is not available", ErrUnsupportedTask)
		default:
			return ErrInvalidRequest
		}
	}
	for _, task := range tasks {
		seen := make(map[string]struct{}, len(task.DependsOn))
		for _, dependency := range task.DependsOn {
			if dependency == task.TaskID {
				return fmt.Errorf("%w: self dependency", ErrInvalidRequest)
			}
			if _, ok := byID[dependency]; !ok {
				return fmt.Errorf("%w: missing dependency", ErrInvalidRequest)
			}
			if _, duplicate := seen[dependency]; duplicate {
				return fmt.Errorf("%w: duplicate dependency", ErrInvalidRequest)
			}
			seen[dependency] = struct{}{}
		}
		if task.Operation == OperationReviewChanges && len(tasks) > 1 {
			for otherID, other := range byID {
				if otherID == task.TaskID || other.Operation == OperationReviewChanges {
					continue
				}
				if _, ok := seen[otherID]; !ok {
					return fmt.Errorf("%w: review must depend on every prior work unit", ErrInvalidRequest)
				}
			}
		}
	}
	return nil
}

func buildWaves(tasks []Task, maxParallel int) ([]Wave, error) {
	byID := make(map[string]Task, len(tasks))
	indegree := make(map[string]int, len(tasks))
	dependents := make(map[string][]string, len(tasks))
	for _, task := range tasks {
		byID[task.TaskID] = task
		indegree[task.TaskID] = len(task.DependsOn)
		for _, dependency := range task.DependsOn {
			dependents[dependency] = append(dependents[dependency], task.TaskID)
		}
	}
	remaining := len(tasks)
	waves := make([]Wave, 0, len(tasks))
	for remaining > 0 {
		ready := make([]string, 0, remaining)
		for taskID, degree := range indegree {
			if degree == 0 {
				ready = append(ready, taskID)
			}
		}
		if len(ready) == 0 {
			return nil, ErrCyclicPlan
		}
		sort.Strings(ready)
		selected := ready[:1]
		allShareable := true
		for _, taskID := range ready {
			if !IsParallelSafeTask(byID[taskID]) {
				allShareable = false
				break
			}
		}
		if allShareable {
			limit := min(maxParallel, len(ready))
			selected = ready[:limit]
		}
		waveIDs := append([]string(nil), selected...)
		mode := "sequential"
		if len(waveIDs) > 1 {
			mode = "parallel"
		}
		waves = append(waves, Wave{WaveID: fmt.Sprintf("wave-%d", len(waves)+1), Index: len(waves), Mode: mode, TaskIDs: waveIDs})
		for _, taskID := range selected {
			delete(indegree, taskID)
			remaining--
			for _, dependent := range dependents[taskID] {
				indegree[dependent]--
			}
		}
	}
	return waves, nil
}

func rationale(decision string, tasks, waves, maxParallel int) string {
	switch decision {
	case "single":
		return "One bounded native subagent work unit is sufficient; additional delegation would add no independent evidence."
	case "parallel":
		return fmt.Sprintf("%d bounded work units include independent isolated reads; VGXNESS scheduled them across %d dependency waves with a maximum native concurrency of %d.", tasks, waves, maxParallel)
	default:
		return fmt.Sprintf("%d bounded work units have dependencies or exclusive review/continuity constraints; VGXNESS scheduled %d sequential waves.", tasks, waves)
	}
}
