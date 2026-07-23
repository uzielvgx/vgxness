package contracts

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

const validCurrentRun = `{"schemaVersion":"1","id":"run-1","project":"vgxness","goal":"test","status":"running","phase":"apply","selectionId":"selection-1","decisionId":"decision-1","preflightId":"preflight-1","taskId":"task-1","lastEventId":"event-1","artifactIds":[],"storageMode":"project-local","startedAt":"2026-07-20T12:00:00Z","updatedAt":"2026-07-20T12:01:00Z"}`

func TestKernel_CompilesEmbeddedDraft2020ContractsOffline(t *testing.T) {
	kernel, err := NewKernel()
	if err != nil {
		t.Fatal(err)
	}
	if err := kernel.Validate(context.Background(), CurrentRunSchemaURI, []byte(validCurrentRun), false); err != nil {
		t.Fatal(err)
	}

	const artifact = `{"kind":"artifact.reference","schemaVersion":"1","provider":"memory","id":"obs-1","artifactType":"memory.observation","provenance":{"producer":"test","createdAt":"2026-07-20T12:00:00Z"}}`
	if err := kernel.Validate(context.Background(), CommonSchemaURI+"#/$defs/artifactReference", []byte(artifact), false); err != nil {
		t.Fatal(err)
	}

	const prompts = `{"schemaVersion":"1","version":"prompts-v1","generatedAt":"2026-07-20T12:00:00Z","prompts":[{"schemaVersion":"1","id":"manager-main","version":"1","audience":"manager","instructions":"Coordinate bounded work.","personality":{"identity":"VGXNESS manager","voice":"clear","traits":["curious"],"interactionStyle":"collaborative"},"provenance":{"producer":"test","createdAt":"2026-07-20T12:00:00Z"}}]}`
	if err := kernel.Validate(context.Background(), PromptsSchemaURI, []byte(prompts), false); err != nil {
		t.Fatal(err)
	}
}

func TestKernel_AgentResultAcceptsBoundedMemoryCandidates(t *testing.T) {
	kernel, err := NewKernel()
	if err != nil {
		t.Fatal(err)
	}
	valid := []byte(`{"kind":"agent.result","schemaVersion":"1","resultId":"result-1","taskId":"task-1","agentId":"agent-1","status":"success","summary":"done","artifacts":[],"nextRecommended":"none","risks":[],"errors":[],"memoryCandidates":[{"type":"architecture","title":"Runtime authority","content":"VGXNESS owns runtime authority.","topicKey":"runtime-authority","reason":"Verified in bounded evidence.","confidence":0.95}]}`)
	if err := kernel.Validate(context.Background(), ExecutionSchemaURI+"#/$defs/agentResult", valid, false); err != nil {
		t.Fatal(err)
	}
	invalid := []byte(strings.Replace(string(valid), `"confidence":0.95`, `"confidence":2`, 1))
	if err := kernel.Validate(context.Background(), ExecutionSchemaURI+"#/$defs/agentResult", invalid, false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid candidate confidence was accepted: %v", err)
	}
}

func TestKernel_ReturnsProviderNeutralContractFailure(t *testing.T) {
	kernel, err := NewKernel()
	if err != nil {
		t.Fatal(err)
	}
	invalid := []byte(strings.Replace(validCurrentRun, "2026-07-20T12:00:00Z", "yesterday", 1))
	err = kernel.Validate(context.Background(), CurrentRunSchemaURI, invalid, true)
	var failure *ContractError
	if !errors.Is(err, ErrInvalid) || !errors.As(err, &failure) {
		t.Fatalf("expected contract error, got %v", err)
	}
	if failure.Kind != "contract.invalid" || failure.Code != "contract.invalid" || failure.SchemaVersion != "1" || failure.SchemaURI != CurrentRunSchemaURI || failure.Pointer != "/startedAt" || !failure.Recoverable || failure.Message == "" {
		t.Fatalf("unexpected failure: %+v", failure)
	}
	encoded, err := json.Marshal(failure)
	if err != nil {
		t.Fatal(err)
	}
	if err := kernel.Validate(context.Background(), CommonSchemaURI+"#/$defs/contractError", encoded, false); err != nil {
		t.Fatalf("failure does not satisfy its contract: %v", err)
	}
}

func TestKernel_DoesNotExposeRejectedValues(t *testing.T) {
	kernel, err := NewKernel()
	if err != nil {
		t.Fatal(err)
	}
	const secret = "SECRET TOKEN"
	document := []byte(`{"kind":"artifact.reference","schemaVersion":"1","provider":"` + secret + `","id":"obs-1","artifactType":"memory.observation","provenance":{"producer":"test","createdAt":"2026-07-20T12:00:00Z"}}`)
	err = kernel.Validate(context.Background(), CommonSchemaURI+"#/$defs/artifactReference", document, false)
	var failure *ContractError
	if !errors.As(err, &failure) || failure.Pointer != "/provider" || strings.Contains(failure.Message, secret) {
		t.Fatalf("rejected value leaked through failure: %+v", failure)
	}
}

func TestKernel_RejectsInvalidJSONUnknownSchemaAndCancellation(t *testing.T) {
	kernel, err := NewKernel()
	if err != nil {
		t.Fatal(err)
	}
	for name, document := range map[string][]byte{
		"malformed": []byte(`{"schemaVersion":`),
		"trailing":  []byte(validCurrentRun + ` {}`),
	} {
		t.Run(name, func(t *testing.T) {
			err := kernel.Validate(context.Background(), CurrentRunSchemaURI, document, false)
			var failure *ContractError
			if !errors.As(err, &failure) || failure.Pointer != "" {
				t.Fatalf("expected root contract failure, got %v", err)
			}
		})
	}
	if err := kernel.Validate(context.Background(), schemaBaseURI+"missing.schema.json", []byte(`{}`), false); !errors.Is(err, ErrUnknownSchema) {
		t.Fatalf("expected unknown schema, got %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := kernel.Validate(cancelled, CurrentRunSchemaURI, []byte(validCurrentRun), false); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestKernel_ConcurrentFragmentValidation(t *testing.T) {
	kernel, err := NewKernel()
	if err != nil {
		t.Fatal(err)
	}
	const workers = 8
	var wait sync.WaitGroup
	errorsFound := make(chan error, workers)
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errorsFound <- kernel.Validate(context.Background(), CurrentRunSchemaURI, []byte(validCurrentRun), false)
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
}
