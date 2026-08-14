// Package runtime provides storage-backed runtimes for application entrypoints.
package runtime

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/hooks"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/sdd"
	"github.com/vgxness/vgxness/internal/secrets"
	"github.com/vgxness/vgxness/internal/syncclient"
	"github.com/vgxness/vgxness/internal/syncservice"
)

const (
	foregroundSyncBatches = 16
	foregroundSyncLease   = time.Minute
	foregroundSyncTimeout = 15 * time.Second
)

var canonicalBearer = regexp.MustCompile(`^vgx1\.([0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})\.([A-Za-z0-9_-]{1,128})$`)

// Memory adapts memory services to the CLI and TUI runtime contracts.
type Memory struct {
	producer   string
	readOnly   bool
	transport  http.RoundTripper
	credential func(string) (string, error)
	hooks      hooks.Emitter
}

// NewMemory creates a memory runtime with the supplied producer and access mode.
func NewMemory(producer string, readOnly bool) Memory {
	return NewMemoryWithHooks(producer, readOnly, nil)
}

// NewMemoryWithHooks adds best-effort lifecycle observation to a memory runtime.
func NewMemoryWithHooks(producer string, readOnly bool, emitter hooks.Emitter) Memory {
	return Memory{producer: producer, readOnly: readOnly, transport: http.DefaultTransport, credential: secrets.System().Get, hooks: emitter}
}

func (runtime Memory) Remember(ctx context.Context, opts config.Options, request memory.Remember) (memory.Entry, error) {
	entry, err := memory.NewMemoryService(storeRuntime{opts}, runtime.producerName(), nil).Remember(ctx, request)
	if err == nil {
		runtime.emitMemory(ctx, false, entry)
	}
	return entry, err
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
	entry, err := withWritableStore(ctx, opts, func(store *memory.Store) (memory.Entry, error) {
		return memory.NewMemoryService(store, runtime.producerName(), nil).Forget(ctx, request)
	})
	if err == nil {
		runtime.emitMemory(ctx, true, entry)
	}
	return entry, err
}

func (runtime Memory) emitMemory(ctx context.Context, forgotten bool, entry memory.Entry) {
	if runtime.hooks == nil {
		return
	}
	defer func() { recover() }()
	var draft hooks.Draft
	var err error
	if forgotten {
		draft, err = hooks.NewMemoryForgotten(entry.Project, entry.ID, string(entry.Scope), entry.Type, string(entry.State), entry.CreatedAt, entry.UpdatedAt)
	} else {
		draft, err = hooks.NewMemorySaved(entry.Project, entry.ID, string(entry.Scope), entry.Type, string(entry.State), entry.CreatedAt, entry.UpdatedAt)
	}
	if err == nil {
		runtime.hooks.Emit(ctx, draft)
	}
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

// Sync performs a bounded, foreground synchronization without exposing credentials.
func (runtime Memory) Sync(ctx context.Context, opts config.Options) (memory.SyncResult, error) {
	result := memory.SyncResult{Status: memory.SyncStatusUnavailable}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if opts.ProjectLocal || runtime.readOnly {
		return result, nil
	}
	store, err := openStore(ctx, opts)
	if err != nil {
		return result, err
	}
	defer store.Close()
	profile, found, err := store.GetSyncProfile(ctx)
	if err != nil {
		return result, err
	}
	if !found {
		return memory.SyncResult{Status: memory.SyncStatusAbsent}, nil
	}
	if !profile.Enabled {
		return memory.SyncResult{Status: memory.SyncStatusDisabled}, nil
	}
	getCredential := runtime.credential
	if getCredential == nil {
		getCredential = secrets.System().Get
	}
	credential, err := getCredential(profile.CredentialRef)
	if err != nil {
		if errors.Is(err, secrets.ErrMissing) {
			return memory.SyncResult{Status: memory.SyncStatusCredentialMissing}, nil
		}
		if errors.Is(err, secrets.ErrUnavailable) {
			return memory.SyncResult{Status: memory.SyncStatusCredentialUnavailable}, nil
		}
		return result, nil
	}
	if !validBearer(credential, profile.DeviceID) {
		return memory.SyncResult{Status: memory.SyncStatusInvalid}, nil
	}
	transport := runtime.transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client, err := syncclient.New(profile.Endpoint, transport)
	if err != nil {
		return memory.SyncResult{Status: memory.SyncStatusInvalid}, nil
	}
	remote := syncRemote{client: client, credential: credential}
	return runForegroundSync(ctx, store, remote)
}

func validBearer(value, deviceID string) bool {
	match := canonicalBearer.FindStringSubmatch(value)
	return match != nil && match[1] == strings.ToLower(deviceID)
}

type syncRemote struct {
	client     *syncclient.Client
	credential string
}

func (remote syncRemote) Discover(ctx context.Context) (syncservice.Discovery, error) {
	return remote.client.Discover(ctx, remote.credential)
}

func (remote syncRemote) Pull(ctx context.Context, cursor syncservice.Cursor, limit int) (syncservice.PullPage, error) {
	page, err := remote.client.Pull(ctx, remote.credential, cursor, limit)
	return syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: page.HistoryID, Position: page.Position, Watermark: page.Watermark}, HasMore: page.HasMore, Changes: page.Changes}, err
}

func (remote syncRemote) Push(ctx context.Context, mutations []syncservice.Mutation) ([]syncservice.Result, error) {
	return remote.client.Push(ctx, remote.credential, mutations)
}

func (remote syncRemote) Capabilities(ctx context.Context) error {
	_, err := remote.client.Capabilities(ctx, remote.credential)
	return err
}

type foregroundRemote interface {
	memory.BootstrapRemote
	Capabilities(context.Context) error
	Push(context.Context, []syncservice.Mutation) ([]syncservice.Result, error)
}

type foregroundStore interface {
	ClaimDueSyncOutbox(context.Context, time.Duration, int) ([]memory.SyncOutboxClaim, error)
	ApplySyncPushResult(context.Context, string, string, syncservice.Result) error
	MarkSyncOutboxRetry(context.Context, string, string, time.Time, string) error
	BootstrapSync(context.Context, memory.BootstrapRemote) error
	BootstrapOwnConflict(context.Context, memory.BootstrapRemote, string) error
	PullConflictResolutions(context.Context, memory.BootstrapRemote) error
	PendingOwnConflictReceipts(context.Context) ([]string, error)
	SyncQueueSummary(context.Context) (memory.SyncQueueSummary, error)
}

func runForegroundSync(ctx context.Context, store foregroundStore, remote foregroundRemote) (memory.SyncResult, error) {
	result := memory.SyncResult{Status: memory.SyncStatusSynced}
	capabilityCtx, cancel := context.WithTimeout(ctx, foregroundSyncTimeout)
	err := remote.Capabilities(capabilityCtx)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			result.Status = memory.SyncStatusPartial
			return result, ctx.Err()
		}
		result.Status = syncStatusForError(err)
		return result, nil
	}
	ids, err := store.PendingOwnConflictReceipts(ctx)
	if err != nil {
		return result, err
	}
	for _, id := range ids {
		conflictCtx, cancel := context.WithTimeout(ctx, foregroundSyncTimeout)
		err = store.BootstrapOwnConflict(conflictCtx, remote, id)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				result.Status = memory.SyncStatusPartial
				return result, ctx.Err()
			}
			result.Status = memory.SyncStatusPartial
			return result, nil
		}
		result.Conflicts++
	}
	if len(ids) != 0 {
		result.Status = memory.SyncStatusConflict
		return result, nil
	}
	for batch := 0; batch < foregroundSyncBatches; batch++ {
		if err := ctx.Err(); err != nil {
			result.Status = memory.SyncStatusPartial
			return result, err
		}
		claims, err := store.ClaimDueSyncOutbox(ctx, foregroundSyncLease, 16)
		if err != nil {
			result.Status = memory.SyncStatusPartial
			return result, err
		}
		if len(claims) == 0 {
			if queue, queueErr := store.SyncQueueSummary(ctx); queueErr != nil {
				return result, queueErr
			} else if queue.Conflict {
				resolutionCtx, cancel := context.WithTimeout(ctx, foregroundSyncTimeout)
				err = store.PullConflictResolutions(resolutionCtx, remote)
				cancel()
				if err != nil {
					if ctx.Err() != nil {
						result.Status = memory.SyncStatusPartial
						return result, ctx.Err()
					}
					if errors.Is(err, memory.ErrConflict) {
						result.Status = memory.SyncStatusConflict
					} else {
						result.Status = memory.SyncStatusPartial
					}
					return result, nil
				}
				continue
			} else if queue.Work {
				result.Status = memory.SyncStatusPartial
				return result, nil
			}
			bootstrapCtx, cancel := context.WithTimeout(ctx, foregroundSyncTimeout)
			err = store.BootstrapSync(bootstrapCtx, remote)
			cancel()
			if err != nil {
				if ctx.Err() != nil {
					result.Status = memory.SyncStatusPartial
					return result, ctx.Err()
				}
				result.Status = syncStatusForError(err)
				return result, nil
			}
			return result, nil
		}
		result.Batches++
		mutations := make([]syncservice.Mutation, len(claims))
		for index := range claims {
			mutations[index] = claims[index].Mutation
		}
		pushCtx, cancel := context.WithTimeout(ctx, foregroundSyncTimeout)
		results, pushErr := remote.Push(pushCtx, mutations)
		cancel()
		if pushErr != nil {
			if ctx.Err() != nil {
				result.Status = memory.SyncStatusPartial
				return result, ctx.Err()
			}
			if errors.Is(pushErr, syncclient.ErrUnauthorized) {
				result.Status = memory.SyncStatusUnauthorized
				return result, nil
			}
			if markClaimsRetry(ctx, store, claims, &result) {
				result.Status = memory.SyncStatusUnreachable
			} else {
				result.Status = memory.SyncStatusPartial
			}
			return result, nil
		}
		if len(results) != len(claims) {
			markClaimsRetry(ctx, store, claims, &result)
			result.Status = memory.SyncStatusPartial
			return result, nil
		}
		blocking := false
		for index, pushResult := range results {
			claim := claims[index]
			if pushResult.MutationID != claim.Mutation.MutationID {
				markClaimsRetry(ctx, store, claims[index:], &result)
				result.Status = memory.SyncStatusPartial
				return result, nil
			}
			if err := store.ApplySyncPushResult(ctx, claim.Mutation.MutationID, claim.ClaimToken, pushResult); err != nil {
				if ctx.Err() != nil {
					result.Status = memory.SyncStatusPartial
					return result, ctx.Err()
				}
				markClaimsRetry(ctx, store, claims[index:], &result)
				result.Status = memory.SyncStatusPartial
				return result, nil
			}
			switch pushResult.Disposition {
			case syncservice.DispositionAccepted:
				result.Pushed++
			case syncservice.DispositionPreviouslyAccepted:
				result.Pushed++
				result.PreviouslyAccepted++
			case syncservice.DispositionRejected:
				if pushResult.Retryable {
					result.Retried++
					result.Status = memory.SyncStatusPartial
				} else {
					result.Rejected++
					result.Status = memory.SyncStatusRejected
				}
				blocking = true
			case syncservice.DispositionConflict:
				result.Conflicts++
				result.Status = memory.SyncStatusConflict
				conflictCtx, cancel := context.WithTimeout(ctx, foregroundSyncTimeout)
				err := store.BootstrapOwnConflict(conflictCtx, remote, claim.Mutation.MutationID)
				cancel()
				if err != nil {
					if ctx.Err() != nil {
						result.Status = memory.SyncStatusPartial
						return result, ctx.Err()
					}
					result.Status = memory.SyncStatusPartial
				}
				blocking = true
			}
		}
		if blocking {
			return result, nil
		}
	}
	queue, err := store.SyncQueueSummary(ctx)
	if err != nil {
		return result, err
	}
	if queue.Conflict {
		result.Status = memory.SyncStatusConflict
		return result, nil
	}
	if queue.Work {
		result.Status = memory.SyncStatusPartial
		return result, nil
	}
	bootstrapCtx, cancel := context.WithTimeout(ctx, foregroundSyncTimeout)
	err = store.BootstrapSync(bootstrapCtx, remote)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			result.Status = memory.SyncStatusPartial
			return result, ctx.Err()
		}
		result.Status = syncStatusForError(err)
	}
	return result, nil
}

func markClaimsRetry(ctx context.Context, store foregroundStore, claims []memory.SyncOutboxClaim, result *memory.SyncResult) bool {
	allMarked := true
	for _, claim := range claims {
		if err := store.MarkSyncOutboxRetry(ctx, claim.Mutation.MutationID, claim.ClaimToken, time.Now().UTC().Add(time.Second), "transport"); err == nil {
			result.Retried++
		} else {
			allMarked = false
		}
	}
	return allMarked
}

func syncStatusForError(err error) memory.SyncStatus {
	if errors.Is(err, syncclient.ErrUnauthorized) {
		return memory.SyncStatusUnauthorized
	}
	if errors.Is(err, syncclient.ErrUnavailable) {
		return memory.SyncStatusUnreachable
	}
	if errors.Is(err, syncclient.ErrRemote) || errors.Is(err, syncclient.ErrDiscoveryUnsupported) {
		return memory.SyncStatusIncompatible
	}
	if errors.Is(err, memory.ErrConflict) {
		return memory.SyncStatusConflict
	}
	return memory.SyncStatusUnavailable
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
type SDD struct{ hooks hooks.Emitter }

// NewSDD creates an SDD runtime.
func NewSDD() SDD {
	return NewSDDWithHooks(nil)
}

// NewSDDWithHooks adds best-effort lifecycle observation to an SDD runtime.
func NewSDDWithHooks(emitter hooks.Emitter) SDD { return SDD{hooks: emitter} }

func (SDD) ResolveSDDProject(ctx context.Context, opts config.Options, workspace string) (string, error) {
	return withWritableStore(ctx, opts, func(store *memory.Store) (string, error) { return store.ResolveProject(ctx, workspace) })
}

func (runtime SDD) CreateChange(ctx context.Context, opts config.Options, request sdd.CreateChangeRequest) (sdd.Change, error) {
	result, err := withWritableStore(ctx, opts, func(store *memory.Store) (sdd.Change, error) { return sdd.NewService(store).CreateChange(ctx, request) })
	if err == nil {
		draft, draftErr := hooks.NewChangeCreated(result.Project, result.ID, string(result.Phase), string(result.Status), result.StateVersion)
		runtime.emitDraft(ctx, draft, draftErr)
	}
	return result, err
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

func (runtime SDD) AcceptRevision(ctx context.Context, opts config.Options, request sdd.AcceptRevisionRequest) (sdd.Revision, error) {
	result, err := withWritableStore(ctx, opts, func(store *memory.Store) (sdd.Revision, error) {
		return sdd.NewService(store).AcceptRevision(ctx, request)
	})
	if err == nil {
		draft, draftErr := hooks.NewRevisionAccepted(result.Project, result.ChangeID, result.ArtifactID, result.ID, string(result.Artifact), string(result.Status), string(result.Digest), string(result.InputDigest), result.StateVersion)
		runtime.emitDraft(ctx, draft, draftErr)
	}
	return result, err
}

func (runtime SDD) TransitionChange(ctx context.Context, opts config.Options, request sdd.TransitionChangeRequest) (sdd.Change, error) {
	result, err := withWritableStore(ctx, opts, func(store *memory.Store) (sdd.Change, error) {
		return sdd.NewService(store).TransitionChange(ctx, request)
	})
	if err == nil {
		draft, draftErr := hooks.NewChangeTransitioned(result.Project, result.ID, string(result.Phase), string(result.Status), result.StateVersion)
		runtime.emitDraft(ctx, draft, draftErr)
	}
	return result, err
}

func (SDD) ProjectionStatus(ctx context.Context, opts config.Options, request sdd.ProjectionStatusRequest) (sdd.Projection, error) {
	store, err := openStoreRead(ctx, opts)
	if err != nil {
		return sdd.Projection{}, err
	}
	defer store.Close()
	return sdd.NewService(store).ProjectionStatus(ctx, request)
}

func (runtime SDD) RecordProjection(ctx context.Context, opts config.Options, request sdd.RecordProjectionRequest) (sdd.Projection, error) {
	result, err := withWritableStore(ctx, opts, func(store *memory.Store) (sdd.Projection, error) {
		return sdd.NewService(store).RecordProjection(ctx, request)
	})
	if err == nil {
		draft, draftErr := hooks.NewProjectionRecorded(result.Project, result.ChangeID, result.ArtifactID, result.RevisionID, string(result.Status), string(result.Digest), result.StateVersion)
		runtime.emitDraft(ctx, draft, draftErr)
	}
	return result, err
}

func (runtime SDD) emitDraft(ctx context.Context, draft hooks.Draft, err error) {
	if runtime.hooks == nil || err != nil {
		return
	}
	defer func() { recover() }()
	runtime.hooks.Emit(ctx, draft)
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
