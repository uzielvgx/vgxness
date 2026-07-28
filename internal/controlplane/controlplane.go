package controlplane

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/chronicle"
	"github.com/vgxness/vgxness/internal/codegraph"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/gatekeeper"
	"github.com/vgxness/vgxness/internal/orchestrator"
	"github.com/vgxness/vgxness/internal/prompts"
	"github.com/vgxness/vgxness/internal/providers"
	"github.com/vgxness/vgxness/internal/providers/opencode"
	"github.com/vgxness/vgxness/internal/registry"
)

const (
	agentID              = "vgxness-worker"
	capabilityID         = "bounded-execution"
	registryTime         = "2026-07-21T00:00:00Z"
	returnContract       = "https://vgxness.dev/schemas/execution.schema.json#/$defs/agentResult"
	defaultStatusTimeout = 15 * time.Second
)

type AdapterFactory func(string) (providers.Adapter, error)
type CodeGraphFactory func() (codegraph.Runtime, error)

type GitEvidence struct {
	StatusShort  string `json:"statusShort"`
	WorktreeDiff string `json:"worktreeDiff"`
	StagedDiff   string `json:"stagedDiff"`
}

type GitInspector func(context.Context, string) (GitEvidence, error)

type GitBaselineEvidence struct {
	Head     string `json:"head"`
	Branch   string `json:"branch,omitempty"`
	Clean    bool   `json:"clean"`
	Detached bool   `json:"detached"`
}

type GitBaselineInspector func(context.Context, string) (GitBaselineEvidence, error)

// ContinuityFault is a test seam for failures between durable completion steps.
// Production callers should leave it nil.
type ContinuityFault func(string) error

type Options struct {
	StorageRoot      string
	AdapterFactory   AdapterFactory
	CodeGraphFactory CodeGraphFactory
	GitInspector     GitInspector
	GitBaseline      GitBaselineInspector
	Memory           MemoryRuntime
	Now              func() time.Time
	NewID            func(string) (string, error)
	StatusTimeout    time.Duration
	ContinuityFault  ContinuityFault
}

type Service struct {
	storageRoot     string
	adapter         AdapterFactory
	codegraph       CodeGraphFactory
	now             func() time.Time
	newID           func(string) (string, error)
	inspectGit      GitInspector
	inspectBaseline GitBaselineInspector
	memory          MemoryRuntime
	statusTimeout   time.Duration
	continuityFault ContinuityFault
}

func New(options Options) *Service {
	factory := options.AdapterFactory
	if factory == nil {
		factory = openCodeAdapter
	}
	codegraphFactory := options.CodeGraphFactory
	if codegraphFactory == nil {
		codegraphFactory = func() (codegraph.Runtime, error) { return codegraph.New("") }
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	newID := options.NewID
	if newID == nil {
		newID = randomID
	}
	inspectGit := options.GitInspector
	if inspectGit == nil {
		inspectGit = collectGitEvidence
	}
	inspectBaseline := options.GitBaseline
	if inspectBaseline == nil {
		inspectBaseline = collectGitBaselineEvidence
	}
	statusTimeout := options.StatusTimeout
	if statusTimeout <= 0 {
		statusTimeout = defaultStatusTimeout
	}
	return &Service{storageRoot: options.StorageRoot, adapter: factory, codegraph: codegraphFactory, now: now, newID: newID, inspectGit: inspectGit, inspectBaseline: inspectBaseline, memory: options.Memory, statusTimeout: statusTimeout, continuityFault: options.ContinuityFault}
}

func (service *Service) Status(ctx context.Context, workspace string) (bridge.Response, error) {
	root, err := canonicalWorkspace(ctx, workspace)
	if err != nil {
		return bridge.Response{}, err
	}
	adapter, err := service.newAdapter(root)
	if err != nil {
		return bridge.Response{}, err
	}
	healthContext, cancelHealth := context.WithTimeout(ctx, service.statusTimeout)
	defer cancelHealth()
	health := adapter.Health(healthContext)
	if err := healthContext.Err(); err != nil {
		return bridge.Response{}, err
	}
	response := bridge.Response{
		ProtocolVersion: bridge.ProtocolVersion, OK: health.Status == gatekeeper.AdapterHealthy,
		Bridge: string(health.Status), Provider: "opencode", Workspace: root, Status: string(health.Status),
	}
	if !response.OK {
		failure := bridge.Failure(healthError(health.Status))
		response.Error = &failure
	}
	return response, nil
}

func (service *Service) Dispatch(ctx context.Context, workspace string, input bridge.DispatchRequest) (bridge.Response, error) {
	if err := bridge.ValidateDispatch(input); err != nil {
		return bridge.Response{}, err
	}
	root, err := canonicalWorkspace(ctx, workspace)
	if err != nil {
		return bridge.Response{}, err
	}
	var gitEvidence *GitEvidence
	if input.Operation == bridge.ReviewChanges {
		if service == nil || service.inspectGit == nil {
			return bridge.Response{}, bridge.ErrUnavailable
		}
		evidence, inspectErr := service.inspectGit(ctx, root)
		if inspectErr != nil {
			return bridge.Response{}, fmt.Errorf("%w: bounded Git inspection", bridge.ErrExecution)
		}
		gitEvidence = &evidence
	}
	repositoryEvidence := service.repositoryBaseline(ctx, root)
	paths, err := config.Prepare(ctx, config.Options{StorageRoot: service.storageRoot, ProjectDir: root})
	if err != nil {
		return bridge.Response{}, fmt.Errorf("%w: storage", bridge.ErrExecution)
	}
	continuity, err := service.openContinuity(ctx, paths, root, input)
	if err != nil {
		return bridge.Response{}, err
	}
	taskMemory := taskMemoryFromContinuity(continuity)
	if taskMemory == nil {
		taskMemory, err = service.openTaskMemory(ctx, root, input.Goal)
		if err != nil {
			return bridge.Response{}, err
		}
	}
	adapter, err := service.newAdapter(root)
	if err != nil {
		return bridge.Response{}, err
	}
	entries, err := runtimeRegistry(ctx, root, input.Model)
	if err != nil {
		return bridge.Response{}, fmt.Errorf("%w: registry", bridge.ErrExecution)
	}
	evaluator, err := gatekeeper.New(entries, gatekeeper.Policy{Version: "bridge-balanced-v1", Profile: gatekeeper.ProfileBalanced})
	if err != nil {
		return bridge.Response{}, fmt.Errorf("%w: gatekeeper", bridge.ErrExecution)
	}
	runner, err := providers.New(entries, evaluator, prompts.New(), adapter)
	if err != nil {
		return bridge.Response{}, normalizeProviderError(err)
	}
	runID := ""
	if continuity != nil {
		runID = continuity.runID
	} else if runID, err = service.newID("run"); err != nil {
		return bridge.Response{}, fmt.Errorf("%w: run identity", bridge.ErrExecution)
	}
	taskID, err := service.newID("task")
	if err != nil {
		return bridge.Response{}, fmt.Errorf("%w: task identity", bridge.ErrExecution)
	}
	executionID, err := service.newID("execution")
	if err != nil {
		return bridge.Response{}, fmt.Errorf("%w: execution identity", bridge.ErrExecution)
	}
	identities, err := service.executionIdentities(continuity)
	if err != nil {
		return bridge.Response{}, err
	}
	log := (*chronicle.EventLog)(nil)
	if continuity != nil {
		log = continuity.log
	} else if log, err = chronicle.NewEventLog(paths.Root, runID); err != nil {
		return bridge.Response{}, fmt.Errorf("%w: chronicle", bridge.ErrExecution)
	}
	coordinator, err := orchestrator.New(log, runner, orchestrator.Limits{
		MaxIterations: 1, MaxBackground: 0, MaxDuration: 10 * time.Minute, CleanupTimeout: 5 * time.Second,
	})
	if err != nil {
		return bridge.Response{}, fmt.Errorf("%w: coordinator", bridge.ErrExecution)
	}
	request, err := service.executionRequest(root, runID, taskID, executionID, identities, input, gitEvidence, repositoryEvidence, continuity, taskMemory)
	if err != nil {
		return bridge.Response{}, err
	}
	if err := service.stageContinuity(ctx, continuity, input, taskID, identities.packetID, identities.loopID); err != nil {
		return bridge.Response{}, err
	}
	receipt, err := coordinator.Run(ctx, request)
	if err != nil {
		if continuity != nil {
			if _, persistErr := service.completeContinuity(context.WithoutCancel(ctx), continuity, input, taskID, nil, true); persistErr != nil {
				return bridge.Response{}, errors.Join(normalizeProviderError(err), persistErr)
			}
		} else if _, persistErr := service.completeTaskMemory(context.WithoutCancel(ctx), taskMemory, input, runID, taskID, nil, true); persistErr != nil {
			return bridge.Response{}, errors.Join(normalizeProviderError(err), persistErr)
		}
		return bridge.Response{}, normalizeProviderError(err)
	}
	if receipt.Provider == nil {
		return bridge.Response{}, fmt.Errorf("%w: missing provider receipt", bridge.ErrExecution)
	}
	providerReceipt := receipt.Provider
	result := append(json.RawMessage(nil), providerReceipt.Result...)
	continuityResult, err := service.completeContinuity(context.WithoutCancel(ctx), continuity, input, taskID, result, false)
	if err != nil {
		return bridge.Response{}, err
	}
	if continuity == nil {
		continuityResult.memoryRefs, err = service.completeTaskMemory(context.WithoutCancel(ctx), taskMemory, input, runID, taskID, result, false)
		if err != nil {
			return bridge.Response{}, err
		}
	}
	return bridge.Response{
		ProtocolVersion: bridge.ProtocolVersion, OK: true, Bridge: "healthy", Provider: "opencode", Workspace: root,
		RunID: runID, TaskID: taskID, CapsuleID: continuityResult.capsuleID, StateVersion: continuityResult.stateVersion, MemoryRefs: continuityResult.memoryRefs,
		Status: string(receipt.Status), Result: result,
		Receipt: &bridge.Receipt{
			ExecutionID: providerReceipt.ExecutionID, Decision: string(providerReceipt.Decision.Outcome),
			DecisionCondition: providerReceipt.Decision.Condition, Provider: providerReceipt.Provider.Reference.Provider,
			ProviderID: providerReceipt.Provider.Reference.ID, ProviderVersion: providerReceipt.Provider.Reference.Version,
			Prompt:    bridge.PromptReceipt{ID: providerReceipt.PromptRef.ID, Version: providerReceipt.PromptRef.Version, SHA256: providerReceipt.PromptSHA256},
			StartedAt: providerReceipt.StartedAt.UTC().Format(time.RFC3339Nano), FinishedAt: providerReceipt.FinishedAt.UTC().Format(time.RFC3339Nano),
			EventCount: len(receipt.Events),
		},
	}, nil
}

func (service *Service) newAdapter(workspace string) (providers.Adapter, error) {
	if service == nil || service.adapter == nil || service.now == nil || service.newID == nil {
		return nil, bridge.ErrUnavailable
	}
	adapter, err := service.adapter(workspace)
	if err != nil || adapter == nil {
		return nil, bridge.ErrUnavailable
	}
	return adapter, nil
}

func (service *Service) newCodeGraph() (codegraph.Runtime, error) {
	if service == nil || service.codegraph == nil {
		return nil, bridge.ErrUnavailable
	}
	runtime, err := service.codegraph()
	if err != nil || runtime == nil {
		return nil, bridge.ErrUnavailable
	}
	return runtime, nil
}

func (service *Service) repositoryBaseline(ctx context.Context, workspace string) *GitBaselineEvidence {
	if service == nil || service.inspectBaseline == nil {
		return nil
	}
	evidence, err := service.inspectBaseline(ctx, workspace)
	if err != nil {
		// A workspace need not be a Git repository for bounded read operations.
		// Write preparation independently requires and validates a clean HEAD.
		return nil
	}
	return &evidence
}

func (service *Service) executionRequest(workspace, runID, taskID, executionID string, identities executionIDs, input bridge.DispatchRequest, gitEvidence *GitEvidence, repositoryEvidence *GitBaselineEvidence, continuity *continuityState, taskMemory *taskMemoryState) (providers.Request, error) {
	operation := effectiveOperation(input.Operation)
	operations := []gatekeeper.OperationClass{gatekeeper.ReadFiles}
	if operation != gatekeeper.ReadFiles {
		operations = append(operations, operation)
	}
	tools := []string{}
	criteria := append([]string(nil), input.AcceptanceCriteria...)
	if len(criteria) == 0 {
		criteria = []string{"Return a valid structured agent result describing the bounded outcome."}
	}
	deadline := service.now().UTC().Add(10 * time.Minute).Format(time.RFC3339Nano)
	inputs := map[string]any{"operation": input.Operation}
	if gitEvidence != nil {
		inputs["git"] = gitEvidence
	}
	if repositoryEvidence != nil {
		inputs["repository"] = repositoryEvidence
	}
	if continuity != nil {
		continuityInput := map[string]any{
			"mode": continuity.mode, "runId": runID, "stateVersion": len(continuity.snapshot.Capsules) + 1,
		}
		if len(continuity.previousCapsule) > 0 {
			continuityInput["previousCapsule"] = continuity.previousCapsule
		}
		inputs["continuity"] = continuityInput
	}
	if taskMemory != nil {
		inputs["memoryContext"] = memoryContext(taskMemory.retrievedMemories)
	}
	packet := map[string]any{
		"kind": "execution.packet", "schemaVersion": "1", "executionId": executionID, "selectionId": identities.selectionID, "decisionId": identities.decisionID,
		"context": map[string]any{
			"kind": "context.packet", "schemaVersion": "1", "packetId": identities.packetID, "runId": runID, "taskId": taskID,
			"phase": "apply", "goal": strings.TrimSpace(input.Goal), "scope": map[string]any{"included": []string{workspace}, "excluded": []string{}},
			"inputs": inputs, "allowedPaths": []string{workspace}, "allowedTools": tools,
			"artifactRefs": []any{}, "skillRefs": []any{}, "acceptanceCriteria": criteria, "approvalState": "not-required", "returnContract": returnContract,
		},
		"loop":           map[string]any{"kind": "loop.control", "schemaVersion": "1", "loopId": identities.loopID, "loopType": "agent", "maxIterations": 1, "currentIteration": 0, "deadline": deadline, "terminal": false},
		"languagePolicy": map[string]any{"kind": "language.policy", "schemaVersion": "1", "userFacing": "match-user", "technicalArtifacts": "english", "subagentInstructions": "english"},
	}
	data, err := json.Marshal(packet)
	if err != nil {
		return providers.Request{}, fmt.Errorf("%w: execution packet", bridge.ErrExecution)
	}
	risk := gatekeeper.RiskLow
	if operation != gatekeeper.ReadFiles {
		risk = gatekeeper.RiskMedium
	}
	return providers.Request{
		Mode: chronicle.TaskForeground, Packet: data,
		Authorization: gatekeeper.Request{
			AgentID: agentID, RequiredCapability: registry.CapabilityNeed{Capability: capabilityID, Version: "1"},
			WorkUnit:  gatekeeper.WorkUnit{ID: taskID, Active: true, AllowedRoots: []string{workspace}, AllowedTools: tools, Operations: operations, RiskCeiling: gatekeeper.RiskMedium, ContextHash: executionID},
			Operation: operation, Path: pathFor(operation, workspace), Risk: risk, CorrelationID: executionID,
		},
	}, nil
}

func runtimeRegistry(ctx context.Context, workspace, model string) (*registry.Registry, error) {
	prompt := registry.Prompt{
		SchemaVersion: "1", ID: "vgxness-bounded-execution", Version: "1", Audience: "subagent",
		Instructions: "Execute only the exact bounded operation and workspace scope supplied by VGXNESS. For review-changes, use only the pre-collected status and diffs in context.inputs.git and do not run Git or shell commands or read files. Treat paths listed only as untracked in statusShort as unreviewed and report them as requiring explicit follow-up authorization. Do not delegate, install packages, commit, push, use the network, change permissions, or access secrets. Return exactly one JSON object conforming to the required agent.result contract; report blockers instead of broadening scope.",
		Provenance:   json.RawMessage(`{"producer":"vgxness","createdAt":"` + registryTime + `"}`),
	}
	agent := registry.Agent{
		SchemaVersion: "1", ID: agentID, Name: "VGXNESS Native Subagent", Role: "execute", Mode: "executor",
		Model:    model,
		Provider: registry.ProviderReference{Provider: "opencode", ID: "primary", Version: "1"}, Hidden: true,
		Capabilities: []registry.Capability{{Capability: capabilityID, Version: "1"}}, SkillRefs: []registry.ExactSkillReference{},
		Permissions: registry.Permissions{
			MayReadFiles: true, MayWriteFiles: true, MayRunCommands: false, AllowedTools: []string{}, AllowedPaths: []string{workspace},
			DeniedTools: []string{"task", "skill", "webfetch", "websearch"},
		},
		ExecutionPolicy: registry.ExecutionPolicy{ForegroundSequential: true, MayRunBackground: false, BackgroundReadOnly: true, MayDelegate: false},
		Provenance:      json.RawMessage(`{"producer":"vgxness","createdAt":"` + registryTime + `"}`),
		PromptRef:       registry.PromptReference{Kind: "prompt.reference", SchemaVersion: "1", ID: prompt.ID, Version: prompt.Version, Checksum: registry.PromptChecksum(prompt)},
	}
	agents, _ := json.Marshal(map[string]any{"schemaVersion": "1", "version": "bridge-agents-v1", "generatedAt": registryTime, "agents": []registry.Agent{agent}})
	skills, _ := json.Marshal(map[string]any{"schemaVersion": "1", "version": "bridge-skills-v1", "generatedAt": registryTime, "sourceRoots": []registry.SkillSource{}, "skills": []registry.Skill{}})
	promptsJSON, _ := json.Marshal(map[string]any{"schemaVersion": "1", "version": "bridge-prompts-v1", "generatedAt": registryTime, "prompts": []registry.Prompt{prompt}})
	return registry.New(ctx, agents, skills, promptsJSON)
}

func openCodeAdapter(workspace string) (providers.Adapter, error) {
	return opencode.New(opencode.Config{
		Reference: registry.ProviderReference{Provider: "opencode", ID: "primary", Version: "1"}, InterfaceVersion: "1",
		Capabilities: []registry.Capability{{Capability: capabilityID, Version: "1"}}, WorkingDirectory: workspace,
	})
}

func canonicalWorkspace(ctx context.Context, workspace string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(workspace) == "" || !filepath.IsAbs(workspace) {
		return "", bridge.ErrInvalid
	}
	cleaned := filepath.Clean(workspace)
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return "", bridge.ErrInvalid
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", bridge.ErrInvalid
	}
	return filepath.Clean(resolved), nil
}

func healthError(health gatekeeper.AdapterHealth) error {
	if health == gatekeeper.AdapterIncompatible {
		return bridge.ErrIncompatible
	}
	return bridge.ErrUnavailable
}

func normalizeProviderError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, providers.ErrDenied) || errors.Is(err, providers.ErrApprovalRequired) {
		return bridge.ErrDenied
	}
	var failure *providers.Failure
	if errors.As(err, &failure) {
		if failure.Category == providers.FailureIncompatible {
			return bridge.ErrIncompatible
		}
		if failure.Category == providers.FailurePermissionDenied {
			return bridge.ErrDenied
		}
		return bridge.ErrUnavailable
	}
	return fmt.Errorf("%w: %v", bridge.ErrExecution, err)
}

func pathFor(operation gatekeeper.OperationClass, workspace string) string {
	if operation == gatekeeper.ReadFiles || operation == gatekeeper.WriteFiles {
		return workspace
	}
	return ""
}

func effectiveOperation(operation bridge.Operation) gatekeeper.OperationClass {
	if operation == bridge.WriteFiles {
		return gatekeeper.WriteFiles
	}
	return gatekeeper.ReadFiles
}

func randomID(prefix string) (string, error) {
	var data [12]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(data[:]), nil
}
