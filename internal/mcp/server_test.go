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
