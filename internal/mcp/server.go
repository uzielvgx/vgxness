// Package mcp provides capability-gated MCP tools: read-only by default, with
// explicit --full memory and SDD mutation capabilities.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vgxness/vgxness/internal/app/runtime"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/sdd"
	"github.com/vgxness/vgxness/internal/skillregistry"
)

var (
	ErrInvalidInput = errors.New("invalid MCP tool input")
	ErrUnavailable  = errors.New("memory service unavailable")
	ErrNotFound     = errors.New("memory record not found")
	ErrConflict     = errors.New("memory record conflict")
	ErrStale        = errors.New("SDD state version changed")
	ErrSDDCancelled = errors.New("SDD change is cancelled")
)

const maxLimit = 50
const maxJSONInteger = 9_007_199_254_740_991

// Server is an MCP server bound to one canonical workspace.
type Server struct {
	server    *sdk.Server
	reader    memoryReader
	project   string
	full      bool
	sdd       sddReader
	workspace string
	skillHome string
}

type sddReader interface {
	CreateChange(context.Context, sdd.CreateChangeRequest) (sdd.Change, error)
	ListChanges(context.Context, sdd.ListChangesRequest) ([]sdd.Change, error)
	GetChange(context.Context, sdd.GetChangeRequest) (sdd.Change, error)
	UpdateInteractionMode(context.Context, sdd.UpdateInteractionModeRequest) (sdd.Change, error)
	TransitionChange(context.Context, sdd.TransitionChangeRequest) (sdd.Change, error)
	SaveRevision(context.Context, sdd.SaveRevisionRequest) (sdd.Revision, error)
	GetRevision(context.Context, sdd.GetRevisionRequest) (sdd.Revision, error)
	ListRevisions(context.Context, sdd.ListRevisionsRequest) ([]sdd.Revision, error)
	AcceptRevision(context.Context, sdd.AcceptRevisionRequest) (sdd.Revision, error)
	RenderProjection(context.Context, sdd.RenderProjectionRequest) (sdd.ProjectionDocument, error)
	CompareProjection(context.Context, sdd.CompareProjectionRequest) (sdd.ProjectionComparison, error)
	RecordProjection(context.Context, sdd.RecordProjectionRequest) (sdd.Projection, error)
	ProjectionStatus(context.Context, sdd.ProjectionStatusRequest) (sdd.Projection, error)
}

type memoryReader interface {
	ResolveProject(context.Context, string) (string, error)
	Recent(context.Context, memory.Recent) ([]memory.Entry, error)
	Recall(context.Context, memory.Recall) ([]memory.Entry, error)
	Get(context.Context, memory.Lookup) (memory.Entry, error)
	Remember(context.Context, memory.Remember) (memory.Entry, error)
	Forget(context.Context, memory.Forget) (memory.Entry, error)
}

type runtimeReader struct {
	runtime runtime.Memory
	opts    config.Options
}

type runtimeSDDReader struct {
	runtime runtime.SDD
	opts    config.Options
}

func (reader runtimeSDDReader) CreateChange(ctx context.Context, request sdd.CreateChangeRequest) (sdd.Change, error) {
	return reader.runtime.CreateChange(ctx, reader.opts, request)
}
func (reader runtimeSDDReader) ListChanges(ctx context.Context, request sdd.ListChangesRequest) ([]sdd.Change, error) {
	return reader.runtime.ListChanges(ctx, reader.opts, request)
}
func (reader runtimeSDDReader) GetChange(ctx context.Context, request sdd.GetChangeRequest) (sdd.Change, error) {
	return reader.runtime.GetChange(ctx, reader.opts, request)
}
func (reader runtimeSDDReader) UpdateInteractionMode(ctx context.Context, request sdd.UpdateInteractionModeRequest) (sdd.Change, error) {
	return reader.runtime.UpdateInteractionMode(ctx, reader.opts, request)
}
func (reader runtimeSDDReader) TransitionChange(ctx context.Context, request sdd.TransitionChangeRequest) (sdd.Change, error) {
	return reader.runtime.TransitionChange(ctx, reader.opts, request)
}
func (reader runtimeSDDReader) SaveRevision(ctx context.Context, request sdd.SaveRevisionRequest) (sdd.Revision, error) {
	return reader.runtime.SaveRevision(ctx, reader.opts, request)
}
func (reader runtimeSDDReader) GetRevision(ctx context.Context, request sdd.GetRevisionRequest) (sdd.Revision, error) {
	return reader.runtime.GetRevision(ctx, reader.opts, request)
}
func (reader runtimeSDDReader) ListRevisions(ctx context.Context, request sdd.ListRevisionsRequest) ([]sdd.Revision, error) {
	return reader.runtime.ListRevisions(ctx, reader.opts, request)
}
func (reader runtimeSDDReader) AcceptRevision(ctx context.Context, request sdd.AcceptRevisionRequest) (sdd.Revision, error) {
	return reader.runtime.AcceptRevision(ctx, reader.opts, request)
}
func (reader runtimeSDDReader) RenderProjection(ctx context.Context, request sdd.RenderProjectionRequest) (sdd.ProjectionDocument, error) {
	return reader.runtime.RenderProjection(ctx, reader.opts, request)
}
func (reader runtimeSDDReader) CompareProjection(ctx context.Context, request sdd.CompareProjectionRequest) (sdd.ProjectionComparison, error) {
	return reader.runtime.CompareProjection(ctx, reader.opts, request)
}
func (reader runtimeSDDReader) RecordProjection(ctx context.Context, request sdd.RecordProjectionRequest) (sdd.Projection, error) {
	return reader.runtime.RecordProjection(ctx, reader.opts, request)
}
func (reader runtimeSDDReader) ProjectionStatus(ctx context.Context, request sdd.ProjectionStatusRequest) (sdd.Projection, error) {
	return reader.runtime.ProjectionStatus(ctx, reader.opts, request)
}

type unavailableSDDReader struct{}

func (unavailableSDDReader) CreateChange(context.Context, sdd.CreateChangeRequest) (sdd.Change, error) {
	return sdd.Change{}, ErrUnavailable
}
func (unavailableSDDReader) ListChanges(context.Context, sdd.ListChangesRequest) ([]sdd.Change, error) {
	return nil, ErrUnavailable
}
func (unavailableSDDReader) GetChange(context.Context, sdd.GetChangeRequest) (sdd.Change, error) {
	return sdd.Change{}, ErrUnavailable
}
func (unavailableSDDReader) UpdateInteractionMode(context.Context, sdd.UpdateInteractionModeRequest) (sdd.Change, error) {
	return sdd.Change{}, ErrUnavailable
}
func (unavailableSDDReader) TransitionChange(context.Context, sdd.TransitionChangeRequest) (sdd.Change, error) {
	return sdd.Change{}, ErrUnavailable
}
func (unavailableSDDReader) SaveRevision(context.Context, sdd.SaveRevisionRequest) (sdd.Revision, error) {
	return sdd.Revision{}, ErrUnavailable
}
func (unavailableSDDReader) GetRevision(context.Context, sdd.GetRevisionRequest) (sdd.Revision, error) {
	return sdd.Revision{}, ErrUnavailable
}
func (unavailableSDDReader) ListRevisions(context.Context, sdd.ListRevisionsRequest) ([]sdd.Revision, error) {
	return nil, ErrUnavailable
}
func (unavailableSDDReader) AcceptRevision(context.Context, sdd.AcceptRevisionRequest) (sdd.Revision, error) {
	return sdd.Revision{}, ErrUnavailable
}
func (unavailableSDDReader) RenderProjection(context.Context, sdd.RenderProjectionRequest) (sdd.ProjectionDocument, error) {
	return sdd.ProjectionDocument{}, ErrUnavailable
}
func (unavailableSDDReader) CompareProjection(context.Context, sdd.CompareProjectionRequest) (sdd.ProjectionComparison, error) {
	return sdd.ProjectionComparison{}, ErrUnavailable
}
func (unavailableSDDReader) RecordProjection(context.Context, sdd.RecordProjectionRequest) (sdd.Projection, error) {
	return sdd.Projection{}, ErrUnavailable
}
func (unavailableSDDReader) ProjectionStatus(context.Context, sdd.ProjectionStatusRequest) (sdd.Projection, error) {
	return sdd.Projection{}, ErrUnavailable
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

// New creates a server whose project identity is resolved once from workspace.
// Missing or inaccessible read-only storage returns ErrUnavailable.
func New(ctx context.Context, workspace string, opts config.Options) (*Server, error) {
	return newWithReader(ctx, workspace, runtimeReader{runtime: runtime.NewMemory("mcp", true), opts: opts})
}

// NewFull creates an explicitly write-capable server. Callers must opt in; no
// caller identity is inferred from the MCP transport.
func NewFull(ctx context.Context, workspace string, opts config.Options) (*Server, error) {
	return newFullWithReaders(ctx, workspace, runtimeReader{runtime: runtime.NewMemory("mcp", false), opts: opts}, runtimeSDDReader{runtime: runtime.NewSDD(), opts: opts})
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
	return newFullWithReaders(ctx, workspace, reader, unavailableSDDReader{})
}

func newFullWithReaders(ctx context.Context, workspace string, reader memoryReader, sddReader sddReader) (*Server, error) {
	server, err := newServerWithReader(ctx, workspace, reader, true)
	if err != nil {
		return nil, err
	}
	server.sdd = sddReader
	return server, nil
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
	home, _ := os.UserHomeDir()
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return nil, ErrInvalidInput
	}
	server := &Server{reader: reader, project: project, full: full, workspace: absolute, skillHome: home}
	server.server = sdk.NewServer(&sdk.Implementation{Name: "vgxness-memory", Version: "0.1.0"}, nil)
	annotations := &sdk.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: boolPtr(false), IdempotentHint: true, OpenWorldHint: boolPtr(false)}
	sdk.AddTool(server.server, &sdk.Tool{Name: "memory_recent", Description: "Read recent project memory entries. This tool never writes data.", Annotations: annotations}, server.callRecent)
	sdk.AddTool(server.server, &sdk.Tool{Name: "memory_search", Description: "Search project memory entries. This tool never writes data.", Annotations: annotations}, server.callSearch)
	sdk.AddTool(server.server, &sdk.Tool{Name: "skill_registry_list", Description: "List skill metadata from the current workspace policy. This tool never writes data.", Annotations: annotations}, server.callSkillList)
	sdk.AddTool(server.server, &sdk.Tool{Name: "skill_registry_resolve", Description: "Resolve one skill to an exact binding from the current workspace policy. This tool never writes data.", Annotations: annotations}, server.callSkillResolve)
	sdk.AddTool(server.server, &sdk.Tool{Name: "skill_registry_verify", Description: "Verify one exact skill binding from the current workspace policy. This tool never writes data.", Annotations: annotations}, server.callSkillVerify)
	if full {
		writeAnnotations := &sdk.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(false), IdempotentHint: false, OpenWorldHint: boolPtr(false)}
		sdk.AddTool(server.server, &sdk.Tool{Name: "memory_get", Description: "Read one full project memory entry by exact ID. This tool never writes data.", Annotations: annotations}, server.callGet)
		sdk.AddTool(server.server, &sdk.Tool{Name: "memory_save", Description: "Write a durable project memory entry. This tool stores data.", Annotations: writeAnnotations}, server.callSave)
		sdk.AddTool(server.server, &sdk.Tool{Name: "memory_forget", Description: "Archive one exact project memory entry. This tool changes stored data and removes it from normal search.", Annotations: &sdk.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(true), IdempotentHint: false, OpenWorldHint: boolPtr(false)}}, server.callForget)
		sdk.AddTool(server.server, &sdk.Tool{Name: "sdd_create", Description: "Create one structured SDD change. This stores state only and does not execute a workflow.", Annotations: &sdk.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(false), IdempotentHint: true, OpenWorldHint: boolPtr(false)}}, server.callSDDCreate)
		sdk.AddTool(server.server, &sdk.Tool{Name: "sdd_list", Description: "List structured SDD changes for the trusted workspace project.", Annotations: annotations}, server.callSDDList)
		sdk.AddTool(server.server, &sdk.Tool{Name: "sdd_get", Description: "Get one structured SDD change by exact ID.", Annotations: annotations}, server.callSDDGet)
		sdk.AddTool(server.server, &sdk.Tool{Name: "sdd_set_interaction_mode", Description: "Change an SDD change interaction mode using optimistic state versioning. This tool changes stored data.", Annotations: writeAnnotations}, server.callSDDSetInteractionMode)
		sdk.AddTool(server.server, &sdk.Tool{Name: "sdd_transition", Description: "Record a legal SDD phase transition or cancellation using optimistic state versioning. This tool changes stored data.", Annotations: &sdk.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(true), IdempotentHint: false, OpenWorldHint: boolPtr(false)}}, server.callSDDTransition)
		sdk.AddTool(server.server, &sdk.Tool{Name: "sdd_save_revision", Description: "Save a candidate SDD artifact revision. This tool changes stored data.", Annotations: writeAnnotations}, server.callSDDSaveRevision)
		sdk.AddTool(server.server, &sdk.Tool{Name: "sdd_get_revision", Description: "Get one SDD artifact revision by exact change and revision IDs.", Annotations: annotations}, server.callSDDGetRevision)
		sdk.AddTool(server.server, &sdk.Tool{Name: "sdd_list_revisions", Description: "List bounded SDD revision summaries without body content.", Annotations: annotations}, server.callSDDListRevisions)
		sdk.AddTool(server.server, &sdk.Tool{Name: "sdd_accept_revision", Description: "Accept one immutable candidate revision using optimistic state versioning. This tool changes stored data.", Annotations: writeAnnotations}, server.callSDDAcceptRevision)
		sdk.AddTool(server.server, &sdk.Tool{Name: "sdd_render_projection", Description: "Render deterministic managed OpenSpec bytes and a repository-relative target path.", Annotations: annotations}, server.callSDDRenderProjection)
		sdk.AddTool(server.server, &sdk.Tool{Name: "sdd_compare_projection", Description: "Compare caller-supplied OpenSpec bytes with accepted memory state. This tool never writes data.", Annotations: annotations}, server.callSDDCompareProjection)
		sdk.AddTool(server.server, &sdk.Tool{Name: "sdd_record_projection", Description: "Record supplied projection evidence. This tool changes stored data without filesystem access.", Annotations: &sdk.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(true), IdempotentHint: false, OpenWorldHint: boolPtr(false)}}, server.callSDDRecordProjection)
		sdk.AddTool(server.server, &sdk.Tool{Name: "sdd_projection_status", Description: "Read recorded projection status for one SDD artifact.", Annotations: annotations}, server.callSDDProjectionStatus)
	}
	return server, nil
}

func boolPtr(value bool) *bool { return &value }

// Run serves MCP requests over an injected transport until ctx is cancelled or
// the transport closes. It never writes diagnostic output to the transport.
func (server *Server) Run(ctx context.Context, transport sdk.Transport) error {
	return server.server.Run(ctx, transport)
}

type recentInput struct {
	Limit int `json:"limit,omitempty"`
}

type searchInput struct {
	Query string `json:"query" jsonschema:"required"`
	Limit int    `json:"limit,omitempty"`
}

type getInput struct {
	ID string `json:"id" jsonschema:"required"`
}
type skillResolveInput struct {
	Name string `json:"name" jsonschema:"required"`
	Host string `json:"host,omitempty"`
}
type skillVerifyInput struct {
	Binding skillregistry.Binding `json:"binding" jsonschema:"required"`
	Host    string                `json:"host,omitempty"`
}
type skillListInput struct {
	Host string `json:"host,omitempty"`
}
type skillListResult struct {
	Snapshot skillregistry.Snapshot `json:"snapshot"`
}
type skillVerifyResult struct {
	Binding  skillregistry.Binding `json:"binding"`
	Verified bool                  `json:"verified"`
}

type saveInput struct {
	Title   string `json:"title" jsonschema:"required"`
	Content string `json:"content" jsonschema:"required"`
	Type    string `json:"type,omitempty"`
	Topic   string `json:"topic,omitempty"`
}

type sddCreateInput struct {
	IdempotencyKey  string              `json:"idempotencyKey" jsonschema:"required"`
	Title           string              `json:"title" jsonschema:"required"`
	Backend         sdd.Backend         `json:"backend" jsonschema:"required"`
	InteractionMode sdd.InteractionMode `json:"interactionMode" jsonschema:"required"`
	Plan            sdd.Plan            `json:"plan" jsonschema:"required"`
}
type sddListInput struct {
	Status sdd.ChangeStatus `json:"status,omitempty"`
	Limit  float64          `json:"limit,omitempty"`
}
type sddGetInput struct {
	ID string `json:"id" jsonschema:"required"`
}
type sddModeInput struct {
	ChangeID             string              `json:"changeId" jsonschema:"required"`
	InteractionMode      sdd.InteractionMode `json:"interactionMode" jsonschema:"required"`
	ExpectedStateVersion float64             `json:"expectedStateVersion" jsonschema:"required"`
}
type sddTransitionInput struct {
	ChangeID             string    `json:"changeId" jsonschema:"required"`
	TargetPhase          sdd.Phase `json:"targetPhase,omitempty"`
	Cancel               bool      `json:"cancel,omitempty"`
	ExpectedStateVersion float64   `json:"expectedStateVersion" jsonschema:"required"`
}
type sddSaveRevisionInput struct {
	ChangeID             string                    `json:"changeId" jsonschema:"required"`
	Artifact             sdd.Phase                 `json:"artifact" jsonschema:"required"`
	Content              string                    `json:"content" jsonschema:"required"`
	ExternalLocation     string                    `json:"externalLocation,omitempty"`
	Digest               sdd.Digest                `json:"digest,omitempty"`
	Inputs               []sddRevisionBindingInput `json:"inputs,omitempty"`
	InputDigest          sdd.Digest                `json:"inputDigest,omitempty"`
	ExpectedStateVersion float64                   `json:"expectedStateVersion" jsonschema:"required"`
}
type sddRevisionBindingInput struct {
	ArtifactID string     `json:"artifactId" jsonschema:"required"`
	RevisionID string     `json:"revisionId" jsonschema:"required"`
	Digest     sdd.Digest `json:"digest" jsonschema:"required"`
}
type sddGetRevisionInput struct {
	ChangeID   string `json:"changeId" jsonschema:"required"`
	RevisionID string `json:"revisionId" jsonschema:"required"`
}
type sddListRevisionsInput struct {
	ChangeID string    `json:"changeId" jsonschema:"required"`
	Artifact sdd.Phase `json:"artifact,omitempty"`
	Limit    float64   `json:"limit,omitempty"`
}
type sddAcceptRevisionInput struct {
	ChangeID             string  `json:"changeId" jsonschema:"required"`
	RevisionID           string  `json:"revisionId" jsonschema:"required"`
	ExpectedStateVersion float64 `json:"expectedStateVersion" jsonschema:"required"`
}
type sddRenderProjectionInput struct {
	ChangeID   string `json:"changeId" jsonschema:"required"`
	RevisionID string `json:"revisionId" jsonschema:"required"`
}
type sddCompareProjectionInput struct {
	ChangeID          string `json:"changeId" jsonschema:"required"`
	RevisionID        string `json:"revisionId" jsonschema:"required"`
	RelativePath      string `json:"relativePath" jsonschema:"required"`
	ProjectionContent string `json:"projectionContent,omitempty"`
	Missing           bool   `json:"missing,omitempty"`
	Symlink           bool   `json:"symlink,omitempty"`
}
type sddRecordProjectionInput struct {
	ChangeID             string               `json:"changeId" jsonschema:"required"`
	ArtifactID           string               `json:"artifactId" jsonschema:"required"`
	RevisionID           string               `json:"revisionId" jsonschema:"required"`
	Status               sdd.ProjectionStatus `json:"status" jsonschema:"required"`
	Digest               sdd.Digest           `json:"digest" jsonschema:"required"`
	Location             string               `json:"location" jsonschema:"required"`
	ExpectedStateVersion float64              `json:"expectedStateVersion" jsonschema:"required"`
}
type sddProjectionStatusInput struct {
	ChangeID   string `json:"changeId" jsonschema:"required"`
	ArtifactID string `json:"artifactId" jsonschema:"required"`
}

type result struct {
	Entries []entry `json:"entries"`
}

type sddChangesResult struct {
	Changes []sdd.Change `json:"changes"`
}

type sddRevisionsResult struct {
	Revisions []sdd.Revision `json:"revisions"`
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
	item, err := server.reader.Remember(ctx, memory.Remember{Title: input.Title, Content: input.Content, Type: input.Type, TopicKey: input.Topic, Project: server.project, Scope: memory.ScopeProject})
	return shapeEntry(ctx, item, err, true)
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

func (server *Server) sddCreate(ctx context.Context, input sddCreateInput) (sdd.Change, error) {
	request := sdd.CreateChangeRequest{Project: server.project, IdempotencyKey: input.IdempotencyKey, Title: input.Title, Backend: input.Backend, InteractionMode: input.InteractionMode, Plan: input.Plan}
	if request.Validate() != nil {
		return sdd.Change{}, ErrInvalidInput
	}
	return server.sddCall(ctx, func() (sdd.Change, error) {
		return server.sdd.CreateChange(ctx, request)
	})
}
func (server *Server) sddList(ctx context.Context, input sddListInput) ([]sdd.Change, error) {
	limit, err := sddLimit(input.Limit)
	if err != nil {
		return nil, err
	}
	if err := (sdd.ListChangesRequest{Project: server.project, Status: input.Status, Limit: limit}).Validate(); err != nil {
		return nil, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if server.sdd == nil {
		return nil, ErrUnavailable
	}
	items, err := server.sdd.ListChanges(ctx, sdd.ListChangesRequest{Project: server.project, Status: input.Status, Limit: limit})
	return items, shapeSDDError(ctx, err)
}
func (server *Server) sddGet(ctx context.Context, input sddGetInput) (sdd.Change, error) {
	request := sdd.GetChangeRequest{Project: server.project, ID: input.ID}
	if request.Validate() != nil {
		return sdd.Change{}, ErrInvalidInput
	}
	return server.sddCall(ctx, func() (sdd.Change, error) {
		return server.sdd.GetChange(ctx, request)
	})
}
func (server *Server) sddSetInteractionMode(ctx context.Context, input sddModeInput) (sdd.Change, error) {
	version, err := sddVersion(input.ExpectedStateVersion)
	if err != nil {
		return sdd.Change{}, err
	}
	request := sdd.UpdateInteractionModeRequest{Project: server.project, ChangeID: input.ChangeID, InteractionMode: input.InteractionMode, ExpectedStateVersion: version}
	if request.Validate() != nil {
		return sdd.Change{}, ErrInvalidInput
	}
	return server.sddCall(ctx, func() (sdd.Change, error) {
		return server.sdd.UpdateInteractionMode(ctx, request)
	})
}
func (server *Server) sddTransition(ctx context.Context, input sddTransitionInput) (sdd.Change, error) {
	version, err := sddVersion(input.ExpectedStateVersion)
	if err != nil {
		return sdd.Change{}, err
	}
	request := sdd.TransitionChangeRequest{Project: server.project, ChangeID: input.ChangeID, TargetPhase: input.TargetPhase, Cancel: input.Cancel, ExpectedStateVersion: version}
	if request.Validate() != nil {
		return sdd.Change{}, ErrInvalidInput
	}
	return server.sddCall(ctx, func() (sdd.Change, error) {
		return server.sdd.TransitionChange(ctx, request)
	})
}
func (server *Server) sddSaveRevision(ctx context.Context, input sddSaveRevisionInput) (sdd.Revision, error) {
	version, err := sddVersion(input.ExpectedStateVersion)
	if err != nil || len(input.Content) > 48<<10 || len(input.Inputs) > 32 {
		return sdd.Revision{}, ErrInvalidInput
	}
	inputs := make([]sdd.RevisionBinding, len(input.Inputs))
	for index, value := range input.Inputs {
		inputs[index] = sdd.RevisionBinding{ArtifactID: value.ArtifactID, RevisionID: value.RevisionID, Digest: value.Digest}
	}
	request := sdd.SaveRevisionRequest{Project: server.project, ChangeID: input.ChangeID, Artifact: input.Artifact, Content: []byte(input.Content), ExternalLocation: input.ExternalLocation, Digest: input.Digest, Inputs: inputs, InputDigest: input.InputDigest, ExpectedStateVersion: version}
	return sddValue(server, ctx, request.Validate, func() (sdd.Revision, error) { return server.sdd.SaveRevision(ctx, request) })
}
func (server *Server) sddGetRevision(ctx context.Context, input sddGetRevisionInput) (sdd.Revision, error) {
	request := sdd.GetRevisionRequest{Project: server.project, ChangeID: input.ChangeID, RevisionID: input.RevisionID}
	return sddValue(server, ctx, request.Validate, func() (sdd.Revision, error) { return server.sdd.GetRevision(ctx, request) })
}
func (server *Server) sddListRevisions(ctx context.Context, input sddListRevisionsInput) ([]sdd.Revision, error) {
	limit, err := sddRevisionLimit(input.Limit)
	if err != nil {
		return nil, err
	}
	request := sdd.ListRevisionsRequest{Project: server.project, ChangeID: input.ChangeID, Artifact: input.Artifact, Limit: limit}
	return sddValue(server, ctx, request.Validate, func() ([]sdd.Revision, error) { return server.sdd.ListRevisions(ctx, request) })
}
func (server *Server) sddAcceptRevision(ctx context.Context, input sddAcceptRevisionInput) (sdd.Revision, error) {
	version, err := sddVersion(input.ExpectedStateVersion)
	if err != nil {
		return sdd.Revision{}, err
	}
	request := sdd.AcceptRevisionRequest{Project: server.project, ChangeID: input.ChangeID, RevisionID: input.RevisionID, ExpectedStateVersion: version}
	return sddValue(server, ctx, request.Validate, func() (sdd.Revision, error) { return server.sdd.AcceptRevision(ctx, request) })
}
func (server *Server) sddRenderProjection(ctx context.Context, input sddRenderProjectionInput) (sdd.ProjectionDocument, error) {
	request := sdd.RenderProjectionRequest{Project: server.project, ChangeID: input.ChangeID, RevisionID: input.RevisionID}
	return sddValue(server, ctx, request.Validate, func() (sdd.ProjectionDocument, error) { return server.sdd.RenderProjection(ctx, request) })
}
func (server *Server) sddCompareProjection(ctx context.Context, input sddCompareProjectionInput) (sdd.ProjectionComparison, error) {
	if input.Symlink || len(input.ProjectionContent) > 48<<10 || len(input.RelativePath) > 512 {
		return sdd.ProjectionComparison{}, ErrInvalidInput
	}
	request := sdd.CompareProjectionRequest{Project: server.project, ChangeID: input.ChangeID, RevisionID: input.RevisionID, Input: sdd.ProjectionInput{RelativePath: input.RelativePath, Content: []byte(input.ProjectionContent), Missing: input.Missing, Symlink: input.Symlink}}
	return sddValue(server, ctx, request.Validate, func() (sdd.ProjectionComparison, error) { return server.sdd.CompareProjection(ctx, request) })
}
func (server *Server) sddRecordProjection(ctx context.Context, input sddRecordProjectionInput) (sdd.Projection, error) {
	version, err := sddVersion(input.ExpectedStateVersion)
	if err != nil {
		return sdd.Projection{}, err
	}
	request := sdd.RecordProjectionRequest{Project: server.project, ChangeID: input.ChangeID, ArtifactID: input.ArtifactID, RevisionID: input.RevisionID, Status: input.Status, Digest: input.Digest, Location: input.Location, ExpectedStateVersion: version}
	return sddValue(server, ctx, request.Validate, func() (sdd.Projection, error) { return server.sdd.RecordProjection(ctx, request) })
}
func (server *Server) sddProjectionStatus(ctx context.Context, input sddProjectionStatusInput) (sdd.Projection, error) {
	request := sdd.ProjectionStatusRequest{Project: server.project, ChangeID: input.ChangeID, ArtifactID: input.ArtifactID}
	return sddValue(server, ctx, request.Validate, func() (sdd.Projection, error) { return server.sdd.ProjectionStatus(ctx, request) })
}
func sddValue[T any](server *Server, ctx context.Context, validate func() error, call func() (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if validate() != nil {
		return zero, ErrInvalidInput
	}
	if server.sdd == nil {
		return zero, ErrUnavailable
	}
	value, err := call()
	return value, shapeSDDError(ctx, err)
}
func (server *Server) sddCall(ctx context.Context, call func() (sdd.Change, error)) (sdd.Change, error) {
	if err := ctx.Err(); err != nil {
		return sdd.Change{}, err
	}
	if server.sdd == nil {
		return sdd.Change{}, ErrUnavailable
	}
	item, err := call()
	return item, shapeSDDError(ctx, err)
}
func sddLimit(value float64) (int, error) {
	if value == 0 {
		return 20, nil
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value || value < 1 || value > 100 {
		return 0, ErrInvalidInput
	}
	return int(math.Trunc(value)), nil
}
func sddRevisionLimit(value float64) (int, error) {
	if value == 0 {
		return 50, nil
	}
	return sddLimit(value)
}
func sddVersion(value float64) (int64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value || value < 1 || value > maxJSONInteger {
		return 0, ErrInvalidInput
	}
	return int64(math.Trunc(value)), nil
}
func shapeSDDError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, sdd.ErrInvalid) || errors.Is(err, sdd.ErrIllegalTransition) || errors.Is(err, sdd.ErrDigestMismatch) {
		return ErrInvalidInput
	}
	if errors.Is(err, sdd.ErrNotFound) {
		return ErrNotFound
	}
	if errors.Is(err, sdd.ErrStaleState) {
		return ErrStale
	}
	if errors.Is(err, sdd.ErrConflict) || errors.Is(err, sdd.ErrInputsChanged) || errors.Is(err, sdd.ErrImmutable) {
		return ErrConflict
	}
	if errors.Is(err, sdd.ErrChangeCancelled) {
		return ErrSDDCancelled
	}
	return ErrUnavailable
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
	entries, err := server.reader.Recall(ctx, memory.Recall{Query: input.Query, Project: server.project, Scope: memory.ScopeProject, Limit: input.Limit, MatchAny: true})
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

func (server *Server) skills(ctx context.Context, host string) (skillregistry.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return skillregistry.Snapshot{}, err
	}
	if !validSkillHost(host) {
		return skillregistry.Snapshot{}, ErrInvalidInput
	}
	if server.skillHome == "" {
		return skillregistry.Snapshot{}, ErrUnavailable
	}
	return skillregistry.Scan(ctx, skillregistry.Options{CWD: server.workspace, Home: server.skillHome, Host: host})
}
func (server *Server) skillList(ctx context.Context, input skillListInput) (skillListResult, error) {
	snapshot, err := server.skills(ctx, input.Host)
	if err != nil {
		return skillListResult{}, err
	}
	return skillListResult{Snapshot: snapshot}, nil
}
func (server *Server) skillResolve(ctx context.Context, input skillResolveInput) (skillregistry.Selection, error) {
	if strings.TrimSpace(input.Name) == "" {
		return skillregistry.Selection{}, ErrInvalidInput
	}
	snapshot, err := server.skills(ctx, input.Host)
	if err != nil {
		return skillregistry.Selection{}, err
	}
	return snapshot.Resolve(input.Name)
}
func (server *Server) skillVerify(ctx context.Context, input skillVerifyInput) (skillVerifyResult, error) {
	snapshot, err := server.skills(ctx, input.Host)
	if err != nil {
		return skillVerifyResult{}, err
	}
	if err := snapshot.Verify(input.Binding); err != nil {
		return skillVerifyResult{}, err
	}
	return skillVerifyResult{Binding: input.Binding, Verified: true}, nil
}
func (server *Server) callSkillList(ctx context.Context, _ *sdk.CallToolRequest, input skillListInput) (*sdk.CallToolResult, skillListResult, error) {
	output, err := server.skillList(ctx, input)
	return skillResponse(err, output)
}
func validSkillHost(host string) bool {
	return host == "" || host == "common" || host == "codex" || host == "opencode"
}
func (server *Server) callSkillResolve(ctx context.Context, _ *sdk.CallToolRequest, input skillResolveInput) (*sdk.CallToolResult, skillregistry.Selection, error) {
	output, err := server.skillResolve(ctx, input)
	return skillResponse(err, output)
}
func (server *Server) callSkillVerify(ctx context.Context, _ *sdk.CallToolRequest, input skillVerifyInput) (*sdk.CallToolResult, skillVerifyResult, error) {
	output, err := server.skillVerify(ctx, input)
	return skillResponse(err, output)
}
func skillResponse[T any](err error, output T) (*sdk.CallToolResult, T, error) {
	if err == nil {
		return toolText("Skill registry result returned.", false), output, nil
	}
	var zero T
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return toolText("request cancelled", true), zero, nil
	}
	if errors.Is(err, ErrInvalidInput) {
		return toolText("invalid tool input", true), zero, nil
	}
	if errors.Is(err, skillregistry.ErrNotFound) {
		return toolText("skill not found", true), zero, nil
	}
	if errors.Is(err, skillregistry.ErrAmbiguous) {
		return toolText("skill selection is ambiguous", true), zero, nil
	}
	if errors.Is(err, skillregistry.ErrDrift) {
		return toolText("skill binding drifted", true), zero, nil
	}
	return toolText("skill registry unavailable", true), zero, nil
}

func (server *Server) callGet(ctx context.Context, _ *sdk.CallToolRequest, input getInput) (*sdk.CallToolResult, entry, error) {
	output, err := server.get(ctx, input)
	return entryResponse(err, output)
}

func (server *Server) callSave(ctx context.Context, _ *sdk.CallToolRequest, input saveInput) (*sdk.CallToolResult, entry, error) {
	output, err := server.save(ctx, input)
	return entryResponse(err, output)
}

func (server *Server) callForget(ctx context.Context, _ *sdk.CallToolRequest, input getInput) (*sdk.CallToolResult, entry, error) {
	output, err := server.forget(ctx, input)
	return entryResponse(err, output)
}

func (server *Server) callSDDCreate(ctx context.Context, _ *sdk.CallToolRequest, input sddCreateInput) (*sdk.CallToolResult, sdd.Change, error) {
	output, err := server.sddCreate(ctx, input)
	return sddResponse(err, output)
}
func (server *Server) callSDDList(ctx context.Context, _ *sdk.CallToolRequest, input sddListInput) (*sdk.CallToolResult, sddChangesResult, error) {
	output, err := server.sddList(ctx, input)
	return sddListResponse(err, sddChangesResult{Changes: output})
}
func (server *Server) callSDDGet(ctx context.Context, _ *sdk.CallToolRequest, input sddGetInput) (*sdk.CallToolResult, sdd.Change, error) {
	output, err := server.sddGet(ctx, input)
	return sddResponse(err, output)
}
func (server *Server) callSDDSetInteractionMode(ctx context.Context, _ *sdk.CallToolRequest, input sddModeInput) (*sdk.CallToolResult, sdd.Change, error) {
	output, err := server.sddSetInteractionMode(ctx, input)
	return sddResponse(err, output)
}
func (server *Server) callSDDTransition(ctx context.Context, _ *sdk.CallToolRequest, input sddTransitionInput) (*sdk.CallToolResult, sdd.Change, error) {
	output, err := server.sddTransition(ctx, input)
	return sddResponse(err, output)
}
func (server *Server) callSDDSaveRevision(ctx context.Context, _ *sdk.CallToolRequest, input sddSaveRevisionInput) (*sdk.CallToolResult, sdd.Revision, error) {
	output, err := server.sddSaveRevision(ctx, input)
	return sddToolResponse(err, output)
}
func (server *Server) callSDDGetRevision(ctx context.Context, _ *sdk.CallToolRequest, input sddGetRevisionInput) (*sdk.CallToolResult, sdd.Revision, error) {
	output, err := server.sddGetRevision(ctx, input)
	return sddToolResponse(err, output)
}
func (server *Server) callSDDListRevisions(ctx context.Context, _ *sdk.CallToolRequest, input sddListRevisionsInput) (*sdk.CallToolResult, sddRevisionsResult, error) {
	output, err := server.sddListRevisions(ctx, input)
	return sddToolResponse(err, sddRevisionsResult{Revisions: output})
}
func (server *Server) callSDDAcceptRevision(ctx context.Context, _ *sdk.CallToolRequest, input sddAcceptRevisionInput) (*sdk.CallToolResult, sdd.Revision, error) {
	output, err := server.sddAcceptRevision(ctx, input)
	return sddToolResponse(err, output)
}
func (server *Server) callSDDRenderProjection(ctx context.Context, _ *sdk.CallToolRequest, input sddRenderProjectionInput) (*sdk.CallToolResult, sdd.ProjectionDocument, error) {
	output, err := server.sddRenderProjection(ctx, input)
	return sddToolResponse(err, output)
}
func (server *Server) callSDDCompareProjection(ctx context.Context, _ *sdk.CallToolRequest, input sddCompareProjectionInput) (*sdk.CallToolResult, sdd.ProjectionComparison, error) {
	output, err := server.sddCompareProjection(ctx, input)
	return sddToolResponse(err, output)
}
func (server *Server) callSDDRecordProjection(ctx context.Context, _ *sdk.CallToolRequest, input sddRecordProjectionInput) (*sdk.CallToolResult, sdd.Projection, error) {
	output, err := server.sddRecordProjection(ctx, input)
	return sddToolResponse(err, output)
}
func (server *Server) callSDDProjectionStatus(ctx context.Context, _ *sdk.CallToolRequest, input sddProjectionStatusInput) (*sdk.CallToolResult, sdd.Projection, error) {
	output, err := server.sddProjectionStatus(ctx, input)
	return sddToolResponse(err, output)
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

func entryResponse(err error, output entry) (*sdk.CallToolResult, entry, error) {
	if err == nil {
		return toolText("Memory entry returned.", false), output, nil
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

func sddResponse(err error, output sdd.Change) (*sdk.CallToolResult, sdd.Change, error) {
	if err == nil {
		return toolText("SDD change returned.", false), output, nil
	}
	return sddErrorResponse(err), sdd.Change{}, nil
}
func sddListResponse(err error, output sddChangesResult) (*sdk.CallToolResult, sddChangesResult, error) {
	if err == nil {
		return toolText(fmt.Sprintf("Returned %d SDD changes.", len(output.Changes)), false), output, nil
	}
	return sddErrorResponse(err), sddChangesResult{}, nil
}
func sddToolResponse[T any](err error, output T) (*sdk.CallToolResult, T, error) {
	if err == nil {
		return toolText("SDD result returned.", false), output, nil
	}
	var zero T
	return sddErrorResponse(err), zero, nil
}
func sddErrorResponse(err error) *sdk.CallToolResult {
	if errors.Is(err, ErrSDDCancelled) {
		return toolText("SDD change is cancelled", true)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return toolText("request cancelled", true)
	}
	if errors.Is(err, ErrInvalidInput) {
		return toolText("invalid tool input", true)
	}
	if errors.Is(err, ErrNotFound) {
		return toolText("SDD record not found", true)
	}
	if errors.Is(err, ErrStale) {
		return toolText("SDD state version changed", true)
	}
	if errors.Is(err, ErrConflict) {
		return toolText("SDD state changed", true)
	}
	return toolText("SDD service unavailable", true)
}

func toolText(text string, isError bool) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}, IsError: isError}
}
