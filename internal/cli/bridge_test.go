package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/bridge"
)

type fakeBridgeRuntime struct {
	status     bridge.Response
	response   bridge.Response
	err        error
	request    bridge.DispatchRequest
	completion bridge.NativeCompletionRequest
	failure    bridge.NativeFailureRequest
	read       bridge.NativeReadRequest
	edit       bridge.NativeEditRequest
	codegraph  bridge.NativeCodeGraphRequest
	plan       bridge.OrchestratePlanRequest
	wave       bridge.OrchestrateWaveRequest
	terminal   bridge.OrchestrateTerminalRequest
	reference  bridge.OrchestrateReferenceRequest
	action     string
}

func (runtime *fakeBridgeRuntime) Prepare(_ context.Context, _ string, request bridge.DispatchRequest) (bridge.Response, error) {
	runtime.request = request
	return runtime.response, runtime.err
}

func (runtime *fakeBridgeRuntime) Complete(_ context.Context, _ string, request bridge.NativeCompletionRequest) (bridge.Response, error) {
	runtime.completion = request
	return runtime.response, runtime.err
}

func (runtime *fakeBridgeRuntime) Fail(_ context.Context, _ string, request bridge.NativeFailureRequest) (bridge.Response, error) {
	runtime.failure = request
	return runtime.response, runtime.err
}

func (runtime *fakeBridgeRuntime) ReadNative(_ context.Context, _ string, request bridge.NativeReadRequest) (bridge.Response, error) {
	runtime.read = request
	return runtime.response, runtime.err
}

func (runtime *fakeBridgeRuntime) EditNative(_ context.Context, _ string, request bridge.NativeEditRequest) (bridge.Response, error) {
	runtime.edit = request
	return runtime.response, runtime.err
}

func (runtime *fakeBridgeRuntime) QueryNativeCodeGraph(_ context.Context, _ string, request bridge.NativeCodeGraphRequest) (bridge.Response, error) {
	runtime.codegraph = request
	return runtime.response, runtime.err
}

func (runtime *fakeBridgeRuntime) Status(context.Context, string) (bridge.Response, error) {
	return runtime.status, runtime.err
}

func (runtime *fakeBridgeRuntime) Dispatch(_ context.Context, _ string, request bridge.DispatchRequest) (bridge.Response, error) {
	runtime.request = request
	return runtime.response, runtime.err
}

func (runtime *fakeBridgeRuntime) PlanOrchestration(_ context.Context, _ string, request bridge.OrchestratePlanRequest) (bridge.Response, error) {
	runtime.plan, runtime.action = request, "plan"
	return runtime.response, runtime.err
}

func (runtime *fakeBridgeRuntime) PrepareOrchestrationWave(_ context.Context, _ string, request bridge.OrchestrateWaveRequest) (bridge.Response, error) {
	runtime.wave, runtime.action = request, "wave"
	return runtime.response, runtime.err
}

func (runtime *fakeBridgeRuntime) RecordOrchestrationTerminal(_ context.Context, _ string, request bridge.OrchestrateTerminalRequest) (bridge.Response, error) {
	runtime.terminal, runtime.action = request, "terminal"
	return runtime.response, runtime.err
}

func (runtime *fakeBridgeRuntime) recordReference(action string, request bridge.OrchestrateReferenceRequest) (bridge.Response, error) {
	runtime.reference, runtime.action = request, action
	return runtime.response, runtime.err
}

func (runtime *fakeBridgeRuntime) JoinOrchestration(_ context.Context, _ string, request bridge.OrchestrateReferenceRequest) (bridge.Response, error) {
	return runtime.recordReference("join", request)
}

func (runtime *fakeBridgeRuntime) StatusOrchestration(_ context.Context, _ string, request bridge.OrchestrateReferenceRequest) (bridge.Response, error) {
	return runtime.recordReference("status", request)
}

func (runtime *fakeBridgeRuntime) ResumeOrchestration(_ context.Context, _ string, request bridge.OrchestrateReferenceRequest) (bridge.Response, error) {
	return runtime.recordReference("resume", request)
}

func (runtime *fakeBridgeRuntime) CancelOrchestration(_ context.Context, _ string, request bridge.OrchestrateReferenceRequest) (bridge.Response, error) {
	return runtime.recordReference("cancel", request)
}

func TestBridgeRejectsLegacyDispatchRoute(t *testing.T) {
	runtime := &fakeBridgeRuntime{}
	var stdout, stderr bytes.Buffer
	code := runBridge(context.Background(), []string{"dispatch", "--workspace", t.TempDir(), "--stdin"}, strings.NewReader(`{"protocolVersion":"1","model":"openai/gpt-5.6-sol","operation":"read-files","goal":"inspect"}`), &stdout, &stderr, runtime)
	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "status|prepare|complete|fail|read") || runtime.request.Operation != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q request=%#v", code, stdout.String(), stderr.String(), runtime.request)
	}
}

func TestBridgeFailureIsNormalizedToJSON(t *testing.T) {
	runtime := &fakeBridgeRuntime{err: errors.New("provider secret")}
	var stdout, stderr bytes.Buffer
	code := runBridge(context.Background(), []string{"status", "--workspace", t.TempDir()}, strings.NewReader(""), &stdout, &stderr, runtime)
	if code != 1 || stderr.Len() != 0 || strings.Contains(stdout.String(), "provider secret") || !strings.Contains(stdout.String(), `"code":"execution_failed"`) {
		t.Fatalf("unsafe failure: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestBridgeNativeLifecycleRoutesExactRequests(t *testing.T) {
	runtime := &fakeBridgeRuntime{response: bridge.Response{ProtocolVersion: "1", OK: true, Bridge: "healthy", Provider: "opencode", Status: "completed"}}
	workspace := t.TempDir()
	for _, test := range []struct {
		command string
		input   string
	}{
		{"prepare", `{"protocolVersion":"1","ticketId":"ticket-1","model":"openai/gpt-5.6-sol","operation":"read-files","goal":"inspect","parentSessionId":"ses_parent","parentMessageId":"msg_parent","childSessionId":"ses_child"}`},
		{"complete", `{"protocolVersion":"1","ticketId":"ticket-1","parentSessionId":"ses_parent","childSessionId":"ses_child","messageId":"msg_child","result":{}}`},
		{"fail", `{"protocolVersion":"1","ticketId":"ticket-1","parentSessionId":"ses_parent","childSessionId":"ses_child","category":"native-subagent-failed"}`},
		{"read", `{"protocolVersion":"1","ticketId":"ticket-1","childSessionId":"ses_child","path":"go.mod","limit":4096}`},
		{"edit", `{"protocolVersion":"1","ticketId":"ticket-1","childSessionId":"ses_child","path":"go.mod","content":"module changed\n","expectedSha256":"sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`},
		{"codegraph", `{"protocolVersion":"1","ticketId":"ticket-1","childSessionId":"ses_child","operation":"impact","symbol":"Service.Dispatch","depth":3}`},
	} {
		var stdout, stderr bytes.Buffer
		if code := runBridge(context.Background(), []string{test.command, "--workspace", workspace, "--stdin"}, strings.NewReader(test.input), &stdout, &stderr, runtime); code != 0 {
			t.Fatalf("%s code=%d stdout=%s stderr=%s", test.command, code, stdout.String(), stderr.String())
		}
	}
	if runtime.request.ParentSessionID != "ses_parent" || runtime.completion.ChildSessionID != "ses_child" || runtime.failure.Category != "native-subagent-failed" || runtime.read.Path != "go.mod" || runtime.edit.Path != "go.mod" || runtime.codegraph.Symbol != "Service.Dispatch" {
		t.Fatalf("native lifecycle was not routed: request=%#v completion=%#v failure=%#v read=%#v edit=%#v codegraph=%#v", runtime.request, runtime.completion, runtime.failure, runtime.read, runtime.edit, runtime.codegraph)
	}
}

func TestBridgeOrchestrationLifecycleRoutesExactRequests(t *testing.T) {
	runtime := &fakeBridgeRuntime{response: bridge.Response{ProtocolVersion: "1", OK: true, Bridge: "healthy", Provider: "opencode", Status: "running"}}
	workspace := t.TempDir()
	tests := []struct {
		command string
		action  string
		input   string
	}{
		{"orchestrate-plan", "plan", `{"protocolVersion":"1","model":"openai/gpt-5.6-sol","input":{"goal":"inspect"},"parentSessionId":"ses_parent","parentMessageId":"msg_parent","candidateTasks":[{"taskId":"task-1","capability":"explore","operation":"read-files","goal":"inspect","acceptanceCriteria":[],"dependsOn":[],"continuity":"isolated"}]}`},
		{"orchestrate-wave", "wave", `{"protocolVersion":"1","orchestrationId":"orchestration-1","ownerId":"owner-1","bindings":[{"taskId":"task-1","childSessionId":"ses_child","ticketId":"ticket-1","claimToken":"claim-1"}]}`},
		{"orchestrate-terminal", "terminal", `{"protocolVersion":"1","orchestrationId":"orchestration-1","ownerId":"owner-1","taskId":"task-1","ticketId":"ticket-1","childSessionId":"ses_child","status":"completed","messageId":"msg_child","resultId":"result-1","result":{}}`},
		{"orchestrate-join", "join", `{"protocolVersion":"1","orchestrationId":"orchestration-1","ownerId":"owner-1"}`},
		{"orchestrate-status", "status", `{"protocolVersion":"1","orchestrationId":"orchestration-1"}`},
		{"orchestrate-resume", "resume", `{"protocolVersion":"1","orchestrationId":"orchestration-1","ownerId":"owner-1"}`},
		{"orchestrate-cancel", "cancel", `{"protocolVersion":"1","orchestrationId":"orchestration-1","ownerId":"owner-1"}`},
	}
	for _, test := range tests {
		var stdout, stderr bytes.Buffer
		if code := runBridge(context.Background(), []string{test.command, "--workspace", workspace, "--stdin"}, strings.NewReader(test.input), &stdout, &stderr, runtime); code != 0 || runtime.action != test.action {
			t.Fatalf("%s code=%d action=%q stdout=%s stderr=%s", test.command, code, runtime.action, stdout.String(), stderr.String())
		}
	}
	if runtime.plan.ParentSessionID != "ses_parent" || runtime.wave.Bindings[0].ChildSessionID != "ses_child" || runtime.terminal.ResultID != "result-1" || runtime.reference.OrchestrationID != "orchestration-1" {
		t.Fatalf("orchestration lifecycle was not routed: plan=%#v wave=%#v terminal=%#v reference=%#v", runtime.plan, runtime.wave, runtime.terminal, runtime.reference)
	}
}
