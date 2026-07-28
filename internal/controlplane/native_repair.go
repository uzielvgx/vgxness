package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/contracts"
)

const nativeRepairDuration = 15 * time.Minute

func nativeRepairLeasePath(root string) string {
	return filepath.Join(root, "native-repair.lease")
}

// prepareNativeRepair is deliberately smaller than Prepare. It does not open
// continuity, memory, Registry, Gatekeeper, Chronicle, or the normal native
// coordinator, so a failure in those layers cannot prevent a bounded repair.
func (service *Service) prepareNativeRepair(ctx context.Context, workspace string, input bridge.DispatchRequest) (bridge.Response, error) {
	if input.Continuity != bridge.ContinuitySingle || input.RunID != "" {
		return bridge.Response{}, bridge.ErrInvalid
	}
	root, err := canonicalWorkspace(ctx, workspace)
	if err != nil {
		return bridge.Response{}, err
	}
	if err := verifyVGXNESSRepository(ctx, root); err != nil {
		return bridge.Response{}, err
	}
	info, err := os.Lstat(root)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return bridge.Response{}, bridge.ErrDenied
	}
	workspaceID, ok := nativeFileIdentity(info)
	if !ok {
		return bridge.Response{}, bridge.ErrUnavailable
	}
	paths, err := config.Prepare(ctx, config.Options{StorageRoot: service.storageRoot, ProjectDir: root})
	if err != nil {
		return bridge.Response{}, fmt.Errorf("%w: repair storage", bridge.ErrExecution)
	}
	deadline := service.now().UTC().Add(nativeRepairDuration).Format(time.RFC3339Nano)
	if err := acquireNativeRepairLease(paths.Root, input.TicketID, deadline, service.now().UTC()); err != nil {
		return bridge.Response{}, err
	}
	leaseOwned := true
	defer func() {
		if leaseOwned {
			_ = releaseNativeLease(paths.Root, input.TicketID)
		}
	}()
	edit, err := prepareNativeEditWorkspaceMode(ctx, root, input.TicketID, false)
	if err != nil {
		return bridge.Response{}, err
	}
	editOwned := true
	defer func() {
		if editOwned {
			removeNativeEditWorkspace(root, edit)
		}
	}()
	runID, err := service.newID("repair-run")
	if err != nil {
		return bridge.Response{}, bridge.ErrExecution
	}
	taskID, err := service.newID("repair-task")
	if err != nil {
		return bridge.Response{}, bridge.ErrExecution
	}
	prompt := nativeRepairPrompt(taskID, input)
	promptSHA := nativeSHA256([]byte(prompt))
	document := nativeTicketDocument{
		SchemaVersion: nativeTicketVersion, TicketID: input.TicketID,
		Workspace: root, WorkspaceID: workspaceID, Input: input,
		RunID: runID, TaskID: taskID, Deadline: deadline, State: "prepared",
		Edit: edit,
	}
	if err := createNativeTicket(paths.Root, document); err != nil {
		return bridge.Response{}, fmt.Errorf("%w: persist native repair ticket", bridge.ErrExecution)
	}
	leaseOwned, editOwned = false, false
	return bridge.Response{
		ProtocolVersion: bridge.ProtocolVersion, OK: true, Bridge: "healthy", Provider: "opencode",
		Workspace: root, RunID: runID, TaskID: taskID, Status: "prepared",
		Prepared: &bridge.PreparedDispatch{
			TicketID: input.TicketID, ExecutionID: runID, Agent: "vgxness-maintainer", Model: input.Model,
			Prompt: prompt, PromptSHA256: promptSHA, Deadline: deadline,
			PromptRef: bridge.PromptReceipt{ID: "native-self-repair", Version: "1", SHA256: promptSHA},
		},
	}, nil
}

func verifyVGXNESSRepository(ctx context.Context, root string) error {
	data, err := runGitCommand(ctx, root, nativeGitArgs("show", "HEAD:go.mod"), cleanGitEnvironment(nil))
	if err != nil {
		return fmt.Errorf("%w: self-repair requires the VGXNESS Git repository", bridge.ErrDenied)
	}
	first, _, _ := strings.Cut(string(data), "\n")
	if strings.TrimSpace(first) != "module github.com/vgxness/vgxness" {
		return fmt.Errorf("%w: self-repair is restricted to the VGXNESS repository", bridge.ErrDenied)
	}
	return nil
}

func acquireNativeRepairLease(root, ticketID, deadline string, now time.Time) error {
	path := nativeRepairLeasePath(root)
	guard, err := acquireNativeAdmissionGuard(context.Background(), root)
	if err != nil {
		return err
	}
	defer guard.Release()
	if _, statErr := os.Lstat(path); statErr == nil {
		if !nativeLeaseReclaimable(path, now) {
			return fmt.Errorf("%w: another native repair is active", bridge.ErrDenied)
		}
		if removeErr := os.Remove(path); removeErr != nil {
			return fmt.Errorf("%w: reclaim native repair", bridge.ErrExecution)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("%w: inspect native repair lease", bridge.ErrExecution)
	}
	data, err := json.Marshal(nativeLease{
		SchemaVersion: nativeTicketVersion, TicketID: ticketID, Deadline: deadline,
	})
	if err != nil {
		return bridge.ErrExecution
	}
	return publishNativeLease(path, data)
}

func nativeRepairPrompt(taskID string, input bridge.DispatchRequest) string {
	criteria, _ := json.Marshal(input.AcceptanceCriteria)
	return fmt.Sprintf(`You are repairing the VGXNESS control plane itself through its isolated emergency maintenance channel.

Failure to diagnose:
%s

Acceptance criteria:
%s

Work only on the committed VGXNESS snapshot exposed by the ticket. Inspect exact files with vgxness_native_read, make the smallest root-cause fix with vgxness_native_edit, and validate it with vgxness_native_validate. After the final edit, successful test and vet operations are mandatory; format edited Go files first. Do not weaken ticket authentication, content digests, path restrictions, isolation, validation, or durable receipts merely to make the failure disappear. Do not use shell, Git, network, package installation, direct filesystem tools, delegation, commits, pushes, or integration. If evidence is insufficient, return blocked rather than guessing.

Return exactly one compact agent.result JSON object with kind "agent.result", schemaVersion "1", resultId "result-%s", taskId %q, agentId "vgxness-maintainer", status, summary, artifacts, nextRecommended, risks, errors, and memoryCandidates.`, input.Goal, string(criteria), taskID, taskID)
}

func (service *Service) completeNativeRepair(ctx context.Context, root string, paths config.Paths, document nativeTicketDocument, input bridge.NativeCompletionRequest) (bridge.Response, error) {
	digest := nativeCompletionDigest(input.ParentSessionID, input.ChildSessionID, input.MessageID, input.Result)
	if document.State == "completed" {
		if document.CompletionSHA == digest && document.Response != nil {
			if document.Response.EditArtifact == nil && document.Edit != nil {
				if err := removeNativeEditWorkspaceChecked(ctx, root, document.Edit); err != nil {
					return bridge.Response{}, err
				}
			}
			if err := releaseNativeLease(paths.Root, document.TicketID); err != nil {
				return bridge.Response{}, err
			}
			return *document.Response, nil
		}
		return bridge.Response{}, bridge.ErrDenied
	}
	if document.State != "prepared" || document.Input.ParentSessionID != input.ParentSessionID ||
		document.Input.ChildSessionID != input.ChildSessionID || service.now().UTC().After(parseNativeDeadline(document.Deadline)) {
		return bridge.Response{}, bridge.ErrDenied
	}
	guard, err := acquireOwnedNativeLeaseGuard(paths.Root, document.TicketID)
	if err != nil {
		return bridge.Response{}, err
	}
	defer guard.Release()
	var outcome struct {
		Kind     string `json:"kind"`
		ResultID string `json:"resultId"`
		TaskID   string `json:"taskId"`
		AgentID  string `json:"agentId"`
		Status   string `json:"status"`
	}
	if contracts.Validate(ctx, contracts.ExecutionSchemaURI+"#/$defs/agentResult", input.Result, false) != nil ||
		json.Unmarshal(input.Result, &outcome) != nil || outcome.Kind != "agent.result" ||
		outcome.ResultID != "result-"+document.TaskID || outcome.TaskID != document.TaskID ||
		outcome.AgentID != "vgxness-maintainer" {
		return bridge.Response{}, bridge.ErrDenied
	}
	var artifact *bridge.NativeEditArtifact
	if outcome.Status == "success" {
		if len(document.Edits) == 0 {
			return bridge.Response{}, fmt.Errorf("%w: native repair produced no bounded edits", bridge.ErrDenied)
		}
		if !nativeRepairValidated(document.Validations) {
			return bridge.Response{}, fmt.Errorf("%w: native repair requires successful test and vet after its latest edit", bridge.ErrDenied)
		}
		finalized, err := finalizeNativeEditArtifact(ctx, document)
		if err != nil {
			return bridge.Response{}, err
		}
		artifact = &finalized
	}
	result := append(json.RawMessage(nil), input.Result...)
	promptSHA := nativeSHA256([]byte(nativeRepairPrompt(document.TaskID, document.Input)))
	decision := "repair-isolated"
	if artifact == nil {
		decision = "repair-not-produced"
	}
	response := bridge.Response{
		ProtocolVersion: bridge.ProtocolVersion, OK: true, Bridge: "healthy", Provider: "opencode",
		Workspace: root, RunID: document.RunID, TaskID: document.TaskID, Status: "completed",
		Result: result, EditArtifact: artifact,
		Receipt: &bridge.Receipt{
			ExecutionID: document.RunID, Decision: decision, Provider: "opencode",
			Prompt:     bridge.PromptReceipt{ID: "native-self-repair", Version: "1", SHA256: promptSHA},
			FinishedAt: service.now().UTC().Format(time.RFC3339Nano),
		},
	}
	document.State, document.TerminalStatus = "completed", "completed"
	document.CompletionSHA, document.CompletionMessageID, document.Response = digest, input.MessageID, &response
	if err := writeNativeTicket(paths.Root, document); err != nil {
		return bridge.Response{}, fmt.Errorf("%w: persist native repair completion", bridge.ErrExecution)
	}
	if artifact == nil && document.Edit != nil {
		if err := removeNativeEditWorkspaceChecked(ctx, root, document.Edit); err != nil {
			return bridge.Response{}, err
		}
	}
	if err := releaseNativeLeaseLocked(paths.Root, document.TicketID); err != nil {
		return bridge.Response{}, err
	}
	return response, nil
}

func nativeRepairValidated(receipts []bridge.NativeValidationReceipt) bool {
	tested, vetted := false, false
	for _, receipt := range receipts {
		if !receipt.Success {
			continue
		}
		switch receipt.Operation {
		case bridge.NativeValidationTest:
			tested = true
		case bridge.NativeValidationVet:
			vetted = true
		}
	}
	return tested && vetted
}

func (service *Service) failNativeRepair(ctx context.Context, paths config.Paths, root string, document nativeTicketDocument, input bridge.NativeFailureRequest) (bridge.Response, error) {
	digest := nativeCompletionDigest(input.ParentSessionID, input.ChildSessionID, input.Category, nil)
	if document.State == "failed" {
		if document.CompletionSHA == digest && document.Response != nil {
			if document.Edit != nil {
				if err := removeNativeEditWorkspaceChecked(ctx, root, document.Edit); err != nil {
					return bridge.Response{}, err
				}
			}
			if err := releaseNativeLease(paths.Root, document.TicketID); err != nil {
				return bridge.Response{}, err
			}
			return *document.Response, nil
		}
		return bridge.Response{}, bridge.ErrDenied
	}
	if document.State != "prepared" || document.Input.ParentSessionID != input.ParentSessionID ||
		document.Input.ChildSessionID != input.ChildSessionID {
		return bridge.Response{}, bridge.ErrDenied
	}
	guard, err := acquireOwnedNativeLeaseGuard(paths.Root, document.TicketID)
	if err != nil {
		return bridge.Response{}, err
	}
	defer guard.Release()
	response := bridge.Response{
		ProtocolVersion: bridge.ProtocolVersion, OK: true, Bridge: "healthy", Provider: "opencode",
		Workspace: root, RunID: document.RunID, TaskID: document.TaskID, Status: "failed",
	}
	document.State, document.TerminalStatus = "failed", "failed"
	document.TerminalFailure = "native maintainer ended without a validated repair"
	document.CompletionSHA, document.Response = digest, &response
	if err := writeNativeTicket(paths.Root, document); err != nil {
		return bridge.Response{}, fmt.Errorf("%w: persist native repair failure", bridge.ErrExecution)
	}
	if document.Edit != nil {
		if err := removeNativeEditWorkspaceChecked(ctx, root, document.Edit); err != nil {
			return bridge.Response{}, err
		}
	}
	if err := releaseNativeLeaseLocked(paths.Root, document.TicketID); err != nil {
		return bridge.Response{}, err
	}
	return response, nil
}
