package navigator

import (
	"context"
	"errors"
	"testing"
)

func request(tasks ...Task) Request {
	return Request{
		Kind: RequestKind, SchemaVersion: SchemaVersion, Goal: "Inspect the requested boundaries",
		AcceptanceCriteria: []string{"Return bounded evidence"}, CandidateTasks: tasks,
		PolicyVersion: "bridge-balanced-v1", MaxParallel: DefaultMaxParallel,
	}
}

func readTask(id string, dependencies ...string) Task {
	return Task{
		TaskID: id, Capability: CapabilityExplore, Operation: OperationReadFiles, Goal: "Inspect " + id,
		AcceptanceCriteria: []string{"Return evidence for " + id}, DependsOn: dependencies, Continuity: ContinuityIsolated,
	}
}

func TestPlanRequestDefaultsToOneBoundedNativeTask(t *testing.T) {
	plan, err := PlanRequest(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Decision != "single" || len(plan.Tasks) != 1 || len(plan.Waves) != 1 || plan.Waves[0].Mode != "sequential" {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if plan.Tasks[0].Operation != OperationReadFiles || plan.Tasks[0].Continuity != ContinuityIsolated {
		t.Fatalf("unsafe default task: %#v", plan.Tasks[0])
	}
}

func TestPlanRequestDefaultsWithNoCriteriaOrCandidateTasks(t *testing.T) {
	input := request()
	input.AcceptanceCriteria = []string{}
	input.CandidateTasks = []Task{}
	plan, err := PlanRequest(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Decision != "single" || plan.Tasks[0].AcceptanceCriteria == nil || len(plan.Tasks[0].AcceptanceCriteria) != 0 {
		t.Fatalf("default plan did not preserve an explicit empty criteria array: %#v", plan)
	}
}

func TestPlanRequestParallelizesOnlyIndependentIsolatedReads(t *testing.T) {
	plan, err := PlanRequest(context.Background(), request(readTask("task-memory"), readTask("task-delegation")))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Decision != "parallel" || len(plan.Waves) != 1 || plan.Waves[0].Mode != "parallel" || len(plan.Waves[0].TaskIDs) != 2 {
		t.Fatalf("unexpected parallel plan: %#v", plan)
	}
}

func TestPlanRequestKeepsDependenciesAndLinkedContinuitySequential(t *testing.T) {
	first := readTask("task-a")
	first.Continuity = ContinuityLinked
	plan, err := PlanRequest(context.Background(), request(first, readTask("task-b", "task-a")))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Decision != "sequential" || len(plan.Waves) != 2 || plan.Waves[0].TaskIDs[0] != "task-a" || plan.Waves[1].TaskIDs[0] != "task-b" {
		t.Fatalf("unexpected sequential plan: %#v", plan)
	}
}

func TestPlanRequestSchedulesReviewAfterEveryEvidenceTask(t *testing.T) {
	review := Task{
		TaskID: "task-review", Capability: CapabilityReview, Operation: OperationReviewChanges, Goal: "Review evidence",
		AcceptanceCriteria: []string{}, DependsOn: []string{"task-a", "task-b"}, Continuity: ContinuityIsolated,
	}
	plan, err := PlanRequest(context.Background(), request(readTask("task-a"), readTask("task-b"), review))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Waves) != 2 || plan.Waves[0].Mode != "parallel" || plan.Waves[1].Mode != "sequential" || plan.Waves[1].TaskIDs[0] != "task-review" {
		t.Fatalf("unexpected review plan: %#v", plan)
	}
	review.DependsOn = []string{"task-a"}
	if _, err := PlanRequest(context.Background(), request(readTask("task-a"), readTask("task-b"), review)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("review without complete dependency proof accepted: %v", err)
	}
}

func TestPlanRequestSplitsCapacityAndRejectsCyclesOrWrites(t *testing.T) {
	tasks := []Task{readTask("task-a"), readTask("task-b"), readTask("task-c"), readTask("task-d"), readTask("task-e")}
	plan, err := PlanRequest(context.Background(), request(tasks...))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Waves) != 2 || len(plan.Waves[0].TaskIDs) != 4 || len(plan.Waves[1].TaskIDs) != 1 {
		t.Fatalf("capacity was not sliced: %#v", plan.Waves)
	}
	if _, err := PlanRequest(context.Background(), request(readTask("task-a", "task-b"), readTask("task-b", "task-a"))); !errors.Is(err, ErrCyclicPlan) {
		t.Fatalf("cycle accepted: %v", err)
	}
	write := readTask("task-write")
	write.Capability, write.Operation = CapabilityImplement, OperationWriteFiles
	if _, err := PlanRequest(context.Background(), request(write)); !errors.Is(err, ErrUnsupportedTask) {
		t.Fatalf("write accepted before native edit broker: %v", err)
	}
}

func TestPlanRequestIsContentBoundAndDeterministic(t *testing.T) {
	input := request(readTask("task-b"), readTask("task-a"))
	first, err := PlanRequest(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PlanRequest(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.PlanID != second.PlanID || first.RequestDigest != second.RequestDigest || first.Tasks[0].TaskID != "task-a" {
		t.Fatalf("plan is not deterministic: first=%#v second=%#v", first, second)
	}
	reordered := request(readTask("task-a"), readTask("task-b"))
	third, err := PlanRequest(context.Background(), reordered)
	if err != nil {
		t.Fatal(err)
	}
	if first.PlanID != third.PlanID || first.RequestDigest != third.RequestDigest {
		t.Fatalf("equivalent task order changed content identity: first=%#v third=%#v", first, third)
	}
	mutated := first
	mutated.Tasks = append([]Task(nil), first.Tasks...)
	mutated.Tasks[0].Goal = "A different executable goal"
	if err := ValidatePlan(context.Background(), mutated); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("plan identity did not reject semantic mutation: %v", err)
	}
}
