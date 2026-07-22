package gatekeeper

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/chronicle"
	"github.com/vgxness/vgxness/internal/registry"
)

func TestGatekeeperAllowsBoundedBalancedOperation(t *testing.T) {
	root := t.TempDir()
	evaluator := testEvaluator(t, root, Policy{Version: "policy-1", Profile: ProfileBalanced})
	request := baseRequest(root)

	decision, err := evaluator.Evaluate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != Allow || decision.AgentRegistryVersion != "agents-v1" || decision.Condition != "policy.allowed" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestGatekeeperRejectsOutOfScopeAndSymlinkPaths(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	evaluator := testEvaluator(t, root, Policy{Version: "policy-1", Profile: ProfileBalanced})
	for name, path := range map[string]string{
		"outside": filepath.Join(external, "file.go"),
		"symlink": filepath.Join(root, "escape", "file.go"),
		"denied":  filepath.Join(root, "denied", "file.go"),
	} {
		t.Run(name, func(t *testing.T) {
			request := baseRequest(root)
			request.Path = path
			decision, err := evaluator.Evaluate(context.Background(), request)
			if err != nil || decision.Outcome != Deny || decision.Condition != "work_unit.path" {
				t.Fatalf("unexpected decision: %+v err=%v", decision, err)
			}
		})
	}
	request := baseRequest(root)
	request.WorkUnit.DeniedRoots = []string{filepath.Join(root, "private")}
	request.Path = filepath.Join(root, "private", "file.go")
	decision, err := evaluator.Evaluate(context.Background(), request)
	if err != nil || decision.Outcome != Deny || decision.Condition != "work_unit.path" {
		t.Fatalf("unexpected work-unit exclusion decision: %+v err=%v", decision, err)
	}
}

func TestGatekeeperEnforcesCapabilityPermissionAndTools(t *testing.T) {
	root := t.TempDir()
	evaluator := testEvaluator(t, root, Policy{Version: "policy-1", Profile: ProfileBalanced})
	tests := []struct {
		name      string
		mutate    func(*Request)
		condition string
	}{
		{"capability", func(r *Request) {
			r.RequiredCapability.Version = "2"
			r.Adapter.Capabilities[0].Version = "2"
		}, "capability.missing"},
		{"permission", func(r *Request) {
			r.Operation, r.Path = Network, ""
			r.WorkUnit.Operations = append(r.WorkUnit.Operations, Network)
		}, "permission.denied"},
		{"tool", func(r *Request) {
			r.Operation, r.Path, r.Tool = RunCommand, "", "danger"
			r.WorkUnit.Operations = append(r.WorkUnit.Operations, RunCommand)
			r.WorkUnit.AllowedTools = append(r.WorkUnit.AllowedTools, "danger")
		}, "tool.denied"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := baseRequest(root)
			test.mutate(&request)
			decision, err := evaluator.Evaluate(context.Background(), request)
			if err != nil || decision.Outcome != Deny || decision.Condition != test.condition {
				t.Fatalf("unexpected decision: %+v err=%v", decision, err)
			}
		})
	}
}

func TestGatekeeperEnforcesAdapterIdentityHealthAndCapability(t *testing.T) {
	root := t.TempDir()
	evaluator := testEvaluator(t, root, Policy{Version: "policy-1", Profile: ProfileBalanced})
	tests := []struct {
		name      string
		mutate    func(*Request)
		condition string
	}{
		{"identity", func(r *Request) { r.Adapter.Reference.ID = "other" }, "adapter.identity"},
		{"health", func(r *Request) { r.Adapter.Health = AdapterStale }, "adapter.health"},
		{"capability", func(r *Request) { r.Adapter.Capabilities[0].Version = "2" }, "adapter.capability"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := baseRequest(root)
			test.mutate(&request)
			decision, err := evaluator.Evaluate(context.Background(), request)
			if err != nil || decision.Outcome != Deny || decision.Condition != test.condition {
				t.Fatalf("unexpected adapter decision: %+v err=%v", decision, err)
			}
		})
	}
}

func TestGatekeeperSafeProfileRequiresFreshApproval(t *testing.T) {
	root := t.TempDir()
	evaluator := testEvaluator(t, root, Policy{Version: "policy-1", Profile: ProfileSafe})
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	evaluator.now = func() time.Time { return now }
	request := baseRequest(root)

	decision, err := evaluator.Evaluate(context.Background(), request)
	if err != nil || decision.Outcome != Ask || decision.Condition != "profile.approval_required" {
		t.Fatalf("unexpected decision: %+v err=%v", decision, err)
	}
	request.Approval = validApproval(request, now)
	decision, err = evaluator.Evaluate(context.Background(), request)
	if err != nil || decision.Outcome != Allow {
		t.Fatalf("fresh approval should allow operation: %+v err=%v", decision, err)
	}
	request.Approval.ApprovedAt = time.Time{}
	decision, err = evaluator.Evaluate(context.Background(), request)
	if err != nil || decision.Outcome != Ask || decision.Condition != "profile.approval_required" {
		t.Fatalf("zero-time approval must be rejected: %+v err=%v", decision, err)
	}
}

func TestGatekeeperEnforcesResolvedSkillScopes(t *testing.T) {
	root := t.TempDir()
	evaluator := testEvaluator(t, root, Policy{Version: "policy-1", Profile: ProfileBalanced})
	request := baseRequest(root)
	request.WorkUnit.AllowedSkillScopes = map[string]bool{"user": true}

	decision, err := evaluator.Evaluate(context.Background(), request)
	if err != nil || decision.Outcome != Deny || decision.Condition != "registry.scope" {
		t.Fatalf("unexpected skill scope decision: %+v err=%v", decision, err)
	}
}

func TestGatekeeperFreezesAndValidatesPolicy(t *testing.T) {
	root := t.TempDir()
	leaseRequired := map[OperationClass]bool{WriteFiles: false}
	evaluator := testEvaluator(t, root, Policy{Version: "policy-1", Profile: ProfileBalanced, LeaseRequired: leaseRequired})
	leaseRequired[WriteFiles] = true

	decision, err := evaluator.Evaluate(context.Background(), baseRequest(root))
	if err != nil || decision.Outcome != Allow {
		t.Fatalf("caller mutation changed frozen policy: %+v err=%v", decision, err)
	}
	if _, err := New(testRegistry(t, root), Policy{Version: "policy-1", Profile: ProfileBalanced, LeaseRequired: map[OperationClass]bool{"unknown": true}}); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("expected invalid policy operation, got %v", err)
	}
}

func TestGatekeeperHardGateRequiresApprovalInAdditionToLease(t *testing.T) {
	root := t.TempDir()
	policy := Policy{Version: "policy-1", Profile: ProfileAutonomous, LeaseRequired: map[OperationClass]bool{Commit: true}}
	evaluator := testEvaluator(t, root, policy)
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	evaluator.now = func() time.Time { return now }
	request := baseRequest(root)
	request.Operation, request.Path, request.Tool, request.Risk = Commit, "", "git", RiskHigh
	request.WorkUnit.Operations = append(request.WorkUnit.Operations, Commit)
	request.Lease = validLease(request, now)

	decision, err := evaluator.Evaluate(context.Background(), request)
	if err != nil || decision.Outcome != Ask || decision.Condition != "hard_gate.approval_required" {
		t.Fatalf("lease must not bypass hard gate: %+v err=%v", decision, err)
	}
	request.Approval = validApproval(request, now)
	decision, err = evaluator.Evaluate(context.Background(), request)
	if err != nil || decision.Outcome != Allow {
		t.Fatalf("lease and approval should allow hard gate: %+v err=%v", decision, err)
	}
	request.Lease.ExpiresAt = now
	decision, err = evaluator.Evaluate(context.Background(), request)
	if err != nil || decision.Outcome != Ask || decision.Condition != "lease.required" {
		t.Fatalf("expired lease should require renewal: %+v err=%v", decision, err)
	}
}

func TestGatekeeperRejectsIllegalTaskTransition(t *testing.T) {
	root := t.TempDir()
	evaluator := testEvaluator(t, root, Policy{Version: "policy-1", Profile: ProfileBalanced})
	request := baseRequest(root)
	request.Transition = &TaskTransition{From: chronicle.TaskCompleted, To: chronicle.TaskRunning}

	decision, err := evaluator.Evaluate(context.Background(), request)
	if err != nil || decision.Outcome != Deny || decision.Condition != "transition.illegal" {
		t.Fatalf("unexpected transition decision: %+v err=%v", decision, err)
	}
}

func TestGatekeeperHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	evaluator := testEvaluator(t, root, Policy{Version: "policy-1", Profile: ProfileBalanced})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := evaluator.Evaluate(ctx, baseRequest(root)); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	for name, allowedChecks := range map[string]int{"agent resolution": 1, "skill resolution": 2} {
		t.Run(name, func(t *testing.T) {
			ctx := &cancelAfterChecksContext{allowedChecks: allowedChecks}
			if _, err := evaluator.Evaluate(ctx, baseRequest(root)); !errors.Is(err, context.Canceled) {
				t.Fatalf("expected late cancellation, got %v", err)
			}
		})
	}
}

type cancelAfterChecksContext struct {
	checks        int
	allowedChecks int
}

func (*cancelAfterChecksContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*cancelAfterChecksContext) Done() <-chan struct{}       { return nil }
func (*cancelAfterChecksContext) Value(any) any               { return nil }
func (c *cancelAfterChecksContext) Err() error {
	c.checks++
	if c.checks > c.allowedChecks {
		return context.Canceled
	}
	return nil
}

func baseRequest(root string) Request {
	return Request{
		AgentID: "forge", RequiredCapability: registry.CapabilityNeed{Capability: "implementation", Version: "1", Constraints: map[string]any{"language": "go"}},
		Adapter:   AdapterEvidence{Reference: registry.ProviderReference{Provider: "opencode", ID: "primary", Version: "1"}, Capabilities: []registry.Capability{{Capability: "implementation", Version: "1", Constraints: map[string]any{"language": "go"}}}, Health: AdapterHealthy},
		WorkUnit:  WorkUnit{ID: "work-1", Active: true, AllowedRoots: []string{root}, AllowedTools: []string{"shell", "git"}, AllowedSkillScopes: map[string]bool{"project": true}, Operations: []OperationClass{ReadFiles, WriteFiles}, RiskCeiling: RiskHigh, ContextHash: "context-1"},
		Operation: WriteFiles, Path: filepath.Join(root, "file.go"), Risk: RiskLow, CorrelationID: "correlation-1",
	}
}

func validApproval(request Request, now time.Time) *Approval {
	return &Approval{ID: "approval-1", WorkUnitID: request.WorkUnit.ID, Actor: "user", Wording: "approve exact operation", Operation: request.Operation, CorrelationID: request.CorrelationID, ContextHash: request.WorkUnit.ContextHash, ApprovedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Human: true}
}

func validLease(request Request, now time.Time) *Lease {
	return &Lease{ID: "lease-1", WorkUnitID: request.WorkUnit.ID, ApprovedBy: "user", ApprovalWording: "lease exact operation", AllowedRoots: request.WorkUnit.AllowedRoots, AllowedTools: request.WorkUnit.AllowedTools, Operations: []OperationClass{request.Operation}, RiskCeiling: RiskHigh, ExpiresAt: now.Add(time.Hour), CorrelationID: request.CorrelationID, ContextHash: request.WorkUnit.ContextHash}
}

func testEvaluator(t *testing.T, root string, policy Policy) *Evaluator {
	t.Helper()
	entries := testRegistry(t, root)
	evaluator, err := New(entries, policy)
	if err != nil {
		t.Fatal(err)
	}
	return evaluator
}

func testRegistry(t *testing.T, root string) *registry.Registry {
	t.Helper()
	generatedAt := "2026-07-21T12:00:00Z"
	source := map[string]any{"scope": "project", "provider": "filesystem", "id": "project-skills", "path": "skills"}
	provenance := map[string]any{
		"registryId": "skills-main", "registryVersion": "skills-v1", "generatedAt": generatedAt,
		"entryRef": map[string]any{"provider": "filesystem", "id": "implement-entry", "path": "skills/implement/SKILL.md"},
	}
	skillRef := map[string]any{"kind": "skill.reference", "schemaVersion": "1", "id": "implement", "version": "1", "source": source, "provenance": provenance}
	promptTemplate := map[string]any{
		"schemaVersion": "1", "id": "forge-apply", "version": "1", "audience": "subagent",
		"instructions": "Implement the assigned bounded change and return a structured result.",
		"provenance":   map[string]any{"producer": "registry", "createdAt": generatedAt},
	}
	promptRef := promptReferenceFor(t, promptTemplate)
	agents := map[string]any{
		"schemaVersion": "1", "version": "agents-v1", "generatedAt": generatedAt,
		"agents": []any{map[string]any{
			"schemaVersion": "1", "id": "forge", "name": "Forge", "role": "apply", "mode": "executor",
			"provider": map[string]any{"provider": "opencode", "id": "primary", "version": "1"}, "hidden": false,
			"capabilities": []any{map[string]any{"capability": "implementation", "version": "1", "constraints": map[string]any{"language": "go"}}},
			"skillRefs":    []any{skillRef},
			"promptRef":    promptRef,
			"permissions": map[string]any{
				"mayReadFiles": true, "mayWriteFiles": true, "mayRunCommands": true, "mayInstallPackages": false,
				"mayCommit": true, "mayPush": true, "mayUseNetwork": false, "mayUseMcp": true,
				"allowedTools": []any{"shell", "git", "codegraph"}, "deniedTools": []any{"danger"},
				"allowedPaths": []any{root}, "deniedPaths": []any{filepath.Join(root, "denied")},
			},
			"executionPolicy": map[string]any{"foregroundSequential": true, "mayRunBackground": false, "backgroundReadOnly": true, "mayDelegate": false},
			"provenance":      map[string]any{"producer": "registry", "createdAt": generatedAt},
		}},
	}
	skills := map[string]any{
		"schemaVersion": "1", "version": "skills-v1", "generatedAt": generatedAt, "sourceRoots": []any{source},
		"skills": []any{map[string]any{
			"schemaVersion": "1", "id": "implement", "name": "Implement", "version": "1", "source": source,
			"description": "Implement bounded Go changes", "triggers": []any{"implement"}, "scope": "project", "provenance": provenance,
		}},
	}
	prompts := map[string]any{
		"schemaVersion": "1", "version": "prompts-v1", "generatedAt": generatedAt,
		"prompts": []any{promptTemplate},
	}
	agentData, _ := json.Marshal(agents)
	skillData, _ := json.Marshal(skills)
	promptData, _ := json.Marshal(prompts)
	entries, err := registry.New(context.Background(), agentData, skillData, promptData)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func promptReferenceFor(t *testing.T, template map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(template)
	if err != nil {
		t.Fatal(err)
	}
	var prompt registry.Prompt
	if err := json.Unmarshal(data, &prompt); err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"kind": "prompt.reference", "schemaVersion": "1", "id": prompt.ID, "version": prompt.Version,
		"checksum": registry.PromptChecksum(prompt),
	}
}
