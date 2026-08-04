package syncapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/vgxness/vgxness/internal/syncservice"
)

func TestHandlerLimitsGlobalAndDevice(t *testing.T) {
	identity := Identity{OwnerID: uuid.New(), DeviceID: uuid.New()}
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	handler := newHandlerWithLimits(&testAuthenticator{identity: identity}, func(context.Context) CapabilitiesResponse {
		entered <- struct{}{}
		<-release
		return CapabilitiesResponse{ProtocolVersion: ProtocolVersion}
	}, nil)
	request := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/v1/sync/capabilities", nil)
		r.Header.Set("Authorization", "Bearer "+testBearer)
		r.Header.Set("Accept", MediaType)
		return r
	}
	responses := make(chan *httptest.ResponseRecorder, 5)
	for range 4 {
		go func() { r := httptest.NewRecorder(); handler.ServeHTTP(r, request()); responses <- r }()
	}
	for range 4 {
		<-entered
	}
	rejected := httptest.NewRecorder()
	handler.ServeHTTP(rejected, request())
	if rejected.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rejected.Code)
	}
	close(release)
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for range 4 {
		select {
		case <-responses:
		case <-timer.C:
			t.Fatal("requests did not complete")
		}
	}
}

func TestRequestLimitsGlobalAndDeviceCleanup(t *testing.T) {
	limits := newRequestLimits()
	for range 64 {
		if !limits.acquireGlobal() {
			t.Fatal("global limit rejected before capacity")
		}
	}
	if limits.acquireGlobal() {
		t.Fatal("global limit did not reject saturation")
	}
	for range 64 {
		limits.releaseGlobal()
	}
	device := uuid.New()
	for range 4 {
		if !limits.acquireDevice(device) {
			t.Fatal("device limit rejected before capacity")
		}
	}
	if limits.acquireDevice(device) {
		t.Fatal("device limit did not reject saturation")
	}
	other := uuid.New()
	if !limits.acquireDevice(other) {
		t.Fatal("one device limited another device")
	}
	limits.releaseDevice(other)
	for range 4 {
		limits.releaseDevice(device)
	}
	limits.mu.Lock()
	deferred := len(limits.devices)
	limits.mu.Unlock()
	if deferred != 0 {
		t.Fatalf("device limiter entries = %d, want 0", deferred)
	}
}

const testBearer = "vgx1.123e4567-e89b-12d3-a456-426614174000.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

type testAuthenticator struct {
	mu       sync.Mutex
	identity Identity
	err      error
	calls    int
}

func (auth *testAuthenticator) Authenticate(_ context.Context, bearer string) (Identity, error) {
	auth.mu.Lock()
	defer auth.mu.Unlock()
	auth.calls++
	if bearer != testBearer {
		return Identity{}, ErrUnauthenticated
	}
	return auth.identity, auth.err
}

type zeroThenDataReader struct{ reads int }

type failingResponseWriter struct{ header http.Header }

func (writer *failingResponseWriter) Header() http.Header { return writer.header }
func (writer *failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("secret response body")
}
func (writer *failingResponseWriter) WriteHeader(int) {}

func TestResponseFailureObserverIsContentFree(t *testing.T) {
	writer := &failingResponseWriter{header: make(http.Header)}
	var status int
	var observed error
	writeJSON(writer, http.StatusOK, map[string]string{"secret": "secret response body"}, false, func(got int, err error) {
		status, observed = got, err
	})
	if status != http.StatusOK || !errors.Is(observed, errResponseWrite) || strings.Contains(observed.Error(), "secret") {
		t.Fatal("observer received unsafe failure data")
	}
}

func TestHandlerAppliesThirtySecondRequestDeadline(t *testing.T) {
	identity := Identity{OwnerID: uuid.New(), DeviceID: uuid.New()}
	seen := time.Duration(0)
	handler := newHandlerWithLimits(&testAuthenticator{identity: identity}, func(ctx context.Context) CapabilitiesResponse {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("request deadline missing")
		}
		seen = time.Until(deadline)
		return CapabilitiesResponse{ProtocolVersion: ProtocolVersion}
	}, nil)
	request := httptest.NewRequest(http.MethodGet, "/v1/sync/capabilities", nil)
	request.Header.Set("Authorization", "Bearer "+testBearer)
	request.Header.Set("Accept", MediaType)
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if seen <= 29*time.Second || seen > 30*time.Second {
		t.Fatalf("deadline = %s, want about 30s", seen)
	}
}

func (reader *zeroThenDataReader) Read(value []byte) (int, error) {
	reader.reads++
	if reader.reads == 1 {
		return 0, nil
	}
	value[0] = 'x'
	return 1, io.EOF
}

func TestCapabilitiesSuccess(t *testing.T) {
	t.Parallel()
	identity := Identity{OwnerID: uuid.MustParse("123e4567-e89b-12d3-a456-426614174001"), DeviceID: uuid.MustParse("123e4567-e89b-12d3-a456-426614174002")}
	auth := &testAuthenticator{identity: identity}
	seen := Identity{}
	handler := newHandler(auth, func(ctx context.Context) CapabilitiesResponse {
		seen, _ = IdentityFromContext(ctx)
		return CapabilitiesResponse{ProtocolVersion: ProtocolVersion, Capabilities: []string{"capabilities"}}
	})
	request := httptest.NewRequest(http.MethodGet, "/v1/sync/capabilities", nil)
	request.Header.Set("Authorization", "Bearer "+testBearer)
	request.Header.Set("Accept", MediaType)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if seen != identity {
		t.Fatalf("context identity = %#v, want %#v", seen, identity)
	}
	if got, want := recorder.Body.String(), `{"protocol_version":1,"capabilities":["capabilities"]}`+"\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	var response CapabilitiesResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if response.ProtocolVersion != ProtocolVersion || len(response.Capabilities) != 1 || response.Capabilities[0] != "capabilities" {
		t.Fatalf("decoded capabilities = %#v", response)
	}
	assertHeaders(t, recorder, false)
}

func TestCapabilitiesRejectsBeforeAcceptAndBody(t *testing.T) {
	t.Parallel()
	auth := &testAuthenticator{}
	handler := NewHandler(auth)
	request := httptest.NewRequest(http.MethodGet, "/v1/sync/capabilities", strings.NewReader("x"))
	request.Header.Set("Accept", "not-checked")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized || auth.calls != 0 {
		t.Fatalf("status/calls = %d/%d, want %d/0", recorder.Code, auth.calls, http.StatusUnauthorized)
	}
	assertError(t, recorder, ErrorUnauthorized, true)
}

func TestCapabilitiesValidation(t *testing.T) {
	t.Parallel()
	identity := Identity{OwnerID: uuid.New(), DeviceID: uuid.New()}
	cases := []struct {
		name   string
		method string
		path   string
		auth   []string
		accept []string
		body   io.Reader
		want   int
		code   ErrorCode
	}{
		{"unknown path", http.MethodGet, "/unknown", nil, nil, nil, http.StatusNotFound, ErrorInvalidInput},
		{"wrong method", http.MethodPost, "/v1/sync/capabilities", nil, nil, nil, http.StatusMethodNotAllowed, ErrorInvalidInput},
		{"missing auth", http.MethodGet, "/v1/sync/capabilities", nil, []string{MediaType}, nil, http.StatusUnauthorized, ErrorUnauthorized},
		{"duplicate auth", http.MethodGet, "/v1/sync/capabilities", []string{"Bearer " + testBearer, "Bearer " + testBearer}, []string{MediaType}, nil, http.StatusUnauthorized, ErrorUnauthorized},
		{"malformed auth", http.MethodGet, "/v1/sync/capabilities", []string{"Bearer invalid"}, []string{MediaType}, nil, http.StatusUnauthorized, ErrorUnauthorized},
		{"oversized auth", http.MethodGet, "/v1/sync/capabilities", []string{"Bearer " + testBearer + "x"}, []string{MediaType}, nil, http.StatusUnauthorized, ErrorUnauthorized},
		{"duplicate accept", http.MethodGet, "/v1/sync/capabilities", []string{"Bearer " + testBearer}, []string{MediaType, MediaType}, nil, http.StatusNotAcceptable, ErrorUnsupportedVersion},
		{"wrong accept", http.MethodGet, "/v1/sync/capabilities", []string{"Bearer " + testBearer}, []string{"application/json"}, nil, http.StatusNotAcceptable, ErrorUnsupportedVersion},
		{"body", http.MethodGet, "/v1/sync/capabilities", []string{"Bearer " + testBearer}, []string{MediaType}, bytes.NewBufferString("x"), http.StatusBadRequest, ErrorInvalidInput},
		{"zero read then body", http.MethodGet, "/v1/sync/capabilities", []string{"Bearer " + testBearer}, []string{MediaType}, &zeroThenDataReader{}, http.StatusBadRequest, ErrorInvalidInput},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			auth := &testAuthenticator{identity: identity}
			request := httptest.NewRequest(test.method, test.path, test.body)
			request.Header["Authorization"] = test.auth
			request.Header["Accept"] = test.accept
			recorder := httptest.NewRecorder()
			NewHandler(auth).ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d", recorder.Code, test.want)
			}
			if test.want == http.StatusMethodNotAllowed && recorder.Header().Get("Allow") != http.MethodGet {
				t.Fatalf("Allow = %q, want GET", recorder.Header().Get("Allow"))
			}
			assertError(t, recorder, test.code, test.want == http.StatusUnauthorized)
		})
	}
}

func TestCapabilitiesAuthenticatorFailuresAndNoSecrets(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		auth Authenticator
		want int
		code ErrorCode
	}{
		{"nil", nil, http.StatusServiceUnavailable, ErrorUnavailable},
		{"backend", &testAuthenticator{err: errors.New("database failure")}, http.StatusServiceUnavailable, ErrorUnavailable},
		{"unauthenticated", &testAuthenticator{err: ErrUnauthenticated}, http.StatusUnauthorized, ErrorUnauthorized},
		{"zero identity", &testAuthenticator{}, http.StatusUnauthorized, ErrorUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/v1/sync/capabilities", nil)
			request.Header.Set("Authorization", "Bearer "+testBearer)
			request.Header.Set("Accept", MediaType)
			recorder := httptest.NewRecorder()
			NewHandler(test.auth).ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d", recorder.Code, test.want)
			}
			if strings.Contains(recorder.Body.String(), testBearer) || strings.Contains(recorder.Body.String(), "database failure") {
				t.Fatal("response leaks a secret or backend error")
			}
			assertError(t, recorder, test.code, test.want == http.StatusUnauthorized)
		})
	}
}

func assertHeaders(t *testing.T, recorder *httptest.ResponseRecorder, authenticate bool) {
	t.Helper()
	if got := recorder.Header().Get("Content-Type"); got != MediaType {
		t.Errorf("Content-Type = %q, want %q", got, MediaType)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if authenticate && recorder.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want Bearer", recorder.Header().Get("WWW-Authenticate"))
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if response["protocol_version"] != float64(ProtocolVersion) {
		t.Errorf("protocol_version = %#v", response["protocol_version"])
	}
}

func assertError(t *testing.T, recorder *httptest.ResponseRecorder, code ErrorCode, authenticate bool) {
	t.Helper()
	assertHeaders(t, recorder, authenticate)
	if got, want := recorder.Body.String(), `{"protocol_version":1,"error":"`+string(code)+`"}`+"\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

type testSyncBackend struct {
	mu       sync.Mutex
	pushes   int
	pulls    int
	deviceID uuid.UUID
	items    []syncservice.Mutation
	cursor   syncservice.Cursor
	limit    int
	pushErr  error
	pullErr  error
	page     syncservice.PullPage
}

func (backend *testSyncBackend) Push(_ context.Context, deviceID uuid.UUID, items []syncservice.Mutation) ([]syncservice.Result, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.pushes++
	backend.deviceID, backend.items = deviceID, append([]syncservice.Mutation(nil), items...)
	if backend.pushErr != nil {
		return nil, backend.pushErr
	}
	results := make([]syncservice.Result, len(items))
	for index, item := range items {
		sequence := int64(index + 1)
		results[index] = syncservice.Result{MutationID: item.MutationID, Disposition: syncservice.DispositionAccepted, Sequence: &sequence, Version: item.BaseVersion + 1}
	}
	return results, nil
}

func (backend *testSyncBackend) Pull(_ context.Context, deviceID uuid.UUID, cursor syncservice.Cursor, limit int) (syncservice.PullPage, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.pulls++
	backend.deviceID, backend.cursor, backend.limit = deviceID, cursor, limit
	return backend.page, backend.pullErr
}

func validProjectMutation(id string) syncservice.Mutation {
	return syncservice.Mutation{MutationID: id, RecordID: "project-" + id, RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "project-" + id}}
}

func TestPushSuccessPreservesOrderAndAuthenticatedDevice(t *testing.T) {
	identity := Identity{OwnerID: uuid.New(), DeviceID: uuid.New()}
	backend := &testSyncBackend{}
	requestBody, err := json.Marshal(PushRequest{ProtocolVersion: ProtocolVersion, Items: []syncservice.Mutation{validProjectMutation(uuid.NewString()), validProjectMutation(uuid.NewString())}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/sync/push", bytes.NewReader(requestBody))
	request.Header.Set("Authorization", "Bearer "+testBearer)
	request.Header.Set("Accept", MediaType)
	request.Header.Set("Content-Type", MediaType)
	recorder := httptest.NewRecorder()
	NewSyncServerHandler(&testAuthenticator{identity: identity}, backend, nil).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || backend.pushes != 1 || backend.deviceID != identity.DeviceID || len(backend.items) != 2 {
		t.Fatalf("push result = status %d calls %d device %s items %d", recorder.Code, backend.pushes, backend.deviceID, len(backend.items))
	}
	var response PushResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || len(response.Results) != 2 || response.Results[0].MutationID != backend.items[0].MutationID || response.Results[1].MutationID != backend.items[1].MutationID {
		t.Fatalf("response = %s", recorder.Body.String())
	}
}

func TestPullQueryDefaultsWatermarkAndResponse(t *testing.T) {
	identity, history := Identity{OwnerID: uuid.New(), DeviceID: uuid.New()}, uuid.New()
	backend := &testSyncBackend{page: syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history.String(), Position: 2, Watermark: 4}, HasMore: true, Changes: []syncservice.Change{{Sequence: 1, CanonicalVersion: 1, Mutation: validProjectMutation(uuid.NewString())}, {Sequence: 2, CanonicalVersion: 1, Mutation: validProjectMutation(uuid.NewString())}}}}
	request := httptest.NewRequest(http.MethodGet, "/v1/sync/pull?history_id="+history.String()+"&after=0", nil)
	request.Header.Set("Authorization", "Bearer "+testBearer)
	request.Header.Set("Accept", MediaType)
	recorder := httptest.NewRecorder()
	NewSyncServerHandler(&testAuthenticator{identity: identity}, backend, nil).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || backend.pulls != 1 || backend.deviceID != identity.DeviceID || backend.limit != DefaultPullLimit || backend.cursor.Watermark != 0 {
		t.Fatalf("pull result = status %d calls %d device %s limit %d cursor %#v", recorder.Code, backend.pulls, backend.deviceID, backend.limit, backend.cursor)
	}
	var response PullResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.HistoryID != history.String() || response.Position != 2 || response.Watermark != 4 || !response.HasMore {
		t.Fatalf("response = %s", recorder.Body.String())
	}
}

func TestPullRejectsNoncanonicalHistoryIDWithoutBackendEffects(t *testing.T) {
	identity, history := Identity{OwnerID: uuid.New(), DeviceID: uuid.New()}, uuid.New()
	backend := &testSyncBackend{}
	request := httptest.NewRequest(http.MethodGet, "/v1/sync/pull?history_id="+strings.ToUpper(history.String())+"&after=0", nil)
	request.Header.Set("Authorization", "Bearer "+testBearer)
	request.Header.Set("Accept", MediaType)
	recorder := httptest.NewRecorder()
	NewSyncServerHandler(&testAuthenticator{identity: identity}, backend, nil).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || backend.pulls != 0 {
		t.Fatalf("status/pulls = %d/%d, want 400/0", recorder.Code, backend.pulls)
	}
}

func TestSyncEndpointsRejectInvalidRequestsWithoutBackendEffects(t *testing.T) {
	identity := Identity{OwnerID: uuid.New(), DeviceID: uuid.New()}
	backend := &testSyncBackend{}
	handler := NewSyncServerHandler(&testAuthenticator{identity: identity}, backend, nil)
	history := uuid.New().String()
	request := func(method, target string, body io.Reader) *http.Request {
		r := httptest.NewRequest(method, target, body)
		r.Header.Set("Authorization", "Bearer "+testBearer)
		r.Header.Set("Accept", MediaType)
		if method == http.MethodPost {
			r.Header.Set("Content-Type", MediaType)
		}
		return r
	}
	oversized := strings.NewReader(strings.Repeat("x", MaxBodyBytes+1))
	cases := []struct {
		name string
		r    *http.Request
		want int
	}{
		{"push wrong method", request(http.MethodGet, "/v1/sync/push", nil), http.StatusMethodNotAllowed},
		{"pull wrong method", request(http.MethodPost, "/v1/sync/pull", nil), http.StatusMethodNotAllowed},
		{"push missing auth", httptest.NewRequest(http.MethodPost, "/v1/sync/push", nil), http.StatusUnauthorized},
		{"push duplicate auth", request(http.MethodPost, "/v1/sync/push", nil), http.StatusUnauthorized},
		{"push missing accept", request(http.MethodPost, "/v1/sync/push", nil), http.StatusNotAcceptable},
		{"push duplicate accept", request(http.MethodPost, "/v1/sync/push", nil), http.StatusNotAcceptable},
		{"push missing content type", request(http.MethodPost, "/v1/sync/push", nil), http.StatusUnsupportedMediaType},
		{"push duplicate content type", request(http.MethodPost, "/v1/sync/push", nil), http.StatusUnsupportedMediaType},
		{"pull body", request(http.MethodGet, "/v1/sync/pull?history_id="+history+"&after=0", strings.NewReader("x")), http.StatusBadRequest},
		{"pull unknown query", request(http.MethodGet, "/v1/sync/pull?history_id="+history+"&after=0&unknown=1", nil), http.StatusBadRequest},
		{"pull semicolon query", request(http.MethodGet, "/v1/sync/pull?history_id="+history+"&after=0&limit=1;ignored=1", nil), http.StatusBadRequest},
		{"pull duplicate query", request(http.MethodGet, "/v1/sync/pull?history_id="+history+"&history_id="+history+"&after=0", nil), http.StatusBadRequest},
		{"pull malformed history", request(http.MethodGet, "/v1/sync/pull?history_id=bad&after=0", nil), http.StatusBadRequest},
		{"pull noncanonical history", request(http.MethodGet, "/v1/sync/pull?history_id="+strings.ToUpper(history)+"&after=0", nil), http.StatusBadRequest},
		{"pull malformed after", request(http.MethodGet, "/v1/sync/pull?history_id="+history+"&after=01", nil), http.StatusBadRequest},
		{"pull out of range after", request(http.MethodGet, "/v1/sync/pull?history_id="+history+"&after=-1", nil), http.StatusBadRequest},
		{"pull malformed watermark", request(http.MethodGet, "/v1/sync/pull?history_id="+history+"&after=1&watermark=01", nil), http.StatusBadRequest},
		{"pull out of range watermark", request(http.MethodGet, "/v1/sync/pull?history_id="+history+"&after=2&watermark=1", nil), http.StatusBadRequest},
		{"pull out of range limit", request(http.MethodGet, "/v1/sync/pull?history_id="+history+"&after=0&limit=26", nil), http.StatusBadRequest},
		{"push empty", request(http.MethodPost, "/v1/sync/push", strings.NewReader(`{"protocol_version":1,"items":[]}`)), http.StatusBadRequest},
		{"push invalid", request(http.MethodPost, "/v1/sync/push", strings.NewReader(`{"protocol_version":1,"items":[{}]}`)), http.StatusBadRequest},
		{"push oversized", request(http.MethodPost, "/v1/sync/push", oversized), http.StatusBadRequest},
	}
	cases[3].r.Header.Add("Authorization", "Bearer "+testBearer)
	cases[4].r.Header.Del("Accept")
	cases[5].r.Header.Add("Accept", MediaType)
	cases[6].r.Header.Del("Content-Type")
	cases[7].r.Header.Add("Content-Type", MediaType)
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, test.r)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d", recorder.Code, test.want)
			}
		})
	}
	if backend.pushes != 0 || backend.pulls != 0 {
		t.Fatalf("backend effects = push %d pull %d", backend.pushes, backend.pulls)
	}
}

func TestPullRejectsUnboundBackendPages(t *testing.T) {
	identity, history := Identity{OwnerID: uuid.New(), DeviceID: uuid.New()}, uuid.New()
	change := func(sequence int64) syncservice.Change {
		return syncservice.Change{Sequence: sequence, CanonicalVersion: 1, Mutation: validProjectMutation(uuid.NewString())}
	}
	for _, test := range []struct {
		name   string
		target string
		page   syncservice.PullPage
		valid  bool
	}{
		{"history mismatch", "after=0", syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: uuid.NewString()}}, false},
		{"rewind", "after=1", syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history.String()}}, false},
		{"limit exceeded", "after=0&limit=1", syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history.String(), Position: 2}, Changes: []syncservice.Change{change(1), change(2)}}, false},
		{"gap", "after=0", syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history.String(), Position: 2}, Changes: []syncservice.Change{change(2)}}, false},
		{"end mismatch", "after=0", syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history.String(), Position: 2}, Changes: []syncservice.Change{change(1)}}, false},
		{"watermark changed", "after=0&watermark=2", syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history.String(), Watermark: 3}, HasMore: true}, false},
		{"empty stalled", "after=0", syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history.String()}, HasMore: true}, false},
		{"zero watermark with changes", "after=0", syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history.String(), Position: 1}, Changes: []syncservice.Change{change(1)}}, false},
		{"empty current", "after=2", syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history.String(), Position: 2, Watermark: 2}}, true},
		{"empty initial zero watermark", "after=0", syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history.String()}}, true},
		{"contiguous established watermark", "after=0", syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history.String(), Position: 1, Watermark: 2}, HasMore: true, Changes: []syncservice.Change{change(1)}}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := &testSyncBackend{page: test.page}
			request := httptest.NewRequest(http.MethodGet, "/v1/sync/pull?history_id="+history.String()+"&"+test.target, nil)
			request.Header.Set("Authorization", "Bearer "+testBearer)
			request.Header.Set("Accept", MediaType)
			recorder := httptest.NewRecorder()
			NewSyncServerHandler(&testAuthenticator{identity: identity}, backend, nil).ServeHTTP(recorder, request)
			if test.valid {
				if recorder.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200", recorder.Code)
				}
				return
			}
			content := ""
			if len(test.page.Changes) != 0 {
				content = test.page.Changes[0].Mutation.RecordID
			}
			if recorder.Code != http.StatusServiceUnavailable || content != "" && strings.Contains(recorder.Body.String(), content) {
				t.Fatalf("unsafe status/body = %d/%q", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestSyncBackendErrorsAreSafe(t *testing.T) {
	identity := Identity{OwnerID: uuid.New(), DeviceID: uuid.New()}
	backend := &testSyncBackend{pushErr: errors.New("database secret")}
	body, _ := json.Marshal(PushRequest{ProtocolVersion: ProtocolVersion, Items: []syncservice.Mutation{validProjectMutation(uuid.NewString())}})
	request := httptest.NewRequest(http.MethodPost, "/v1/sync/push", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+testBearer)
	request.Header.Set("Accept", MediaType)
	request.Header.Set("Content-Type", MediaType)
	recorder := httptest.NewRecorder()
	NewSyncServerHandler(&testAuthenticator{identity: identity}, backend, nil).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || strings.Contains(recorder.Body.String(), "database secret") {
		t.Fatalf("unsafe backend response: %d %s", recorder.Code, recorder.Body.String())
	}
	assertError(t, recorder, ErrorUnavailable, false)

	for _, page := range []syncservice.PullPage{
		{Cursor: syncservice.Cursor{}},
		{Cursor: syncservice.Cursor{HistoryID: uuid.NewString()}, Changes: []syncservice.Change{{Sequence: 1, CanonicalVersion: 1, Mutation: syncservice.Mutation{Observation: &syncservice.Observation{Content: strings.Repeat("x", MaxPullResponseBytes)}}}}},
	} {
		backend := &testSyncBackend{page: page}
		request := httptest.NewRequest(http.MethodGet, "/v1/sync/pull?history_id="+uuid.NewString()+"&after=0", nil)
		request.Header.Set("Authorization", "Bearer "+testBearer)
		request.Header.Set("Accept", MediaType)
		recorder := httptest.NewRecorder()
		NewSyncServerHandler(&testAuthenticator{identity: identity}, backend, nil).ServeHTTP(recorder, request)
		if recorder.Code != http.StatusServiceUnavailable || strings.Contains(recorder.Body.String(), "x") {
			t.Fatalf("unsafe pull response: %d", recorder.Code)
		}
	}
}

func TestSyncBackendUnauthenticatedIsUnauthorized(t *testing.T) {
	identity := Identity{OwnerID: uuid.New(), DeviceID: uuid.New()}
	body, _ := json.Marshal(PushRequest{ProtocolVersion: ProtocolVersion, Items: []syncservice.Mutation{validProjectMutation(uuid.NewString())}})
	for _, test := range []struct {
		name    string
		request *http.Request
		backend *testSyncBackend
	}{
		{"push", httptest.NewRequest(http.MethodPost, "/v1/sync/push", bytes.NewReader(body)), &testSyncBackend{pushErr: ErrUnauthenticated}},
		{"pull", httptest.NewRequest(http.MethodGet, "/v1/sync/pull?history_id="+uuid.NewString()+"&after=0", nil), &testSyncBackend{pullErr: ErrUnauthenticated}},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.request.Header.Set("Authorization", "Bearer "+testBearer)
			test.request.Header.Set("Accept", MediaType)
			if test.request.Method == http.MethodPost {
				test.request.Header.Set("Content-Type", MediaType)
			}
			recorder := httptest.NewRecorder()
			NewSyncServerHandler(&testAuthenticator{identity: identity}, test.backend, nil).ServeHTTP(recorder, test.request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", recorder.Code)
			}
			assertError(t, recorder, ErrorUnauthorized, true)
		})
	}
}
