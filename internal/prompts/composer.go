package prompts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/vgxness/vgxness/internal/chronicle"
	"github.com/vgxness/vgxness/internal/registry"
)

const maxBundleBytes = 256 << 10

var ErrInvalid = errors.New("invalid prompt composition")

type Scope struct {
	Included []string
	Excluded []string
}

type WorkContext struct {
	RunID              string
	TaskID             string
	Phase              string
	Goal               string
	Scope              Scope
	Inputs             map[string]any
	AllowedPaths       []string
	AllowedTools       []string
	AcceptanceCriteria []string
	ApprovalState      string
	ReturnContract     string
	LoopID             string
	LoopType           string
	MaxIterations      int
	CurrentIteration   int
	Deadline           string
}

type LanguagePolicy struct {
	UserFacing           string
	TechnicalArtifacts   string
	SubagentInstructions string
	ExplicitLanguage     string
}

type Input struct {
	Agent    registry.Agent
	Prompt   registry.ResolvedPrompt
	Skills   []registry.Skill
	Mode     chronicle.TaskMode
	Work     WorkContext
	Language LanguagePolicy
}

// Bundle is provider-neutral, immutable prompt input. System is canonical JSON
// so adapters do not reinterpret prompt ordering or manager/subagent policy.
type Bundle struct {
	SchemaVersion         string
	AgentID               string
	Audience              string
	PromptRef             registry.PromptReference
	PromptRegistryVersion string
	System                string
	SHA256                string
}

type Composer struct{}

func New() *Composer { return &Composer{} }

func (c *Composer) Compose(ctx context.Context, input Input) (Bundle, error) {
	if c == nil {
		return Bundle{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Bundle{}, err
	}
	if err := validateInput(input); err != nil {
		return Bundle{}, err
	}

	audience := "subagent"
	if input.Agent.Mode == "manager" {
		audience = "manager"
	}
	payload := systemPayload{
		Contract: "vgxness.prompt.bundle/v1",
		Prompt: promptSection{
			ID: input.Prompt.Prompt.ID, Version: input.Prompt.Prompt.Version,
			RegistryVersion: input.Prompt.RegistryVersion, Instructions: input.Prompt.Prompt.Instructions,
			Checksum: input.Agent.PromptRef.Checksum,
		},
		Agent: agentSection{
			ID: input.Agent.ID, Name: input.Agent.Name, Role: input.Agent.Role,
			Mode: input.Agent.Mode, Audience: audience, ExecutionMode: string(input.Mode),
		},
		Safety: safetySection{
			AgentCeiling: permissionsSection{
				MayReadFiles: input.Agent.Permissions.MayReadFiles, MayWriteFiles: input.Agent.Permissions.MayWriteFiles,
				MayRunCommands: input.Agent.Permissions.MayRunCommands, MayInstallPackages: input.Agent.Permissions.MayInstallPackages,
				MayCommit: input.Agent.Permissions.MayCommit, MayPush: input.Agent.Permissions.MayPush,
				MayUseNetwork: input.Agent.Permissions.MayUseNetwork, MayUseMCP: input.Agent.Permissions.MayUseMCP,
				AllowedTools: sortedClone(input.Agent.Permissions.AllowedTools), DeniedTools: sortedClone(input.Agent.Permissions.DeniedTools),
				AllowedPaths: sortedClone(input.Agent.Permissions.AllowedPaths), DeniedPaths: sortedClone(input.Agent.Permissions.DeniedPaths),
			},
			ExecutionPolicy: executionPolicySection{
				ForegroundSequential: input.Agent.ExecutionPolicy.ForegroundSequential,
				MayRunBackground:     input.Agent.ExecutionPolicy.MayRunBackground,
				BackgroundReadOnly:   input.Agent.ExecutionPolicy.BackgroundReadOnly,
				MayDelegate:          input.Agent.ExecutionPolicy.MayDelegate,
			},
			EffectiveWorkScope: effectiveWorkScopeSection{
				AllowedPaths: sortedClone(input.Work.AllowedPaths), AllowedTools: sortedClone(input.Work.AllowedTools),
				ExcludedPaths: sortedClone(input.Work.Scope.Excluded), ApprovalState: input.Work.ApprovalState,
				ReadOnly:      input.Mode == chronicle.TaskBackground,
				MayDelegate:   input.Mode == chronicle.TaskForeground && input.Agent.ExecutionPolicy.MayDelegate,
				MayAdvanceRun: input.Mode == chronicle.TaskForeground,
			},
		},
		Skills: skillSections(input.Skills),
		Work: workSection{
			RunID: input.Work.RunID, TaskID: input.Work.TaskID, Phase: input.Work.Phase, Goal: input.Work.Goal,
			Scope: scopeSection{Included: sortedClone(input.Work.Scope.Included), Excluded: sortedClone(input.Work.Scope.Excluded)}, Inputs: input.Work.Inputs,
			AllowedPaths: sortedClone(input.Work.AllowedPaths), AllowedTools: sortedClone(input.Work.AllowedTools),
			AcceptanceCriteria: append([]string(nil), input.Work.AcceptanceCriteria...), ApprovalState: input.Work.ApprovalState,
			Loop: loopSection{ID: input.Work.LoopID, Type: input.Work.LoopType, MaxIterations: input.Work.MaxIterations, CurrentIteration: input.Work.CurrentIteration, Deadline: input.Work.Deadline},
		},
		Language: languageSection{
			UserFacing: input.Language.UserFacing, TechnicalArtifacts: input.Language.TechnicalArtifacts,
			SubagentInstructions: input.Language.SubagentInstructions, ExplicitLanguage: input.Language.ExplicitLanguage,
		},
		Output: outputSection{
			Contract: input.Work.ReturnContract, RequireStructuredResult: true, AdditionalProperties: false,
			Instructions:    "Return only one JSON object with exactly the template keys. Preserve kind, schemaVersion, resultId, taskId, and agentId; replace the outcome fields with valid values and add no prose or Markdown. Every required string, including summary and nextRecommended, must contain non-whitespace text; when no further work is needed, use a short terminal value such as 'No further action.'. Propose memoryCandidates only for durable, reusable, evidence-backed project knowledge; omit routine steps, transient status, speculation, duplicates, credentials, tokens, secrets, and personal data. Each candidate needs type, title, content, stable topicKey, reason, and confidence. VGXNESS validates every proposal and may reject it.",
			AllowedStatuses: []string{"success", "blocked", "failed", "needs_followup", "unsupported"},
			Template: agentResultTemplate{
				Kind: "agent.result", SchemaVersion: "1", ResultID: "result-" + input.Work.TaskID,
				TaskID: input.Work.TaskID, AgentID: input.Agent.ID, Status: "success",
				Summary: "replace with the bounded outcome", Artifacts: []any{}, NextRecommended: "replace with the next safe action", Risks: []string{}, Errors: []any{}, MemoryCandidates: []memoryCandidateTemplate{},
			},
		},
	}
	if audience == "manager" {
		personality := *input.Prompt.Prompt.Personality
		personality.Traits = append([]string(nil), personality.Traits...)
		payload.Personality = &personality
	}
	data, err := json.Marshal(payload)
	if err != nil || len(data) > maxBundleBytes {
		return Bundle{}, fmt.Errorf("%w: composed prompt exceeds safe boundary", ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return Bundle{}, err
	}
	digest := sha256.Sum256(data)
	return Bundle{
		SchemaVersion: "1", AgentID: input.Agent.ID, Audience: audience,
		PromptRef: input.Agent.PromptRef, PromptRegistryVersion: input.Prompt.RegistryVersion,
		System: string(data), SHA256: hex.EncodeToString(digest[:]),
	}, nil
}

func validateInput(input Input) error {
	prompt := input.Prompt.Prompt
	if input.Agent.ID == "" || input.Agent.Name == "" || input.Agent.Role == "" || strings.TrimSpace(prompt.Instructions) == "" || input.Prompt.RegistryVersion == "" {
		return ErrInvalid
	}
	if input.Agent.PromptRef.ID != prompt.ID || input.Agent.PromptRef.Version != prompt.Version || input.Agent.PromptRef.Checksum != registry.PromptChecksum(prompt) {
		return fmt.Errorf("%w: prompt identity mismatch", ErrInvalid)
	}
	wantAudience := "subagent"
	if input.Agent.Mode == "manager" {
		wantAudience = "manager"
	}
	if prompt.Audience != wantAudience || wantAudience == "manager" && prompt.Personality == nil || wantAudience == "subagent" && prompt.Personality != nil {
		return fmt.Errorf("%w: prompt audience or personality mismatch", ErrInvalid)
	}
	if input.Mode != chronicle.TaskForeground && input.Mode != chronicle.TaskBackground {
		return fmt.Errorf("%w: execution mode", ErrInvalid)
	}
	if input.Mode == chronicle.TaskBackground && (!input.Agent.ExecutionPolicy.MayRunBackground || !input.Agent.ExecutionPolicy.BackgroundReadOnly || input.Agent.ExecutionPolicy.MayDelegate) {
		return fmt.Errorf("%w: unsafe background policy", ErrInvalid)
	}
	if input.Work.RunID == "" || input.Work.TaskID == "" || input.Work.Phase == "" || input.Work.Goal == "" || input.Work.ReturnContract == "" || input.Work.LoopID == "" || input.Work.LoopType == "" || input.Work.MaxIterations < 1 || input.Work.CurrentIteration < 0 || input.Work.CurrentIteration >= input.Work.MaxIterations || len(input.Work.AcceptanceCriteria) == 0 {
		return fmt.Errorf("%w: work context", ErrInvalid)
	}
	if !validLanguage(input.Language) || !skillsMatch(input.Agent.SkillRefs, input.Skills) {
		return fmt.Errorf("%w: language policy or skill identity", ErrInvalid)
	}
	return nil
}

func validLanguage(policy LanguagePolicy) bool {
	if policy.UserFacing != "match-user" && policy.UserFacing != "explicit" {
		return false
	}
	if policy.TechnicalArtifacts != "english" && policy.TechnicalArtifacts != "explicit" && policy.TechnicalArtifacts != "project-policy" {
		return false
	}
	if policy.SubagentInstructions != "english" && policy.SubagentInstructions != "explicit" && policy.SubagentInstructions != "project-policy" {
		return false
	}
	if policy.ExplicitLanguage != "" && len(policy.ExplicitLanguage) < 2 {
		return false
	}
	return policy.UserFacing != "explicit" && policy.TechnicalArtifacts != "explicit" && policy.SubagentInstructions != "explicit" || len(policy.ExplicitLanguage) >= 2
}

func skillsMatch(refs []registry.ExactSkillReference, skills []registry.Skill) bool {
	if len(refs) != len(skills) {
		return false
	}
	matched := make([]bool, len(skills))
	for _, ref := range refs {
		found := false
		for index, skill := range skills {
			if !matched[index] && ref.ID == skill.ID && ref.Version == skill.Version && reflect.DeepEqual(ref.Source, skill.Source) && reflect.DeepEqual(ref.Provenance, skill.Provenance) {
				matched[index] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func sortedClone(values []string) []string {
	clone := append([]string(nil), values...)
	sort.Strings(clone)
	return clone
}

func skillSections(skills []registry.Skill) []skillSection {
	sections := make([]skillSection, 0, len(skills))
	for _, skill := range skills {
		sections = append(sections, skillSection{ID: skill.ID, Version: skill.Version, Name: skill.Name, Description: skill.Description, Scope: skill.Scope})
	}
	sort.Slice(sections, func(i, j int) bool {
		return sections[i].ID < sections[j].ID || sections[i].ID == sections[j].ID && sections[i].Version < sections[j].Version
	})
	return sections
}

type systemPayload struct {
	Contract    string                `json:"contract"`
	Prompt      promptSection         `json:"prompt"`
	Personality *registry.Personality `json:"personality,omitempty"`
	Agent       agentSection          `json:"agent"`
	Safety      safetySection         `json:"safety"`
	Skills      []skillSection        `json:"skills"`
	Work        workSection           `json:"work"`
	Language    languageSection       `json:"language"`
	Output      outputSection         `json:"output"`
}

type promptSection struct {
	ID              string            `json:"id"`
	Version         string            `json:"version"`
	RegistryVersion string            `json:"registryVersion"`
	Instructions    string            `json:"instructions"`
	Checksum        registry.Checksum `json:"checksum"`
}

type agentSection struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Role          string `json:"role"`
	Mode          string `json:"mode"`
	Audience      string `json:"audience"`
	ExecutionMode string `json:"executionMode"`
}

type safetySection struct {
	AgentCeiling       permissionsSection        `json:"agentCeiling"`
	EffectiveWorkScope effectiveWorkScopeSection `json:"effectiveWorkScope"`
	ExecutionPolicy    executionPolicySection    `json:"executionPolicy"`
}

type effectiveWorkScopeSection struct {
	AllowedPaths  []string `json:"allowedPaths"`
	AllowedTools  []string `json:"allowedTools"`
	ExcludedPaths []string `json:"excludedPaths"`
	ApprovalState string   `json:"approvalState"`
	ReadOnly      bool     `json:"readOnly"`
	MayDelegate   bool     `json:"mayDelegate"`
	MayAdvanceRun bool     `json:"mayAdvanceRun"`
}

type permissionsSection struct {
	MayReadFiles       bool     `json:"mayReadFiles"`
	MayWriteFiles      bool     `json:"mayWriteFiles"`
	MayRunCommands     bool     `json:"mayRunCommands"`
	MayInstallPackages bool     `json:"mayInstallPackages"`
	MayCommit          bool     `json:"mayCommit"`
	MayPush            bool     `json:"mayPush"`
	MayUseNetwork      bool     `json:"mayUseNetwork"`
	MayUseMCP          bool     `json:"mayUseMcp"`
	AllowedTools       []string `json:"allowedTools"`
	DeniedTools        []string `json:"deniedTools"`
	AllowedPaths       []string `json:"allowedPaths"`
	DeniedPaths        []string `json:"deniedPaths"`
}

type executionPolicySection struct {
	ForegroundSequential bool `json:"foregroundSequential"`
	MayRunBackground     bool `json:"mayRunBackground"`
	BackgroundReadOnly   bool `json:"backgroundReadOnly"`
	MayDelegate          bool `json:"mayDelegate"`
}

type skillSection struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       string `json:"scope"`
}

type scopeSection struct {
	Included []string `json:"included"`
	Excluded []string `json:"excluded"`
}

type workSection struct {
	RunID              string         `json:"runId"`
	TaskID             string         `json:"taskId"`
	Phase              string         `json:"phase"`
	Goal               string         `json:"goal"`
	Scope              scopeSection   `json:"scope"`
	Inputs             map[string]any `json:"inputs"`
	AllowedPaths       []string       `json:"allowedPaths"`
	AllowedTools       []string       `json:"allowedTools"`
	AcceptanceCriteria []string       `json:"acceptanceCriteria"`
	ApprovalState      string         `json:"approvalState"`
	Loop               loopSection    `json:"loop"`
}

type loopSection struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	MaxIterations    int    `json:"maxIterations"`
	CurrentIteration int    `json:"currentIteration"`
	Deadline         string `json:"deadline,omitempty"`
}

type languageSection struct {
	UserFacing           string `json:"userFacing"`
	TechnicalArtifacts   string `json:"technicalArtifacts"`
	SubagentInstructions string `json:"subagentInstructions"`
	ExplicitLanguage     string `json:"explicitLanguage,omitempty"`
}

type outputSection struct {
	Contract                string              `json:"contract"`
	RequireStructuredResult bool                `json:"requireStructuredResult"`
	AdditionalProperties    bool                `json:"additionalProperties"`
	Instructions            string              `json:"instructions"`
	AllowedStatuses         []string            `json:"allowedStatuses"`
	Template                agentResultTemplate `json:"template"`
}

type agentResultTemplate struct {
	Kind             string                    `json:"kind"`
	SchemaVersion    string                    `json:"schemaVersion"`
	ResultID         string                    `json:"resultId"`
	TaskID           string                    `json:"taskId"`
	AgentID          string                    `json:"agentId"`
	Status           string                    `json:"status"`
	Summary          string                    `json:"summary"`
	Artifacts        []any                     `json:"artifacts"`
	NextRecommended  string                    `json:"nextRecommended"`
	Risks            []string                  `json:"risks"`
	Errors           []any                     `json:"errors"`
	MemoryCandidates []memoryCandidateTemplate `json:"memoryCandidates"`
}

type memoryCandidateTemplate struct {
	Type       string  `json:"type"`
	Title      string  `json:"title"`
	Content    string  `json:"content"`
	TopicKey   string  `json:"topicKey"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}
