// Package mcp exposes the read-only Model Context Protocol surface.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vgxness/vgxness/internal/app/runtime"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
)

var (
	ErrInvalidInput = errors.New("invalid MCP tool input")
	ErrUnavailable  = errors.New("memory service unavailable")
)

const maxLimit = 50

// Server is a read-only MCP server bound to one canonical workspace.
type Server struct {
	server  *sdk.Server
	reader  memoryReader
	project string
}

type memoryReader interface {
	ResolveProject(context.Context, string) (string, error)
	Recent(context.Context, memory.Recent) ([]memory.Entry, error)
	Recall(context.Context, memory.Recall) ([]memory.Entry, error)
}

type runtimeReader struct {
	runtime runtime.Memory
	opts    config.Options
}

func (reader runtimeReader) ResolveProject(ctx context.Context, workspace string) (string, error) {
	return reader.runtime.ResolveProject(ctx, reader.opts, workspace)
}

func (reader runtimeReader) Recent(ctx context.Context, request memory.Recent) ([]memory.Entry, error) {
	return reader.runtime.Recent(ctx, reader.opts, request)
}

func (reader runtimeReader) Recall(ctx context.Context, request memory.Recall) ([]memory.Entry, error) {
	return reader.runtime.Recall(ctx, reader.opts, request)
}

// New creates a server whose project identity is resolved once from workspace.
// Missing or inaccessible read-only storage returns ErrUnavailable.
func New(ctx context.Context, workspace string, opts config.Options) (*Server, error) {
	return newWithReader(ctx, workspace, runtimeReader{runtime: runtime.NewMemory("mcp", true), opts: opts})
}

func newWithReader(ctx context.Context, workspace string, reader memoryReader) (*Server, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(workspace) == "" || reader == nil {
		return nil, ErrInvalidInput
	}
	project, err := reader.ResolveProject(ctx, workspace)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	server := &Server{reader: reader, project: project}
	server.server = sdk.NewServer(&sdk.Implementation{Name: "vgxness-memory", Version: "0.1.0"}, nil)
	annotations := &sdk.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: boolPtr(false), IdempotentHint: true, OpenWorldHint: boolPtr(false)}
	sdk.AddTool(server.server, &sdk.Tool{Name: "memory_recent", Description: "Read recent project memory entries. This tool never writes data.", Annotations: annotations}, server.callRecent)
	sdk.AddTool(server.server, &sdk.Tool{Name: "memory_search", Description: "Search project memory entries. This tool never writes data.", Annotations: annotations}, server.callSearch)
	return server, nil
}

func boolPtr(value bool) *bool { return &value }

// Run serves MCP requests over an injected transport until ctx is cancelled or
// the transport closes. It never writes diagnostic output to the transport.
func (server *Server) Run(ctx context.Context, transport sdk.Transport) error {
	return server.server.Run(ctx, transport)
}

func (server *Server) toolNames() []string { return []string{"memory_recent", "memory_search"} }

type recentInput struct {
	Limit int `json:"limit,omitempty"`
}

type searchInput struct {
	Query string `json:"query" jsonschema:"required"`
	Limit int    `json:"limit,omitempty"`
}

type result struct {
	Entries []entry `json:"entries"`
}

type entry struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Type      string    `json:"type"`
	TopicKey  string    `json:"topicKey,omitempty"`
	State     string    `json:"state"`
	Preview   string    `json:"preview"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (server *Server) recent(ctx context.Context, input recentInput) (result, error) {
	if err := ctx.Err(); err != nil {
		return result{}, err
	}
	if input.Limit < 0 || input.Limit > maxLimit {
		return result{}, ErrInvalidInput
	}
	entries, err := server.reader.Recent(ctx, memory.Recent{Project: server.project, Scope: memory.ScopeProject, Limit: input.Limit})
	return shapeResult(ctx, entries, err)
}

func (server *Server) search(ctx context.Context, input searchInput) (result, error) {
	if err := ctx.Err(); err != nil {
		return result{}, err
	}
	if strings.TrimSpace(input.Query) == "" || input.Limit < 0 || input.Limit > maxLimit {
		return result{}, ErrInvalidInput
	}
	entries, err := server.reader.Recall(ctx, memory.Recall{Query: input.Query, Project: server.project, Scope: memory.ScopeProject, Limit: input.Limit})
	return shapeResult(ctx, entries, err)
}

func shapeResult(ctx context.Context, entries []memory.Entry, err error) (result, error) {
	if err != nil {
		if ctx.Err() != nil {
			return result{}, ctx.Err()
		}
		return result{}, ErrUnavailable
	}
	result := result{Entries: make([]entry, len(entries))}
	for index, item := range entries {
		result.Entries[index] = entry{ID: item.ID, Title: item.Title, Type: item.Type, TopicKey: item.TopicKey, State: string(item.State), Preview: item.Preview, UpdatedAt: item.UpdatedAt}
	}
	return result, nil
}

func (server *Server) callRecent(ctx context.Context, _ *sdk.CallToolRequest, input recentInput) (*sdk.CallToolResult, result, error) {
	output, err := server.recent(ctx, input)
	return toolResponse(err, output)
}

func (server *Server) callSearch(ctx context.Context, _ *sdk.CallToolRequest, input searchInput) (*sdk.CallToolResult, result, error) {
	output, err := server.search(ctx, input)
	return toolResponse(err, output)
}

func toolResponse(err error, output result) (*sdk.CallToolResult, result, error) {
	if err == nil {
		return toolText(fmt.Sprintf("Returned %d memory entries.", len(output.Entries)), false), output, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return toolText("request cancelled", true), result{}, nil
	}
	if errors.Is(err, ErrInvalidInput) {
		return toolText("invalid tool input", true), result{}, nil
	}
	return toolText("memory service unavailable", true), result{}, nil
}

func toolText(text string, isError bool) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}, IsError: isError}
}
