package controlplane

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/chronicle"
	"github.com/vgxness/vgxness/internal/codegraph"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/gatekeeper"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/orchestrator"
	"github.com/vgxness/vgxness/internal/prompts"
	"github.com/vgxness/vgxness/internal/providers"
	"github.com/vgxness/vgxness/internal/sensitivepaths"
)

const (
	nativeTicketVersion = "1"
	// Terminal ticket headroom covers a maximally valid prepared envelope plus
	// six-byte JSON escaping for every accepted result byte.
	nativeTicketLimit         = 32 << 20
	nativeTerminalHeadroom    = 6*bridge.MaxNativeResultBytes + bridge.MaxRequestBytes
	nativePreparedTicketLimit = nativeTicketLimit - nativeTerminalHeadroom
	nativeLeaseRecoveryAge    = 10 * time.Minute
	nativeMaxConcurrentLeases = 4
	nativeAdmissionWait       = 2 * time.Second
	nativeAdmissionRetry      = 2 * time.Millisecond
	nativeCodeGraphTimeout    = 30 * time.Second
	nativeMaxCodeGraphQueries = 16
)

type nativeTicketDocument struct {
	SchemaVersion       string                          `json:"schemaVersion"`
	TicketID            string                          `json:"ticketId"`
	Workspace           string                          `json:"workspace"`
	WorkspaceID         string                          `json:"workspaceIdentity"`
	Input               bridge.DispatchRequest          `json:"input"`
	RunID               string                          `json:"runId"`
	TaskID              string                          `json:"taskId"`
	Deadline            string                          `json:"deadline"`
	State               string                          `json:"state"`
	Coordinator         orchestrator.NativeTicket       `json:"coordinator"`
	Continuity          *nativeContinuityState          `json:"continuity,omitempty"`
	Memory              *nativeMemoryState              `json:"memory,omitempty"`
	CompletionSHA       string                          `json:"completionSha256,omitempty"`
	CompletionMessageID string                          `json:"completionMessageId,omitempty"`
	Response            *bridge.Response                `json:"response,omitempty"`
	CodeGraph           []bridge.NativeCodeGraphReceipt `json:"codegraph,omitempty"`
	Edit                *nativeEditWorkspace            `json:"editWorkspace,omitempty"`
	Edits               []bridge.NativeEditResult       `json:"edits,omitempty"`
	EditLifecycle       *nativeEditLifecycleDocument    `json:"editLifecycle,omitempty"`
}

type nativeEditWorkspace struct {
	Root         string `json:"root"`
	RootIdentity string `json:"rootIdentity"`
	BaseRevision string `json:"baseRevision"`
}

type nativeLease struct {
	SchemaVersion string `json:"schemaVersion"`
	TicketID      string `json:"ticketId"`
	Deadline      string `json:"deadline"`
	SharedRead    bool   `json:"sharedRead,omitempty"`
}

type nativeContinuityState struct {
	Mode              bridge.ContinuityMode `json:"mode"`
	RunID             string                `json:"runId"`
	Project           string                `json:"project"`
	Snapshot          runSnapshot           `json:"snapshot"`
	Staged            chronicle.CurrentRun  `json:"staged"`
	SelectionID       string                `json:"selectionId"`
	DecisionID        string                `json:"decisionId"`
	PreflightID       string                `json:"preflightId"`
	RetrievedMemories []memory.MemoryResult `json:"retrievedMemories,omitempty"`
}

type nativeMemoryState struct {
	Project      string   `json:"project"`
	RetrievedIDs []string `json:"retrievedIds,omitempty"`
}

type nativeDeadline struct {
	Loop struct {
		Deadline string `json:"deadline"`
	} `json:"loop"`
}

type nativeHostAdapter struct {
	descriptor providers.Descriptor
	checkedAt  time.Time
}

func (adapter nativeHostAdapter) Descriptor() providers.Descriptor { return adapter.descriptor }
func (adapter nativeHostAdapter) Health(ctx context.Context) providers.Health {
	if ctx.Err() != nil {
		return providers.Health{Status: gatekeeper.AdapterUnavailable, CheckedAt: adapter.checkedAt}
	}
	return providers.Health{Status: gatekeeper.AdapterHealthy, CheckedAt: adapter.checkedAt}
}
func (nativeHostAdapter) Run(context.Context, providers.Invocation) ([]byte, error) {
	return nil, &providers.Failure{Category: providers.FailureUnavailable}
}

// Prepare creates a durable, content-bound ticket for execution by a native
// OpenCode child session. It never starts another OpenCode process.
func (service *Service) Prepare(ctx context.Context, workspace string, input bridge.DispatchRequest) (bridge.Response, error) {
	if err := bridge.ValidateNativePrepare(input); err != nil {
		return bridge.Response{}, bridge.ErrInvalid
	}
	root, err := canonicalWorkspace(ctx, workspace)
	if err != nil {
		return bridge.Response{}, err
	}
	workspaceInfo, err := os.Lstat(root)
	if err != nil || workspaceInfo.Mode()&os.ModeSymlink != 0 || !workspaceInfo.IsDir() {
		return bridge.Response{}, bridge.ErrDenied
	}
	workspaceID, ok := nativeFileIdentity(workspaceInfo)
	if !ok {
		return bridge.Response{}, bridge.ErrUnavailable
	}
	paths, err := config.Prepare(ctx, config.Options{StorageRoot: service.storageRoot, ProjectDir: root})
	if err != nil {
		return bridge.Response{}, fmt.Errorf("%w: storage", bridge.ErrExecution)
	}
	// Recovery must run before continuity admission. A lost native prepare may
	// have left the current run in its staged/running state, which would make
	// openContinuity reject the very request that is meant to trigger recovery.
	recovered, err := service.recoverExpiredNativeLease(ctx, paths, root)
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
	executionRoot := root
	var editWorkspace *nativeEditWorkspace
	if input.Operation == bridge.WriteFiles {
		editWorkspace, err = prepareNativeEditWorkspace(ctx, root, input.TicketID)
		if err != nil {
			return bridge.Response{}, err
		}
		executionRoot = editWorkspace.Root
	}
	editWorkspaceOwned := editWorkspace != nil
	defer func() {
		if editWorkspaceOwned {
			removeNativeEditWorkspace(root, editWorkspace)
		}
	}()
	continuity, err := service.openContinuity(ctx, paths, root, input)
	if err != nil {
		if recovered != nil && recovered.CapsuleID != "" && input.Continuity == bridge.ContinuityStart && errors.Is(err, bridge.ErrDenied) {
			response := *recovered
			response.Status = "recovered"
			return response, nil
		}
		return bridge.Response{}, err
	}
	taskMemory := taskMemoryFromContinuity(continuity)
	if taskMemory == nil {
		taskMemory, err = service.openTaskMemory(ctx, root, input.Goal)
		if err != nil {
			return bridge.Response{}, err
		}
	}
	adapter, err := service.newNativeAdapter(executionRoot)
	if err != nil {
		return bridge.Response{}, err
	}
	entries, err := runtimeRegistry(ctx, executionRoot, input.Model)
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
	coordinator, err := orchestrator.New(log, runner, orchestrator.Limits{MaxIterations: 1, MaxBackground: 0, MaxDuration: 10 * time.Minute, CleanupTimeout: 5 * time.Second})
	if err != nil {
		return bridge.Response{}, fmt.Errorf("%w: coordinator", bridge.ErrExecution)
	}
	request, err := service.executionRequest(executionRoot, runID, taskID, executionID, identities, input, gitEvidence, continuity, taskMemory)
	if err != nil {
		return bridge.Response{}, err
	}
	ticketID := input.TicketID
	deadline := nativePromptDeadline(request.Packet)
	agent := nativeAgentFor(input.Operation)
	if agent == "" {
		return bridge.Response{}, bridge.ErrUnavailable
	}
	if err := acquireNativeLeaseModeContext(ctx, paths.Root, ticketID, deadline, nativeDispatchMayShareLease(input)); err != nil {
		return bridge.Response{}, err
	}
	leaseOwned := true
	defer func() {
		if leaseOwned {
			_ = releaseNativeLease(paths.Root, ticketID)
		}
	}()
	document := nativeTicketDocument{
		SchemaVersion: nativeTicketVersion, TicketID: ticketID, Workspace: root, WorkspaceID: workspaceID, Input: input,
		RunID: runID, TaskID: taskID, Deadline: deadline, State: "preparing",
		Coordinator: orchestrator.NativeTicket{RunID: runID, TaskID: taskID, Mode: request.Mode, Request: request},
		Continuity:  freezeNativeContinuity(continuity),
		Memory:      freezeNativeMemory(taskMemory),
		Edit:        editWorkspace,
	}
	if err := createNativeTicket(paths.Root, document); err != nil {
		if existing, readErr := readNativeTicket(paths.Root, ticketID); readErr == nil && sameNativeTicketIdentity(existing, document) {
			// os.Link may have published the ticket before directory sync failed.
			// Preserve its lease so Fail or expiry recovery can close that identity.
			leaseOwned = false
			editWorkspaceOwned = false
		}
		return bridge.Response{}, fmt.Errorf("%w: persist native recovery ticket", bridge.ErrExecution)
	}
	editWorkspaceOwned = false
	// From this point the durable ticket owns the foreground lease. Errors leave
	// both in place so the caller can invoke Fail or expiry recovery can finish
	// the same identity; the setup defer must no longer make it undiscoverable.
	leaseOwned = false
	if err := service.stageContinuity(ctx, continuity, input, taskID, identities.packetID, identities.loopID); err != nil {
		return bridge.Response{}, err
	}
	document.Continuity = freezeNativeContinuity(continuity)
	if err := writeNativeTicket(paths.Root, document); err != nil {
		return bridge.Response{}, fmt.Errorf("%w: persist staged native recovery ticket", bridge.ErrExecution)
	}
	ticket, receipt, err := coordinator.StartNative(ctx, request)
	if err != nil {
		return bridge.Response{}, normalizeProviderError(err)
	}
	document.State, document.Coordinator = "prepared", ticket
	if err := writeNativeTicket(paths.Root, document); err != nil {
		return bridge.Response{}, fmt.Errorf("%w: persist native ticket", bridge.ErrExecution)
	}
	return bridge.Response{
		ProtocolVersion: bridge.ProtocolVersion, OK: true, Bridge: "healthy", Provider: "opencode", Workspace: root,
		RunID: runID, TaskID: taskID, Status: string(receipt.Status),
		Prepared: &bridge.PreparedDispatch{
			TicketID: ticketID, ExecutionID: ticket.Prepared.Invocation.ExecutionID, Agent: agent, Model: input.Model,
			Prompt: ticket.Prepared.Invocation.Prompt.System, PromptSHA256: ticket.Prepared.PromptSHA256, Deadline: deadline,
			PromptRef: bridge.PromptReceipt{ID: ticket.Prepared.PromptRef.ID, Version: ticket.Prepared.PromptRef.Version, SHA256: ticket.Prepared.PromptSHA256},
		},
	}, nil
}

func (service *Service) Complete(ctx context.Context, workspace string, input bridge.NativeCompletionRequest) (bridge.Response, error) {
	if err := bridge.ValidateNativeCompletion(input); err != nil {
		return bridge.Response{}, err
	}
	root, paths, document, release, err := service.openNativeTicket(ctx, workspace, input.TicketID)
	if err != nil {
		return bridge.Response{}, err
	}
	defer release()
	digest := nativeCompletionDigest(input.ParentSessionID, input.ChildSessionID, input.MessageID, input.Result)
	if document.State == "completed" || document.State == "failed" {
		if document.CompletionSHA == digest && document.Response != nil {
			if err := releaseNativeLease(paths.Root, document.TicketID); err != nil {
				return bridge.Response{}, err
			}
			return *document.Response, nil
		}
		return bridge.Response{}, bridge.ErrDenied
	}
	if document.State != "prepared" || document.Input.ParentSessionID != input.ParentSessionID || document.Input.ChildSessionID != input.ChildSessionID || service.now().UTC().After(parseNativeDeadline(document.Deadline)) {
		return bridge.Response{}, bridge.ErrDenied
	}
	leaseGuard, err := acquireOwnedNativeLeaseGuard(paths.Root, document.TicketID)
	if err != nil {
		return bridge.Response{}, err
	}
	defer leaseGuard.Release()
	var editArtifact *bridge.NativeEditArtifact
	if document.Input.Operation == bridge.WriteFiles {
		artifact, artifactErr := finalizeNativeEditArtifact(ctx, document)
		if artifactErr != nil {
			return bridge.Response{}, artifactErr
		}
		editArtifact = &artifact
	}
	coordinator, continuity, err := service.nativeCoordinator(ctx, paths, root, document)
	if err != nil {
		return bridge.Response{}, err
	}
	taskMemory := thawNativeMemory(document.Memory, root)
	receipt, err := coordinator.CompleteNative(ctx, document.Coordinator, input.Result)
	if err != nil {
		if errors.Is(err, orchestrator.ErrDurability) {
			return bridge.Response{}, bridge.ErrDenied
		}
		continuityResult, continuityErr := service.completeContinuity(context.WithoutCancel(ctx), continuity, document.Input, document.TaskID, nil, true)
		if continuity == nil {
			continuityResult.memoryRefs, continuityErr = service.completeTaskMemory(context.WithoutCancel(ctx), taskMemory, document.Input, document.RunID, document.TaskID, nil, true)
		}
		if continuityErr != nil {
			return bridge.Response{}, errors.Join(normalizeProviderError(err), continuityErr)
		}
		response := bridge.Response{
			ProtocolVersion: bridge.ProtocolVersion, OK: true, Bridge: "healthy", Provider: "opencode", Workspace: root,
			RunID: document.RunID, TaskID: document.TaskID, CapsuleID: continuityResult.capsuleID, StateVersion: continuityResult.stateVersion,
			MemoryRefs: continuityResult.memoryRefs, Status: string(receipt.Status),
			EditArtifact: editArtifact,
		}
		document.State, document.CompletionSHA, document.CompletionMessageID, document.Response = "failed", digest, input.MessageID, &response
		if persistErr := writeNativeTicket(paths.Root, document); persistErr != nil {
			return bridge.Response{}, fmt.Errorf("%w: persist rejected native completion", bridge.ErrExecution)
		}
		if releaseErr := releaseNativeLeaseLocked(paths.Root, document.TicketID); releaseErr != nil {
			return bridge.Response{}, releaseErr
		}
		return response, nil
	}
	if receipt.Provider == nil {
		return bridge.Response{}, fmt.Errorf("%w: missing provider receipt", bridge.ErrExecution)
	}
	providerReceipt := receipt.Provider
	result := append(json.RawMessage(nil), providerReceipt.Result...)
	continuityResult, err := service.completeContinuity(context.WithoutCancel(ctx), continuity, document.Input, document.TaskID, result, false)
	if err != nil {
		return bridge.Response{}, err
	}
	if continuity == nil {
		continuityResult.memoryRefs, err = service.completeTaskMemory(context.WithoutCancel(ctx), taskMemory, document.Input, document.RunID, document.TaskID, result, false)
		if err != nil {
			return bridge.Response{}, err
		}
	}
	response := bridge.Response{
		ProtocolVersion: bridge.ProtocolVersion, OK: true, Bridge: "healthy", Provider: "opencode", Workspace: root,
		RunID: document.RunID, TaskID: document.TaskID, CapsuleID: continuityResult.capsuleID, StateVersion: continuityResult.stateVersion,
		MemoryRefs: continuityResult.memoryRefs, Status: string(receipt.Status), Result: result,
		EditArtifact: editArtifact,
		Receipt: &bridge.Receipt{
			ExecutionID: providerReceipt.ExecutionID, Decision: string(providerReceipt.Decision.Outcome), DecisionCondition: providerReceipt.Decision.Condition,
			Provider: providerReceipt.Provider.Reference.Provider, ProviderID: providerReceipt.Provider.Reference.ID, ProviderVersion: providerReceipt.Provider.Reference.Version,
			Prompt:    bridge.PromptReceipt{ID: providerReceipt.PromptRef.ID, Version: providerReceipt.PromptRef.Version, SHA256: providerReceipt.PromptSHA256},
			StartedAt: providerReceipt.StartedAt.UTC().Format(time.RFC3339Nano), FinishedAt: providerReceipt.FinishedAt.UTC().Format(time.RFC3339Nano), EventCount: len(receipt.Events) + 1,
		},
	}
	document.State, document.CompletionSHA, document.CompletionMessageID, document.Response = "completed", digest, input.MessageID, &response
	if err := writeNativeTicket(paths.Root, document); err != nil {
		return bridge.Response{}, fmt.Errorf("%w: persist native completion", bridge.ErrExecution)
	}
	if err := releaseNativeLeaseLocked(paths.Root, document.TicketID); err != nil {
		return bridge.Response{}, err
	}
	return response, nil
}

func (service *Service) Fail(ctx context.Context, workspace string, input bridge.NativeFailureRequest) (bridge.Response, error) {
	if err := bridge.ValidateNativeFailure(input); err != nil {
		return bridge.Response{}, err
	}
	root, paths, document, release, err := service.openNativeTicket(ctx, workspace, input.TicketID)
	if err != nil {
		return bridge.Response{}, err
	}
	defer release()
	digest := nativeCompletionDigest(input.ParentSessionID, input.ChildSessionID, input.Category, nil)
	if document.State == "failed" {
		if document.CompletionSHA == digest && document.Response != nil {
			if err := releaseNativeLease(paths.Root, document.TicketID); err != nil {
				return bridge.Response{}, err
			}
			return *document.Response, nil
		}
		return bridge.Response{}, bridge.ErrDenied
	}
	if document.State != "prepared" && document.State != "preparing" || document.Input.ParentSessionID != input.ParentSessionID || document.Input.ChildSessionID != input.ChildSessionID {
		return bridge.Response{}, bridge.ErrDenied
	}
	leaseGuard, err := acquireOwnedNativeLeaseGuard(paths.Root, document.TicketID)
	if err != nil {
		return bridge.Response{}, err
	}
	defer leaseGuard.Release()
	coordinator, continuity, err := service.nativeCoordinator(ctx, paths, root, document)
	if err != nil {
		return bridge.Response{}, err
	}
	taskMemory := thawNativeMemory(document.Memory, root)
	state, started, err := nativeChronicleTaskState(ctx, paths.Root, document)
	if err != nil {
		return bridge.Response{}, err
	}
	receipt := orchestrator.Receipt{Status: chronicle.TaskFailed}
	switch {
	case !started && document.State == "preparing":
		// The caller owns the recovery identity before StartNative. An immediate
		// prepare failure can therefore reach Fail while Chronicle has no task.
	case !started:
		return bridge.Response{}, fmt.Errorf("%w: prepared native ticket has no started task", orchestrator.ErrDurability)
	case state.Status == chronicle.TaskRunning:
		receipt, err = coordinator.FailNative(ctx, document.Coordinator, input.Category)
		if err != nil {
			return bridge.Response{}, err
		}
	case state.Status == chronicle.TaskFailed:
		digest, digestErr := nativeChronicleFailureDigest(ctx, paths.Root, document)
		if digestErr != nil {
			return bridge.Response{}, digestErr
		}
		if digest != "" {
			receipt, err = coordinator.FailNative(ctx, document.Coordinator, input.Category)
			if err != nil {
				return bridge.Response{}, err
			}
		}
	default:
		return bridge.Response{}, fmt.Errorf("%w: native failure task is already %s", orchestrator.ErrDurability, state.Status)
	}
	continuityResult := continuityOutcome{}
	if started {
		continuityResult, err = service.completeContinuity(context.WithoutCancel(ctx), continuity, document.Input, document.TaskID, nil, true)
	} else {
		continuityResult, err = service.completeUnstartedContinuity(context.WithoutCancel(ctx), continuity, document.Input, document.TaskID)
	}
	if err != nil {
		return bridge.Response{}, err
	}
	if continuity == nil {
		continuityResult.memoryRefs, err = service.completeTaskMemory(context.WithoutCancel(ctx), taskMemory, document.Input, document.RunID, document.TaskID, nil, true)
		if err != nil {
			return bridge.Response{}, err
		}
	}
	response := bridge.Response{
		ProtocolVersion: bridge.ProtocolVersion, OK: true, Bridge: "healthy", Provider: "opencode", Workspace: root,
		RunID: document.RunID, TaskID: document.TaskID, CapsuleID: continuityResult.capsuleID, StateVersion: continuityResult.stateVersion,
		MemoryRefs: continuityResult.memoryRefs, Status: string(receipt.Status),
	}
	document.State, document.CompletionSHA, document.Response = "failed", digest, &response
	if err := writeNativeTicket(paths.Root, document); err != nil {
		return bridge.Response{}, fmt.Errorf("%w: persist native failure", bridge.ErrExecution)
	}
	if err := releaseNativeLeaseLocked(paths.Root, document.TicketID); err != nil {
		return bridge.Response{}, err
	}
	return response, nil
}

// ReadNative serves one bounded file read to the child session that owns a
// prepared read-files ticket. Content never crosses OpenCode's built-in read
// boundary; the actual open is traversal-resistant and rejects aliases.
func (service *Service) ReadNative(ctx context.Context, workspace string, input bridge.NativeReadRequest) (bridge.Response, error) {
	if err := bridge.ValidateNativeRead(input); err != nil {
		return bridge.Response{}, err
	}
	root, paths, document, release, err := service.openNativeTicket(ctx, workspace, input.TicketID)
	if err != nil {
		return bridge.Response{}, err
	}
	defer release()
	if document.State != "prepared" || document.Input.Operation != bridge.ReadFiles && document.Input.Operation != bridge.AnalyzeStructure && document.Input.Operation != bridge.WriteFiles || document.Input.ChildSessionID != input.ChildSessionID || service.now().UTC().After(parseNativeDeadline(document.Deadline)) || sensitivepaths.IsSensitive(input.Path) {
		return bridge.Response{}, bridge.ErrDenied
	}
	leaseGuard, err := acquireOwnedNativeLeaseGuard(paths.Root, document.TicketID)
	if err != nil {
		return bridge.Response{}, err
	}
	defer leaseGuard.Release()
	readRoot, readIdentity := root, document.WorkspaceID
	if document.Input.Operation == bridge.WriteFiles {
		if document.Edit == nil {
			return bridge.Response{}, bridge.ErrDenied
		}
		readRoot, readIdentity = document.Edit.Root, document.Edit.RootIdentity
	}
	read, err := secureNativeRead(readRoot, readIdentity, input)
	if err != nil {
		return bridge.Response{}, err
	}
	return bridge.Response{
		ProtocolVersion: bridge.ProtocolVersion, OK: true, Bridge: "healthy", Provider: "opencode", Workspace: root,
		RunID: document.RunID, TaskID: document.TaskID, Status: "reading", Read: &read,
	}, nil
}

// QueryNativeCodeGraph serves one bounded structural query to the child session
// that owns a prepared analyze-structure ticket. The child never receives
// process, MCP, lifecycle, or index administration access.
func (service *Service) QueryNativeCodeGraph(ctx context.Context, workspace string, input bridge.NativeCodeGraphRequest) (bridge.Response, error) {
	if err := bridge.ValidateNativeCodeGraph(input); err != nil {
		return bridge.Response{}, err
	}
	root, paths, document, release, err := service.openNativeTicket(ctx, workspace, input.TicketID)
	if err != nil {
		return bridge.Response{}, err
	}
	defer release()
	if document.State != "prepared" || document.Input.Operation != bridge.AnalyzeStructure || document.Input.ChildSessionID != input.ChildSessionID || service.now().UTC().After(parseNativeDeadline(document.Deadline)) {
		return bridge.Response{}, bridge.ErrDenied
	}
	if len(document.CodeGraph) >= nativeMaxCodeGraphQueries {
		return bridge.Response{}, bridge.ErrDenied
	}
	workspaceInfo, err := os.Lstat(root)
	if err != nil || !workspaceInfo.IsDir() || workspaceInfo.Mode()&os.ModeSymlink != 0 {
		return bridge.Response{}, bridge.ErrDenied
	}
	workspaceID, ok := nativeFileIdentity(workspaceInfo)
	if !ok || workspaceID != document.WorkspaceID {
		return bridge.Response{}, bridge.ErrDenied
	}
	leaseGuard, err := acquireOwnedNativeLeaseGuard(paths.Root, document.TicketID)
	if err != nil {
		return bridge.Response{}, err
	}
	defer leaseGuard.Release()
	runtime, err := service.newCodeGraph()
	if err != nil {
		return bridge.Response{}, err
	}
	queryContext, cancel := context.WithTimeout(ctx, nativeCodeGraphTimeout)
	defer cancel()
	result, err := runtime.Query(queryContext, root, codegraph.Request{
		Operation: codegraph.Operation(input.Operation), Query: input.Query, Symbol: input.Symbol,
		Files: append([]string(nil), input.Files...), Depth: input.Depth, MaxFiles: input.MaxFiles,
	})
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return bridge.Response{}, err
		case errors.Is(err, codegraph.ErrUnavailable):
			return bridge.Response{}, bridge.ErrUnavailable
		case errors.Is(err, codegraph.ErrInvalid):
			return bridge.Response{}, bridge.ErrInvalid
		default:
			return bridge.Response{}, fmt.Errorf("%w: bounded CodeGraph query", bridge.ErrExecution)
		}
	}
	requestData, err := json.Marshal(input)
	if err != nil {
		return bridge.Response{}, bridge.ErrExecution
	}
	inputDigest := sha256.Sum256(requestData)
	receipt := bridge.NativeCodeGraphReceipt{
		Operation: input.Operation, InputSHA256: "sha256-" + hex.EncodeToString(inputDigest[:]),
		OutputSHA256: result.OutputSHA256, StartedAt: result.StartedAt.UTC().Format(time.RFC3339Nano),
		FinishedAt: result.FinishedAt.UTC().Format(time.RFC3339Nano),
	}
	document.CodeGraph = append(document.CodeGraph, receipt)
	if err := writeNativeTicket(paths.Root, document); err != nil {
		return bridge.Response{}, fmt.Errorf("%w: persist CodeGraph receipt", bridge.ErrExecution)
	}
	return bridge.Response{
		ProtocolVersion: bridge.ProtocolVersion, OK: true, Bridge: "healthy", Provider: "opencode", Workspace: root,
		RunID: document.RunID, TaskID: document.TaskID, Status: "analyzing",
		CodeGraph: &bridge.NativeCodeGraphResult{
			Operation: input.Operation, Format: result.Format, Content: result.Content, OutputSHA256: result.OutputSHA256,
			StartedAt: result.StartedAt.UTC().Format(time.RFC3339Nano), FinishedAt: result.FinishedAt.UTC().Format(time.RFC3339Nano),
		},
	}, nil
}

func (service *Service) nativeCoordinator(ctx context.Context, paths config.Paths, root string, document nativeTicketDocument) (*orchestrator.Coordinator, *continuityState, error) {
	adapter, err := service.newNativeAdapter(root)
	if err != nil {
		return nil, nil, err
	}
	entries, err := runtimeRegistry(ctx, root, document.Input.Model)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: registry", bridge.ErrExecution)
	}
	evaluator, err := gatekeeper.New(entries, gatekeeper.Policy{Version: "bridge-balanced-v1", Profile: gatekeeper.ProfileBalanced})
	if err != nil {
		return nil, nil, fmt.Errorf("%w: gatekeeper", bridge.ErrExecution)
	}
	runner, err := providers.New(entries, evaluator, prompts.New(), adapter)
	if err != nil {
		return nil, nil, normalizeProviderError(err)
	}
	log, err := chronicle.NewEventLog(paths.Root, document.RunID)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: chronicle", bridge.ErrExecution)
	}
	coordinator, err := orchestrator.New(log, runner, orchestrator.Limits{MaxIterations: 1, MaxBackground: 0, MaxDuration: 10 * time.Minute, CleanupTimeout: 5 * time.Second})
	if err != nil {
		return nil, nil, fmt.Errorf("%w: coordinator", bridge.ErrExecution)
	}
	continuity, err := thawNativeContinuity(ctx, paths, root, document.Continuity, document.TaskID)
	return coordinator, continuity, err
}

func freezeNativeContinuity(state *continuityState) *nativeContinuityState {
	if state == nil {
		return nil
	}
	return &nativeContinuityState{
		Mode: state.mode, RunID: state.runID, Project: state.project, Snapshot: state.snapshot, Staged: state.staged,
		SelectionID: state.selectionID, DecisionID: state.decisionID, PreflightID: state.preflightID,
		RetrievedMemories: append([]memory.MemoryResult(nil), state.retrievedMemories...),
	}
}

func freezeNativeMemory(state *taskMemoryState) *nativeMemoryState {
	if state == nil {
		return nil
	}
	return &nativeMemoryState{Project: state.project, RetrievedIDs: memoryIDs(state.retrievedMemories)}
}

func thawNativeMemory(frozen *nativeMemoryState, workspace string) *taskMemoryState {
	if frozen == nil {
		return nil
	}
	items := make([]memory.MemoryResult, 0, len(frozen.RetrievedIDs))
	for _, id := range frozen.RetrievedIDs {
		items = append(items, memory.MemoryResult{ID: id, Project: frozen.Project, Scope: memory.ScopeProject})
	}
	return &taskMemoryState{project: frozen.Project, workspace: workspace, retrievedMemories: items}
}

func thawNativeContinuity(ctx context.Context, paths config.Paths, workspace string, frozen *nativeContinuityState, taskID string) (*continuityState, error) {
	if frozen == nil {
		return nil, nil
	}
	store, err := chronicle.NewSnapshotStore(paths.Root, frozen.RunID)
	if err != nil {
		return nil, fmt.Errorf("%w: continuity store", bridge.ErrExecution)
	}
	log, err := chronicle.NewEventLog(paths.Root, frozen.RunID)
	if err != nil {
		return nil, fmt.Errorf("%w: continuity log", bridge.ErrExecution)
	}
	state := &continuityState{
		mode: frozen.Mode, runID: frozen.RunID, project: frozen.Project, workspace: workspace, store: store, log: log,
		snapshot: frozen.Snapshot, staged: frozen.Staged, selectionID: frozen.SelectionID, decisionID: frozen.DecisionID,
		preflightID: frozen.PreflightID, retrievedMemories: append([]memory.MemoryResult(nil), frozen.RetrievedMemories...),
	}
	if state.staged.ID != "" {
		return state, nil
	}
	// A provisional ticket exists before continuity staging begins. Absence of a
	// matching current pointer means the new phase never became active, so there
	// is no snapshot or event log to recover or complete.
	current, present, err := chronicle.ReadCurrent(ctx, paths.CurrentRun)
	if err != nil {
		return nil, fmt.Errorf("%w: inspect provisional native continuity", bridge.ErrExecution)
	}
	if !present || current.ID != frozen.RunID || current.TaskID != taskID {
		return nil, nil
	}
	recovered, err := store.Recover(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: recover provisional native continuity", bridge.ErrExecution)
	}
	if !recovered.CurrentPresent || recovered.Current.ID != current.ID || recovered.Current.TaskID != current.TaskID {
		return nil, fmt.Errorf("%w: provisional native continuity changed during recovery", orchestrator.ErrDurability)
	}
	if err := json.Unmarshal(recovered.Run, &state.snapshot); err != nil {
		return nil, fmt.Errorf("%w: decode provisional native continuity", bridge.ErrExecution)
	}
	state.staged = recovered.Current
	return state, nil
}

func sameNativeTicketIdentity(left, right nativeTicketDocument) bool {
	return left.SchemaVersion == right.SchemaVersion && left.TicketID == right.TicketID && left.Workspace == right.Workspace &&
		left.RunID == right.RunID && left.TaskID == right.TaskID && left.Input.ParentSessionID == right.Input.ParentSessionID &&
		left.Input.ChildSessionID == right.Input.ChildSessionID && sameNativeEditIdentity(left.Edit, right.Edit)
}

func sameNativeEditIdentity(left, right *nativeEditWorkspace) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Root == right.Root && left.RootIdentity == right.RootIdentity && left.BaseRevision == right.BaseRevision
}

func nativeAgentFor(operation bridge.Operation) string {
	switch operation {
	case bridge.ReviewChanges:
		return "vgxness-reviewer"
	case bridge.WriteFiles:
		return "vgxness-implementer"
	default:
		return "vgxness-explorer"
	}
}

func (service *Service) newNativeAdapter(workspace string) (providers.Adapter, error) {
	adapter, err := service.newAdapter(workspace)
	if err != nil {
		return nil, err
	}
	return nativeHostAdapter{descriptor: adapter.Descriptor(), checkedAt: service.now().UTC()}, nil
}

func nativePromptDeadline(packet []byte) string {
	var value nativeDeadline
	if json.Unmarshal(packet, &value) == nil && value.Loop.Deadline != "" {
		return value.Loop.Deadline
	}
	return time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339Nano)
}

func parseNativeDeadline(value string) time.Time {
	deadline, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return deadline
}

func nativeCompletionDigest(parts ...any) string {
	hash := sha256.New()
	for _, part := range parts {
		data, _ := json.Marshal(part)
		_, _ = hash.Write(data)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func nativeTicketDirectory(root string) string { return filepath.Join(root, "native-tickets") }

// nativeLeasePath preserves the original unsuffixed slot-zero location so
// existing installations and recovery tests remain compatible with the pool.
func nativeLeasePath(root string) string { return nativeLeaseSlotPath(root, 0) }

func nativeLeaseSlotPath(root string, slot int) string {
	if slot == 0 {
		return filepath.Join(root, "native-foreground.lease")
	}
	return filepath.Join(root, fmt.Sprintf("native-foreground.%d.lease", slot))
}

func nativeLeasePaths(root string) []string {
	paths := make([]string, nativeMaxConcurrentLeases)
	for slot := range paths {
		paths[slot] = nativeLeaseSlotPath(root, slot)
	}
	return paths
}

func nativeAdmissionGuardPath(root string) string {
	return filepath.Join(root, "native-foreground.admission.guard")
}

func acquireNativeAdmissionGuard(ctx context.Context, root string) (orchestrator.FileLock, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return orchestrator.FileLock{}, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, nativeAdmissionWait)
	defer cancel()
	for {
		guard, err := orchestrator.AcquireFileLock(nativeAdmissionGuardPath(root))
		if err == nil {
			if err := ctx.Err(); err != nil {
				guard.Release()
				return orchestrator.FileLock{}, err
			}
			return guard, nil
		}
		if !errors.Is(err, orchestrator.ErrCoordinatorBusy) {
			return orchestrator.FileLock{}, fmt.Errorf("%w: reserve native dispatch admission", bridge.ErrExecution)
		}
		timer := time.NewTimer(nativeAdmissionRetry)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return orchestrator.FileLock{}, ctx.Err()
			}
			return orchestrator.FileLock{}, fmt.Errorf("%w: native dispatch admission is busy", bridge.ErrDenied)
		case <-timer.C:
		}
	}
}

func acquireNativeLease(root, ticketID, deadline string) error {
	return acquireNativeLeaseMode(root, ticketID, deadline, false)
}

func nativeDispatchMayShareLease(input bridge.DispatchRequest) bool {
	return (input.Operation == bridge.ReadFiles || input.Operation == bridge.AnalyzeStructure) && input.Continuity == bridge.ContinuitySingle
}

func acquireNativeLeaseMode(root, ticketID, deadline string, sharedRead bool) error {
	return acquireNativeLeaseModeContext(context.Background(), root, ticketID, deadline, sharedRead)
}

func acquireNativeLeaseModeContext(ctx context.Context, root, ticketID, deadline string, sharedRead bool) error {
	data, err := json.Marshal(nativeLease{SchemaVersion: nativeTicketVersion, TicketID: ticketID, Deadline: deadline, SharedRead: sharedRead})
	if err != nil {
		return bridge.ErrExecution
	}
	admissionGuard, err := acquireNativeAdmissionGuard(ctx, root)
	if err != nil {
		return err
	}
	defer admissionGuard.Release()
	if !sharedRead {
		for _, path := range nativeLeasePaths(root) {
			if _, statErr := os.Lstat(path); statErr == nil {
				return fmt.Errorf("%w: another native dispatch is active", bridge.ErrDenied)
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return fmt.Errorf("%w: reserve native dispatch", bridge.ErrExecution)
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := publishNativeLease(nativeLeasePath(root), data); err != nil {
			if errors.Is(err, bridge.ErrDenied) {
				return fmt.Errorf("%w: another native dispatch is active", bridge.ErrDenied)
			}
			return err
		}
		return nil
	}
	for _, path := range nativeLeasePaths(root) {
		if _, statErr := os.Lstat(path); errors.Is(statErr, os.ErrNotExist) {
			if err := ctx.Err(); err != nil {
				return err
			}
			if publishErr := publishNativeLease(path, data); publishErr == nil {
				return nil
			} else if errors.Is(publishErr, bridge.ErrDenied) {
				lease, readErr := readNativeLeasePath(path)
				if readErr != nil {
					return fmt.Errorf("%w: active native dispatch lease is unreadable", bridge.ErrDenied)
				}
				if !lease.SharedRead {
					return fmt.Errorf("%w: exclusive native dispatch is active", bridge.ErrDenied)
				}
				continue
			} else {
				return publishErr
			}
		} else if statErr != nil {
			return fmt.Errorf("%w: reserve native dispatch", bridge.ErrExecution)
		}
		lease, readErr := readNativeLeasePath(path)
		if readErr != nil {
			return fmt.Errorf("%w: active native dispatch lease is unreadable", bridge.ErrDenied)
		}
		if !lease.SharedRead {
			return fmt.Errorf("%w: exclusive native dispatch is active", bridge.ErrDenied)
		}
	}
	return fmt.Errorf("%w: native dispatch capacity exhausted", bridge.ErrDenied)
}

func nativeLeaseReclaimable(path string, now time.Time) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 4096 {
		return false
	}
	data, readErr := os.ReadFile(path)
	var lease nativeLease
	if readErr == nil && json.Unmarshal(data, &lease) == nil && lease.SchemaVersion == nativeTicketVersion && lease.TicketID != "" {
		deadline := parseNativeDeadline(lease.Deadline)
		if !deadline.IsZero() {
			return now.After(deadline)
		}
	}
	return now.After(info.ModTime().Add(nativeLeaseRecoveryAge))
}

func publishNativeLease(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".native-lease-*.tmp")
	if err != nil {
		return fmt.Errorf("%w: reserve native dispatch", bridge.ErrExecution)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("%w: reserve native dispatch", bridge.ErrExecution)
	}
	_, writeErr := temporary.Write(data)
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return fmt.Errorf("%w: reserve native dispatch", bridge.ErrExecution)
	}
	if err := os.Link(temporaryPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%w: another native dispatch is active", bridge.ErrDenied)
		}
		return fmt.Errorf("%w: reserve native dispatch", bridge.ErrExecution)
	}
	if err := syncNativeDirectory(filepath.Dir(path)); err != nil {
		return fmt.Errorf("%w: reserve native dispatch", bridge.ErrExecution)
	}
	return nil
}

func readNativeLeasePath(path string) (nativeLease, error) {
	before, err := os.Lstat(path)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > 4096 {
		return nativeLease{}, fmt.Errorf("%w: native dispatch lease", bridge.ErrDenied)
	}
	file, err := os.Open(path)
	if err != nil {
		return nativeLease{}, fmt.Errorf("%w: native dispatch lease", bridge.ErrDenied)
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() || after.Size() <= 0 || after.Size() > 4096 {
		return nativeLease{}, fmt.Errorf("%w: native dispatch lease", bridge.ErrDenied)
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) == 0 || len(data) > 4096 {
		return nativeLease{}, fmt.Errorf("%w: native dispatch lease", bridge.ErrDenied)
	}
	var lease nativeLease
	if json.Unmarshal(data, &lease) != nil || lease.SchemaVersion != nativeTicketVersion || lease.TicketID == "" {
		return nativeLease{}, fmt.Errorf("%w: native dispatch lease", bridge.ErrDenied)
	}
	return lease, nil
}

func findNativeLease(root, ticketID string) (string, bool, error) {
	for _, path := range nativeLeasePaths(root) {
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return "", false, fmt.Errorf("%w: native dispatch lease", bridge.ErrDenied)
		}
		lease, err := readNativeLeasePath(path)
		if err != nil {
			continue
		}
		if lease.TicketID == ticketID {
			return path, true, nil
		}
	}
	return "", false, nil
}

func verifyNativeLease(root, ticketID string) error {
	_, present, err := findNativeLease(root, ticketID)
	if err != nil || !present {
		return fmt.Errorf("%w: native dispatch lease", bridge.ErrDenied)
	}
	return nil
}

func acquireOwnedNativeLeaseGuard(root, ticketID string) (orchestrator.FileLock, error) {
	path, present, err := findNativeLease(root, ticketID)
	if err != nil || !present {
		return orchestrator.FileLock{}, fmt.Errorf("%w: reserve native dispatch lease", bridge.ErrDenied)
	}
	guard, err := orchestrator.AcquireFileLock(path + ".guard")
	if err != nil {
		return orchestrator.FileLock{}, fmt.Errorf("%w: reserve native dispatch lease", bridge.ErrDenied)
	}
	lease, verifyErr := readNativeLeasePath(path)
	if verifyErr != nil || lease.TicketID != ticketID {
		guard.Release()
		return orchestrator.FileLock{}, fmt.Errorf("%w: native dispatch lease", bridge.ErrDenied)
	}
	return guard, nil
}

func releaseNativeLease(root, ticketID string) error {
	path, present, err := findNativeLease(root, ticketID)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	guard, err := orchestrator.AcquireFileLock(path + ".guard")
	if err != nil {
		return fmt.Errorf("%w: release native dispatch lease", bridge.ErrExecution)
	}
	defer guard.Release()
	return releaseNativeLeaseLocked(root, ticketID)
}

func releaseNativeLeaseLocked(root, ticketID string) error {
	admissionGuard, err := acquireNativeAdmissionGuard(context.Background(), root)
	if err != nil {
		return fmt.Errorf("%w: release native dispatch admission", bridge.ErrExecution)
	}
	defer admissionGuard.Release()
	path, present, err := findNativeLease(root, ticketID)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	lease, err := readNativeLeasePath(path)
	if err != nil {
		return err
	}
	if lease.TicketID != ticketID {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: release native dispatch lease", bridge.ErrExecution)
	}
	if err := syncNativeDirectory(filepath.Dir(path)); err != nil {
		return fmt.Errorf("%w: release native dispatch lease", bridge.ErrExecution)
	}
	return nil
}

func (service *Service) recoverExpiredNativeLease(ctx context.Context, paths config.Paths, root string) (*bridge.Response, error) {
	var recovered *bridge.Response
	var recoveryErr error
	for _, leasePath := range nativeLeasePaths(paths.Root) {
		response, err := service.recoverExpiredNativeLeasePath(ctx, paths, root, leasePath)
		if err != nil {
			recoveryErr = errors.Join(recoveryErr, err)
			continue
		}
		if response != nil && (recovered == nil || response.CapsuleID != "") {
			recovered = response
		}
	}
	if recoveryErr != nil {
		return nil, recoveryErr
	}
	return recovered, nil
}

func (service *Service) recoverExpiredNativeLeasePath(ctx context.Context, paths config.Paths, root, leasePath string) (*bridge.Response, error) {
	if _, err := os.Lstat(leasePath); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil || !nativeLeaseReclaimable(leasePath, service.now().UTC()) {
		return nil, nil
	}
	lease, err := readNativeLeasePath(leasePath)
	if err != nil {
		leaseGuard, lockErr := orchestrator.AcquireFileLock(leasePath + ".guard")
		if lockErr != nil {
			if errors.Is(lockErr, orchestrator.ErrCoordinatorBusy) {
				return nil, nil
			}
			return nil, fmt.Errorf("%w: guard unreadable native lease", bridge.ErrExecution)
		}
		defer leaseGuard.Release()
		admissionGuard, admissionErr := acquireNativeAdmissionGuard(ctx, paths.Root)
		if admissionErr != nil {
			return nil, fmt.Errorf("%w: reclaim unreadable native lease admission", bridge.ErrExecution)
		}
		defer admissionGuard.Release()
		if !nativeLeaseReclaimable(leasePath, service.now().UTC()) {
			return nil, nil
		}
		if removeErr := os.Remove(leasePath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: reclaim unreadable native lease", bridge.ErrExecution)
		}
		if syncErr := syncNativeDirectory(filepath.Dir(leasePath)); syncErr != nil {
			return nil, fmt.Errorf("%w: reclaim unreadable native lease", bridge.ErrExecution)
		}
		return nil, nil
	}
	ticketPath, err := nativeTicketPath(paths.Root, lease.TicketID)
	if err != nil {
		return nil, bridge.ErrDenied
	}
	ticketLock, err := orchestrator.AcquireFileLock(ticketPath + ".lock")
	if err != nil {
		if errors.Is(err, orchestrator.ErrCoordinatorBusy) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: recover native ticket", bridge.ErrDenied)
	}
	defer ticketLock.Release()
	leaseGuard, err := orchestrator.AcquireFileLock(leasePath + ".guard")
	if err != nil {
		if errors.Is(err, orchestrator.ErrCoordinatorBusy) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: recover native lease", bridge.ErrDenied)
	}
	defer leaseGuard.Release()
	currentLease, err := readNativeLeasePath(leasePath)
	if err != nil || currentLease.TicketID != lease.TicketID || !nativeLeaseReclaimable(leasePath, service.now().UTC()) {
		return nil, nil
	}
	document, err := readNativeTicket(paths.Root, lease.TicketID)
	if err != nil {
		admissionGuard, admissionErr := acquireNativeAdmissionGuard(ctx, paths.Root)
		if admissionErr != nil {
			return nil, fmt.Errorf("%w: reclaim orphan native lease admission", bridge.ErrExecution)
		}
		defer admissionGuard.Release()
		if removeErr := os.Remove(leasePath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: reclaim orphan native lease", bridge.ErrExecution)
		}
		if syncErr := syncNativeDirectory(filepath.Dir(leasePath)); syncErr != nil {
			return nil, fmt.Errorf("%w: reclaim orphan native lease", bridge.ErrExecution)
		}
		return nil, nil
	}
	if document.State == "completed" || document.State == "failed" {
		if err := releaseNativeLeaseLocked(paths.Root, document.TicketID); err != nil {
			return nil, err
		}
		return document.Response, nil
	}
	if document.State != "preparing" && document.State != "prepared" || document.Workspace != root {
		return nil, bridge.ErrDenied
	}
	cleanup := context.WithoutCancel(ctx)
	coordinator, continuity, err := service.nativeCoordinator(cleanup, paths, root, document)
	if err != nil {
		return nil, err
	}
	state, started, err := nativeChronicleTaskState(cleanup, paths.Root, document)
	if err != nil {
		return nil, err
	}
	receipt := orchestrator.Receipt{Status: chronicle.TaskFailed}
	category := "native-subagent-deadline"
	switch {
	case !started && document.State == "preparing":
		// The durable recovery ticket is published before StartNative. If the
		// host crashed in that window, there is deliberately no terminal task
		// event to append: Chronicle forbids terminal-without-start histories.
	case !started:
		return nil, fmt.Errorf("%w: prepared native ticket has no started task", orchestrator.ErrDurability)
	case state.Status == chronicle.TaskRunning:
		receipt, err = coordinator.FailNative(cleanup, document.Coordinator, category)
		if err != nil {
			return nil, err
		}
	case state.Status != chronicle.TaskFailed:
		return nil, fmt.Errorf("%w: native recovery task is already %s", orchestrator.ErrDurability, state.Status)
	}
	continuityResult := continuityOutcome{}
	if started {
		continuityResult, err = service.completeContinuity(cleanup, continuity, document.Input, document.TaskID, nil, true)
	} else {
		continuityResult, err = service.completeUnstartedContinuity(cleanup, continuity, document.Input, document.TaskID)
	}
	if err != nil {
		return nil, err
	}
	response := bridge.Response{
		ProtocolVersion: bridge.ProtocolVersion, OK: true, Bridge: "healthy", Provider: "opencode", Workspace: root,
		RunID: document.RunID, TaskID: document.TaskID, CapsuleID: continuityResult.capsuleID, StateVersion: continuityResult.stateVersion,
		MemoryRefs: continuityResult.memoryRefs, Status: string(receipt.Status),
	}
	document.State = "failed"
	document.CompletionSHA = nativeCompletionDigest(document.Input.ParentSessionID, document.Input.ChildSessionID, category, nil)
	document.Response = &response
	if err := writeNativeTicket(paths.Root, document); err != nil {
		return nil, fmt.Errorf("%w: persist recovered native failure", bridge.ErrExecution)
	}
	if err := releaseNativeLeaseLocked(paths.Root, document.TicketID); err != nil {
		return nil, err
	}
	return &response, nil
}

func nativeChronicleTaskState(ctx context.Context, storageRoot string, document nativeTicketDocument) (chronicle.TaskEventState, bool, error) {
	log, err := chronicle.NewEventLog(storageRoot, document.RunID)
	if err != nil {
		return chronicle.TaskEventState{}, false, fmt.Errorf("%w: recover native chronicle", bridge.ErrExecution)
	}
	events, err := log.Read(ctx)
	if err != nil {
		return chronicle.TaskEventState{}, false, fmt.Errorf("%w: read native recovery chronicle", bridge.ErrExecution)
	}
	states, err := chronicle.DeriveTaskStates(events)
	if err != nil {
		return chronicle.TaskEventState{}, false, fmt.Errorf("%w: derive native recovery state", orchestrator.ErrDurability)
	}
	state, present := states[document.TaskID]
	return state, present, nil
}

func nativeChronicleFailureDigest(ctx context.Context, storageRoot string, document nativeTicketDocument) (string, error) {
	log, err := chronicle.NewEventLog(storageRoot, document.RunID)
	if err != nil {
		return "", fmt.Errorf("%w: inspect native failure chronicle", bridge.ErrExecution)
	}
	events, err := log.Read(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: read native failure chronicle", bridge.ErrExecution)
	}
	for _, event := range events {
		if event.Type != "task.failed" && event.Type != "background.failed" {
			continue
		}
		var refs struct {
			TaskID  string `json:"taskId"`
			Failure struct {
				Digest string `json:"digest"`
			} `json:"failure"`
		}
		if json.Unmarshal(event.Raw, &refs) == nil && refs.TaskID == document.TaskID {
			return refs.Failure.Digest, nil
		}
	}
	return "", fmt.Errorf("%w: failed native task lacks terminal evidence", orchestrator.ErrDurability)
}

func secureNativeRead(workspace, expectedIdentity string, request bridge.NativeReadRequest) (bridge.NativeReadResult, error) {
	beforeRoot, err := os.Lstat(workspace)
	if err != nil {
		return bridge.NativeReadResult{}, bridge.ErrDenied
	}
	identity, identityOK := nativeFileIdentity(beforeRoot)
	if beforeRoot.Mode()&os.ModeSymlink != 0 || !beforeRoot.IsDir() || !identityOK || identity != expectedIdentity {
		return bridge.NativeReadResult{}, bridge.ErrDenied
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return bridge.NativeReadResult{}, fmt.Errorf("%w: open workspace root", bridge.ErrExecution)
	}
	defer func() { _ = root.Close() }()
	openedRoot, err := root.Stat(".")
	openedIdentity, openedIdentityOK := nativeFileIdentity(openedRoot)
	if err != nil || !os.SameFile(beforeRoot, openedRoot) || !openedIdentityOK || openedIdentity != expectedIdentity {
		return bridge.NativeReadResult{}, bridge.ErrDenied
	}
	parts := strings.Split(request.Path, string(filepath.Separator))
	for _, component := range parts[:len(parts)-1] {
		before, statErr := root.Lstat(component)
		if statErr != nil || before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
			return bridge.NativeReadResult{}, bridge.ErrDenied
		}
		next, openErr := root.OpenRoot(component)
		if openErr != nil {
			return bridge.NativeReadResult{}, bridge.ErrDenied
		}
		opened, openedErr := next.Stat(".")
		if openedErr != nil || !os.SameFile(before, opened) {
			next.Close()
			return bridge.NativeReadResult{}, bridge.ErrDenied
		}
		root.Close()
		root = next
	}
	name := parts[len(parts)-1]
	before, err := root.Lstat(name)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || !nativeSingleLink(before) {
		return bridge.NativeReadResult{}, bridge.ErrDenied
	}
	file, err := root.Open(name)
	if err != nil {
		return bridge.NativeReadResult{}, bridge.ErrDenied
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) || !opened.Mode().IsRegular() || !nativeSingleLink(opened) {
		return bridge.NativeReadResult{}, bridge.ErrDenied
	}
	if _, err := file.Seek(request.Offset, io.SeekStart); err != nil {
		return bridge.NativeReadResult{}, bridge.ErrDenied
	}
	limit := request.Limit
	if limit == 0 {
		limit = bridge.MaxNativeReadBytes
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+utf8.UTFMax))
	if err != nil {
		return bridge.NativeReadResult{}, bridge.ErrDenied
	}
	truncated := len(data) > limit
	end := 0
	for end < len(data) && (!truncated || end < limit) {
		runeValue, size := utf8.DecodeRune(data[end:])
		if runeValue == utf8.RuneError && size == 1 {
			return bridge.NativeReadResult{}, bridge.ErrDenied
		}
		if truncated && end+size > limit {
			break
		}
		end += size
	}
	if !truncated && end != len(data) {
		return bridge.NativeReadResult{}, bridge.ErrDenied
	}
	if truncated && end == 0 {
		return bridge.NativeReadResult{}, bridge.ErrDenied
	}
	data = data[:end]
	result := bridge.NativeReadResult{Path: request.Path, Content: string(data), Truncated: truncated}
	if truncated {
		result.NextOffset = request.Offset + int64(len(data))
	}
	return result, nil
}

func nativeTicketPath(root, ticketID string) (string, error) {
	if ticketID == "" || filepath.Base(ticketID) != ticketID || strings.Contains(ticketID, string(filepath.Separator)) {
		return "", bridge.ErrInvalid
	}
	return filepath.Join(nativeTicketDirectory(root), ticketID+".json"), nil
}

func createNativeTicket(root string, document nativeTicketDocument) error {
	directory := nativeTicketDirectory(root)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(directory); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return bridge.ErrExecution
	}
	path, err := nativeTicketPath(root, document.TicketID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(document)
	if err != nil || len(data) > nativeTicketWriteLimit(document.State) {
		return bridge.ErrExecution
	}
	file, err := os.CreateTemp(directory, ".native-ticket-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := file.Name()
	defer os.Remove(temporaryPath)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return bridge.ErrExecution
	}
	if err := os.Link(temporaryPath, path); err != nil {
		return err
	}
	return syncNativeDirectory(directory)
}

func readNativeTicket(root, ticketID string) (nativeTicketDocument, error) {
	path, err := nativeTicketPath(root, ticketID)
	if err != nil {
		return nativeTicketDocument{}, err
	}
	data, err := readBoundedControlPlaneFile(path, nativeTicketLimit)
	if err != nil {
		return nativeTicketDocument{}, bridge.ErrDenied
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document nativeTicketDocument
	if decoder.Decode(&document) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		document.SchemaVersion != nativeTicketVersion || document.TicketID != ticketID {
		return nativeTicketDocument{}, bridge.ErrDenied
	}
	return document, nil
}

func readBoundedControlPlaneFile(path string, limit int64) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > limit {
		return nil, bridge.ErrDenied
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, bridge.ErrDenied
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) || !opened.Mode().IsRegular() {
		return nil, bridge.ErrDenied
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	after, statErr := os.Lstat(path)
	if err != nil || statErr != nil || int64(len(data)) == 0 || int64(len(data)) > limit ||
		after.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, after) {
		return nil, bridge.ErrDenied
	}
	return data, nil
}

func writeNativeTicket(root string, document nativeTicketDocument) error {
	path, err := nativeTicketPath(root, document.TicketID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(document)
	if err != nil || len(data) > nativeTicketWriteLimit(document.State) {
		return bridge.ErrExecution
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".native-ticket-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return syncNativeDirectory(filepath.Dir(path))
}

func syncNativeDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func nativeTicketWriteLimit(state string) int {
	if state == "preparing" || state == "prepared" {
		return nativePreparedTicketLimit
	}
	return nativeTicketLimit
}

func (service *Service) openNativeTicket(ctx context.Context, workspace, ticketID string) (string, config.Paths, nativeTicketDocument, func(), error) {
	root, err := canonicalWorkspace(ctx, workspace)
	if err != nil {
		return "", config.Paths{}, nativeTicketDocument{}, nil, err
	}
	paths, err := config.PathsFor(config.Options{StorageRoot: service.storageRoot, ProjectDir: root})
	if err != nil {
		return "", config.Paths{}, nativeTicketDocument{}, nil, err
	}
	lockPath, err := nativeTicketPath(paths.Root, ticketID)
	if err != nil {
		return "", config.Paths{}, nativeTicketDocument{}, nil, err
	}
	lockPath += ".lock"
	lock, err := acquireBoundedControlPlaneLock(ctx, lockPath)
	if err != nil {
		if errors.Is(err, orchestrator.ErrCoordinatorBusy) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return "", config.Paths{}, nativeTicketDocument{}, nil, bridge.ErrUnavailable
		}
		return "", config.Paths{}, nativeTicketDocument{}, nil, bridge.ErrDenied
	}
	release := lock.Release
	document, err := readNativeTicket(paths.Root, ticketID)
	if err != nil || document.Workspace != root {
		release()
		if err == nil {
			err = bridge.ErrDenied
		}
		return "", config.Paths{}, nativeTicketDocument{}, nil, err
	}
	return root, paths, document, release, nil
}

func acquireBoundedControlPlaneLock(ctx context.Context, path string) (orchestrator.FileLock, error) {
	waitCtx, cancel := context.WithTimeout(ctx, nativeAdmissionWait)
	defer cancel()
	for {
		lock, err := orchestrator.AcquireFileLock(path)
		if err == nil || !errors.Is(err, orchestrator.ErrCoordinatorBusy) {
			return lock, err
		}
		timer := time.NewTimer(nativeAdmissionRetry)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return orchestrator.FileLock{}, waitCtx.Err()
		case <-timer.C:
		}
	}
}

var _ bridge.NativeRuntime = (*Service)(nil)
