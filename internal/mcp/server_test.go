package mcp

import (
	"context"
	"errors"
	"sort"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/sdd"
)

func TestServerProtocolDiscoveryListAndCall(t *testing.T) {
	backend := &fakeReader{project: "project-1"}
	server, err := newWithReader(context.Background(), "/workspace", backend)
	if err != nil {
		t.Fatalf("newWithReader() error = %v", err)
	}
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Run(ctx, serverTransport) }()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools.Tools) != 2 || tools.Tools[0].Name != "memory_recent" || tools.Tools[1].Name != "memory_search" {
		t.Fatalf("listed tools = %+v", tools.Tools)
	}
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "memory_search", Arguments: map[string]any{"query": "alpha"}})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if result.IsError || backend.recall.Project != "project-1" {
		t.Fatalf("CallTool() result = %+v, request = %+v", result, backend.recall)
	}
}

func TestServerBindsWorkspaceAndExposesOnlyReadTools(t *testing.T) {
	backend := &fakeReader{project: "project-1"}
	server, err := newWithReader(context.Background(), "/canonical/workspace", backend)
	if err != nil {
		t.Fatalf("newWithReader() error = %v", err)
	}
	if backend.workspace != "/canonical/workspace" {
		t.Fatalf("resolved workspace = %q", backend.workspace)
	}
	if _, err := server.recent(context.Background(), recentInput{Limit: 1}); err != nil {
		t.Fatalf("recent() error = %v", err)
	}
	if backend.recent.Project != "project-1" || backend.recent.Scope != memory.ScopeProject {
		t.Fatalf("recent request = %+v", backend.recent)
	}
}

func TestFullServerExposesMemoryParityTools(t *testing.T) {
	backend := &fakeReader{project: "project-1"}
	server, err := newFullWithReader(context.Background(), "/workspace", backend)
	if err != nil {
		t.Fatalf("newFullWithReader() error = %v", err)
	}
	want := []string{"memory_recent", "memory_search", "memory_get", "memory_save", "memory_forget", "sdd_create", "sdd_list", "sdd_get", "sdd_set_interaction_mode", "sdd_transition", "sdd_save_revision", "sdd_get_revision", "sdd_list_revisions", "sdd_accept_revision", "sdd_render_projection", "sdd_compare_projection", "sdd_record_projection", "sdd_projection_status"}
	sort.Strings(want)
	if got := discoveredNames(t, server); !sameStrings(got, want) {
		t.Fatalf("tool names = %v, want %v", got, want)
	}
}

func TestFullServerAdvertisesExactMutationSchemas(t *testing.T) {
	server, err := newFullWithReader(context.Background(), "/workspace", &fakeReader{project: "project-1"})
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Run(ctx, serverTransport) }()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "memory_get" || tool.Name == "memory_forget" {
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"id": {required: true, kind: "string"}})
		}
		if tool.Name == "memory_forget" && tool.Annotations.IdempotentHint {
			t.Fatal("memory_forget advertised as idempotent")
		}
		if tool.Name == "memory_save" {
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"title": {true, "string"}, "content": {true, "string"}, "type": {false, "string"}, "topic": {false, "string"}})
		}
		switch tool.Name {
		case "sdd_create":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"idempotencyKey": {true, "string"}, "title": {true, "string"}, "backend": {true, "string"}, "interactionMode": {true, "string"}, "plan": {true, "string"}})
		case "sdd_list":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"status": {false, "string"}, "limit": {false, "number"}})
		case "sdd_get":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"id": {true, "string"}})
		case "sdd_set_interaction_mode":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"changeId": {true, "string"}, "interactionMode": {true, "string"}, "expectedStateVersion": {true, "number"}})
		case "sdd_transition":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"changeId": {true, "string"}, "targetPhase": {false, "string"}, "cancel": {false, "boolean"}, "expectedStateVersion": {true, "number"}})
		case "sdd_save_revision":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"changeId": {true, "string"}, "artifact": {true, "string"}, "content": {true, "string"}, "externalLocation": {false, "string"}, "digest": {false, "string"}, "inputs": {false, "array"}, "inputDigest": {false, "string"}, "expectedStateVersion": {true, "number"}})
			assertRevisionBindingSchema(t, tool.InputSchema)
		case "sdd_get_revision", "sdd_render_projection":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"changeId": {true, "string"}, "revisionId": {true, "string"}})
		case "sdd_list_revisions":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"changeId": {true, "string"}, "artifact": {false, "string"}, "limit": {false, "number"}})
		case "sdd_accept_revision":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"changeId": {true, "string"}, "revisionId": {true, "string"}, "expectedStateVersion": {true, "number"}})
		case "sdd_compare_projection":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"changeId": {true, "string"}, "revisionId": {true, "string"}, "relativePath": {true, "string"}, "projectionContent": {false, "string"}, "missing": {false, "boolean"}, "symlink": {false, "boolean"}})
		case "sdd_record_projection":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"changeId": {true, "string"}, "artifactId": {true, "string"}, "revisionId": {true, "string"}, "status": {true, "string"}, "digest": {true, "string"}, "location": {true, "string"}, "expectedStateVersion": {true, "number"}})
			if tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
				t.Fatal("sdd_record_projection is not destructive")
			}
		case "sdd_projection_status":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"changeId": {true, "string"}, "artifactId": {true, "string"}})
		}
	}
}

func TestFullServerMemoryMutationsStayProjectScoped(t *testing.T) {
	backend := &fakeReader{project: "project-1", entry: memory.Entry{ID: "obs-1", Title: "Decision", Content: "durable", Project: "project-1", Scope: memory.ScopeProject, State: memory.StateActive}}
	server, err := newFullWithReader(context.Background(), "/workspace", backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.save(context.Background(), saveInput{Title: "Decision", Content: "durable"}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.get(context.Background(), getInput{ID: "obs-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.forget(context.Background(), getInput{ID: "obs-1"}); err != nil {
		t.Fatal(err)
	}
	if backend.remember.Project != "project-1" || backend.lookup.Project != "project-1" || backend.forget.Project != "project-1" || backend.remember.Scope != memory.ScopeProject || backend.lookup.Scope != memory.ScopeProject || backend.forget.Scope != memory.ScopeProject {
		t.Fatalf("requests were not project scoped: save=%+v get=%+v forget=%+v", backend.remember, backend.lookup, backend.forget)
	}
}

func TestFullServerMemoryErrorsAndCancellation(t *testing.T) {
	backend := &fakeReader{project: "project-1", getErr: memory.ErrNotFound}
	server, err := newFullWithReader(context.Background(), "/workspace", backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.get(context.Background(), getInput{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty get error = %v", err)
	}
	if _, err := server.get(context.Background(), getInput{ID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing get error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := server.save(ctx, saveInput{Title: "T", Content: "C"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled save error = %v", err)
	}
}

func TestFullServerSDDLifecycleBindsProjectAndMapsErrors(t *testing.T) {
	backend := &fakeReader{project: "project-1"}
	sdds := &fakeSDDReader{change: sdd.Change{ID: "change-1", Project: "project-1", StateVersion: 1}}
	server, err := newFullWithReaders(context.Background(), "/workspace", backend, sdds)
	if err != nil {
		t.Fatal(err)
	}
	create := sddCreateInput{IdempotencyKey: "key-1", Title: "Title", Backend: sdd.BackendMemory, InteractionMode: sdd.InteractionAutomatic, Plan: sdd.PlanLow}
	if _, err := server.sddCreate(context.Background(), create); err != nil {
		t.Fatal(err)
	}
	if _, err := server.sddList(context.Background(), sddListInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.sddGet(context.Background(), sddGetInput{ID: "change-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.sddSetInteractionMode(context.Background(), sddModeInput{ChangeID: "change-1", InteractionMode: sdd.InteractionInteractive, ExpectedStateVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.sddTransition(context.Background(), sddTransitionInput{ChangeID: "change-1", TargetPhase: sdd.PhaseProposal, ExpectedStateVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if sdds.create.Project != "project-1" || sdds.list.Project != "project-1" || sdds.get.Project != "project-1" || sdds.mode.Project != "project-1" || sdds.transition.Project != "project-1" {
		t.Fatalf("requests not project scoped: %+v", sdds)
	}
	sdds.modeErr = sdd.ErrStaleState
	if _, err := server.sddSetInteractionMode(context.Background(), sddModeInput{ChangeID: "change-1", InteractionMode: sdd.InteractionAutomatic, ExpectedStateVersion: 1}); !errors.Is(err, ErrStale) {
		t.Fatalf("stale error = %v", err)
	}
	sdds.transitionErr = sdd.ErrConflict
	if _, err := server.sddTransition(context.Background(), sddTransitionInput{ChangeID: "change-1", Cancel: true, ExpectedStateVersion: 1}); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict error = %v", err)
	}
	if _, err := server.sddCreate(context.Background(), sddCreateInput{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid create error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := server.sddGet(ctx, sddGetInput{ID: "change-1"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled get error = %v", err)
	}
}

func TestSDDNumericInputsRejectFractionsAndUnsafeVersions(t *testing.T) {
	for _, value := range []float64{1.5, 9007199254740992} {
		if _, err := sddVersion(value); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("sddVersion(%v) error = %v, want invalid", value, err)
		}
	}
	if _, err := sddLimit(1.5); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("sddLimit(1.5) error = %v, want invalid", err)
	}
	if got, err := sddVersion(9007199254740991); err != nil || got != 9007199254740991 {
		t.Fatalf("sddVersion(safe max) = %d, %v", got, err)
	}
}

func TestFullServerRemainingSDDOperations(t *testing.T) {
	backend := &fakeReader{project: "project-1"}
	sdds := &fakeSDDReader{revision: sdd.Revision{ID: "rev-1"}, projection: sdd.Projection{ArtifactID: "artifact-1"}}
	server, err := newFullWithReaders(context.Background(), "/workspace", backend, sdds)
	if err != nil {
		t.Fatal(err)
	}
	digest := sdd.ContentDigest([]byte("content"))
	if _, err := server.sddSaveRevision(context.Background(), sddSaveRevisionInput{ChangeID: "change-1", Artifact: sdd.PhaseExplore, Content: "content", Digest: digest, ExpectedStateVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.sddGetRevision(context.Background(), sddGetRevisionInput{ChangeID: "change-1", RevisionID: "rev-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.sddListRevisions(context.Background(), sddListRevisionsInput{ChangeID: "change-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.sddAcceptRevision(context.Background(), sddAcceptRevisionInput{ChangeID: "change-1", RevisionID: "rev-1", ExpectedStateVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.sddRenderProjection(context.Background(), sddRenderProjectionInput{ChangeID: "change-1", RevisionID: "rev-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.sddCompareProjection(context.Background(), sddCompareProjectionInput{ChangeID: "change-1", RevisionID: "rev-1", RelativePath: "openspec/changes/change-1/research.md", ProjectionContent: "content"}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.sddRecordProjection(context.Background(), sddRecordProjectionInput{ChangeID: "change-1", ArtifactID: "artifact-1", RevisionID: "rev-1", Status: sdd.ProjectionCurrent, Digest: digest, Location: "openspec/changes/change-1/research.md", ExpectedStateVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.sddProjectionStatus(context.Background(), sddProjectionStatusInput{ChangeID: "change-1", ArtifactID: "artifact-1"}); err != nil {
		t.Fatal(err)
	}
	if sdds.saveRevision.Project != "project-1" || sdds.compare.Project != "project-1" || sdds.record.Project != "project-1" {
		t.Fatalf("requests not project scoped: %+v", sdds)
	}
	if _, err := server.sddCompareProjection(context.Background(), sddCompareProjectionInput{ChangeID: "change-1", RevisionID: "rev-1", RelativePath: "x", Missing: true, Symlink: true}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid projection = %v", err)
	}
	if _, err := server.sddSaveRevision(context.Background(), sddSaveRevisionInput{ChangeID: "change-1", Artifact: sdd.PhaseExplore, Content: "content", Inputs: make([]sddRevisionBindingInput, 33), ExpectedStateVersion: 1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("oversized inputs error = %v", err)
	}
}

func TestSDDDigestMismatchIsInvalidAtToolBoundary(t *testing.T) {
	backend := &fakeReader{project: "project-1"}
	sdds := &fakeSDDReader{saveErr: sdd.ErrDigestMismatch}
	server, err := newFullWithReaders(context.Background(), "/workspace", backend, sdds)
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := server.callSDDSaveRevision(context.Background(), nil, sddSaveRevisionInput{ChangeID: "change-1", Artifact: sdd.PhaseExplore, Content: "content", ExpectedStateVersion: 1})
	if err != nil || !result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	text := result.Content[0].(*sdk.TextContent).Text
	if text != "invalid tool input" {
		t.Fatalf("text=%q", text)
	}
}

func TestSDDToolErrorsDistinguishCancelledNotFoundAndUnavailable(t *testing.T) {
	backend := &fakeReader{project: "project-1"}
	sdds := &fakeSDDReader{getErr: sdd.ErrChangeCancelled}
	server, err := newFullWithReaders(context.Background(), "/workspace", backend, sdds)
	if err != nil {
		t.Fatal(err)
	}
	assertSDDToolText(t, server, context.Background(), "SDD change is cancelled")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assertSDDToolText(t, server, ctx, "request cancelled")
	sdds.getErr = sdd.ErrNotFound
	assertSDDToolText(t, server, context.Background(), "SDD record not found")
	sdds.getErr = errors.New("storage path leak")
	assertSDDToolText(t, server, context.Background(), "SDD service unavailable")
}

func TestServerSearchValidationAndCancellation(t *testing.T) {
	backend := &fakeReader{project: "project-1"}
	server, err := newWithReader(context.Background(), "/workspace", backend)
	if err != nil {
		t.Fatalf("newWithReader() error = %v", err)
	}
	for _, input := range []searchInput{{}, {Query: "alpha", Limit: 51}} {
		if _, err := server.search(context.Background(), input); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("search(%+v) error = %v, want invalid input", input, err)
		}
	}
	if _, err := server.recent(context.Background(), recentInput{Limit: 51}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("recent() error = %v, want invalid input", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := server.search(ctx, searchInput{Query: "alpha"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled search error = %v", err)
	}
	if backend.recall.Project != "" {
		t.Fatal("cancelled search called backend")
	}
}

func TestNewRejectsAbsentStorageWithoutCreatingIt(t *testing.T) {
	workspace := t.TempDir()
	storage := t.TempDir()
	if _, err := New(context.Background(), workspace, config.Options{StorageRoot: storage}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("New() error = %v, want unavailable", err)
	}
}

func TestServerSanitizesUnavailableStorage(t *testing.T) {
	backend := &fakeReader{project: "project-1", recentErr: errors.New("sqlite /private/path failed")}
	server, err := newWithReader(context.Background(), "/workspace", backend)
	if err != nil {
		t.Fatalf("newWithReader() error = %v", err)
	}
	if _, err := server.recent(context.Background(), recentInput{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("recent() error = %v, want unavailable", err)
	}
	toolResult, _, err := server.callRecent(context.Background(), nil, recentInput{})
	if err != nil || !toolResult.IsError {
		t.Fatalf("callRecent() result = %+v, error = %v", toolResult, err)
	}
	text, ok := toolResult.Content[0].(*sdk.TextContent)
	if !ok || text.Text != "memory service unavailable" {
		t.Fatalf("error content = %#v", toolResult.Content)
	}
}

type fakeReader struct {
	project   string
	workspace string
	recent    memory.Recent
	recall    memory.Recall
	recentErr error
	entry     memory.Entry
	remember  memory.Remember
	lookup    memory.Lookup
	forget    memory.Forget
	getErr    error
}

type fakeSDDReader struct {
	change                         sdd.Change
	create                         sdd.CreateChangeRequest
	list                           sdd.ListChangesRequest
	get                            sdd.GetChangeRequest
	mode                           sdd.UpdateInteractionModeRequest
	transition                     sdd.TransitionChangeRequest
	getErr, modeErr, transitionErr error
	saveErr                        error
	revision                       sdd.Revision
	projection                     sdd.Projection
	saveRevision                   sdd.SaveRevisionRequest
	getRevision                    sdd.GetRevisionRequest
	listRevisions                  sdd.ListRevisionsRequest
	accept                         sdd.AcceptRevisionRequest
	render                         sdd.RenderProjectionRequest
	compare                        sdd.CompareProjectionRequest
	record                         sdd.RecordProjectionRequest
	projectionStatus               sdd.ProjectionStatusRequest
}

func (reader *fakeSDDReader) CreateChange(_ context.Context, request sdd.CreateChangeRequest) (sdd.Change, error) {
	reader.create = request
	return reader.change, nil
}
func (reader *fakeSDDReader) ListChanges(_ context.Context, request sdd.ListChangesRequest) ([]sdd.Change, error) {
	reader.list = request
	return []sdd.Change{reader.change}, nil
}
func (reader *fakeSDDReader) GetChange(_ context.Context, request sdd.GetChangeRequest) (sdd.Change, error) {
	reader.get = request
	return reader.change, reader.getErr
}

func assertSDDToolText(t *testing.T, server *Server, ctx context.Context, want string) {
	t.Helper()
	result, _, err := server.callSDDGet(ctx, nil, sddGetInput{ID: "change-1"})
	if err != nil || !result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	text, ok := result.Content[0].(*sdk.TextContent)
	if !ok || text.Text != want {
		t.Fatalf("text=%#v want=%q", result.Content, want)
	}
}
func (reader *fakeSDDReader) UpdateInteractionMode(_ context.Context, request sdd.UpdateInteractionModeRequest) (sdd.Change, error) {
	reader.mode = request
	return reader.change, reader.modeErr
}
func (reader *fakeSDDReader) TransitionChange(_ context.Context, request sdd.TransitionChangeRequest) (sdd.Change, error) {
	reader.transition = request
	return reader.change, reader.transitionErr
}
func (reader *fakeSDDReader) SaveRevision(_ context.Context, request sdd.SaveRevisionRequest) (sdd.Revision, error) {
	reader.saveRevision = request
	return reader.revision, reader.saveErr
}
func (reader *fakeSDDReader) GetRevision(_ context.Context, request sdd.GetRevisionRequest) (sdd.Revision, error) {
	reader.getRevision = request
	return reader.revision, nil
}
func (reader *fakeSDDReader) ListRevisions(_ context.Context, request sdd.ListRevisionsRequest) ([]sdd.Revision, error) {
	reader.listRevisions = request
	return []sdd.Revision{reader.revision}, nil
}
func (reader *fakeSDDReader) AcceptRevision(_ context.Context, request sdd.AcceptRevisionRequest) (sdd.Revision, error) {
	reader.accept = request
	return reader.revision, nil
}
func (reader *fakeSDDReader) RenderProjection(_ context.Context, request sdd.RenderProjectionRequest) (sdd.ProjectionDocument, error) {
	reader.render = request
	return sdd.ProjectionDocument{}, nil
}
func (reader *fakeSDDReader) CompareProjection(_ context.Context, request sdd.CompareProjectionRequest) (sdd.ProjectionComparison, error) {
	reader.compare = request
	return sdd.ProjectionComparison{}, nil
}
func (reader *fakeSDDReader) RecordProjection(_ context.Context, request sdd.RecordProjectionRequest) (sdd.Projection, error) {
	reader.record = request
	return reader.projection, nil
}
func (reader *fakeSDDReader) ProjectionStatus(_ context.Context, request sdd.ProjectionStatusRequest) (sdd.Projection, error) {
	reader.projectionStatus = request
	return reader.projection, nil
}

func (reader *fakeReader) ResolveProject(_ context.Context, workspace string) (string, error) {
	reader.workspace = workspace
	return reader.project, nil
}

func (reader *fakeReader) Recent(_ context.Context, request memory.Recent) ([]memory.Entry, error) {
	reader.recent = request
	return nil, reader.recentErr
}

func (reader *fakeReader) Recall(_ context.Context, request memory.Recall) ([]memory.Entry, error) {
	reader.recall = request
	return nil, nil
}

func (reader *fakeReader) Get(_ context.Context, request memory.Lookup) (memory.Entry, error) {
	reader.lookup = request
	return reader.entry, reader.getErr
}

func (reader *fakeReader) Remember(_ context.Context, request memory.Remember) (memory.Entry, error) {
	reader.remember = request
	return reader.entry, nil
}

func (reader *fakeReader) Forget(_ context.Context, request memory.Forget) (memory.Entry, error) {
	reader.forget = request
	return reader.entry, nil
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

type schemaExpectation struct {
	required bool
	kind     string
}

func assertSchemaProperties(t *testing.T, schema any, expected map[string]schemaExpectation) {
	t.Helper()
	value, ok := schema.(map[string]any)
	if !ok {
		t.Fatalf("schema type = %T", schema)
	}
	properties, ok := value["properties"].(map[string]any)
	if !ok || len(properties) != len(expected) {
		t.Fatalf("schema properties = %#v", value["properties"])
	}
	required, present := value["required"].([]any)
	if value["required"] != nil && !present {
		t.Fatalf("schema required = %#v", value["required"])
	}
	for name, expected := range expected {
		property, ok := properties[name].(map[string]any)
		if !ok {
			t.Errorf("missing schema property %q", name)
			continue
		}
		if !schemaHasType(property["type"], expected.kind) {
			t.Errorf("schema property %q type = %#v, want %s", name, property["type"], expected.kind)
		}
		found := false
		for _, field := range required {
			if field == name {
				found = true
			}
		}
		if found != expected.required {
			t.Errorf("schema property %q required = %v, want %v", name, found, expected.required)
		}
	}
}

func assertRevisionBindingSchema(t *testing.T, schema any) {
	t.Helper()
	root := schema.(map[string]any)
	properties := root["properties"].(map[string]any)
	input := properties["inputs"].(map[string]any)
	items := input["items"].(map[string]any)
	if reference, ok := items["$ref"].(string); ok {
		items = root["$defs"].(map[string]any)[reference[len("#/$defs/"):]].(map[string]any)
	}
	assertSchemaProperties(t, items, map[string]schemaExpectation{"artifactId": {true, "string"}, "revisionId": {true, "string"}, "digest": {true, "string"}})
}

func discoveredNames(t *testing.T, server *Server) []string {
	t.Helper()
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = server.Run(ctx, serverTransport) }()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(tools.Tools))
	for index, tool := range tools.Tools {
		names[index] = tool.Name
	}
	return names
}

func schemaHasType(value any, want string) bool {
	if value == want {
		return true
	}
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
