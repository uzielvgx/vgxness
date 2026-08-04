package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/vgxness/vgxness/internal/syncapi"
	"github.com/vgxness/vgxness/internal/syncpg"
	"github.com/vgxness/vgxness/internal/syncservice"
)

type fakeDevices struct {
	credential syncpg.DeviceCredential
	issueErr   error
	revokeErr  error
	issues     int
	revokes    int
	revokedID  uuid.UUID
	onIssue    func()
}

func (f *fakeDevices) IssueDevice(_ context.Context, _ string) (syncpg.DeviceCredential, error) {
	f.issues++
	if f.onIssue != nil {
		f.onIssue()
	}
	return f.credential, f.issueErr
}

func (f *fakeDevices) RevokeDevice(_ context.Context, id uuid.UUID) error {
	f.revokes++
	f.revokedID = id
	return f.revokeErr
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("write") }

func TestRunIssueWritesOnlyBearerAndNewline(t *testing.T) {
	repo := &fakeDevices{credential: syncpg.DeviceCredential{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Bearer: "bearer"}}
	cleanupCalls := 0
	withFakes(t, repo, func() { cleanupCalls++ })
	var stdout, stderr strings.Builder
	if got := runDevice(context.Background(), []string{"device", "issue", "--name", "laptop"}, strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("exit code = %d", got)
	}
	if got := stdout.String(); got != "bearer\n" {
		t.Fatal("unexpected stdout")
	}
	if stderr.Len() != 0 || repo.issues != 1 || repo.revokes != 0 || cleanupCalls != 1 {
		t.Fatal("unexpected issue side effects")
	}
}

func TestRunIssueShortWriteRevokesOnce(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	repo := &fakeDevices{credential: syncpg.DeviceCredential{ID: id, Bearer: "bearer"}}
	withFakes(t, repo, func() {})
	var stderr strings.Builder
	if got := runDevice(context.Background(), []string{"device", "issue", "--name", "laptop"}, strings.NewReader(""), shortWriter{}, &stderr); got != 1 {
		t.Fatalf("exit code = %d", got)
	}
	if repo.issues != 1 || repo.revokes != 1 || repo.revokedID != id {
		t.Fatal("credential was not revoked exactly once")
	}
	if got := stderr.String(); got != "device credential capture failed; device revoked\n" {
		t.Fatal("unexpected stderr")
	}
}

func TestRunIssueShortWriteReportsManualRevokeWithoutBearer(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	repo := &fakeDevices{credential: syncpg.DeviceCredential{ID: id, Bearer: "bearer"}, revokeErr: errors.New("nope")}
	withFakes(t, repo, func() {})
	var stderr strings.Builder
	if got := runDevice(context.Background(), []string{"device", "issue", "--name", "laptop"}, strings.NewReader(""), shortWriter{}, &stderr); got != 1 {
		t.Fatalf("exit code = %d", got)
	}
	if got := stderr.String(); got != "device credential capture failed; manually revoke device 11111111-1111-1111-1111-111111111111\n" {
		t.Fatal("unexpected stderr")
	}
	if strings.Contains(stderr.String(), "bearer") {
		t.Fatal("bearer leaked")
	}
}

func TestRunIssueWriteErrorRevokesOnce(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	repo := &fakeDevices{credential: syncpg.DeviceCredential{ID: id, Bearer: "bearer"}}
	withFakes(t, repo, func() {})
	var stderr strings.Builder
	if got := runDevice(context.Background(), []string{"device", "issue", "--name", "laptop"}, strings.NewReader(""), errorWriter{}, &stderr); got != 1 {
		t.Fatalf("exit code = %d", got)
	}
	if repo.issues != 1 || repo.revokes != 1 || repo.revokedID != id {
		t.Fatal("credential was not revoked exactly once")
	}
	if strings.Contains(stderr.String(), "bearer") {
		t.Fatal("bearer leaked")
	}
}

func TestRunIssueCancellationAfterCommitRevokesWithoutWritingBearer(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ctx, cancel := context.WithCancel(context.Background())
	repo := &fakeDevices{credential: syncpg.DeviceCredential{ID: id, Bearer: "bearer"}, onIssue: cancel}
	cleanupCalls := 0
	withFakes(t, repo, func() { cleanupCalls++ })
	var stdout, stderr strings.Builder
	if got := runDevice(ctx, []string{"device", "issue", "--name", "laptop"}, strings.NewReader(""), &stdout, &stderr); got != 1 {
		t.Fatalf("exit code = %d", got)
	}
	if stdout.Len() != 0 || repo.issues != 1 || repo.revokes != 1 || repo.revokedID != id || cleanupCalls != 1 {
		t.Fatal("cancelled credential capture had unexpected side effects")
	}
	if stderr.String() != "device credential capture failed; device revoked\n" || strings.Contains(stderr.String(), "bearer") {
		t.Fatal("cancelled credential capture was unsafe")
	}
}

func TestRunRejectsOutputAndInvalidRevokeBeforeSetup(t *testing.T) {
	setupCalls := 0
	oldSetup, oldTerminal := setup, terminal
	setup = func(context.Context, string, uuid.UUID) (deviceRepository, func(), error) {
		setupCalls++
		return nil, nil, nil
	}
	terminal = func(any) bool { return true }
	t.Cleanup(func() { setup, terminal = oldSetup, oldTerminal })
	for _, args := range [][]string{
		{"device", "issue", "--name", "laptop", "--output", "file"},
		{"device", "revoke", "00000000-0000-0000-0000-000000000000"},
		{"device", "revoke", "11111111-1111-1111-1111-11111111111A"},
	} {
		if got := runDevice(context.Background(), args, strings.NewReader(""), io.Discard, io.Discard); got != 2 {
			t.Fatalf("exit code = %d", got)
		}
	}
	if setupCalls != 0 {
		t.Fatal("setup called for invalid arguments")
	}
}

func TestRunRejectsMissingOrMalformedConfigurationWithoutLeak(t *testing.T) {
	for _, environment := range []map[string]string{
		{"VGXNESS_SYNC_OWNER_ID": "22222222-2222-2222-2222-222222222222"},
		{"VGXNESS_SYNC_POSTGRES_DSN": "sensitive-dsn", "VGXNESS_SYNC_OWNER_ID": "not-a-uuid"},
	} {
		setupCalls := 0
		oldSetup, oldTerminal := setup, terminal
		setup = func(context.Context, string, uuid.UUID) (deviceRepository, func(), error) {
			setupCalls++
			return nil, nil, nil
		}
		terminal = func(any) bool { return true }
		withEnvironment(t, environment)
		var stderr strings.Builder
		if got := runDevice(context.Background(), []string{"device", "issue", "--name", "laptop"}, strings.NewReader(""), io.Discard, &stderr); got != 1 {
			t.Fatalf("exit code = %d", got)
		}
		if stderr.String() != "device setup failed\n" || setupCalls != 0 || strings.Contains(stderr.String(), "sensitive-dsn") {
			t.Fatal("configuration error leaked or invoked setup")
		}
		setup, terminal = oldSetup, oldTerminal
	}
}

func TestRunOperationFailuresAreSafeAndCleanUpOnce(t *testing.T) {
	secret := "sensitive-dsn"
	for _, test := range []struct {
		name string
		args []string
		repo *fakeDevices
		want string
	}{
		{"issue", []string{"device", "issue", "--name", "laptop"}, &fakeDevices{issueErr: errors.New(secret)}, "device issue failed\n"},
		{"revoke", []string{"device", "revoke", "11111111-1111-1111-1111-111111111111"}, &fakeDevices{revokeErr: errors.New(secret)}, "device revoke failed\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cleanupCalls := 0
			withFakes(t, test.repo, func() { cleanupCalls++ })
			var stderr strings.Builder
			if got := runDevice(context.Background(), test.args, strings.NewReader(""), io.Discard, &stderr); got != 1 {
				t.Fatalf("exit code = %d", got)
			}
			if stderr.String() != test.want || strings.Contains(stderr.String(), secret) || cleanupCalls != 1 {
				t.Fatal("operational failure leaked or cleanup count was wrong")
			}
		})
	}
}

func TestRunSetupErrorCleansUpOnce(t *testing.T) {
	cleanupCalls := 0
	oldSetup, oldTerminal, oldGetenv := setup, terminal, getenv
	setup = func(context.Context, string, uuid.UUID) (deviceRepository, func(), error) {
		return nil, func() { cleanupCalls++ }, errors.New("setup")
	}
	terminal = func(any) bool { return true }
	getenv = func(name string) string {
		if name == "VGXNESS_SYNC_POSTGRES_DSN" {
			return "postgres://example"
		}
		return "22222222-2222-2222-2222-222222222222"
	}
	t.Cleanup(func() { setup, terminal, getenv = oldSetup, oldTerminal, oldGetenv })
	if got := runDevice(context.Background(), []string{"device", "issue", "--name", "laptop"}, strings.NewReader(""), io.Discard, io.Discard); got != 1 {
		t.Fatalf("exit code = %d", got)
	}
	if cleanupCalls != 1 {
		t.Fatal("cleanup was not called exactly once")
	}
}

func TestRealPostgresCLI(t *testing.T) {
	dsn := os.Getenv("VGXNESS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("VGXNESS_TEST_POSTGRES_DSN is not set")
	}
	oldSetup, oldTerminal, oldGetenv := setup, terminal, getenv
	setup = defaultSetup
	terminal = func(any) bool { return true }
	getenv = func(name string) string {
		switch name {
		case "VGXNESS_SYNC_POSTGRES_DSN":
			return dsn
		case "VGXNESS_SYNC_OWNER_ID":
			return "33333333-3333-3333-3333-333333333333"
		default:
			return ""
		}
	}
	t.Cleanup(func() { setup, terminal, getenv = oldSetup, oldTerminal, oldGetenv })
	var stdout, stderr strings.Builder
	if got := runDevice(context.Background(), []string{"device", "issue", "--name", "cli-test"}, strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("issue exit code = %d", got)
	}
	token := strings.TrimSpace(stdout.String())
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatal("invalid credential output")
	}
	id, err := uuid.Parse(parts[1])
	if err != nil || id == uuid.Nil {
		t.Fatal("invalid credential identifier")
	}
	if got := runDevice(context.Background(), []string{"device", "revoke", id.String()}, strings.NewReader(""), io.Discard, &stderr); got != 0 {
		t.Fatalf("revoke exit code = %d", got)
	}
	if stderr.Len() != 0 {
		t.Fatal("unexpected stderr")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("postgres reconnect failed")
	}
	defer conn.Close(context.Background())
	repository, err := syncpg.NewRepository(conn, uuid.MustParse("33333333-3333-3333-3333-333333333333"))
	if err != nil {
		t.Fatal("repository reopen failed")
	}
	if _, err := repository.AuthenticateDevice(ctx, token); !errors.Is(err, syncpg.ErrUnauthenticated) {
		t.Fatal("revoked device authenticated")
	}
}

func TestRunRejectsIssueWithoutTerminalsBeforeSetup(t *testing.T) {
	setupCalls := 0
	oldTerminal := terminal
	terminal = func(any) bool { return false }
	t.Cleanup(func() { terminal = oldTerminal })
	oldSetup := setup
	setup = func(context.Context, string, uuid.UUID) (deviceRepository, func(), error) {
		setupCalls++
		return nil, nil, nil
	}
	t.Cleanup(func() { setup = oldSetup })
	if got := runDevice(context.Background(), []string{"device", "issue", "--name", "laptop"}, strings.NewReader(""), io.Discard, io.Discard); got != 2 {
		t.Fatalf("exit code = %d", got)
	}
	if setupCalls != 0 {
		t.Fatal("setup called before terminal validation")
	}
}

func TestRunRevokeAllowsRedirectedOutput(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	repo := &fakeDevices{}
	withFakes(t, repo, func() {})
	var stdout, stderr strings.Builder
	if got := runDevice(context.Background(), []string{"device", "revoke", id.String()}, strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("exit code = %d", got)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 || repo.revokes != 1 || repo.revokedID != id {
		t.Fatal("unexpected revoke result")
	}
}

func TestServeRejectsUnsafeListenBeforeConfiguration(t *testing.T) {
	oldGetenv := getenv
	getenv = func(string) string { t.Fatal("environment read before listen validation"); return "" }
	t.Cleanup(func() { getenv = oldGetenv })
	var stderr strings.Builder
	if got := runServe(context.Background(), []string{"--listen", "0.0.0.0:8787"}, &stderr); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
	if !strings.Contains(stderr.String(), "loopback") {
		t.Fatal("unsafe listen was not rejected")
	}
}

func TestServeValidationDefaultsAndDevelopmentOverride(t *testing.T) {
	if defaultListenAddress != "127.0.0.1:8787" || !validListenAddress(defaultListenAddress, false) {
		t.Fatal("default listener is not the exact safe address")
	}
	for _, address := range []string{"", ":8787", "0.0.0.0:8787", "localhost:8787", "example.com:8787", "127.0.0.1:0"} {
		if validListenAddress(address, false) {
			t.Fatalf("unsafe address accepted: %q", address)
		}
	}
	if !validListenAddress("203.0.113.1:8787", true) {
		t.Fatal("development override rejected literal public address")
	}
}

type failingHTTPWriter struct{ header http.Header }

func (writer *failingHTTPWriter) Header() http.Header       { return writer.header }
func (writer *failingHTTPWriter) Write([]byte) (int, error) { return 0, errors.New("response content") }
func (writer *failingHTTPWriter) WriteHeader(int)           {}

type serverTestAuthenticator struct{ identity syncapi.Identity }

func (auth serverTestAuthenticator) Authenticate(context.Context, string) (syncapi.Identity, error) {
	return auth.identity, nil
}

type serverTestBackend struct{ pushes int }

func (backend *serverTestBackend) Push(_ context.Context, _ uuid.UUID, items []syncservice.Mutation) ([]syncservice.Result, error) {
	backend.pushes++
	sequence := int64(1)
	return []syncservice.Result{{MutationID: items[0].MutationID, Disposition: syncservice.DispositionAccepted, Sequence: &sequence, Version: 1}}, nil
}

func (backend *serverTestBackend) Pull(context.Context, uuid.UUID, syncservice.Cursor, int) (syncservice.PullPage, error) {
	return syncservice.PullPage{}, nil
}

func (backend *serverTestBackend) Discover(context.Context, uuid.UUID) (syncservice.Discovery, error) {
	return syncservice.Discovery{ProtocolVersion: 1, HistoryID: "123e4567-e89b-12d3-a456-426614174000", Capabilities: []syncservice.Capability{syncservice.CapabilityBootstrapDiscovery}}, nil
}

func TestServerConfigurationAndContentFreeObserver(t *testing.T) {
	var stderr strings.Builder
	server := newServer(nil, nil, &stderr)
	if server.ReadHeaderTimeout != 5*time.Second || server.ReadTimeout != 15*time.Second || server.WriteTimeout != 30*time.Second || server.IdleTimeout != 60*time.Second || server.MaxHeaderBytes != 16<<10 {
		t.Fatal("server timeouts or header limit changed")
	}
	request := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	server.Handler.ServeHTTP(&failingHTTPWriter{header: make(http.Header)}, request)
	if stderr.String() != "serve response write failed\n" || strings.Contains(stderr.String(), "response content") {
		t.Fatal("observer leaked response content")
	}
}

func TestServeWiresConfiguredRepositoryAsAuthenticatorAndSyncBackend(t *testing.T) {
	identity := syncapi.Identity{OwnerID: uuid.New(), DeviceID: uuid.New()}
	backend := &serverTestBackend{}
	server := newServer(serverTestAuthenticator{identity: identity}, backend, io.Discard)
	body, err := json.Marshal(syncapi.PushRequest{ProtocolVersion: syncapi.ProtocolVersion, Items: []syncservice.Mutation{{MutationID: uuid.NewString(), RecordID: "server-project", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "server-project"}}}})
	if err != nil {
		t.Fatal("encode request")
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/sync/push", strings.NewReader(string(body)))
	request.Header.Set("Authorization", "Bearer vgx1.123e4567-e89b-12d3-a456-426614174000.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	request.Header.Set("Accept", syncapi.MediaType)
	request.Header.Set("Content-Type", syncapi.MediaType)
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || backend.pushes != 1 {
		t.Fatalf("status/pushes = %d/%d, want 200/1", recorder.Code, backend.pushes)
	}
}

func TestRepositoryBackendMapsUnauthenticated(t *testing.T) {
	if !errors.Is(repositoryBackendError(syncpg.ErrUnauthenticated), syncapi.ErrUnauthenticated) {
		t.Fatal("repository unauthenticated error was not mapped")
	}
	sentinel := errors.New("repository sentinel")
	if !errors.Is(repositoryBackendError(sentinel), sentinel) {
		t.Fatal("ordinary repository error was not preserved")
	}
}

func TestServeRealListenerLifecycle(t *testing.T) {
	dsn := os.Getenv("VGXNESS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("VGXNESS_TEST_POSTGRES_DSN is not set")
	}
	owner := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	repository, cleanup, err := defaultSetup(context.Background(), dsn, owner)
	if err != nil {
		t.Fatal("setup failed")
	}
	defer cleanup()
	credential, err := repository.IssueDevice(context.Background(), "serve-test")
	if err != nil {
		t.Fatal("issue failed")
	}
	oldGetenv := getenv
	getenv = func(name string) string {
		if name == "VGXNESS_SYNC_POSTGRES_DSN" {
			return dsn
		}
		if name == "VGXNESS_SYNC_OWNER_ID" {
			return owner.String()
		}
		return ""
	}
	t.Cleanup(func() { getenv = oldGetenv })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("pre-open listener failed")
	}
	oldListen := listenTCP
	seenListen := make(chan string, 1)
	listenTCP = func(network, address string) (net.Listener, error) {
		seenListen <- network + " " + address
		return listener, nil
	}
	t.Cleanup(func() { listenTCP = oldListen })
	done := make(chan int, 1)
	go func() { done <- runServe(ctx, []string{"--listen", defaultListenAddress}, io.Discard) }()
	request, err := http.NewRequest(http.MethodGet, "http://"+listener.Addr().String()+"/v1/sync/capabilities", nil)
	if err != nil {
		t.Fatal("request failed")
	}
	request.Header.Set("Authorization", "Bearer "+credential.Bearer)
	request.Header.Set("Accept", syncapi.MediaType)
	client := &http.Client{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(3 * time.Second)
	for {
		response, requestErr := client.Do(request)
		if requestErr == nil {
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status = %d", response.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("listener did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := <-seenListen; got != "tcp "+defaultListenAddress {
		t.Fatalf("listen arguments = %q", got)
	}
	mutationID := uuid.NewString()
	mutation := syncservice.Mutation{MutationID: mutationID, RecordID: "serve-project-" + mutationID, RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "serve-project-" + mutationID}}
	body, err := json.Marshal(syncapi.PushRequest{ProtocolVersion: syncapi.ProtocolVersion, Items: []syncservice.Mutation{mutation}})
	if err != nil {
		t.Fatal("encode push")
	}
	push, err := http.NewRequest(http.MethodPost, "http://"+listener.Addr().String()+"/v1/sync/push", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal("push request failed")
	}
	push.Header.Set("Authorization", "Bearer "+credential.Bearer)
	push.Header.Set("Accept", syncapi.MediaType)
	push.Header.Set("Content-Type", syncapi.MediaType)
	response, err := client.Do(push)
	if err != nil || response.StatusCode != http.StatusOK {
		if response != nil {
			response.Body.Close()
		}
		t.Fatal("authenticated push failed")
	}
	response.Body.Close()
	state, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal("state connection failed")
	}
	var historyID uuid.UUID
	err = state.QueryRow(context.Background(), "SELECT history_id FROM owner_sync_state WHERE owner_id=$1", owner).Scan(&historyID)
	state.Close(context.Background())
	if err != nil {
		t.Fatal("history query failed")
	}
	pull, err := http.NewRequest(http.MethodGet, "http://"+listener.Addr().String()+"/v1/sync/pull?history_id="+historyID.String()+"&after=0", nil)
	if err != nil {
		t.Fatal("pull request failed")
	}
	pull.Header.Set("Authorization", "Bearer "+credential.Bearer)
	pull.Header.Set("Accept", syncapi.MediaType)
	response, err = client.Do(pull)
	if err != nil || response.StatusCode != http.StatusOK {
		if response != nil {
			response.Body.Close()
		}
		t.Fatal("authenticated pull failed")
	}
	pullBody, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	pullPage, decodeErr := syncapi.DecodePullResponse(pullBody)
	if readErr != nil || decodeErr != nil || len(pullPage.Changes) == 0 {
		t.Fatal("pulled change was invalid")
	}
	found := false
	for _, change := range pullPage.Changes {
		found = found || change.Mutation.MutationID == mutation.MutationID
	}
	if !found {
		t.Fatal("pushed mutation was not pulled")
	}
	if err := repository.RevokeDevice(context.Background(), credential.ID); err != nil {
		t.Fatal("revoke failed")
	}
	response, err = client.Do(request)
	if err != nil || response.StatusCode != http.StatusUnauthorized {
		if response != nil {
			response.Body.Close()
		}
		t.Fatal("revoked device was not denied")
	}
	response.Body.Close()
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("exit code = %d", code)
	}
}

func withFakes(t *testing.T, repo deviceRepository, cleanup func()) {
	t.Helper()
	oldSetup, oldTerminal, oldGetenv := setup, terminal, getenv
	setup = func(context.Context, string, uuid.UUID) (deviceRepository, func(), error) { return repo, cleanup, nil }
	terminal = func(any) bool { return true }
	getenv = func(name string) string {
		switch name {
		case "VGXNESS_SYNC_POSTGRES_DSN":
			return "postgres://example"
		case "VGXNESS_SYNC_OWNER_ID":
			return "22222222-2222-2222-2222-222222222222"
		default:
			return ""
		}
	}
	t.Cleanup(func() { setup, terminal, getenv = oldSetup, oldTerminal, oldGetenv })
}

func withEnvironment(t *testing.T, values map[string]string) {
	t.Helper()
	oldGetenv := getenv
	getenv = func(name string) string { return values[name] }
	t.Cleanup(func() { getenv = oldGetenv })
}
