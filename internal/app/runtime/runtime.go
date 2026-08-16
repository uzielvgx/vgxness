// Package runtime provides storage-backed runtimes for application entrypoints.
package runtime

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/hooks"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/sdd"
	"github.com/vgxness/vgxness/internal/secrets"
	"github.com/vgxness/vgxness/internal/syncapi"
	"github.com/vgxness/vgxness/internal/syncclient"
	"github.com/vgxness/vgxness/internal/syncservice"
)

const (
	foregroundSyncBatches = 16
	foregroundSyncLease   = time.Minute
	foregroundSyncTimeout = 15 * time.Second
)

var canonicalBearer = regexp.MustCompile(`^vgx1\.([0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})\.([A-Za-z0-9_-]{1,128})$`)

const credentialFileReference = "secret://keychain/sync/file"

// Memory adapts memory services to the CLI and TUI runtime contracts.
type Memory struct {
	producer               string
	readOnly               bool
	transport              http.RoundTripper
	credential             func(string) (string, error)
	putSecret              func(string, string) error
	deleteSecret           func(string) error
	configureProfile       func(context.Context, *memory.Store, memory.SyncProfile) (memory.SyncProfile, error)
	closeStore             func(*memory.Store) error
	afterSyncCredentialPut func() error
	afterSyncProfileCommit func() error
	hooks                  hooks.Emitter
}

type syncEnrollmentFailure struct{ cause error }

func (failure syncEnrollmentFailure) Error() string {
	return "sync profile was not activated; credential compensation failed"
}

func (failure syncEnrollmentFailure) Unwrap() error { return failure.cause }

// NewMemory creates a memory runtime with the supplied producer and access mode.
func NewMemory(producer string, readOnly bool) Memory {
	return NewMemoryWithHooks(producer, readOnly, nil)
}

// NewMemoryWithHooks adds best-effort lifecycle observation to a memory runtime.
func NewMemoryWithHooks(producer string, readOnly bool, emitter hooks.Emitter) Memory {
	store := secrets.System()
	return Memory{producer: producer, readOnly: readOnly, transport: http.DefaultTransport, credential: store.Get, putSecret: store.Put, deleteSecret: store.Delete, hooks: emitter}
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

// InitializeProject publishes a portable marker and binds it to the existing
// local project. It intentionally never rekeys local data or sync artifacts.
func (runtime Memory) InitializeProject(ctx context.Context, opts config.Options, workspace string) (string, error) {
	if runtime.readOnly {
		return "", memory.ErrInvalid
	}
	workspace, err := canonicalInvocationWorkspace(workspace)
	if err != nil {
		return "", memory.ErrInvalid
	}
	return withWritableStore(ctx, opts, func(store *memory.Store) (string, error) {
		if _, err := store.ResolveProject(ctx, workspace); err != nil {
			return "", err
		}
		known, found, err := store.PortableProjectID(ctx, workspace)
		if err != nil {
			return "", err
		}
		var id string
		if found {
			id, _, err = memory.EnsureProjectID(workspace, known)
		} else {
			id, _, err = memory.InitializeProjectID(workspace)
		}
		if err != nil {
			return "", err
		}
		if err := store.BindPortableProjectID(ctx, workspace, id); err != nil {
			return "", err
		}
		return id, nil
	})
}

// Sync performs a bounded, foreground synchronization without exposing credentials.
func (runtime Memory) Sync(ctx context.Context, opts config.Options) (memory.SyncResult, error) {
	result, err := runtime.sync(ctx, opts)
	if result.Mode == "" {
		result.Mode = memory.SyncModeProjectPushOnly
	}
	if err == nil {
		runtime.emitMemorySync(ctx, opts.ProjectDir, result)
	}
	return result, err
}

func (runtime Memory) emitMemorySync(ctx context.Context, canonicalWorkspace string, result memory.SyncResult) {
	if runtime.hooks == nil {
		return
	}
	canonicalWorkspace, err := canonicalInvocationWorkspace(canonicalWorkspace)
	if err != nil {
		return
	}
	projectID, err := memory.StableProjectID(canonicalWorkspace)
	if err != nil {
		return
	}
	defer func() { recover() }()
	draft, err := hooks.NewMemorySyncCompleted(projectID, string(result.Status), int64(result.Pushed), int64(result.PreviouslyAccepted), int64(result.Rejected), int64(result.Retried), int64(result.Conflicts), int64(result.Batches))
	if err == nil {
		runtime.hooks.Emit(ctx, draft)
	}
}

func canonicalInvocationWorkspace(workspace string) (string, error) {
	var err error
	if workspace == "" {
		workspace, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	workspace, err = filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	workspace, err = filepath.EvalSymlinks(filepath.Clean(workspace))
	if err != nil {
		return "", err
	}
	return config.CanonicalizeExistingPathCase(workspace), nil
}

func (runtime Memory) sync(ctx context.Context, opts config.Options) (memory.SyncResult, error) {
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
	credential, err := runtime.syncCredential(opts, profile.CredentialRef)
	if err != nil {
		if errors.Is(err, secrets.ErrUnsupported) {
			return result, err
		}
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
	workspace, err := canonicalInvocationWorkspace(opts.ProjectDir)
	if err != nil {
		return memory.SyncResult{Status: memory.SyncStatusInvalid}, nil
	}
	portableID, present, err := memory.ReadProjectID(workspace)
	if err != nil || !present {
		return memory.SyncResult{Status: memory.SyncStatusUnavailable}, nil
	}
	projectID, bound, err := store.BoundPortableProject(ctx, workspace, portableID)
	if err != nil || !bound {
		return memory.SyncResult{Status: memory.SyncStatusUnavailable}, nil
	}
	resolved, err := store.ResolveProject(ctx, workspace)
	if err != nil || resolved != projectID {
		return memory.SyncResult{Status: memory.SyncStatusUnavailable}, nil
	}
	remote := syncRemote{client: client, credential: credential}
	return runForegroundProjectSync(ctx, store, remote, projectID, portableID)
}

func validBearer(value, deviceID string) bool {
	match := canonicalBearer.FindStringSubmatch(value)
	return match != nil && match[1] == strings.ToLower(deviceID)
}

func (runtime Memory) syncCredential(opts config.Options, reference string) (string, error) {
	if opts.CredentialFile != "" && reference == credentialFileReference {
		return secrets.ReadCredentialFile(opts.CredentialFile)
	}
	if opts.CredentialFile != "" || reference == credentialFileReference {
		return "", secrets.ErrMissing
	}
	get := runtime.credential
	if get == nil {
		get = secrets.System().Get
	}
	return get(reference)
}

// ConfigureSync stores a bearer in the keyring before activating its profile.
// It validates entirely locally and never contacts the remote endpoint.
func (runtime Memory) ConfigureSync(ctx context.Context, opts config.Options, endpoint, deviceID, bearer string) (memory.SyncConfigurationStatus, error) {
	if runtime.readOnly || opts.ProjectLocal {
		return memory.SyncConfigurationStatus{}, memory.ErrInvalid
	}
	if opts.CredentialFile != "" {
		return runtime.configureSyncFile(ctx, opts, endpoint, deviceID)
	}
	profile, err := memory.ValidateSyncProfile(memory.SyncProfile{Enabled: true, Endpoint: endpoint, DeviceID: deviceID, CredentialRef: "secret://keychain/sync/pending"})
	if err != nil || !validBearer(bearer, profile.DeviceID) {
		return memory.SyncConfigurationStatus{}, memory.ErrInvalid
	}
	paths, err := config.Prepare(ctx, opts)
	if err != nil {
		return memory.SyncConfigurationStatus{}, err
	}
	release, err := acquireSyncEnrollmentLock(ctx, paths.Database)
	if err != nil {
		return memory.SyncConfigurationStatus{}, err
	}
	defer release()
	store, err := memory.Open(ctx, paths.Database, nil)
	if err != nil {
		return memory.SyncConfigurationStatus{}, err
	}
	close := runtime.closeStore
	if close == nil {
		close = func(store *memory.Store) error { return store.Close() }
	}
	previous, found, err := store.GetSyncProfile(ctx)
	if err != nil {
		return memory.SyncConfigurationStatus{}, errors.Join(err, close(store))
	}
	firstRef, secondRef := syncEnrollmentCredentialRefs(paths.Database)
	put := runtime.putSecret
	if put == nil {
		put = secrets.System().Put
	}
	remove := runtime.deleteSecret
	if remove == nil {
		remove = secrets.System().Delete
	}
	configure := runtime.configureProfile
	if configure == nil {
		configure = func(ctx context.Context, store *memory.Store, profile memory.SyncProfile) (memory.SyncProfile, error) {
			return store.ConfigureSyncProfile(ctx, profile)
		}
	}
	if found && previous.PreviousCredentialRef != "" {
		if !validSyncRecoveryMarker(previous.CredentialRef, previous.PreviousCredentialRef, firstRef, secondRef) {
			return memory.SyncConfigurationStatus{}, errors.Join(memory.ErrCorrupt, close(store))
		}
		if err := reconcileSyncEnrollment(ctx, store, previous, remove, configure); err != nil {
			return memory.SyncConfigurationStatus{}, errors.Join(err, close(store))
		}
		previous, found, err = store.GetSyncProfile(ctx)
		if err != nil {
			return memory.SyncConfigurationStatus{}, errors.Join(err, close(store))
		}
	}
	inactive := firstRef
	if found && previous.CredentialRef == firstRef {
		inactive = secondRef
	}
	if err := deleteSyncCredential(remove, inactive); err != nil {
		return memory.SyncConfigurationStatus{}, errors.Join(err, close(store))
	}
	profile.CredentialRef = inactive
	priorWasSlot := found && (previous.CredentialRef == firstRef || previous.CredentialRef == secondRef)
	if priorWasSlot {
		profile.PreviousCredentialRef = previous.CredentialRef
	}
	if err := put(profile.CredentialRef, bearer); err != nil {
		return memory.SyncConfigurationStatus{}, errors.Join(err, close(store))
	}
	if runtime.afterSyncCredentialPut != nil {
		if err := runtime.afterSyncCredentialPut(); err != nil {
			return memory.SyncConfigurationStatus{}, errors.Join(err, close(store))
		}
	}
	configured, mutationErr := configure(ctx, store, profile)
	if mutationErr != nil {
		compensationErr := deleteSyncCredential(remove, profile.CredentialRef)
		closeErr := close(store)
		if compensationErr != nil {
			return memory.SyncConfigurationStatus{}, syncEnrollmentFailure{cause: errors.Join(mutationErr, compensationErr, closeErr)}
		}
		return memory.SyncConfigurationStatus{}, errors.Join(mutationErr, closeErr)
	}
	if runtime.afterSyncProfileCommit != nil {
		if err := runtime.afterSyncProfileCommit(); err != nil {
			return memory.SyncConfigurationStatus{Configured: true, Enabled: configured.Enabled, Credential: memory.SyncCredentialAvailable}, errors.Join(err, close(store))
		}
	}
	if priorWasSlot {
		if err := deleteSyncCredential(remove, previous.CredentialRef); err != nil {
			return memory.SyncConfigurationStatus{Configured: true, Enabled: configured.Enabled, Credential: memory.SyncCredentialAvailable}, errors.Join(err, close(store))
		}
		configured.PreviousCredentialRef = ""
		if _, err := configure(ctx, store, configured); err != nil {
			return memory.SyncConfigurationStatus{Configured: true, Enabled: configured.Enabled, Credential: memory.SyncCredentialAvailable}, errors.Join(err, close(store))
		}
	}
	closeErr := close(store)
	status := memory.SyncConfigurationStatus{Configured: true, Enabled: configured.Enabled, Credential: memory.SyncCredentialAvailable}
	return status, closeErr
}

// configureSyncFile validates the explicitly supplied file and stores only a
// fixed marker; neither its path nor its bearer is persisted or sent anywhere.
func (runtime Memory) configureSyncFile(ctx context.Context, opts config.Options, endpoint, deviceID string) (memory.SyncConfigurationStatus, error) {
	profile, err := memory.ValidateSyncProfile(memory.SyncProfile{Enabled: true, Endpoint: endpoint, DeviceID: deviceID, CredentialRef: credentialFileReference})
	if err != nil {
		return memory.SyncConfigurationStatus{}, memory.ErrInvalid
	}
	bearer, err := secrets.ReadCredentialFile(opts.CredentialFile)
	if errors.Is(err, secrets.ErrUnsupported) {
		return memory.SyncConfigurationStatus{}, err
	}
	if err != nil || !validBearer(bearer, profile.DeviceID) {
		return memory.SyncConfigurationStatus{}, memory.ErrInvalid
	}
	paths, err := config.Prepare(ctx, opts)
	if err != nil {
		return memory.SyncConfigurationStatus{}, err
	}
	release, err := acquireSyncEnrollmentLock(ctx, paths.Database)
	if err != nil {
		return memory.SyncConfigurationStatus{}, err
	}
	defer release()
	store, err := memory.Open(ctx, paths.Database, nil)
	if err != nil {
		return memory.SyncConfigurationStatus{}, err
	}
	defer store.Close()
	previous, found, err := store.GetSyncProfile(ctx)
	if err != nil {
		return memory.SyncConfigurationStatus{}, err
	}
	if found && (previous.CredentialRef != profile.CredentialRef || previous.PreviousCredentialRef != "") {
		return memory.SyncConfigurationStatus{}, memory.ErrConflict
	}
	if _, err := store.ConfigureSyncProfile(ctx, profile); err != nil {
		return memory.SyncConfigurationStatus{}, err
	}
	return memory.SyncConfigurationStatus{Configured: true, Enabled: true, Credential: memory.SyncCredentialAvailable}, nil
}

func validSyncRecoveryMarker(active, previous, first, second string) bool {
	return active == first && previous == second || active == second && previous == first
}

func deleteSyncCredential(remove func(string) error, reference string) error {
	err := remove(reference)
	if errors.Is(err, secrets.ErrMissing) {
		return nil
	}
	return err
}

func reconcileSyncEnrollment(ctx context.Context, store *memory.Store, profile memory.SyncProfile, remove func(string) error, configure func(context.Context, *memory.Store, memory.SyncProfile) (memory.SyncProfile, error)) error {
	if err := deleteSyncCredential(remove, profile.PreviousCredentialRef); err != nil {
		return err
	}
	profile.PreviousCredentialRef = ""
	_, err := configure(ctx, store, profile)
	return err
}

// SyncStatus reports local enrollment state without opening a network connection
// or returning a credential value.
func (runtime Memory) SyncStatus(ctx context.Context, opts config.Options) (memory.SyncConfigurationStatus, error) {
	status := memory.SyncConfigurationStatus{Credential: memory.SyncCredentialNotConfigured}
	if opts.ProjectLocal {
		return status, nil
	}
	paths, err := config.PathsFor(opts)
	if err != nil {
		return status, err
	}
	if _, err := os.Stat(paths.Database); errors.Is(err, os.ErrNotExist) {
		return status, nil
	} else if err != nil {
		return status, err
	}
	store, err := openStoreRead(ctx, opts)
	if err != nil {
		return status, err
	}
	defer store.Close()
	profile, found, err := store.GetSyncProfile(ctx)
	if err != nil || !found {
		return status, err
	}
	status.Configured, status.Enabled = true, profile.Enabled
	credential, err := runtime.syncCredential(opts, profile.CredentialRef)
	if errors.Is(err, secrets.ErrUnsupported) {
		return status, err
	}
	switch {
	case err == nil && validBearer(credential, profile.DeviceID):
		status.Credential = memory.SyncCredentialAvailable
	case err == nil:
		status.Credential = memory.SyncCredentialInvalid
	case errors.Is(err, secrets.ErrMissing):
		status.Credential = memory.SyncCredentialMissing
	default:
		status.Credential = memory.SyncCredentialUnavailable
	}
	return status, nil
}

// BackfillSyncProject queues unsynced local records for one resolved workspace.
// It is local-only and deliberately does not load credentials.
func (runtime Memory) BackfillSyncProject(ctx context.Context, opts config.Options, workspace string, limit int) (memory.SyncBackfillResult, error) {
	if runtime.readOnly || opts.ProjectLocal || workspace == "" || limit < 1 || limit > 1000 {
		return memory.SyncBackfillResult{}, memory.ErrInvalid
	}
	return withWritableStore(ctx, opts, func(store *memory.Store) (memory.SyncBackfillResult, error) {
		project, err := store.ResolveProject(ctx, workspace)
		if err != nil {
			return memory.SyncBackfillResult{}, err
		}
		return store.BackfillSyncProject(ctx, project, limit)
	})
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

func (remote syncRemote) PullProject(ctx context.Context, cursor syncservice.Cursor, project string, limit int) (syncservice.PullPage, error) {
	page, err := remote.client.PullProject(ctx, remote.credential, cursor, project, limit)
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
	PullProject(context.Context, syncservice.Cursor, string, int) (syncservice.PullPage, error)
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

type foregroundProjectStore interface {
	ClaimDueSyncOutboxForProject(context.Context, time.Duration, int, string) ([]memory.SyncOutboxClaim, error)
	TranslateSyncMutations(context.Context, string, string, []syncservice.Mutation) ([]syncservice.Mutation, error)
	ApplySyncPushResult(context.Context, string, string, syncservice.Result) error
	MarkSyncOutboxRetry(context.Context, string, string, time.Time, string) error
	ProjectPullCursor(context.Context, string, string) (syncservice.Cursor, error)
	ApplyProjectPulledPage(context.Context, string, string, syncservice.PullPage) error
}

// runForegroundProjectSync synchronizes one locally bound project without
// touching owner-global pull cursors or bootstrap state.
func runForegroundProjectSync(ctx context.Context, store foregroundProjectStore, remote foregroundRemote, project, portableProject string) (memory.SyncResult, error) {
	result := memory.SyncResult{Mode: memory.SyncModeProjectBidirectional, Status: memory.SyncStatusSynced}
	if err := ctx.Err(); err != nil {
		result.Status = memory.SyncStatusPartial
		return result, err
	}
	capabilitiesChecked := false
	for batch := 0; batch < foregroundSyncBatches; batch++ {
		claims, err := store.ClaimDueSyncOutboxForProject(ctx, foregroundSyncLease, 16, project)
		if err != nil {
			result.Status = memory.SyncStatusPartial
			return result, err
		}
		if len(claims) == 0 {
			return runForegroundProjectPull(ctx, store, remote, project, portableProject, result)
		}
		result.Batches++
		mutations := make([]syncservice.Mutation, len(claims))
		for index := range claims {
			mutations[index] = claims[index].Mutation
		}
		mutations, err = store.TranslateSyncMutations(ctx, portableProject, project, mutations)
		if err != nil {
			result.Status = memory.SyncStatusPartial
			return result, err
		}
		if !capabilitiesChecked {
			capabilityCtx, cancel := context.WithTimeout(ctx, foregroundSyncTimeout)
			err = remote.Capabilities(capabilityCtx)
			cancel()
			if err != nil {
				if ctx.Err() != nil {
					result.Status = memory.SyncStatusPartial
					return result, ctx.Err()
				}
				result.Status = syncStatusForError(err)
				if result.Status == memory.SyncStatusUnreachable && !markClaimsRetry(ctx, store, claims, &result) {
					result.Status = memory.SyncStatusPartial
				}
				return result, nil
			}
			capabilitiesChecked = true
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
			} else if markClaimsRetry(ctx, store, claims, &result) {
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
				blocking = true
			}
		}
		if blocking {
			return result, nil
		}
	}
	result.Status = memory.SyncStatusPartial
	return result, nil
}

func runForegroundProjectPull(ctx context.Context, store foregroundProjectStore, remote foregroundRemote, project, portableProject string, result memory.SyncResult) (memory.SyncResult, error) {
	if err := ctx.Err(); err != nil {
		result.Status = memory.SyncStatusPartial
		return result, err
	}
	discoverCtx, cancel := context.WithTimeout(ctx, foregroundSyncTimeout)
	discovery, err := remote.Discover(discoverCtx)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			result.Status = memory.SyncStatusPartial
			return result, ctx.Err()
		}
		result.Status = syncStatusForError(err)
		return result, nil
	}
	if syncservice.ValidateDiscovery(discovery) != nil {
		result.Status = memory.SyncStatusIncompatible
		return result, nil
	}
	cursor, err := store.ProjectPullCursor(ctx, portableProject, discovery.HistoryID)
	if err != nil {
		result.Status = memory.SyncStatusPartial
		return result, err
	}
	for pull := 0; pull < foregroundSyncBatches; pull++ {
		if err := ctx.Err(); err != nil {
			result.Status = memory.SyncStatusPartial
			return result, err
		}
		pullCtx, cancel := context.WithTimeout(ctx, foregroundSyncTimeout)
		page, pullErr := remote.PullProject(pullCtx, cursor, portableProject, syncapi.DefaultPullLimit)
		cancel()
		if pullErr != nil {
			if ctx.Err() != nil {
				result.Status = memory.SyncStatusPartial
				return result, ctx.Err()
			}
			result.Status = syncStatusForError(pullErr)
			return result, nil
		}
		if page.HasMore && page.Cursor.Position <= cursor.Position {
			result.Status = memory.SyncStatusPartial
			return result, nil
		}
		if err = store.ApplyProjectPulledPage(ctx, portableProject, project, page); err != nil {
			if ctx.Err() != nil {
				result.Status = memory.SyncStatusPartial
				return result, ctx.Err()
			}
			result.Status = memory.SyncStatusPartial
			return result, err
		}
		if !page.HasMore {
			return result, nil
		}
		cursor = page.Cursor
	}
	result.Status = memory.SyncStatusPartial
	return result, nil
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

type foregroundRetryStore interface {
	MarkSyncOutboxRetry(context.Context, string, string, time.Time, string) error
}

func markClaimsRetry(ctx context.Context, store foregroundRetryStore, claims []memory.SyncOutboxClaim, result *memory.SyncResult) bool {
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
