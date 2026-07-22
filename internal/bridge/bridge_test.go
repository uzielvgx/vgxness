package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/navigator"
)

func TestDecodeDispatchAcceptsOneBoundedExactRequest(t *testing.T) {
	request, err := DecodeDispatch(strings.NewReader(`{"protocolVersion":"1","model":"openai/gpt-5.6-sol","operation":"write-files","goal":"Implement the bounded change","acceptanceCriteria":["tests pass"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if request.Operation != WriteFiles || request.Goal != "Implement the bounded change" || len(request.AcceptanceCriteria) != 1 {
		t.Fatalf("unexpected request: %#v", request)
	}
}

func TestDecodeOrchestrateAcceptsOnlyPublicIntentAndEnrichesTrustedContext(t *testing.T) {
	input, err := DecodeOrchestrateInput(strings.NewReader(`{"goal":"Inspect memory and delegation independently","acceptanceCriteria":["Explain both boundaries"]}`))
	if err != nil || input.Goal != "Inspect memory and delegation independently" || len(input.AcceptanceCriteria) != 1 {
		t.Fatalf("input=%#v err=%v", input, err)
	}
	request, err := NewOrchestrateRequest(input, OrchestrateContext{Model: "openai/gpt-5.6-sol", ParentSessionID: "ses_parent", ParentMessageID: "msg_parent"})
	if err != nil || request.Input.Goal != input.Goal || request.ParentSessionID != "ses_parent" {
		t.Fatalf("request=%#v err=%v", request, err)
	}
	for _, input := range []string{
		`{"goal":"inspect","tasks":[]}`,
		`{"goal":"inspect","model":"openai/gpt-5.6-sol"}`,
		`{"goal":"inspect","parentSessionId":"ses_parent"}`,
	} {
		if _, err := DecodeOrchestrateInput(strings.NewReader(input)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("input %q: expected ErrInvalid, got %v", input, err)
		}
	}
	if _, err := NewOrchestrateRequest(input, OrchestrateContext{Model: "openai/gpt-5.6-sol", ParentSessionID: "../parent", ParentMessageID: "msg_parent"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("untrusted parent context accepted: %v", err)
	}
}

func TestDecodeOrchestrationLifecycleIsExactAndBounded(t *testing.T) {
	planPayload := `{"protocolVersion":"1","model":"openai/gpt-5.6-sol","input":{"goal":"inspect"},"parentSessionId":"ses_parent","parentMessageId":"msg_parent","candidateTasks":[{"taskId":"task-1","capability":"explore","operation":"read-files","goal":"inspect","acceptanceCriteria":[],"dependsOn":[],"continuity":"isolated"}]}`
	planned, err := DecodeOrchestratePlan(strings.NewReader(planPayload))
	if err != nil || len(planned.CandidateTasks) != 1 || planned.CandidateTasks[0].Operation != navigator.OperationReadFiles {
		t.Fatalf("planned=%#v err=%v", planned, err)
	}
	wave, err := DecodeOrchestrateWave(strings.NewReader(`{"protocolVersion":"1","orchestrationId":"orchestration-1","ownerId":"owner-1","bindings":[{"taskId":"task-1","childSessionId":"ses_child","ticketId":"ticket-1"}]}`))
	if err != nil || len(wave.Bindings) != 1 || wave.Bindings[0].TicketID != "ticket-1" {
		t.Fatalf("wave=%#v err=%v", wave, err)
	}
	terminal, err := DecodeOrchestrateTerminal(strings.NewReader(`{"protocolVersion":"1","orchestrationId":"orchestration-1","ownerId":"owner-1","taskId":"task-1","ticketId":"ticket-1","childSessionId":"ses_child","status":"completed","messageId":"msg_child","resultId":"result-1","result":{}}`))
	if err != nil || terminal.ResultID != "result-1" {
		t.Fatalf("terminal=%#v err=%v", terminal, err)
	}
	reference, err := DecodeOrchestrateReference(strings.NewReader(`{"protocolVersion":"1","orchestrationId":"orchestration-1","ownerId":"owner-1"}`))
	if err != nil || reference.OwnerID != "owner-1" {
		t.Fatalf("reference=%#v err=%v", reference, err)
	}
	for _, invalid := range []struct {
		decode func(io.Reader) error
		input  string
	}{
		{func(reader io.Reader) error { _, err := DecodeOrchestratePlan(reader); return err }, strings.TrimSuffix(planPayload, "}") + `,"agent":"explorer"}`},
		{func(reader io.Reader) error { _, err := DecodeOrchestrateWave(reader); return err }, `{"protocolVersion":"1","orchestrationId":"orchestration-1","ownerId":"owner-1","bindings":[{"taskId":"task-1","childSessionId":"ses_child","ticketId":"ticket-1"},{"taskId":"task-1","childSessionId":"ses_other","ticketId":"ticket-2"}]}`},
		{func(reader io.Reader) error { _, err := DecodeOrchestrateTerminal(reader); return err }, `{"protocolVersion":"1","orchestrationId":"orchestration-1","ownerId":"owner-1","taskId":"task-1","ticketId":"ticket-1","childSessionId":"ses_child","status":"completed","result":{}}`},
		{func(reader io.Reader) error { _, err := DecodeOrchestrateReference(reader); return err }, `{"protocolVersion":"1","orchestrationId":"../escape"}`},
	} {
		if err := invalid.decode(strings.NewReader(invalid.input)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid orchestration input accepted: %s err=%v", invalid.input, err)
		}
	}
	oversizedResult := json.RawMessage(`"` + strings.Repeat("x", MaxOrchestrationResultBytes) + `"`)
	if err := ValidateOrchestrateTerminal(OrchestrateTerminalRequest{
		ProtocolVersion: ProtocolVersion, OrchestrationID: "orchestration-1", OwnerID: "owner-1", TaskID: "task-1",
		TicketID: "ticket-1", ChildSessionID: "ses_child", Status: "completed", MessageID: "msg_child", ResultID: "result-1", Result: oversizedResult,
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized aggregate result accepted: %v", err)
	}
}

func TestDecodeDispatchAcceptsBoundedChangeReview(t *testing.T) {
	request, err := DecodeDispatch(strings.NewReader(`{"protocolVersion":"1","model":"openai/gpt-5.6-sol","operation":"review-changes","goal":"Review the current changes"}`))
	if err != nil {
		t.Fatal(err)
	}
	if request.Operation != ReviewChanges || request.Goal != "Review the current changes" {
		t.Fatalf("unexpected request: %#v", request)
	}
}

func TestDecodeDispatchAcceptsExplicitContinuity(t *testing.T) {
	started, err := DecodeDispatch(strings.NewReader(`{"protocolVersion":"1","model":"openai/gpt-5.6-sol","operation":"read-files","goal":"Inspect the project","continuity":"start"}`))
	if err != nil || started.Continuity != ContinuityStart || started.RunID != "" {
		t.Fatalf("start continuity: request=%#v err=%v", started, err)
	}
	continued, err := DecodeDispatch(strings.NewReader(`{"protocolVersion":"1","model":"openai/gpt-5.6-sol","operation":"write-files","goal":"Apply the next phase","continuity":"continue","runId":"run-123"}`))
	if err != nil || continued.Continuity != ContinuityContinue || continued.RunID != "run-123" {
		t.Fatalf("continue continuity: request=%#v err=%v", continued, err)
	}
	finished, err := DecodeDispatch(strings.NewReader(`{"protocolVersion":"1","model":"openai/gpt-5.6-sol","operation":"review-changes","goal":"Verify and close the run","continuity":"finish","runId":"run-123"}`))
	if err != nil || finished.Continuity != ContinuityFinish || finished.RunID != "run-123" {
		t.Fatalf("finish continuity: request=%#v err=%v", finished, err)
	}
}

func TestDecodeDispatchRejectsBroadOrAmbiguousInput(t *testing.T) {
	tests := []string{
		`{"protocolVersion":"2","model":"openai/gpt-5.6-sol","operation":"read-files","goal":"inspect"}`,
		`{"protocolVersion":"1","operation":"read-files","goal":"inspect"}`,
		`{"protocolVersion":"1","model":"invalid","operation":"read-files","goal":"inspect"}`,
		`{"protocolVersion":"1","model":"--auto/model","operation":"read-files","goal":"inspect"}`,
		`{"protocolVersion":"1","model":"openai/gpt/extra","operation":"read-files","goal":"inspect"}`,
		`{"protocolVersion":"1","model":"openai/` + strings.Repeat("x", maxModelBytes) + `","operation":"read-files","goal":"inspect"}`,
		`{"protocolVersion":"1","model":"openai/gpt-5.6-sol","operation":"run-command","goal":"execute"}`,
		`{"protocolVersion":"1","model":"openai/gpt-5.6-sol","operation":"push","goal":"publish"}`,
		`{"protocolVersion":"1","model":"openai/gpt-5.6-sol","operation":"read-files","goal":"inspect","extra":true}`,
		`{"protocolVersion":"1","model":"openai/gpt-5.6-sol","operation":"read-files","goal":"inspect"}{}`,
		`{"protocolVersion":"1","model":"openai/gpt-5.6-sol","operation":"read-files","goal":"inspect","continuity":"start","runId":"run-1"}`,
		`{"protocolVersion":"1","model":"openai/gpt-5.6-sol","operation":"read-files","goal":"inspect","continuity":"continue"}`,
		`{"protocolVersion":"1","model":"openai/gpt-5.6-sol","operation":"read-files","goal":"inspect","continuity":"finish"}`,
		`{"protocolVersion":"1","model":"openai/gpt-5.6-sol","operation":"read-files","goal":"inspect","continuity":"continue","runId":"../run"}`,
		`{"protocolVersion":"1","model":"openai/gpt-5.6-sol","operation":"read-files","goal":"inspect","continuity":"parallel"}`,
		`{"protocolVersion":"1","model":"openai/gpt-5.6-sol","operation":"read-files","goal":"inspect","childSessionId":"../child"}`,
	}
	for _, input := range tests {
		if _, err := DecodeDispatch(strings.NewReader(input)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("input %q: expected ErrInvalid, got %v", input, err)
		}
	}
	oversized := bytes.Repeat([]byte("x"), MaxRequestBytes+1)
	if _, err := DecodeDispatch(bytes.NewReader(oversized)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized request: %v", err)
	}
}

func TestValidateDispatchMatchesCanonicalBounds(t *testing.T) {
	request := DispatchRequest{ProtocolVersion: ProtocolVersion, Model: "openai/gpt-5.6-sol", Operation: ReadFiles, Goal: strings.Repeat("€", 8192), AcceptanceCriteria: make([]string, 32)}
	for index := range request.AcceptanceCriteria {
		request.AcceptanceCriteria[index] = "criterion"
	}
	if err := ValidateDispatch(request); err != nil {
		t.Fatalf("canonical boundary rejected: %v", err)
	}
	request.Goal += "x"
	if err := ValidateDispatch(request); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized goal accepted: %v", err)
	}
	request.Goal = "inspect"
	request.AcceptanceCriteria = append(request.AcceptanceCriteria, "extra")
	if err := ValidateDispatch(request); !errors.Is(err, ErrInvalid) {
		t.Fatalf("excess criteria accepted: %v", err)
	}
	request.Goal, request.AcceptanceCriteria = strings.Repeat("x", 8192)+" ", []string{"criterion"}
	if err := ValidateDispatch(request); !errors.Is(err, ErrInvalid) {
		t.Fatalf("whitespace-padded goal accepted: %v", err)
	}
	request.Goal, request.AcceptanceCriteria = "inspect", []string{strings.Repeat("x", 2048) + " "}
	if err := ValidateDispatch(request); !errors.Is(err, ErrInvalid) {
		t.Fatalf("whitespace-padded criterion accepted: %v", err)
	}
}

func TestFailureDoesNotExposeProviderDetails(t *testing.T) {
	failure := Failure(errors.New("secret provider stderr"))
	if failure.Code != "execution_failed" || strings.Contains(failure.Message, "secret") {
		t.Fatalf("unsafe failure normalization: %#v", failure)
	}
}

func TestDecodeNativeCompletionAndFailureAreExactAndBounded(t *testing.T) {
	completion, err := DecodeNativeCompletion(strings.NewReader(`{"protocolVersion":"1","ticketId":"ticket-1","parentSessionId":"ses_parent","childSessionId":"ses_child","messageId":"msg_child","result":{"kind":"agent.result"}}`))
	if err != nil || completion.TicketID != "ticket-1" || !json.Valid(completion.Result) {
		t.Fatalf("completion=%#v err=%v", completion, err)
	}
	failure, err := DecodeNativeFailure(strings.NewReader(`{"protocolVersion":"1","ticketId":"ticket-1","parentSessionId":"ses_parent","childSessionId":"ses_child","category":"native-subagent-failed"}`))
	if err != nil || failure.Category != "native-subagent-failed" {
		t.Fatalf("failure=%#v err=%v", failure, err)
	}
	for _, input := range []string{
		`{"protocolVersion":"1","ticketId":"../ticket","parentSessionId":"ses_parent","childSessionId":"ses_child","messageId":"msg_child","result":{}}`,
		`{"protocolVersion":"1","ticketId":"ticket-1","parentSessionId":"ses_parent","childSessionId":"ses_child","messageId":"msg_child","result":{},"extra":true}`,
		`{"protocolVersion":"1","ticketId":"ticket-1","parentSessionId":"ses_parent","category":"unknown"}`,
	} {
		if _, err := DecodeNativeCompletion(strings.NewReader(input)); err == nil {
			if _, failErr := DecodeNativeFailure(strings.NewReader(input)); failErr == nil {
				t.Fatalf("invalid native input accepted: %s", input)
			}
		}
	}
}

func TestDecodeNativeCompletionAllowsBoundedResultPlusEnvelope(t *testing.T) {
	result := json.RawMessage(`"` + strings.Repeat("x", MaxNativeResultBytes-2) + `"`)
	payload, err := json.Marshal(NativeCompletionRequest{
		ProtocolVersion: ProtocolVersion, TicketID: "ticket-1", ParentSessionID: "ses_parent",
		ChildSessionID: "ses_child", MessageID: "msg_child", Result: result,
	})
	if err != nil || len(payload) <= MaxNativeResultBytes || len(payload) > MaxNativeCompletionBytes {
		t.Fatalf("payload size=%d err=%v", len(payload), err)
	}
	decoded, err := DecodeNativeCompletion(bytes.NewReader(payload))
	if err != nil || len(decoded.Result) != MaxNativeResultBytes {
		t.Fatalf("decoded result size=%d err=%v", len(decoded.Result), err)
	}
}

func TestDecodeNativeReadRequiresOneCleanBoundedLocalFile(t *testing.T) {
	request, err := DecodeNativeRead(strings.NewReader(`{"protocolVersion":"1","ticketId":"ticket-1","childSessionId":"ses_child","path":"internal/app/app.go","offset":10,"limit":4096}`))
	if err != nil || request.Path != "internal/app/app.go" || request.Offset != 10 || request.Limit != 4096 {
		t.Fatalf("request=%#v err=%v", request, err)
	}
	for _, input := range []string{
		`{"protocolVersion":"1","ticketId":"ticket-1","childSessionId":"ses_child","path":"../secret"}`,
		`{"protocolVersion":"1","ticketId":"ticket-1","childSessionId":"ses_child","path":"internal/../go.mod"}`,
		`{"protocolVersion":"1","ticketId":"ticket-1","childSessionId":"ses_child","path":"internal/"}`,
		`{"protocolVersion":"1","ticketId":"ticket-1","childSessionId":"ses_child","path":"go.mod","limit":262145}`,
	} {
		if _, err := DecodeNativeRead(strings.NewReader(input)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("input %q: expected ErrInvalid, got %v", input, err)
		}
	}
}

func TestBridgeOutputBoundCoversWorstCaseNativeJSONEscaping(t *testing.T) {
	count := (MaxNativeResultBytes - 2) / len("\u2028")
	result := json.RawMessage(`"` + strings.Repeat("\u2028", count) + `"`)
	if len(result) > MaxNativeResultBytes || !json.Valid(result) {
		t.Fatalf("invalid test result size=%d", len(result))
	}
	var output bytes.Buffer
	if err := Encode(&output, Response{ProtocolVersion: ProtocolVersion, OK: true, Bridge: "healthy", Provider: "opencode", Status: "completed", Result: result}); err != nil {
		t.Fatal(err)
	}
	if output.Len() > MaxBridgeOutputBytes || MaxBridgeOutputBytes < 2*MaxNativeResultBytes+MaxRequestBytes {
		t.Fatalf("encoded output size=%d bound=%d", output.Len(), MaxBridgeOutputBytes)
	}
}
