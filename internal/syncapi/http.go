package syncapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/vgxness/vgxness/internal/syncservice"
)

var ErrUnauthenticated = errors.New("syncapi unauthenticated")
var errResponseWrite = errors.New("syncapi response write failure")

// FailureObserver receives only a fixed failure classification and response status.
// Request contents and backend errors are never passed to it.
type FailureObserver func(status int, err error)

// Identity is the non-secret identity authenticated for a sync request.
type Identity struct {
	OwnerID  uuid.UUID
	DeviceID uuid.UUID
}

// Authenticator authenticates a syntactically valid bearer and returns its
// non-secret identity.
type Authenticator interface {
	Authenticate(context.Context, string) (Identity, error)
}

// SyncBackend applies authenticated mutations and returns owner-scoped history.
type SyncBackend interface {
	Push(context.Context, uuid.UUID, []syncservice.Mutation) ([]syncservice.Result, error)
	Pull(context.Context, uuid.UUID, syncservice.Cursor, int) (syncservice.PullPage, error)
}

// CapabilitiesResponse is the v1 capabilities representation.
type CapabilitiesResponse struct {
	ProtocolVersion int      `json:"protocol_version"`
	Capabilities    []string `json:"capabilities"`
}

type identityContextKey struct{}

// IdentityFromContext returns the identity authenticated by Handler.
func IdentityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityContextKey{}).(Identity)
	return identity, ok
}

type capabilitiesFunc func(context.Context) CapabilitiesResponse

type handler struct {
	authenticator Authenticator
	backend       SyncBackend
	capabilities  capabilitiesFunc
	limits        *requestLimits
	observer      FailureObserver
}

// NewHandler returns the HTTP handler for the implemented sync v1 endpoints.
func NewHandler(authenticator Authenticator) http.Handler {
	return NewServerHandler(authenticator, nil)
}

// NewServerHandler returns a server handler with fixed non-blocking global (64)
// and per-device (4) concurrency limits.
func NewServerHandler(authenticator Authenticator, observer FailureObserver) http.Handler {
	return newHandlerWithLimits(authenticator, nil, observer)
}

// NewSyncServerHandler returns a v1 sync handler backed by the supplied service.
func NewSyncServerHandler(authenticator Authenticator, backend SyncBackend, observer FailureObserver) http.Handler {
	return newHandlerWithBackend(authenticator, nil, backend, observer)
}

func newHandler(authenticator Authenticator, capabilities capabilitiesFunc) http.Handler {
	return newHandlerWithLimits(authenticator, capabilities, nil)
}

func newHandlerWithLimits(authenticator Authenticator, capabilities capabilitiesFunc, observer FailureObserver) http.Handler {
	return newHandlerWithBackend(authenticator, capabilities, nil, observer)
}

func newHandlerWithBackend(authenticator Authenticator, capabilities capabilitiesFunc, backend SyncBackend, observer FailureObserver) http.Handler {
	if capabilities == nil {
		capabilities = func(context.Context) CapabilitiesResponse {
			return CapabilitiesResponse{ProtocolVersion: ProtocolVersion, Capabilities: []string{"capabilities"}}
		}
	}
	return &handler{authenticator: authenticator, backend: backend, capabilities: capabilities, limits: newRequestLimits(), observer: observer}
}

func (handler *handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if !handler.limits.acquireGlobal() {
		writeError(writer, http.StatusTooManyRequests, ErrorLimitExceeded, false, handler.observer)
		return
	}
	defer handler.limits.releaseGlobal()
	ctx, cancel := context.WithTimeout(request.Context(), 30*time.Second)
	defer cancel()
	request = request.WithContext(ctx)
	if request.URL.Path != "/v1/sync/capabilities" && request.URL.Path != "/v1/sync/push" && request.URL.Path != "/v1/sync/pull" {
		writeError(writer, http.StatusNotFound, ErrorInvalidInput, false, handler.observer)
		return
	}
	validMethod := request.URL.Path == "/v1/sync/capabilities" && request.Method == http.MethodGet || request.URL.Path == "/v1/sync/push" && request.Method == http.MethodPost || request.URL.Path == "/v1/sync/pull" && request.Method == http.MethodGet
	if !validMethod {
		if request.URL.Path == "/v1/sync/push" {
			writer.Header().Set("Allow", http.MethodPost)
		} else {
			writer.Header().Set("Allow", http.MethodGet)
		}
		writeError(writer, http.StatusMethodNotAllowed, ErrorInvalidInput, false, handler.observer)
		return
	}

	identity, status := handler.authenticate(request)
	if status != 0 {
		code := ErrorUnavailable
		if status == http.StatusUnauthorized {
			code = ErrorUnauthorized
		}
		writeError(writer, status, code, status == http.StatusUnauthorized, handler.observer)
		return
	}
	if !handler.limits.acquireDevice(identity.DeviceID) {
		writeError(writer, http.StatusTooManyRequests, ErrorLimitExceeded, false, handler.observer)
		return
	}
	defer handler.limits.releaseDevice(identity.DeviceID)
	if len(request.Header.Values("Accept")) != 1 || request.Header.Get("Accept") != MediaType {
		writeError(writer, http.StatusNotAcceptable, ErrorUnsupportedVersion, false, handler.observer)
		return
	}
	ctx = context.WithValue(request.Context(), identityContextKey{}, identity)
	switch request.URL.Path {
	case "/v1/sync/capabilities":
		if request.Body != nil && requestHasBody(request.Body) {
			writeError(writer, http.StatusBadRequest, ErrorInvalidInput, false, handler.observer)
			return
		}
		writeJSON(writer, http.StatusOK, handler.capabilities(ctx), false, handler.observer)
	case "/v1/sync/push":
		handler.servePush(writer, request.WithContext(ctx), identity)
	case "/v1/sync/pull":
		if request.Body != nil && requestHasBody(request.Body) {
			writeError(writer, http.StatusBadRequest, ErrorInvalidInput, false, handler.observer)
			return
		}
		handler.servePull(writer, request.WithContext(ctx), identity)
	}
}

func (handler *handler) servePush(writer http.ResponseWriter, request *http.Request, identity Identity) {
	if len(request.Header.Values("Content-Type")) != 1 || request.Header.Get("Content-Type") != MediaType {
		writeError(writer, http.StatusUnsupportedMediaType, ErrorUnsupportedVersion, false, handler.observer)
		return
	}
	if handler.backend == nil {
		writeError(writer, http.StatusServiceUnavailable, ErrorUnavailable, false, handler.observer)
		return
	}
	defer request.Body.Close()
	body, err := io.ReadAll(io.LimitReader(request.Body, MaxBodyBytes+1))
	if err != nil || len(body) > MaxBodyBytes {
		writeError(writer, http.StatusBadRequest, ErrorLimitExceeded, false, handler.observer)
		return
	}
	push, err := DecodePushRequest(body)
	if err != nil {
		writeError(writer, http.StatusBadRequest, CodeFor(err), false, handler.observer)
		return
	}
	results, err := handler.backend.Push(request.Context(), identity.DeviceID, push.Items)
	response := PushResponse{ProtocolVersion: ProtocolVersion, Results: results}
	if errors.Is(err, ErrUnauthenticated) {
		writeError(writer, http.StatusUnauthorized, ErrorUnauthorized, true, handler.observer)
		return
	}
	if err != nil || ValidatePushResponse(push, response) != nil {
		writeError(writer, http.StatusServiceUnavailable, ErrorUnavailable, false, handler.observer)
		return
	}
	writeJSON(writer, http.StatusOK, response, false, handler.observer)
}

func (handler *handler) servePull(writer http.ResponseWriter, request *http.Request, identity Identity) {
	if handler.backend == nil {
		writeError(writer, http.StatusServiceUnavailable, ErrorUnavailable, false, handler.observer)
		return
	}
	pull, err := pullQuery(request)
	if err != nil {
		writeError(writer, http.StatusBadRequest, CodeFor(err), false, handler.observer)
		return
	}
	page, err := handler.backend.Pull(request.Context(), identity.DeviceID, pull.Cursor, pull.Limit)
	if errors.Is(err, ErrUnauthenticated) {
		writeError(writer, http.StatusUnauthorized, ErrorUnauthorized, true, handler.observer)
		return
	}
	if err != nil || validatePullPage(pull, page) != nil {
		writeError(writer, http.StatusServiceUnavailable, ErrorUnavailable, false, handler.observer)
		return
	}
	response := PullResponse{ProtocolVersion: ProtocolVersion, HistoryID: page.Cursor.HistoryID, Position: page.Cursor.Position, Watermark: page.Cursor.Watermark, HasMore: page.HasMore, Changes: page.Changes}
	body, responseErr := json.Marshal(response)
	if responseErr != nil || len(body)+1 > MaxPullResponseBytes {
		writeError(writer, http.StatusServiceUnavailable, ErrorUnavailable, false, handler.observer)
		return
	}
	if _, responseErr = DecodePullResponse(body); responseErr != nil {
		writeError(writer, http.StatusServiceUnavailable, ErrorUnavailable, false, handler.observer)
		return
	}
	writeJSON(writer, http.StatusOK, response, false, handler.observer)
}

func pullQuery(request *http.Request) (PullRequest, error) {
	query, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		return PullRequest{}, ErrInvalidRequest
	}
	for key, values := range query {
		if key != "history_id" && key != "after" && key != "limit" && key != "watermark" || len(values) != 1 {
			return PullRequest{}, ErrInvalidRequest
		}
	}
	history, ok := query["history_id"]
	if !ok || len(history) != 1 {
		return PullRequest{}, ErrInvalidRequest
	}
	historyID, err := uuid.Parse(history[0])
	if err != nil || historyID == uuid.Nil || historyID.String() != history[0] {
		return PullRequest{}, syncservice.ErrInvalidCursor
	}
	after, ok := query["after"]
	if !ok || len(after) != 1 {
		return PullRequest{}, ErrInvalidRequest
	}
	position, err := canonicalInt(after[0], 0)
	if err != nil {
		return PullRequest{}, syncservice.ErrInvalidCursor
	}
	requestValue := PullRequest{ProtocolVersion: ProtocolVersion, Cursor: syncservice.Cursor{HistoryID: historyID.String(), Position: position}}
	if values, ok := query["watermark"]; ok {
		watermark, err := canonicalInt(values[0], 0)
		if err != nil {
			return PullRequest{}, syncservice.ErrInvalidCursor
		}
		requestValue.Cursor.Watermark = watermark
	}
	if values, ok := query["limit"]; ok {
		limit, err := canonicalInt(values[0], 1)
		if err != nil {
			return PullRequest{}, ErrInvalidRequest
		}
		requestValue.Limit = int(limit)
	}
	if err := ValidatePullRequest(&requestValue); err != nil {
		return PullRequest{}, err
	}
	return requestValue, nil
}

func validatePullPage(request PullRequest, page syncservice.PullPage) error {
	if page.Cursor.HistoryID != request.Cursor.HistoryID || page.Cursor.Position < request.Cursor.Position || len(page.Changes) > request.Limit || request.Cursor.Watermark != 0 && page.Cursor.Watermark != request.Cursor.Watermark {
		return ErrInvalidRequest
	}
	if page.Cursor.Watermark == 0 && (len(page.Changes) != 0 || page.Cursor.Position != 0 || page.HasMore) {
		return ErrInvalidRequest
	}
	if len(page.Changes) == 0 {
		if page.Cursor.Position != request.Cursor.Position || page.HasMore {
			return ErrInvalidRequest
		}
		return nil
	}
	expected := request.Cursor.Position + 1
	for _, change := range page.Changes {
		if change.Sequence != expected || syncservice.VerifyChangeHash(change) != nil {
			return ErrInvalidRequest
		}
		expected++
	}
	if page.Cursor.Position != page.Changes[len(page.Changes)-1].Sequence {
		return ErrInvalidRequest
	}
	return nil
}

func canonicalInt(value string, minimum int64) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < minimum || strconv.FormatInt(parsed, 10) != value {
		return 0, ErrInvalidRequest
	}
	return parsed, nil
}

func (handler *handler) authenticate(request *http.Request) (Identity, int) {
	values := request.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return Identity{}, http.StatusUnauthorized
	}
	bearer := strings.TrimPrefix(values[0], "Bearer ")
	if !validBearer(bearer) {
		return Identity{}, http.StatusUnauthorized
	}
	if handler.authenticator == nil {
		return Identity{}, http.StatusServiceUnavailable
	}
	identity, err := handler.authenticator.Authenticate(request.Context(), bearer)
	if errors.Is(err, ErrUnauthenticated) {
		return Identity{}, http.StatusUnauthorized
	}
	if err != nil {
		return Identity{}, http.StatusServiceUnavailable
	}
	if identity.OwnerID == uuid.Nil || identity.DeviceID == uuid.Nil {
		return Identity{}, http.StatusUnauthorized
	}
	return identity, 0
}

func validBearer(bearer string) bool {
	if len(bearer) != 85 {
		return false
	}
	parts := strings.Split(bearer, ".")
	if len(parts) != 3 || parts[0] != "vgx1" {
		return false
	}
	id, err := uuid.Parse(parts[1])
	if err != nil || id == uuid.Nil || id.String() != parts[1] {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[2])
	defer clearBytes(raw)
	return err == nil && len(raw) == 32 && base64.RawURLEncoding.EncodeToString(raw) == parts[2]
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func requestHasBody(body io.ReadCloser) bool {
	defer body.Close()
	var value [1]byte
	for attempts := 0; attempts < 2; attempts++ {
		count, err := body.Read(value[:])
		if count > 0 || err != nil && !errors.Is(err, io.EOF) {
			return true
		}
		if errors.Is(err, io.EOF) {
			return false
		}
	}
	return true
}

func writeError(writer http.ResponseWriter, status int, code ErrorCode, authenticate bool, observer FailureObserver) {
	writeJSON(writer, status, struct {
		ProtocolVersion int       `json:"protocol_version"`
		Error           ErrorCode `json:"error"`
	}{ProtocolVersion: ProtocolVersion, Error: code}, authenticate, observer)
}

func writeJSON(writer http.ResponseWriter, status int, value any, authenticate bool, observer FailureObserver) {
	writer.Header().Set("Content-Type", MediaType)
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if authenticate {
		writer.Header().Set("WWW-Authenticate", "Bearer")
	}
	observed := &observingWriter{ResponseWriter: writer, status: status, observer: observer}
	observed.WriteHeader(status)
	if err := json.NewEncoder(observed).Encode(value); err != nil {
		observed.fail()
	}
}

type observingWriter struct {
	http.ResponseWriter
	status   int
	observer FailureObserver
	failed   bool
}

func (writer *observingWriter) Write(value []byte) (int, error) {
	n, err := writer.ResponseWriter.Write(value)
	if err != nil {
		writer.fail()
	}
	return n, err
}
func (writer *observingWriter) fail() {
	if !writer.failed && writer.observer != nil {
		writer.failed = true
		writer.observer(writer.status, errResponseWrite)
	}
}

type requestLimits struct {
	global  chan struct{}
	mu      sync.Mutex
	devices map[string]*deviceLimit
}
type deviceLimit struct {
	semaphore chan struct{}
	active    int
}

func newRequestLimits() *requestLimits {
	return &requestLimits{global: make(chan struct{}, 64), devices: make(map[string]*deviceLimit)}
}
func (limits *requestLimits) acquireGlobal() bool {
	select {
	case limits.global <- struct{}{}:
		return true
	default:
		return false
	}
}
func (limits *requestLimits) releaseGlobal() { <-limits.global }
func (limits *requestLimits) acquireDevice(id uuid.UUID) bool {
	key := id.String()
	limits.mu.Lock()
	limit := limits.devices[key]
	if limit == nil {
		limit = &deviceLimit{semaphore: make(chan struct{}, 4)}
		limits.devices[key] = limit
	}
	select {
	case limit.semaphore <- struct{}{}:
		limit.active++
		limits.mu.Unlock()
		return true
	default:
		limits.mu.Unlock()
		return false
	}
}
func (limits *requestLimits) releaseDevice(id uuid.UUID) {
	key := id.String()
	limits.mu.Lock()
	limit := limits.devices[key]
	<-limit.semaphore
	limit.active--
	if limit.active == 0 {
		delete(limits.devices, key)
	}
	limits.mu.Unlock()
}
