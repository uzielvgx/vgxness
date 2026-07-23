package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/navigator"
	"github.com/vgxness/vgxness/internal/providers"
)

func TestAdaptiveOrchestrationPersistsParallelNativeWaveAndJoin(t *testing.T) {
	workspace := t.TempDir()
	storage := filepath.Join(t.TempDir(), "storage")
	now := time.Now().UTC()
	adapter := nativeTestAdapter(now)
	sequence := 0
	service := New(Options{
		StorageRoot: storage, Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return adapter, nil },
		NewID: func(prefix string) (string, error) {
			sequence++
			return prefix + "-orchestration-" + strconv.Itoa(sequence), nil
		},
	})
	request := bridge.OrchestratePlanRequest{
		ProtocolVersion: bridge.ProtocolVersion, Model: "openai/gpt-5.6-sol",
		Input:           bridge.OrchestrateInput{Goal: "Inspect memory and delegation independently", AcceptanceCriteria: []string{"Return both boundaries"}},
		ParentSessionID: "ses_parent", ParentMessageID: "msg_parent",
		CandidateTasks: []navigator.Task{
			orchestrationReadTask("task-memory", nil), orchestrationReadTask("task-delegation", nil),
		},
	}
	planned, err := service.PlanOrchestration(context.Background(), workspace, request)
	if err != nil || planned.Orchestration == nil || planned.Orchestration.Plan.Decision != "parallel" {
		t.Fatalf("planned=%#v err=%v", planned, err)
	}
	view := planned.Orchestration
	if view.ParentSessionID != "ses_parent" || view.NextWave != 0 {
		t.Fatalf("planned visible wave projection=%#v", view)
	}
	bindings := []bridge.OrchestrationBinding{
		{TaskID: "task-memory", ChildSessionID: "ses_child_memory", TicketID: "ticket-memory", ClaimToken: "claim-memory"},
		{TaskID: "task-delegation", ChildSessionID: "ses_child_delegation", TicketID: "ticket-delegation", ClaimToken: "claim-delegation"},
	}
	failed := bindings[1]
	if os.MkdirAll(nativeTicketDirectory(storage), 0o700) != nil || os.WriteFile(filepath.Join(nativeTicketDirectory(storage), failed.TicketID+".json"), []byte("{}"), 0o600) != nil {
		t.Fatal("failed to create conflicting native ticket")
	}
	prepared, err := service.PrepareOrchestrationWave(context.Background(), workspace, bridge.OrchestrateWaveRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID, OwnerID: view.OwnerID, Bindings: bindings,
	})
	if err != nil || prepared.Orchestration == nil || len(prepared.Orchestration.Prepared) != 1 || prepared.Status != "running" {
		t.Fatalf("prepared=%#v err=%v", prepared, err)
	}
	document, err := readOrchestrationDocument(storage, view.OrchestrationID)
	if err != nil {
		t.Fatal(err)
	}
	document.PreparedBindings = map[string]string{}
	document.ClaimTokens = map[string]string{}
	if err := writeOrchestrationDocument(storage, document); err != nil {
		t.Fatal(err)
	}
	prepared, err = service.PrepareOrchestrationWave(context.Background(), workspace, bridge.OrchestrateWaveRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID, OwnerID: view.OwnerID, Bindings: bindings,
	})
	if err != nil || len(prepared.Orchestration.Prepared) != 1 {
		t.Fatalf("prepared replay=%#v err=%v", prepared, err)
	}
	for _, binding := range bindings {
		if binding.TaskID == failed.TaskID {
			continue
		}
		document, readErr := readNativeTicket(storage, binding.TicketID)
		if readErr != nil {
			t.Fatal(readErr)
		}
		result := nativeResult(t, document.TaskID)
		messageID := "msg_" + strings.TrimPrefix(binding.TaskID, "task-")
		completed, completeErr := service.Complete(context.Background(), workspace, bridge.NativeCompletionRequest{
			ProtocolVersion: bridge.ProtocolVersion, TicketID: binding.TicketID, ParentSessionID: "ses_parent",
			ChildSessionID: binding.ChildSessionID, MessageID: messageID, Result: result,
		})
		if completeErr != nil || !completed.OK {
			t.Fatalf("complete task=%s response=%#v err=%v", binding.TaskID, completed, completeErr)
		}
		if recovered, recoverErr := service.StatusOrchestration(context.Background(), workspace, bridge.OrchestrateReferenceRequest{ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID}); recoverErr != nil || recovered.Status != "failed" {
			t.Fatalf("native completion was not recovered: %#v err=%v", recovered, recoverErr)
		}
		terminal, terminalErr := service.RecordOrchestrationTerminal(context.Background(), workspace, bridge.OrchestrateTerminalRequest{
			ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID, OwnerID: view.OwnerID,
			TaskID: binding.TaskID, TicketID: binding.TicketID, ChildSessionID: binding.ChildSessionID,
			Status: "completed", MessageID: "message-" + binding.TicketID, ResultID: "result-" + binding.TicketID, Result: result,
		})
		if terminalErr != nil || terminal.Orchestration == nil {
			t.Fatalf("terminal=%#v err=%v", terminal, terminalErr)
		}
	}
	joined, err := service.CancelOrchestration(context.Background(), workspace, bridge.OrchestrateReferenceRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID, OwnerID: view.OwnerID,
	})
	if err != nil || joined.Orchestration == nil || joined.Status != "failed" || len(joined.Orchestration.Join) == 0 {
		t.Fatalf("terminal cancellation projection=%#v err=%v", joined, err)
	}
	var join struct {
		Status    string `json:"status"`
		Completed int    `json:"completed"`
	}
	if json.Unmarshal(joined.Orchestration.Join, &join) != nil || join.Status != "partial" || join.Completed != 1 {
		t.Fatalf("unexpected durable join: %s", joined.Orchestration.Join)
	}

	reopened := New(Options{StorageRoot: storage, AdapterFactory: func(string) (providers.Adapter, error) { return adapter, nil }})
	status, err := reopened.StatusOrchestration(context.Background(), workspace, bridge.OrchestrateReferenceRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID,
	})
	if err != nil || status.Status != "failed" || status.Orchestration == nil || len(status.Orchestration.Join) == 0 {
		t.Fatalf("reopened status=%#v err=%v", status, err)
	}
}

func TestWriteOrchestrationPublishesAuthoritativeEditArtifact(t *testing.T) {
	workspace := nativeEditRepository(t)
	storage := filepath.Join(t.TempDir(), "storage")
	now := time.Now().UTC()
	sequence := 0
	service := New(Options{
		StorageRoot: storage, Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
		NewID: func(prefix string) (string, error) {
			sequence++
			return prefix + "-write-" + strconv.Itoa(sequence), nil
		},
	})
	planned, err := service.PlanOrchestration(context.Background(), workspace, bridge.OrchestratePlanRequest{
		ProtocolVersion: bridge.ProtocolVersion, Model: "openai/gpt-5.6-sol",
		Input:           bridge.OrchestrateInput{Goal: "Update one bounded file"},
		ParentSessionID: "ses_parent", ParentMessageID: "msg_parent",
		CandidateTasks: []navigator.Task{{
			TaskID: "task-edit", Capability: navigator.CapabilityImplement, Operation: navigator.OperationWriteFiles,
			Goal: "Update the value", AcceptanceCriteria: []string{"Change the value"}, DependsOn: []string{}, Continuity: navigator.ContinuityIsolated,
		}},
	})
	if err != nil || planned.Orchestration == nil {
		t.Fatalf("planned=%#v err=%v", planned, err)
	}
	view := planned.Orchestration
	binding := bridge.OrchestrationBinding{TaskID: "task-edit", ChildSessionID: "ses_child", TicketID: "ticket-edit-visible", ClaimToken: "claim-edit"}
	prepared, err := service.PrepareOrchestrationWave(context.Background(), workspace, bridge.OrchestrateWaveRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID, OwnerID: view.OwnerID,
		Bindings: []bridge.OrchestrationBinding{binding},
	})
	if err != nil || prepared.Orchestration == nil || len(prepared.Orchestration.Prepared) != 1 || prepared.Orchestration.Prepared[0].Prepared.Agent != "vgxness-implementer" {
		t.Fatalf("prepared=%#v err=%v", prepared, err)
	}
	native, err := readNativeTicket(storage, binding.TicketID)
	if err != nil || native.Edit == nil {
		t.Fatalf("native edit ticket=%#v err=%v", native.Edit, err)
	}
	t.Cleanup(func() { removeNativeEditWorkspace(workspace, native.Edit) })
	read, err := service.ReadNative(context.Background(), workspace, bridge.NativeReadRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: binding.TicketID, ChildSessionID: binding.ChildSessionID, Path: "internal/app.go",
	})
	if err != nil || read.Read == nil {
		t.Fatalf("read=%#v err=%v", read, err)
	}
	if _, err := service.EditNative(context.Background(), workspace, bridge.NativeEditRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: binding.TicketID, ChildSessionID: binding.ChildSessionID,
		Path: "internal/app.go", Content: "package internal\n\nconst Value = \"visible\"\n", ExpectedSHA256: nativeSHA256([]byte(read.Read.Content)),
	}); err != nil {
		t.Fatal(err)
	}
	result := nativeResult(t, native.TaskID)
	completed, err := service.Complete(context.Background(), workspace, bridge.NativeCompletionRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: binding.TicketID, ParentSessionID: "ses_parent",
		ChildSessionID: binding.ChildSessionID, MessageID: "msg_child", Result: result,
	})
	if err != nil || completed.EditArtifact == nil {
		t.Fatalf("completed=%#v err=%v", completed, err)
	}
	terminal, err := service.RecordOrchestrationTerminal(context.Background(), workspace, bridge.OrchestrateTerminalRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID, OwnerID: view.OwnerID,
		TaskID: binding.TaskID, TicketID: binding.TicketID, ChildSessionID: binding.ChildSessionID,
		Status: "completed", MessageID: "msg_child", ResultID: "result-" + binding.TicketID, Result: result,
	})
	if err != nil || terminal.Orchestration == nil {
		t.Fatalf("terminal=%#v err=%v", terminal, err)
	}
	artifact, present := terminal.Orchestration.EditArtifacts[binding.TaskID]
	if !present || artifact.ManifestSHA != completed.EditArtifact.ManifestSHA {
		t.Fatalf("terminal edit artifact=%#v response=%#v err=%v", artifact, terminal, err)
	}
	joined, err := service.JoinOrchestration(context.Background(), workspace, bridge.OrchestrateReferenceRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID, OwnerID: view.OwnerID,
	})
	if err != nil || joined.Orchestration == nil || joined.Orchestration.EditArtifacts[binding.TaskID].Worktree != native.Edit.Root {
		t.Fatalf("joined edit artifact=%#v err=%v", joined, err)
	}
}

func TestStatusOrchestrationReopensMixedParallelReadWave(t *testing.T) {
	workspace := t.TempDir()
	storage := filepath.Join(t.TempDir(), "storage")
	sequence := 0
	service := New(Options{
		StorageRoot: storage,
		NewID: func(prefix string) (string, error) {
			sequence++
			return prefix + "-mixed-wave-" + strconv.Itoa(sequence), nil
		},
	})
	analyze := orchestrationReadTask("task-analyze", nil)
	analyze.Operation = navigator.OperationAnalyzeStructure
	planned, err := service.PlanOrchestration(context.Background(), workspace, bridge.OrchestratePlanRequest{
		ProtocolVersion: bridge.ProtocolVersion,
		Model:           "openai/gpt-5.6-sol",
		Input:           bridge.OrchestrateInput{Goal: "Inspect code and documentation independently"},
		ParentSessionID: "ses_parent",
		ParentMessageID: "msg_parent",
		CandidateTasks: []navigator.Task{
			analyze,
			orchestrationReadTask("task-read", nil),
		},
	})
	if err != nil || planned.Orchestration == nil || planned.Orchestration.Plan.Decision != "parallel" {
		t.Fatalf("planned=%#v err=%v", planned, err)
	}
	status, err := service.StatusOrchestration(context.Background(), workspace, bridge.OrchestrateReferenceRequest{
		ProtocolVersion: bridge.ProtocolVersion,
		OrchestrationID: planned.Orchestration.OrchestrationID,
	})
	if err != nil || status.Orchestration == nil || status.Status != "pending" || status.Orchestration.NextWave != 0 {
		t.Fatalf("reopened mixed wave=%#v err=%v", status, err)
	}
}

func TestAdaptiveOrchestrationAcceptsCompletedTerminalAfterParallelSiblingFailsAcrossServices(t *testing.T) {
	workspace := t.TempDir()
	storage := filepath.Join(t.TempDir(), "storage")
	now := time.Now().UTC()
	adapter := nativeTestAdapter(now)
	sequence := 0
	service := New(Options{
		StorageRoot: storage, Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return adapter, nil },
		NewID: func(prefix string) (string, error) {
			sequence++
			return prefix + "-mixed-terminal-" + strconv.Itoa(sequence), nil
		},
	})
	reopen := func() *Service {
		return New(Options{
			StorageRoot: storage, Now: func() time.Time { return now },
			AdapterFactory: func(string) (providers.Adapter, error) { return adapter, nil },
		})
	}
	planned, err := service.PlanOrchestration(context.Background(), workspace, bridge.OrchestratePlanRequest{
		ProtocolVersion: bridge.ProtocolVersion, Model: "openai/gpt-5.6-sol",
		Input:           bridge.OrchestrateInput{Goal: "Inspect two boundaries independently"},
		ParentSessionID: "ses_parent", ParentMessageID: "msg_parent",
		CandidateTasks: []navigator.Task{
			orchestrationReadTask("task-completed", nil), orchestrationReadTask("task-failed", nil),
		},
	})
	if err != nil || planned.Orchestration == nil {
		t.Fatalf("planned=%#v err=%v", planned, err)
	}
	view := planned.Orchestration
	completedBinding := bridge.OrchestrationBinding{TaskID: "task-completed", ChildSessionID: "ses_child_completed", TicketID: "ticket-completed", ClaimToken: "claim-completed"}
	failedBinding := bridge.OrchestrationBinding{TaskID: "task-failed", ChildSessionID: "ses_child_failed", TicketID: "ticket-failed", ClaimToken: "claim-failed"}
	prepared, err := reopen().PrepareOrchestrationWave(context.Background(), workspace, bridge.OrchestrateWaveRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID, OwnerID: view.OwnerID,
		Bindings: []bridge.OrchestrationBinding{completedBinding, failedBinding},
	})
	if err != nil || prepared.Orchestration == nil || len(prepared.Orchestration.Prepared) != 2 {
		t.Fatalf("prepared=%#v err=%v", prepared, err)
	}
	replayed, err := reopen().StatusOrchestration(context.Background(), workspace, bridge.OrchestrateReferenceRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID,
		TaskID: completedBinding.TaskID, ChildSessionID: completedBinding.ChildSessionID, ClaimToken: completedBinding.ClaimToken,
	})
	if err != nil || replayed.Orchestration == nil || len(replayed.Orchestration.Prepared) != 1 {
		t.Fatalf("claim-bound prepared replay=%#v err=%v", replayed, err)
	}
	for _, item := range replayed.Orchestration.Prepared {
		if item.ChildSessionID != completedBinding.ChildSessionID || item.Prepared.TicketID != completedBinding.TicketID {
			t.Fatalf("prepared replay lost native binding: %#v", item)
		}
	}
	publicStatus, err := reopen().StatusOrchestration(context.Background(), workspace, bridge.OrchestrateReferenceRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID,
	})
	if err != nil || publicStatus.Orchestration == nil || len(publicStatus.Orchestration.Prepared) != 0 {
		t.Fatalf("ownerless status exposed prepared prompts: %#v err=%v", publicStatus, err)
	}
	failed, err := reopen().Fail(context.Background(), workspace, bridge.NativeFailureRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: failedBinding.TicketID,
		ParentSessionID: "ses_parent", ChildSessionID: failedBinding.ChildSessionID, Category: "native-subagent-failed",
	})
	if err != nil || !failed.OK {
		t.Fatalf("failed=%#v err=%v", failed, err)
	}
	failedTerminal, err := reopen().StatusOrchestration(context.Background(), workspace, bridge.OrchestrateReferenceRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID,
	})
	if err != nil || failedTerminal.Orchestration == nil || failedTerminal.Status != "running" {
		t.Fatalf("reconciled failed terminal=%#v err=%v", failedTerminal, err)
	}
	native, err := readNativeTicket(storage, completedBinding.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	result := nativeResult(t, native.TaskID)
	completed, err := reopen().Complete(context.Background(), workspace, bridge.NativeCompletionRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: completedBinding.TicketID,
		ParentSessionID: "ses_parent", ChildSessionID: completedBinding.ChildSessionID,
		MessageID: "msg_completed", Result: result,
	})
	if err != nil || !completed.OK {
		t.Fatalf("completed=%#v err=%v", completed, err)
	}
	completedTerminal, err := reopen().RecordOrchestrationTerminal(context.Background(), workspace, bridge.OrchestrateTerminalRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID, OwnerID: view.OwnerID,
		TaskID: completedBinding.TaskID, TicketID: completedBinding.TicketID, ChildSessionID: completedBinding.ChildSessionID,
		Status: "completed", MessageID: "message-" + completedBinding.TicketID,
		ResultID: "result-" + completedBinding.TicketID, Result: result,
	})
	if err != nil || completedTerminal.Orchestration == nil || completedTerminal.Status != "failed" {
		t.Fatalf("completed terminal=%#v err=%v", completedTerminal, err)
	}
}

func TestStatusOrchestrationExpiresPreparedVisibleTask(t *testing.T) {
	workspace := t.TempDir()
	storage := filepath.Join(t.TempDir(), "storage")
	now := time.Now().UTC()
	adapter := nativeTestAdapter(now)
	sequence := 0
	service := New(Options{
		StorageRoot: storage,
		Now:         func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) {
			return adapter, nil
		},
		NewID: func(prefix string) (string, error) {
			sequence++
			return prefix + "-expiry-" + strconv.Itoa(sequence), nil
		},
	})
	planned, err := service.PlanOrchestration(context.Background(), workspace, bridge.OrchestratePlanRequest{
		ProtocolVersion: bridge.ProtocolVersion, Model: "openai/gpt-5.6-sol",
		Input:           bridge.OrchestrateInput{Goal: "Inspect one bounded file"},
		ParentSessionID: "ses_parent", ParentMessageID: "msg_parent",
		CandidateTasks: []navigator.Task{orchestrationReadTask("task-expiry", nil)},
	})
	if err != nil || planned.Orchestration == nil {
		t.Fatalf("planned=%#v err=%v", planned, err)
	}
	view := planned.Orchestration
	binding := bridge.OrchestrationBinding{
		TaskID: "task-expiry", ChildSessionID: "ses_child_expiry",
		TicketID: "ticket-expiry", ClaimToken: "claim-expiry",
	}
	prepared, err := service.PrepareOrchestrationWave(context.Background(), workspace, bridge.OrchestrateWaveRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID,
		OwnerID: view.OwnerID, Bindings: []bridge.OrchestrationBinding{binding},
	})
	if err != nil || prepared.Status != "running" {
		t.Fatalf("prepared=%#v err=%v", prepared, err)
	}
	now = now.Add(11 * time.Minute)
	expired, err := service.StatusOrchestration(context.Background(), workspace, bridge.OrchestrateReferenceRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID,
	})
	if err != nil || expired.Orchestration == nil || expired.Status != "failed" {
		t.Fatalf("expired=%#v err=%v", expired, err)
	}
	native, err := readNativeTicket(storage, binding.TicketID)
	if err != nil || native.State != "failed" || native.Response == nil {
		t.Fatalf("expired native ticket=%#v err=%v", native, err)
	}
}

func TestPlanOrchestrationUsesDefaultProjectStorage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	sequence := 0
	service := New(Options{NewID: func(prefix string) (string, error) {
		sequence++
		return prefix + "-default-storage-0" + strconv.Itoa(sequence), nil
	}})

	planned, err := service.PlanOrchestration(context.Background(), workspace, bridge.OrchestratePlanRequest{
		ProtocolVersion: bridge.ProtocolVersion,
		Model:           "openai/gpt-5.6-sol",
		Input: bridge.OrchestrateInput{
			Goal:               "Inspect the project",
			AcceptanceCriteria: []string{"Use repository evidence"},
		},
		ParentSessionID: "ses_parent",
		ParentMessageID: "msg_parent",
		CandidateTasks: []navigator.Task{
			orchestrationReadTask("task-project", nil),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if planned.Orchestration == nil {
		t.Fatal("missing orchestration response")
	}
	resolvedWorkspace, err := canonicalWorkspace(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := config.PathsFor(config.Options{HomeDir: home, ProjectDir: resolvedWorkspace})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(paths.Root, "orchestration-plans", planned.Orchestration.OrchestrationID+".json")); err != nil {
		t.Fatal(err)
	}
}

func TestAdaptiveOrchestrationCarriesBoundedDependencyResults(t *testing.T) {
	workspace := t.TempDir()
	storage := filepath.Join(t.TempDir(), "storage")
	now := time.Now().UTC()
	adapter := nativeTestAdapter(now)
	sequence := 0
	service := New(Options{
		StorageRoot: storage, Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return adapter, nil },
		NewID: func(prefix string) (string, error) {
			sequence++
			return prefix + "-dependency-" + strconv.Itoa(sequence), nil
		},
	})
	first := orchestrationReadTask("task-first", nil)
	second := orchestrationReadTask("task-second", []string{"task-first"})
	planned, err := service.PlanOrchestration(context.Background(), workspace, bridge.OrchestratePlanRequest{
		ProtocolVersion: bridge.ProtocolVersion, Model: "openai/gpt-5.6-sol",
		Input: bridge.OrchestrateInput{Goal: "Inspect then verify"}, ParentSessionID: "ses_parent", ParentMessageID: "msg_parent",
		CandidateTasks: []navigator.Task{first, second},
	})
	if err != nil {
		t.Fatal(err)
	}
	view := planned.Orchestration
	firstBinding := bridge.OrchestrationBinding{TaskID: first.TaskID, ChildSessionID: "ses_child_first", TicketID: "ticket-first", ClaimToken: "claim-first"}
	if _, err := service.PrepareOrchestrationWave(context.Background(), workspace, bridge.OrchestrateWaveRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID, OwnerID: view.OwnerID, Bindings: []bridge.OrchestrationBinding{firstBinding},
	}); err != nil {
		t.Fatal(err)
	}
	native, err := readNativeTicket(storage, firstBinding.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	result := nativeResult(t, native.TaskID)
	if _, err := service.Complete(context.Background(), workspace, bridge.NativeCompletionRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: firstBinding.TicketID, ParentSessionID: "ses_parent", ChildSessionID: firstBinding.ChildSessionID, MessageID: "msg_first", Result: result,
	}); err != nil {
		t.Fatal(err)
	}
	// Simulate response loss after native completion but before orchestration
	// acknowledgement. Status must reconcile and project the durable result in
	// the same request so the dependent wave can consume it immediately.
	reconciled, err := service.StatusOrchestration(context.Background(), workspace, bridge.OrchestrateReferenceRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID,
	})
	if err != nil || reconciled.Orchestration == nil || reconciled.Orchestration.NextWave != 1 {
		t.Fatalf("next visible wave projection=%#v err=%v", reconciled, err)
	}
	secondBinding := bridge.OrchestrationBinding{TaskID: second.TaskID, ChildSessionID: "ses_child_second", TicketID: "ticket-second", ClaimToken: "claim-second"}
	prepared, err := service.PrepareOrchestrationWave(context.Background(), workspace, bridge.OrchestrateWaveRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID, OwnerID: view.OwnerID, Bindings: []bridge.OrchestrationBinding{secondBinding},
	})
	if err != nil || prepared.Orchestration == nil || len(prepared.Orchestration.Prepared) != 1 {
		t.Fatalf("prepared=%#v err=%v", prepared, err)
	}
	prompt := prepared.Orchestration.Prepared[0].Prepared.Prompt
	if !strings.Contains(prompt, "Validated dependency evidence (bounded JSON)") || !strings.Contains(prompt, "task-first") {
		t.Fatalf("dependent prompt lacks bounded result evidence: %s", prompt)
	}
	secondNative, err := readNativeTicket(storage, secondBinding.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	finalResult := nativeResult(t, secondNative.TaskID)
	if _, err := service.Complete(context.Background(), workspace, bridge.NativeCompletionRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: secondBinding.TicketID, ParentSessionID: "ses_parent", ChildSessionID: secondBinding.ChildSessionID, MessageID: "msg_second", Result: finalResult,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordOrchestrationTerminal(context.Background(), workspace, bridge.OrchestrateTerminalRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID, OwnerID: view.OwnerID,
		TaskID: secondBinding.TaskID, TicketID: secondBinding.TicketID, ChildSessionID: secondBinding.ChildSessionID,
		Status: "completed", MessageID: "msg_second", ResultID: "result-second", Result: finalResult,
	}); err != nil {
		t.Fatal(err)
	}
	joined, err := service.JoinOrchestration(context.Background(), workspace, bridge.OrchestrateReferenceRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID, OwnerID: view.OwnerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(joined.Result, finalResult) {
		t.Fatalf("joined result=%s want=%s", joined.Result, finalResult)
	}
}

func TestOrchestrationTaskGoalCompactsLargeParallelEvidence(t *testing.T) {
	dependencies := []string{"task-purpose", "task-architecture", "task-quality", "task-risks"}
	results := make(map[string]json.RawMessage, len(dependencies))
	for _, dependency := range dependencies {
		result, err := json.Marshal(map[string]any{
			"kind": "agent.result", "schemaVersion": "1", "resultId": "result-" + dependency,
			"taskId": dependency, "agentId": "vgxness-explorer", "status": "success",
			"summary":   strings.Repeat("evidencia útil con ruta internal/controlplane/orchestration.go; ", 220),
			"artifacts": []any{}, "nextRecommended": strings.Repeat("priorizar la siguiente validación; ", 80),
			"risks":  []string{strings.Repeat("riesgo uno; ", 80), strings.Repeat("riesgo dos; ", 80), strings.Repeat("riesgo tres; ", 80)},
			"errors": []any{},
		})
		if err != nil {
			t.Fatal(err)
		}
		results[dependency] = result
	}
	goal, err := orchestrationTaskGoal(navigator.Task{
		TaskID: "task-synthesis", Goal: "Produce an executive synthesis", DependsOn: dependencies,
	}, results)
	if err != nil {
		t.Fatal(err)
	}
	if len(goal) > orchestrationTaskGoalLimit || !strings.Contains(goal, "task-purpose") || !strings.Contains(goal, `"truncated":true`) {
		t.Fatalf("unexpected bounded dependency goal bytes=%d: %s", len(goal), goal)
	}
	for _, dependency := range dependencies {
		if !strings.Contains(goal, dependency) {
			t.Fatalf("bounded dependency goal omitted %s", dependency)
		}
	}
}

func TestAdaptiveOrchestrationResumeAdvancesOwnerAndKeepsCheckpoint(t *testing.T) {
	workspace := t.TempDir()
	storage := filepath.Join(t.TempDir(), "storage")
	now := time.Now().UTC()
	adapter := nativeTestAdapter(now)
	sequence := 0
	service := New(Options{
		StorageRoot: storage, Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return adapter, nil },
		NewID: func(prefix string) (string, error) {
			sequence++
			return prefix + "-resume-" + strconv.Itoa(sequence), nil
		},
	})
	first := orchestrationReadTask("task-first", nil)
	second := orchestrationReadTask("task-second", []string{"task-first"})
	planned, err := service.PlanOrchestration(context.Background(), workspace, bridge.OrchestratePlanRequest{
		ProtocolVersion: bridge.ProtocolVersion, Model: "openai/gpt-5.6-sol",
		Input: bridge.OrchestrateInput{Goal: "Inspect then resume"}, ParentSessionID: "ses_parent", ParentMessageID: "msg_parent",
		CandidateTasks: []navigator.Task{first, second},
	})
	if err != nil {
		t.Fatal(err)
	}
	initial := planned.Orchestration
	binding := bridge.OrchestrationBinding{TaskID: first.TaskID, ChildSessionID: "ses_child_first", TicketID: "ticket-first", ClaimToken: "claim-first"}
	if _, err := service.PrepareOrchestrationWave(context.Background(), workspace, bridge.OrchestrateWaveRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: initial.OrchestrationID, OwnerID: initial.OwnerID, Bindings: []bridge.OrchestrationBinding{binding},
	}); err != nil {
		t.Fatal(err)
	}
	native, err := readNativeTicket(storage, binding.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	result := nativeResult(t, native.TaskID)
	if _, err := service.Complete(context.Background(), workspace, bridge.NativeCompletionRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: binding.TicketID, ParentSessionID: "ses_parent", ChildSessionID: binding.ChildSessionID, MessageID: "msg_first", Result: result,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordOrchestrationTerminal(context.Background(), workspace, bridge.OrchestrateTerminalRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: initial.OrchestrationID, OwnerID: initial.OwnerID,
		TaskID: binding.TaskID, TicketID: binding.TicketID, ChildSessionID: binding.ChildSessionID,
		Status: "completed", MessageID: "msg_first", ResultID: "result-first", Result: result,
	}); err != nil {
		t.Fatal(err)
	}
	resumed, err := service.ResumeOrchestration(context.Background(), workspace, bridge.OrchestrateReferenceRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: initial.OrchestrationID, OwnerID: initial.OwnerID,
	})
	if err != nil || resumed.Orchestration == nil || resumed.Orchestration.OwnerID == initial.OwnerID {
		t.Fatalf("resumed=%#v err=%v", resumed, err)
	}
	next := bridge.OrchestrationBinding{TaskID: second.TaskID, ChildSessionID: "ses_child_second", TicketID: "ticket-second", ClaimToken: "claim-second"}
	request := bridge.OrchestrateWaveRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: initial.OrchestrationID, OwnerID: initial.OwnerID, Bindings: []bridge.OrchestrationBinding{next},
	}
	if _, err := service.PrepareOrchestrationWave(context.Background(), workspace, request); err == nil {
		t.Fatal("superseded owner prepared the next wave")
	}
	request.OwnerID = resumed.Orchestration.OwnerID
	prepared, err := service.PrepareOrchestrationWave(context.Background(), workspace, request)
	if err != nil || prepared.Orchestration == nil || len(prepared.Orchestration.Prepared) != 1 || !strings.Contains(prepared.Orchestration.Prepared[0].Prepared.Prompt, "task-first") {
		t.Fatalf("prepared=%#v err=%v", prepared, err)
	}
}

func orchestrationReadTask(id string, dependencies []string) navigator.Task {
	return navigator.Task{
		TaskID: id, Capability: navigator.CapabilityExplore, Operation: navigator.OperationReadFiles,
		Goal: "Inspect " + id, AcceptanceCriteria: []string{"Return bounded evidence"},
		DependsOn: append([]string(nil), dependencies...), Continuity: navigator.ContinuityIsolated,
	}
}
