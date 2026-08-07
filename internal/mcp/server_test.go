package mcp

import (
	"context"
	"errors"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
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
	if got, want := server.toolNames(), []string{"memory_recent", "memory_search"}; !sameStrings(got, want) {
		t.Fatalf("tool names = %v, want %v", got, want)
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
	want := []string{"memory_recent", "memory_search", "memory_get", "memory_save", "memory_forget"}
	if got := server.toolNames(); !sameStrings(got, want) {
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
			assertSchemaProperties(t, tool.InputSchema, map[string]bool{"id": true})
		}
		if tool.Name == "memory_forget" && tool.Annotations.IdempotentHint {
			t.Fatal("memory_forget advertised as idempotent")
		}
		if tool.Name == "memory_save" {
			assertSchemaProperties(t, tool.InputSchema, map[string]bool{"title": true, "content": true, "type": false, "topic": false})
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

func assertSchemaProperties(t *testing.T, schema any, expected map[string]bool) {
	t.Helper()
	value, ok := schema.(map[string]any)
	if !ok {
		t.Fatalf("schema type = %T", schema)
	}
	properties, ok := value["properties"].(map[string]any)
	if !ok || len(properties) != len(expected) {
		t.Fatalf("schema properties = %#v", value["properties"])
	}
	required, ok := value["required"].([]any)
	if !ok {
		t.Fatalf("schema required = %#v", value["required"])
	}
	for name, requiredExpected := range expected {
		property, ok := properties[name].(map[string]any)
		if !ok {
			t.Errorf("missing schema property %q", name)
			continue
		}
		if property["type"] != "string" {
			t.Errorf("schema property %q type = %#v, want string", name, property["type"])
		}
		found := false
		for _, field := range required {
			if field == name {
				found = true
			}
		}
		if found != requiredExpected {
			t.Errorf("schema property %q required = %v, want %v", name, found, requiredExpected)
		}
	}
}
