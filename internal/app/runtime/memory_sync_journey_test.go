package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	appruntime "github.com/vgxness/vgxness/internal/app/runtime"
	"github.com/vgxness/vgxness/internal/cli"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/secrets"
	"github.com/vgxness/vgxness/internal/syncapi"
	"github.com/vgxness/vgxness/internal/syncservice"
)

const journeyDeviceID = "550e8400-e29b-41d4-a716-446655440010"
const journeyHistoryID = "550e8400-e29b-41d4-a716-446655440011"

// TestExplicitMemorySyncJourney exercises the public command boundary with a
// real store, marker/binding checks, client, and loopback-only TLS server.
func TestExplicitMemorySyncJourney(t *testing.T) {
	newFixture := func(t *testing.T) journeyFixture {
		t.Helper()
		ctx, root := context.Background(), t.TempDir()
		a, b := journeyCanonicalDir(t), journeyCanonicalDir(t)
		runtime := appruntime.NewMemory("journey", false)
		portableA, err := runtime.InitializeProject(ctx, config.Options{StorageRoot: root}, a)
		journeyRequireStatic(t, err == nil, "project A initialization failed")
		portableB, err := runtime.InitializeProject(ctx, config.Options{StorageRoot: root}, b)
		journeyRequireStatic(t, err == nil, "project B initialization failed")
		store, err := memory.Open(ctx, filepath.Join(root, "memory.db"), nil)
		journeyRequireStatic(t, err == nil, "journey store open failed")
		projectA, err := store.ResolveProject(ctx, a)
		projectB := ""
		if err == nil {
			projectB, err = store.ResolveProject(ctx, b)
		}
		if err != nil || projectA == projectB {
			_ = store.Close()
			t.Fatal("project identity contract failed")
		}
		var events []string
		authCalls := 0
		credentialLoads := 0
		reject := false
		auth := &journeyAuthenticator{identity: syncapi.Identity{OwnerID: uuid.MustParse("550e8400-e29b-41d4-a716-446655440012"), DeviceID: uuid.MustParse(journeyDeviceID)}, calls: &authCalls, reject: &reject}
		backend := &journeyBackend{project: portableA}
		handler := syncapi.NewSyncServerHandler(auth, backend, nil)
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			events = append(events, r.Method+" "+r.URL.Path)
			handler.ServeHTTP(w, r)
		}))
		t.Cleanup(server.Close)
		_, err = store.ConfigureSyncProfile(ctx, memory.SyncProfile{Enabled: true, Endpoint: server.URL, DeviceID: journeyDeviceID, CredentialRef: "secret://keychain/sync"})
		if err != nil {
			_ = store.Close()
		}
		journeyRequireStatic(t, err == nil, "sync profile configuration failed")
		err = store.Close()
		journeyRequireStatic(t, err == nil, "journey store close failed")
		appruntime.SetMemorySyncDependenciesForTest(&runtime, server.Client().Transport, func(string) (string, error) {
			credentialLoads++
			return journeyBearer(journeyDeviceID), nil
		})
		_, err = runtime.Remember(ctx, config.Options{StorageRoot: root}, memory.Remember{Project: projectA, Scope: memory.ScopeProject, Content: "A-only-observation"})
		journeyRequireStatic(t, err == nil, "project A observation setup failed")
		_, err = runtime.Remember(ctx, config.Options{StorageRoot: root}, memory.Remember{Project: projectB, Scope: memory.ScopeProject, Content: "B-only-observation"})
		journeyRequireStatic(t, err == nil, "project B observation setup failed")
		run := func(args []string) (int, string, string) {
			var out, stderr bytes.Buffer
			code := cli.RunProductSDDRuntime(ctx, args, strings.NewReader(""), &out, &stderr, nil, runtime, nil, nil, nil, nil, nil)
			journeyRequireSafeOutput(t, out.String()+stderr.String(), journeyBearer(journeyDeviceID), server.URL, root, "A-only-observation", "B-only-observation")
			return code, out.String(), stderr.String()
		}
		return journeyFixture{runtime: &runtime, root: root, a: a, b: b, projectA: projectA, projectB: projectB, portableA: portableA, portableB: portableB, run: run, events: &events, authCalls: &authCalls, credentialLoads: &credentialLoads, reject: &reject, transport: server.Client().Transport, backend: backend}
	}

	t.Run("success status sync status", func(t *testing.T) {
		fixture := newFixture(t)
		journeyRequireCounters(t, fixture, 0, 0, 0)
		code, first, stderr := fixture.run([]string{"memory", "sync", "status", "--storage-root", fixture.root, "--json"})
		if code != 0 || stderr != "" || !strings.Contains(first, `"credential":"available"`) || len(*fixture.events) != 0 {
			t.Fatal("initial status contract failed")
		}
		journeyRequireCounters(t, fixture, 1, 0, 0)
		beforeSync := journeySnapshotB(t, fixture)
		code, result, stderr := fixture.run([]string{"memory", "sync", "--storage-root", fixture.root, "--workspace", fixture.a, "--json"})
		if code != 0 || stderr != "" || !strings.Contains(result, `"status":"synced"`) || !strings.Contains(result, `"mode":"project_bidirectional"`) {
			t.Fatal("sync contract failed")
		}
		journeyRequireCounters(t, fixture, 2, 4, 4)
		captured := fixture.backend.snapshot(t)
		if len(captured) == 0 {
			t.Fatal("outbound capture was empty")
		}
		hasAObservation := false
		for _, mutation := range captured {
			owner, ok := journeyMutationOwner(mutation)
			if !ok || owner != fixture.portableA {
				t.Fatal("outbound ownership contract failed")
			}
			if mutation.Observation != nil && strings.Contains(mutation.Observation.Content, "A-only-observation") {
				hasAObservation = true
			}
		}
		serialized, err := json.Marshal(captured)
		if err != nil || strings.Contains(string(serialized), fixture.portableB) || strings.Contains(string(serialized), "B-only-observation") || !hasAObservation {
			t.Fatal("outbound isolation contract failed")
		}
		if afterSync := journeySnapshotB(t, fixture); !reflect.DeepEqual(afterSync, beforeSync) {
			t.Fatal("B snapshot changed after A sync")
		}
		beforeFinal := journeySnapshotB(t, fixture)
		code, final, stderr := fixture.run([]string{"memory", "sync", "status", "--storage-root", fixture.root, "--json"})
		if code != 0 || stderr != "" || final == "" || strings.Join(*fixture.events, ",") != "GET /v1/sync/capabilities,POST /v1/sync/push,GET /v1/sync/discovery,GET /v1/sync/pull" {
			t.Fatal("final status contract failed")
		}
		journeyRequireCounters(t, fixture, 3, 4, 4)
		if afterFinal := journeySnapshotB(t, fixture); !reflect.DeepEqual(afterFinal, beforeFinal) {
			t.Fatal("B snapshot changed after final status")
		}
		if portable, present, err := memory.ReadProjectID(fixture.a); err != nil || !present || portable == "" || fixture.runtime == nil {
			t.Fatal("A marker contract failed")
		}
	})

	t.Run("invalid workspace and action have no effects", func(t *testing.T) {
		fixture := newFixture(t)
		for _, args := range [][]string{{"memory", "sync", "--storage-root", fixture.root}, {"memory", "sync", "--storage-root", fixture.root, "--workspace", "relative"}, {"memory", "sync", "auto", "--storage-root", fixture.root}} {
			code, out, stderr := fixture.run(args)
			if code != 2 || out != "" || stderr == "" {
				t.Fatal("invalid command contract failed")
			}
		}
		if len(*fixture.events) != 0 || *fixture.credentialLoads != 0 {
			t.Fatal("invalid command had effects")
		}
		journeyRequireCounters(t, fixture, 0, 0, 0)
	})

	t.Run("missing preflight and terminal authentication do not retry", func(t *testing.T) {
		fixture := newFixture(t)
		appruntime.SetMemorySyncDependenciesForTest(fixture.runtime, fixture.transport, func(string) (string, error) { *fixture.credentialLoads++; return "", secrets.ErrMissing })
		code, out, stderr := fixture.run([]string{"memory", "sync", "--storage-root", fixture.root, "--workspace", fixture.a, "--json"})
		if code != 0 || stderr != "" || !strings.Contains(out, `"credential_missing"`) || len(*fixture.events) != 0 || *fixture.credentialLoads != 1 || *fixture.authCalls != 0 {
			t.Fatal("missing credential contract failed")
		}
		appruntime.SetMemorySyncDependenciesForTest(fixture.runtime, fixture.transport, func(string) (string, error) { *fixture.credentialLoads++; return journeyBearer(journeyDeviceID), nil })
		code, status, stderr := fixture.run([]string{"memory", "sync", "status", "--storage-root", fixture.root, "--json"})
		if code != 0 || stderr != "" || !strings.Contains(status, `"credential":"available"`) || len(*fixture.events) != 0 || *fixture.credentialLoads != 2 || *fixture.authCalls != 0 {
			t.Fatal("available status contract failed")
		}
		beforeRejected := journeySnapshotB(t, fixture)
		*fixture.reject = true
		code, result, stderr := fixture.run([]string{"memory", "sync", "--storage-root", fixture.root, "--workspace", fixture.a, "--json"})
		if code != 0 || stderr != "" || !strings.Contains(result, `"status":"unauthorized"`) || strings.Join(*fixture.events, ",") != "GET /v1/sync/capabilities" || *fixture.credentialLoads != 3 || *fixture.authCalls != 1 {
			t.Fatal("terminal authentication contract failed")
		}
		if afterRejected := journeySnapshotB(t, fixture); !reflect.DeepEqual(afterRejected, beforeRejected) {
			t.Fatal("B snapshot changed after rejected A sync")
		}
		beforeFinal := journeySnapshotB(t, fixture)
		code, final, stderr := fixture.run([]string{"memory", "sync", "status", "--storage-root", fixture.root, "--json"})
		if code != 0 || stderr != "" || !strings.Contains(final, `"credential":"available"`) || strings.Join(*fixture.events, ",") != "GET /v1/sync/capabilities" || *fixture.credentialLoads != 4 || *fixture.authCalls != 1 {
			t.Fatal("terminal final status contract failed")
		}
		if afterFinal := journeySnapshotB(t, fixture); !reflect.DeepEqual(afterFinal, beforeFinal) {
			t.Fatal("B snapshot changed after terminal final status")
		}
	})
}

type journeyFixture struct {
	runtime                        *appruntime.Memory
	root, a, b, projectA, projectB string
	portableA, portableB           string
	run                            func([]string) (int, string, string)
	events                         *[]string
	authCalls, credentialLoads     *int
	reject                         *bool
	transport                      http.RoundTripper
	backend                        *journeyBackend
}

func journeyRequireSafeOutput(t *testing.T, output string, forbidden ...string) {
	t.Helper()
	for _, value := range forbidden {
		if strings.Contains(output, value) {
			t.Fatal("command output contained a forbidden value")
		}
	}
}

func journeyRequireCounters(t *testing.T, fixture journeyFixture, loads, auth, events int) {
	t.Helper()
	if *fixture.credentialLoads != loads || *fixture.authCalls != auth || len(*fixture.events) != events {
		t.Fatal("counter contract failed")
	}
}

func journeyRequireStatic(t *testing.T, ok bool, label string) {
	t.Helper()
	if !ok {
		t.Fatal(label)
	}
}

type journeyProjectSnapshot struct {
	marker       string
	observations []memory.Observation
}

func journeySnapshotB(t *testing.T, fixture journeyFixture) journeyProjectSnapshot {
	t.Helper()
	marker, present, err := memory.ReadProjectID(fixture.b)
	if err != nil || !present || marker == "" {
		t.Fatal("B marker contract failed")
	}
	store, err := memory.OpenRead(context.Background(), filepath.Join(fixture.root, "memory.db"))
	journeyRequireStatic(t, err == nil, "snapshot store open failed")
	defer store.Close()
	observations, err := store.Recent(context.Background(), memory.Recent{Project: fixture.projectB, Scope: memory.ScopeProject, States: []memory.State{memory.StateActive, memory.StateNeedsReview, memory.StateArchived}, Limit: 10})
	if err != nil || len(observations) != 1 || observations[0].Project != fixture.projectB || observations[0].Content != "B-only-observation" {
		t.Fatal("B observation snapshot contract failed")
	}
	return journeyProjectSnapshot{marker: marker, observations: observations}
}

func journeyCanonicalDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join(t.TempDir(), "workspace"))
	journeyRequireStatic(t, err == nil, "canonical path resolution failed")
	err = os.MkdirAll(path, 0o755)
	journeyRequireStatic(t, err == nil, "canonical directory creation failed")
	return path
}

func journeyBearer(deviceID string) string {
	return "vgx1." + deviceID + ".AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
}

type journeyAuthenticator struct {
	identity syncapi.Identity
	calls    *int
	reject   *bool
}

func (a *journeyAuthenticator) Authenticate(context.Context, string) (syncapi.Identity, error) {
	*a.calls++
	if *a.reject {
		return syncapi.Identity{}, syncapi.ErrUnauthenticated
	}
	return a.identity, nil
}

type journeyBackend struct {
	project string
	mu      sync.Mutex
	items   []syncservice.Mutation
}

func (b *journeyBackend) Push(_ context.Context, _ uuid.UUID, items []syncservice.Mutation) ([]syncservice.Result, error) {
	copy, err := journeyCopyMutations(items)
	if err != nil {
		return nil, errors.New("invalid push batch")
	}
	for _, item := range copy {
		if err := syncservice.ValidateMutation(item); err != nil {
			return nil, errors.New("invalid push batch")
		}
	}
	b.mu.Lock()
	b.items = append(b.items, copy...)
	b.mu.Unlock()
	results := make([]syncservice.Result, len(copy))
	for i, item := range copy {
		sequence := int64(i + 1)
		results[i] = syncservice.Result{MutationID: item.MutationID, Disposition: syncservice.DispositionAccepted, Sequence: &sequence, Version: 1}
	}
	return results, nil
}

func (b *journeyBackend) snapshot(t *testing.T) []syncservice.Mutation {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	copy, err := journeyCopyMutations(b.items)
	if err != nil {
		t.Fatal("outbound snapshot failed")
	}
	return copy
}

func journeyCopyMutations(items []syncservice.Mutation) ([]syncservice.Mutation, error) {
	encoded, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	var copy []syncservice.Mutation
	if err := json.Unmarshal(encoded, &copy); err != nil {
		return nil, err
	}
	return copy, nil
}

func journeyMutationOwner(mutation syncservice.Mutation) (string, bool) {
	if syncservice.ValidateMutation(mutation) != nil {
		return "", false
	}
	var owner string
	switch {
	case mutation.Project != nil:
		owner = mutation.Project.ID
	case mutation.Session != nil:
		owner = mutation.Session.ProjectID
	case mutation.Observation != nil:
		owner = mutation.Observation.ProjectID
	case mutation.Tombstone != nil:
		owner = mutation.Tombstone.ProjectID
	case mutation.Resolution != nil && mutation.Resolution.Observation != nil:
		owner = mutation.Resolution.Observation.ProjectID
	default:
		return "", false
	}
	return owner, owner != ""
}

func (b *journeyBackend) Discover(context.Context, uuid.UUID) (syncservice.Discovery, error) {
	return syncservice.Discovery{ProtocolVersion: 1, HistoryID: journeyHistoryID, Capabilities: []syncservice.Capability{syncservice.CapabilityBootstrapDiscovery}}, nil
}

func (b *journeyBackend) Pull(context.Context, uuid.UUID, syncservice.Cursor, int) (syncservice.PullPage, error) {
	return syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: journeyHistoryID}}, nil
}

func (b *journeyBackend) PullProject(_ context.Context, _ uuid.UUID, cursor syncservice.Cursor, project string, _ int) (syncservice.PullPage, error) {
	if project != b.project {
		return syncservice.PullPage{}, errors.New("unexpected project")
	}
	return syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: journeyHistoryID, Position: cursor.Position, Watermark: cursor.Watermark}}, nil
}
