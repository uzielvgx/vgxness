// Package syncclient implements bounded HTTPS calls to a sync v1 server.
package syncclient

import (
	"context"
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
)

// HTTPDoer implementations other than *http.Client must not follow redirects or
// disclose credential-bearing requests.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	endpoint *url.URL
	doer     HTTPDoer
}

func New(endpoint string, doer HTTPDoer) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") || doer == nil {
		return nil, ErrInvalidEndpoint
	}
	if client, ok := doer.(*http.Client); ok {
		if client == nil {
			return nil, ErrInvalidEndpoint
		}
		clone := *client
		clone.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		doer = &clone
	}
	return &Client{endpoint: u, doer: doer}, nil
}

func (client *Client) Discover(ctx context.Context, credential string) (syncservice.Discovery, error) {
	var value syncservice.Discovery
	if err := client.get(ctx, "/v1/sync/discovery", nil, credential, syncapi.MaxBodyBytes, &value); err != nil {
		return value, err
	}
	if err := syncservice.ValidateDiscovery(value); err != nil {
		return syncservice.Discovery{}, ErrRemote
	}
	return value, nil
}

func (client *Client) Pull(ctx context.Context, credential string, cursor syncservice.Cursor, limit int) (syncapi.PullResponse, error) {
	if err := syncservice.ValidateCursor(cursor); err != nil || limit < 1 || limit > syncapi.MaxPullLimit {
		return syncapi.PullResponse{}, ErrInvalidInput
	}
	q := url.Values{"history_id": {cursor.HistoryID}, "after": {"0"}}
	q.Set("after", strconv.FormatInt(cursor.Position, 10))
	q.Set("limit", strconv.Itoa(limit))
	if cursor.Watermark > 0 {
		q.Set("watermark", strconv.FormatInt(cursor.Watermark, 10))
	}
	var value syncapi.PullResponse
	if err := client.get(ctx, "/v1/sync/pull", q, credential, syncapi.MaxPullResponseBytes, &value); err != nil {
		return value, err
	}
	if !pullMatches(syncapi.PullRequest{ProtocolVersion: syncapi.ProtocolVersion, Cursor: cursor, Limit: limit}, value) {
		return syncapi.PullResponse{}, ErrRemote
	}
	return value, nil
}

func (client *Client) get(ctx context.Context, path string, query url.Values, credential string, limit int64, out any) error {
	if !validCredential(credential) {
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
	response, doErr := client.doer.Do(request)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if doErr != nil || response == nil || response.Body == nil {
		return ErrRemote
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(body)) > limit || len(response.Header.Values("Content-Type")) != 1 || response.Header.Get("Content-Type") != mediaType {
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
	if path == "/v1/sync/discovery" {
		value, err := syncapi.DecodeDiscoveryResponse(body)
		if err != nil {
			return ErrRemote
		}
		*(out.(*syncservice.Discovery)) = value
		return nil
	}
	value, err := syncapi.DecodeStrictPullResponse(body)
	if err != nil {
		return ErrRemote
	}
	*(out.(*syncapi.PullResponse)) = value
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
	if response.HistoryID != request.Cursor.HistoryID || response.Position < request.Cursor.Position || len(response.Changes) > request.Limit || request.Cursor.Watermark != 0 && response.Watermark != request.Cursor.Watermark {
		return false
	}
	if len(response.Changes) == 0 {
		return response.Position == request.Cursor.Position && !response.HasMore
	}
	for index, change := range response.Changes {
		if change.Sequence != request.Cursor.Position+int64(index)+1 {
			return false
		}
	}
	return response.Position == response.Changes[len(response.Changes)-1].Sequence
}
