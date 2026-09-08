// Package tools adapts closed Pi protocol operations to existing Go runtimes.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/vgxness/vgxness/internal/app/runtime"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/providers/pi"
	"github.com/vgxness/vgxness/internal/sdd"
)

type Dispatcher struct {
	Options  config.Options
	Memory   runtime.Memory
	SDD      runtime.SDD
	sessions *sessionAuthority
}

func New(opts config.Options, readOnly bool) Dispatcher {
	return Dispatcher{Options: opts, Memory: runtime.NewMemory("pi", readOnly), SDD: runtime.NewSDD(), sessions: &sessionAuthority{byHandle: map[string]sessionSecret{}}}
}

var operationNames = []string{
	"model.resolve", "memory.remember", "memory.recall", "memory.recent", "memory.get", "memory.forget", "memory.project.resolve", "memory.project.initialize", "memory.sync.configure", "memory.sync.status", "memory.sync", "memory.sync.backfill", "memory.sync.repair_project", "memory.sync.reseed", "memory.sync.rejoin", "memory.session.start", "memory.session.checkpoint", "memory.session.renew", "memory.session.end", "memory.session.context", "memory.session.draft_save",
	"sdd.create", "sdd.list", "sdd.get", "sdd.set_interaction_mode", "sdd.save_revision", "sdd.get_revision", "sdd.list_revisions", "sdd.accept_revision", "sdd.transition", "sdd.cancel", "sdd.projection_status", "sdd.record_projection", "sdd.render_projection", "sdd.compare_projection",
}

func OperationNames() []string { return append([]string(nil), operationNames...) }

func (d Dispatcher) project(ctx context.Context) (string, error) {
	return d.Memory.ResolveProject(ctx, d.Options, d.Options.ProjectDir)
}
func (d Dispatcher) Dispatch(ctx context.Context, r pi.Request) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, err := d.project(ctx)
	if err != nil {
		return nil, err
	}
	switch r.Operation {
	case "model.resolve":
		return ResolveModel(r.Payload)
	case "memory.recall":
		var v memory.Recall
		if err = decode(r.Payload, &v, "query", "type", "topicKey", "scope", "states", "limit", "matchAny"); err == nil {
			if v.Scope != "" && v.Scope != memory.ScopeProject {
				return nil, errors.New("project scope required")
			}
			v.Project = project
			if v.Scope == "" {
				v.Scope = memory.ScopeProject
			}
			return d.Memory.Recall(ctx, d.Options, v)
		}
	case "memory.get":
		var v memory.Lookup
		if err = decode(r.Payload, &v, "id", "scope"); err == nil {
			if v.Scope != "" && v.Scope != memory.ScopeProject {
				return nil, errors.New("project scope required")
			}
			v.Project = project
			if v.Scope == "" {
				v.Scope = memory.ScopeProject
			}
			return d.Memory.Get(ctx, d.Options, v)
		}
	case "memory.recent":
		var v memory.Recent
		if err = decode(r.Payload, &v, "scope", "states", "limit"); err == nil {
			if v.Scope != "" && v.Scope != memory.ScopeProject {
				return nil, errors.New("project scope required")
			}
			v.Project = project
			if v.Scope == "" {
				v.Scope = memory.ScopeProject
			}
			return d.Memory.Recent(ctx, d.Options, v)
		}
	case "memory.remember":
		var v memory.Remember
		if err = decode(r.Payload, &v, "title", "content", "type", "topicKey", "session", "sourceProvider", "sourceId", "scope", "state", "references"); err == nil {
			if v.Scope != "" && v.Scope != memory.ScopeProject {
				return nil, errors.New("project scope required")
			}
			v.Project = project
			return d.Memory.Remember(ctx, d.Options, v)
		}
	case "memory.forget":
		var v memory.Forget
		if err = decode(r.Payload, &v, "id", "scope"); err == nil {
			if v.Scope != "" && v.Scope != memory.ScopeProject {
				return nil, errors.New("project scope required")
			}
			v.Project = project
			if v.Scope == "" {
				v.Scope = memory.ScopeProject
			}
			return d.Memory.Forget(ctx, d.Options, v)
		}
	case "memory.project.resolve":
		if err = decode(r.Payload, &struct{}{}); err == nil {
			return project, nil
		}
	case "memory.project.initialize":
		if err = decode(r.Payload, &struct{}{}); err == nil {
			return d.Memory.InitializeProject(ctx, d.Options, d.Options.ProjectDir)
		}
	case "memory.sync.configure":
		var v syncConfigure
		if err = decode(r.Payload, &v, "endpoint", "deviceId"); err == nil {
			if d.Options.CredentialFile == "" {
				return nil, errors.New("credential file required")
			}
			return d.Memory.ConfigureSync(ctx, d.Options, v.Endpoint, v.DeviceID, "")
		}
	case "memory.sync.status":
		if err = decode(r.Payload, &struct{}{}); err == nil {
			return d.Memory.SyncStatus(ctx, d.Options)
		}
	case "memory.sync":
		if err = decode(r.Payload, &struct{}{}); err == nil {
			return d.Memory.Sync(ctx, d.Options)
		}
	case "memory.sync.backfill":
		var v backfill
		if err = decode(r.Payload, &v, "limit"); err == nil {
			return d.Memory.BackfillSyncProject(ctx, d.Options, d.Options.ProjectDir, v.Limit)
		}
	case "memory.sync.repair_project":
		var v repair
		if err = decode(r.Payload, &v, "confirmedRemoteAbsent"); err == nil {
			return d.Memory.RepairSyncProject(ctx, d.Options, d.Options.ProjectDir, v.ConfirmedRemoteAbsent)
		}
	case "memory.sync.reseed":
		if err = decode(r.Payload, &struct{}{}); err == nil {
			return d.Memory.TransitionSyncProject(ctx, d.Options, d.Options.ProjectDir, memory.SyncProjectTransitionReseedSource)
		}
	case "memory.sync.rejoin":
		if err = decode(r.Payload, &struct{}{}); err == nil {
			return d.Memory.TransitionSyncProject(ctx, d.Options, d.Options.ProjectDir, memory.SyncProjectTransitionRejoinMerge)
		}
	case "memory.session.start":
		var v sessionStart
		if err = decode(r.Payload, &v, "externalId"); err == nil {
			value, e := d.Memory.StartProviderSession(ctx, d.Options, memory.ProviderSessionStart{Project: project, Provider: "pi", ExternalID: v.ExternalID})
			if e == nil {
				d.sessions.put(value.Handle, value.LeaseToken, v.ExternalID)
			}
			return publicSession(value), e
		}
	case "memory.session.checkpoint":
		var v sessionLease
		if err = decode(r.Payload, &v, "handle"); err == nil {
			token, ok := d.sessions.token(v.Handle)
			if !ok {
				return nil, errors.New("unknown session")
			}
			value, e := d.Memory.MarkProviderSessionCheckpoint(ctx, d.Options, project, v.Handle, token)
			return publicSession(value), e
		}
	case "memory.session.renew":
		var v sessionLease
		if err = decode(r.Payload, &v, "handle"); err == nil {
			token, ok := d.sessions.token(v.Handle)
			if !ok {
				return nil, errors.New("unknown session")
			}
			value, e := d.Memory.RenewProviderSession(ctx, d.Options, project, v.Handle, token)
			return publicSession(value), e
		}
	case "memory.session.end":
		var v sessionEnd
		if err = decode(r.Payload, &v, "handle", "state", "summary"); err == nil {
			secret, ok := d.sessions.get(v.Handle)
			if !ok {
				return nil, errors.New("unknown session")
			}
			value, e := d.Memory.EndProviderSession(ctx, d.Options, memory.ProviderSessionEnd{Project: project, Handle: v.Handle, LeaseToken: secret.token, ExternalID: secret.externalID, State: v.State, Summary: v.Summary})
			if e == nil {
				d.sessions.remove(v.Handle)
			}
			return publicSession(value), e
		}
	case "memory.session.context":
		var v sessionHandle
		if err = decode(r.Payload, &v, "handle"); err == nil {
			value, e := d.Memory.ProviderSessionContext(ctx, d.Options, project, v.Handle)
			return publicContext(value), e
		}
	case "memory.session.draft_save":
		var v draftSave
		if err = decode(r.Payload, &v, "handle", "summary", "expectedUpdatedAt"); err == nil {
			return d.Memory.SaveProviderSessionDraft(ctx, d.Options, memory.ProviderSessionDraftSave{Project: project, Handle: v.Handle, Summary: v.Summary, ExpectedUpdatedAt: v.ExpectedUpdatedAt})
		}
	case "sdd.create":
		var v sdd.CreateChangeRequest
		if err = decode(r.Payload, &v, "idempotencyKey", "title", "backend", "interactionMode", "plan"); err == nil {
			v.Project = project
			return d.manager(r, func() (any, error) { return d.SDD.CreateChange(ctx, d.Options, v) })
		}
	case "sdd.list":
		var v sdd.ListChangesRequest
		if err = decode(r.Payload, &v, "status", "limit"); err == nil {
			v.Project = project
			return d.SDD.ListChanges(ctx, d.Options, v)
		}
	case "sdd.get":
		var v sdd.GetChangeRequest
		if err = decode(r.Payload, &v, "id"); err == nil {
			v.Project = project
			return d.SDD.GetChange(ctx, d.Options, v)
		}
	case "sdd.set_interaction_mode":
		var v sdd.UpdateInteractionModeRequest
		if err = decode(r.Payload, &v, "changeId", "interactionMode", "expectedStateVersion"); err == nil {
			v.Project = project
			return d.manager(r, func() (any, error) { return d.SDD.UpdateInteractionMode(ctx, d.Options, v) })
		}
	case "sdd.save_revision":
		var v sdd.SaveRevisionRequest
		if err = decode(r.Payload, &v, "changeId", "artifact", "content", "externalLocation", "digest", "inputs", "inputDigest", "expectedStateVersion"); err == nil {
			v.Project = project
			return d.manager(r, func() (any, error) { return d.SDD.SaveRevision(ctx, d.Options, v) })
		}
	case "sdd.get_revision":
		var v sdd.GetRevisionRequest
		if err = decode(r.Payload, &v, "changeId", "revisionId"); err == nil {
			v.Project = project
			return d.SDD.GetRevision(ctx, d.Options, v)
		}
	case "sdd.list_revisions":
		var v sdd.ListRevisionsRequest
		if err = decode(r.Payload, &v, "changeId", "artifact", "limit"); err == nil {
			v.Project = project
			return d.SDD.ListRevisions(ctx, d.Options, v)
		}
	case "sdd.accept_revision":
		var v sdd.AcceptRevisionRequest
		if err = decode(r.Payload, &v, "changeId", "revisionId", "expectedStateVersion"); err == nil {
			v.Project = project
			return d.manager(r, func() (any, error) { return d.SDD.AcceptRevision(ctx, d.Options, v) })
		}
	case "sdd.transition":
		var v sdd.TransitionChangeRequest
		if err = decode(r.Payload, &v, "changeId", "targetPhase", "expectedStateVersion"); err == nil {
			v.Project = project
			return d.manager(r, func() (any, error) { return d.SDD.TransitionChange(ctx, d.Options, v) })
		}
	case "sdd.cancel":
		var v cancelChange
		if err = decode(r.Payload, &v, "changeId", "expectedStateVersion"); err == nil {
			return d.manager(r, func() (any, error) {
				return d.SDD.TransitionChange(ctx, d.Options, sdd.TransitionChangeRequest{Project: project, ChangeID: v.ChangeID, Cancel: true, ExpectedStateVersion: v.ExpectedStateVersion})
			})
		}
	case "sdd.projection_status":
		var v sdd.ProjectionStatusRequest
		if err = decode(r.Payload, &v, "changeId", "artifactId"); err == nil {
			v.Project = project
			return d.SDD.ProjectionStatus(ctx, d.Options, v)
		}
	case "sdd.record_projection":
		var v sdd.RecordProjectionRequest
		if err = decode(r.Payload, &v, "changeId", "artifactId", "revisionId", "status", "digest", "location", "expectedStateVersion"); err == nil {
			v.Project = project
			return d.manager(r, func() (any, error) { return d.SDD.RecordProjection(ctx, d.Options, v) })
		}
	case "sdd.render_projection":
		var v sdd.RenderProjectionRequest
		if err = decode(r.Payload, &v, "changeId", "revisionId"); err == nil {
			v.Project = project
			return d.SDD.RenderProjection(ctx, d.Options, v)
		}
	case "sdd.compare_projection":
		var v compareProjection
		if err = decode(r.Payload, &v, "changeId", "revisionId", "relativePath", "projectionContent", "missing", "symlink"); err == nil {
			return d.SDD.CompareProjection(ctx, d.Options, sdd.CompareProjectionRequest{Project: project, ChangeID: v.ChangeID, RevisionID: v.RevisionID, Input: sdd.ProjectionInput{RelativePath: v.RelativePath, Content: []byte(v.ProjectionContent), Missing: v.Missing, Symlink: v.Symlink}})
		}
	default:
		return nil, errors.New("unsupported operation")
	}
	return nil, err
}
func (d Dispatcher) manager(r pi.Request, f func() (any, error)) (any, error) {
	if r.Role != "manager" {
		return nil, errors.New("manager authority required")
	}
	return f()
}
func decode(raw json.RawMessage, value any, allowed ...string) error {
	if len(raw) == 0 {
		return errors.New("payload required")
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return errors.New("invalid payload")
	}
	var fields map[string]json.RawMessage
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&fields); err != nil || d.Decode(&struct{}{}) != io.EOF {
		return errors.New("invalid payload")
	}
	ok := map[string]bool{}
	for _, k := range allowed {
		ok[k] = true
	}
	for k := range fields {
		if !ok[k] {
			return errors.New("invalid payload")
		}
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil || d.Decode(&struct{}{}) != io.EOF {
		return errors.New("invalid payload")
	}
	return nil
}

type syncConfigure struct {
	Endpoint string `json:"endpoint"`
	DeviceID string `json:"deviceId"`
}
type backfill struct {
	Limit int `json:"limit"`
}
type repair struct {
	ConfirmedRemoteAbsent bool `json:"confirmedRemoteAbsent"`
}
type sessionStart struct {
	ExternalID string `json:"externalId"`
}
type sessionHandle struct {
	Handle string `json:"handle"`
}
type sessionLease struct {
	Handle string `json:"handle"`
}
type sessionEnd struct {
	Handle  string                      `json:"handle"`
	State   memory.ProviderSessionState `json:"state"`
	Summary string                      `json:"summary"`
}
type draftSave struct {
	Handle            string    `json:"handle"`
	Summary           string    `json:"summary"`
	ExpectedUpdatedAt time.Time `json:"expectedUpdatedAt"`
}
type cancelChange struct {
	ChangeID             string `json:"changeId"`
	ExpectedStateVersion int64  `json:"expectedStateVersion"`
}
type compareProjection struct {
	ChangeID          string `json:"changeId"`
	RevisionID        string `json:"revisionId"`
	RelativePath      string `json:"relativePath"`
	ProjectionContent string `json:"projectionContent"`
	Missing           bool   `json:"missing"`
	Symlink           bool   `json:"symlink"`
}
type sessionResult struct {
	Handle             string                      `json:"handle"`
	State              memory.ProviderSessionState `json:"state"`
	Checkpointed       bool                        `json:"checkpointed"`
	DraftPresent       bool                        `json:"draftPresent"`
	LeaseUntil         *time.Time                  `json:"leaseUntil,omitempty"`
	CreatedAt          time.Time                   `json:"createdAt"`
	UpdatedAt          time.Time                   `json:"updatedAt"`
	CompletedAt        *time.Time                  `json:"completedAt,omitempty"`
	FinalObservationID string                      `json:"finalObservationId,omitempty"`
}

func publicSession(v memory.ProviderSession) sessionResult {
	return sessionResult{v.Handle, v.State, v.Checkpointed, v.DraftPresent, v.LeaseUntil, v.CreatedAt, v.UpdatedAt, v.CompletedAt, v.FinalObservationID}
}
func publicContext(v memory.ProviderSessionContext) map[string]any {
	return map[string]any{"session": publicSession(v.Session), "handoff": v.Handoff}
}

type sessionSecret struct{ token, externalID string }
type sessionAuthority struct {
	mu       sync.Mutex
	byHandle map[string]sessionSecret
}

func (s *sessionAuthority) put(handle, token, externalID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byHandle[handle] = sessionSecret{token, externalID}
}
func (s *sessionAuthority) token(handle string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.byHandle[handle]
	return value.token, ok
}
func (s *sessionAuthority) get(handle string) (sessionSecret, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.byHandle[handle]
	return value, ok
}
func (s *sessionAuthority) remove(handle string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byHandle, handle)
}
