package sdd

import "context"

type Repository interface {
	CreateChange(context.Context, CreateChangeRequest) (Change, error)
	ListChanges(context.Context, ListChangesRequest) ([]Change, error)
	GetChange(context.Context, GetChangeRequest) (Change, error)
	UpdateInteractionMode(context.Context, UpdateInteractionModeRequest) (Change, error)
	SaveRevision(context.Context, SaveRevisionRequest) (Revision, error)
	GetRevision(context.Context, GetRevisionRequest) (Revision, error)
	ListRevisions(context.Context, ListRevisionsRequest) ([]Revision, error)
	AcceptRevision(context.Context, AcceptRevisionRequest) (Revision, error)
	TransitionChange(context.Context, TransitionChangeRequest) (Change, error)
	ProjectionStatus(context.Context, ProjectionStatusRequest) (Projection, error)
	RecordProjection(context.Context, RecordProjectionRequest) (Projection, error)
}

type Service struct{ repository Repository }

func NewService(repository Repository) Service { return Service{repository: repository} }

func (service Service) CreateChange(ctx context.Context, request CreateChangeRequest) (Change, error) {
	if err := check(ctx, request.Validate(), service.repository); err != nil {
		return Change{}, err
	}
	return service.repository.CreateChange(ctx, request)
}

func (service Service) ListChanges(ctx context.Context, request ListChangesRequest) ([]Change, error) {
	if err := check(ctx, request.Validate(), service.repository); err != nil {
		return nil, err
	}
	return service.repository.ListChanges(ctx, request)
}

func (service Service) GetChange(ctx context.Context, request GetChangeRequest) (Change, error) {
	if err := check(ctx, request.Validate(), service.repository); err != nil {
		return Change{}, err
	}
	return service.repository.GetChange(ctx, request)
}

func (service Service) UpdateInteractionMode(ctx context.Context, request UpdateInteractionModeRequest) (Change, error) {
	if err := check(ctx, request.Validate(), service.repository); err != nil {
		return Change{}, err
	}
	return service.repository.UpdateInteractionMode(ctx, request)
}

func (service Service) SaveRevision(ctx context.Context, request SaveRevisionRequest) (Revision, error) {
	if err := check(ctx, request.Validate(), service.repository); err != nil {
		return Revision{}, err
	}
	return service.repository.SaveRevision(ctx, request)
}

func (service Service) GetRevision(ctx context.Context, request GetRevisionRequest) (Revision, error) {
	if err := check(ctx, request.Validate(), service.repository); err != nil {
		return Revision{}, err
	}
	return service.repository.GetRevision(ctx, request)
}

func (service Service) ListRevisions(ctx context.Context, request ListRevisionsRequest) ([]Revision, error) {
	if err := check(ctx, request.Validate(), service.repository); err != nil {
		return nil, err
	}
	return service.repository.ListRevisions(ctx, request)
}

func (service Service) AcceptRevision(ctx context.Context, request AcceptRevisionRequest) (Revision, error) {
	if err := check(ctx, request.Validate(), service.repository); err != nil {
		return Revision{}, err
	}
	return service.repository.AcceptRevision(ctx, request)
}

func (service Service) TransitionChange(ctx context.Context, request TransitionChangeRequest) (Change, error) {
	if err := check(ctx, request.Validate(), service.repository); err != nil {
		return Change{}, err
	}
	return service.repository.TransitionChange(ctx, request)
}

func (service Service) ProjectionStatus(ctx context.Context, request ProjectionStatusRequest) (Projection, error) {
	if err := check(ctx, request.Validate(), service.repository); err != nil {
		return Projection{}, err
	}
	return service.repository.ProjectionStatus(ctx, request)
}

func (service Service) RecordProjection(ctx context.Context, request RecordProjectionRequest) (Projection, error) {
	if err := check(ctx, request.Validate(), service.repository); err != nil {
		return Projection{}, err
	}
	return service.repository.RecordProjection(ctx, request)
}

func check(ctx context.Context, validation error, repository Repository) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if validation != nil || repository == nil {
		return ErrInvalid
	}
	return nil
}
