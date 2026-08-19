package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/sdd"
)

type fakeSDDRuntime struct {
	project     string
	created     sdd.CreateChangeRequest
	modeUpdated sdd.UpdateInteractionModeRequest
	called      string
}

func (f *fakeSDDRuntime) ResolveSDDProject(context.Context, config.Options, string) (string, error) {
	return f.project, nil
}
func (f *fakeSDDRuntime) CreateChange(_ context.Context, _ config.Options, request sdd.CreateChangeRequest) (sdd.Change, error) {
	f.created = request
	f.called = "create"
	return sdd.Change{ID: "change-1", Project: request.Project, Title: request.Title}, nil
}
func (f *fakeSDDRuntime) ListChanges(context.Context, config.Options, sdd.ListChangesRequest) ([]sdd.Change, error) {
	f.called = "list"
	return []sdd.Change{}, nil
}
func (f *fakeSDDRuntime) GetChange(context.Context, config.Options, sdd.GetChangeRequest) (sdd.Change, error) {
	f.called = "get"
	return sdd.Change{}, nil
}
func (f *fakeSDDRuntime) UpdateInteractionMode(_ context.Context, _ config.Options, request sdd.UpdateInteractionModeRequest) (sdd.Change, error) {
	f.modeUpdated = request
	f.called = "set-interaction-mode"
	return sdd.Change{ID: request.ChangeID, InteractionMode: request.InteractionMode}, nil
}
func (f *fakeSDDRuntime) SaveRevision(context.Context, config.Options, sdd.SaveRevisionRequest) (sdd.Revision, error) {
	f.called = "save-revision"
	return sdd.Revision{}, nil
}
func (f *fakeSDDRuntime) GetRevision(context.Context, config.Options, sdd.GetRevisionRequest) (sdd.Revision, error) {
	f.called = "get-revision"
	return sdd.Revision{}, nil
}
func (f *fakeSDDRuntime) ListRevisions(context.Context, config.Options, sdd.ListRevisionsRequest) ([]sdd.Revision, error) {
	f.called = "list-revisions"
	return nil, nil
}
func (f *fakeSDDRuntime) AcceptRevision(context.Context, config.Options, sdd.AcceptRevisionRequest) (sdd.Revision, error) {
	f.called = "accept-revision"
	return sdd.Revision{}, nil
}
func (f *fakeSDDRuntime) TransitionChange(context.Context, config.Options, sdd.TransitionChangeRequest) (sdd.Change, error) {
	f.called = "transition"
	return sdd.Change{}, nil
}
func (f *fakeSDDRuntime) ProjectionStatus(context.Context, config.Options, sdd.ProjectionStatusRequest) (sdd.Projection, error) {
	f.called = "projection-status"
	return sdd.Projection{}, nil
}
func (f *fakeSDDRuntime) RecordProjection(context.Context, config.Options, sdd.RecordProjectionRequest) (sdd.Projection, error) {
	f.called = "record-projection"
	return sdd.Projection{}, nil
}
func (f *fakeSDDRuntime) RenderProjection(context.Context, config.Options, sdd.RenderProjectionRequest) (sdd.ProjectionDocument, error) {
	f.called = "render-projection"
	return sdd.ProjectionDocument{RelativePath: "openspec/changes/change-1/proposal.md"}, nil
}
func (f *fakeSDDRuntime) CompareProjection(context.Context, config.Options, sdd.CompareProjectionRequest) (sdd.ProjectionComparison, error) {
	f.called = "compare-projection"
	return sdd.ProjectionComparison{State: sdd.DriftSynced}, nil
}

func TestRunSDDCreateJSONContract(t *testing.T) {
	runtime := &fakeSDDRuntime{project: "trusted-project"}
	input := bytes.NewBufferString(`{"schemaVersion":1,"idempotencyKey":"create-feature-1","title":"Feature","backend":"hybrid","interactionMode":"interactive","plan":"high"}`)
	var stdout, stderr bytes.Buffer
	code := runSDD(context.Background(), []string{"create", "--stdin", "--json", "--workspace", t.TempDir()}, input, &stdout, &stderr, runtime)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if runtime.created.Project != "trusted-project" || runtime.created.Title != "Feature" {
		t.Fatalf("request=%+v", runtime.created)
	}
	var envelope struct {
		SchemaVersion int        `json:"schemaVersion"`
		Result        sdd.Change `json:"result"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || envelope.SchemaVersion != 1 || envelope.Result.ID != "change-1" {
		t.Fatalf("output=%q envelope=%+v err=%v", stdout.String(), envelope, err)
	}
}

func TestRunSDDRequiredOperations(t *testing.T) {
	digest := sdd.ContentDigest([]byte("content"))
	tests := []struct {
		operation string
		body      string
	}{
		{"list", `{"schemaVersion":1}`},
		{"get", `{"schemaVersion":1,"id":"change-1"}`},
		{"set-interaction-mode", `{"schemaVersion":1,"changeId":"change-1","interactionMode":"interactive","expectedStateVersion":1}`},
		{"get-revision", `{"schemaVersion":1,"changeId":"change-1","revisionId":"revision-1"}`},
		{"list-revisions", `{"schemaVersion":1,"changeId":"change-1","artifact":"proposal"}`},
		{"save-revision", `{"schemaVersion":1,"changeId":"change-1","artifact":"proposal","content":"content","expectedStateVersion":1}`},
		{"accept-revision", `{"schemaVersion":1,"changeId":"change-1","revisionId":"revision-1","expectedStateVersion":2}`},
		{"transition", `{"schemaVersion":1,"changeId":"change-1","targetPhase":"proposal","expectedStateVersion":1}`},
		{"projection-status", `{"schemaVersion":1,"changeId":"change-1","artifactId":"artifact-1"}`},
		{"record-projection", `{"schemaVersion":1,"changeId":"change-1","artifactId":"artifact-1","revisionId":"revision-1","status":"current","digest":"` + string(digest) + `","location":"openspec/proposal.md","expectedStateVersion":3}`},
		{"render-projection", `{"schemaVersion":1,"changeId":"change-1","revisionId":"revision-1"}`},
		{"compare-projection", `{"schemaVersion":1,"changeId":"change-1","revisionId":"revision-1","relativePath":"openspec/changes/change-1/proposal.md","projectionContent":"managed bytes"}`},
	}
	for _, test := range tests {
		t.Run(test.operation, func(t *testing.T) {
			runtime := &fakeSDDRuntime{project: "trusted-project"}
			var stdout, stderr bytes.Buffer
			code := runSDD(context.Background(), []string{test.operation, "--stdin", "--json", "--workspace", t.TempDir()}, bytes.NewBufferString(test.body), &stdout, &stderr, runtime)
			if code != 0 || runtime.called != test.operation {
				t.Fatalf("code=%d called=%q stderr=%q", code, runtime.called, stderr.String())
			}
		})
	}
}

func TestRunSDDSaveExternalRevisionContract(t *testing.T) {
	runtime := &fakeSDDRuntime{project: "trusted-project"}
	input := bytes.NewBufferString(`{"schemaVersion":1,"changeId":"change-1","artifact":"proposal","content":"content","externalLocation":"openspec/changes/change-1/proposal.md","expectedStateVersion":1}`)
	var stdout, stderr bytes.Buffer
	code := runSDD(context.Background(), []string{"save-revision", "--stdin", "--json", "--workspace", t.TempDir()}, input, &stdout, &stderr, runtime)
	if code != 0 || runtime.called != "save-revision" {
		t.Fatalf("code=%d called=%q stderr=%q", code, runtime.called, stderr.String())
	}
}

func TestRunSDDRejectsUntrustedProjectAndUnknownFields(t *testing.T) {
	runtime := &fakeSDDRuntime{project: "trusted-project"}
	for _, body := range []string{
		`{"schemaVersion":1,"project":"caller-project","idempotencyKey":"create-1","title":"Feature","backend":"memory","interactionMode":"automatic","plan":"low"}`,
		`{"schemaVersion":1,"idempotencyKey":"create-1","title":"Feature","backend":"memory","interactionMode":"automatic","plan":"low","extra":true}`,
		`{"schemaVersion":1,"idempotencyKey":"create-1","title":"Feature","title":"Replacement","backend":"memory","interactionMode":"automatic","plan":"low"}`,
	} {
		var stdout, stderr bytes.Buffer
		code := runSDD(context.Background(), []string{"create", "--stdin", "--json", "--workspace", t.TempDir()}, bytes.NewBufferString(body), &stdout, &stderr, runtime)
		if code != 2 || stdout.Len() != 0 {
			t.Fatalf("body=%s code=%d stdout=%q stderr=%q", body, code, stdout.String(), stderr.String())
		}
	}
}

func TestRunSDDReportsActionableInvocationAndInputDiagnostics(t *testing.T) {
	runtime := &fakeSDDRuntime{project: "trusted-project"}
	for _, test := range []struct {
		name string
		args []string
		body string
		want string
	}{
		{"unsupported operation", []string{"unknown"}, "", "invalid: unsupported SDD operation\n"},
		{"missing stdin", []string{"create", "--json", "--workspace", t.TempDir()}, "", "invalid: use --stdin to provide SDD JSON input\n"},
		{"missing workspace", []string{"create", "--stdin", "--json"}, `{}`, "invalid: provide --workspace for the trusted project\n"},
		{"malformed JSON", []string{"create", "--stdin", "--json", "--workspace", t.TempDir()}, `{`, "invalid: provide one SDD JSON object with schemaVersion 1\n"},
		{"schema version", []string{"create", "--stdin", "--json", "--workspace", t.TempDir()}, `{"schemaVersion":2}`, "invalid: schemaVersion must be 1 and project is resolved from --workspace\n"},
		{"invalid operation field", []string{"get", "--stdin", "--json", "--workspace", t.TempDir()}, `{"schemaVersion":1,"id":"change-1","title":"wrong"}`, "invalid: field title is not accepted by SDD get\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := runSDD(context.Background(), test.args, bytes.NewBufferString(test.body), &stdout, &stderr, runtime)
			if code != 2 || stdout.Len() != 0 || stderr.String() != test.want {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestSDDFailurePreservesSafeCategory(t *testing.T) {
	for _, test := range []struct {
		err      error
		category string
	}{
		{sdd.ErrInvalid, "invalid:"},
		{sdd.ErrNotFound, "not_found:"},
		{sdd.ErrConflict, "conflict:"},
		{sdd.ErrStaleState, "stale:"},
		{sdd.ErrChangeCancelled, "cancelled:"},
		{errors.New("private storage detail"), "operational:"},
	} {
		var stderr bytes.Buffer
		sddFailure(&stderr, test.err)
		if !strings.HasPrefix(stderr.String(), test.category) || strings.Contains(stderr.String(), "private storage detail") {
			t.Fatalf("error=%v output=%q", test.err, stderr.String())
		}
	}
}
