package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

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
	toolNames := make([]string, len(tools.Tools))
	for index, tool := range tools.Tools {
		toolNames[index] = tool.Name
	}
	sort.Strings(toolNames)
	if !sameStrings(toolNames, []string{"memory_context", "memory_recent", "memory_search"}) {
		t.Fatalf("listed tools = %+v", tools.Tools)
	}
	var searchSchema map[string]any
	for _, tool := range tools.Tools {
		if tool.Name == "memory_search" {
			searchSchema, _ = tool.InputSchema.(map[string]any)
		}
	}
	if searchSchema == nil {
		t.Fatal("memory_search schema missing")
	}
	if additional, ok := searchSchema["additionalProperties"].(bool); !ok || additional {
		t.Fatalf("memory_search additionalProperties = %#v, want false", searchSchema["additionalProperties"])
	}
	searchProperties := searchSchema["properties"].(map[string]any)
	matchMode := searchProperties["match_mode"].(map[string]any)
	if got := matchMode["enum"]; !sameAnyStrings(got, []string{"all", "any"}) {
		t.Fatalf("memory_search match_mode enum = %#v", got)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "memory_get" || tool.Name == "memory_sync" || isMutationTool(tool.Name) {
			t.Fatalf("normal mode exposed protected tool %q", tool.Name)
		}
	}
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "memory_search", Arguments: map[string]any{"query": "alpha and beta"}})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if result.IsError || backend.recall.Project != "project-1" {
		t.Fatalf("CallTool() result = %+v, request = %+v", result, backend.recall)
	}
	if backend.recall.MatchAny || backend.recall.Query != "alpha and beta" {
		t.Fatalf("memory_search request = %+v", backend.recall)
	}
	result, err = session.CallTool(ctx, &sdk.CallToolParams{Name: "memory_search", Arguments: map[string]any{"query": "alpha beta", "match_mode": "any"}})
	if err != nil || result.IsError {
		t.Fatalf("CallTool(match_mode:any) result = %+v, error = %v", result, err)
	}
	if backend.recallCalls != 2 || !backend.recall.MatchAny {
		t.Fatalf("CallTool(match_mode:any) calls = %d, request = %+v", backend.recallCalls, backend.recall)
	}
	for name, arguments := range map[string]map[string]any{
		"invalid match mode": {"query": "alpha beta", "match_mode": "phrase"},
		"unknown field":      {"query": "alpha beta", "unexpected": true},
	} {
		rejected, rejectedErr := session.CallTool(ctx, &sdk.CallToolParams{Name: "memory_search", Arguments: arguments})
		if rejectedErr == nil && (rejected == nil || !rejected.IsError) {
			t.Errorf("%s input was accepted: result=%+v error=%v", name, rejected, rejectedErr)
		}
		if backend.recallCalls != 2 {
			t.Fatalf("%s input reached Recall: calls=%d", name, backend.recallCalls)
		}
	}
	for _, name := range append([]string{"memory_get"}, mutationToolNames...) {
		protected, protectedErr := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: map[string]any{"id": "entry-1"}})
		if protectedErr == nil && !protected.IsError {
			t.Fatalf("normal mode invoked unregistered tool %q: %+v", name, protected)
		}
	}
	if backend.getCalls != 0 || backend.rememberCalls != 0 || backend.forgetCalls != 0 {
		t.Fatalf("normal mode reached protected memory backend: get=%d save=%d forget=%d", backend.getCalls, backend.rememberCalls, backend.forgetCalls)
	}
}

func TestSearchMatchModeControlsRecallAndRejectsInvalidBeforeBackend(t *testing.T) {
	for _, test := range []struct {
		name      string
		matchMode string
		matchAny  bool
		wantErr   error
	}{
		{name: "omitted uses all"},
		{name: "all", matchMode: "all"},
		{name: "any", matchMode: "any", matchAny: true},
		{name: "invalid", matchMode: "phrase", wantErr: ErrInvalidInput},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := &fakeReader{project: "project-1"}
			server, err := newWithReader(context.Background(), "/workspace", backend)
			if err != nil {
				t.Fatal(err)
			}
			_, err = server.search(context.Background(), searchInput{Query: "alpha beta", MatchMode: test.matchMode})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("search() error = %v, want %v", err, test.wantErr)
			}
			wantCalls := 1
			if test.wantErr != nil {
				wantCalls = 0
			}
			if backend.recallCalls != wantCalls {
				t.Fatalf("Recall calls = %d, want %d", backend.recallCalls, wantCalls)
			}
			if test.wantErr == nil && backend.recall.MatchAny != test.matchAny {
				t.Fatalf("Recall MatchAny = %v, want %v", backend.recall.MatchAny, test.matchAny)
			}
		})
	}
}

func TestFullServerProtocolListResultsUseObjectEnvelopes(t *testing.T) {
	backend := &fakeReader{project: "project-1"}
	sdds := &fakeSDDReader{change: sdd.Change{ID: "change-1", Project: "project-1"}, revision: sdd.Revision{ID: "revision-1", ChangeID: "change-1"}}
	server, err := newFullWithReaders(context.Background(), "/workspace", backend, sdds)
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Run(ctx, serverTransport) }()
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		args map[string]any
		key  string
	}{
		{"sdd_list", nil, "changes"},
		{"sdd_list_revisions", map[string]any{"changeId": "change-1"}, "revisions"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, callErr := session.CallTool(ctx, &sdk.CallToolParams{Name: test.name, Arguments: test.args})
			if callErr != nil || result.IsError {
				t.Fatalf("CallTool() result=%+v err=%v", result, callErr)
			}
			encoded, marshalErr := json.Marshal(result.StructuredContent)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			var envelope map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &envelope); err != nil || len(envelope) != 1 || envelope[test.key] == nil {
				t.Fatalf("structured content=%s err=%v", encoded, err)
			}
			var values []json.RawMessage
			if err := json.Unmarshal(envelope[test.key], &values); err != nil {
				t.Fatalf("structured content field %q=%s is not an array: %v", test.key, envelope[test.key], err)
			}
		})
	}
}

func TestFullServerProtocolMemoryResultsExposeCanonicalJSONTextAndSchemas(t *testing.T) {
	updatedAt := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	entry := memory.Entry{ID: "entry-1", Title: "Decision", Type: "observation", TopicKey: "testing", State: memory.StateActive, Content: "private content", Preview: "private preview", References: []string{"ref-1"}, UpdatedAt: updatedAt}
	backend := &fakeReader{project: "project-1", entries: []memory.Entry{entry}, entry: entry, handoff: "UNTRUSTED DATA\nprior"}
	server, err := newFullWithReader(context.Background(), "/workspace", backend)
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Run(ctx, serverTransport) }()
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if strings.HasPrefix(tool.Name, "memory_") && tool.OutputSchema == nil {
			t.Errorf("%s omits output schema", tool.Name)
		}
		switch tool.Name {
		case "memory_recent", "memory_search":
			assertSchemaProperties(t, tool.OutputSchema, map[string]schemaExpectation{"entries": {required: true, kind: "array"}})
		case "memory_get", "memory_save", "memory_update", "memory_forget":
			assertSchemaProperties(t, tool.OutputSchema, map[string]schemaExpectation{"id": {true, "string"}, "title": {true, "string"}, "type": {true, "string"}, "topicKey": {false, "string"}, "state": {true, "string"}, "preview": {true, "string"}, "updatedAt": {true, "string"}, "content": {false, "string"}, "references": {false, "array"}})
		case "memory_context":
			assertSchemaProperties(t, tool.OutputSchema, map[string]schemaExpectation{"handoff": {required: true, kind: "string"}})
		case "memory_session_summary":
			assertSchemaProperties(t, tool.OutputSchema, map[string]schemaExpectation{"status": {true, "string"}, "updated_at": {true, "string"}})
		}
	}
	for _, test := range []struct {
		name   string
		args   map[string]any
		secret string
	}{
		{"memory_recent", nil, "private content"},
		{"memory_search", map[string]any{"query": "decision"}, "private content"},
		{"memory_get", map[string]any{"id": "entry-1"}, ""},
		{"memory_save", map[string]any{"title": "Decision", "content": "private content"}, ""},
		{"memory_update", map[string]any{"id": "entry-1", "content": "private content", "expected_updated_at": updatedAt.Format(time.RFC3339)}, ""},
		{"memory_forget", map[string]any{"id": "entry-1"}, ""},
		{"memory_context", map[string]any{"session_handle": "ps-1"}, ""},
		{"memory_session_summary", map[string]any{"session_handle": "ps-1", "summary": "draft"}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, callErr := session.CallTool(ctx, &sdk.CallToolParams{Name: test.name, Arguments: test.args})
			if callErr != nil || got.IsError {
				t.Fatalf("CallTool() result=%+v err=%v", got, callErr)
			}
			text, ok := got.Content[0].(*sdk.TextContent)
			if !ok || !json.Valid([]byte(text.Text)) {
				t.Fatalf("content=%#v", got.Content)
			}
			encoded, err := json.Marshal(got.StructuredContent)
			if err != nil || string(encoded) != text.Text {
				t.Fatalf("text=%q structured=%s err=%v", text.Text, encoded, err)
			}
			if test.secret != "" && strings.Contains(text.Text, test.secret) {
				t.Fatalf("text leaked full content: %q", text.Text)
			}
		})
	}
}

func TestFullServerProtocolSDDChangeResultsIncludeDeterministicJSONText(t *testing.T) {
	backend := &fakeReader{project: "project-1"}
	sdds := &fakeSDDReader{change: sdd.Change{ID: "change-1", Project: "project-1", Title: "Title", StateVersion: 1}}
	server, err := newFullWithReaders(context.Background(), "/workspace", backend, sdds)
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Run(ctx, serverTransport) }()
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, tool := range []string{"sdd_create", "sdd_get", "sdd_set_interaction_mode", "sdd_transition"} {
		t.Run(tool, func(t *testing.T) {
			args := map[string]any{"id": "change-1"}
			if tool == "sdd_create" {
				args = map[string]any{"idempotencyKey": "key-1", "title": "Title", "backend": "memory", "interactionMode": "automatic", "plan": "low"}
			} else if tool != "sdd_get" {
				args = map[string]any{"changeId": "change-1", "expectedStateVersion": 1}
				if tool == "sdd_set_interaction_mode" {
					args["interactionMode"] = "interactive"
				} else {
					args["targetPhase"] = "proposal"
				}
			}
			result, callErr := session.CallTool(ctx, &sdk.CallToolParams{Name: tool, Arguments: args})
			if callErr != nil || result.IsError {
				t.Fatalf("CallTool() result=%+v err=%v", result, callErr)
			}
			text, ok := result.Content[0].(*sdk.TextContent)
			if !ok {
				t.Fatalf("content=%#v", result.Content)
			}
			var change sdd.Change
			if err := json.Unmarshal([]byte(text.Text), &change); err != nil || change.ID != "change-1" || change.Project != "project-1" {
				t.Fatalf("text=%q change=%+v err=%v", text.Text, change, err)
			}
			if text.Text != `{"id":"change-1","project":"project-1","title":"Title","backend":"","interactionMode":"","plan":"","phase":"","status":"","stateVersion":1,"createdAt":"0001-01-01T00:00:00Z","updatedAt":"0001-01-01T00:00:00Z"}` {
				t.Fatalf("text is not deterministic: %q", text.Text)
			}
		})
	}
}

func TestFullServerProtocolSDDLifecycleVisibleJSONContinuity(t *testing.T) {
	backend := &fakeReader{project: "project-1"}
	sdds := &fakeSDDReader{lifecycle: true}
	server, err := newFullWithReaders(context.Background(), "/workspace", backend, sdds)
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Run(ctx, serverTransport) }()
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	call := func(name string, arguments map[string]any, output any) {
		t.Helper()
		result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: arguments})
		if err != nil || result.IsError {
			t.Fatalf("%s result=%+v err=%v", name, result, err)
		}
		text := result.Content[0].(*sdk.TextContent).Text
		if err := json.Unmarshal([]byte(text), output); err != nil {
			t.Fatalf("%s visible text=%q: %v", name, text, err)
		}
	}
	var change, fetched, transitioned sdd.Change
	call("sdd_create", map[string]any{"idempotencyKey": "journey-1", "title": "Journey", "backend": "memory", "interactionMode": "automatic", "plan": "low"}, &change)
	call("sdd_get", map[string]any{"id": change.ID}, &fetched)
	if change.ID == "" || fetched.ID != change.ID || fetched.StateVersion != change.StateVersion {
		t.Fatalf("create/get continuity: create=%+v get=%+v", change, fetched)
	}
	var revision, accepted sdd.Revision
	call("sdd_save_revision", map[string]any{"changeId": change.ID, "artifact": "explore", "content": "research", "expectedStateVersion": change.StateVersion}, &revision)
	call("sdd_accept_revision", map[string]any{"changeId": change.ID, "revisionId": revision.ID, "expectedStateVersion": revision.StateVersion}, &accepted)
	call("sdd_transition", map[string]any{"changeId": change.ID, "targetPhase": "proposal", "expectedStateVersion": accepted.StateVersion}, &transitioned)
	if revision.ID == "" || revision.ChangeID != change.ID || revision.Digest == "" || accepted.ID != revision.ID || accepted.Status != sdd.RevisionAccepted || transitioned.ID != change.ID || transitioned.StateVersion != accepted.StateVersion+1 {
		t.Fatalf("revision/transition continuity: revision=%+v accepted=%+v transitioned=%+v", revision, accepted, transitioned)
	}
	var changes struct {
		Changes []sdd.Change `json:"changes"`
	}
	call("sdd_list", nil, &changes)
	if len(changes.Changes) != 1 || changes.Changes[0].ID != change.ID {
		t.Fatalf("visible list=%+v", changes)
	}
	var revisions struct {
		Revisions []sdd.Revision `json:"revisions"`
	}
	call("sdd_list_revisions", map[string]any{"changeId": change.ID}, &revisions)
	if len(revisions.Revisions) != 1 || revisions.Revisions[0].ID != revision.ID || revisions.Revisions[0].Digest != revision.Digest {
		t.Fatalf("visible revisions=%+v", revisions)
	}
	var projection, status sdd.Projection
	call("sdd_record_projection", map[string]any{"changeId": change.ID, "artifactId": revision.ArtifactID, "revisionId": revision.ID, "status": "current", "digest": string(revision.Digest), "location": "openspec/changes/change-1/explore.md", "expectedStateVersion": transitioned.StateVersion}, &projection)
	call("sdd_projection_status", map[string]any{"changeId": change.ID, "artifactId": revision.ArtifactID}, &status)
	if projection.ChangeID != change.ID || projection.ArtifactID != revision.ArtifactID || projection.RevisionID != revision.ID || projection.Digest != revision.Digest || status != projection {
		t.Fatalf("projection continuity: record=%+v status=%+v", projection, status)
	}
}

func TestFullServerProtocolSDDValidationIsSafeAndFieldSpecific(t *testing.T) {
	server, err := newFullWithReaders(context.Background(), "/workspace", &fakeReader{project: "project-1"}, &fakeSDDReader{})
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Run(ctx, serverTransport) }()
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string]map[string]any{
		"enum":          {"idempotencyKey": "key-1", "title": "Title", "backend": "invalid", "interactionMode": "automatic", "plan": "low"},
		"state version": {"changeId": "change-1", "interactionMode": "automatic", "expectedStateVersion": 1.5},
	} {
		t.Run(name, func(t *testing.T) {
			tool := "sdd_create"
			if name == "state version" {
				tool = "sdd_set_interaction_mode"
			}
			result, callErr := session.CallTool(ctx, &sdk.CallToolParams{Name: tool, Arguments: args})
			if callErr == nil {
				if result == nil || !result.IsError {
					t.Fatalf("invalid %s accepted: result=%+v", name, result)
				}
				text := result.Content[0].(*sdk.TextContent).Text
				if name == "state version" && text != "invalid tool input: expectedStateVersion" {
					t.Fatalf("state version text=%q", text)
				}
			}
		})
	}
}

func TestFullServerProtocolRenderContentCanBeComparedUnchanged(t *testing.T) {
	backend := &fakeReader{project: "project-1"}
	sdds := &fakeSDDReader{rendered: sdd.ProjectionDocument{RelativePath: "openspec/changes/change-1/explore.md", Content: []byte("<!-- managed -->\nresearch\n"), Digest: sdd.ContentDigest([]byte("<!-- managed -->\nresearch\n"))}}
	server, err := newFullWithReaders(context.Background(), "/workspace", backend, sdds)
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Run(ctx, serverTransport) }()
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "sdd_render_projection", Arguments: map[string]any{"changeId": "change-1", "revisionId": "revision-1"}})
	if err != nil || rendered.IsError {
		t.Fatalf("render result=%+v err=%v", rendered, err)
	}
	var visible, structured struct {
		RelativePath string `json:"relativePath"`
		Content      string `json:"content"`
	}
	if err := json.Unmarshal([]byte(rendered.Content[0].(*sdk.TextContent).Text), &visible); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(rendered.StructuredContent)
	if err != nil || json.Unmarshal(encoded, &structured) != nil {
		t.Fatalf("structured=%s err=%v", encoded, err)
	}
	if visible.Content != string(sdds.rendered.Content) || structured.Content != visible.Content {
		t.Fatalf("visible=%+v structured=%+v", visible, structured)
	}
	compared, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "sdd_compare_projection", Arguments: map[string]any{"changeId": "change-1", "revisionId": "revision-1", "relativePath": visible.RelativePath, "projectionContent": visible.Content}})
	if err != nil || compared.IsError {
		t.Fatalf("compare result=%+v err=%v", compared, err)
	}
	var comparison sdd.ProjectionComparison
	if err := json.Unmarshal([]byte(compared.Content[0].(*sdk.TextContent).Text), &comparison); err != nil || comparison.State != sdd.DriftSynced {
		t.Fatalf("comparison=%+v err=%v", comparison, err)
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

func TestFullServerExposesExactToolAndMutationInventory(t *testing.T) {
	backend := &fakeReader{project: "project-1"}
	server, err := newFullWithReader(context.Background(), "/workspace", backend)
	if err != nil {
		t.Fatalf("newFullWithReader() error = %v", err)
	}
	want := []string{"memory_recent", "memory_search", "memory_context", "memory_get", "memory_save", "memory_forget", "memory_session_summary", "memory_update", "sdd_create", "sdd_list", "sdd_get", "sdd_set_interaction_mode", "sdd_transition", "sdd_save_revision", "sdd_get_revision", "sdd_list_revisions", "sdd_accept_revision", "sdd_render_projection", "sdd_compare_projection", "sdd_record_projection", "sdd_projection_status"}
	sort.Strings(want)
	names := discoveredNames(t, server)
	if !sameStrings(names, want) {
		t.Fatalf("tool names = %v, want %v", names, want)
	}
	mutations := 0
	for _, name := range want {
		if isMutationTool(name) {
			mutations++
		}
	}
	for _, name := range names {
		if name == "memory_sync" {
			t.Fatal("full server exposed memory sync")
		}
	}
	if mutations != 10 {
		t.Fatalf("full mode mutation set = %d, want 10", mutations)
	}
}

func isMutationTool(name string) bool {
	for _, mutation := range mutationToolNames {
		if name == mutation {
			return true
		}
	}
	return false
}

var mutationToolNames = []string{"memory_save", "memory_forget", "memory_session_summary", "memory_update", "sdd_create", "sdd_set_interaction_mode", "sdd_transition", "sdd_save_revision", "sdd_accept_revision", "sdd_record_projection"}

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
		if strings.HasPrefix(tool.Name, "sdd_") && tool.OutputSchema == nil {
			t.Errorf("%s omits output schema", tool.Name)
		}
		if tool.Name == "memory_get" || tool.Name == "memory_forget" {
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"id": {required: true, kind: "string"}})
		}
		if tool.Name == "memory_forget" && tool.Annotations.IdempotentHint {
			t.Fatal("memory_forget advertised as idempotent")
		}
		if tool.Name == "memory_save" {
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"title": {true, "string"}, "content": {true, "string"}, "type": {false, "string"}, "topic": {false, "string"}, "session_handle": {false, "string"}})
		}
		if tool.Name == "memory_context" {
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"session_handle": {true, "string"}})
		}
		if tool.Name == "memory_session_summary" {
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"session_handle": {true, "string"}, "summary": {true, "string"}, "expected_updated_at": {false, "string"}})
		}
		if tool.Name == "memory_update" {
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"id": {true, "string"}, "content": {true, "string"}, "expected_updated_at": {true, "string"}})
		}
		switch tool.Name {
		case "sdd_create":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"idempotencyKey": {true, "string"}, "title": {true, "string"}, "backend": {true, "string"}, "interactionMode": {true, "string"}, "plan": {true, "string"}})
			assertSchemaEnum(t, tool.InputSchema, "backend", []string{"openspec", "memory", "hybrid"})
			assertSchemaEnum(t, tool.InputSchema, "interactionMode", []string{"automatic", "interactive"})
			assertSchemaEnum(t, tool.InputSchema, "plan", []string{"low", "medium", "high", "ultra"})
		case "sdd_list":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"status": {false, "string"}, "limit": {false, "number"}})
			assertSchemaEnum(t, tool.InputSchema, "status", []string{"active", "completed", "cancelled"})
		case "sdd_get":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"id": {true, "string"}})
		case "sdd_set_interaction_mode":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"changeId": {true, "string"}, "interactionMode": {true, "string"}, "expectedStateVersion": {true, "number"}})
			assertSchemaEnum(t, tool.InputSchema, "interactionMode", []string{"automatic", "interactive"})
		case "sdd_transition":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"changeId": {true, "string"}, "targetPhase": {false, "string"}, "cancel": {false, "boolean"}, "expectedStateVersion": {true, "number"}})
			assertSchemaEnum(t, tool.InputSchema, "targetPhase", []string{"explore", "proposal", "spec", "design", "tasks", "apply", "verify", "complete"})
		case "sdd_save_revision":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"changeId": {true, "string"}, "artifact": {true, "string"}, "content": {true, "string"}, "externalLocation": {false, "string"}, "digest": {false, "string"}, "inputs": {false, "array"}, "inputDigest": {false, "string"}, "expectedStateVersion": {true, "number"}})
			assertSchemaEnum(t, tool.InputSchema, "artifact", []string{"explore", "proposal", "spec", "design", "tasks", "apply", "verify", "complete"})
			assertRevisionBindingSchema(t, tool.InputSchema)
		case "sdd_get_revision", "sdd_render_projection":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"changeId": {true, "string"}, "revisionId": {true, "string"}})
		case "sdd_list_revisions":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"changeId": {true, "string"}, "artifact": {false, "string"}, "limit": {false, "number"}})
			assertSchemaEnum(t, tool.InputSchema, "artifact", []string{"explore", "proposal", "spec", "design", "tasks", "apply", "verify", "complete"})
		case "sdd_accept_revision":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"changeId": {true, "string"}, "revisionId": {true, "string"}, "expectedStateVersion": {true, "number"}})
		case "sdd_compare_projection":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"changeId": {true, "string"}, "revisionId": {true, "string"}, "relativePath": {true, "string"}, "projectionContent": {false, "string"}, "missing": {false, "boolean"}, "symlink": {false, "boolean"}})
		case "sdd_record_projection":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"changeId": {true, "string"}, "artifactId": {true, "string"}, "revisionId": {true, "string"}, "status": {true, "string"}, "digest": {true, "string"}, "location": {true, "string"}, "expectedStateVersion": {true, "number"}})
			assertSchemaEnum(t, tool.InputSchema, "status", []string{"current", "stale", "drift", "failed"})
			if tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
				t.Fatal("sdd_record_projection is not destructive")
			}
		case "sdd_projection_status":
			assertSchemaProperties(t, tool.InputSchema, map[string]schemaExpectation{"changeId": {true, "string"}, "artifactId": {true, "string"}})
		}
	}
}

func TestFullServerToolSchemasUseRequiredArrays(t *testing.T) {
	server, err := newFullWithReader(context.Background(), "/workspace", &fakeReader{project: "project-1"})
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Run(ctx, serverTransport) }()
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.InputSchema == nil {
			t.Errorf("%s has no input schema", tool.Name)
		}
		assertSchemaRequiredArrays(t, tool.Name+" input", tool.InputSchema)
		assertSchemaRequiredArrays(t, tool.Name+" output", tool.OutputSchema)
	}
}

func TestSDDGetRevisionInputSchemaRequiresChangeAndRevision(t *testing.T) {
	encoded, err := json.Marshal(sddGetRevisionInputSchema())
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatal(err)
	}
	assertSchemaProperties(t, schema, map[string]schemaExpectation{
		"changeId":   {required: true, kind: "string"},
		"revisionId": {required: true, kind: "string"},
	})
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

func TestFullServerProtocolProviderSessionToolsBindProjectAndKeepOutputsSafe(t *testing.T) {
	backend := &fakeReader{project: "project-1", handoff: "UNTRUSTED DATA\nprior", entry: memory.Entry{ID: "obs-1", Project: "project-1", Content: "saved"}}
	server, err := newFullWithReader(context.Background(), "/workspace", backend)
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = server.Run(ctx, serverTransport) }()
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	call := func(name string, args map[string]any) *sdk.CallToolResult {
		t.Helper()
		result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil || result.IsError {
			t.Fatalf("%s result=%+v err=%v", name, result, err)
		}
		encoded, _ := json.Marshal(result)
		if strings.Contains(string(encoded), "provider-secret") || strings.Contains(string(encoded), "private-draft") {
			t.Fatalf("%s leaked provider data: %s", name, encoded)
		}
		return result
	}
	contextResult := call("memory_context", map[string]any{"session_handle": "ps-1"})
	encoded, _ := json.Marshal(contextResult)
	if !strings.Contains(string(encoded), "UNTRUSTED DATA") {
		t.Fatalf("context value missing: %s", encoded)
	}
	call("memory_session_summary", map[string]any{"session_handle": "ps-1", "summary": "private-draft"})
	call("memory_update", map[string]any{"id": "obs-1", "content": "saved", "expected_updated_at": "2026-07-20T12:00:00Z"})
	call("memory_save", map[string]any{"title": "Saved", "content": "saved", "session_handle": "ps-1"})
	if backend.context.Session.Project != "project-1" || backend.draft.Project != "project-1" || backend.update.Project != "project-1" || backend.remember.Project != "project-1" || backend.remember.Session != "" {
		t.Fatalf("provider tools were not project bound: context=%+v draft=%+v update=%+v save=%+v", backend.context, backend.draft, backend.update, backend.remember)
	}
	backend.providerErr = memory.ErrNotFound
	if _, err := server.sessionContext(context.Background(), contextInput{SessionHandle: "ps-1"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("provider context error=%v", err)
	}
	invalid, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "memory_context", Arguments: map[string]any{"session_handle": ""}})
	if err == nil && (invalid == nil || !invalid.IsError) {
		t.Fatalf("invalid context accepted: %+v", invalid)
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

func TestSDDToolValidationNamesSafeInvalidField(t *testing.T) {
	server, err := newFullWithReaders(context.Background(), "/workspace", &fakeReader{project: "project-1"}, &fakeSDDReader{})
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := server.callSDDSetInteractionMode(context.Background(), nil, sddModeInput{ChangeID: "change-1", InteractionMode: sdd.InteractionAutomatic, ExpectedStateVersion: 1.5})
	if err != nil || !result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	text := result.Content[0].(*sdk.TextContent).Text
	if text != "invalid tool input: expectedStateVersion" {
		t.Fatalf("text=%q", text)
	}
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

func TestServerSearchMapsInvalidRecallToInvalidInput(t *testing.T) {
	backend := &fakeReader{project: "project-1", recallErr: memory.ErrInvalid}
	server, err := newWithReader(context.Background(), "/workspace", backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.search(context.Background(), searchInput{Query: "legacy and migration"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("search error = %v, want invalid input", err)
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
	project                                           string
	workspace                                         string
	recent                                            memory.Recent
	recall                                            memory.Recall
	recentErr                                         error
	recallErr                                         error
	entry                                             memory.Entry
	remember                                          memory.Remember
	lookup                                            memory.Lookup
	forget                                            memory.Forget
	context                                           memory.ProviderSessionContext
	draft                                             memory.ProviderSessionDraftSave
	update                                            memory.ObservationUpdate
	handoff                                           string
	providerErr                                       error
	getErr                                            error
	entries                                           []memory.Entry
	recallCalls, getCalls, rememberCalls, forgetCalls int
}

func (reader *fakeReader) ProviderSessionContext(_ context.Context, project, handle string) (memory.ProviderSessionContext, error) {
	if project != reader.project || handle == "" {
		return memory.ProviderSessionContext{}, memory.ErrInvalid
	}
	reader.context = memory.ProviderSessionContext{Session: memory.ProviderSession{Project: project, Handle: handle, State: memory.ProviderSessionActive}}
	reader.context.Handoff = reader.handoff
	return reader.context, reader.providerErr
}
func (reader *fakeReader) SaveProviderSessionDraft(_ context.Context, request memory.ProviderSessionDraftSave) (memory.ProviderSessionDraft, error) {
	if request.Project != reader.project {
		return memory.ProviderSessionDraft{}, memory.ErrInvalid
	}
	reader.draft = request
	return memory.ProviderSessionDraft{Project: request.Project, Handle: request.Handle}, nil
}
func (reader *fakeReader) UpdateObservation(_ context.Context, request memory.ObservationUpdate) (memory.Observation, error) {
	if request.Project != reader.project {
		return memory.Observation{}, memory.ErrInvalid
	}
	reader.update = request
	return memory.Observation{ID: request.ID, Project: request.Project, Content: request.Content, State: memory.StateActive}, nil
}

type fakeSDDReader struct {
	lifecycle                      bool
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
	rendered                       sdd.ProjectionDocument
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
	if reader.lifecycle {
		reader.change = sdd.Change{ID: "change-1", Project: request.Project, Title: request.Title, Backend: request.Backend, InteractionMode: request.InteractionMode, Plan: request.Plan, Phase: sdd.PhaseExplore, Status: sdd.ChangeActive, StateVersion: 1}
	}
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
	if reader.lifecycle {
		reader.change.Phase = request.TargetPhase
		reader.change.StateVersion = request.ExpectedStateVersion + 1
	}
	return reader.change, reader.transitionErr
}
func (reader *fakeSDDReader) SaveRevision(_ context.Context, request sdd.SaveRevisionRequest) (sdd.Revision, error) {
	reader.saveRevision = request
	if reader.lifecycle {
		reader.revision = sdd.Revision{ID: "revision-1", Project: request.Project, ChangeID: request.ChangeID, ArtifactID: "artifact-1", Artifact: request.Artifact, ArtifactStatus: sdd.ArtifactDraft, Status: sdd.RevisionCandidate, Content: request.Content, Digest: sdd.ContentDigest(request.Content), StateVersion: request.ExpectedStateVersion + 1}
	}
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
	if reader.lifecycle {
		reader.revision.Status = sdd.RevisionAccepted
		reader.revision.StateVersion = request.ExpectedStateVersion + 1
	}
	return reader.revision, nil
}
func (reader *fakeSDDReader) RenderProjection(_ context.Context, request sdd.RenderProjectionRequest) (sdd.ProjectionDocument, error) {
	reader.render = request
	return reader.rendered, nil
}
func (reader *fakeSDDReader) CompareProjection(_ context.Context, request sdd.CompareProjectionRequest) (sdd.ProjectionComparison, error) {
	reader.compare = request
	if request.Input.RelativePath == reader.rendered.RelativePath && string(request.Input.Content) == string(reader.rendered.Content) {
		return sdd.ProjectionComparison{State: sdd.DriftSynced}, nil
	}
	return sdd.ProjectionComparison{State: sdd.DriftDrifted}, nil
}
func (reader *fakeSDDReader) RecordProjection(_ context.Context, request sdd.RecordProjectionRequest) (sdd.Projection, error) {
	reader.record = request
	if reader.lifecycle {
		reader.projection = sdd.Projection{Project: request.Project, ChangeID: request.ChangeID, ArtifactID: request.ArtifactID, RevisionID: request.RevisionID, Status: request.Status, Digest: request.Digest, Location: request.Location, StateVersion: request.ExpectedStateVersion + 1}
	}
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
	return reader.entries, reader.recentErr
}

func (reader *fakeReader) Recall(_ context.Context, request memory.Recall) ([]memory.Entry, error) {
	reader.recallCalls++
	reader.recall = request
	return reader.entries, reader.recallErr
}

func sameAnyStrings(value any, want []string) bool {
	items, ok := value.([]any)
	if !ok || len(items) != len(want) {
		return false
	}
	for index, item := range items {
		if item != want[index] {
			return false
		}
	}
	return true
}

func (reader *fakeReader) Get(_ context.Context, request memory.Lookup) (memory.Entry, error) {
	reader.getCalls++
	reader.lookup = request
	return reader.entry, reader.getErr
}

func (reader *fakeReader) Remember(_ context.Context, request memory.Remember) (memory.Entry, error) {
	reader.rememberCalls++
	reader.remember = request
	return reader.entry, nil
}

func (reader *fakeReader) Forget(_ context.Context, request memory.Forget) (memory.Entry, error) {
	reader.forgetCalls++
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

func assertSchemaRequiredArrays(t *testing.T, name string, schema any) {
	t.Helper()
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("%s schema marshal: %v", name, err)
	}
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatalf("%s schema unmarshal: %v", name, err)
	}
	assertRequiredArrays(t, name, value)
}

func assertRequiredArrays(t *testing.T, name string, value any) {
	t.Helper()
	switch value := value.(type) {
	case map[string]any:
		if required, ok := value["required"]; ok {
			if _, ok := required.([]any); !ok {
				t.Errorf("%s schema required = %#v, want array", name, required)
			}
		}
		for _, child := range value {
			assertRequiredArrays(t, name, child)
		}
	case []any:
		for _, child := range value {
			assertRequiredArrays(t, name, child)
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

func assertSchemaEnum(t *testing.T, schema any, field string, want []string) {
	t.Helper()
	properties := schema.(map[string]any)["properties"].(map[string]any)
	if !sameAnyStrings(properties[field].(map[string]any)["enum"], want) {
		t.Errorf("schema property %q enum = %#v, want %v", field, properties[field].(map[string]any)["enum"], want)
	}
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
