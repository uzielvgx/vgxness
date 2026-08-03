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
	"testing"

	"github.com/google/uuid"
)

const testBearer = "vgx1.123e4567-e89b-12d3-a456-426614174000.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

type testAuthenticator struct {
	identity Identity
	err      error
	calls    int
}

func (auth *testAuthenticator) Authenticate(_ context.Context, bearer string) (Identity, error) {
	auth.calls++
	if bearer != testBearer {
		return Identity{}, ErrUnauthenticated
	}
	return auth.identity, auth.err
}

type zeroThenDataReader struct{ reads int }

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
