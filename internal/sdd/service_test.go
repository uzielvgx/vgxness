package sdd

import (
	"context"
	"errors"
	"testing"
)

type serviceRepository struct {
	created     CreateChangeRequest
	modeUpdated UpdateInteractionModeRequest
	change      Change
	revision    Revision
	err         error
}

func (r *serviceRepository) CreateChange(_ context.Context, request CreateChangeRequest) (Change, error) {
	r.created = request
	return r.change, r.err
}
func (*serviceRepository) ListChanges(context.Context, ListChangesRequest) ([]Change, error) {
	return nil, nil
}
func (r *serviceRepository) GetChange(context.Context, GetChangeRequest) (Change, error) {
	return r.change, r.err
}
func (r *serviceRepository) UpdateInteractionMode(_ context.Context, request UpdateInteractionModeRequest) (Change, error) {
	r.modeUpdated = request
	return r.change, r.err
}
func (*serviceRepository) SaveRevision(context.Context, SaveRevisionRequest) (Revision, error) {
	return Revision{}, nil
}
func (r *serviceRepository) GetRevision(context.Context, GetRevisionRequest) (Revision, error) {
	return r.revision, r.err
}
func (*serviceRepository) ListRevisions(context.Context, ListRevisionsRequest) ([]Revision, error) {
	return nil, nil
}
func (*serviceRepository) AcceptRevision(context.Context, AcceptRevisionRequest) (Revision, error) {
	return Revision{}, nil
}
func (*serviceRepository) TransitionChange(context.Context, TransitionChangeRequest) (Change, error) {
	return Change{}, nil
}
func (*serviceRepository) ProjectionStatus(context.Context, ProjectionStatusRequest) (Projection, error) {
	return Projection{}, nil
}
func (*serviceRepository) RecordProjection(context.Context, RecordProjectionRequest) (Projection, error) {
	return Projection{}, nil
}

func TestServiceValidatesBeforeRepository(t *testing.T) {
	repository := &serviceRepository{}
	service := NewService(repository)
	_, err := service.CreateChange(context.Background(), CreateChangeRequest{})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want invalid", err)
	}
	if repository.created.Project != "" {
		t.Fatal("repository called for invalid request")
	}
}

func TestServiceDelegatesValidChange(t *testing.T) {
	repository := &serviceRepository{change: Change{ID: "change-1"}}
	service := NewService(repository)
	request := CreateChangeRequest{Project: "project", IdempotencyKey: "create-change-1", Title: "Change", Backend: BackendMemory, InteractionMode: InteractionAutomatic, Plan: PlanMedium}
	got, err := service.CreateChange(context.Background(), request)
	if err != nil || got.ID != "change-1" || repository.created != request {
		t.Fatalf("got=%+v request=%+v err=%v", got, repository.created, err)
	}
}

func TestServiceCancellation(t *testing.T) {
	service := NewService(&serviceRepository{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.ListChanges(ctx, ListChangesRequest{Project: "project"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}
}

func TestServiceDelegatesInteractionModeUpdate(t *testing.T) {
	repository := &serviceRepository{change: Change{ID: "change-1", InteractionMode: InteractionInteractive}}
	service := NewService(repository)
	request := UpdateInteractionModeRequest{Project: "project", ChangeID: "change-1", InteractionMode: InteractionInteractive, ExpectedStateVersion: 2}
	got, err := service.UpdateInteractionMode(context.Background(), request)
	if err != nil || got.InteractionMode != InteractionInteractive || repository.modeUpdated != request {
		t.Fatalf("got=%+v request=%+v err=%v", got, repository.modeUpdated, err)
	}
}

func TestServiceRendersAndComparesProjectionWithoutStorageWrites(t *testing.T) {
	revision := acceptedProjectionRevision(PhaseProposal, "proposal")
	revision.Project = "project"
	repository := &serviceRepository{change: Change{ID: revision.ChangeID, Project: "project", Backend: BackendHybrid}, revision: revision}
	service := NewService(repository)
	rendered, err := service.RenderProjection(context.Background(), RenderProjectionRequest{Project: "project", ChangeID: revision.ChangeID, RevisionID: revision.ID})
	if err != nil || rendered.RelativePath != "openspec/changes/change-1/proposal.md" {
		t.Fatalf("rendered=%+v err=%v", rendered, err)
	}
	compared, err := service.CompareProjection(context.Background(), CompareProjectionRequest{Project: "project", ChangeID: revision.ChangeID, RevisionID: revision.ID, Input: ProjectionInput{RelativePath: rendered.RelativePath, Content: rendered.Content}})
	if err != nil || compared.State != DriftSynced {
		t.Fatalf("compared=%+v err=%v", compared, err)
	}
}
