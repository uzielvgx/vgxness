package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/sdd"
	"github.com/vgxness/vgxness/internal/secrets"
	"github.com/vgxness/vgxness/internal/syncapi"
	"github.com/vgxness/vgxness/internal/syncclient"
	"github.com/vgxness/vgxness/internal/syncservice"
)

func TestMemoryFacadePersistsAndReadsEntries(t *testing.T) {
	ctx := context.Background()
	opts := config.Options{StorageRoot: t.TempDir()}
	runtime := NewMemory("test", false)
	entry, err := runtime.Remember(ctx, opts, memory.Remember{Content: "durable runtime behavior", Project: "project", Scope: memory.ScopeProject})
	if err != nil || entry.Producer != "test" {
		t.Fatalf("remember = %+v, %v", entry, err)
	}

	recalled, err := runtime.Recall(ctx, opts, memory.Recall{Query: "runtime", Project: "project", Scope: memory.ScopeProject})
	if err != nil || len(recalled) != 1 || recalled[0].ID != entry.ID {
		t.Fatalf("recall = %+v, %v", recalled, err)
	}
	recent, err := runtime.Recent(ctx, opts, memory.Recent{Project: "project", Scope: memory.ScopeProject})
	if err != nil || len(recent) != 1 || recent[0].ID != entry.ID {
		t.Fatalf("recent = %+v, %v", recent, err)
	}
	got, err := runtime.Get(ctx, opts, memory.Lookup{ID: entry.ID, Project: "project", Scope: memory.ScopeProject})
	if err != nil || got.Content != "durable runtime behavior" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	forgotten, err := runtime.Forget(ctx, opts, memory.Forget{ID: entry.ID, Project: "project", Scope: memory.ScopeProject})
	if err != nil || forgotten.State != memory.StateArchived {
		t.Fatalf("forget = %+v, %v", forgotten, err)
	}
}

func TestMemorySyncShortCircuitsWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := NewMemory("test", false).Sync(ctx, config.Options{StorageRoot: t.TempDir(), ProjectDir: t.TempDir()})
	if !errors.Is(err, context.Canceled) || result.Status != memory.SyncStatusUnavailable {
		t.Fatalf("cancelled sync = %+v, %v", result, err)
	}
}

func TestMemorySyncShortCircuitsProjectLocalAndReadOnly(t *testing.T) {
	for _, test := range []struct {
		name     string
		opts     config.Options
		readOnly bool
	}{
		{name: "project local", opts: config.Options{StorageRoot: t.TempDir(), ProjectDir: t.TempDir(), ProjectLocal: true}},
		{name: "read only", opts: config.Options{StorageRoot: t.TempDir(), ProjectDir: t.TempDir()}, readOnly: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			runtime := NewMemory("test", test.readOnly)
			runtime.credential = func(string) (string, error) { calls++; return "", errors.New("unexpected credential") }
			result, err := runtime.Sync(context.Background(), test.opts)
			if err != nil || result.Status != memory.SyncStatusUnavailable || calls != 0 {
				t.Fatalf("sync = %+v, %v; credential calls=%d", result, err, calls)
			}
		})
	}
}

func TestMemorySyncClassifiesCredentialAndProfileValidationFailures(t *testing.T) {
	root := t.TempDir()
	opts := config.Options{StorageRoot: root, ProjectDir: t.TempDir()}
	store, err := openStore(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	profile := memory.SyncProfile{Enabled: true, Endpoint: "https://sync.example.test", DeviceID: "550e8400-e29b-41d4-a716-446655440000", CredentialRef: "secret://keychain/sync"}
	if _, err = store.ConfigureSyncProfile(context.Background(), profile); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		credential string
		err        error
		want       memory.SyncStatus
	}{
		{name: "missing", err: secrets.ErrMissing, want: memory.SyncStatusCredentialMissing},
		{name: "unavailable", err: secrets.ErrUnavailable, want: memory.SyncStatusCredentialUnavailable},
		{name: "generic", err: errors.New("credential failure"), want: memory.SyncStatusUnavailable},
		{name: "malformed", credential: "malformed", want: memory.SyncStatusInvalid},
		{name: "mismatched device", credential: "vgx1.550e8400-e29b-41d4-a716-446655440001.secret", want: memory.SyncStatusInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime := NewMemory("test", false)
			runtime.credential = func(string) (string, error) { return test.credential, test.err }
			result, err := runtime.Sync(context.Background(), opts)
			if err != nil || result.Status != test.want {
				t.Fatalf("sync = %+v, %v; want %q", result, err, test.want)
			}
		})
	}

	store, err = openStore(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	profile.Endpoint = "https://sync.example.test/not-supported"
	if _, err = store.ConfigureSyncProfile(context.Background(), profile); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	runtime := NewMemory("test", false)
	runtime.credential = func(string) (string, error) { return "vgx1.550e8400-e29b-41d4-a716-446655440000.secret", nil }
	result, err := runtime.Sync(context.Background(), opts)
	if err != nil || result.Status != memory.SyncStatusInvalid {
		t.Fatalf("invalid endpoint sync = %+v, %v", result, err)
	}
}

func TestSyncRemoteForwardsCredentialAndHTTPErrors(t *testing.T) {
	var requests []*http.Request
	client, err := syncclient.New("https://sync.example.test", roundTripper(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request)
		return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(http.NoBody), Request: request}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	remote := syncRemote{client: client, credential: "credential"}
	cursor := syncservice.Cursor{HistoryID: "550e8400-e29b-41d4-a716-446655440000"}
	if _, err = remote.Discover(context.Background()); !errors.Is(err, syncclient.ErrUnauthorized) {
		t.Fatalf("discover error = %v", err)
	}
	if _, err = remote.Pull(context.Background(), cursor, 1); !errors.Is(err, syncclient.ErrUnauthorized) {
		t.Fatalf("pull error = %v", err)
	}
	if err = remote.Capabilities(context.Background()); !errors.Is(err, syncclient.ErrUnauthorized) {
		t.Fatalf("capabilities error = %v", err)
	}
	if len(requests) != 3 {
		t.Fatalf("requests = %d, want 3", len(requests))
	}
	for _, request := range requests {
		if request.Header.Get("Authorization") != "Bearer credential" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
	}
}

func TestSyncRemoteMapsSuccessfulPullAndPush(t *testing.T) {
	cursor := syncservice.Cursor{HistoryID: "550e8400-e29b-41d4-a716-446655440000", Position: 2, Watermark: 5}
	mutation := syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440001", RecordID: "project", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "project"}}
	sequence := int64(3)
	client, err := syncclient.New("https://sync.example.test", roundTripper(func(request *http.Request) (*http.Response, error) {
		var body []byte
		switch request.URL.Path {
		case "/v1/sync/pull":
			if request.URL.Query().Get("history_id") != cursor.HistoryID || request.URL.Query().Get("after") != "2" || request.URL.Query().Get("watermark") != "5" || request.URL.Query().Get("limit") != "1" {
				return nil, errors.New("unexpected pull query")
			}
			change := syncservice.Change{Sequence: 3, CanonicalVersion: 1, Mutation: mutation}
			change.ChangeHash, _ = syncservice.CanonicalChangeHash(change)
			body, _ = json.Marshal(syncapi.PullResponse{ProtocolVersion: syncapi.ProtocolVersion, HistoryID: cursor.HistoryID, Position: 3, Watermark: cursor.Watermark, HasMore: true, Changes: []syncservice.Change{change}})
		case "/v1/sync/push":
			var pushed syncapi.PushRequest
			if err := json.NewDecoder(request.Body).Decode(&pushed); err != nil || len(pushed.Items) != 1 || pushed.Items[0].MutationID != mutation.MutationID {
				return nil, errors.New("unexpected push body")
			}
			body, _ = json.Marshal(syncapi.PushResponse{ProtocolVersion: syncapi.ProtocolVersion, Results: []syncservice.Result{{MutationID: mutation.MutationID, Disposition: syncservice.DispositionAccepted, Sequence: &sequence, Version: 1}}})
		default:
			return nil, errors.New("unexpected request path")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{syncapi.MediaType}}, Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	remote := syncRemote{client: client, credential: "credential"}
	page, err := remote.Pull(context.Background(), cursor, 1)
	if err != nil || page.Cursor != (syncservice.Cursor{HistoryID: cursor.HistoryID, Position: 3, Watermark: cursor.Watermark}) || !page.HasMore || len(page.Changes) != 1 || page.Changes[0].Mutation.MutationID != mutation.MutationID {
		t.Fatalf("pull = %+v, %v", page, err)
	}
	results, err := remote.Push(context.Background(), []syncservice.Mutation{mutation})
	if err != nil || len(results) != 1 || results[0].MutationID != mutation.MutationID || results[0].Sequence == nil || *results[0].Sequence != sequence {
		t.Fatalf("push = %+v, %v", results, err)
	}
}

func TestSyncStatusAndWithStorePreserveAllErrors(t *testing.T) {
	for _, test := range []struct {
		err  error
		want memory.SyncStatus
	}{
		{syncclient.ErrUnauthorized, memory.SyncStatusUnauthorized},
		{syncclient.ErrUnavailable, memory.SyncStatusUnreachable},
		{syncclient.ErrRemote, memory.SyncStatusIncompatible},
		{syncclient.ErrDiscoveryUnsupported, memory.SyncStatusIncompatible},
		{memory.ErrConflict, memory.SyncStatusConflict},
		{errors.New("other"), memory.SyncStatusUnavailable},
	} {
		if got := syncStatusForError(test.err); got != test.want {
			t.Fatalf("syncStatusForError(%v) = %q, want %q", test.err, got, test.want)
		}
	}
	operationErr, closeErr := errors.New("operation"), errors.New("close")
	_, err := withStore(func() (*memory.Store, error) { return nil, nil }, func(*memory.Store) (string, error) { return "", operationErr }, func(*memory.Store) error { return closeErr })
	if !errors.Is(err, operationErr) || !errors.Is(err, closeErr) {
		t.Fatalf("joined error = %v", err)
	}
}

func TestSDDFacadeCreatesAndReadsChangeAndRevision(t *testing.T) {
	ctx := context.Background()
	opts := config.Options{StorageRoot: t.TempDir()}
	runtime := NewSDD()
	change, err := runtime.CreateChange(ctx, opts, sdd.CreateChangeRequest{Project: "project", IdempotencyKey: "change-1", Title: "Runtime test", Backend: sdd.BackendMemory, InteractionMode: sdd.InteractionAutomatic, Plan: sdd.PlanLow})
	if err != nil || change.ID == "" {
		t.Fatalf("create = %+v, %v", change, err)
	}
	changes, err := runtime.ListChanges(ctx, opts, sdd.ListChangesRequest{Project: "project"})
	if err != nil || len(changes) != 1 || changes[0].ID != change.ID {
		t.Fatalf("list = %+v, %v", changes, err)
	}
	got, err := runtime.GetChange(ctx, opts, sdd.GetChangeRequest{Project: "project", ID: change.ID})
	if err != nil || got.Title != change.Title {
		t.Fatalf("get = %+v, %v", got, err)
	}
	revision, err := runtime.SaveRevision(ctx, opts, sdd.SaveRevisionRequest{Project: "project", ChangeID: change.ID, Artifact: sdd.PhaseExplore, Content: []byte("exploration"), ExpectedStateVersion: change.StateVersion})
	if err != nil || revision.ID == "" {
		t.Fatalf("save revision = %+v, %v", revision, err)
	}
	revisions, err := runtime.ListRevisions(ctx, opts, sdd.ListRevisionsRequest{Project: "project", ChangeID: change.ID})
	if err != nil || len(revisions) != 1 || revisions[0].ID != revision.ID {
		t.Fatalf("list revisions = %+v, %v", revisions, err)
	}
	gotRevision, err := runtime.GetRevision(ctx, opts, sdd.GetRevisionRequest{Project: "project", ChangeID: change.ID, RevisionID: revision.ID})
	if err != nil || string(gotRevision.Content) != "exploration" {
		t.Fatalf("get revision = %+v, %v", gotRevision, err)
	}
}
