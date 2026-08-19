package syncadmin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/syncpg"
)

const testOperatorSecret, testAuthority = "operator-secret-that-must-not-be-reflected", "127.0.0.1:8788"

type testReader struct {
	calls atomic.Int32
	err   error
}

func (reader *testReader) AdminOverview(context.Context, syncpg.AdminPage, syncpg.AdminPage) (syncpg.AdminOverview, error) {
	reader.calls.Add(1)
	if reader.err != nil {
		return syncpg.AdminOverview{}, reader.err
	}
	return syncpg.AdminOverview{Health: syncpg.AdminHealth{Database: true}}, nil
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
func loginSession(t *testing.T, handler http.Handler) string {
	recorder := post(handler, "/login", url.Values{"secret": {testOperatorSecret}})
	assertHeaders(t, recorder.Header())
	token := sessionFromBody(t, recorder.Body.String())
	if recorder.Code != http.StatusOK || recorder.Header().Get("Location") != "" || strings.Contains(recorder.Body.String(), testOperatorSecret) || strings.Count(recorder.Body.String(), token) != 2 {
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
	if strings.Contains(get.Body.String(), stale) || strings.Contains(get.Body.String(), current) || logout.Code != http.StatusSeeOther || logout.Header().Get("Location") != "/login" || refresh.Code != http.StatusOK || refresh.Header().Get("Cache-Control") != "no-store" || strings.Count(refresh.Body.String(), current) != 2 || strings.Count(refresh.Body.String(), `scope="col"`) != 8 {
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
	requests := []*http.Request{adminRequest(http.MethodPost, "/", url.Values{"session": {"malformed"}}), adminRequest(http.MethodPost, "/", url.Values{"session": {token, token}}), adminRequest(http.MethodPost, "/?session="+url.QueryEscape(token), url.Values{}), adminRequest(http.MethodPost, "/", url.Values{"session": {strings.Repeat("x", maxFormBytes+1)}})}
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
