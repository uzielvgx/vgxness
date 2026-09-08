package pi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"unicode/utf8"

	"github.com/vgxness/vgxness/internal/sdd"
)

// Dispatch is deliberately narrow: T03 supplies domain operation adapters.
type Dispatch func(context.Context, Request) (any, error)

type Hello struct {
	Type           string         `json:"type"`
	Protocol       string         `json:"protocol"`
	Implementation Implementation `json:"implementation"`
	Workspace      string         `json:"workspace"`
	Mode           Mode           `json:"mode"`
	Role           string         `json:"role"`
	Capabilities   []string       `json:"capabilities"`
	Limits         HelloLimits    `json:"limits"`
}
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type HelloLimits struct {
	MaxRecordBytes    int `json:"maxRecordBytes"`
	MaxActiveRequests int `json:"maxActiveRequests"`
	MaxCorrelationIDs int `json:"maxCorrelationIds"`
}

var serverCapabilities = []string{"memory.forget", "memory.get", "memory.project.initialize", "memory.project.resolve", "memory.recall", "memory.recent", "memory.remember", "memory.session.checkpoint", "memory.session.context", "memory.session.draft_save", "memory.session.end", "memory.session.renew", "memory.session.start", "memory.sync", "memory.sync.backfill", "memory.sync.configure", "memory.sync.rejoin", "memory.sync.repair_project", "memory.sync.reseed", "memory.sync.status", "model.resolve", "sdd.accept_revision", "sdd.cancel", "sdd.compare_projection", "sdd.create", "sdd.get", "sdd.get_revision", "sdd.list", "sdd.list_revisions", "sdd.projection_status", "sdd.record_projection", "sdd.render_projection", "sdd.save_revision", "sdd.set_interaction_mode", "sdd.transition"}

// Server owns a single private stdin/stdout protocol session.
type Server struct {
	binding       Binding
	workspaceInfo os.FileInfo
	replay        *Replay
	dispatch      Dispatch
	hello         Hello
	mutationGate  chan struct{}
	mutationMu    sync.Mutex
	nextMutation  uint64
	serveMutation uint64
	mutationCond  *sync.Cond
}

type activeRequest struct {
	cancel      context.CancelFunc
	fingerprint [sha256.Size]byte
}

type cachedTerminalRecord struct {
	record      any
	fingerprint [sha256.Size]byte
}

func NewServer(binding Binding, limit int, dispatch Dispatch) (*Server, error) {
	workspace, info, err := CanonicalWorkspace(binding.Workspace)
	if err != nil {
		return nil, err
	}
	if (binding.Mode != ReadOnly && binding.Mode != Full) || !allRoles[binding.Role] || dispatch == nil {
		return nil, errors.New("invalid server binding")
	}
	binding.Workspace = workspace
	hello, err := makeHello(binding)
	if err != nil {
		return nil, err
	}
	s := &Server{binding: binding, workspaceInfo: info, replay: NewReplay(4096), dispatch: dispatch, hello: hello, mutationGate: make(chan struct{}, 1)}
	s.mutationCond = sync.NewCond(&s.mutationMu)
	return s, nil
}

func (s *Server) reserveMutation() (uint64, bool) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	ticket := s.nextMutation
	s.nextMutation++
	return ticket, ticket != s.serveMutation
}

func (s *Server) waitMutation(ticket uint64) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	for ticket != s.serveMutation {
		s.mutationCond.Wait()
	}
}

func (s *Server) releaseMutation() {
	s.mutationMu.Lock()
	s.serveMutation++
	s.mutationCond.Broadcast()
	s.mutationMu.Unlock()
}

// CanonicalWorkspace resolves the one workspace identity shared by the
// dispatcher configuration and protocol binding.
func CanonicalWorkspace(workspace string) (string, os.FileInfo, error) {
	if workspace == "" {
		return "", nil, errors.New("workspace required")
	}
	abs, err := filepath.Abs(filepath.Clean(workspace))
	if err != nil {
		return "", nil, err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", nil, errors.New("workspace directory required")
	}
	return resolved, info, nil
}

func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) (resultErr error) {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	r := bufio.NewReaderSize(in, MaxRecordBytes+1)
	w := bufio.NewWriter(out)
	if err := writeJSONL(w, s.hello); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}
	closeInput := func() {}
	if closer, ok := in.(io.Closer); ok {
		var once sync.Once
		closeInput = func() { once.Do(func() { _ = closer.Close() }) }
		go func() { <-ctx.Done(); closeInput() }()
	}
	line, err := readRecord(r)
	if err != nil {
		return err
	}
	var hello Hello
	if err := decodeHello(line, &hello); err != nil || !sameHello(hello, s.hello) {
		return errors.New("handshake rejected")
	}
	var output sync.Mutex
	active := make(map[string]activeRequest)
	queuedMutations := make(map[string]context.CancelFunc)
	terminal := make(map[string]cachedTerminalRecord)
	var activeMu sync.Mutex
	var work sync.WaitGroup
	var outputErr error
	var outputErrMu sync.Mutex
	transportError := func() error {
		outputErrMu.Lock()
		defer outputErrMu.Unlock()
		return outputErr
	}
	failOutput := func(err error) {
		if err == nil {
			return
		}
		outputErrMu.Lock()
		if outputErr == nil {
			outputErr = err
			stop()
			closeInput()
		}
		outputErrMu.Unlock()
	}
	write := func(value any) error {
		output.Lock()
		defer output.Unlock()
		if err := writeBoundedJSONL(w, value); err != nil {
			if !errors.Is(err, errOutputTooLarge) || writeJSONL(w, map[string]any{"type": "error", "code": "limit_exceeded", "retrySafe": true, "message": "response too large"}) != nil {
				return err
			}
		}
		return w.Flush()
	}
	cancelAll := func() {
		activeMu.Lock()
		defer activeMu.Unlock()
		for _, request := range active {
			request.cancel()
		}
	}
	defer func() {
		cancelAll()
		closeInput()
		work.Wait()
		if resultErr == nil {
			resultErr = transportError()
		}
	}()
	for {
		if err := transportError(); err != nil {
			return err
		}
		line, err = readRecord(r)
		if err == io.EOF {
			if outputErr := transportError(); outputErr != nil {
				return outputErr
			}
			if len(line) != 0 {
				return errors.New("invalid record framing")
			}
			cancelAll()
			work.Wait()
			return nil
		}
		if err != nil {
			if outputErr := transportError(); outputErr != nil {
				return outputErr
			}
			return err
		}
		var kind struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if err := json.Unmarshal(bytes.TrimSpace(line), &kind); err != nil {
			return errors.New("invalid record")
		}
		if kind.Type == "cancel" {
			cancelRecord, decodeErr := DecodeCancel(line)
			if decodeErr != nil {
				return decodeErr
			}
			activeMu.Lock()
			request, ok := active[cancelRecord.ID]
			activeMu.Unlock()
			if ok {
				request.cancel()
			} else if writeErr := write(map[string]any{"type": "error", "id": cancelRecord.ID, "code": "invalid_request", "retrySafe": true, "message": "request not active"}); writeErr != nil {
				return writeErr
			}
			continue
		}
		req, err := DecodeRecord(line)
		if err != nil {
			// Framing/schema failures terminate this private transport. Continuing
			// would permit an attacker to smuggle later work after malformed input.
			return err
		}
		err = s.binding.Authorize(req)
		if err == nil {
			err = s.revalidateWorkspace()
		}
		if err == nil {
			fingerprint := requestFingerprint(req)
			activeMu.Lock()
			cached, finished := terminal[req.ID]
			running, runningNow := active[req.ID]
			activeMu.Unlock()
			if finished {
				if cached.fingerprint != fingerprint {
					return errors.New("correlation ID reused for different request")
				}
				if writeErr := write(cached.record); writeErr != nil {
					return writeErr
				}
				continue
			}
			if runningNow {
				if running.fingerprint != fingerprint {
					return errors.New("correlation ID reused for different request")
				}
				continue
			}
		}
		if err == nil {
			activeMu.Lock()
			atLimit := len(active) >= 32
			activeMu.Unlock()
			if atLimit {
				if writeErr := write(map[string]any{"type": "error", "id": req.ID, "code": "limit_exceeded", "retrySafe": true, "message": "request rejected"}); writeErr != nil {
					return writeErr
				}
				continue
			}
			if !s.replay.Accept(req.ID) {
				err = errors.New("replay rejected")
			}
		}
		if err != nil {
			code := "invalid_request"
			var authorityErr *AuthorityError
			if errors.As(err, &authorityErr) {
				code = authorityErr.Code
			}
			if writeErr := write(map[string]any{"type": "error", "id": kind.ID, "code": code, "retrySafe": true, "message": "request rejected"}); writeErr != nil {
				return writeErr
			}
			continue
		}
		activeMu.Lock()
		requestCtx, cancel := context.WithCancel(ctx)
		active[req.ID] = activeRequest{cancel: cancel, fingerprint: requestFingerprint(req)}
		activeMu.Unlock()
		mutates := operationMutates(req.Operation)
		var mutationTicket uint64
		queuedMutation := false
		if mutates {
			mutationTicket, queuedMutation = s.reserveMutation()
			if queuedMutation {
				activeMu.Lock()
				queuedMutations[req.ID] = cancel
				activeMu.Unlock()
			}
		}
		work.Add(1)
		go func(req Request, requestCtx context.Context, mutates bool, mutationTicket uint64) {
			defer work.Done()
			defer func() { activeMu.Lock(); delete(active, req.ID); activeMu.Unlock() }()
			dispatched := false
			if mutates {
				s.waitMutation(mutationTicket)
				if queuedMutation {
					activeMu.Lock()
					delete(queuedMutations, req.ID)
					activeMu.Unlock()
				}
				defer s.releaseMutation()
			}
			var result any
			var dispatchErr error
			if requestCtx.Err() == nil {
				dispatchErr = s.revalidateWorkspace()
				if dispatchErr == nil {
					dispatched = true
					result, dispatchErr = s.dispatch(requestCtx, req)
				}
			}
			var record any
			if requestCtx.Err() != nil {
				if mutates && dispatched {
					record = map[string]any{"type": "error", "id": req.ID, "code": "recovery_pending", "retrySafe": false, "recoveryState": "recovery_pending", "message": "mutation outcome uncertain"}
				} else {
					record = map[string]any{"type": "error", "id": req.ID, "code": "cancelled", "retrySafe": true, "message": "request cancelled"}
				}
			} else if dispatchErr != nil {
				if mutates && dispatched && !safeDomainError(dispatchErr) {
					record = map[string]any{"type": "error", "id": req.ID, "code": "recovery_pending", "retrySafe": false, "recoveryState": "recovery_pending", "message": "mutation outcome uncertain"}
				} else {
					code := "unavailable"
					if errors.Is(dispatchErr, sdd.ErrConflict) || errors.Is(dispatchErr, sdd.ErrStaleState) {
						code = "conflict"
					}
					if errors.Is(dispatchErr, sdd.ErrInvalid) {
						code = "invalid_request"
					}
					record = map[string]any{"type": "error", "id": req.ID, "code": code, "retrySafe": true, "message": "operation rejected"}
				}
			} else {
				record = map[string]any{"type": "result", "id": req.ID, "result": result}
			}
			if recordTooLarge(record) {
				record = map[string]any{"type": "error", "id": req.ID, "code": "limit_exceeded", "retrySafe": !mutates, "message": "response too large"}
			}
			activeMu.Lock()
			terminal[req.ID] = cachedTerminalRecord{record: record, fingerprint: requestFingerprint(req)}
			activeMu.Unlock()
			// A broken transport makes every in-flight result uncertain. Cancelling
			// the root context and closing input wakes the reader; defer reaps every
			// active worker before Serve returns the write error.
			failOutput(write(record))
		}(req, requestCtx, mutates, mutationTicket)
	}
}

func requestFingerprint(request Request) [sha256.Size]byte {
	data, _ := json.Marshal(request)
	return sha256.Sum256(data)
}

func safeDomainError(err error) bool {
	return errors.Is(err, sdd.ErrConflict) || errors.Is(err, sdd.ErrStaleState) || errors.Is(err, sdd.ErrInvalid) || errors.Is(err, sdd.ErrDigestMismatch) || errors.Is(err, sdd.ErrInputsChanged)
}

var errOutputTooLarge = errors.New("output record too large")

func recordTooLarge(value any) bool {
	b, err := json.Marshal(value)
	return err != nil || len(b)+1 > MaxRecordBytes
}

func writeBoundedJSONL(w io.Writer, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(b)+1 > MaxRecordBytes {
		return errOutputTooLarge
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

func readRecord(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadSlice('\n')
	if err == bufio.ErrBufferFull || len(line) > MaxRecordBytes {
		return nil, errors.New("record too large")
	}
	return line, err
}
func decodeHello(line []byte, hello *Hello) error {
	if len(line) == 0 || len(line) > MaxRecordBytes || !utf8.Valid(line) || line[len(line)-1] != '\n' || bytes.Contains(line, []byte{'\r'}) || hasDuplicateKeys(line[:len(line)-1]) {
		return errors.New("invalid hello")
	}
	d := json.NewDecoder(bytes.NewReader(line[:len(line)-1]))
	d.DisallowUnknownFields()
	if err := d.Decode(hello); err != nil || d.Decode(&struct{}{}) != io.EOF || hello.Type != "hello" {
		return errors.New("invalid hello")
	}
	return nil
}
func makeHello(binding Binding) (Hello, error) {
	path, err := os.Executable()
	if err != nil {
		return Hello{}, err
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return Hello{}, err
	}
	digest := sha256.Sum256(bytes)
	return Hello{Type: "hello", Protocol: Protocol, Implementation: Implementation{Name: "vgxness-pi-backend", Version: "0.1.0", SHA256: fmt.Sprintf("%x", digest)}, Workspace: binding.Workspace, Mode: binding.Mode, Role: binding.Role, Capabilities: serverCapabilities, Limits: HelloLimits{MaxRecordBytes: MaxRecordBytes, MaxActiveRequests: 32, MaxCorrelationIDs: 4096}}, nil
}
func serverHello(binding Binding) Hello { value, _ := makeHello(binding); return value }
func (s *Server) revalidateWorkspace() error {
	workspace, info, err := CanonicalWorkspace(s.binding.Workspace)
	if err != nil || workspace != s.binding.Workspace || !os.SameFile(info, s.workspaceInfo) {
		return errors.New("workspace mismatch")
	}
	return nil
}
func sameHello(a, b Hello) bool {
	if a.Type != b.Type || a.Protocol != b.Protocol || a.Implementation != b.Implementation || a.Workspace != b.Workspace || a.Mode != b.Mode || a.Role != b.Role || a.Limits != b.Limits || len(a.Capabilities) != len(b.Capabilities) {
		return false
	}
	for i := range a.Capabilities {
		if a.Capabilities[i] != b.Capabilities[i] {
			return false
		}
	}
	return true
}
func writeJSONL(w io.Writer, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}
