package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/vgxness/vgxness/internal/chronicle"
	"github.com/vgxness/vgxness/internal/contracts"
	"github.com/vgxness/vgxness/internal/gatekeeper"
	"github.com/vgxness/vgxness/internal/prompts"
	"github.com/vgxness/vgxness/internal/registry"
)

const (
	maxExecutionDocumentBytes = 8 << 20
	maxHealthAge              = 5 * time.Minute
)

var (
	ErrInvalidAdapter   = errors.New("invalid provider adapter")
	ErrAdapterNotFound  = errors.New("provider adapter not found")
	ErrDenied           = errors.New("provider execution denied")
	ErrApprovalRequired = errors.New("provider execution requires approval")
	ErrInvalidPacket    = errors.New("invalid execution packet")
	ErrInvalidResult    = errors.New("invalid provider result")
	ErrInvalidPrompt    = errors.New("invalid composed prompt")
)

type FailureCategory string

const (
	FailureUnavailable      FailureCategory = "unavailable"
	FailureIncompatible     FailureCategory = "incompatible"
	FailureStale            FailureCategory = "stale"
	FailurePermissionDenied FailureCategory = "permission-denied"
	FailureInvalidResult    FailureCategory = "invalid-result"
	FailureInterrupted      FailureCategory = "interrupted"
)

type Failure struct {
	Category    FailureCategory
	Recoverable bool
}

func (f *Failure) Error() string {
	if f == nil {
		return "provider failure"
	}
	return "provider failure: " + string(f.Category)
}

type Descriptor struct {
	Reference        registry.ProviderReference
	Source           registry.SourceReference
	InterfaceVersion string
	Capabilities     []registry.Capability
}

type Health struct {
	Status    gatekeeper.AdapterHealth
	CheckedAt time.Time
}

type Invocation struct {
	ExecutionID          string
	CorrelationID        string
	WorkUnitID           string
	Mode                 chronicle.TaskMode
	Operation            gatekeeper.OperationClass
	AuthorizedOperations []gatekeeper.OperationClass
	Packet               json.RawMessage
	Agent                registry.Agent
	Skills               []registry.Skill
	Prompt               prompts.Bundle
}

type Adapter interface {
	Descriptor() Descriptor
	Health(context.Context) Health
	Run(context.Context, Invocation) ([]byte, error)
}

type Request struct {
	Authorization gatekeeper.Request
	Mode          chronicle.TaskMode
	Packet        []byte
}

type Receipt struct {
	Decision     gatekeeper.Decision
	Provider     Descriptor
	Health       Health
	ExecutionID  string
	Selection    json.RawMessage
	StartedAt    time.Time
	FinishedAt   time.Time
	Result       json.RawMessage
	PromptRef    registry.PromptReference
	PromptSHA256 string
}

// Prepared is the content-bound provider invocation produced after registry,
// health, permission, and prompt validation, but before any model is executed.
// It is serializable so the OpenCode host can execute a native child session
// between prepare and accept bridge calls without creating another OpenCode
// process.
type Prepared struct {
	Invocation   Invocation
	Decision     gatekeeper.Decision
	Provider     Descriptor
	Health       Health
	Selection    json.RawMessage
	StartedAt    time.Time
	PromptRef    registry.PromptReference
	PromptSHA256 string
}

type DecisionError struct {
	Decision gatekeeper.Decision
}

func (e *DecisionError) Error() string {
	if e.Decision.Outcome == gatekeeper.Ask {
		return ErrApprovalRequired.Error()
	}
	return ErrDenied.Error()
}

func (e *DecisionError) Unwrap() error {
	if e.Decision.Outcome == gatekeeper.Ask {
		return ErrApprovalRequired
	}
	return ErrDenied
}

type registeredAdapter struct {
	adapter    Adapter
	descriptor Descriptor
}

type Runner struct {
	registry   *registry.Registry
	gatekeeper *gatekeeper.Evaluator
	composer   *prompts.Composer
	adapters   map[string]registeredAdapter
	now        func() time.Time
}

func New(entries *registry.Registry, evaluator *gatekeeper.Evaluator, composer *prompts.Composer, adapters ...Adapter) (*Runner, error) {
	if entries == nil || evaluator == nil || composer == nil {
		return nil, ErrInvalidAdapter
	}
	registered := make(map[string]registeredAdapter, len(adapters))
	for _, adapter := range adapters {
		if adapter == nil {
			return nil, ErrInvalidAdapter
		}
		descriptor, err := freezeDescriptor(adapter.Descriptor())
		if err != nil {
			return nil, err
		}
		key := providerKey(descriptor.Reference)
		if _, duplicate := registered[key]; duplicate {
			return nil, fmt.Errorf("%w: duplicate exact provider identity", ErrInvalidAdapter)
		}
		registered[key] = registeredAdapter{adapter: adapter, descriptor: descriptor}
	}
	return &Runner{registry: entries, gatekeeper: evaluator, composer: composer, adapters: registered, now: time.Now}, nil
}

func (r *Runner) Run(ctx context.Context, request Request) (Receipt, error) {
	prepared, err := r.Prepare(ctx, request)
	if err != nil {
		return Receipt{}, err
	}
	registered, ok := r.adapters[providerKey(prepared.Provider.Reference)]
	if !ok || !reflect.DeepEqual(registered.descriptor, prepared.Provider) {
		return Receipt{}, ErrAdapterNotFound
	}
	resultData, err := registered.adapter.Run(ctx, prepared.Invocation)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Receipt{}, err
		}
		var failure *Failure
		if errors.As(err, &failure) && validFailure(failure.Category) {
			return Receipt{}, failure
		}
		return Receipt{}, &Failure{Category: FailureUnavailable, Recoverable: true}
	}
	return r.Accept(ctx, prepared, resultData)
}

// Prepare validates and freezes a provider invocation without executing it.
func (r *Runner) Prepare(ctx context.Context, request Request) (Prepared, error) {
	if err := ctx.Err(); err != nil {
		return Prepared{}, err
	}
	packet, packetData, err := decodePacket(ctx, request.Packet)
	if err != nil {
		return Prepared{}, err
	}
	agent, err := r.registry.ResolveAgent(ctx, request.Authorization.AgentID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Prepared{}, err
		}
		return Prepared{}, fmt.Errorf("%w: exact agent identity", ErrInvalidPacket)
	}
	registered, ok := r.adapters[providerKey(agent.Agent.Provider)]
	if !ok {
		return Prepared{}, ErrAdapterNotFound
	}
	if err := bindPacket(request, packet, agent.Agent); err != nil {
		return Prepared{}, err
	}
	skills, err := r.resolveSkills(ctx, packet.Context.SkillRefs, request.Authorization.WorkUnit.AllowedSkillScopes)
	if err != nil {
		return Prepared{}, err
	}
	health := registered.adapter.Health(ctx)
	if err := ctx.Err(); err != nil {
		return Prepared{}, err
	}
	if health.CheckedAt.IsZero() || !validHealth(health.Status) {
		return Prepared{}, &Failure{Category: FailureInvalidResult}
	}
	now := r.now().UTC()
	if health.CheckedAt.After(now) {
		return Prepared{}, &Failure{Category: FailureInvalidResult}
	}
	if health.Status == gatekeeper.AdapterHealthy && now.Sub(health.CheckedAt) > maxHealthAge {
		health.Status = gatekeeper.AdapterStale
	}
	authorization := request.Authorization
	authorization.Adapter = gatekeeper.AdapterEvidence{
		Reference: registered.descriptor.Reference, Capabilities: cloneCapabilities(registered.descriptor.Capabilities), Health: health.Status,
	}
	decision, err := r.gatekeeper.Evaluate(ctx, authorization)
	if err != nil {
		return Prepared{}, err
	}
	if decision.Outcome != gatekeeper.Allow {
		return Prepared{}, &DecisionError{Decision: decision}
	}
	resolvedPrompt, err := r.registry.ResolvePrompt(ctx, agent.Agent.PromptRef)
	if err != nil {
		if contextError(err) {
			return Prepared{}, err
		}
		return Prepared{}, fmt.Errorf("%w: exact prompt identity", ErrInvalidPrompt)
	}
	prompt, err := r.composer.Compose(ctx, prompts.Input{
		Agent: agent.Agent, Prompt: resolvedPrompt, Skills: skills, Mode: request.Mode,
		Work: prompts.WorkContext{
			RunID: packet.Context.RunID, TaskID: packet.Context.TaskID, Phase: packet.Context.Phase, Goal: packet.Context.Goal,
			Scope: prompts.Scope{Included: packet.Context.Scope.Included, Excluded: packet.Context.Scope.Excluded}, Inputs: packet.Context.Inputs,
			AllowedPaths: packet.Context.AllowedPaths, AllowedTools: packet.Context.AllowedTools,
			AcceptanceCriteria: packet.Context.AcceptanceCriteria, ApprovalState: packet.Context.ApprovalState,
			ReturnContract: packet.Context.ReturnContract, LoopID: packet.Loop.LoopID, LoopType: packet.Loop.LoopType,
			MaxIterations: packet.Loop.MaxIterations, CurrentIteration: packet.Loop.CurrentIteration, Deadline: packet.Loop.Deadline,
		},
		Language: prompts.LanguagePolicy{
			UserFacing: packet.LanguagePolicy.UserFacing, TechnicalArtifacts: packet.LanguagePolicy.TechnicalArtifacts,
			SubagentInstructions: packet.LanguagePolicy.SubagentInstructions, ExplicitLanguage: packet.LanguagePolicy.ExplicitLanguage,
		},
	})
	if err != nil {
		if contextError(err) {
			return Prepared{}, err
		}
		return Prepared{}, ErrInvalidPrompt
	}
	startedAt := r.now().UTC()
	selection, err := buildSelection(ctx, packet.SelectionID, authorization.RequiredCapability, registered.descriptor, decision.PolicyVersion, startedAt)
	if err != nil {
		return Prepared{}, err
	}
	return Prepared{
		Invocation: Invocation{
			ExecutionID: packet.ExecutionID, CorrelationID: authorization.CorrelationID, WorkUnitID: authorization.WorkUnit.ID,
			Mode: request.Mode, Operation: authorization.Operation,
			AuthorizedOperations: append([]gatekeeper.OperationClass(nil), authorization.WorkUnit.Operations...),
			Packet:               append(json.RawMessage(nil), packetData...), Agent: agent.Agent, Skills: skills, Prompt: prompt,
		},
		Decision: decision, Provider: cloneDescriptor(registered.descriptor), Health: health,
		Selection: selection, StartedAt: startedAt, PromptRef: prompt.PromptRef, PromptSHA256: prompt.SHA256,
	}, nil
}

// Accept validates a native host result against its prepared invocation and
// produces the same provider receipt used by the legacy synchronous path.
func (r *Runner) Accept(ctx context.Context, prepared Prepared, resultData []byte) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	registered, ok := r.adapters[providerKey(prepared.Provider.Reference)]
	if !ok || !reflect.DeepEqual(registered.descriptor, prepared.Provider) || prepared.Invocation.ExecutionID == "" || prepared.Invocation.WorkUnitID == "" {
		return Receipt{}, ErrInvalidPacket
	}
	digestPrompt := prepared.Invocation.Prompt
	if digestPrompt.PromptRef != prepared.PromptRef || digestPrompt.SHA256 != prepared.PromptSHA256 || digestPrompt.System == "" {
		return Receipt{}, ErrInvalidPrompt
	}
	result, canonicalResult, err := decodeResult(ctx, resultData)
	if err != nil {
		return Receipt{}, err
	}
	if result.TaskID != prepared.Invocation.WorkUnitID || result.AgentID != prepared.Invocation.Agent.ID {
		return Receipt{}, fmt.Errorf("%w: result identity mismatch", ErrInvalidResult)
	}
	finishedAt := r.now().UTC()
	if finishedAt.Before(prepared.StartedAt) {
		finishedAt = prepared.StartedAt
	}
	return Receipt{
		Decision: prepared.Decision, Provider: cloneDescriptor(prepared.Provider), Health: prepared.Health, ExecutionID: prepared.Invocation.ExecutionID,
		Selection: append(json.RawMessage(nil), prepared.Selection...), StartedAt: prepared.StartedAt, FinishedAt: finishedAt, Result: canonicalResult,
		PromptRef: prepared.PromptRef, PromptSHA256: prepared.PromptSHA256,
	}, nil
}

func (r *Runner) resolveSkills(ctx context.Context, references []registry.ExactSkillReference, allowedScopes map[string]bool) ([]registry.Skill, error) {
	skills := make([]registry.Skill, 0, len(references))
	for _, reference := range references {
		skill, err := r.registry.ResolveSkill(ctx, reference, allowedScopes)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			return nil, fmt.Errorf("%w: unresolved skill", ErrInvalidPacket)
		}
		skills = append(skills, skill)
	}
	return skills, nil
}

type executionPacket struct {
	ExecutionID string `json:"executionId"`
	SelectionID string `json:"selectionId"`
	DecisionID  string `json:"decisionId"`
	Context     struct {
		PacketID string `json:"packetId"`
		RunID    string `json:"runId"`
		TaskID   string `json:"taskId"`
		Phase    string `json:"phase"`
		Goal     string `json:"goal"`
		Scope    struct {
			Included []string `json:"included"`
			Excluded []string `json:"excluded"`
		} `json:"scope"`
		Inputs             map[string]any                 `json:"inputs"`
		AllowedPaths       []string                       `json:"allowedPaths"`
		AllowedTools       []string                       `json:"allowedTools"`
		AcceptanceCriteria []string                       `json:"acceptanceCriteria"`
		ReturnContract     string                         `json:"returnContract"`
		SkillRefs          []registry.ExactSkillReference `json:"skillRefs"`
		ApprovalState      string                         `json:"approvalState"`
	} `json:"context"`
	Loop struct {
		LoopID           string `json:"loopId"`
		LoopType         string `json:"loopType"`
		MaxIterations    int    `json:"maxIterations"`
		CurrentIteration int    `json:"currentIteration"`
		Deadline         string `json:"deadline"`
		Terminal         bool   `json:"terminal"`
	} `json:"loop"`
	LanguagePolicy struct {
		UserFacing           string `json:"userFacing"`
		TechnicalArtifacts   string `json:"technicalArtifacts"`
		SubagentInstructions string `json:"subagentInstructions"`
		ExplicitLanguage     string `json:"explicitLanguage"`
	} `json:"languagePolicy"`
}

type agentResult struct {
	ResultID string `json:"resultId"`
	TaskID   string `json:"taskId"`
	AgentID  string `json:"agentId"`
}

func decodePacket(ctx context.Context, document []byte) (executionPacket, []byte, error) {
	if len(document) == 0 || len(document) > maxExecutionDocumentBytes {
		return executionPacket{}, nil, ErrInvalidPacket
	}
	if err := contracts.Validate(ctx, contracts.ExecutionSchemaURI+"#/$defs/executionPacket", document, false); err != nil {
		if contextError(err) {
			return executionPacket{}, nil, err
		}
		return executionPacket{}, nil, fmt.Errorf("%w: %v", ErrInvalidPacket, err)
	}
	var packet executionPacket
	if err := json.Unmarshal(document, &packet); err != nil {
		return executionPacket{}, nil, ErrInvalidPacket
	}
	if packet.Loop.Terminal || packet.Loop.CurrentIteration >= packet.Loop.MaxIterations {
		return executionPacket{}, nil, fmt.Errorf("%w: loop is terminal or exhausted", ErrInvalidPacket)
	}
	return packet, compactJSON(document), nil
}

func decodeResult(ctx context.Context, document []byte) (agentResult, json.RawMessage, error) {
	if len(document) == 0 || len(document) > maxExecutionDocumentBytes {
		return agentResult{}, nil, ErrInvalidResult
	}
	if err := contracts.Validate(ctx, contracts.ExecutionSchemaURI+"#/$defs/agentResult", document, false); err != nil {
		if contextError(err) {
			return agentResult{}, nil, err
		}
		return agentResult{}, nil, fmt.Errorf("%w: %v", ErrInvalidResult, err)
	}
	var result agentResult
	if err := json.Unmarshal(document, &result); err != nil {
		return agentResult{}, nil, ErrInvalidResult
	}
	return result, compactJSON(document), nil
}

func buildSelection(ctx context.Context, selectionID string, need registry.CapabilityNeed, descriptor Descriptor, policyVersion string, decidedAt time.Time) (json.RawMessage, error) {
	capabilities := make([]map[string]any, 0, len(descriptor.Capabilities))
	for _, capability := range descriptor.Capabilities {
		constraints := capability.Constraints
		if constraints == nil {
			constraints = map[string]any{}
		}
		capabilities = append(capabilities, map[string]any{
			"capability": capability.Capability, "version": capability.Version, "constraints": constraints,
		})
	}
	selection := map[string]any{
		"kind": "orchestrator.selection", "schemaVersion": "1", "selectionId": selectionID,
		"needs": []any{need},
		"candidates": []any{map[string]any{
			"provider": descriptor.Reference.Provider, "capabilities": capabilities, "eligible": true, "reasons": []any{"exact", "healthy", "capable", "policy-allowed"},
		}},
		"status": "selected", "selectedProvider": descriptor.Reference.Provider, "policyVersion": policyVersion,
		"rationale": "exact registered provider is healthy, capable, and policy-authorized", "decidedAt": decidedAt.Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(selection)
	if err != nil {
		return nil, fmt.Errorf("%w: encode provider selection", ErrInvalidResult)
	}
	if err := contracts.Validate(ctx, contracts.OrchestrationURI+"#/$defs/providerSelection", data, false); err != nil {
		if contextError(err) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: provider selection evidence: %v", ErrInvalidResult, err)
	}
	return compactJSON(data), nil
}

func bindPacket(request Request, packet executionPacket, agent registry.Agent) error {
	authorization := request.Authorization
	if packet.ExecutionID != authorization.CorrelationID || packet.Context.TaskID != authorization.WorkUnit.ID {
		return fmt.Errorf("%w: execution, correlation, or work-unit identity mismatch", ErrInvalidPacket)
	}
	if !sameSet(packet.Context.AllowedPaths, authorization.WorkUnit.AllowedRoots) || !subset(packet.Context.AllowedTools, authorization.WorkUnit.AllowedTools) {
		return fmt.Errorf("%w: context packet broadens work-unit scope", ErrInvalidPacket)
	}
	if !sameSet(packet.Context.Scope.Included, authorization.WorkUnit.AllowedRoots) {
		return fmt.Errorf("%w: declared context scope differs from authorized roots", ErrInvalidPacket)
	}
	if !sameSet(packet.Context.Scope.Excluded, authorization.WorkUnit.DeniedRoots) {
		return fmt.Errorf("%w: declared context exclusions differ from the work unit", ErrInvalidPacket)
	}
	if authorization.Tool != "" && !contains(packet.Context.AllowedTools, authorization.Tool) {
		return fmt.Errorf("%w: operation tool is absent from context packet", ErrInvalidPacket)
	}
	for _, reference := range packet.Context.SkillRefs {
		if !containsSkillReference(agent.SkillRefs, reference) {
			return fmt.Errorf("%w: context packet broadens skill scope", ErrInvalidPacket)
		}
	}
	if request.Mode == chronicle.TaskBackground {
		if !agent.ExecutionPolicy.MayRunBackground || !agent.ExecutionPolicy.BackgroundReadOnly || agent.ExecutionPolicy.MayDelegate || authorization.Operation != gatekeeper.ReadFiles {
			return fmt.Errorf("%w: background execution is not read-only and non-delegating", ErrInvalidPacket)
		}
	} else if request.Mode != chronicle.TaskForeground {
		return fmt.Errorf("%w: unknown task mode", ErrInvalidPacket)
	}
	if packet.Context.ApprovalState != "not-required" && packet.Context.ApprovalState != "approved" {
		return fmt.Errorf("%w: approval is not executable", ErrInvalidPacket)
	}
	if packet.Context.ApprovalState == "approved" && authorization.Approval == nil || packet.Context.ApprovalState != "approved" && authorization.Approval != nil {
		return fmt.Errorf("%w: approval state does not match authorization evidence", ErrInvalidPacket)
	}
	return nil
}

func freezeDescriptor(descriptor Descriptor) (Descriptor, error) {
	if descriptor.Reference.Provider == "" || descriptor.Reference.ID == "" || descriptor.Reference.Version == "" || descriptor.Source.Provider == "" || descriptor.Source.ID == "" || descriptor.InterfaceVersion == "" || len(descriptor.Capabilities) == 0 {
		return Descriptor{}, ErrInvalidAdapter
	}
	seen := map[string]struct{}{}
	for _, capability := range descriptor.Capabilities {
		if capability.Capability == "" || capability.Version == "" {
			return Descriptor{}, ErrInvalidAdapter
		}
		key := capability.Capability + "\x00" + capability.Version
		if _, duplicate := seen[key]; duplicate {
			return Descriptor{}, fmt.Errorf("%w: duplicate adapter capability", ErrInvalidAdapter)
		}
		seen[key] = struct{}{}
	}
	data, err := json.Marshal(descriptor)
	if err != nil {
		return Descriptor{}, ErrInvalidAdapter
	}
	var frozen Descriptor
	if json.Unmarshal(data, &frozen) != nil {
		return Descriptor{}, ErrInvalidAdapter
	}
	return frozen, nil
}

func cloneCapabilities(capabilities []registry.Capability) []registry.Capability {
	data, _ := json.Marshal(capabilities)
	var clone []registry.Capability
	_ = json.Unmarshal(data, &clone)
	return clone
}

func cloneDescriptor(descriptor Descriptor) Descriptor {
	data, _ := json.Marshal(descriptor)
	var clone Descriptor
	_ = json.Unmarshal(data, &clone)
	return clone
}

func providerKey(reference registry.ProviderReference) string {
	return reference.Provider + "\x00" + reference.ID + "\x00" + reference.Version
}

func validHealth(status gatekeeper.AdapterHealth) bool {
	switch status {
	case gatekeeper.AdapterHealthy, gatekeeper.AdapterUnavailable, gatekeeper.AdapterIncompatible, gatekeeper.AdapterStale, gatekeeper.AdapterPermissionDenied:
		return true
	default:
		return false
	}
}

func validFailure(category FailureCategory) bool {
	switch category {
	case FailureUnavailable, FailureIncompatible, FailureStale, FailurePermissionDenied, FailureInvalidResult, FailureInterrupted:
		return true
	default:
		return false
	}
}

func contextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func compactJSON(document []byte) []byte {
	var output bytes.Buffer
	if json.Compact(&output, document) != nil {
		return append([]byte(nil), document...)
	}
	return output.Bytes()
}

func subset(values, allowed []string) bool {
	for _, value := range values {
		if !contains(allowed, value) {
			return false
		}
	}
	return true
}

func sameSet(left, right []string) bool {
	return len(left) == len(right) && subset(left, right) && subset(right, left)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsSkillReference(values []registry.ExactSkillReference, target registry.ExactSkillReference) bool {
	for _, value := range values {
		if reflect.DeepEqual(value, target) {
			return true
		}
	}
	return false
}
