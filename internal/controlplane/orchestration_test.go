package controlplane

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
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
	bindings := []bridge.OrchestrationBinding{
		{TaskID: "task-memory", ChildSessionID: "ses_child_memory", TicketID: "ticket-memory"},
		{TaskID: "task-delegation", ChildSessionID: "ses_child_delegation", TicketID: "ticket-delegation"},
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
	firstBinding := bridge.OrchestrationBinding{TaskID: first.TaskID, ChildSessionID: "ses_child_first", TicketID: "ticket-first"}
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
	if _, err := service.RecordOrchestrationTerminal(context.Background(), workspace, bridge.OrchestrateTerminalRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID, OwnerID: view.OwnerID,
		TaskID: firstBinding.TaskID, TicketID: firstBinding.TicketID, ChildSessionID: firstBinding.ChildSessionID,
		Status: "completed", MessageID: "msg_first", ResultID: "result-first", Result: result,
	}); err != nil {
		t.Fatal(err)
	}
	// Simulate response loss after authority acceptance but before its document
	// projection. Status must reconstruct both the ticket binding and result.
	document, err := readOrchestrationDocument(storage, view.OrchestrationID)
	if err != nil {
		t.Fatal(err)
	}
	document.PreparedBindings = map[string]string{}
	document.Results = map[string]json.RawMessage{}
	if err := writeOrchestrationDocument(storage, document); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StatusOrchestration(context.Background(), workspace, bridge.OrchestrateReferenceRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID,
	}); err != nil {
		t.Fatal(err)
	}
	secondBinding := bridge.OrchestrationBinding{TaskID: second.TaskID, ChildSessionID: "ses_child_second", TicketID: "ticket-second"}
	prepared, err := service.PrepareOrchestrationWave(context.Background(), workspace, bridge.OrchestrateWaveRequest{
		ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: view.OrchestrationID, OwnerID: view.OwnerID, Bindings: []bridge.OrchestrationBinding{secondBinding},
	})
	if err != nil || prepared.Orchestration == nil || len(prepared.Orchestration.Prepared) != 1 {
		t.Fatalf("prepared=%#v err=%v", prepared, err)
	}
	prompt := prepared.Orchestration.Prepared[0].Prepared.Prompt
	if !strings.Contains(prompt, "Validated dependency results (JSON)") || !strings.Contains(prompt, "task-first") {
		t.Fatalf("dependent prompt lacks bounded result evidence: %s", prompt)
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
	binding := bridge.OrchestrationBinding{TaskID: first.TaskID, ChildSessionID: "ses_child_first", TicketID: "ticket-first"}
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
	next := bridge.OrchestrationBinding{TaskID: second.TaskID, ChildSessionID: "ses_child_second", TicketID: "ticket-second"}
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
