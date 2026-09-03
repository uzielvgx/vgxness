// Package syncclient implements bounded HTTPS calls to a sync v1 server.
package syncclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/google/uuid"

	"github.com/vgxness/vgxness/internal/syncapi"
	"github.com/vgxness/vgxness/internal/syncservice"
)

const mediaType = syncapi.MediaType

var (
	ErrInvalidEndpoint         = errors.New("sync client invalid endpoint")
	ErrInvalidInput            = errors.New("sync client invalid input")
	ErrRemote                  = errors.New("sync client remote failure")
	ErrUnauthorized            = errors.New("sync client unauthorized")
	ErrUnavailable             = errors.New("sync client unavailable")
	ErrDiscoveryUnsupported    = errors.New("sync client discovery unsupported")
	ErrProjectStateUnsupported = errors.New("sync client project state unsupported")
	errNilTransportResponse    = errors.New("sync client nil transport response")
)

// Operation is a stable, allowlisted sync capability identifier.
type Operation string

const (
	OperationCapabilities     Operation = "capabilities"
	OperationPush             Operation = "push"
	OperationProjectDiscovery Operation = "project_discovery"
	OperationPull             Operation = "pull"
	OperationProjectPull      Operation = "project_pull"
	OperationProjectState     Operation = "project_state"
)

// ErrorClass is a stable, allowlisted category that never includes remote data.
type ErrorClass string

const (
	ErrorClassTransport       ErrorClass = "transport"
	ErrorClassHTTPStatus      ErrorClass = "http_status"
	ErrorClassResponseInvalid ErrorClass = "response_invalid"
	ErrorClassAuthentication  ErrorClass = "authentication"
	ErrorClassContext         ErrorClass = "context"
)

// Diagnostic contains only stable, privacy-safe client failure metadata.
type Diagnostic struct {
	Operation  Operation         `json:"operation"`
	HTTPStatus int               `json:"httpStatus,omitempty"`
	Class      ErrorClass        `json:"class"`
	Code       syncapi.ErrorCode `json:"code,omitempty"`
}

// diagnosticError is private so its state cannot be forged outside this package.
type diagnosticError struct {
	operation Operation
	status    int
	class     ErrorClass
	code      syncapi.ErrorCode
	cause     error
}

func (err *diagnosticError) Error() string {
	if err.status != 0 {
		return "sync client " + string(err.operation) + " " + string(err.class) + " status=" + strconv.Itoa(err.status)
	}
	return "sync client " + string(err.operation) + " " + string(err.class)
}
func (err *diagnosticError) Unwrap() error { return err.cause }

// NewDiagnosticError creates a sanitized error that preserves an allowlisted sentinel.
func NewDiagnosticError(operation Operation, class ErrorClass, status int, cause error) error {
	canonical := canonicalCause(cause)
	if !validOperation(operation) || !validErrorClass(class) || status < 0 || status > 999 || canonical == nil {
		return canonical
	}
	return &diagnosticError{operation: operation, class: class, status: status, cause: canonical}
}

// DiagnosticFrom extracts a validated copy of sanitized metadata from an error chain.
func DiagnosticFrom(err error) (Diagnostic, bool) {
	var diagnostic *diagnosticError
	if !errors.As(err, &diagnostic) || diagnostic == nil || !validOperation(diagnostic.operation) || !validErrorClass(diagnostic.class) || diagnostic.status < 0 || diagnostic.status > 999 || diagnostic.code != "" && !validRemoteCode(diagnostic.code) || canonicalCause(diagnostic.cause) == nil {
		return Diagnostic{}, false
	}
	return Diagnostic{Operation: diagnostic.operation, Class: diagnostic.class, HTTPStatus: diagnostic.status, Code: diagnostic.code}, true
}

func canonicalCause(cause error) error {
	switch {
	case errors.Is(cause, context.Canceled):
		return context.Canceled
	case errors.Is(cause, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(cause, ErrUnauthorized):
		return ErrUnauthorized
	case errors.Is(cause, ErrUnavailable):
		return ErrUnavailable
	case errors.Is(cause, ErrDiscoveryUnsupported):
		return ErrDiscoveryUnsupported
	case errors.Is(cause, ErrProjectStateUnsupported):
		return ErrProjectStateUnsupported
	case errors.Is(cause, ErrRemote):
		return ErrRemote
	default:
		return nil
	}
}

func validOperation(operation Operation) bool {
	switch operation {
	case OperationCapabilities, OperationPush, OperationProjectDiscovery, OperationPull, OperationProjectPull, OperationProjectState:
		return true
	default:
		return false
	}
}

func validErrorClass(class ErrorClass) bool {
	switch class {
	case ErrorClassTransport, ErrorClassHTTPStatus, ErrorClassResponseInvalid, ErrorClassAuthentication, ErrorClassContext:
		return true
	default:
		return false
	}
}

type Client struct {
	endpoint   *url.URL
	httpClient *http.Client
}

type responseCheckingTransport struct {
	base http.RoundTripper
}

func (transport responseCheckingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if response == nil && err == nil {
		return nil, errNilTransportResponse
	}
	return response, err
}

// New creates a client that never follows credential-bearing redirects.
func New(endpoint string, transport http.RoundTripper) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") || transport == nil {
		return nil, ErrInvalidEndpoint
	}
	return &Client{endpoint: u, httpClient: &http.Client{Transport: responseCheckingTransport{base: transport}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (client *Client) Discover(ctx context.Context, credential string) (syncservice.Discovery, error) {
	var value syncservice.Discovery
	if err := client.get(ctx, OperationProjectDiscovery, "/v1/sync/discovery", nil, credential, syncapi.MaxBodyBytes, func(body []byte) error {
		decoded, err := syncapi.DecodeDiscoveryResponse(body)
		value = decoded
		return err
	}); err != nil {
		return value, err
	}
	if err := syncservice.ValidateDiscovery(value); err != nil {
		return syncservice.Discovery{}, NewDiagnosticError(OperationProjectDiscovery, ErrorClassResponseInvalid, 0, ErrRemote)
	}
	return value, nil
}

func (client *Client) Pull(ctx context.Context, credential string, cursor syncservice.Cursor, limit int) (syncapi.PullResponse, error) {
	return client.pull(ctx, credential, cursor, "", limit, OperationPull)
}

// PullProject retrieves sparse history for one portable project identity.
func (client *Client) PullProject(ctx context.Context, credential string, cursor syncservice.Cursor, projectID string, limit int) (syncapi.PullResponse, error) {
	if projectID == "" {
		return syncapi.PullResponse{}, ErrInvalidInput
	}
	return client.pull(ctx, credential, cursor, projectID, limit, OperationProjectPull)
}

func (client *Client) pull(ctx context.Context, credential string, cursor syncservice.Cursor, projectID string, limit int, operation Operation) (syncapi.PullResponse, error) {
	if err := syncservice.ValidateCursor(cursor); err != nil || limit < 1 || limit > syncapi.MaxPullLimit {
		return syncapi.PullResponse{}, ErrInvalidInput
	}
	request := syncapi.PullRequest{ProtocolVersion: syncapi.ProtocolVersion, Cursor: cursor, ProjectID: projectID, Limit: limit}
	if syncapi.ValidatePullRequest(&request) != nil {
		return syncapi.PullResponse{}, ErrInvalidInput
	}
	q := url.Values{"history_id": {cursor.HistoryID}, "after": {"0"}}
	q.Set("after", strconv.FormatInt(cursor.Position, 10))
	q.Set("limit", strconv.Itoa(limit))
	if cursor.Watermark > 0 {
		q.Set("watermark", strconv.FormatInt(cursor.Watermark, 10))
	}
	if projectID != "" {
		q.Set("project_id", projectID)
	}
	var value syncapi.PullResponse
	if err := client.get(ctx, operation, "/v1/sync/pull", q, credential, syncapi.MaxPullResponseBytes, func(body []byte) error {
		decoded, err := syncapi.DecodeStrictPullResponse(body)
		value = decoded
		return err
	}); err != nil {
		return value, err
	}
	if !pullMatches(request, value) {
		return syncapi.PullResponse{}, NewDiagnosticError(operation, ErrorClassResponseInvalid, 0, ErrRemote)
	}
	return value, nil
}

// Capabilities discovers the remote protocol before sending mutations.
func (client *Client) Capabilities(ctx context.Context, credential string) (syncapi.CapabilitiesResponse, error) {
	var value syncapi.CapabilitiesResponse
	if err := client.get(ctx, OperationCapabilities, "/v1/sync/capabilities", nil, credential, syncapi.MaxBodyBytes, func(body []byte) error {
		decoded, err := syncapi.DecodeCapabilitiesResponse(body)
		value = decoded
		return err
	}); err != nil {
		return value, err
	}
	if value.ProtocolVersion != syncapi.ProtocolVersion || len(value.Capabilities) == 0 || len(value.Capabilities) > 64 {
		return syncapi.CapabilitiesResponse{}, NewDiagnosticError(OperationCapabilities, ErrorClassResponseInvalid, 0, ErrRemote)
	}
	seen := make(map[string]struct{}, len(value.Capabilities))
	for _, capability := range value.Capabilities {
		if capability == "" || len(capability) > 64 {
			return syncapi.CapabilitiesResponse{}, NewDiagnosticError(OperationCapabilities, ErrorClassResponseInvalid, 0, ErrRemote)
		}
		if _, exists := seen[capability]; exists {
			return syncapi.CapabilitiesResponse{}, NewDiagnosticError(OperationCapabilities, ErrorClassResponseInvalid, 0, ErrRemote)
		}
		seen[capability] = struct{}{}
	}
	return value, nil
}

// ProjectState retrieves authenticated state for exactly one portable project.
// It fails closed when the server did not negotiate the capability.
func (client *Client) ProjectState(ctx context.Context, credential, projectID string) (syncapi.ProjectStateResponse, error) {
	if !validProjectID(projectID) || !validCredential(credential) {
		return syncapi.ProjectStateResponse{}, ErrInvalidInput
	}
	capabilities, err := client.Capabilities(ctx, credential)
	if err != nil {
		return syncapi.ProjectStateResponse{}, err
	}
	if !hasCapability(capabilities, string(syncservice.CapabilityProjectState)) {
		return syncapi.ProjectStateResponse{}, NewDiagnosticError(OperationProjectState, ErrorClassResponseInvalid, 0, ErrProjectStateUnsupported)
	}
	var value syncapi.ProjectStateResponse
	if err := client.get(ctx, OperationProjectState, "/v1/sync/projects/"+projectID+"/state", nil, credential, syncapi.MaxBodyBytes, func(body []byte) error {
		decoded, err := syncapi.DecodeProjectStateResponse(body)
		value = decoded
		return err
	}); err != nil {
		return value, err
	}
	if syncservice.ValidateProjectState(value) != nil {
		return syncapi.ProjectStateResponse{}, NewDiagnosticError(OperationProjectState, ErrorClassResponseInvalid, 0, ErrRemote)
	}
	return value, nil
}

func hasCapability(value syncapi.CapabilitiesResponse, want string) bool {
	for _, capability := range value.Capabilities {
		if capability == want {
			return true
		}
	}
	return false
}

// Push sends no more than one protocol batch and retries only one transient failure.
func (client *Client) Push(ctx context.Context, credential string, items []syncservice.Mutation) ([]syncservice.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, NewDiagnosticError(OperationPush, ErrorClassContext, 0, err)
	}
	request := syncapi.PushRequest{ProtocolVersion: syncapi.ProtocolVersion, Items: items}
	if !validCredential(credential) || syncapi.ValidatePushRequest(request) != nil {
		return nil, ErrInvalidInput
	}
	body, err := json.Marshal(request)
	if err != nil || len(body) > syncapi.MaxBodyBytes {
		return nil, ErrInvalidInput
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, NewDiagnosticError(OperationPush, ErrorClassContext, 0, err)
		}
		results, retry, err := client.pushOnce(ctx, credential, request, body)
		if err == nil || !retry || attempt == 1 {
			return results, err
		}
	}
	return nil, NewDiagnosticError(OperationPush, ErrorClassResponseInvalid, 0, ErrRemote)
}

func (client *Client) pushOnce(ctx context.Context, credential string, push syncapi.PushRequest, body []byte) ([]syncservice.Result, bool, error) {
	u := *client.endpoint
	u.Path = "/v1/sync/push"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, false, ErrInvalidEndpoint
	}
	request.Header.Set("Accept", mediaType)
	request.Header.Set("Content-Type", mediaType)
	request.Header.Set("Authorization", "Bearer "+credential)
	response, doErr := client.httpClient.Do(request)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err := contextError(ctx, doErr); err != nil {
		return nil, false, NewDiagnosticError(OperationPush, ErrorClassContext, 0, err)
	}
	if doErr != nil {
		if errors.Is(doErr, errNilTransportResponse) {
			return nil, false, NewDiagnosticError(OperationPush, ErrorClassResponseInvalid, 0, ErrRemote)
		}
		return nil, true, NewDiagnosticError(OperationPush, ErrorClassTransport, 0, ErrUnavailable)
	}
	if response == nil {
		return nil, false, NewDiagnosticError(OperationPush, ErrorClassResponseInvalid, 0, ErrRemote)
	}
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, false, newRemoteHTTPError(OperationPush, response, ErrorClassAuthentication, ErrUnauthorized)
	case http.StatusServiceUnavailable:
		return nil, true, newRemoteHTTPError(OperationPush, response, ErrorClassHTTPStatus, ErrUnavailable)
	default:
		return nil, false, newRemoteHTTPError(OperationPush, response, ErrorClassHTTPStatus, ErrRemote)
	}
	if response.Body == nil {
		return nil, false, NewDiagnosticError(OperationPush, ErrorClassResponseInvalid, response.StatusCode, ErrRemote)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, syncapi.MaxBodyBytes+1))
	if contextErr := contextError(ctx, err); contextErr != nil {
		return nil, false, NewDiagnosticError(OperationPush, ErrorClassContext, response.StatusCode, contextErr)
	}
	if err != nil {
		return nil, true, NewDiagnosticError(OperationPush, ErrorClassTransport, response.StatusCode, ErrUnavailable)
	}
	if len(data) > syncapi.MaxBodyBytes || len(response.Header.Values("Content-Type")) != 1 || response.Header.Get("Content-Type") != mediaType {
		return nil, false, NewDiagnosticError(OperationPush, ErrorClassResponseInvalid, response.StatusCode, ErrRemote)
	}
	reply, err := syncapi.DecodePushResponse(data)
	if err != nil || syncapi.ValidatePushResponse(push, reply) != nil {
		return nil, false, NewDiagnosticError(OperationPush, ErrorClassResponseInvalid, response.StatusCode, ErrRemote)
	}
	return reply.Results, false, nil
}

func (client *Client) get(ctx context.Context, operation Operation, path string, query url.Values, credential string, limit int64, decode func([]byte) error) error {
	if err := ctx.Err(); err != nil {
		return NewDiagnosticError(operation, ErrorClassContext, 0, err)
	}
	if !validCredential(credential) || decode == nil {
		return ErrInvalidInput
	}
	u := *client.endpoint
	u.Path, u.RawQuery = path, query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return ErrInvalidEndpoint
	}
	request.Header.Set("Accept", mediaType)
	request.Header.Set("Authorization", "Bearer "+credential)
	response, doErr := client.httpClient.Do(request)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err := contextError(ctx, doErr); err != nil {
		return NewDiagnosticError(operation, ErrorClassContext, 0, err)
	}
	if doErr != nil {
		if errors.Is(doErr, errNilTransportResponse) {
			return NewDiagnosticError(operation, ErrorClassResponseInvalid, 0, ErrRemote)
		}
		return NewDiagnosticError(operation, ErrorClassTransport, 0, ErrUnavailable)
	}
	if response == nil {
		return NewDiagnosticError(operation, ErrorClassResponseInvalid, 0, ErrRemote)
	}
	if response.StatusCode == http.StatusNotFound && path == "/v1/sync/discovery" {
		return newRemoteHTTPError(operation, response, ErrorClassHTTPStatus, ErrDiscoveryUnsupported)
	}
	if response.StatusCode == http.StatusUnauthorized {
		return newRemoteHTTPError(operation, response, ErrorClassAuthentication, ErrUnauthorized)
	}
	if response.StatusCode == http.StatusServiceUnavailable {
		return newRemoteHTTPError(operation, response, ErrorClassHTTPStatus, ErrUnavailable)
	}
	if response.StatusCode != http.StatusOK {
		return newRemoteHTTPError(operation, response, ErrorClassHTTPStatus, ErrRemote)
	}
	if response.Body == nil {
		return NewDiagnosticError(operation, ErrorClassResponseInvalid, response.StatusCode, ErrRemote)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if contextErr := contextError(ctx, err); contextErr != nil {
		return NewDiagnosticError(operation, ErrorClassContext, response.StatusCode, contextErr)
	}
	if err != nil {
		return NewDiagnosticError(operation, ErrorClassTransport, response.StatusCode, ErrRemote)
	}
	if int64(len(body)) > limit || len(response.Header.Values("Content-Type")) != 1 || response.Header.Get("Content-Type") != mediaType || decode(body) != nil {
		return NewDiagnosticError(operation, ErrorClassResponseInvalid, response.StatusCode, ErrRemote)
	}
	return nil
}

func newRemoteHTTPError(operation Operation, response *http.Response, class ErrorClass, cause error) error {
	err := NewDiagnosticError(operation, class, response.StatusCode, cause)
	if response.Body == nil || len(response.Header.Values("Content-Type")) != 1 || response.Header.Get("Content-Type") != mediaType {
		return err
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, syncapi.MaxBodyBytes+1))
	if readErr != nil || len(body) > syncapi.MaxBodyBytes {
		return err
	}
	var payload struct {
		ProtocolVersion int               `json:"protocol_version"`
		Error           syncapi.ErrorCode `json:"error"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || decoder.Decode(&struct{}{}) != io.EOF || payload.ProtocolVersion != syncapi.ProtocolVersion || !validRemoteCode(payload.Error) {
		return err
	}
	return &diagnosticError{operation: operation, class: class, status: response.StatusCode, code: payload.Error, cause: canonicalCause(cause)}
}

func validRemoteCode(code syncapi.ErrorCode) bool {
	switch code {
	case syncapi.ErrorInvalidInput, syncapi.ErrorLimitExceeded, syncapi.ErrorUnsupportedVersion, syncapi.ErrorUnsupportedSemantic, syncapi.ErrorUnavailable, syncapi.ErrorUnauthorized, syncapi.ErrorRevoked, syncapi.ErrorConflict, syncapi.ErrorHistory, syncapi.ErrorCursor:
		return true
	default:
		return false
	}
}

func contextError(ctx context.Context, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return nil
}

func validCredential(value string) bool {
	_, ok := syncapi.ParseBearer(value)
	return ok
}

func validProjectID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value && id.Variant() == uuid.RFC4122 && id.Version() >= 1 && id.Version() <= 5
}

func pullMatches(request syncapi.PullRequest, response syncapi.PullResponse) bool {
	if response.HistoryID != request.Cursor.HistoryID || response.ProjectID != request.ProjectID || response.Position < request.Cursor.Position || len(response.Changes) > request.Limit || request.Cursor.Watermark != 0 && response.Watermark != request.Cursor.Watermark {
		return false
	}
	if len(response.Changes) == 0 {
		return !response.HasMore && (request.ProjectID == "" && response.Position == request.Cursor.Position || request.ProjectID != "" && response.Position == response.Watermark)
	}
	previous := request.Cursor.Position
	for index, change := range response.Changes {
		if request.ProjectID == "" && change.Sequence != request.Cursor.Position+int64(index)+1 || request.ProjectID != "" && (change.Sequence <= previous || syncapi.ValidateProjectPullChange(change, request.ProjectID) != nil) {
			return false
		}
		previous = change.Sequence
	}
	last := response.Changes[len(response.Changes)-1].Sequence
	return request.ProjectID == "" && response.Position == last || request.ProjectID != "" && response.HasMore && response.Position == last || request.ProjectID != "" && !response.HasMore && response.Position == response.Watermark
}
