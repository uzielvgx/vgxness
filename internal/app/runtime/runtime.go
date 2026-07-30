// Package runtime provides storage-backed runtimes for application entrypoints.
package runtime

import (
	"context"
	"errors"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/sdd"
)

// Memory adapts memory services to the CLI and TUI runtime contracts.
type Memory struct {
	producer string
	readOnly bool
}

// NewMemory creates a memory runtime with the supplied producer and access mode.
func NewMemory(producer string, readOnly bool) Memory {
	return Memory{producer: producer, readOnly: readOnly}
}

func (runtime Memory) Remember(ctx context.Context, opts config.Options, request memory.Remember) (memory.Entry, error) {
	return memory.NewMemoryService(storeRuntime{opts}, runtime.producerName(), nil).Remember(ctx, request)
}

func (runtime Memory) Recall(ctx context.Context, opts config.Options, request memory.Recall) ([]memory.Entry, error) {
	return memory.NewMemoryService(storeRuntime{opts}, runtime.producerName(), nil).Recall(ctx, request)
}

func (runtime Memory) Recent(ctx context.Context, opts config.Options, request memory.Recent) ([]memory.Entry, error) {
	return memory.NewMemoryService(storeRuntime{opts}, runtime.producerName(), nil).Recent(ctx, request)
}

func (runtime Memory) Get(ctx context.Context, opts config.Options, request memory.Lookup) (memory.Entry, error) {
	return memory.NewMemoryService(storeRuntime{opts}, runtime.producerName(), nil).Get(ctx, request)
}

func (runtime Memory) Forget(ctx context.Context, opts config.Options, request memory.Forget) (memory.Entry, error) {
	return withWritableStore(ctx, opts, func(store *memory.Store) (memory.Entry, error) {
		return memory.NewMemoryService(store, runtime.producerName(), nil).Forget(ctx, request)
	})
}

func (runtime Memory) ResolveProject(ctx context.Context, opts config.Options, workspace string) (string, error) {
	if runtime.readOnly {
		store, err := openStoreRead(ctx, opts)
		if err != nil {
			return "", err
		}
		defer store.Close()
		return store.ResolveProject(ctx, workspace)
	}
	return withWritableStore(ctx, opts, func(store *memory.Store) (string, error) { return store.ResolveProject(ctx, workspace) })
}

func (runtime Memory) producerName() string {
	if runtime.producer == "" {
		return "cli"
	}
	return runtime.producer
}

type storeRuntime struct{ opts config.Options }

func (runtime storeRuntime) Save(ctx context.Context, item memory.Observation) (memory.Observation, error) {
	return withWritableStore(ctx, runtime.opts, func(store *memory.Store) (memory.Observation, error) { return store.Save(ctx, item) })
}

func (runtime storeRuntime) Search(ctx context.Context, query memory.Search) ([]memory.Observation, error) {
	store, err := openStoreRead(ctx, runtime.opts)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.Search(ctx, query)
}

func (runtime storeRuntime) Recent(ctx context.Context, request memory.Recent) ([]memory.Observation, error) {
	store, err := openStoreRead(ctx, runtime.opts)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.Recent(ctx, request)
}

func (runtime storeRuntime) Get(ctx context.Context, id, project string, scope memory.Scope) (memory.Observation, error) {
	store, err := openStoreRead(ctx, runtime.opts)
	if err != nil {
		return memory.Observation{}, err
	}
	defer store.Close()
	return store.Get(ctx, id, project, scope)
}

func openStore(ctx context.Context, opts config.Options) (*memory.Store, error) {
	paths, err := config.Prepare(ctx, opts)
	if err != nil {
		return nil, err
	}
	return memory.Open(ctx, paths.Database, nil)
}

func openStoreRead(ctx context.Context, opts config.Options) (*memory.Store, error) {
	paths, err := config.PathsFor(opts)
	if err != nil {
		return nil, err
	}
	return memory.OpenRead(ctx, paths.Database)
}

func withWritableStore[T any](ctx context.Context, opts config.Options, operation func(*memory.Store) (T, error)) (T, error) {
	return withStore(func() (*memory.Store, error) { return openStore(ctx, opts) }, operation, func(store *memory.Store) error { return store.Close() })
}

func withStore[T any](open func() (*memory.Store, error), operation func(*memory.Store) (T, error), close func(*memory.Store) error) (result T, resultErr error) {
	var zero T
	store, err := open()
	if err != nil {
		return zero, err
	}
	defer func() { resultErr = errors.Join(resultErr, close(store)) }()
	return operation(store)
}

// SDD adapts SDD services to the CLI runtime contract.
type SDD struct{}

// NewSDD creates an SDD runtime.
func NewSDD() SDD {
	return SDD{}
}

func (SDD) ResolveSDDProject(ctx context.Context, opts config.Options, workspace string) (string, error) {
	return withWritableStore(ctx, opts, func(store *memory.Store) (string, error) { return store.ResolveProject(ctx, workspace) })
}

func (SDD) CreateChange(ctx context.Context, opts config.Options, request sdd.CreateChangeRequest) (sdd.Change, error) {
	return withWritableStore(ctx, opts, func(store *memory.Store) (sdd.Change, error) { return sdd.NewService(store).CreateChange(ctx, request) })
}

func (SDD) ListChanges(ctx context.Context, opts config.Options, request sdd.ListChangesRequest) ([]sdd.Change, error) {
	store, err := openStoreRead(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return sdd.NewService(store).ListChanges(ctx, request)
}

func (SDD) GetChange(ctx context.Context, opts config.Options, request sdd.GetChangeRequest) (sdd.Change, error) {
	store, err := openStoreRead(ctx, opts)
	if err != nil {
		return sdd.Change{}, err
	}
	defer store.Close()
	return sdd.NewService(store).GetChange(ctx, request)
}

func (SDD) UpdateInteractionMode(ctx context.Context, opts config.Options, request sdd.UpdateInteractionModeRequest) (sdd.Change, error) {
	return withWritableStore(ctx, opts, func(store *memory.Store) (sdd.Change, error) {
		return sdd.NewService(store).UpdateInteractionMode(ctx, request)
	})
}

func (SDD) SaveRevision(ctx context.Context, opts config.Options, request sdd.SaveRevisionRequest) (sdd.Revision, error) {
	return withWritableStore(ctx, opts, func(store *memory.Store) (sdd.Revision, error) {
		return sdd.NewService(store).SaveRevision(ctx, request)
	})
}

func (SDD) GetRevision(ctx context.Context, opts config.Options, request sdd.GetRevisionRequest) (sdd.Revision, error) {
	store, err := openStoreRead(ctx, opts)
	if err != nil {
		return sdd.Revision{}, err
	}
	defer store.Close()
	return sdd.NewService(store).GetRevision(ctx, request)
}

func (SDD) ListRevisions(ctx context.Context, opts config.Options, request sdd.ListRevisionsRequest) ([]sdd.Revision, error) {
	store, err := openStoreRead(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return sdd.NewService(store).ListRevisions(ctx, request)
}

func (SDD) AcceptRevision(ctx context.Context, opts config.Options, request sdd.AcceptRevisionRequest) (sdd.Revision, error) {
	return withWritableStore(ctx, opts, func(store *memory.Store) (sdd.Revision, error) {
		return sdd.NewService(store).AcceptRevision(ctx, request)
	})
}

func (SDD) TransitionChange(ctx context.Context, opts config.Options, request sdd.TransitionChangeRequest) (sdd.Change, error) {
	return withWritableStore(ctx, opts, func(store *memory.Store) (sdd.Change, error) {
		return sdd.NewService(store).TransitionChange(ctx, request)
	})
}

func (SDD) ProjectionStatus(ctx context.Context, opts config.Options, request sdd.ProjectionStatusRequest) (sdd.Projection, error) {
	store, err := openStoreRead(ctx, opts)
	if err != nil {
		return sdd.Projection{}, err
	}
	defer store.Close()
	return sdd.NewService(store).ProjectionStatus(ctx, request)
}

func (SDD) RecordProjection(ctx context.Context, opts config.Options, request sdd.RecordProjectionRequest) (sdd.Projection, error) {
	return withWritableStore(ctx, opts, func(store *memory.Store) (sdd.Projection, error) {
		return sdd.NewService(store).RecordProjection(ctx, request)
	})
}

func (SDD) RenderProjection(ctx context.Context, opts config.Options, request sdd.RenderProjectionRequest) (sdd.ProjectionDocument, error) {
	store, err := openStoreRead(ctx, opts)
	if err != nil {
		return sdd.ProjectionDocument{}, err
	}
	defer store.Close()
	return sdd.NewService(store).RenderProjection(ctx, request)
}

func (SDD) CompareProjection(ctx context.Context, opts config.Options, request sdd.CompareProjectionRequest) (sdd.ProjectionComparison, error) {
	store, err := openStoreRead(ctx, opts)
	if err != nil {
		return sdd.ProjectionComparison{}, err
	}
	defer store.Close()
	return sdd.NewService(store).CompareProjection(ctx, request)
}
