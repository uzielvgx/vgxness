package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/secrets"
	"github.com/vgxness/vgxness/internal/syncclient"
	"github.com/vgxness/vgxness/internal/syncservice"
)

func TestMemoryReadOnlyResolveProjectDoesNotCreateAbsentStore(t *testing.T) {
	storageRoot := filepath.Join(t.TempDir(), "absent-store")
	_, err := NewMemory("cli", true).ResolveProject(context.Background(), config.Options{StorageRoot: storageRoot}, t.TempDir())
	if !errors.Is(err, memory.ErrCorrupt) {
		t.Fatalf("resolve project error = %v, want corrupt absent-store error", err)
	}
	if _, err := os.Stat(storageRoot); !os.IsNotExist(err) {
		t.Fatalf("read-only resolve created storage root: %v", err)
	}
}

func TestMemorySyncFailsClosedBeforeSecretsOrTransport(t *testing.T) {
	storageRoot := filepath.Join(t.TempDir(), "project-local")
	credentials, requests := 0, 0
	runtime := NewMemory("cli", false)
	runtime.credential = func(string) (string, error) { credentials++; return "", nil }
	runtime.transport = roundTripper(func(*http.Request) (*http.Response, error) { requests++; return nil, errors.New("unexpected request") })
	result, err := runtime.Sync(context.Background(), config.Options{StorageRoot: storageRoot, ProjectLocal: true})
	if err != nil || result.Status != memory.SyncStatusUnavailable || credentials != 0 || requests != 0 {
		t.Fatalf("project-local sync = %+v, %v; credentials=%d requests=%d", result, err, credentials, requests)
	}
	if _, err := os.Stat(storageRoot); !os.IsNotExist(err) {
		t.Fatalf("project-local sync opened storage root: %v", err)
	}
}

func TestMemorySyncProfileAndCredentialStatesAvoidNetwork(t *testing.T) {
	root := t.TempDir()
	setup := func(profile *memory.SyncProfile) {
		t.Helper()
		store, err := openStore(context.Background(), config.Options{StorageRoot: root})
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		if profile != nil {
			if _, err := store.ConfigureSyncProfile(context.Background(), *profile); err != nil {
				t.Fatal(err)
			}
		}
	}
	run := func(credential string, credentialErr error) (memory.SyncResult, int, int) {
		credentials, requests := 0, 0
		runtime := NewMemory("cli", false)
		runtime.credential = func(string) (string, error) { credentials++; return credential, credentialErr }
		runtime.transport = roundTripper(func(*http.Request) (*http.Response, error) { requests++; return nil, errors.New("unexpected request") })
		result, err := runtime.Sync(context.Background(), config.Options{StorageRoot: root})
		if err != nil {
			t.Fatal(err)
		}
		return result, credentials, requests
	}

	setup(nil)
	result, credentials, requests := run("", nil)
	if result.Status != memory.SyncStatusAbsent || credentials != 0 || requests != 0 {
		t.Fatalf("absent = %+v credentials=%d requests=%d", result, credentials, requests)
	}
	profile := memory.SyncProfile{Enabled: false, Endpoint: "https://sync.example.test", DeviceID: "550e8400-e29b-41d4-a716-446655440000", CredentialRef: "secret://keychain/sync"}
	setup(&profile)
	result, credentials, requests = run("", nil)
	if result.Status != memory.SyncStatusDisabled || credentials != 0 || requests != 0 {
		t.Fatalf("disabled = %+v credentials=%d requests=%d", result, credentials, requests)
	}
	profile.Enabled = true
	setup(&profile)
	for _, credential := range []string{"malformed", "vgx1.550e8400-e29b-41d4-a716-446655440001.secret"} {
		result, credentials, requests = run(credential, nil)
		if result.Status != memory.SyncStatusInvalid || credentials != 1 || requests != 0 {
			t.Fatalf("credential %q = %+v credentials=%d requests=%d", credential, result, credentials, requests)
		}
	}
	for credentialErr, want := range map[error]memory.SyncStatus{secrets.ErrMissing: memory.SyncStatusCredentialMissing, secrets.ErrUnavailable: memory.SyncStatusCredentialUnavailable} {
		result, credentials, requests = run("vgx1.550e8400-e29b-41d4-a716-446655440000.secret", credentialErr)
		if result.Status != want || credentials != 1 || requests != 0 {
			t.Fatalf("credential error %v = %+v credentials=%d requests=%d", credentialErr, result, credentials, requests)
		}
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (round roundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return round(request)
}

func TestRunForegroundSyncPreflightsAndPersistsOutcomes(t *testing.T) {
	store := newForegroundStore(t)
	remote := &testForegroundRemote{disposition: syncservice.DispositionPreviouslyAccepted}
	result, err := runForegroundSync(context.Background(), store, remote)
	if err != nil || result.Status != memory.SyncStatusSynced || result.Pushed != 2 || result.PreviouslyAccepted != 2 || remote.capabilities != 1 || remote.pushes != 1 || remote.discovers != 1 {
		t.Fatalf("result=%+v err=%v remote=%+v", result, err, remote)
	}

	store = newForegroundStore(t)
	remote = &testForegroundRemote{disposition: syncservice.DispositionRejected, retryable: true}
	result, err = runForegroundSync(context.Background(), store, remote)
	entries, entriesErr := store.DueSyncOutbox(context.Background(), time.Now().Add(time.Hour))
	if err != nil || entriesErr != nil || result.Status != memory.SyncStatusPartial || result.Pushed != 0 || result.Retried != 2 || len(entries) != 2 || remote.discovers != 0 {
		t.Fatalf("retry result=%+v err=%v entries=%+v entriesErr=%v remote=%+v", result, err, entries, entriesErr, remote)
	}
}

func TestRunForegroundSyncCapabilityAndTransportFailuresDoNotBootstrap(t *testing.T) {
	store := newForegroundStore(t)
	remote := &testForegroundRemote{capabilityErr: syncclient.ErrUnauthorized}
	result, err := runForegroundSync(context.Background(), store, remote)
	if err != nil || result.Status != memory.SyncStatusUnauthorized || remote.pushes != 0 || remote.discovers != 0 {
		t.Fatalf("capability result=%+v err=%v remote=%+v", result, err, remote)
	}

	store = newForegroundStore(t)
	remote = &testForegroundRemote{pushErr: syncclient.ErrUnavailable}
	result, err = runForegroundSync(context.Background(), store, remote)
	entries, entriesErr := store.DueSyncOutbox(context.Background(), time.Now().Add(time.Hour))
	if err != nil || entriesErr != nil || result.Status != memory.SyncStatusUnreachable || result.Retried != 2 || len(entries) != 2 || remote.discovers != 0 {
		t.Fatalf("transport result=%+v err=%v entries=%+v entriesErr=%v remote=%+v", result, err, entries, entriesErr, remote)
	}
}

func TestRunForegroundSyncRejectedAndCapBoundaries(t *testing.T) {
	store := newForegroundStore(t)
	remote := &testForegroundRemote{disposition: syncservice.DispositionRejected}
	result, err := runForegroundSync(context.Background(), store, remote)
	entries, entriesErr := store.DueSyncOutbox(context.Background(), time.Now().Add(time.Hour))
	if err != nil || entriesErr != nil || result.Status != memory.SyncStatusRejected || result.Pushed != 0 || result.Rejected != 2 || len(entries) != 0 || remote.pushes != 1 || remote.discovers != 0 {
		t.Fatalf("rejected result=%+v err=%v entries=%+v entriesErr=%v remote=%+v", result, err, entries, entriesErr, remote)
	}

	store = batchStore(t, 256)
	remote = &testForegroundRemote{disposition: syncservice.DispositionAccepted}
	result, err = runForegroundSync(context.Background(), store, remote)
	if err != nil || result.Status != memory.SyncStatusSynced || result.Pushed != 256 || result.Batches != 16 || remote.pushes != 16 || remote.discovers != 1 {
		t.Fatalf("exact cap result=%+v err=%v remote=%+v", result, err, remote)
	}

	store = batchStore(t, 257)
	remote = &testForegroundRemote{disposition: syncservice.DispositionAccepted}
	result, err = runForegroundSync(context.Background(), store, remote)
	if err != nil || result.Status != memory.SyncStatusPartial || result.Pushed != 256 || result.Batches != 16 || remote.pushes != 16 || remote.discovers != 0 {
		t.Fatalf("over cap result=%+v err=%v remote=%+v", result, err, remote)
	}
}

func TestRunForegroundSyncConflictOrderingAndCancellation(t *testing.T) {
	claim := memory.SyncOutboxClaim{SyncOutboxEntry: memory.SyncOutboxEntry{Mutation: syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440099"}}, ClaimToken: "550e8400-e29b-41d4-a716-446655440098"}
	store := &orderedStore{claims: [][]memory.SyncOutboxClaim{{claim}, nil}}
	remote := &testForegroundRemote{disposition: syncservice.DispositionConflict}
	result, err := runForegroundSync(context.Background(), store, remote)
	if err != nil || result.Status != memory.SyncStatusConflict || result.Conflicts != 1 || result.Pushed != 0 || store.ownID != claim.Mutation.MutationID || fmt.Sprint(store.events) != "[claim apply own]" {
		t.Fatalf("conflict result=%+v err=%v events=%v", result, err, store.events)
	}

	ctx, cancel := context.WithCancel(context.Background())
	store = &orderedStore{claims: [][]memory.SyncOutboxClaim{{claim}}}
	remote = &testForegroundRemote{disposition: syncservice.DispositionAccepted, cancelPush: cancel}
	result, err = runForegroundSync(ctx, store, remote)
	if !errors.Is(err, context.Canceled) || result.Status == memory.SyncStatusSynced || fmt.Sprint(store.events) != "[claim]" {
		t.Fatalf("cancel result=%+v err=%v events=%v", result, err, store.events)
	}
	for _, claimErr := range []error{context.Canceled, context.DeadlineExceeded, errors.New("durable")} {
		result, err = runForegroundSync(context.Background(), &orderedStore{claimErr: claimErr}, remote)
		if !errors.Is(err, claimErr) || result.Status != memory.SyncStatusPartial { t.Fatalf("claim error %v: result=%+v err=%v", claimErr, result, err) }
	}

	store = &orderedStore{ids: []string{claim.Mutation.MutationID}, ownErr: errors.New("temporary")}
	result, err = runForegroundSync(context.Background(), store, remote)
	if err != nil || result.Status != memory.SyncStatusPartial || fmt.Sprint(store.events) != "[own]" {
		t.Fatalf("recovery failure result=%+v err=%v events=%v", result, err, store.events)
	}
	store.ownErr, store.events = nil, nil
	result, err = runForegroundSync(context.Background(), store, remote)
	if err != nil || result.Status != memory.SyncStatusConflict || result.Conflicts != 1 || fmt.Sprint(store.events) != "[own]" {
		t.Fatalf("recovery result=%+v err=%v events=%v", result, err, store.events)
	}
}

func batchStore(t *testing.T, count int) *memory.Store {
	t.Helper()
	store := newForegroundStore(t)
	for index := 0; index < (count-2)/2; index++ {
		if _, err := memory.NewMemoryService(store, "cli", nil).Remember(context.Background(), memory.Remember{Content: fmt.Sprintf("sync-%d", index), Project: fmt.Sprintf("project-%d", index), Scope: memory.ScopeProject}); err != nil {
			t.Fatal(err)
		}
	}
	if count%2 != 0 {
		if _, err := memory.NewMemoryService(store, "cli", nil).Remember(context.Background(), memory.Remember{Content: "odd", Project: "default", Scope: memory.ScopeProject}); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

type orderedStore struct {
	claims [][]memory.SyncOutboxClaim
	events []string
	ownID  string
	ids    []string
	ownErr, claimErr error
}

func (store *orderedStore) ClaimDueSyncOutbox(context.Context, time.Duration, int) ([]memory.SyncOutboxClaim, error) {
	store.events = append(store.events, "claim")
	if store.claimErr != nil {
		return nil, store.claimErr
	}
	claims := store.claims[0]
	store.claims = store.claims[1:]
	return claims, nil
}
func (store *orderedStore) ApplySyncPushResult(context.Context, string, string, syncservice.Result) error {
	store.events = append(store.events, "apply")
	return nil
}
func (store *orderedStore) MarkSyncOutboxRetry(context.Context, string, string, time.Time, string) error {
	store.events = append(store.events, "retry")
	return nil
}
func (store *orderedStore) BootstrapSync(context.Context, memory.BootstrapRemote) error {
	store.events = append(store.events, "bootstrap")
	return nil
}
func (store *orderedStore) BootstrapOwnConflict(_ context.Context, _ memory.BootstrapRemote, mutationID string) error {
	store.events = append(store.events, "own")
	store.ownID = mutationID
	return store.ownErr
}
func (store *orderedStore) PendingOwnConflictReceipts(context.Context) ([]string, error) {
	return store.ids, nil
}
func (store *orderedStore) SyncQueueSummary(context.Context) (memory.SyncQueueSummary, error) {
	return memory.SyncQueueSummary{}, nil
}

func newForegroundStore(t *testing.T) *memory.Store {
	t.Helper()
	store, err := openStore(context.Background(), config.Options{StorageRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	_, err = store.ConfigureSyncProfile(context.Background(), memory.SyncProfile{Enabled: true, Endpoint: "https://sync.example.test", DeviceID: "550e8400-e29b-41d4-a716-446655440000", CredentialRef: "secret://keychain/sync"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = memory.NewMemoryService(store, "cli", nil).Remember(context.Background(), memory.Remember{Content: "sync me"}); err != nil {
		t.Fatal(err)
	}
	return store
}

type testForegroundRemote struct {
	disposition   syncservice.Disposition
	retryable     bool
	capabilityErr error
	pushErr       error
	cancelPush    func()
	capabilities  int
	pushes        int
	discovers     int
}

func (remote *testForegroundRemote) Capabilities(context.Context) error {
	remote.capabilities++
	return remote.capabilityErr
}
func (remote *testForegroundRemote) Discover(context.Context) (syncservice.Discovery, error) {
	remote.discovers++
	return syncservice.Discovery{ProtocolVersion: 1, HistoryID: "550e8400-e29b-41d4-a716-446655440010", Capabilities: []syncservice.Capability{syncservice.CapabilityBootstrapDiscovery}}, nil
}
func (remote *testForegroundRemote) Pull(_ context.Context, cursor syncservice.Cursor, _ int) (syncservice.PullPage, error) {
	return syncservice.PullPage{Cursor: cursor}, nil
}
func (remote *testForegroundRemote) Push(_ context.Context, mutations []syncservice.Mutation) ([]syncservice.Result, error) {
	remote.pushes++
	if remote.cancelPush != nil {
		remote.cancelPush()
		return nil, context.Canceled
	}
	if remote.pushErr != nil {
		return nil, remote.pushErr
	}
	results := make([]syncservice.Result, len(mutations))
	for index, mutation := range mutations {
		if remote.retryable {
			results[index] = syncservice.Result{MutationID: mutation.MutationID, Disposition: syncservice.DispositionRejected, Retryable: true, Code: "temporary"}
			continue
		}
		if remote.disposition == syncservice.DispositionRejected {
			results[index] = syncservice.Result{MutationID: mutation.MutationID, Disposition: remote.disposition, Code: "invalid_input"}
			continue
		}
		sequence := int64((remote.pushes-1)*16 + index + 1)
		results[index] = syncservice.Result{MutationID: mutation.MutationID, Disposition: remote.disposition, Sequence: &sequence, Version: mutation.BaseVersion + 1}
	}
	return results, nil
}
