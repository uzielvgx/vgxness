package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/config"
)

func TestNativeRepairBypassesDirtyCheckoutAndNormalLeaseButRequiresFreshValidation(t *testing.T) {
	workspace := nativeEditRepository(t)
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module github.com/vgxness/vgxness\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nativeEditGit(t, workspace, "add", "go.mod")
	nativeEditGit(t, workspace, "commit", "--quiet", "-m", "identify vgxness")
	if err := os.WriteFile(filepath.Join(workspace, "dirty.txt"), []byte("user change\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	storage := filepath.Join(t.TempDir(), "storage")
	service := New(Options{StorageRoot: storage, Now: func() time.Time { return now }})
	paths, err := config.Prepare(context.Background(), config.Options{StorageRoot: storage, ProjectDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if err := acquireNativeLease(paths.Root, "ticket-stuck", now.Add(time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = releaseNativeLease(paths.Root, "ticket-stuck") })

	prepared, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-repair", Model: "openai/gpt-5.6-sol",
		Operation: bridge.RepairSystem, Goal: "Service.Prepare failed before normal writes could start",
		AcceptanceCriteria: []string{"repair the root cause and validate it"},
		ParentSessionID:    "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Prepared == nil || prepared.Prepared.Agent != "vgxness-maintainer" {
		t.Fatalf("repair did not select the maintainer: %#v", prepared)
	}
	if _, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-repair-concurrent", Model: "openai/gpt-5.6-sol",
		Operation: bridge.RepairSystem, Goal: "second repair", ParentSessionID: "ses_parent",
		ParentMessageID: "msg_parent", ChildSessionID: "ses_other",
	}); !errors.Is(err, bridge.ErrDenied) {
		t.Fatalf("concurrent emergency repair was accepted: %v", err)
	}
	document, err := readNativeTicket(storage, "ticket-repair")
	if err != nil || document.Edit == nil {
		t.Fatalf("repair ticket was not isolated: %#v err=%v", document.Edit, err)
	}
	t.Cleanup(func() { removeNativeEditWorkspace(workspace, document.Edit) })

	read, err := service.ReadNative(context.Background(), workspace, bridge.NativeReadRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-repair", ChildSessionID: "ses_child", Path: "README.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	edit, err := service.EditNative(context.Background(), workspace, bridge.NativeEditRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-repair", ChildSessionID: "ses_child",
		Path: "README.md", Content: "repair one\n", ExpectedSHA256: read.Read.SHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	validateRepair(t, service, workspace, bridge.NativeValidationTest)
	validateRepair(t, service, workspace, bridge.NativeValidationVet)

	if _, err := service.EditNative(context.Background(), workspace, bridge.NativeEditRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-repair", ChildSessionID: "ses_child",
		Path: "README.md", Content: "repair final\n", ExpectedSHA256: edit.Edit.SHA256,
	}); err != nil {
		t.Fatal(err)
	}
	result := nativeRepairResult(t, prepared.TaskID, "success")
	_, err = service.Complete(context.Background(), workspace, bridge.NativeCompletionRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-repair", ParentSessionID: "ses_parent",
		ChildSessionID: "ses_child", MessageID: "msg_child", Result: result,
	})
	if !errors.Is(err, bridge.ErrDenied) {
		t.Fatalf("repair completed with stale validation: %v", err)
	}

	validateRepair(t, service, workspace, bridge.NativeValidationTest)
	validateRepair(t, service, workspace, bridge.NativeValidationVet)
	completed, err := service.Complete(context.Background(), workspace, bridge.NativeCompletionRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-repair", ParentSessionID: "ses_parent",
		ChildSessionID: "ses_child", MessageID: "msg_child", Result: result,
	})
	if err != nil || completed.Status != "completed" || completed.EditArtifact == nil ||
		len(completed.EditArtifact.Changes) != 1 || completed.EditArtifact.Changes[0].Path != "README.md" {
		t.Fatalf("validated repair did not produce an artifact: %#v err=%v", completed, err)
	}
	inspected, err := service.InspectNativeEdit(context.Background(), workspace, bridge.NativeEditInspectRequest{TicketID: "ticket-repair"})
	if err != nil || inspected.Artifact.ManifestSHA != completed.EditArtifact.ManifestSHA {
		t.Fatalf("repair artifact did not enter the explicit delivery lifecycle: %#v err=%v", inspected, err)
	}
	if data, err := os.ReadFile(filepath.Join(workspace, "README.md")); err != nil || string(data) != "base\n" {
		t.Fatalf("repair changed the canonical checkout: %q err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "dirty.txt")); err != nil {
		t.Fatalf("repair disturbed the user's dirty checkout: %v", err)
	}
}

func TestNativeRepairPreservesBlockedDiagnosisAndDiscardsPartialWorktree(t *testing.T) {
	workspace := nativeEditRepository(t)
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module github.com/vgxness/vgxness\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nativeEditGit(t, workspace, "add", "go.mod")
	nativeEditGit(t, workspace, "commit", "--quiet", "-m", "identify vgxness")
	service := New(Options{StorageRoot: filepath.Join(t.TempDir(), "storage")})
	prepared, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-repair-blocked", Model: "openai/gpt-5.6-sol",
		Operation: bridge.RepairSystem, Goal: "diagnose an ambiguous control-plane failure",
		ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := readNativeTicket(service.storageRoot, "ticket-repair-blocked")
	if err != nil || document.Edit == nil {
		t.Fatal(err)
	}
	read, err := service.ReadNative(context.Background(), workspace, bridge.NativeReadRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-repair-blocked", ChildSessionID: "ses_child", Path: "README.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.EditNative(context.Background(), workspace, bridge.NativeEditRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-repair-blocked", ChildSessionID: "ses_child",
		Path: "README.md", Content: "unvalidated hypothesis\n", ExpectedSHA256: read.Read.SHA256,
	}); err != nil {
		t.Fatal(err)
	}
	completed, err := service.Complete(context.Background(), workspace, bridge.NativeCompletionRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-repair-blocked", ParentSessionID: "ses_parent",
		ChildSessionID: "ses_child", MessageID: "msg_child", Result: nativeRepairResult(t, prepared.TaskID, "blocked"),
	})
	if err != nil || completed.Status != "completed" || completed.EditArtifact != nil ||
		completed.Receipt == nil || completed.Receipt.Decision != "repair-not-produced" {
		t.Fatalf("blocked diagnosis was not preserved safely: %#v err=%v", completed, err)
	}
	if _, err := os.Stat(document.Edit.Root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("blocked repair left its partial worktree behind: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(workspace, "README.md")); err != nil || string(data) != "base\n" {
		t.Fatalf("blocked repair changed the canonical checkout: %q err=%v", data, err)
	}
}

func TestNativeRepairRejectsNonVGXNESSRepository(t *testing.T) {
	workspace := nativeEditRepository(t)
	service := New(Options{StorageRoot: filepath.Join(t.TempDir(), "storage")})
	_, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-repair", Model: "openai/gpt-5.6-sol",
		Operation: bridge.RepairSystem, Goal: "repair", ParentSessionID: "ses_parent",
		ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if !errors.Is(err, bridge.ErrDenied) {
		t.Fatalf("repair was allowed outside VGXNESS: %v", err)
	}
}

func validateRepair(t *testing.T, service *Service, workspace string, operation bridge.NativeValidationOperation) {
	t.Helper()
	response, err := service.ValidateNative(context.Background(), workspace, bridge.NativeValidationRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-repair", ChildSessionID: "ses_child",
		Operation: operation, Packages: []string{"./..."},
	})
	if err != nil || response.Validation == nil || !response.Validation.Success {
		t.Fatalf("%s failed: %#v err=%v", operation, response.Validation, err)
	}
}

func nativeRepairResult(t *testing.T, taskID, status string) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"kind": "agent.result", "schemaVersion": "1", "resultId": "result-" + taskID, "taskId": taskID,
		"agentId": "vgxness-maintainer", "status": status, "summary": "bounded repair outcome",
		"artifacts": []any{}, "nextRecommended": "review the repair outcome", "risks": []any{}, "errors": []any{},
		"memoryCandidates": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}
