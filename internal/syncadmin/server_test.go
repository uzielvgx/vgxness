package syncadmin

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/vgxness/vgxness/internal/syncpg"
)

const testOperatorSecret, testAuthority = "operator-secret-that-must-not-be-reflected", "127.0.0.1:8788"

type testReader struct {
	calls                          atomic.Int32
	err                            error
	view                           syncpg.AdminOverview
	mutex                          sync.Mutex
	issue                          syncpg.DeviceCredential
	issueErr, commitErr, revokeErr error
	issued                         []string
	revoked                        []uuid.UUID
}

func (reader *testReader) AdminOverview(context.Context, syncpg.AdminPage, syncpg.AdminPage) (syncpg.AdminOverview, error) {
	reader.calls.Add(1)
	if reader.err != nil {
		return syncpg.AdminOverview{}, reader.err
	}
	if reader.view.Health.Database {
		return reader.view, nil
	}
	return syncpg.AdminOverview{Health: syncpg.AdminHealth{Database: true}}, nil
}
func (reader *testReader) IssueDevice(_ context.Context, name string) (syncpg.DeviceCredential, error) {
	reader.mutex.Lock()
	defer reader.mutex.Unlock()
	reader.issued = append(reader.issued, name)
	return reader.issue, reader.issueErr
}
func (reader *testReader) IssueDeviceWithDelivery(_ context.Context, name string, deliver func(syncpg.DeviceCredential) error) (syncpg.DeviceCredential, error) {
	reader.mutex.Lock()
	defer reader.mutex.Unlock()
	reader.issued = append(reader.issued, name)
	if reader.issueErr != nil {
		return syncpg.DeviceCredential{}, reader.issueErr
	}
	if err := deliver(reader.issue); err != nil {
		return syncpg.DeviceCredential{}, err
	}
	return reader.issue, reader.commitErr
}
func (reader *testReader) RevokeDevice(_ context.Context, id uuid.UUID) error {
	reader.mutex.Lock()
	defer reader.mutex.Unlock()
	reader.revoked = append(reader.revoked, id)
	return reader.revokeErr
}
func adminRequest(method, target string, form url.Values) *http.Request {
	body := strings.NewReader("")
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	request := httptest.NewRequest(method, target, body)
	request.Host = testAuthority
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Origin", "http://"+testAuthority)
	}
	return request
}
func response(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
func post(handler http.Handler, target string, form url.Values) *httptest.ResponseRecorder {
	return response(handler, adminRequest(http.MethodPost, target, form))
}
func assertHeaders(t *testing.T, header http.Header) {
	t.Helper()
	if header.Get("Cache-Control") != "no-store" || header.Get("Content-Security-Policy") != "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'" || header.Get("X-Frame-Options") != "DENY" || header.Get("X-Content-Type-Options") != "nosniff" || header.Get("Referrer-Policy") != "no-referrer" || header.Get("Content-Type") != "text/html; charset=utf-8" || header.Get("Set-Cookie") != "" {
		t.Fatal("admin security headers changed")
	}
}
func newTestHandler(_ *testing.T, reader *testReader, authority string) http.Handler {
	handler, _ := New(reader, testOperatorSecret, authority)
	return handler
}
func sessionFromBody(t *testing.T, body string) string {
	const marker = `type="hidden" name="session" value="`
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatal("session hidden field missing")
	}
	value := body[start+len(marker):]
	return value[:strings.IndexByte(value, '"')]
}
func confirmationFromBody(t *testing.T, body string) string {
	t.Helper()
	const marker = `type="hidden" name="confirmation" value="`
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatal("confirmation hidden field missing")
	}
	value := body[start+len(marker):]
	return value[:strings.IndexByte(value, '"')]
}
func loginSession(t *testing.T, handler http.Handler) string {
	recorder := post(handler, "/login", url.Values{"secret": {testOperatorSecret}})
	assertHeaders(t, recorder.Header())
	token := sessionFromBody(t, recorder.Body.String())
	if recorder.Code != http.StatusOK || recorder.Header().Get("Location") != "" || strings.Contains(recorder.Body.String(), testOperatorSecret) || strings.Count(recorder.Body.String(), token) < 3 || strings.Count(recorder.Body.String(), token) != strings.Count(recorder.Body.String(), `name="session"`) {
		t.Fatal("successful login exposed credentials outside authenticated forms")
	}
	return token
}
func TestAdminGETAndUnknownRoutesNeverExposeCredentialsOrRead(t *testing.T) {
	reader, requests := &testReader{}, []*http.Request{adminRequest(http.MethodGet, "/", nil), adminRequest(http.MethodGet, "/login?secret="+url.QueryEscape(testOperatorSecret), nil), adminRequest(http.MethodGet, "/v1/admin", nil)}
	handler := newTestHandler(t, reader, testAuthority)
	for _, request := range requests {
		recorder := response(handler, request)
		assertHeaders(t, recorder.Header())
		if request.URL.Path == "/v1/admin" && recorder.Code != http.StatusNotFound || strings.Contains(recorder.Body.String(), testOperatorSecret) || strings.Contains(recorder.Header().Get("Location"), testOperatorSecret) {
			t.Fatal("GET/unknown route exposed credentials or wrong status")
		}
	}
	if reader.calls.Load() != 0 {
		t.Fatal("unauthenticated request reached read model")
	}
}
func TestAdminAcceptsLiteralAuthoritiesAndRejectsMismatch(t *testing.T) {
	for _, authority := range []string{"127.0.0.1:8788", "[::1]:8788"} {
		handler := newTestHandler(t, &testReader{}, authority)
		request := adminRequest(http.MethodPost, "/login", url.Values{"secret": {testOperatorSecret}})
		request.Host, request.Header["Origin"] = authority, []string{"http://" + authority}
		recorder := response(handler, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("literal authority/origin %q rejected", authority)
		}
	}
	handler := newTestHandler(t, &testReader{}, testAuthority)
	for _, mismatch := range []struct{ host, origin string }{{"127.0.0.1:8789", ""}, {"[::1]:8788", ""}, {testAuthority, "-"}, {testAuthority, "http://127.0.0.1:8789"}, {testAuthority, "https://" + testAuthority}} {
		request := adminRequest(http.MethodPost, "/login", url.Values{"secret": {testOperatorSecret}})
		request.Host = mismatch.host
		if mismatch.origin == "-" {
			request.Header.Del("Origin")
		} else if mismatch.origin != "" {
			request.Header.Set("Origin", mismatch.origin)
		}
		recorder := response(handler, request)
		if recorder.Code < 400 {
			t.Fatal("mismatched Host/Origin accepted")
		}
	}
	for _, authority := range []string{"127.0.0.1:08788", "127.0.0.1:+8788"} {
		if _, err := New(&testReader{}, testOperatorSecret, authority); err == nil {
			t.Fatal("noncanonical authority accepted")
		}
	}
}
func TestAdminSessionPublicationFailureRefreshAndLogout(t *testing.T) {
	reader := &testReader{}
	handler := newTestHandler(t, reader, testAuthority)
	stale := loginSession(t, handler)
	reader.err = errors.New("database unavailable")
	failed := post(handler, "/login", url.Values{"secret": {testOperatorSecret}})
	reader.err = nil
	retained := post(handler, "/", url.Values{"session": {stale}})
	if failed.Code != http.StatusServiceUnavailable || retained.Code != http.StatusOK {
		t.Fatal("failed login replaced existing session")
	}
	current := loginSession(t, handler)
	get := response(handler, adminRequest(http.MethodGet, "/login", nil))
	logout := post(handler, "/logout", url.Values{"session": {stale}})
	refresh := post(handler, "/", url.Values{"session": {current}})
	if strings.Contains(get.Body.String(), stale) || strings.Contains(get.Body.String(), current) || logout.Code != http.StatusSeeOther || logout.Header().Get("Location") != "/login" || refresh.Code != http.StatusOK || refresh.Header().Get("Cache-Control") != "no-store" || strings.Count(refresh.Body.String(), current) != 3 || strings.Count(refresh.Body.String(), `scope="col"`) != 11 {
		t.Fatal("GET, stale logout, or refresh publication was unsafe")
	}
	post(handler, "/logout", url.Values{"session": {current}})
	denied := post(handler, "/", url.Values{"session": {current}})
	if denied.Code != http.StatusUnauthorized {
		t.Fatal("current logout did not clear session")
	}
}
func TestAdminSessionInputIsBodyOnlyUniqueAndBounded(t *testing.T) {
	reader := &testReader{}
	handler := newTestHandler(t, reader, testAuthority)
	token := loginSession(t, handler)
	requests := []*http.Request{adminRequest(http.MethodPost, "/", url.Values{"session": {"malformed"}}), adminRequest(http.MethodPost, "/", url.Values{"session": {token, token}}), adminRequest(http.MethodPost, "/?session="+url.QueryEscape(token), url.Values{}), adminRequest(http.MethodPost, "/login?replace=1", url.Values{"secret": {testOperatorSecret}}), adminRequest(http.MethodPost, "/logout?session=1", url.Values{"session": {token}}), adminRequest(http.MethodPost, "/", url.Values{"session": {strings.Repeat("x", maxFormBytes+1)}})}
	for _, request := range requests {
		recorder := response(handler, request)
		if recorder.Code < 400 || recorder.Header().Get("Set-Cookie") != "" {
			t.Fatal("invalid session input accepted")
		}
	}
	sibling := adminRequest(http.MethodPost, "/", url.Values{})
	sibling.Host, sibling.Header["Origin"] = "127.0.0.1:8789", []string{"http://127.0.0.1:8789"}
	before := reader.calls.Load()
	recorder := response(handler, sibling)
	if recorder.Code < 400 || reader.calls.Load() != before {
		t.Fatal("sibling port received automatic credentials")
	}
	if post(handler, "/", url.Values{"session": {token}}).Code != http.StatusOK {
		t.Fatal("query-bearing login/logout changed the session")
	}
}

type blockingWriter struct {
	*httptest.ResponseRecorder
	started, release chan struct{}
	mode             int
}

func (writer *blockingWriter) Write(value []byte) (int, error) {
	if writer.started != nil {
		close(writer.started)
		<-writer.release
	}
	if writer.mode != 0 {
		_, _ = writer.Body.Write(value)
		if writer.mode < 0 {
			return 0, errors.New("write failed")
		}
		return len(value) - 1, nil
	}
	return writer.ResponseRecorder.Write(value)
}
func TestOverlappingLoginsPublishInResponseOrder(t *testing.T) {
	handler := newTestHandler(t, &testReader{}, testAuthority)
	first := &blockingWriter{ResponseRecorder: httptest.NewRecorder(), started: make(chan struct{}), release: make(chan struct{})}
	firstDone, secondDone := make(chan struct{}), make(chan struct{})
	go func() {
		handler.ServeHTTP(first, adminRequest(http.MethodPost, "/login", url.Values{"secret": {testOperatorSecret}}))
		close(firstDone)
	}()
	<-first.started
	second := httptest.NewRecorder()
	go func() {
		handler.ServeHTTP(second, adminRequest(http.MethodPost, "/login", url.Values{"secret": {testOperatorSecret}}))
		close(secondDone)
	}()
	select {
	case <-secondDone:
		t.Fatal("later login completed before blocked publication")
	case <-time.After(20 * time.Millisecond):
	}
	close(first.release)
	<-firstDone
	<-secondDone
}
func TestFailedLoginPublicationRestoresPriorSession(t *testing.T) {
	handler := newTestHandler(t, &testReader{}, testAuthority)
	prior := loginSession(t, handler)
	for _, mode := range []int{-1, 1} {
		failed := &blockingWriter{ResponseRecorder: httptest.NewRecorder(), mode: mode}
		handler.ServeHTTP(failed, adminRequest(http.MethodPost, "/login", url.Values{"secret": {testOperatorSecret}}))
		undelivered := sessionFromBody(t, failed.Body.String())
		if post(handler, "/", url.Values{"session": {prior}}).Code != http.StatusOK || post(handler, "/", url.Values{"session": {undelivered}}).Code != http.StatusUnauthorized {
			t.Fatal("failed publication changed active session")
		}
	}
}

func TestDeviceMutationRoutesRejectAmbiguousRequestsBeforeRepositoryCalls(t *testing.T) {
	repository := &testReader{}
	handler := newTestHandler(t, repository, testAuthority)
	session := loginSession(t, handler)
	deviceID := uuid.New().String()
	requests := []*http.Request{
		adminRequest(http.MethodGet, "/device/issue", nil),
		adminRequest(http.MethodPost, "/device/issue?name=query", url.Values{"session": {session}, "name": {"desk"}}),
		adminRequest(http.MethodPost, "/device/issue", url.Values{"session": {session}, "name": {"desk", "desk"}}),
		adminRequest(http.MethodPost, "/device/issue", url.Values{"session": {session}, "name": {"desk"}, "extra": {"no"}}),
		adminRequest(http.MethodPost, "/device/revoke/confirm?device_id="+deviceID, url.Values{"session": {session}, "device_id": {deviceID}}),
		adminRequest(http.MethodPost, "/device/revoke/confirm", url.Values{"session": {session}, "device_id": {"AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"}}),
		adminRequest(http.MethodPost, "/device/revoke", url.Values{"session": {session}, "device_id": {deviceID, deviceID}}),
		adminRequest(http.MethodPost, "/device/revoke", url.Values{"session": {session}, "device_id": {deviceID}, "confirmation": {"direct-without-confirmation"}}),
	}
	wrongType := adminRequest(http.MethodPost, "/device/revoke", url.Values{"session": {session}, "device_id": {deviceID}})
	wrongType.Header["Content-Type"] = []string{"application/x-www-form-urlencoded; charset=utf-8"}
	requests = append(requests, wrongType)
	multiOrigin := adminRequest(http.MethodPost, "/device/issue", url.Values{"session": {session}, "name": {"desk"}})
	multiOrigin.Header["Origin"] = []string{"http://" + testAuthority, "http://" + testAuthority}
	requests = append(requests, multiOrigin)
	for _, request := range requests {
		if recorder := response(handler, request); recorder.Code < 400 {
			t.Fatalf("ambiguous %s %s accepted", request.Method, request.URL.String())
		}
	}
	if len(repository.issued) != 0 || len(repository.revoked) != 0 {
		t.Fatal("invalid mutation request reached repository")
	}
}

func TestIssuePublishesBearerOnceWithNonceAndRotatesSession(t *testing.T) {
	id := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	bearer := "browser-bearer-that-must-appear-exactly-once"
	repository := &testReader{issue: syncpg.DeviceCredential{ID: id, DisplayName: "Travel laptop", Prefix: "vgx1.33333333", Bearer: bearer}, commitErr: errors.New("commit acknowledgement lost")}
	handler := newTestHandler(t, repository, testAuthority)
	prior := loginSession(t, handler)
	recorder := post(handler, "/device/issue", url.Values{"session": {prior}, "name": {"Travel laptop"}})
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || recorder.Header().Get("Cache-Control") != "no-store" || strings.Count(body, bearer) != 1 || recorder.Header().Get("Set-Cookie") != "" {
		t.Fatal("issue response did not confine one-time bearer to body")
	}
	for _, values := range recorder.Header() {
		for _, value := range values {
			if strings.Contains(value, bearer) {
				t.Fatal("bearer appeared in response header")
			}
		}
	}
	policy := recorder.Header().Get("Content-Security-Policy")
	const nonceMarker = "script-src 'nonce-"
	start := strings.Index(policy, nonceMarker)
	if start < 0 {
		t.Fatal("issue response lacks nonce-scoped script policy")
	}
	nonce := policy[start+len(nonceMarker):]
	nonce = nonce[:strings.IndexByte(nonce, '\'')]
	for _, want := range []string{`id="issued-bearer"`, `type="button"`, `aria-describedby="copy-status"`, `nonce="` + nonce + `"`, "clipboard.writeText", "shown only in this response"} {
		if !strings.Contains(body, want) {
			t.Fatalf("issue response missing %q", want)
		}
	}
	if !strings.Contains(body, "<title>VGXNESS cloud admin | Device credential</title>") || !strings.Contains(body, "Verify this device appears active on the dashboard") {
		t.Fatal("issue page did not explain ambiguous commit verification")
	}
	if strings.Contains(body, "localStorage") || strings.Contains(body, "sessionStorage") || strings.Contains(body, "document.cookie") || strings.Contains(body, "<script src=") {
		t.Fatal("issue response attempted browser persistence or external script loading")
	}
	next := sessionFromBody(t, body)
	if next == prior || post(handler, "/", url.Values{"session": {prior}}).Code != http.StatusUnauthorized || post(handler, "/", url.Values{"session": {next}}).Code != http.StatusOK {
		t.Fatal("successful issue did not rotate the session")
	}
	if len(repository.issued) != 1 || repository.issued[0] != "Travel laptop" {
		t.Fatal("issue did not use repository domain method")
	}
}

func TestConcurrentReplayCannotPassMutationAuthorization(t *testing.T) {
	id := uuid.MustParse("66666666-6666-4666-8666-666666666666")
	repository := &testReader{issue: syncpg.DeviceCredential{ID: id, DisplayName: "desk", Bearer: "serialized-bearer"}}
	admin := newTestHandler(t, repository, testAuthority)
	prior := loginSession(t, admin)
	first := &blockingWriter{ResponseRecorder: httptest.NewRecorder(), started: make(chan struct{}), release: make(chan struct{})}
	firstDone, replayDone := make(chan struct{}), make(chan struct{})
	go func() {
		admin.ServeHTTP(first, adminRequest(http.MethodPost, "/device/issue", url.Values{"session": {prior}, "name": {"desk"}}))
		close(firstDone)
	}()
	<-first.started
	replay := httptest.NewRecorder()
	go func() {
		admin.ServeHTTP(replay, adminRequest(http.MethodPost, "/device/issue", url.Values{"session": {prior}, "name": {"replay"}}))
		close(replayDone)
	}()
	select {
	case <-replayDone:
		t.Fatal("replay completed while mutation publication was blocked")
	case <-time.After(20 * time.Millisecond):
	}
	close(first.release)
	<-firstDone
	<-replayDone
	if replay.Code != http.StatusUnauthorized || len(repository.issued) != 1 || repository.issued[0] != "desk" {
		t.Fatal("prior session replay reached a second mutation")
	}
}

func TestIssueDeliveryFailureRollsBackAndRetainsPriorSession(t *testing.T) {
	id := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	repository := &testReader{issue: syncpg.DeviceCredential{ID: id, DisplayName: "desk", Bearer: "undelivered-bearer"}}
	admin := newTestHandler(t, repository, testAuthority)
	prior := loginSession(t, admin)
	concrete := admin.(*handler)
	concrete.encodePage = func(*template.Template, any) ([]byte, error) { return nil, errors.New("encode failed") }
	recorder := post(admin, "/device/issue", url.Values{"session": {prior}, "name": {"desk"}})
	if recorder.Code != http.StatusServiceUnavailable || strings.Contains(recorder.Body.String(), repository.issue.Bearer) || post(admin, "/", url.Values{"session": {prior}}).Code != http.StatusOK || len(repository.revoked) != 0 {
		t.Fatal("preparation failure changed session or called revoke")
	}
	concrete.encodePage = encode
	failed := &blockingWriter{ResponseRecorder: httptest.NewRecorder(), mode: -1}
	admin.ServeHTTP(failed, adminRequest(http.MethodPost, "/device/issue", url.Values{"session": {prior}, "name": {"desk"}}))
	if len(repository.revoked) != 0 || post(admin, "/", url.Values{"session": {prior}}).Code != http.StatusOK {
		t.Fatal("write failure changed session or called revoke")
	}
}

func TestRevokeRequiresConfirmationUsesCanonicalIDAndRotatesSession(t *testing.T) {
	id := uuid.MustParse("55555555-5555-4555-8555-555555555555")
	otherID := uuid.MustParse("77777777-7777-4777-8777-777777777777")
	repository := &testReader{view: syncpg.AdminOverview{Health: syncpg.AdminHealth{Database: true}, Devices: []syncpg.AdminDevice{{ID: id, Name: "Build host", IssuedAt: time.Now()}, {ID: otherID, Name: "Travel host", IssuedAt: time.Now()}}}}
	admin := newTestHandler(t, repository, testAuthority)
	concrete := admin.(*handler)
	prior := loginSession(t, admin)
	dashboard := post(admin, "/", url.Values{"session": {prior}}).Body.String()
	if !strings.Contains(dashboard, `action="/device/issue"`) || strings.Count(dashboard, `action="/device/revoke/confirm"`) != 1 || strings.Count(dashboard, `form="revoke-confirmation-form"`) != 2 || strings.Count(dashboard, prior) != 4 {
		t.Fatal("dashboard lacks browser device actions")
	}
	direct := post(admin, "/device/revoke", url.Values{"session": {prior}, "device_id": {id.String()}, "confirmation": {strings.Repeat("A", 43)}})
	if direct.Code < 400 || len(repository.revoked) != 0 {
		t.Fatal("direct revoke reached repository")
	}
	failedConfirm := &blockingWriter{ResponseRecorder: httptest.NewRecorder(), mode: -1}
	admin.ServeHTTP(failedConfirm, adminRequest(http.MethodPost, "/device/revoke/confirm", url.Values{"session": {prior}, "device_id": {id.String()}}))
	unpublished := confirmationFromBody(t, failedConfirm.Body.String())
	if post(admin, "/device/revoke", url.Values{"session": {prior}, "device_id": {id.String()}, "confirmation": {unpublished}}).Code < 400 || len(repository.revoked) != 0 {
		t.Fatal("incompletely published confirmation became usable")
	}
	confirm := post(admin, "/device/revoke/confirm", url.Values{"session": {prior}, "device_id": {id.String()}})
	if confirm.Code != http.StatusOK || !strings.Contains(confirm.Body.String(), "<title>VGXNESS cloud admin | Confirm revocation</title>") || !strings.Contains(confirm.Body.String(), id.String()) || !strings.Contains(confirm.Body.String(), "Confirm revocation") || len(repository.revoked) != 0 {
		t.Fatal("confirmation page omitted canonical identity or mutated early")
	}
	confirmation := confirmationFromBody(t, confirm.Body.String())
	canceled := post(admin, "/device/revoke/cancel", url.Values{"session": {prior}, "device_id": {id.String()}, "confirmation": {confirmation}})
	if canceled.Code != http.StatusOK || !strings.Contains(canceled.Body.String(), "<title>VGXNESS cloud admin | Dashboard</title>") || post(admin, "/device/revoke", url.Values{"session": {prior}, "device_id": {id.String()}, "confirmation": {confirmation}}).Code < 400 || len(repository.revoked) != 0 {
		t.Fatal("cancel did not consume confirmation and return dashboard")
	}
	confirm = post(admin, "/device/revoke/confirm", url.Values{"session": {prior}, "device_id": {id.String()}})
	confirmation = confirmationFromBody(t, confirm.Body.String())
	mismatch := post(admin, "/device/revoke", url.Values{"session": {prior}, "device_id": {otherID.String()}, "confirmation": {confirmation}})
	replayMismatch := post(admin, "/device/revoke", url.Values{"session": {prior}, "device_id": {id.String()}, "confirmation": {confirmation}})
	if mismatch.Code < 400 || replayMismatch.Code < 400 || len(repository.revoked) != 0 {
		t.Fatal("mismatched or consumed confirmation reached repository")
	}
	confirm = post(admin, "/device/revoke/confirm", url.Values{"session": {prior}, "device_id": {id.String()}})
	confirmation = confirmationFromBody(t, confirm.Body.String())
	prior = loginSession(t, admin)
	if post(admin, "/device/revoke", url.Values{"session": {prior}, "device_id": {id.String()}, "confirmation": {confirmation}}).Code < 400 || len(repository.revoked) != 0 {
		t.Fatal("confirmation survived login replacement or session rebinding")
	}
	confirm = post(admin, "/device/revoke/confirm", url.Values{"session": {prior}, "device_id": {id.String()}})
	confirmation = confirmationFromBody(t, confirm.Body.String())
	concrete.now = func() time.Time { return time.Now().Add(confirmValidity + time.Second) }
	if post(admin, "/device/revoke", url.Values{"session": {prior}, "device_id": {id.String()}, "confirmation": {confirmation}}).Code < 400 || len(repository.revoked) != 0 {
		t.Fatal("expired confirmation reached repository")
	}
	concrete.now = time.Now
	confirm = post(admin, "/device/revoke/confirm", url.Values{"session": {prior}, "device_id": {id.String()}})
	confirmation = confirmationFromBody(t, confirm.Body.String())
	originalEncoder := concrete.encodePage
	concrete.encodePage = func(page *template.Template, value any) ([]byte, error) {
		if page == revokeSuccessTemplate {
			return nil, errors.New("encode failed")
		}
		return originalEncoder(page, value)
	}
	if post(admin, "/device/revoke", url.Values{"session": {prior}, "device_id": {id.String()}, "confirmation": {confirmation}}).Code != http.StatusInternalServerError || len(repository.revoked) != 0 {
		t.Fatal("revoke mutated before success response encoding")
	}
	concrete.encodePage = originalEncoder
	confirm = post(admin, "/device/revoke/confirm", url.Values{"session": {prior}, "device_id": {id.String()}})
	confirmation = confirmationFromBody(t, confirm.Body.String())
	repository.revokeErr = errors.New("revoke result unavailable")
	recovery := post(admin, "/device/revoke", url.Values{"session": {prior}, "device_id": {id.String()}, "confirmation": {confirmation}})
	recoveryReplay := post(admin, "/device/revoke", url.Values{"session": {prior}, "device_id": {id.String()}, "confirmation": {confirmation}})
	if recovery.Code != http.StatusServiceUnavailable || recoveryReplay.Code < 400 || !strings.Contains(recovery.Body.String(), "<title>VGXNESS cloud admin | Revoke recovery</title>") || !strings.Contains(recovery.Body.String(), "may still be active or may already be revoked") || !strings.Contains(recovery.Body.String(), `action="/device/revoke/confirm"`) || !strings.Contains(recovery.Body.String(), `action="/"`) || strings.Contains(recovery.Body.String(), `href=`) || len(repository.revoked) != 1 {
		t.Fatal("revoke failure did not provide honest POST recovery")
	}
	repository.revokeErr, repository.revoked = nil, nil
	confirm = post(admin, "/device/revoke/confirm", url.Values{"session": {prior}, "device_id": {id.String()}})
	confirmation = confirmationFromBody(t, confirm.Body.String())
	result := post(admin, "/device/revoke", url.Values{"session": {prior}, "device_id": {id.String()}, "confirmation": {confirmation}})
	next := sessionFromBody(t, result.Body.String())
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), "<title>VGXNESS cloud admin | Device revoked</title>") || !strings.Contains(result.Body.String(), "Device revoked") || !strings.Contains(result.Body.String(), id.String()) || strings.Contains(result.Body.String(), `href="/"`) || len(repository.revoked) != 1 || repository.revoked[0] != id {
		t.Fatal("final revoke response was inaccurate or bypassed domain method")
	}
	if post(admin, "/", url.Values{"session": {prior}}).Code != http.StatusUnauthorized || post(admin, "/", url.Values{"session": {next}}).Code != http.StatusOK || post(admin, "/device/revoke", url.Values{"session": {next}, "device_id": {id.String()}, "confirmation": {confirmation}}).Code < 400 || len(repository.revoked) != 1 {
		t.Fatal("successful revoke did not rotate the session")
	}
}

func TestAdminViewsRenderOperationalDataAndAccessibleStructure(t *testing.T) {
	issued := time.Date(2026, time.August, 17, 10, 30, 0, 0, time.UTC)
	lastSeen := issued.Add(45 * time.Minute)
	revoked := issued.Add(2 * time.Hour)
	activeID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	revokedID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	historyID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	reader := &testReader{view: syncpg.AdminOverview{
		Health: syncpg.AdminHealth{Database: true, HistoryID: historyID, HeadSequence: 42},
		Devices: []syncpg.AdminDevice{
			{ID: activeID, Name: "Operator workstation", IssuedAt: issued, LastSeenAt: &lastSeen},
			{ID: revokedID, Name: "Retired build host", IssuedAt: issued, RevokedAt: &revoked},
		},
		AuditEvents: []syncpg.AdminAuditEvent{
			{OccurredAt: lastSeen, DeviceID: &activeID, Action: "sync.pull", Outcome: "allowed", Reason: "owner_match"},
		},
	}}
	handler := newTestHandler(t, reader, testAuthority)

	login := response(handler, adminRequest(http.MethodGet, "/login", nil)).Body.String()
	for _, want := range []string{"<title>VGXNESS cloud admin | Sign in</title>", "Local operator access", "loopback", `type="password"`, `autocomplete="current-password"`, "no session cookies", `:focus-visible`, `@media (max-width:`, `class="login-shell"`} {
		if !strings.Contains(login, want) {
			t.Fatalf("login view missing %q", want)
		}
	}
	if strings.Contains(login, "no browser storage") || strings.Contains(login, `name="session"`) || strings.Contains(login, "<script") || strings.Contains(login, "linear-gradient") {
		t.Fatal("login view contains inaccurate storage assurance, session, script, or gradient")
	}

	dashboard := post(handler, "/login", url.Values{"secret": {testOperatorSecret}}).Body.String()
	for _, want := range []string{
		"<title>VGXNESS cloud admin | Dashboard</title>", `<main id="main-content"`, "Repository online", "Head sequence", ">42<", "Active in snapshot", "Revoked in snapshot", "Trusted identities in this page", "Withdrawn identities in this page", ">1<",
		historyID.String(), "Operator workstation", "Retired build host", "Last seen", "2026-08-17 11:15 UTC", "Never observed",
		"Recent audit activity", "sync.pull", "allowed", "owner_match", activeID.String(), `scope="col"`, `aria-label="Repository status"`,
	} {
		if !strings.Contains(dashboard, want) {
			t.Fatalf("dashboard missing %q", want)
		}
	}
	if strings.Count(dashboard, `name="session"`) != 4 || strings.Count(dashboard, `type="hidden"`) != 4 || strings.Contains(dashboard, "<script") || strings.Contains(dashboard, "linear-gradient") {
		t.Fatal("dashboard session publication or asset policy changed")
	}
	unavailable, err := encode(overviewTemplate, overviewView{AdminOverview: syncpg.AdminOverview{Health: syncpg.AdminHealth{Database: false}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unavailable), "Repository unavailable") || !strings.Contains(string(unavailable), `class="metric-index">Unavailable</span>`) || strings.Contains(string(unavailable), "> Live</span>") {
		t.Fatal("unavailable repository presented as live")
	}

	errorBody := response(handler, adminRequest(http.MethodGet, "/missing", nil)).Body.String()
	for _, want := range []string{"<title>VGXNESS cloud admin | Error</title>", `aria-label="Status 404"`, `<h1 id="error-title">Not Found</h1>`, "Status 404", "Return to sign in"} {
		if !strings.Contains(errorBody, want) {
			t.Fatalf("error view missing %q", want)
		}
	}
	if strings.Contains(errorBody, "Request interrupted") {
		t.Fatal("error view contains generic heading")
	}
}
