package pi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/modelplan"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.b.Bytes()...)
}

type notifyingReader struct {
	r      io.Reader
	before chan<- struct{}
}

func (r notifyingReader) Read(p []byte) (int, error) {
	r.before <- struct{}{}
	return r.r.Read(p)
}

type failAfterWriter struct {
	writes int
}

func (w *failAfterWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes > 1 {
		return 0, errors.New("output failed")
	}
	return len(p), nil
}

func testWorkspace(t *testing.T) string {
	t.Helper()
	workspace, _, err := CanonicalWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}

func testRecord(t *testing.T, request Request) []byte {
	t.Helper()
	record, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	return append(record, '\n')
}

func TestServerHandshakeBindsAndDispatchesOnce(t *testing.T) {
	workspace := testWorkspace(t)
	called := 0
	completed := make(chan struct{})
	server, err := NewServer(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}, 4, func(_ context.Context, request Request) (any, error) {
		called++
		return map[string]string{"operation": request.Operation}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	hello, err := json.Marshal(serverHello(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}))
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	defer writer.Close()
	var output lockedBuffer
	var resultOnce sync.Once
	outputWriter := writerFunc(func(record []byte) (int, error) {
		n, err := output.Write(record)
		if err == nil && n == len(record) && bytes.Contains(record, []byte(`"id":"one"`)) && bytes.Contains(record, []byte(`"type":"result"`)) {
			resultOnce.Do(func() { close(completed) })
		}
		return n, err
	})
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), reader, outputWriter) }()
	if _, err := writer.Write(append(hello, '\n')); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(testRecord(t, Request{Type: "request", ID: "one", Operation: "memory.recall", Workspace: workspace, Mode: ReadOnly, Role: "general", Payload: json.RawMessage(`{}`)})); err != nil {
		t.Fatal(err)
	}
	select {
	case <-completed:
	case err := <-done:
		t.Fatalf("server returned before result: %v", err)
	case <-time.After(time.Second):
		t.Fatal("request did not complete")
	}
	_ = writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if called != 1 || !bytes.Contains(output.Bytes(), []byte(`"type":"result"`)) {
		t.Fatalf("called=%d output=%s", called, output.Bytes())
	}
}

func TestServerRejectsHandshakeBeforeDispatch(t *testing.T) {
	workspace := testWorkspace(t)
	called := 0
	server, err := NewServer(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}, 4, func(context.Context, Request) (any, error) { called++; return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	hello, err := json.Marshal(serverHello(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}))
	if err != nil {
		t.Fatal(err)
	}
	hello = bytes.Replace(hello, []byte(`"vgxness-pi/v1"`), []byte(`"wrong"`), 1)
	input := bytes.NewBuffer(append(hello, '\n'))
	if err := server.Serve(context.Background(), input, io.Discard); err == nil || called != 0 {
		t.Fatalf("err=%v called=%d", err, called)
	}
}

func TestServerMalformedRecordClosesBeforeLaterDispatch(t *testing.T) {
	for _, malformed := range []string{`{`, `{"type":"request","id":"x","id":"y"}`, `{"type":"unknown","id":"x"}`} {
		t.Run(malformed, func(t *testing.T) {
			workspace := testWorkspace(t)
			called := 0
			server, err := NewServer(Binding{Workspace: workspace, Mode: Full, Role: "manager"}, 8, func(context.Context, Request) (any, error) { called++; return nil, nil })
			if err != nil {
				t.Fatal(err)
			}
			hello, _ := json.Marshal(serverHello(Binding{Workspace: workspace, Mode: Full, Role: "manager"}))
			valid := testRecord(t, Request{Type: "request", ID: "later", Operation: "memory.project.initialize", Workspace: workspace, Mode: Full, Role: "manager", Payload: json.RawMessage(`{}`)})
			input := bytes.NewBuffer(append(append(append(hello, '\n'), []byte(malformed+"\n")...), valid...))
			if err := server.Serve(context.Background(), input, io.Discard); err == nil || called != 0 {
				t.Fatalf("err=%v called=%d", err, called)
			}
		})
	}
}

func TestServerMutationBurstFIFO(t *testing.T) {
	workspace := testWorkspace(t)
	var mu sync.Mutex
	order := []string{}
	started := make(chan struct{})
	release := make(chan struct{})
	server, err := NewServer(Binding{Workspace: workspace, Mode: Full, Role: "manager"}, 32, func(ctx context.Context, request Request) (any, error) {
		mu.Lock()
		order = append(order, request.ID)
		mu.Unlock()
		if request.ID == "m0" {
			close(started)
			<-release
		}
		return request.ID, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	var output lockedBuffer
	go func() { done <- server.Serve(context.Background(), reader, &output) }()
	hello, _ := json.Marshal(serverHello(Binding{Workspace: workspace, Mode: Full, Role: "manager"}))
	_, _ = writer.Write(append(hello, '\n'))
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("m%d", i)
		_, _ = writer.Write(testRecord(t, Request{Type: "request", ID: id, Operation: "memory.project.initialize", Workspace: workspace, Mode: Full, Role: "manager", Payload: json.RawMessage(`{}`)}))
		if i == 0 {
			<-started
		}
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for {
		mu.Lock()
		count := len(order)
		mu.Unlock()
		if count == 8 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	_ = writer.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("burst did not complete")
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"m0", "m1", "m2", "m3", "m4", "m5", "m6", "m7"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order=%v want=%v", order, want)
	}
}

func TestServerMutationDomainErrors(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		err        error
		safe       bool
	}{
		{"invalid model plan", "invalid_request", modelplan.ErrInvalid, true}, {"unknown", "recovery_pending", errors.New("unknown"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := testWorkspace(t)
			server, err := NewServer(Binding{Workspace: workspace, Mode: Full, Role: "manager"}, 8, func(context.Context, Request) (any, error) { return nil, tc.err })
			if err != nil {
				t.Fatal(err)
			}
			hello, _ := json.Marshal(serverHello(Binding{Workspace: workspace, Mode: Full, Role: "manager"}))
			reader, writer := io.Pipe()
			var out lockedBuffer
			done := make(chan error, 1)
			go func() { done <- server.Serve(context.Background(), reader, &out) }()
			_, _ = writer.Write(append(hello, '\n'))
			_, _ = writer.Write(testRecord(t, Request{Type: "request", ID: "x", Operation: "memory.project.initialize", Workspace: workspace, Mode: Full, Role: "manager", Payload: json.RawMessage(`{}`)}))
			deadline := time.Now().Add(time.Second)
			for !bytes.Contains(out.Bytes(), []byte(`"id":"x"`)) && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			_ = writer.Close()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(out.Bytes(), []byte(`"code":"`+tc.want+`"`)) || bytes.Contains(out.Bytes(), []byte(`"retrySafe":`+fmt.Sprint(!tc.safe))) {
				t.Fatalf("out=%s", out.Bytes())
			}
		})
	}
}

func TestServerCoalescesIdenticalActiveAndCompletedRequests(t *testing.T) {
	workspace := testWorkspace(t)
	started, release, firstResult := make(chan struct{}), make(chan struct{}), make(chan struct{})
	calls := 0
	server, err := NewServer(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}, 4, func(context.Context, Request) (any, error) {
		calls++
		close(started)
		<-release
		return "ok", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	beforeRead := make(chan struct{}, 8)
	var output lockedBuffer
	var firstResultOnce sync.Once
	outputWriter := writerFunc(func(record []byte) (int, error) {
		n, err := output.Write(record)
		if err == nil && n == len(record) && bytes.Contains(record, []byte(`"id":"same"`)) && bytes.Contains(record, []byte(`"type":"result"`)) {
			firstResultOnce.Do(func() { close(firstResult) })
		}
		return n, err
	})
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(context.Background(), notifyingReader{r: reader, before: beforeRead}, outputWriter)
	}()
	hello, _ := json.Marshal(serverHello(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}))
	<-beforeRead
	_, _ = writer.Write(append(hello, '\n'))
	record := testRecord(t, Request{Type: "request", ID: "same", Operation: "memory.recall", Workspace: workspace, Mode: ReadOnly, Role: "general", Payload: json.RawMessage(`{"key":"value"}`)})
	<-beforeRead
	_, _ = writer.Write(record)
	<-started
	<-beforeRead
	_, _ = writer.Write(record)
	<-beforeRead
	if bytes.Contains(output.Bytes(), []byte(`"code":"conflict"`)) {
		t.Fatal("identical active request conflicted")
	}
	close(release)
	<-firstResult
	_, _ = writer.Write(record)
	_ = writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls != 1 || bytes.Count(output.Bytes(), []byte(`"id":"same"`)) != 2 {
		t.Fatalf("calls=%d output=%s", calls, output.Bytes())
	}
}

func TestServerRetriesCapacityRejectedIDAfterActiveWorkCompletes(t *testing.T) {
	workspace := testWorkspace(t)
	started := make(chan string, 33)
	release := make(chan struct{})
	var calls atomic.Int32
	server, err := NewServer(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}, 4, func(_ context.Context, request Request) (any, error) {
		calls.Add(1)
		started <- request.ID
		<-release
		return request.ID, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	var output lockedBuffer
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), reader, &output) }()
	hello, _ := json.Marshal(serverHello(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}))
	_, _ = writer.Write(append(hello, '\n'))
	for i := 0; i < 32; i++ {
		_, _ = writer.Write(testRecord(t, Request{Type: "request", ID: fmt.Sprintf("active-%d", i), Operation: "memory.recall", Workspace: workspace, Mode: ReadOnly, Role: "general", Payload: json.RawMessage(`{}`)}))
	}
	for i := 0; i < 32; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("active capacity was not reached")
		}
	}
	retry := testRecord(t, Request{Type: "request", ID: "retry", Operation: "memory.recall", Workspace: workspace, Mode: ReadOnly, Role: "general", Payload: json.RawMessage(`{}`)})
	_, _ = writer.Write(retry)
	limitDeadline := time.After(time.Second)
	for !bytes.Contains(output.Bytes(), []byte(`"code":"limit_exceeded"`)) {
		select {
		case <-limitDeadline:
			t.Fatalf("capacity rejection was not observed: output=%s", output.Bytes())
		case <-time.After(time.Millisecond):
		}
	}
	close(release)
	deadline := time.After(time.Second)
	for calls.Load() < 33 {
		_, _ = writer.Write(retry)
		select {
		case <-deadline:
			t.Fatalf("same ID remained rejected after capacity freed: calls=%d output=%s", calls.Load(), output.Bytes())
		case <-time.After(time.Millisecond):
		}
	}
	_ = writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	got := output.Bytes()
	if !bytes.Contains(got, []byte(`"code":"limit_exceeded"`)) || !bytes.Contains(got, []byte(`"id":"retry"`)) || calls.Load() != 33 {
		t.Fatalf("calls=%d output=%s", calls.Load(), got)
	}
}

func TestServerClosesOnMismatchedRequestIDReuse(t *testing.T) {
	workspace := testWorkspace(t)
	started, cancelled := make(chan struct{}), make(chan struct{})
	calls := 0
	server, err := NewServer(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}, 4, func(ctx context.Context, _ Request) (any, error) {
		calls++
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), reader, io.Discard) }()
	hello, _ := json.Marshal(serverHello(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}))
	_, _ = writer.Write(append(hello, '\n'))
	_, _ = writer.Write(testRecord(t, Request{Type: "request", ID: "reuse", Operation: "memory.recall", Workspace: workspace, Mode: ReadOnly, Role: "general", Payload: json.RawMessage(`{}`)}))
	<-started
	_, _ = writer.Write(testRecord(t, Request{Type: "request", ID: "reuse", Operation: "memory.get", Workspace: workspace, Mode: ReadOnly, Role: "general", Payload: json.RawMessage(`{}`)}))
	if err := <-done; err == nil {
		t.Fatal("mismatched request ID reuse accepted")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("active request was not cancelled")
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestServerOutputFailureCancelsAndReapsActiveWork(t *testing.T) {
	workspace := testWorkspace(t)
	started, cancelled := make(chan struct{}), make(chan struct{})
	server, err := NewServer(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}, 4, func(ctx context.Context, _ Request) (any, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), reader, &failAfterWriter{}) }()
	hello, _ := json.Marshal(serverHello(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}))
	_, _ = writer.Write(append(hello, '\n'))
	_, _ = writer.Write(testRecord(t, Request{Type: "request", ID: "active", Operation: "memory.recall", Workspace: workspace, Mode: ReadOnly, Role: "general", Payload: json.RawMessage(`{}`)}))
	<-started
	_ = writer.Close()
	if err := <-done; err == nil {
		t.Fatal("output failure accepted")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("active work was not reaped")
	}
}

func TestServerCancelsActiveRequestAndCachesTerminalReplay(t *testing.T) {
	workspace := testWorkspace(t)
	server, err := NewServer(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}, 4, func(ctx context.Context, _ Request) (any, error) { <-ctx.Done(); return nil, ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), reader, &output) }()
	hello, err := json.Marshal(serverHello(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = writer.Write(append(hello, '\n'))
	request := testRecord(t, Request{Type: "request", ID: "cancel-me", Operation: "memory.recall", Workspace: workspace, Mode: ReadOnly, Role: "general", Payload: json.RawMessage(`{}`)})
	_, _ = writer.Write(request)
	_, _ = writer.Write([]byte(`{"type":"cancel","id":"cancel-me"}` + "\n"))
	_, _ = writer.Write(request)
	_ = writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := output.String(); bytes.Count([]byte(got), []byte(`"id":"cancel-me"`)) < 1 || !bytes.Contains([]byte(got), []byte(`"code":"cancelled"`)) {
		t.Fatalf("terminal replay=%s", got)
	}
}

func TestServerEOFCancelsActiveRequestBeforeWaiting(t *testing.T) {
	workspace := testWorkspace(t)
	started := make(chan struct{})
	server, err := NewServer(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}, 4, func(ctx context.Context, _ Request) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), reader, io.Discard) }()
	hello, _ := json.Marshal(serverHello(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}))
	_, _ = writer.Write(append(hello, '\n'))
	_, _ = writer.Write(testRecord(t, Request{Type: "request", ID: "active", Operation: "memory.recall", Workspace: workspace, Mode: ReadOnly, Role: "general", Payload: json.RawMessage(`{}`)}))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	_ = writer.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("EOF waited for active work before cancelling it")
	}
}

func TestServerRevalidatesWorkspaceBeforeDispatch(t *testing.T) {
	workspace := testWorkspace(t)
	called := 0
	server, err := NewServer(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}, 4, func(context.Context, Request) (any, error) { called++; return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	hello, _ := json.Marshal(serverHello(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}))
	if err := os.Remove(workspace); err != nil {
		t.Fatal(err)
	}
	input := bytes.NewBuffer(append(append(hello, '\n'), testRecord(t, Request{Type: "request", ID: "one", Operation: "memory.recall", Workspace: workspace, Mode: ReadOnly, Role: "general", Payload: json.RawMessage(`{}`)})...))
	var output bytes.Buffer
	if err := server.Serve(context.Background(), input, &output); err != nil {
		t.Fatal(err)
	}
	if called != 0 || !bytes.Contains(output.Bytes(), []byte(`"invalid_request"`)) {
		t.Fatalf("called=%d output=%s", called, output.String())
	}
}

func TestServerRejectsUnknownCancel(t *testing.T) {
	workspace := testWorkspace(t)
	server, err := NewServer(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}, 4, func(context.Context, Request) (any, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	hello, _ := json.Marshal(serverHello(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}))
	var output bytes.Buffer
	if err := server.Serve(context.Background(), bytes.NewBuffer(append(append(hello, '\n'), []byte(`{"type":"cancel","id":"missing"}`+"\n")...)), &output); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte(`"code":"invalid_request"`)) {
		t.Fatalf("output=%s", output.Bytes())
	}
}

func TestServerBoundsOversizedDispatchOutput(t *testing.T) {
	workspace := testWorkspace(t)
	server, err := NewServer(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}, 4, func(context.Context, Request) (any, error) {
		return map[string]string{"value": string(bytes.Repeat([]byte("x"), MaxRecordBytes))}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	hello, _ := json.Marshal(serverHello(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}))
	request := testRecord(t, Request{Type: "request", ID: "large", Operation: "memory.recall", Workspace: workspace, Mode: ReadOnly, Role: "general", Payload: json.RawMessage(`{}`)})
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	var output lockedBuffer
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), reader, &output) }()
	if _, err := writer.Write(append(append(hello, '\n'), request...)); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for !bytes.Contains(output.Bytes(), []byte(`"code":"limit_exceeded"`)) {
		select {
		case <-deadline:
			t.Fatalf("no oversized response: %s", output.Bytes())
		case <-time.After(time.Millisecond):
		}
	}
	writer.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop")
	}
	data := output.Bytes()
	if len(data) > 2*MaxRecordBytes || !bytes.Contains(data, []byte(`"id":"large"`)) {
		t.Fatalf("invalid oversized response %s", data)
	}

}

func TestServerSerializesMutationsAndSkipsCancelledQueuedMutation(t *testing.T) {
	workspace := testWorkspace(t)
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var calls []string
	var callsMu sync.Mutex
	server, err := NewServer(Binding{Workspace: workspace, Mode: Full, Role: "manager"}, 4, func(ctx context.Context, request Request) (any, error) {
		callsMu.Lock()
		calls = append(calls, request.ID)
		callsMu.Unlock()
		if request.ID == "first" {
			close(firstStarted)
			select {
			case <-releaseFirst:
			case <-ctx.Done():
			}
		}
		return request.ID, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), reader, &output) }()
	hello, _ := json.Marshal(serverHello(Binding{Workspace: workspace, Mode: Full, Role: "manager"}))
	_, _ = writer.Write(append(hello, '\n'))
	for _, id := range []string{"first", "second"} {
		_, _ = writer.Write(testRecord(t, Request{Type: "request", ID: id, Operation: "memory.remember", Workspace: workspace, Mode: Full, Role: "manager", Payload: json.RawMessage(`{}`)}))
	}
	<-firstStarted
	_, _ = writer.Write([]byte(`{"type":"cancel","id":"second"}` + "\n"))
	time.Sleep(20 * time.Millisecond) // Let the reader apply cancellation before releasing the gate.
	close(releaseFirst)
	_ = writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	callsMu.Lock()
	defer callsMu.Unlock()
	if !reflect.DeepEqual(calls, []string{"first"}) {
		t.Fatalf("mutation calls=%v", calls)
	}
	if !bytes.Contains(output.Bytes(), []byte(`"id":"second"`)) || !bytes.Contains(output.Bytes(), []byte(`"code":"cancelled"`)) || !bytes.Contains(output.Bytes(), []byte(`"retrySafe":true`)) {
		t.Fatalf("output=%s", output.Bytes())
	}
}

func TestServerUsesSpecificAuthorityErrors(t *testing.T) {
	workspace := testWorkspace(t)
	server, err := NewServer(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}, 4, func(context.Context, Request) (any, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	hello, _ := json.Marshal(serverHello(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}))
	request := testRecord(t, Request{Type: "request", ID: "denied", Operation: "memory.remember", Workspace: workspace, Mode: ReadOnly, Role: "general", Payload: json.RawMessage(`{}`)})
	var output bytes.Buffer
	if err := server.Serve(context.Background(), bytes.NewBuffer(append(append(hello, '\n'), request...)), &output); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte(`"code":"authorization_denied"`)) {
		t.Fatalf("output=%s", output.Bytes())
	}
}

func TestServerSerializesMutationDispatches(t *testing.T) {
	workspace := testWorkspace(t)
	firstStarted, releaseFirst, secondStarted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server, err := NewServer(Binding{Workspace: workspace, Mode: Full, Role: "manager"}, 4, func(_ context.Context, request Request) (any, error) {
		if request.ID == "first" {
			close(firstStarted)
			<-releaseFirst
		} else {
			close(secondStarted)
		}
		return request.ID, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), reader, io.Discard) }()
	hello, _ := json.Marshal(serverHello(Binding{Workspace: workspace, Mode: Full, Role: "manager"}))
	_, _ = writer.Write(append(hello, '\n'))
	for _, id := range []string{"first", "second"} {
		_, _ = writer.Write(testRecord(t, Request{Type: "request", ID: id, Operation: "memory.remember", Workspace: workspace, Mode: Full, Role: "manager", Payload: json.RawMessage(`{}`)}))
	}
	<-firstStarted
	select {
	case <-secondStarted:
		t.Fatal("second mutation dispatched while first mutation was active")
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseFirst)
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("second mutation did not dispatch after first completed")
	}
	_ = writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestServerMarksMutationFailureRetryUnsafeAndReadFailureSafe(t *testing.T) {
	workspace := testWorkspace(t)
	server, err := NewServer(Binding{Workspace: workspace, Mode: Full, Role: "manager"}, 4, func(_ context.Context, request Request) (any, error) { return nil, errors.New(request.ID) })
	if err != nil {
		t.Fatal(err)
	}
	hello, _ := json.Marshal(serverHello(Binding{Workspace: workspace, Mode: Full, Role: "manager"}))
	input := append(hello, '\n')
	input = append(input, testRecord(t, Request{Type: "request", ID: "write", Operation: "memory.remember", Workspace: workspace, Mode: Full, Role: "manager", Payload: json.RawMessage(`{}`)})...)
	input = append(input, testRecord(t, Request{Type: "request", ID: "read", Operation: "memory.recall", Workspace: workspace, Mode: Full, Role: "manager", Payload: json.RawMessage(`{}`)})...)
	var output bytes.Buffer
	if err := server.Serve(context.Background(), bytes.NewBuffer(input), &output); err != nil {
		t.Fatal(err)
	}
	var records []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n")) {
		var record map[string]any
		if json.Unmarshal(line, &record) == nil && record["type"] == "error" {
			records = append(records, record)
		}
	}
	byID := map[string]map[string]any{}
	for _, record := range records {
		byID[record["id"].(string)] = record
	}
	if len(records) != 2 || byID["read"]["retrySafe"] != true {
		t.Fatalf("records=%v", records)
	}
}

func TestServerMalformedProtocolCancelsActiveRequest(t *testing.T) {
	workspace := testWorkspace(t)
	started := make(chan struct{})
	cancelled := make(chan struct{})
	server, err := NewServer(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}, 4, func(ctx context.Context, _ Request) (any, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), reader, io.Discard) }()
	hello, _ := json.Marshal(serverHello(Binding{Workspace: workspace, Mode: ReadOnly, Role: "general"}))
	_, _ = writer.Write(append(hello, '\n'))
	_, _ = writer.Write(testRecord(t, Request{Type: "request", ID: "active", Operation: "memory.recall", Workspace: workspace, Mode: ReadOnly, Role: "general", Payload: json.RawMessage(`{}`)}))
	<-started
	_, _ = writer.Write([]byte("not-json\n"))
	if err := <-done; err == nil {
		t.Fatal("malformed protocol was accepted")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("malformed protocol did not cancel active request")
	}
}

type scriptedProcess struct {
	trace         *[]string
	closeErr      error
	waits         []error
	waitDeadlines []bool
}

func (p *scriptedProcess) CloseInput() error {
	*p.trace = append(*p.trace, "close-input")
	return p.closeErr
}

func (p *scriptedProcess) Wait(ctx context.Context) error {
	*p.trace = append(*p.trace, "wait")
	_, hasDeadline := ctx.Deadline()
	p.waitDeadlines = append(p.waitDeadlines, hasDeadline)
	if len(p.waits) == 0 {
		return nil
	}
	err := p.waits[0]
	p.waits = p.waits[1:]
	return err
}

func (p *scriptedProcess) TerminateTree() error {
	*p.trace = append(*p.trace, "terminate-tree")
	return nil
}

func (p *scriptedProcess) KillTree() error {
	*p.trace = append(*p.trace, "kill-tree")
	return nil
}

type scriptedLauncher struct {
	trace    []string
	startErr error
	process  *scriptedProcess
	stdout   io.Writer
	stderr   io.Writer
}

func (l *scriptedLauncher) Start(_ context.Context, _ string, _ []string, _ io.Reader, stdout, stderr io.Writer) (processHandle, error) {
	l.trace = append(l.trace, "start")
	l.stdout = stdout
	l.stderr = stderr
	if l.process == nil {
		l.process = &scriptedProcess{}
	}
	l.process.trace = &l.trace
	return l.process, l.startErr
}

func newScriptedLifecycle(l *scriptedLauncher, gate *terminalGate) *processLifecycle {
	return newProcessLifecycle(l, gate, time.Millisecond)
}

func TestProcessLifecycleCompletion(t *testing.T) {
	l := &scriptedLauncher{}
	lifecycle := newScriptedLifecycle(l, &terminalGate{})
	if err := lifecycle.Start(context.Background(), "pi", nil, nil, io.Discard, io.Discard); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := lifecycle.Complete(context.Background()); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if want := []string{"start", "close-input", "wait"}; !reflect.DeepEqual(l.trace, want) {
		t.Fatalf("trace = %v, want %v", l.trace, want)
	}
}

func TestProcessLifecycleCancelEscalation(t *testing.T) {
	l := &scriptedLauncher{process: &scriptedProcess{waits: []error{errors.New("first wait"), errors.New("second wait"), nil}}}
	lifecycle := newScriptedLifecycle(l, &terminalGate{})
	if err := lifecycle.Start(context.Background(), "pi", nil, nil, io.Discard, io.Discard); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := lifecycle.Cancel(context.Background()); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if want := []string{"start", "close-input", "wait", "terminate-tree", "wait", "kill-tree", "wait"}; !reflect.DeepEqual(l.trace, want) {
		t.Fatalf("trace = %v, want %v", l.trace, want)
	}
	if want := []bool{true, true, true}; !reflect.DeepEqual(l.process.waitDeadlines, want) {
		t.Fatalf("Wait deadline presence = %v, want %v", l.process.waitDeadlines, want)
	}
}

func TestProcessLifecycleAttachFailureReaps(t *testing.T) {
	l := &scriptedLauncher{startErr: errors.New("attach failed"), process: &scriptedProcess{waits: []error{errors.New("reap failed"), nil}}}
	lifecycle := newScriptedLifecycle(l, &terminalGate{})
	if err := lifecycle.Start(context.Background(), "pi", nil, nil, io.Discard, io.Discard); !errors.Is(err, l.startErr) {
		t.Fatalf("Start error = %v, want attach failure", err)
	}
	if want := []string{"start", "terminate-tree", "wait", "kill-tree", "wait"}; !reflect.DeepEqual(l.trace, want) {
		t.Fatalf("trace = %v, want %v", l.trace, want)
	}
	got, ok := lifecycle.gate.Result()
	if !ok || got.Kind != terminalFailed || !errors.Is(got.Err, l.startErr) {
		t.Fatalf("Result = %#v, %t; want failed with attach error, true", got, ok)
	}
}

func TestProcessLifecycleTerminalRace(t *testing.T) {
	gate := &terminalGate{}
	completeErr := errors.New("complete failed")
	if !gate.Commit(terminalRecord{Kind: terminalCompleted, Err: completeErr}) {
		t.Fatal("first terminal commit did not win")
	}
	if gate.Commit(terminalRecord{Kind: terminalCancelled}) {
		t.Fatal("second terminal commit won")
	}
	got, ok := gate.Result()
	if !ok || got.Kind != terminalCompleted || !errors.Is(got.Err, completeErr) {
		t.Fatalf("Result = %#v, %t; want completed with error, true", got, ok)
	}
}

func TestProcessLifecycleCommitsCompletion(t *testing.T) {
	gate := &terminalGate{}
	waitErr := errors.New("wait failed")
	l := &scriptedLauncher{process: &scriptedProcess{waits: []error{waitErr}}}
	lifecycle := newScriptedLifecycle(l, gate)
	if err := lifecycle.Start(context.Background(), "pi", nil, nil, io.Discard, io.Discard); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := lifecycle.Complete(context.Background()); !errors.Is(err, waitErr) {
		t.Fatalf("Complete error = %v, want wait failure", err)
	}
	got, ok := gate.Result()
	if !ok || got.Kind != terminalCompleted || !errors.Is(got.Err, waitErr) {
		t.Fatalf("Result = %#v, %t; want completed wait failure, true", got, ok)
	}
}

func TestProcessLifecycleCompletionCloseErrorStillWaits(t *testing.T) {
	gate := &terminalGate{}
	closeErr := errors.New("close failed")
	waitErr := errors.New("wait failed")
	l := &scriptedLauncher{process: &scriptedProcess{closeErr: closeErr, waits: []error{waitErr}}}
	lifecycle := newScriptedLifecycle(l, gate)
	if err := lifecycle.Start(context.Background(), "pi", nil, nil, io.Discard, io.Discard); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := lifecycle.Complete(context.Background()); !errors.Is(err, closeErr) || !errors.Is(err, waitErr) {
		t.Fatalf("Complete error = %v, want joined close and wait errors", err)
	}
	if want := []string{"start", "close-input", "wait"}; !reflect.DeepEqual(l.trace, want) {
		t.Fatalf("trace = %v, want %v", l.trace, want)
	}
	got, ok := gate.Result()
	if !ok || got.Kind != terminalCompleted || !errors.Is(got.Err, closeErr) || !errors.Is(got.Err, waitErr) {
		t.Fatalf("Result = %#v, %t; want completed with joined close and wait errors, true", got, ok)
	}
}

func TestProcessLifecycleCancelCloseErrorStillCleansUp(t *testing.T) {
	gate := &terminalGate{}
	closeErr := errors.New("close failed")
	l := &scriptedLauncher{process: &scriptedProcess{closeErr: closeErr, waits: []error{errors.New("first wait"), errors.New("second wait"), nil}}}
	lifecycle := newScriptedLifecycle(l, gate)
	if err := lifecycle.Start(context.Background(), "pi", nil, nil, io.Discard, io.Discard); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := lifecycle.Cancel(context.Background()); !errors.Is(err, closeErr) {
		t.Fatalf("Cancel error = %v, want close error", err)
	}
	if want := []string{"start", "close-input", "wait", "terminate-tree", "wait", "kill-tree", "wait"}; !reflect.DeepEqual(l.trace, want) {
		t.Fatalf("trace = %v, want %v", l.trace, want)
	}
	got, ok := gate.Result()
	if !ok || got.Kind != terminalCancelled || !errors.Is(got.Err, closeErr) {
		t.Fatalf("Result = %#v, %t; want cancelled with close error, true", got, ok)
	}
}

func TestProcessLifecycleCancelledStartCommitsFailure(t *testing.T) {
	gate := &terminalGate{}
	l := &scriptedLauncher{}
	lifecycle := newScriptedLifecycle(l, gate)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := lifecycle.Start(ctx, "pi", nil, nil, io.Discard, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatalf("Start error = %v, want context cancelled", err)
	}
	if want := []string(nil); !reflect.DeepEqual(l.trace, want) {
		t.Fatalf("trace = %v, want no launcher call", l.trace)
	}
	got, ok := gate.Result()
	if !ok || got.Kind != terminalFailed || !errors.Is(got.Err, context.Canceled) {
		t.Fatalf("Result = %#v, %t; want failed with context cancellation, true", got, ok)
	}
}

func TestProcessLifecycleDiscardsLateOutputOnBothStreams(t *testing.T) {
	gate := &terminalGate{}
	l := &scriptedLauncher{}
	var stdout, stderr []byte
	lifecycle := newScriptedLifecycle(l, gate)
	if err := lifecycle.Start(context.Background(), "pi", nil, nil, writerFunc(func(p []byte) (int, error) {
		stdout = append(stdout, p...)
		return len(p), nil
	}), writerFunc(func(p []byte) (int, error) {
		stderr = append(stderr, p...)
		return len(p), nil
	})); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := l.stdout.Write([]byte("early stdout")); err != nil || string(stdout) != "early stdout" {
		t.Fatalf("early stdout = %q, %v", stdout, err)
	}
	if _, err := l.stderr.Write([]byte("early stderr")); err != nil || string(stderr) != "early stderr" {
		t.Fatalf("early stderr = %q, %v", stderr, err)
	}
	gate.Commit(terminalRecord{Kind: terminalCancelled})
	if n, err := l.stdout.Write([]byte("late stdout")); n != len("late stdout") || err != nil || string(stdout) != "early stdout" {
		t.Fatalf("late stdout = %q, %d, %v", stdout, n, err)
	}
	if n, err := l.stderr.Write([]byte("late stderr")); n != len("late stderr") || err != nil || string(stderr) != "early stderr" {
		t.Fatalf("late stderr = %q, %d, %v", stderr, n, err)
	}
}

type lockCheckingWriter struct {
	gate *terminalGate
}

func (w lockCheckingWriter) Write(p []byte) (int, error) {
	if w.gate.mu.TryLock() {
		w.gate.mu.Unlock()
		return 0, errors.New("gate mutex was not held")
	}
	return len(p), nil
}

func TestGatedWriterHoldsTerminalGateDuringWrite(t *testing.T) {
	gate := &terminalGate{}
	w := gatedWriter{gate: gate, dst: lockCheckingWriter{gate: gate}}
	if n, err := w.Write([]byte("output")); err != nil || n != len("output") {
		t.Fatalf("Write = %d, %v; want %d, nil", n, err, len("output"))
	}
}

func TestPlatformProcessLauncherExistsWithoutStarting(t *testing.T) {
	if newPlatformProcessLauncher == nil {
		t.Fatal("newPlatformProcessLauncher is nil")
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
