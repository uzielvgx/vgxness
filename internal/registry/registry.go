package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/vgxness/vgxness/internal/contracts"
)

const maxRegistryBytes = 8 << 20

var (
	ErrInvalid  = errors.New("invalid registry")
	ErrConflict = errors.New("registry conflict")
	ErrNotFound = errors.New("registry entry not found")
)

type Capability struct {
	Capability  string         `json:"capability"`
	Version     string         `json:"version"`
	Constraints map[string]any `json:"constraints,omitempty"`
}

type CapabilityNeed = Capability

type ProviderReference struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	Version  string `json:"version,omitempty"`
}

type SourceReference struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	URI      string `json:"uri,omitempty"`
	Path     string `json:"path,omitempty"`
}

type SkillSource struct {
	Scope    string `json:"scope"`
	Provider string `json:"provider"`
	ID       string `json:"id"`
	URI      string `json:"uri,omitempty"`
	Path     string `json:"path,omitempty"`
}

type Checksum struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}

type SkillProvenance struct {
	RegistryID      string          `json:"registryId"`
	RegistryVersion string          `json:"registryVersion"`
	GeneratedAt     string          `json:"generatedAt"`
	EntryRef        SourceReference `json:"entryRef"`
	Checksum        *Checksum       `json:"checksum,omitempty"`
}

type ExactSkillReference struct {
	Kind          string          `json:"kind"`
	SchemaVersion string          `json:"schemaVersion"`
	ID            string          `json:"id"`
	Version       string          `json:"version"`
	Source        SkillSource     `json:"source"`
	Provenance    SkillProvenance `json:"provenance"`
}

type Permissions struct {
	MayReadFiles       bool     `json:"mayReadFiles"`
	MayWriteFiles      bool     `json:"mayWriteFiles"`
	MayRunCommands     bool     `json:"mayRunCommands"`
	MayInstallPackages bool     `json:"mayInstallPackages"`
	MayCommit          bool     `json:"mayCommit"`
	MayPush            bool     `json:"mayPush"`
	MayUseNetwork      bool     `json:"mayUseNetwork"`
	MayUseMCP          bool     `json:"mayUseMcp"`
	AllowedTools       []string `json:"allowedTools,omitempty"`
	DeniedTools        []string `json:"deniedTools,omitempty"`
	AllowedPaths       []string `json:"allowedPaths,omitempty"`
	DeniedPaths        []string `json:"deniedPaths,omitempty"`
}

type ExecutionPolicy struct {
	ForegroundSequential bool `json:"foregroundSequential"`
	MayRunBackground     bool `json:"mayRunBackground"`
	BackgroundReadOnly   bool `json:"backgroundReadOnly"`
	MayDelegate          bool `json:"mayDelegate"`
}

type PromptReference struct {
	Kind          string   `json:"kind"`
	SchemaVersion string   `json:"schemaVersion"`
	ID            string   `json:"id"`
	Version       string   `json:"version"`
	Checksum      Checksum `json:"checksum"`
}

type Personality struct {
	Identity         string   `json:"identity"`
	Voice            string   `json:"voice"`
	Traits           []string `json:"traits"`
	InteractionStyle string   `json:"interactionStyle"`
}

type Prompt struct {
	SchemaVersion string          `json:"schemaVersion"`
	ID            string          `json:"id"`
	Version       string          `json:"version"`
	Audience      string          `json:"audience"`
	Instructions  string          `json:"instructions"`
	Personality   *Personality    `json:"personality,omitempty"`
	Provenance    json.RawMessage `json:"provenance"`
}

type Agent struct {
	SchemaVersion   string                `json:"schemaVersion"`
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	Role            string                `json:"role"`
	Mode            string                `json:"mode"`
	Provider        ProviderReference     `json:"provider"`
	Hidden          bool                  `json:"hidden"`
	Capabilities    []Capability          `json:"capabilities"`
	SkillRefs       []ExactSkillReference `json:"skillRefs"`
	Permissions     Permissions           `json:"permissions"`
	ExecutionPolicy ExecutionPolicy       `json:"executionPolicy"`
	Provenance      json.RawMessage       `json:"provenance"`
	PromptRef       PromptReference       `json:"promptRef"`
	Model           string                `json:"model,omitempty"`
}

type Skill struct {
	SchemaVersion string          `json:"schemaVersion"`
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Version       string          `json:"version"`
	Source        SkillSource     `json:"source"`
	Description   string          `json:"description"`
	Triggers      []string        `json:"triggers"`
	Scope         string          `json:"scope"`
	Provenance    SkillProvenance `json:"provenance"`
}

type ResolvedAgent struct {
	Agent           Agent
	RegistryVersion string
	GeneratedAt     string
}

type ResolvedPrompt struct {
	Prompt          Prompt
	RegistryVersion string
	GeneratedAt     string
}

type Registry struct {
	agentVersion, agentGeneratedAt   string
	promptVersion, promptGeneratedAt string
	agents                           map[string][]byte
	skills                           map[string][]byte
	prompts                          map[string][]byte
}

type agentsDocument struct {
	Version     string  `json:"version"`
	GeneratedAt string  `json:"generatedAt"`
	Agents      []Agent `json:"agents"`
}

type skillsDocument struct {
	Version     string        `json:"version"`
	GeneratedAt string        `json:"generatedAt"`
	SourceRoots []SkillSource `json:"sourceRoots"`
	Skills      []Skill       `json:"skills"`
}

type promptsDocument struct {
	Version     string   `json:"version"`
	GeneratedAt string   `json:"generatedAt"`
	Prompts     []Prompt `json:"prompts"`
}

func New(ctx context.Context, agentsJSON, skillsJSON, promptsJSON []byte) (*Registry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(agentsJSON) > maxRegistryBytes || len(skillsJSON) > maxRegistryBytes || len(promptsJSON) > maxRegistryBytes {
		return nil, fmt.Errorf("%w: document exceeds size limit", ErrInvalid)
	}
	if err := contracts.Validate(ctx, contracts.AgentsSchemaURI, agentsJSON, false); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if err := contracts.Validate(ctx, contracts.SkillsSchemaURI, skillsJSON, false); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if err := contracts.Validate(ctx, contracts.PromptsSchemaURI, promptsJSON, false); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	var agents agentsDocument
	var skills skillsDocument
	var prompts promptsDocument
	if json.Unmarshal(agentsJSON, &agents) != nil || json.Unmarshal(skillsJSON, &skills) != nil || json.Unmarshal(promptsJSON, &prompts) != nil {
		return nil, fmt.Errorf("%w: malformed document", ErrInvalid)
	}
	if _, err := time.Parse(time.RFC3339, agents.GeneratedAt); err != nil {
		return nil, fmt.Errorf("%w: malformed agent generation time", ErrInvalid)
	}
	if _, err := time.Parse(time.RFC3339, skills.GeneratedAt); err != nil {
		return nil, fmt.Errorf("%w: malformed skill generation time", ErrInvalid)
	}
	if _, err := time.Parse(time.RFC3339, prompts.GeneratedAt); err != nil {
		return nil, fmt.Errorf("%w: malformed prompt generation time", ErrInvalid)
	}

	r := &Registry{
		agentVersion: agents.Version, agentGeneratedAt: agents.GeneratedAt,
		promptVersion: prompts.Version, promptGeneratedAt: prompts.GeneratedAt,
		agents: map[string][]byte{}, skills: map[string][]byte{}, prompts: map[string][]byte{},
	}
	for _, prompt := range prompts.Prompts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key := promptKey(prompt.ID, prompt.Version)
		if _, duplicate := r.prompts[key]; duplicate {
			return nil, fmt.Errorf("%w: duplicate prompt identity", ErrConflict)
		}
		r.prompts[key] = mustJSON(prompt)
	}
	for _, skill := range skills.Skills {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if skill.Source.Scope != skill.Scope || !containsSkillSource(skills.SourceRoots, skill.Source) || skill.Provenance.RegistryVersion != skills.Version || skill.Provenance.GeneratedAt != skills.GeneratedAt {
			return nil, fmt.Errorf("%w: skill scope or provenance mismatch", ErrInvalid)
		}
		key := skillKey(skill.ID, skill.Version)
		if _, duplicate := r.skills[key]; duplicate {
			return nil, fmt.Errorf("%w: duplicate skill identity", ErrConflict)
		}
		r.skills[key] = mustJSON(skill)
	}
	for _, agent := range agents.Agents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if agent.Provider.Version == "" {
			return nil, fmt.Errorf("%w: agent provider version is required", ErrInvalid)
		}
		if _, duplicate := r.agents[agent.ID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate agent identity", ErrConflict)
		}
		seenCapabilities := map[string]struct{}{}
		for _, capability := range agent.Capabilities {
			key := skillKey(capability.Capability, capability.Version)
			if _, duplicate := seenCapabilities[key]; duplicate {
				return nil, fmt.Errorf("%w: duplicate agent capability", ErrConflict)
			}
			seenCapabilities[key] = struct{}{}
		}
		for _, ref := range agent.SkillRefs {
			skill, ok := r.skill(ref.ID, ref.Version)
			if !ok || !reflect.DeepEqual(ref.Source, skill.Source) || !reflect.DeepEqual(ref.Provenance, skill.Provenance) {
				return nil, fmt.Errorf("%w: unresolved exact skill reference", ErrInvalid)
			}
		}
		prompt, ok := r.prompt(agent.PromptRef.ID, agent.PromptRef.Version)
		if !ok || agent.PromptRef.Checksum != PromptChecksum(prompt) {
			return nil, fmt.Errorf("%w: unresolved exact prompt reference", ErrInvalid)
		}
		expectedAudience := "subagent"
		if agent.Mode == "manager" {
			expectedAudience = "manager"
		}
		if prompt.Audience != expectedAudience {
			return nil, fmt.Errorf("%w: prompt audience differs from agent mode", ErrInvalid)
		}
		r.agents[agent.ID] = mustJSON(agent)
	}
	return r, nil
}

func (r *Registry) ResolveAgent(ctx context.Context, id string) (ResolvedAgent, error) {
	if err := ctx.Err(); err != nil {
		return ResolvedAgent{}, err
	}
	data, ok := r.agents[id]
	if !ok {
		return ResolvedAgent{}, ErrNotFound
	}
	var agent Agent
	if json.Unmarshal(data, &agent) != nil {
		return ResolvedAgent{}, fmt.Errorf("%w: stored agent", ErrInvalid)
	}
	return ResolvedAgent{Agent: agent, RegistryVersion: r.agentVersion, GeneratedAt: r.agentGeneratedAt}, nil
}

func (r *Registry) ResolveSkill(ctx context.Context, ref ExactSkillReference, allowedScopes map[string]bool) (Skill, error) {
	if err := ctx.Err(); err != nil {
		return Skill{}, err
	}
	skill, ok := r.skill(ref.ID, ref.Version)
	if !ok || !reflect.DeepEqual(ref.Source, skill.Source) || !reflect.DeepEqual(ref.Provenance, skill.Provenance) {
		return Skill{}, ErrNotFound
	}
	if len(allowedScopes) != 0 && !allowedScopes[skill.Scope] {
		return Skill{}, fmt.Errorf("%w: skill scope is not allowed", ErrNotFound)
	}
	return skill, nil
}

func (r *Registry) ResolvePrompt(ctx context.Context, ref PromptReference) (ResolvedPrompt, error) {
	if err := ctx.Err(); err != nil {
		return ResolvedPrompt{}, err
	}
	prompt, ok := r.prompt(ref.ID, ref.Version)
	if !ok {
		return ResolvedPrompt{}, ErrNotFound
	}
	return ResolvedPrompt{Prompt: prompt, RegistryVersion: r.promptVersion, GeneratedAt: r.promptGeneratedAt}, nil
}

func Satisfies(agent Agent, need CapabilityNeed) bool {
	return SatisfiesCapabilities(agent.Capabilities, need)
}

func SatisfiesCapabilities(declarations []Capability, need CapabilityNeed) bool {
	for _, declaration := range declarations {
		if declaration.Capability != need.Capability || declaration.Version != need.Version {
			continue
		}
		matched := true
		for key, value := range need.Constraints {
			matched = matched && reflect.DeepEqual(declaration.Constraints[key], value)
		}
		if matched {
			return true
		}
	}
	return false
}

func (r *Registry) skill(id, version string) (Skill, bool) {
	data, ok := r.skills[skillKey(id, version)]
	if !ok {
		return Skill{}, false
	}
	var skill Skill
	return skill, json.Unmarshal(data, &skill) == nil
}

func (r *Registry) prompt(id, version string) (Prompt, bool) {
	data, ok := r.prompts[promptKey(id, version)]
	if !ok {
		return Prompt{}, false
	}
	var prompt Prompt
	return prompt, json.Unmarshal(data, &prompt) == nil
}

func skillKey(id, version string) string { return id + "\x00" + version }

func promptKey(id, version string) string { return id + "\x00" + version }

// PromptChecksum binds an agent prompt reference to the exact stored template.
func PromptChecksum(prompt Prompt) Checksum {
	var provenance any
	if json.Unmarshal(prompt.Provenance, &provenance) != nil {
		return Checksum{}
	}
	payload := struct {
		SchemaVersion string       `json:"schemaVersion"`
		ID            string       `json:"id"`
		Version       string       `json:"version"`
		Audience      string       `json:"audience"`
		Instructions  string       `json:"instructions"`
		Personality   *Personality `json:"personality,omitempty"`
		Provenance    any          `json:"provenance"`
	}{
		SchemaVersion: prompt.SchemaVersion, ID: prompt.ID, Version: prompt.Version,
		Audience: prompt.Audience, Instructions: prompt.Instructions, Personality: prompt.Personality, Provenance: provenance,
	}
	data := mustJSON(payload)
	digest := sha256.Sum256(data)
	return Checksum{Algorithm: "sha256", Value: hex.EncodeToString(digest[:])}
}

func containsSkillSource(sources []SkillSource, target SkillSource) bool {
	for _, source := range sources {
		if reflect.DeepEqual(source, target) {
			return true
		}
	}
	return false
}

func mustJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
