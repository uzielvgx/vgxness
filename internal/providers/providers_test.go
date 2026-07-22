package providers

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/chronicle"
	"github.com/vgxness/vgxness/internal/gatekeeper"
	"github.com/vgxness/vgxness/internal/prompts"
	"github.com/vgxness/vgxness/internal/registry"
)

func TestRunnerExecutesOnlyValidatedAuthorizedAdapter(t *testing.T) {
	root := t.TempDir()
	runner, adapter, authorization, packet := testRunner(t, root, gatekeeper.Policy{Version: "policy-1", Profile: gatekeeper.ProfileBalanced})

	receipt, err := runner.Run(context.Background(), Request{Authorization: authorization, Mode: chronicle.TaskForeground, Packet: packet})
	if err != nil {
		t.Fatal(err)
	}
	if adapter.runs != 1 || receipt.ExecutionID != "execution-1" || receipt.Decision.Outcome != gatekeeper.Allow || receipt.Provider.Reference != adapter.descriptor.Reference {
		t.Fatalf("unexpected execution receipt: %+v runs=%d", receipt, adapter.runs)
	}
	var selection struct {
		SelectionID      string `json:"selectionId"`
		SelectedProvider string `json:"selectedProvider"`
		PolicyVersion    string `json:"policyVersion"`
	}
	if json.Unmarshal(receipt.Selection, &selection) != nil || selection.SelectionID != "selection-1" || selection.SelectedProvider != "opencode" || selection.PolicyVersion != "policy-1" {
		t.Fatalf("unexpected provider selection: %s", receipt.Selection)
	}
	if adapter.last.ExecutionID != "execution-1" || adapter.last.WorkUnitID != "work-1" || len(adapter.last.Skills) != 1 || adapter.last.Agent.ID != "forge" || adapter.last.Operation != gatekeeper.WriteFiles || len(adapter.last.AuthorizedOperations) != 2 {
		t.Fatalf("unexpected invocation: %+v", adapter.last)
	}
	if adapter.last.Prompt.AgentID != "forge" || adapter.last.Prompt.Audience != "subagent" || adapter.last.Prompt.PromptRef.ID != "forge-apply" || adapter.last.Prompt.SHA256 == "" || receipt.PromptSHA256 != adapter.last.Prompt.SHA256 {
		t.Fatalf("prompt composition was not bound to invocation and receipt: invocation=%+v receipt=%+v", adapter.last.Prompt, receipt)
	}
	if !strings.Contains(adapter.last.Prompt.System, "Implement the assigned bounded change") || strings.Contains(adapter.last.Prompt.System, "personality") {
		t.Fatalf("unexpected subagent prompt bundle: %s", adapter.last.Prompt.System)
	}

	receipt.Provider.Capabilities[0].Version = "mutated"
	adapter.descriptor.Capabilities[0].Version = "mutated"
	second, err := runner.Run(context.Background(), Request{Authorization: authorization, Mode: chronicle.TaskForeground, Packet: packet})
	if err != nil || second.Provider.Capabilities[0].Version != "1" {
		var decisionErr *DecisionError
		if errors.As(err, &decisionErr) {
			t.Fatalf("adapter descriptor was not frozen: %+v decision=%+v", second.Provider, decisionErr.Decision)
		}
		t.Fatalf("adapter descriptor was not frozen: %+v err=%v", second.Provider, err)
	}
}

func TestRunnerRejectsPacketScopeAndBackgroundBroadening(t *testing.T) {
	root := t.TempDir()
	runner, adapter, authorization, packet := testRunner(t, root, gatekeeper.Policy{Version: "policy-1", Profile: gatekeeper.ProfileBalanced})

	broadened := mutatePacket(t, packet, func(value map[string]any) {
		value["context"].(map[string]any)["allowedPaths"] = []any{root, t.TempDir()}
	})
	if _, err := runner.Run(context.Background(), Request{Authorization: authorization, Mode: chronicle.TaskForeground, Packet: broadened}); !errors.Is(err, ErrInvalidPacket) {
		t.Fatalf("expected broadened packet rejection, got %v", err)
	}
	if _, err := runner.Run(context.Background(), Request{Authorization: authorization, Mode: chronicle.TaskBackground, Packet: packet}); !errors.Is(err, ErrInvalidPacket) {
		t.Fatalf("expected background write rejection, got %v", err)
	}
	broadenedScope := mutatePacket(t, packet, func(value map[string]any) {
		value["context"].(map[string]any)["scope"].(map[string]any)["included"] = []any{root, "unbounded"}
	})
	if _, err := runner.Run(context.Background(), Request{Authorization: authorization, Mode: chronicle.TaskForeground, Packet: broadenedScope}); !errors.Is(err, ErrInvalidPacket) {
		t.Fatalf("expected declared scope rejection, got %v", err)
	}
	pendingApproval := mutatePacket(t, packet, func(value map[string]any) {
		value["context"].(map[string]any)["approvalState"] = "pending"
	})
	if _, err := runner.Run(context.Background(), Request{Authorization: authorization, Mode: chronicle.TaskForeground, Packet: pendingApproval}); !errors.Is(err, ErrInvalidPacket) {
		t.Fatalf("expected pending approval rejection, got %v", err)
	}
	excludedAuthorization := authorization
	excludedAuthorization.WorkUnit.DeniedRoots = []string{filepath.Join(root, "private")}
	excludedAuthorization.Path = filepath.Join(root, "private", "file.go")
	excludedPacket := mutatePacket(t, packet, func(value map[string]any) {
		value["context"].(map[string]any)["scope"].(map[string]any)["excluded"] = []any{filepath.Join(root, "private")}
	})
	_, err := runner.Run(context.Background(), Request{Authorization: excludedAuthorization, Mode: chronicle.TaskForeground, Packet: excludedPacket})
	var decisionErr *DecisionError
	if !errors.As(err, &decisionErr) || decisionErr.Decision.Condition != "work_unit.path" {
		t.Fatalf("expected excluded path denial, got %v", err)
	}
	if adapter.runs != 0 {
		t.Fatal("adapter ran for invalid packet")
	}
}

func TestRunnerHonorsGatekeeperAndAdapterHealth(t *testing.T) {
	root := t.TempDir()
	runner, adapter, authorization, packet := testRunner(t, root, gatekeeper.Policy{Version: "policy-1", Profile: gatekeeper.ProfileSafe})

	if _, err := runner.Run(context.Background(), Request{Authorization: authorization, Mode: chronicle.TaskForeground, Packet: packet}); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("expected approval requirement, got %v", err)
	}
	if adapter.runs != 0 {
		t.Fatal("adapter bypassed gatekeeper")
	}

	runner, adapter, authorization, packet = testRunner(t, root, gatekeeper.Policy{Version: "policy-1", Profile: gatekeeper.ProfileBalanced})
	adapter.health.Status = gatekeeper.AdapterStale
	_, err := runner.Run(context.Background(), Request{Authorization: authorization, Mode: chronicle.TaskForeground, Packet: packet})
	var decisionErr *DecisionError
	if !errors.As(err, &decisionErr) || !errors.Is(err, ErrDenied) || decisionErr.Decision.Condition != "adapter.health" {
		t.Fatalf("expected adapter health denial, got %v", err)
	}
	if adapter.runs != 0 {
		t.Fatal("unhealthy adapter ran")
	}
	adapter.health.Status = gatekeeper.AdapterHealthy
	adapter.health.CheckedAt = adapter.health.CheckedAt.Add(-maxHealthAge - time.Second)
	_, err = runner.Run(context.Background(), Request{Authorization: authorization, Mode: chronicle.TaskForeground, Packet: packet})
	if !errors.As(err, &decisionErr) || decisionErr.Decision.Condition != "adapter.health" {
		t.Fatalf("expected stale health evidence denial, got %v", err)
	}
}

func TestRunnerRejectsMissingAdapterAndInvalidResult(t *testing.T) {
	root := t.TempDir()
	runner, adapter, authorization, packet := testRunner(t, root, gatekeeper.Policy{Version: "policy-1", Profile: gatekeeper.ProfileBalanced})
	missing, err := New(runner.registry, runner.gatekeeper, prompts.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := missing.Run(context.Background(), Request{Authorization: authorization, Mode: chronicle.TaskForeground, Packet: packet}); !errors.Is(err, ErrAdapterNotFound) {
		t.Fatalf("expected missing adapter, got %v", err)
	}

	adapter.result = validResult(t, "other-task", "forge")
	if _, err := runner.Run(context.Background(), Request{Authorization: authorization, Mode: chronicle.TaskForeground, Packet: packet}); !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("expected result identity rejection, got %v", err)
	}
}

func TestRunnerPropagatesCancellationAndCategorizesFailures(t *testing.T) {
	root := t.TempDir()
	runner, adapter, authorization, packet := testRunner(t, root, gatekeeper.Policy{Version: "policy-1", Profile: gatekeeper.ProfileBalanced})
	validationContext := &cancelAfterChecksContext{allowedChecks: 1}
	if _, err := runner.Run(validationContext, Request{Authorization: authorization, Mode: chronicle.TaskForeground, Packet: packet}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected validation cancellation, got %v", err)
	}
	adapter.err = context.Canceled
	if _, err := runner.Run(context.Background(), Request{Authorization: authorization, Mode: chronicle.TaskForeground, Packet: packet}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected provider cancellation, got %v", err)
	}
	adapter.err = errors.New("provider-specific secret detail")
	_, err := runner.Run(context.Background(), Request{Authorization: authorization, Mode: chronicle.TaskForeground, Packet: packet})
	var failure *Failure
	if !errors.As(err, &failure) || failure.Category != FailureUnavailable || !failure.Recoverable || err.Error() != "provider failure: unavailable" {
		t.Fatalf("unexpected normalized failure: %v", err)
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

func TestRunnerRejectsInvalidAdapterDefinitions(t *testing.T) {
	root := t.TempDir()
	entries, evaluator, _ := testDependencies(t, root, gatekeeper.Policy{Version: "policy-1", Profile: gatekeeper.ProfileBalanced})
	invalid := &fakeAdapter{}
	if _, err := New(entries, evaluator, prompts.New(), invalid); !errors.Is(err, ErrInvalidAdapter) {
		t.Fatalf("expected invalid adapter, got %v", err)
	}
	valid := validFakeAdapter(t)
	if _, err := New(entries, evaluator, prompts.New(), valid, valid); !errors.Is(err, ErrInvalidAdapter) {
		t.Fatalf("expected duplicate adapter, got %v", err)
	}
}

type fakeAdapter struct {
	descriptor Descriptor
	health     Health
	result     []byte
	err        error
	runs       int
	last       Invocation
}

func (f *fakeAdapter) Descriptor() Descriptor        { return f.descriptor }
func (f *fakeAdapter) Health(context.Context) Health { return f.health }
func (f *fakeAdapter) Run(_ context.Context, invocation Invocation) ([]byte, error) {
	f.runs++
	f.last = invocation
	return f.result, f.err
}

func testRunner(t *testing.T, root string, policy gatekeeper.Policy) (*Runner, *fakeAdapter, gatekeeper.Request, []byte) {
	t.Helper()
	entries, evaluator, skillRef := testDependencies(t, root, policy)
	adapter := validFakeAdapter(t)
	runner, err := New(entries, evaluator, prompts.New(), adapter)
	if err != nil {
		t.Fatal(err)
	}
	fixedNow := adapter.health.CheckedAt
	runner.now = func() time.Time { return fixedNow }
	authorization := gatekeeper.Request{
		AgentID: "forge", RequiredCapability: registry.CapabilityNeed{Capability: "implementation", Version: "1", Constraints: map[string]any{"language": "go"}},
		WorkUnit: gatekeeper.WorkUnit{
			ID: "work-1", Active: true, AllowedRoots: []string{root}, AllowedTools: []string{"shell", "git"}, AllowedSkillScopes: map[string]bool{"project": true},
			Operations: []gatekeeper.OperationClass{gatekeeper.ReadFiles, gatekeeper.WriteFiles}, RiskCeiling: gatekeeper.RiskHigh, ContextHash: "context-1",
		},
		Operation: gatekeeper.WriteFiles, Path: filepath.Join(root, "file.go"), Risk: gatekeeper.RiskLow, CorrelationID: "execution-1",
	}
	return runner, adapter, authorization, validPacket(t, root, skillRef)
}

func validFakeAdapter(t *testing.T) *fakeAdapter {
	t.Helper()
	return &fakeAdapter{
		descriptor: Descriptor{
			Reference: registry.ProviderReference{Provider: "opencode", ID: "primary", Version: "1"},
			Source:    registry.SourceReference{Provider: "filesystem", ID: "opencode", Path: "/usr/local/bin/opencode"}, InterfaceVersion: "1",
			Capabilities: []registry.Capability{{Capability: "implementation", Version: "1", Constraints: map[string]any{"language": "go"}}},
		},
		health: Health{Status: gatekeeper.AdapterHealthy, CheckedAt: time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)},
		result: validResult(t, "work-1", "forge"),
	}
}

func testDependencies(t *testing.T, root string, policy gatekeeper.Policy) (*registry.Registry, *gatekeeper.Evaluator, map[string]any) {
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
				"mayCommit": false, "mayPush": false, "mayUseNetwork": false, "mayUseMcp": false,
				"allowedTools": []any{"shell", "git"}, "allowedPaths": []any{root},
			},
			"executionPolicy": map[string]any{"foregroundSequential": true, "mayRunBackground": true, "backgroundReadOnly": true, "mayDelegate": false},
			"provenance":      map[string]any{"producer": "registry", "createdAt": generatedAt},
		}},
	}
	promptRegistry := map[string]any{
		"schemaVersion": "1", "version": "prompts-v1", "generatedAt": generatedAt,
		"prompts": []any{promptTemplate},
	}
	skills := map[string]any{
		"schemaVersion": "1", "version": "skills-v1", "generatedAt": generatedAt, "sourceRoots": []any{source},
		"skills": []any{map[string]any{
			"schemaVersion": "1", "id": "implement", "name": "Implement", "version": "1", "source": source,
			"description": "Implement bounded Go changes", "triggers": []any{"implement"}, "scope": "project", "provenance": provenance,
		}},
	}
	agentData, _ := json.Marshal(agents)
	skillData, _ := json.Marshal(skills)
	promptData, _ := json.Marshal(promptRegistry)
	entries, err := registry.New(context.Background(), agentData, skillData, promptData)
	if err != nil {
		t.Fatal(err)
	}
	evaluator, err := gatekeeper.New(entries, policy)
	if err != nil {
		t.Fatal(err)
	}
	return entries, evaluator, skillRef
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

func validPacket(t *testing.T, root string, skillRef map[string]any) []byte {
	t.Helper()
	packet := map[string]any{
		"kind": "execution.packet", "schemaVersion": "1", "executionId": "execution-1", "selectionId": "selection-1", "decisionId": "decision-1",
		"context": map[string]any{
			"kind": "context.packet", "schemaVersion": "1", "packetId": "packet-1", "runId": "run-1", "taskId": "work-1", "phase": "apply", "goal": "implement",
			"scope": map[string]any{"included": []any{root}, "excluded": []any{}}, "inputs": map[string]any{},
			"allowedPaths": []any{root}, "allowedTools": []any{"shell", "git"}, "artifactRefs": []any{}, "skillRefs": []any{skillRef},
			"acceptanceCriteria": []any{"tests pass"}, "approvalState": "not-required",
			"returnContract": "https://vgxness.dev/schemas/execution.schema.json#/$defs/agentResult",
		},
		"loop":           map[string]any{"kind": "loop.control", "schemaVersion": "1", "loopId": "loop-1", "loopType": "agent", "maxIterations": 2, "currentIteration": 0, "terminal": false},
		"languagePolicy": map[string]any{"kind": "language.policy", "schemaVersion": "1", "userFacing": "match-user", "technicalArtifacts": "english", "subagentInstructions": "english"},
	}
	data, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func validResult(t *testing.T, taskID, agentID string) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"kind": "agent.result", "schemaVersion": "1", "resultId": "result-1", "taskId": taskID, "agentId": agentID,
		"status": "success", "summary": "completed", "artifacts": []any{}, "nextRecommended": "verify", "risks": []any{}, "errors": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mutatePacket(t *testing.T, packet []byte, mutate func(map[string]any)) []byte {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(packet, &value); err != nil {
		t.Fatal(err)
	}
	mutate(value)
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
