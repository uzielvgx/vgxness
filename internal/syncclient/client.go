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

	"github.com/vgxness/vgxness/internal/syncapi"
	"github.com/vgxness/vgxness/internal/syncservice"
)

const mediaType = syncapi.MediaType

var (
	ErrInvalidEndpoint      = errors.New("sync client invalid endpoint")
	ErrInvalidInput         = errors.New("sync client invalid input")
	ErrRemote               = errors.New("sync client remote failure")
	ErrUnauthorized         = errors.New("sync client unauthorized")
	ErrUnavailable          = errors.New("sync client unavailable")
	ErrDiscoveryUnsupported = errors.New("sync client discovery unsupported")
	errNilTransportResponse = errors.New("sync client nil transport response")
)

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
	if err := client.get(ctx, "/v1/sync/discovery", nil, credential, syncapi.MaxBodyBytes, func(body []byte) error {
		decoded, err := syncapi.DecodeDiscoveryResponse(body)
		value = decoded
		return err
	}); err != nil {
		return value, err
	}
	if err := syncservice.ValidateDiscovery(value); err != nil {
		return syncservice.Discovery{}, ErrRemote
	}
	return value, nil
}

func (client *Client) Pull(ctx context.Context, credential string, cursor syncservice.Cursor, limit int) (syncapi.PullResponse, error) {
	return client.pull(ctx, credential, cursor, "", limit)
}

// PullProject retrieves sparse history for one portable project identity.
func (client *Client) PullProject(ctx context.Context, credential string, cursor syncservice.Cursor, projectID string, limit int) (syncapi.PullResponse, error) {
	if projectID == "" {
		return syncapi.PullResponse{}, ErrInvalidInput
	}
	return client.pull(ctx, credential, cursor, projectID, limit)
}

func (client *Client) pull(ctx context.Context, credential string, cursor syncservice.Cursor, projectID string, limit int) (syncapi.PullResponse, error) {
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
	if err := client.get(ctx, "/v1/sync/pull", q, credential, syncapi.MaxPullResponseBytes, func(body []byte) error {
		decoded, err := syncapi.DecodeStrictPullResponse(body)
		value = decoded
		return err
	}); err != nil {
		return value, err
	}
	if !pullMatches(request, value) {
		return syncapi.PullResponse{}, ErrRemote
	}
	return value, nil
}

// Capabilities discovers the remote protocol before sending mutations.
func (client *Client) Capabilities(ctx context.Context, credential string) (syncapi.CapabilitiesResponse, error) {
	var value syncapi.CapabilitiesResponse
	if err := client.get(ctx, "/v1/sync/capabilities", nil, credential, syncapi.MaxBodyBytes, func(body []byte) error {
		decoded, err := syncapi.DecodeCapabilitiesResponse(body)
		value = decoded
		return err
	}); err != nil {
		return value, err
	}
	if value.ProtocolVersion != syncapi.ProtocolVersion || len(value.Capabilities) == 0 || len(value.Capabilities) > 64 {
		return syncapi.CapabilitiesResponse{}, ErrRemote
	}
	seen := make(map[string]struct{}, len(value.Capabilities))
	for _, capability := range value.Capabilities {
		if capability == "" || len(capability) > 64 {
			return syncapi.CapabilitiesResponse{}, ErrRemote
		}
		if _, exists := seen[capability]; exists {
			return syncapi.CapabilitiesResponse{}, ErrRemote
		}
		seen[capability] = struct{}{}
	}
	return value, nil
}

// Push sends no more than one protocol batch and retries only one transient failure.
func (client *Client) Push(ctx context.Context, credential string, items []syncservice.Mutation) ([]syncservice.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
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
			return nil, err
		}
		results, retry, err := client.pushOnce(ctx, credential, request, body)
		if err == nil || !retry || attempt == 1 {
			return results, err
		}
	}
	return nil, ErrRemote
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
		return nil, false, err
	}
	if doErr != nil {
		if errors.Is(doErr, errNilTransportResponse) {
			return nil, false, ErrRemote
		}
		return nil, true, ErrUnavailable
	}
	if response == nil {
		return nil, false, ErrRemote
	}
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, false, ErrUnauthorized
	case http.StatusServiceUnavailable:
		return nil, true, ErrUnavailable
	default:
		return nil, false, ErrRemote
	}
	if response.Body == nil {
		return nil, false, ErrRemote
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, syncapi.MaxBodyBytes+1))
	if contextErr := contextError(ctx, err); contextErr != nil {
		return nil, false, contextErr
	}
	if err != nil {
		return nil, true, ErrUnavailable
	}
	if len(data) > syncapi.MaxBodyBytes || len(response.Header.Values("Content-Type")) != 1 || response.Header.Get("Content-Type") != mediaType {
		return nil, false, ErrRemote
	}
	reply, err := syncapi.DecodePushResponse(data)
	if err != nil || syncapi.ValidatePushResponse(push, reply) != nil {
		return nil, false, ErrRemote
	}
	return reply.Results, false, nil
}

func (client *Client) get(ctx context.Context, path string, query url.Values, credential string, limit int64, decode func([]byte) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validCredential(credential) || decode == nil {
		return ErrInvalidInput
	}
	u := *client.endpoint
	u.Path = path
	u.RawQuery = query.Encode()
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
		return err
	}
	if doErr != nil {
		if errors.Is(doErr, errNilTransportResponse) {
			return ErrRemote
		}
		return ErrUnavailable
	}
	if response == nil {
		return ErrRemote
	}
	if response.StatusCode == http.StatusNotFound && path == "/v1/sync/discovery" {
		return ErrDiscoveryUnsupported
	}
	if response.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if response.StatusCode == http.StatusServiceUnavailable {
		return ErrUnavailable
	}
	if response.StatusCode != http.StatusOK {
		return ErrRemote
	}
	if response.Body == nil {
		return ErrRemote
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if contextErr := contextError(ctx, err); contextErr != nil {
		return contextErr
	}
	if err != nil || int64(len(body)) > limit || len(response.Header.Values("Content-Type")) != 1 || response.Header.Get("Content-Type") != mediaType || decode(body) != nil {
		return ErrRemote
	}
	return nil
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
	if value == "" {
		return false
	}
	for _, character := range value {
		if character <= ' ' || character == 0x7f {
			return false
		}
	}
	return true
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
