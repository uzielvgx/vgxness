package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/syncservice"
)

const (
	planRuntimeDeviceID      = "550e8400-e29b-41d4-a716-446655440000"
	planRuntimeHistoryID     = "550e8400-e29b-41d4-a716-446655440001"
	planRuntimeOtherHistory  = "550e8400-e29b-41d4-a716-446655440002"
	planRuntimeCredentialRef = "secret://keychain/sync/pending"
)

func TestRuntimePlanSyncProject(t *testing.T) {
	ctx := context.Background()
	workspace, storage := canonicalRuntimeTestDir(t), t.TempDir()
	opts := config.Options{ProjectDir: workspace, StorageRoot: storage}
	runtime := NewMemory("test", false)
	portableID, err := runtime.InitializeProject(ctx, opts, workspace)
	if err != nil {
		t.Fatal(err)
	}
	store, err := memory.Open(ctx, filepath.Join(storage, "memory.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.ConfigureSyncProfile(ctx, memory.SyncProfile{Enabled: true, Endpoint: "https://sync.example.test", DeviceID: "550e8400-e29b-41d4-a716-446655440000", CredentialRef: "secret://keychain/sync/pending"})
	if closeErr := store.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	runtime.credential = func(string) (string, error) { return testBearer("550e8400-e29b-41d4-a716-446655440000"), nil }
	var paths []string
	runtime.transport = roundTripper(func(request *http.Request) (*http.Response, error) {
		paths = append(paths, request.URL.RequestURI())
		if request.Method != http.MethodGet || request.URL.RawQuery != "" || request.Body != nil || request.Header.Get("Authorization") != "Bearer "+testBearer("550e8400-e29b-41d4-a716-446655440000") || request.Header.Get("Accept") != "application/vnd.vgxness.sync+json;version=1" {
			t.Fatalf("unexpected request: %s %s headers=%v", request.Method, request.URL, request.Header)
		}
		body := `{"protocol_version":1,"capabilities":["capabilities","project_state"]}`
		if request.URL.Path == "/v1/sync/projects/"+portableID+"/state" {
			body = `{"status":"active","has_history":true,"history_generation":"550e8400-e29b-41d4-a716-446655440001","watermark":0,"active_observations":0}`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/vnd.vgxness.sync+json;version=1"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})

	plan, err := runtime.PlanSyncProject(ctx, opts)
	if err != nil || plan != (SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionForegroundSync, Reason: SyncPlanReasonBoundActive}) {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	want := []string{"/v1/sync/capabilities", "/v1/sync/projects/" + portableID + "/state"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths=%v want=%v", paths, want)
	}
	paths = nil
	plan, err = runtime.PlanSyncProject(ctx, config.Options{ProjectDir: workspace, StorageRoot: storage, ProjectLocal: true})
	if !errors.Is(err, memory.ErrInvalid) || plan != (SyncProjectPlan{}) || len(paths) != 0 {
		t.Fatalf("project-local plan=%+v err=%v paths=%v", plan, err, paths)
	}
}

func TestRuntimePlanSyncProject_LocalBindingMismatchPrecedesSecretsAndNetwork(t *testing.T) {
	ctx := context.Background()
	workspace, otherWorkspace, storage := canonicalRuntimeTestDir(t), canonicalRuntimeTestDir(t), t.TempDir()
	opts := config.Options{ProjectDir: workspace, StorageRoot: storage}
	runtime := NewMemory("test", false)
	_, err := runtime.InitializeProject(ctx, opts, workspace)
	if err != nil {
		t.Fatal(err)
	}
	otherPortableID, err := runtime.InitializeProject(ctx, config.Options{StorageRoot: storage}, otherWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(workspace, ".vgxness", "project-id")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := memory.EnsureProjectID(workspace, otherPortableID); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(storage, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	credentialCalls, transportCalls := 0, 0
	runtime.credential = func(string) (string, error) {
		credentialCalls++
		return "sentinel-bearer", errors.New("sentinel-reference")
	}
	runtime.transport = roundTripper(func(*http.Request) (*http.Response, error) {
		transportCalls++
		return nil, errors.New("unexpected transport")
	})

	plan, err := runtime.PlanSyncProject(ctx, opts)
	if !errors.Is(err, memory.ErrInvalid) || plan != (SyncProjectPlan{}) || credentialCalls != 0 || transportCalls != 0 {
		t.Fatalf("unconfigured plan=%+v err=%v credential=%d transport=%d", plan, err, credentialCalls, transportCalls)
	}
	store, err := memory.Open(ctx, filepath.Join(storage, "memory.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.ConfigureSyncProfile(ctx, memory.SyncProfile{Enabled: false, Endpoint: "https://sync.example.test", DeviceID: "550e8400-e29b-41d4-a716-446655440000", CredentialRef: "secret://keychain/sync/pending"})
	if closeErr := store.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	plan, err = runtime.PlanSyncProject(ctx, opts)
	if !errors.Is(err, memory.ErrInvalid) || plan != (SyncProjectPlan{}) || credentialCalls != 0 || transportCalls != 0 {
		t.Fatalf("disabled plan=%+v err=%v credential=%d transport=%d", plan, err, credentialCalls, transportCalls)
	}
	store, err = memory.Open(ctx, filepath.Join(storage, "memory.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.ConfigureSyncProfile(ctx, memory.SyncProfile{Enabled: true, Endpoint: "https://sync.example.test", DeviceID: "550e8400-e29b-41d4-a716-446655440000", CredentialRef: "secret://keychain/sync/pending"})
	if closeErr := store.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	before, err = os.ReadFile(filepath.Join(storage, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err = runtime.PlanSyncProject(ctx, opts)
	if err != nil || plan.Action != SyncPlanActionBlockedManual || plan.Reason != SyncPlanReasonBindingMismatch {
		t.Fatalf("configured plan=%+v err=%v", plan, err)
	}
	if credentialCalls != 0 || transportCalls != 0 {
		t.Fatalf("credential=%d transport=%d", credentialCalls, transportCalls)
	}
	after, err := os.ReadFile(filepath.Join(storage, "memory.db"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("database changed or unreadable: %v", err)
	}
}

func TestRuntimePlanSyncProject_RedactsWrappedCredentialCancellation(t *testing.T) {
	for _, want := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(want.Error(), func(t *testing.T) {
			ctx := context.Background()
			workspace, storage := canonicalRuntimeTestDir(t), t.TempDir()
			opts := config.Options{ProjectDir: workspace, StorageRoot: storage}
			runtime := NewMemory("test", false)
			if _, err := runtime.InitializeProject(ctx, opts, workspace); err != nil {
				t.Fatal(err)
			}
			store, err := memory.Open(ctx, filepath.Join(storage, "memory.db"), nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = store.ConfigureSyncProfile(ctx, memory.SyncProfile{Enabled: true, Endpoint: "https://sync.example.test", DeviceID: "550e8400-e29b-41d4-a716-446655440000", CredentialRef: "secret://keychain/sync/pending"})
			if closeErr := store.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				t.Fatal(err)
			}
			runtime.credential = func(string) (string, error) { return "sentinel-token", fmt.Errorf("provider-secret-prefix: %w", want) }
			transportCalls := 0
			runtime.transport = roundTripper(func(*http.Request) (*http.Response, error) { transportCalls++; return nil, errors.New("unexpected") })
			plan, err := runtime.PlanSyncProject(ctx, opts)
			if !errors.Is(err, want) || err.Error() != want.Error() || plan != (SyncProjectPlan{}) || transportCalls != 0 || strings.Contains(err.Error(), "provider-secret-prefix") || strings.Contains(err.Error(), "sync/pending") || strings.Contains(err.Error(), "sentinel-token") {
				t.Fatalf("plan=%+v err=%v transport=%d", plan, err, transportCalls)
			}
		})
	}
}

func TestRuntimePlanSyncProject_OutcomesAndNoMutation(t *testing.T) {
	t.Run("unbound absent no-op", func(t *testing.T) {
		runtime, opts, portableID := setupUnboundPlanRuntime(t)
		runtime.transport = exactPlanRuntimeTransport(t, portableID, `{"status":"absent","has_history":false,"watermark":0,"active_observations":0}`, nil)
		beforeDatabase, beforeMarker := snapshotPlanRuntimeFiles(t, opts, portableID)

		plan, err := runtime.PlanSyncProject(context.Background(), opts)
		if err != nil || plan != (SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionNoOp, Reason: SyncPlanReasonRemoteAbsent}) {
			t.Fatalf("plan=%+v err=%v", plan, err)
		}
		assertPlanRuntimeFilesUnchanged(t, opts, portableID, beforeDatabase, beforeMarker)
	})

	t.Run("compatible cursor foreground", func(t *testing.T) {
		runtime, opts, portableID, project := setupBoundPlanRuntime(t)
		store := openPlanRuntimeStore(t, opts)
		if err := store.ApplyProjectPulledPage(context.Background(), portableID, project, syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: planRuntimeHistoryID}}); err != nil {
			store.Close()
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		runtime.transport = exactPlanRuntimeTransport(t, portableID, `{"status":"active","has_history":true,"history_generation":"`+planRuntimeHistoryID+`","watermark":0,"active_observations":0}`, nil)
		beforeDatabase, beforeMarker := snapshotPlanRuntimeFiles(t, opts, portableID)

		plan, err := runtime.PlanSyncProject(context.Background(), opts)
		if err != nil || plan != (SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionForegroundSync, Reason: SyncPlanReasonBoundActive}) {
			t.Fatalf("plan=%+v err=%v", plan, err)
		}
		assertPlanRuntimeFilesUnchanged(t, opts, portableID, beforeDatabase, beforeMarker)
	})

	t.Run("active transition resumes", func(t *testing.T) {
		runtime, opts, portableID, project := setupBoundPlanRuntime(t)
		store := openPlanRuntimeStore(t, opts)
		transition, err := store.PrepareSyncProjectTransition(context.Background(), portableID, project, memory.SyncProjectTransitionRejoinMerge, false)
		if closeErr := store.Close(); err == nil {
			err = closeErr
		}
		if err != nil || transition.Status != memory.SyncProjectTransitionPulling {
			t.Fatalf("transition=%+v err=%v", transition, err)
		}
		runtime.transport = exactPlanRuntimeTransport(t, portableID, `{"status":"absent","has_history":false,"watermark":0,"active_observations":0}`, nil)
		beforeDatabase, beforeMarker := snapshotPlanRuntimeFiles(t, opts, portableID)

		plan, err := runtime.PlanSyncProject(context.Background(), opts)
		if err != nil || plan.SchemaVersion != 1 || plan.Action != SyncPlanActionResumeTransition || plan.Reason != SyncPlanReasonActiveTransition || plan.TransitionMode != memory.SyncProjectTransitionRejoinMerge || plan.transitionIdentity <= 0 {
			t.Fatalf("plan=%+v err=%v", plan, err)
		}
		assertPlanRuntimeFilesUnchanged(t, opts, portableID, beforeDatabase, beforeMarker)
	})

	t.Run("cursor history mismatch blocks", func(t *testing.T) {
		runtime, opts, portableID, project := setupBoundPlanRuntime(t)
		store := openPlanRuntimeStore(t, opts)
		if err := store.ApplyProjectPulledPage(context.Background(), portableID, project, syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: planRuntimeHistoryID}}); err != nil {
			store.Close()
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		runtime.transport = exactPlanRuntimeTransport(t, portableID, `{"status":"active","has_history":true,"history_generation":"`+planRuntimeOtherHistory+`","watermark":0,"active_observations":0}`, nil)
		beforeDatabase, beforeMarker := snapshotPlanRuntimeFiles(t, opts, portableID)

		plan, err := runtime.PlanSyncProject(context.Background(), opts)
		want := SyncProjectPlan{SchemaVersion: 1, Action: SyncPlanActionBlockedManual, Reason: SyncPlanReasonCursorHistoryMismatch}
		if err != nil || plan != want {
			t.Fatalf("plan=%+v want=%+v err=%v", plan, want, err)
		}
		assertPlanRuntimeFilesUnchanged(t, opts, portableID, beforeDatabase, beforeMarker)
	})
}

func TestRuntimePlanSyncProject_LocalFailuresPrecedeSecretsAndNetwork(t *testing.T) {
	t.Run("invalid invocation", func(t *testing.T) {
		workspace := canonicalRuntimeTestDir(t)
		storage := t.TempDir()
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		cases := []struct {
			name string
			ctx  context.Context
			opts config.Options
		}{
			{"nil context", nil, config.Options{ProjectDir: workspace, StorageRoot: storage}},
			{"cancelled", cancelled, config.Options{ProjectDir: workspace, StorageRoot: storage}},
			{"relative workspace", context.Background(), config.Options{ProjectDir: "relative", StorageRoot: storage}},
			{"project local", context.Background(), config.Options{ProjectDir: workspace, StorageRoot: storage, ProjectLocal: true}},
			{"missing marker", context.Background(), config.Options{ProjectDir: workspace, StorageRoot: storage}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				credentialCalls, transportCalls := 0, 0
				runtime := NewMemory("test", false)
				runtime.credential = func(string) (string, error) { credentialCalls++; return "secret", nil }
				runtime.transport = roundTripper(func(*http.Request) (*http.Response, error) {
					transportCalls++
					return nil, errors.New("unexpected transport")
				})
				plan, err := runtime.PlanSyncProject(tc.ctx, tc.opts)
				if err == nil || plan != (SyncProjectPlan{}) || credentialCalls != 0 || transportCalls != 0 {
					t.Fatalf("plan=%+v err=%v credential=%d transport=%d", plan, err, credentialCalls, transportCalls)
				}
			})
		}
	})

	t.Run("unconfigured and disabled profile", func(t *testing.T) {
		runtime, opts, _, _ := setupBoundPlanRuntimeWithoutProfile(t)
		credentialCalls, transportCalls := 0, 0
		runtime.credential = func(string) (string, error) { credentialCalls++; return "secret", nil }
		runtime.transport = roundTripper(func(*http.Request) (*http.Response, error) {
			transportCalls++
			return nil, errors.New("unexpected transport")
		})
		plan, err := runtime.PlanSyncProject(context.Background(), opts)
		if !errors.Is(err, memory.ErrInvalid) || plan != (SyncProjectPlan{}) || credentialCalls != 0 || transportCalls != 0 {
			t.Fatalf("unconfigured plan=%+v err=%v credential=%d transport=%d", plan, err, credentialCalls, transportCalls)
		}

		configurePlanRuntimeProfile(t, opts, false)
		plan, err = runtime.PlanSyncProject(context.Background(), opts)
		if !errors.Is(err, memory.ErrInvalid) || plan != (SyncProjectPlan{}) || credentialCalls != 0 || transportCalls != 0 {
			t.Fatalf("disabled plan=%+v err=%v credential=%d transport=%d", plan, err, credentialCalls, transportCalls)
		}
	})
}

func TestRuntimePlanSyncProject_CredentialFailuresAreRedacted(t *testing.T) {
	baseRuntime, opts, _, _ := setupBoundPlanRuntime(t)
	cases := []struct {
		name       string
		credential func(string) (string, error)
		wantError  error
	}{
		{"getter error", func(string) (string, error) { return "", errors.New("sentinel-secret-value") }, memory.ErrInvalid},
		{"invalid bearer", func(string) (string, error) { return "sentinel-secret-value", nil }, memory.ErrInvalid},
		{"cancelled getter", func(string) (string, error) { return "", context.Canceled }, context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runtime := baseRuntime
			transportCalls := 0
			runtime.credential = tc.credential
			runtime.transport = roundTripper(func(*http.Request) (*http.Response, error) {
				transportCalls++
				return nil, errors.New("unexpected transport")
			})
			plan, err := runtime.PlanSyncProject(context.Background(), opts)
			if !errors.Is(err, tc.wantError) || plan != (SyncProjectPlan{}) || transportCalls != 0 {
				t.Fatalf("plan=%+v err=%v transport=%d", plan, err, transportCalls)
			}
			if strings.Contains(err.Error(), "sentinel-secret-value") || strings.Contains(err.Error(), planRuntimeCredentialRef) {
				t.Fatalf("secret leaked in error: %v", err)
			}
		})
	}
}

func TestRuntimePlanSyncProject_RemoteFailuresDoNotMutateOrLeak(t *testing.T) {
	baseRuntime, opts, portableID, _ := setupBoundPlanRuntime(t)
	beforeDatabase, beforeMarker := snapshotPlanRuntimeFiles(t, opts, portableID)
	token := testBearer(planRuntimeDeviceID)
	cases := []struct {
		name      string
		transport http.RoundTripper
		wantCalls int
		wantError error
	}{
		{
			name: "unsupported capability",
			transport: exactPlanRuntimeTransport(t, portableID, `{"status":"active","has_history":true,"history_generation":"`+planRuntimeHistoryID+`","watermark":0,"active_observations":0}`, func(request *http.Request, call int) (*http.Response, error) {
				if call == 1 {
					return planRuntimeResponse(http.StatusOK, `{"protocol_version":1,"capabilities":["capabilities"]}`), nil
				}
				return nil, errors.New("unexpected request")
			}),
			wantCalls: 1,
		},
		{
			name:      "malformed state",
			transport: exactPlanRuntimeTransport(t, portableID, `{"status":"active","has_history":false,"watermark":0,"active_observations":0}`, nil),
			wantCalls: 2,
		},
		{
			name: "transport failure",
			transport: exactPlanRuntimeTransport(t, portableID, `{}`, func(*http.Request, int) (*http.Response, error) {
				return nil, errors.New("transport-failure")
			}),
			wantCalls: 1,
		},
		{
			name: "status failure redacts body",
			transport: exactPlanRuntimeTransport(t, portableID, `{}`, func(*http.Request, int) (*http.Response, error) {
				return planRuntimeResponse(http.StatusServiceUnavailable, `{"error":"sentinel-response-secret"}`), nil
			}),
			wantCalls: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runtime := baseRuntime
			calls := 0
			runtime.transport = roundTripper(func(request *http.Request) (*http.Response, error) {
				calls++
				return tc.transport.RoundTrip(request)
			})
			plan, err := runtime.PlanSyncProject(context.Background(), opts)
			if err == nil || plan != (SyncProjectPlan{}) || calls != tc.wantCalls {
				t.Fatalf("plan=%+v err=%v calls=%d want=%d", plan, err, calls, tc.wantCalls)
			}
			if tc.wantError != nil && !errors.Is(err, tc.wantError) {
				t.Fatalf("err=%v want=%v", err, tc.wantError)
			}
			if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), planRuntimeCredentialRef) || strings.Contains(err.Error(), "sentinel-response-secret") {
				t.Fatalf("secret leaked in error: %v", err)
			}
			assertPlanRuntimeFilesUnchanged(t, opts, portableID, beforeDatabase, beforeMarker)
		})
	}

	t.Run("request cancellation", func(t *testing.T) {
		runtime := baseRuntime
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		runtime.transport = roundTripper(func(request *http.Request) (*http.Response, error) {
			calls++
			cancel()
			<-request.Context().Done()
			return nil, request.Context().Err()
		})
		plan, err := runtime.PlanSyncProject(ctx, opts)
		if !errors.Is(err, context.Canceled) || plan != (SyncProjectPlan{}) || calls != 1 {
			t.Fatalf("plan=%+v err=%v calls=%d", plan, err, calls)
		}
		if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), planRuntimeCredentialRef) {
			t.Fatalf("secret leaked in error: %v", err)
		}
		assertPlanRuntimeFilesUnchanged(t, opts, portableID, beforeDatabase, beforeMarker)
	})
}

func setupBoundPlanRuntime(t *testing.T) (Memory, config.Options, string, string) {
	t.Helper()
	runtime, opts, portableID, project := setupBoundPlanRuntimeWithoutProfile(t)
	configurePlanRuntimeProfile(t, opts, true)
	runtime.credential = func(string) (string, error) { return testBearer(planRuntimeDeviceID), nil }
	return runtime, opts, portableID, project
}

func setupBoundPlanRuntimeWithoutProfile(t *testing.T) (Memory, config.Options, string, string) {
	t.Helper()
	ctx := context.Background()
	workspace, storage := canonicalRuntimeTestDir(t), t.TempDir()
	opts := config.Options{ProjectDir: workspace, StorageRoot: storage}
	runtime := NewMemory("test", false)
	portableID, err := runtime.InitializeProject(ctx, opts, workspace)
	if err != nil {
		t.Fatal(err)
	}
	store := openPlanRuntimeStore(t, opts)
	project, err := store.ResolveProject(ctx, workspace)
	if closeErr := store.Close(); err == nil {
		err = closeErr
	}
	if err != nil || project == "" {
		t.Fatalf("project=%q err=%v", project, err)
	}
	return runtime, opts, portableID, project
}

func setupUnboundPlanRuntime(t *testing.T) (Memory, config.Options, string) {
	t.Helper()
	workspace, storage := canonicalRuntimeTestDir(t), t.TempDir()
	opts := config.Options{ProjectDir: workspace, StorageRoot: storage}
	portableID := "550e8400-e29b-41d4-a716-446655440010"
	if _, _, err := memory.EnsureProjectID(workspace, portableID); err != nil {
		t.Fatal(err)
	}
	store := openPlanRuntimeStore(t, opts)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	configurePlanRuntimeProfile(t, opts, true)
	runtime := NewMemory("test", false)
	runtime.credential = func(string) (string, error) { return testBearer(planRuntimeDeviceID), nil }
	return runtime, opts, portableID
}

func configurePlanRuntimeProfile(t *testing.T, opts config.Options, enabled bool) {
	t.Helper()
	store := openPlanRuntimeStore(t, opts)
	_, err := store.ConfigureSyncProfile(context.Background(), memory.SyncProfile{Enabled: enabled, Endpoint: "https://sync.example.test", DeviceID: planRuntimeDeviceID, CredentialRef: planRuntimeCredentialRef})
	if closeErr := store.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
}

func openPlanRuntimeStore(t *testing.T, opts config.Options) *memory.Store {
	t.Helper()
	store, err := memory.Open(context.Background(), filepath.Join(opts.StorageRoot, "memory.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func exactPlanRuntimeTransport(t *testing.T, portableID, state string, override func(*http.Request, int) (*http.Response, error)) http.RoundTripper {
	t.Helper()
	calls := 0
	return roundTripper(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != http.MethodGet || request.URL.RawQuery != "" || request.Body != nil || request.Header.Get("Authorization") != "Bearer "+testBearer(planRuntimeDeviceID) || request.Header.Get("Accept") != "application/vnd.vgxness.sync+json;version=1" {
			t.Fatalf("unexpected request: %s %s headers=%v", request.Method, request.URL, request.Header)
		}
		if override != nil {
			return override(request, calls)
		}
		switch calls {
		case 1:
			if request.URL.RequestURI() != "/v1/sync/capabilities" {
				t.Fatalf("capability path=%q", request.URL.RequestURI())
			}
			return planRuntimeResponse(http.StatusOK, `{"protocol_version":1,"capabilities":["capabilities","project_state"]}`), nil
		case 2:
			if request.URL.RequestURI() != "/v1/sync/projects/"+portableID+"/state" {
				t.Fatalf("state path=%q", request.URL.RequestURI())
			}
			return planRuntimeResponse(http.StatusOK, state), nil
		default:
			t.Fatalf("unexpected request %d: %s", calls, request.URL)
			return nil, errors.New("unexpected request")
		}
	})
}

func planRuntimeResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/vnd.vgxness.sync+json;version=1"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func snapshotPlanRuntimeFiles(t *testing.T, opts config.Options, portableID string) ([]byte, []byte) {
	t.Helper()
	database, err := os.ReadFile(filepath.Join(opts.StorageRoot, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(opts.ProjectDir, ".vgxness", "project-id"))
	if err != nil {
		t.Fatal(err)
	}
	if id, present, err := memory.ReadProjectID(opts.ProjectDir); err != nil || !present || id != portableID {
		t.Fatalf("marker id=%q present=%t err=%v", id, present, err)
	}
	return database, marker
}

func assertPlanRuntimeFilesUnchanged(t *testing.T, opts config.Options, portableID string, wantDatabase, wantMarker []byte) {
	t.Helper()
	gotDatabase, gotMarker := snapshotPlanRuntimeFiles(t, opts, portableID)
	if string(gotDatabase) != string(wantDatabase) || string(gotMarker) != string(wantMarker) {
		t.Fatal("read-only plan changed database or project marker")
	}
}
