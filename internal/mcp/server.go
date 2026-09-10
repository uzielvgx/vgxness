// Package mcp provides capability-gated MCP tools: read-only by default, with
// explicit --full memory mutation capabilities.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
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
	ErrNotFound     = errors.New("memory record not found")
	ErrConflict     = errors.New("memory record conflict")
)

const maxLimit = 50
const maxJSONInteger = 9_007_199_254_740_991

// Server is an MCP server bound to one canonical workspace.
type Server struct {
	server  *sdk.Server
	reader  memoryReader
	project string
	full    bool
}

type memoryReader interface {
	ResolveProject(context.Context, string) (string, error)
	Recent(context.Context, memory.Recent) ([]memory.Entry, error)
	Recall(context.Context, memory.Recall) ([]memory.Entry, error)
	Get(context.Context, memory.Lookup) (memory.Entry, error)
	Remember(context.Context, memory.Remember) (memory.Entry, error)
	Forget(context.Context, memory.Forget) (memory.Entry, error)
	ProviderSessionContext(context.Context, string, string) (memory.ProviderSessionContext, error)
	SaveProviderSessionDraft(context.Context, memory.ProviderSessionDraftSave) (memory.ProviderSessionDraft, error)
	UpdateObservation(context.Context, memory.ObservationUpdate) (memory.Observation, error)
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

func (reader runtimeReader) Get(ctx context.Context, request memory.Lookup) (memory.Entry, error) {
	return reader.runtime.Get(ctx, reader.opts, request)
}

func (reader runtimeReader) Remember(ctx context.Context, request memory.Remember) (memory.Entry, error) {
	return reader.runtime.Remember(ctx, reader.opts, request)
}

func (reader runtimeReader) Forget(ctx context.Context, request memory.Forget) (memory.Entry, error) {
	return reader.runtime.Forget(ctx, reader.opts, request)
}
func (reader runtimeReader) ProviderSessionContext(ctx context.Context, project, handle string) (memory.ProviderSessionContext, error) {
	return reader.runtime.ProviderSessionContext(ctx, reader.opts, project, handle)
}
func (reader runtimeReader) SaveProviderSessionDraft(ctx context.Context, request memory.ProviderSessionDraftSave) (memory.ProviderSessionDraft, error) {
	return reader.runtime.SaveProviderSessionDraft(ctx, reader.opts, request)
}
func (reader runtimeReader) UpdateObservation(ctx context.Context, request memory.ObservationUpdate) (memory.Observation, error) {
	return reader.runtime.UpdateObservation(ctx, reader.opts, request)
}

// New creates a server whose project identity is resolved once from workspace.
// Missing or inaccessible read-only storage returns ErrUnavailable.
func New(ctx context.Context, workspace string, opts config.Options) (*Server, error) {
	return newWithReader(ctx, workspace, runtimeReader{runtime: runtime.NewMemory("mcp", true), opts: opts})
}

// NewFull creates an explicitly write-capable server. Callers must opt in; no
// caller identity is inferred from the MCP transport.
func NewFull(ctx context.Context, workspace string, opts config.Options) (*Server, error) {
	return newFullWithReader(ctx, workspace, runtimeReader{runtime: runtime.NewMemory("mcp", false), opts: opts})
}

// RunStdio creates a server bound to workspace and serves it over process standard I/O.
func RunStdio(ctx context.Context, workspace string, opts config.Options) error {
	return RunStdioWithMode(ctx, workspace, opts, false)
}

// RunStdioWithMode serves the explicitly selected capability mode over stdio.
func RunStdioWithMode(ctx context.Context, workspace string, opts config.Options, full bool) error {
	var server *Server
	var err error
	if full {
		server, err = NewFull(ctx, workspace, opts)
	} else {
		server, err = New(ctx, workspace, opts)
	}
	if err != nil {
		return err
	}
	return server.Run(ctx, &sdk.StdioTransport{})
}

func newWithReader(ctx context.Context, workspace string, reader memoryReader) (*Server, error) {
	return newServerWithReader(ctx, workspace, reader, false)
}

func newFullWithReader(ctx context.Context, workspace string, reader memoryReader) (*Server, error) {
	return newServerWithReader(ctx, workspace, reader, true)
}

func newServerWithReader(ctx context.Context, workspace string, reader memoryReader, full bool) (*Server, error) {
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
	server := &Server{reader: reader, project: project, full: full}
	server.server = sdk.NewServer(&sdk.Implementation{Name: "vgxness-memory", Version: "0.1.0"}, nil)
	annotations := &sdk.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: boolPtr(false), IdempotentHint: true, OpenWorldHint: boolPtr(false)}
	sdk.AddTool(server.server, &sdk.Tool{Name: "memory_recent", Description: "Read recent project memory entries. This tool never writes data.", Annotations: annotations, InputSchema: jsonSchema(nil, map[string]any{"limit": jsonNumber()}), OutputSchema: memoryEntriesOutputSchema()}, server.callRecent)
	sdk.AddTool(server.server, &sdk.Tool{
		Name:        "memory_search",
		Description: "Search project memory entries. This tool never writes data.",
		Annotations: annotations,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"query":      map[string]any{"type": "string"},
				"limit":      map[string]any{"type": "integer"},
				"match_mode": map[string]any{"type": "string", "enum": []string{"all", "any"}},
			},
			"required": []string{"query"},
		},
		OutputSchema: memoryEntriesOutputSchema(),
	}, server.callSearch)
	sdk.AddTool(server.server, &sdk.Tool{Name: "memory_context", Description: "Read bounded untrusted handoff for an active local provider session.", Annotations: annotations, InputSchema: jsonSchema([]string{"session_handle"}, map[string]any{"session_handle": jsonString()}), OutputSchema: memoryContextOutputSchema()}, server.callContext)
	if full {
		writeAnnotations := &sdk.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(false), IdempotentHint: false, OpenWorldHint: boolPtr(false)}
		sdk.AddTool(server.server, &sdk.Tool{Name: "memory_get", Description: "Read one full project memory entry by exact ID. This tool never writes data.", Annotations: annotations, InputSchema: jsonSchema([]string{"id"}, map[string]any{"id": jsonString()}), OutputSchema: memoryEntryOutputSchema()}, server.callGet)
		sdk.AddTool(server.server, &sdk.Tool{Name: "memory_save", Description: "Write a durable project memory entry. This tool stores data.", Annotations: writeAnnotations, InputSchema: jsonSchema([]string{"title", "content"}, map[string]any{"title": jsonString(), "content": jsonString(), "type": jsonString(), "topic": jsonString(), "session_handle": jsonString()}), OutputSchema: memoryEntryOutputSchema()}, server.callSave)
		sdk.AddTool(server.server, &sdk.Tool{Name: "memory_session_summary", Description: "Save one local pending provider-session summary.", Annotations: writeAnnotations, InputSchema: jsonSchema([]string{"session_handle", "summary"}, map[string]any{"session_handle": jsonString(), "summary": jsonString(), "expected_updated_at": jsonString()}), OutputSchema: memorySummaryOutputSchema()}, server.callSummary)
		sdk.AddTool(server.server, &sdk.Tool{Name: "memory_update", Description: "Update one mutable memory entry using its exact timestamp.", Annotations: writeAnnotations, InputSchema: jsonSchema([]string{"id", "content", "expected_updated_at"}, map[string]any{"id": jsonString(), "content": jsonString(), "expected_updated_at": jsonString()}), OutputSchema: memoryEntryOutputSchema()}, server.callUpdate)
		sdk.AddTool(server.server, &sdk.Tool{Name: "memory_forget", Description: "Archive one exact project memory entry. This tool changes stored data and removes it from normal search.", Annotations: &sdk.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(true), IdempotentHint: false, OpenWorldHint: boolPtr(false)}, InputSchema: jsonSchema([]string{"id"}, map[string]any{"id": jsonString()}), OutputSchema: memoryEntryOutputSchema()}, server.callForget)
	}
	return server, nil
}

func boolPtr(value bool) *bool { return &value }

func jsonSchema(required []string, properties map[string]any) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
}
func jsonString(values ...string) map[string]any {
	schema := map[string]any{"type": "string"}
	if len(values) > 0 {
		schema["enum"] = values
	}
	return schema
}
func jsonNumber() map[string]any  { return map[string]any{"type": "number"} }
func jsonBoolean() map[string]any { return map[string]any{"type": "boolean"} }
func jsonArray(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}
func jsonNullableArray(items map[string]any) map[string]any {
	return map[string]any{"type": []string{"array", "null"}, "items": items}
}

func memoryEntryOutputSchema() map[string]any {
	return jsonSchema([]string{"id", "title", "type", "state", "preview", "updatedAt"}, map[string]any{"id": jsonString(), "title": jsonString(), "type": jsonString(), "topicKey": jsonString(), "state": jsonString(), "preview": jsonString(), "updatedAt": jsonString(), "content": jsonString(), "references": jsonArray(jsonString())})
}
func memoryEntriesOutputSchema() map[string]any {
	entries := memoryEntryOutputSchema()
	delete(entries["properties"].(map[string]any), "content")
	delete(entries["properties"].(map[string]any), "references")
	return jsonSchema([]string{"entries"}, map[string]any{"entries": map[string]any{"type": "array", "items": entries, "maxItems": maxLimit}})
}
func memoryContextOutputSchema() map[string]any {
	return jsonSchema([]string{"handoff"}, map[string]any{"handoff": jsonString()})
}
func memorySummaryOutputSchema() map[string]any {
	return jsonSchema([]string{"status", "updated_at"}, map[string]any{"status": jsonString(), "updated_at": jsonString()})
}

// Run serves MCP requests over an injected transport until ctx is cancelled or
// the transport closes. It never writes diagnostic output to the transport.
func (server *Server) Run(ctx context.Context, transport sdk.Transport) error {
	return server.server.Run(ctx, transport)
}

type recentInput struct {
	Limit int `json:"limit,omitempty"`
}

type searchInput struct {
	Query     string `json:"query" jsonschema:"required"`
	Limit     int    `json:"limit,omitempty"`
	MatchMode string `json:"match_mode,omitempty"`
}

type getInput struct {
	ID string `json:"id" jsonschema:"required"`
}

type saveInput struct {
	Title         string `json:"title" jsonschema:"required"`
	Content       string `json:"content" jsonschema:"required"`
	Type          string `json:"type,omitempty"`
	Topic         string `json:"topic,omitempty"`
	SessionHandle string `json:"session_handle,omitempty"`
}
type contextInput struct {
	SessionHandle string `json:"session_handle" jsonschema:"required"`
}
type summaryInput struct {
	SessionHandle     string `json:"session_handle" jsonschema:"required"`
	Summary           string `json:"summary" jsonschema:"required"`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty"`
}
type updateInput struct {
	ID                string `json:"id" jsonschema:"required"`
	Content           string `json:"content" jsonschema:"required"`
	ExpectedUpdatedAt string `json:"expected_updated_at" jsonschema:"required"`
}

type result struct {
	Entries []entry `json:"entries"`
}
type contextResult struct {
	Handoff string `json:"handoff"`
}
type summaryResult struct {
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

type entry struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Type       string    `json:"type"`
	TopicKey   string    `json:"topicKey,omitempty"`
	State      string    `json:"state"`
	Preview    string    `json:"preview"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Content    string    `json:"content,omitempty"`
	References []string  `json:"references,omitempty"`
}

func (server *Server) get(ctx context.Context, input getInput) (entry, error) {
	if err := ctx.Err(); err != nil {
		return entry{}, err
	}
	if strings.TrimSpace(input.ID) == "" {
		return entry{}, ErrInvalidInput
	}
	item, err := server.reader.Get(ctx, memory.Lookup{ID: input.ID, Project: server.project, Scope: memory.ScopeProject})
	return shapeEntry(ctx, item, err, true)
}

func (server *Server) save(ctx context.Context, input saveInput) (entry, error) {
	if err := ctx.Err(); err != nil {
		return entry{}, err
	}
	if strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Content) == "" {
		return entry{}, ErrInvalidInput
	}
	if input.SessionHandle != "" {
		if _, err := server.reader.ProviderSessionContext(ctx, server.project, input.SessionHandle); err != nil {
			return entry{}, shapeProviderError(ctx, err)
		}
	}
	item, err := server.reader.Remember(ctx, memory.Remember{Title: input.Title, Content: input.Content, Type: input.Type, TopicKey: input.Topic, Project: server.project, Scope: memory.ScopeProject})
	return shapeEntry(ctx, item, err, true)
}

func (server *Server) sessionContext(ctx context.Context, input contextInput) (contextResult, error) {
	if strings.TrimSpace(input.SessionHandle) == "" {
		return contextResult{}, ErrInvalidInput
	}
	value, err := server.reader.ProviderSessionContext(ctx, server.project, input.SessionHandle)
	return contextResult{Handoff: value.Handoff}, shapeProviderError(ctx, err)
}
func (server *Server) sessionSummary(ctx context.Context, input summaryInput) (summaryResult, error) {
	if strings.TrimSpace(input.SessionHandle) == "" || strings.TrimSpace(input.Summary) == "" {
		return summaryResult{}, ErrInvalidInput
	}
	var expected time.Time
	var err error
	if input.ExpectedUpdatedAt != "" {
		expected, err = time.Parse(time.RFC3339Nano, input.ExpectedUpdatedAt)
		if err != nil {
			return summaryResult{}, ErrInvalidInput
		}
	}
	value, err := server.reader.SaveProviderSessionDraft(ctx, memory.ProviderSessionDraftSave{Project: server.project, Handle: input.SessionHandle, Summary: input.Summary, ExpectedUpdatedAt: expected})
	return summaryResult{Status: "draft", UpdatedAt: value.UpdatedAt}, shapeProviderError(ctx, err)
}
func (server *Server) update(ctx context.Context, input updateInput) (entry, error) {
	expected, err := time.Parse(time.RFC3339Nano, input.ExpectedUpdatedAt)
	if err != nil || strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Content) == "" {
		return entry{}, ErrInvalidInput
	}
	item, err := server.reader.UpdateObservation(ctx, memory.ObservationUpdate{ID: input.ID, Project: server.project, Content: input.Content, ExpectedUpdatedAt: expected})
	if err != nil {
		return entry{}, shapeProviderError(ctx, err)
	}
	return makeEntry(memory.Entry{ID: item.ID, Title: item.Title, Type: item.Type, TopicKey: item.TopicKey, State: item.State, Content: item.Content, UpdatedAt: item.UpdatedAt}, true), nil
}
func shapeProviderError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, memory.ErrInvalid) {
		return ErrInvalidInput
	}
	if errors.Is(err, memory.ErrNotFound) {
		return ErrNotFound
	}
	if errors.Is(err, memory.ErrConflict) {
		return ErrConflict
	}
	return ErrUnavailable
}

func (server *Server) forget(ctx context.Context, input getInput) (entry, error) {
	if err := ctx.Err(); err != nil {
		return entry{}, err
	}
	if strings.TrimSpace(input.ID) == "" {
		return entry{}, ErrInvalidInput
	}
	item, err := server.reader.Forget(ctx, memory.Forget{ID: input.ID, Project: server.project, Scope: memory.ScopeProject})
	return shapeEntry(ctx, item, err, true)
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
	matchAny := false
	switch input.MatchMode {
	case "", "all":
	case "any":
		matchAny = true
	default:
		return result{}, ErrInvalidInput
	}
	entries, err := server.reader.Recall(ctx, memory.Recall{Query: input.Query, Project: server.project, Scope: memory.ScopeProject, Limit: input.Limit, MatchAny: matchAny})
	return shapeResult(ctx, entries, err)
}

func shapeResult(ctx context.Context, entries []memory.Entry, err error) (result, error) {
	if err != nil {
		if ctx.Err() != nil {
			return result{}, ctx.Err()
		}
		if errors.Is(err, memory.ErrInvalid) {
			return result{}, ErrInvalidInput
		}
		return result{}, ErrUnavailable
	}
	result := result{Entries: make([]entry, len(entries))}
	for index, item := range entries {
		result.Entries[index] = makeEntry(item, false)
	}
	return result, nil
}

func shapeEntry(ctx context.Context, item memory.Entry, err error, content bool) (entry, error) {
	if err == nil {
		return makeEntry(item, content), nil
	}
	if ctx.Err() != nil {
		return entry{}, ctx.Err()
	}
	if errors.Is(err, memory.ErrInvalid) {
		return entry{}, ErrInvalidInput
	}
	if errors.Is(err, memory.ErrNotFound) {
		return entry{}, ErrNotFound
	}
	if errors.Is(err, memory.ErrConflict) {
		return entry{}, ErrConflict
	}
	return entry{}, ErrUnavailable
}

func makeEntry(item memory.Entry, content bool) entry {
	output := entry{ID: item.ID, Title: item.Title, Type: item.Type, TopicKey: item.TopicKey, State: string(item.State), Preview: item.Preview, UpdatedAt: item.UpdatedAt}
	if content {
		output.Content, output.References = item.Content, append([]string(nil), item.References...)
	}
	return output
}

func (server *Server) callRecent(ctx context.Context, _ *sdk.CallToolRequest, input recentInput) (*sdk.CallToolResult, result, error) {
	output, err := server.recent(ctx, input)
	return toolResponse(err, output)
}

func (server *Server) callSearch(ctx context.Context, _ *sdk.CallToolRequest, input searchInput) (*sdk.CallToolResult, result, error) {
	output, err := server.search(ctx, input)
	return toolResponse(err, output)
}

func (server *Server) callGet(ctx context.Context, _ *sdk.CallToolRequest, input getInput) (*sdk.CallToolResult, entry, error) {
	output, err := server.get(ctx, input)
	return entryResponse(err, output)
}

func (server *Server) callSave(ctx context.Context, _ *sdk.CallToolRequest, input saveInput) (*sdk.CallToolResult, entry, error) {
	output, err := server.save(ctx, input)
	return entryResponse(err, output)
}
func (server *Server) callContext(ctx context.Context, _ *sdk.CallToolRequest, input contextInput) (*sdk.CallToolResult, contextResult, error) {
	output, err := server.sessionContext(ctx, input)
	if err != nil {
		response, _, _ := entryResponse(err, entry{})
		return response, contextResult{}, nil
	}
	return memorySuccessResponse(output), output, nil
}
func (server *Server) callSummary(ctx context.Context, _ *sdk.CallToolRequest, input summaryInput) (*sdk.CallToolResult, summaryResult, error) {
	output, err := server.sessionSummary(ctx, input)
	if err != nil {
		response, _, _ := entryResponse(err, entry{})
		return response, summaryResult{}, nil
	}
	return memorySuccessResponse(output), output, nil
}
func (server *Server) callUpdate(ctx context.Context, _ *sdk.CallToolRequest, input updateInput) (*sdk.CallToolResult, entry, error) {
	output, err := server.update(ctx, input)
	return entryResponse(err, output)
}

func (server *Server) callForget(ctx context.Context, _ *sdk.CallToolRequest, input getInput) (*sdk.CallToolResult, entry, error) {
	output, err := server.forget(ctx, input)
	return entryResponse(err, output)
}

func toolResponse(err error, output result) (*sdk.CallToolResult, result, error) {
	if err == nil {
		return memorySuccessResponse(output), output, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return toolText("request cancelled", true), result{}, nil
	}
	if errors.Is(err, ErrInvalidInput) {
		return toolText("invalid tool input", true), result{}, nil
	}
	return toolText("memory service unavailable", true), result{}, nil
}

func entryResponse(err error, output entry) (*sdk.CallToolResult, entry, error) {
	if err == nil {
		return memorySuccessResponse(output), output, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return toolText("request cancelled", true), entry{}, nil
	}
	if errors.Is(err, ErrInvalidInput) {
		return toolText("invalid tool input", true), entry{}, nil
	}
	if errors.Is(err, ErrNotFound) {
		return toolText("memory record not found", true), entry{}, nil
	}
	if errors.Is(err, ErrConflict) {
		return toolText("memory record conflict", true), entry{}, nil
	}
	return toolText("memory service unavailable", true), entry{}, nil
}

func memorySuccessResponse(output any) *sdk.CallToolResult {
	encoded, err := json.Marshal(output)
	if err != nil {
		return toolText("memory service unavailable", true)
	}
	var canonical any
	if err := json.Unmarshal(encoded, &canonical); err != nil {
		return toolText("memory service unavailable", true)
	}
	encoded, err = json.Marshal(canonical)
	if err != nil {
		return toolText("memory service unavailable", true)
	}
	return toolText(string(encoded), false)
}

func toolText(text string, isError bool) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}, IsError: isError}
}
