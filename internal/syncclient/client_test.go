package syncclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/vgxness/vgxness/internal/syncapi"
	"github.com/vgxness/vgxness/internal/syncservice"
)

type closingReader struct {
	io.Reader
	closed bool
}

func (r *closingReader) Close() error { r.closed = true; return nil }

type interruptedReader struct{ remaining int }

func (r *interruptedReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.ErrUnexpectedEOF
	}
	r.remaining--
	p[0] = '{'
	return 1, nil
}

type testDoer func(*http.Request) (*http.Response, error)

func (do testDoer) RoundTrip(request *http.Request) (*http.Response, error) { return do(request) }

func TestDiscoverUsesExactAuthenticatedGET(t *testing.T) {
	const credential = "secret-credential"
	client, err := New("https://sync.example", testDoer(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Path != "/v1/sync/discovery" || request.URL.RawQuery != "" || request.Header.Get("Authorization") != "Bearer "+credential || request.Header.Get("Accept") != mediaType || request.Body != nil {
			t.Fatalf("unexpected request: %s %s headers=%v", request.Method, request.URL, request.Header)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{mediaType}}, Body: io.NopCloser(strings.NewReader(`{"protocol_version":1,"history_id":"123e4567-e89b-12d3-a456-426614174000","capabilities":["bootstrap_discovery"]}`))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Discover(context.Background(), credential); err != nil {
		t.Fatalf("discover: %v", err)
	}
}

func TestNewRejectsNonHTTPS(t *testing.T) {
	if _, err := New("http://sync.example", testDoer(func(*http.Request) (*http.Response, error) { return nil, nil })); err == nil {
		t.Fatal("accepted HTTP endpoint")
	}
}

func TestClientDoesNotFollowCredentialBearingRedirects(t *testing.T) {
	const credential = "secret-credential"
	var calls int
	var authorization string
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		authorization = request.Header.Get("Authorization")
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer target.Close()
	origin := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer origin.Close()
	client, err := New(origin.URL, origin.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Discover(context.Background(), credential)
	if !errors.Is(err, ErrRemote) || calls != 0 || authorization != "" || strings.Contains(err.Error(), credential) || strings.Contains(err.Error(), target.URL) {
		t.Fatalf("err=%v calls=%d authorization=%q", err, calls, authorization)
	}
}

func TestPullRejectsResponseForAnotherHistory(t *testing.T) {
	history := uuid.NewString()
	client, err := New("https://sync.example", testDoer(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Path != "/v1/sync/pull" || request.URL.Query().Get("history_id") != history || request.URL.Query().Get("after") != "0" || request.URL.Query().Get("limit") != "1" || request.Body != nil {
			t.Fatalf("unexpected pull request: %s", request.URL)
		}
		body := `{"protocol_version":1,"history_id":"` + uuid.NewString() + `","position":0,"has_more":false}`
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{mediaType}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Pull(context.Background(), "secret-credential", syncservice.Cursor{HistoryID: history}, 1)
	if !errors.Is(err, ErrRemote) {
		t.Fatalf("error = %v, want %v", err, ErrRemote)
	}
}

func TestPullUsesExactContinuationRequestAndClosesBody(t *testing.T) {
	history := uuid.NewString()
	change := syncservice.Change{Sequence: 3, CanonicalVersion: 1, Mutation: syncservice.Mutation{MutationID: uuid.NewString(), RecordID: "project", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "project"}}}
	change.ChangeHash, _ = syncservice.CanonicalChangeHash(change)
	body, _ := json.Marshal(syncapi.PullResponse{ProtocolVersion: 1, HistoryID: history, Position: 3, Watermark: 5, HasMore: true, Changes: []syncservice.Change{change}})
	reader := &closingReader{Reader: strings.NewReader(string(body))}
	client, _ := New("https://sync.example", testDoer(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/sync/pull" || r.URL.RawQuery != "after=2&history_id="+history+"&limit=3&watermark=5" || len(r.Header.Values("Authorization")) != 1 || len(r.Header.Values("Accept")) != 1 || r.Body != nil {
			t.Fatalf("request=%s headers=%v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{mediaType}}, Body: reader}, nil
	}))
	if page, err := client.Pull(context.Background(), "secret-credential", syncservice.Cursor{HistoryID: history, Position: 2, Watermark: 5}, 3); err != nil || page.Position != 3 || !reader.closed {
		t.Fatalf("page=%+v err=%v closed=%v", page, err, reader.closed)
	}
}

func TestClientRejectsUnsafeResponsesWithoutSecrets(t *testing.T) {
	credential, secret := "secret-credential", "server-private-value"
	for _, test := range []struct {
		name                            string
		status                          int
		header                          http.Header
		body                            string
		nilResponse, nilBody, transport bool
		want                            error
	}{
		{"404", 404, http.Header{"Content-Type": []string{mediaType}}, secret, false, false, false, ErrDiscoveryUnsupported}, {"401", 401, http.Header{"Content-Type": []string{mediaType}}, secret, false, false, false, ErrUnauthorized}, {"503", 503, http.Header{"Content-Type": []string{mediaType}}, secret, false, false, false, ErrUnavailable}, {"500", 500, http.Header{"Content-Type": []string{mediaType}}, secret, false, false, false, ErrRemote}, {"duplicate", 200, http.Header{"Content-Type": []string{mediaType, mediaType}}, secret, false, false, false, ErrRemote}, {"wrong", 200, http.Header{"Content-Type": []string{"text/plain"}}, secret, false, false, false, ErrRemote}, {"duplicate json", 200, http.Header{"Content-Type": []string{mediaType}}, `{"protocol_version":1,"protocol_version":1}`, false, false, false, ErrRemote}, {"unknown", 200, http.Header{"Content-Type": []string{mediaType}}, `{"extra":1}`, false, false, false, ErrRemote}, {"utf8", 200, http.Header{"Content-Type": []string{mediaType}}, string([]byte{0xff}), false, false, false, ErrRemote}, {"nil response", 0, nil, "", true, false, false, ErrRemote}, {"nil body", 200, http.Header{"Content-Type": []string{mediaType}}, "", false, true, false, ErrRemote}, {"transport", 200, http.Header{"Content-Type": []string{mediaType}}, secret, false, false, true, ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			var reader *closingReader
			client, _ := New("https://sync.example", testDoer(func(*http.Request) (*http.Response, error) {
				if test.nilResponse {
					return nil, nil
				}
				if test.nilBody {
					return &http.Response{StatusCode: test.status, Header: test.header}, nil
				}
				reader = &closingReader{Reader: strings.NewReader(test.body)}
				response := &http.Response{StatusCode: test.status, Header: test.header, Body: reader}
				if test.transport {
					return response, errors.New(secret)
				}
				return response, nil
			}))
			_, err := client.Discover(context.Background(), credential)
			if !errors.Is(err, test.want) || strings.Contains(err.Error(), credential) || strings.Contains(err.Error(), secret) || (reader != nil && !test.transport && !reader.closed) {
				t.Fatalf("err=%v closed=%v", err, reader != nil && reader.closed)
			}
		})
	}
}

func TestClientRejectsInvalidEndpointAndCredential(t *testing.T) {
	for _, endpoint := range []string{"http://sync.example", "https://u@sync.example", "https://sync.example?q=1", "https://sync.example#x", "https://sync.example/path", "https:opaque"} {
		if _, err := New(endpoint, testDoer(func(*http.Request) (*http.Response, error) { t.Fatal("called"); return nil, nil })); !errors.Is(err, ErrInvalidEndpoint) {
			t.Fatalf("endpoint %q: %v", endpoint, err)
		}
	}
	for _, credential := range []string{"", "a b", "a\nb"} {
		client, _ := New("https://sync.example", testDoer(func(*http.Request) (*http.Response, error) { t.Fatal("called"); return nil, nil }))
		if _, err := client.Discover(context.Background(), credential); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("credential %q: %v", credential, err)
		}
	}
}

func TestPushUsesBoundedAuthenticatedPOSTAndRejectsMismatchedResults(t *testing.T) {
	credential := "secret-credential"
	mutation := syncservice.Mutation{MutationID: uuid.NewString(), RecordID: "project", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "project"}}
	client, _ := New("https://sync.example", testDoer(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/sync/push" || request.Header.Get("Authorization") != "Bearer "+credential || request.Header.Get("Content-Type") != mediaType || request.Header.Get("Accept") != mediaType {
			t.Fatalf("request=%s headers=%v", request.URL, request.Header)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{mediaType}}, Body: io.NopCloser(strings.NewReader(`{"protocol_version":1,"results":[{"mutation_id":"other","disposition":"rejected","code":"invalid_input"}]}`))}, nil
	}))
	_, err := client.Push(context.Background(), credential, []syncservice.Mutation{mutation})
	if !errors.Is(err, ErrRemote) || strings.Contains(err.Error(), credential) {
		t.Fatalf("error=%v", err)
	}
}

func TestPushRetriesServiceUnavailableBeforeSuccessResponseValidation(t *testing.T) {
	mutation := syncservice.Mutation{MutationID: uuid.NewString(), RecordID: "project", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "project"}}
	calls := 0
	client, _ := New("https://sync.example", testDoer(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Content-Type": []string{"text/plain"}}, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
		}
		body := `{"protocol_version":1,"protocol_version":1,"results":[{"mutation_id":"` + mutation.MutationID + `","disposition":"rejected","code":"invalid_input"}]}`
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{mediaType}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	_, err := client.Push(context.Background(), "secret-credential", []syncservice.Mutation{mutation})
	if !errors.Is(err, ErrRemote) || calls != 2 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
}

func TestPushRetriesInterruptedOKResponseBodyAndClosesBothBodies(t *testing.T) {
	const credential = "secret-credential"
	mutation := syncservice.Mutation{MutationID: uuid.NewString(), RecordID: "project", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "project"}}
	var readers []*closingReader
	calls := 0
	client, _ := New("https://sync.example", testDoer(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Header.Get("Authorization") != "Bearer "+credential {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		if calls == 1 {
			reader := &closingReader{Reader: &interruptedReader{remaining: 8}}
			readers = append(readers, reader)
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{mediaType}}, Body: reader}, nil
		}
		reader := &closingReader{Reader: strings.NewReader(`{"protocol_version":1,"results":[{"mutation_id":"` + mutation.MutationID + `","disposition":"rejected","code":"invalid_input"}]}`)}
		readers = append(readers, reader)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{mediaType}}, Body: reader}, nil
	}))
	results, err := client.Push(context.Background(), credential, []syncservice.Mutation{mutation})
	if err != nil || calls != 2 || len(results) != 1 || len(readers) != 2 {
		t.Fatalf("results=%+v err=%v calls=%d bodies=%d", results, err, calls, len(readers))
	}
	if !readers[0].closed || !readers[1].closed {
		t.Fatalf("response bodies closed=%v,%v", readers[0].closed, readers[1].closed)
	}
}

func TestPushReturnsUnavailableAfterSecondInterruptedOKResponseBody(t *testing.T) {
	const credential = "secret-credential"
	mutation := syncservice.Mutation{MutationID: uuid.NewString(), RecordID: "project", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "project"}}
	var readers []*closingReader
	calls := 0
	client, _ := New("https://sync.example", testDoer(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Header.Get("Authorization") != "Bearer "+credential {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		reader := &closingReader{Reader: &interruptedReader{remaining: 8}}
		readers = append(readers, reader)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{mediaType}}, Body: reader}, nil
	}))
	_, err := client.Push(context.Background(), credential, []syncservice.Mutation{mutation})
	if !errors.Is(err, ErrUnavailable) || calls != 2 || len(readers) != 2 || strings.Contains(err.Error(), credential) {
		t.Fatalf("err=%v calls=%d bodies=%d", err, calls, len(readers))
	}
	if !readers[0].closed || !readers[1].closed {
		t.Fatalf("response bodies closed=%v,%v", readers[0].closed, readers[1].closed)
	}
}

func TestPushDoesNotRetryUnauthorized(t *testing.T) {
	mutation := syncservice.Mutation{MutationID: uuid.NewString(), RecordID: "project", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "project"}}
	calls := 0
	client, _ := New("https://sync.example", testDoer(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("unauthorized"))}, nil
	}))
	_, err := client.Push(context.Background(), "secret-credential", []syncservice.Mutation{mutation})
	if !errors.Is(err, ErrUnauthorized) || calls != 1 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
}

func TestPushDoesNotRetryNilTransportResponse(t *testing.T) {
	mutation := syncservice.Mutation{MutationID: uuid.NewString(), RecordID: "project", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "project"}}
	calls := 0
	client, _ := New("https://sync.example", testDoer(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, nil
	}))
	_, err := client.Push(context.Background(), "secret-credential", []syncservice.Mutation{mutation})
	if !errors.Is(err, ErrRemote) || calls != 1 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
}

func TestPushPreservesCancellationWithoutRetry(t *testing.T) {
	mutation := syncservice.Mutation{MutationID: uuid.NewString(), RecordID: "project", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "project"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	client, _ := New("https://sync.example", testDoer(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, context.Canceled
	}))
	_, err := client.Push(ctx, "secret-credential", []syncservice.Mutation{mutation})
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
}

func TestGetPreservesCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	client, _ := New("https://sync.example", testDoer(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, context.Canceled
	}))
	if _, err := client.Discover(ctx, "secret-credential"); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
}

func TestCapabilitiesRejectsInvalidUTF8(t *testing.T) {
	client, _ := New("https://sync.example", testDoer(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{mediaType}}, Body: io.NopCloser(strings.NewReader("{\"protocol_version\":1,\"capabilities\":[\"\xff\"]}"))}, nil
	}))
	if _, err := client.Capabilities(context.Background(), "secret-credential"); !errors.Is(err, ErrRemote) {
		t.Fatalf("error=%v", err)
	}
}

func TestPullAcceptsResponseUpToPullLimit(t *testing.T) {
	history := uuid.NewString()
	body := `{"protocol_version":1,"history_id":"` + history + `","position":0,"has_more":false}` + strings.Repeat(" ", syncapi.MaxBodyBytes)
	client, _ := New("https://sync.example", testDoer(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{mediaType}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	if _, err := client.Pull(context.Background(), "secret-credential", syncservice.Cursor{HistoryID: history}, 1); err != nil {
		t.Fatalf("pull response under limit rejected: %v", err)
	}
}

func TestGetRejectsNilDecoderWithoutPanic(t *testing.T) {
	client, _ := New("https://sync.example", testDoer(func(*http.Request) (*http.Response, error) {
		t.Fatal("request sent with nil decoder")
		return nil, nil
	}))
	if err := client.get(context.Background(), "/v1/sync/capabilities", nil, "secret-credential", syncapi.MaxBodyBytes, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error=%v", err)
	}
}
