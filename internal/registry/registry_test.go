package registry

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestRegistryResolvesImmutableExactEntries(t *testing.T) {
	agents, skills, prompts := validDocuments(t)
	entries, err := New(context.Background(), agents, skills, prompts)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := entries.ResolveAgent(context.Background(), "forge")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.RegistryVersion != "agents-v1" || resolved.Agent.Provider.Provider != "opencode" || len(resolved.Agent.SkillRefs) != 1 {
		t.Fatalf("unexpected agent: %+v", resolved)
	}
	ref := resolved.Agent.SkillRefs[0]
	skill, err := entries.ResolveSkill(context.Background(), ref, map[string]bool{"project": true})
	if err != nil || skill.ID != "implement" {
		t.Fatalf("unexpected skill: %+v err=%v", skill, err)
	}
	prompt, err := entries.ResolvePrompt(context.Background(), resolved.Agent.PromptRef)
	if err != nil || prompt.Prompt.ID != "forge-apply" || prompt.Prompt.Audience != "subagent" {
		t.Fatalf("unexpected prompt: %+v err=%v", prompt, err)
	}
	prompt.Prompt.Instructions = "mutated"

	resolved.Agent.Permissions.AllowedTools[0] = "mutated"
	resolved.Agent.Capabilities[0].Constraints["language"] = "mutated"
	again, err := entries.ResolveAgent(context.Background(), "forge")
	if err != nil {
		t.Fatal(err)
	}
	if again.Agent.Permissions.AllowedTools[0] != "shell" || again.Agent.Capabilities[0].Constraints["language"] != "go" {
		t.Fatal("resolved agent mutated immutable registry state")
	}
	againPrompt, err := entries.ResolvePrompt(context.Background(), again.Agent.PromptRef)
	if err != nil || againPrompt.Prompt.Instructions == "mutated" {
		t.Fatal("resolved prompt mutated immutable registry state")
	}
}

func TestRegistryRejectsDuplicateAndUnresolvedIdentities(t *testing.T) {
	validAgents, validSkills, validPrompts := validDocuments(t)
	for name, mutate := range map[string]func(map[string]any, map[string]any, map[string]any){
		"agent": func(agents, _, _ map[string]any) {
			items := agents["agents"].([]any)
			agents["agents"] = append(items, items[0])
		},
		"skill": func(_, skills, _ map[string]any) {
			items := skills["skills"].([]any)
			skills["skills"] = append(items, items[0])
		},
		"prompt": func(_, _, prompts map[string]any) {
			items := prompts["prompts"].([]any)
			prompts["prompts"] = append(items, items[0])
		},
		"unresolved skill": func(agents, _, _ map[string]any) {
			agents["agents"].([]any)[0].(map[string]any)["skillRefs"].([]any)[0].(map[string]any)["version"] = "2"
		},
		"unresolved prompt": func(agents, _, _ map[string]any) {
			agents["agents"].([]any)[0].(map[string]any)["promptRef"].(map[string]any)["version"] = "2"
		},
		"prompt audience": func(_, _, prompts map[string]any) {
			prompts["prompts"].([]any)[0].(map[string]any)["audience"] = "manager"
			prompts["prompts"].([]any)[0].(map[string]any)["personality"] = managerPersonality()
		},
		"undeclared source": func(_, skills, _ map[string]any) {
			skills["sourceRoots"] = []any{}
		},
		"unversioned provider": func(agents, _, _ map[string]any) {
			delete(agents["agents"].([]any)[0].(map[string]any)["provider"].(map[string]any), "version")
		},
	} {
		t.Run(name, func(t *testing.T) {
			var agents, skills, prompts map[string]any
			if json.Unmarshal(validAgents, &agents) != nil || json.Unmarshal(validSkills, &skills) != nil || json.Unmarshal(validPrompts, &prompts) != nil {
				t.Fatal("decode fixtures")
			}
			mutate(agents, skills, prompts)
			agentData, _ := json.Marshal(agents)
			skillData, _ := json.Marshal(skills)
			promptData, _ := json.Marshal(prompts)
			if _, err := New(context.Background(), agentData, skillData, promptData); !errors.Is(err, ErrConflict) && !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected identity rejection, got %v", err)
			}
		})
	}
}

func TestRegistryEnforcesSkillScopeAndCapabilityConstraints(t *testing.T) {
	agents, skills, prompts := validDocuments(t)
	entries, err := New(context.Background(), agents, skills, prompts)
	if err != nil {
		t.Fatal(err)
	}
	resolved, _ := entries.ResolveAgent(context.Background(), "forge")
	if _, err := entries.ResolveSkill(context.Background(), resolved.Agent.SkillRefs[0], map[string]bool{"user": true}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected scope rejection, got %v", err)
	}
	if !Satisfies(resolved.Agent, CapabilityNeed{Capability: "implementation", Version: "1", Constraints: map[string]any{"language": "go"}}) {
		t.Fatal("exact capability should match")
	}
	if Satisfies(resolved.Agent, CapabilityNeed{Capability: "implementation", Version: "1", Constraints: map[string]any{"language": "rust"}}) {
		t.Fatal("mismatched constraint should not match")
	}
}

func TestRegistryHonorsCancellation(t *testing.T) {
	agents, skills, prompts := validDocuments(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(ctx, agents, skills, prompts); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func validDocuments(t *testing.T) ([]byte, []byte, []byte) {
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
	skills := map[string]any{
		"schemaVersion": "1", "version": "skills-v1", "generatedAt": generatedAt, "sourceRoots": []any{source},
		"skills": []any{map[string]any{
			"schemaVersion": "1", "id": "implement", "name": "Implement", "version": "1", "source": source,
			"description": "Implement bounded Go changes", "triggers": []any{"implement"}, "scope": "project", "provenance": provenance,
		}},
	}
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
			},
			"executionPolicy": map[string]any{"foregroundSequential": true, "mayRunBackground": false, "backgroundReadOnly": true, "mayDelegate": false},
			"provenance":      map[string]any{"producer": "registry", "createdAt": generatedAt},
		}},
	}
	prompts := map[string]any{
		"schemaVersion": "1", "version": "prompts-v1", "generatedAt": generatedAt,
		"prompts": []any{promptTemplate},
	}
	agentData, err := json.Marshal(agents)
	if err != nil {
		t.Fatal(err)
	}
	skillData, err := json.Marshal(skills)
	if err != nil {
		t.Fatal(err)
	}
	promptData, err := json.Marshal(prompts)
	if err != nil {
		t.Fatal(err)
	}
	return agentData, skillData, promptData
}

func promptReferenceFor(t *testing.T, template map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(template)
	if err != nil {
		t.Fatal(err)
	}
	var prompt Prompt
	if err := json.Unmarshal(data, &prompt); err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"kind": "prompt.reference", "schemaVersion": "1", "id": prompt.ID, "version": prompt.Version,
		"checksum": PromptChecksum(prompt),
	}
}

func managerPersonality() map[string]any {
	return map[string]any{
		"identity": "VGXNESS collaborative manager", "voice": "clear and grounded",
		"traits": []any{"curious", "precise"}, "interactionStyle": "work with the user as a thoughtful partner",
	}
}
