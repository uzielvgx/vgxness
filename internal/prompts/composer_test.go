package prompts

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/chronicle"
	"github.com/vgxness/vgxness/internal/registry"
)

func TestComposerIncludesManagerPersonalityAndStablePolicy(t *testing.T) {
	input := validInput("manager")
	input.Agent.Permissions.AllowedTools = []string{"git", "shell"}
	first, err := New().Compose(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	input.Agent.Permissions.AllowedTools = []string{"shell", "git"}
	second, err := New().Compose(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.SHA256 == "" || first.SHA256 != second.SHA256 || first.Audience != "manager" {
		t.Fatalf("prompt bundle is not stable: first=%+v second=%+v", first, second)
	}
	var payload struct {
		Contract    string                `json:"contract"`
		Personality *registry.Personality `json:"personality"`
		Prompt      struct {
			Instructions string `json:"instructions"`
		} `json:"prompt"`
		Language struct {
			SubagentInstructions string `json:"subagentInstructions"`
		} `json:"language"`
	}
	if err := json.Unmarshal([]byte(first.System), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Contract != "vgxness.prompt.bundle/v1" || payload.Personality == nil || payload.Personality.Identity != "VGXNESS manager" || payload.Prompt.Instructions != "Coordinate the work with the user." || payload.Language.SubagentInstructions != "english" {
		t.Fatalf("manager prompt contract is incomplete: %+v", payload)
	}
}

func TestComposerKeepsSubagentFocusedWithoutManagerPersonality(t *testing.T) {
	input := validInput("executor")
	input.Work.Inputs = map[string]any{"operation": "review-changes", "git": map[string]any{"statusShort": " M file.go\n", "worktreeDiff": "diff --git a/file.go b/file.go\n"}}
	input.Agent.SkillRefs = []registry.ExactSkillReference{{ID: "implement", Version: "1"}}
	input.Skills = []registry.Skill{{ID: "implement", Version: "1", Name: "Implement", Description: "Implement bounded changes", Scope: "project"}}
	input.Mode = chronicle.TaskBackground
	input.Agent.ExecutionPolicy.MayRunBackground = true
	bundle, err := New().Compose(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Audience != "subagent" || strings.Contains(bundle.System, "personality") || !strings.Contains(bundle.System, "Implement bounded changes") || !strings.Contains(bundle.System, "Return an exact result") {
		t.Fatalf("unexpected subagent bundle: %s", bundle.System)
	}
	var payload struct {
		Safety struct {
			AgentCeiling struct {
				AllowedPaths []string `json:"allowedPaths"`
			} `json:"agentCeiling"`
			EffectiveWorkScope struct {
				ReadOnly      bool `json:"readOnly"`
				MayAdvanceRun bool `json:"mayAdvanceRun"`
			} `json:"effectiveWorkScope"`
		} `json:"safety"`
		Work struct {
			Inputs map[string]any `json:"inputs"`
		} `json:"work"`
		Output struct {
			AdditionalProperties bool   `json:"additionalProperties"`
			Instructions         string `json:"instructions"`
			Template             struct {
				Kind    string `json:"kind"`
				TaskID  string `json:"taskId"`
				AgentID string `json:"agentId"`
				Summary string `json:"summary"`
			} `json:"template"`
		} `json:"output"`
	}
	if err := json.Unmarshal([]byte(bundle.System), &payload); err != nil || !payload.Safety.EffectiveWorkScope.ReadOnly || payload.Safety.EffectiveWorkScope.MayAdvanceRun || len(payload.Safety.AgentCeiling.AllowedPaths) != 1 || payload.Work.Inputs["operation"] != "review-changes" || payload.Output.AdditionalProperties || payload.Output.Instructions == "" || payload.Output.Template.Kind != "agent.result" || payload.Output.Template.TaskID != "task-1" || payload.Output.Template.AgentID != "forge" || payload.Output.Template.Summary == "" {
		t.Fatalf("effective background scope is ambiguous: %+v err=%v", payload.Safety, err)
	}
}

func TestComposerRejectsIdentityPolicyAndCancellationFailures(t *testing.T) {
	composer := New()
	input := validInput("executor")
	input.Prompt.Prompt.ID = "other"
	if _, err := composer.Compose(context.Background(), input); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected prompt identity rejection, got %v", err)
	}

	input = validInput("executor")
	input.Language.UserFacing = "explicit"
	if _, err := composer.Compose(context.Background(), input); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected incomplete explicit-language rejection, got %v", err)
	}

	input = validInput("executor")
	input.Agent.PromptRef.Checksum.Value = strings.Repeat("0", 64)
	if _, err := composer.Compose(context.Background(), input); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected prompt checksum rejection, got %v", err)
	}

	input = validInput("executor")
	input.Agent.SkillRefs = []registry.ExactSkillReference{{ID: "implement", Version: "1", Source: registry.SkillSource{Scope: "project", ID: "source-a"}}}
	input.Skills = []registry.Skill{{ID: "implement", Version: "1", Source: registry.SkillSource{Scope: "project", ID: "source-b"}}}
	if _, err := composer.Compose(context.Background(), input); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected exact skill provenance rejection, got %v", err)
	}

	input = validInput("executor")
	input.Prompt.Prompt.Instructions = strings.Repeat("x", maxBundleBytes)
	if _, err := composer.Compose(context.Background(), input); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected prompt size rejection, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := composer.Compose(ctx, validInput("executor")); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func validInput(mode string) Input {
	promptRef := registry.PromptReference{Kind: "prompt.reference", SchemaVersion: "1", ID: "forge-prompt", Version: "1"}
	prompt := registry.Prompt{
		SchemaVersion: "1", ID: promptRef.ID, Version: promptRef.Version, Audience: "subagent",
		Instructions: "Return an exact result.",
	}
	if mode == "manager" {
		prompt.Audience = "manager"
		prompt.Instructions = "Coordinate the work with the user."
		prompt.Personality = &registry.Personality{
			Identity: "VGXNESS manager", Voice: "clear and warm", Traits: []string{"curious", "precise"},
			InteractionStyle: "collaborate as a thoughtful partner",
		}
	}
	promptRef.Checksum = registry.PromptChecksum(prompt)
	return Input{
		Agent: registry.Agent{
			SchemaVersion: "1", ID: "forge", Name: "Forge", Role: "apply", Mode: mode, PromptRef: promptRef,
			Permissions:     registry.Permissions{MayReadFiles: true, AllowedTools: []string{"shell"}, AllowedPaths: []string{"/workspace"}},
			ExecutionPolicy: registry.ExecutionPolicy{ForegroundSequential: true, BackgroundReadOnly: true},
		},
		Prompt: registry.ResolvedPrompt{Prompt: prompt, RegistryVersion: "prompts-v1", GeneratedAt: "2026-07-21T12:00:00Z"},
		Mode:   chronicle.TaskForeground,
		Work: WorkContext{
			RunID: "run-1", TaskID: "task-1", Phase: "apply", Goal: "implement",
			Scope: Scope{Included: []string{"/workspace"}}, Inputs: map[string]any{}, AllowedPaths: []string{"/workspace"}, AllowedTools: []string{"shell"},
			AcceptanceCriteria: []string{"tests pass"}, ApprovalState: "not-required",
			ReturnContract: "https://vgxness.dev/schemas/execution.schema.json#/$defs/agentResult",
			LoopID:         "loop-1", LoopType: "agent", MaxIterations: 2, CurrentIteration: 0,
		},
		Language: LanguagePolicy{UserFacing: "match-user", TechnicalArtifacts: "english", SubagentInstructions: "english"},
	}
}
