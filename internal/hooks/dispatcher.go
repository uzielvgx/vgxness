// Package hooks provides bounded, best-effort notifications for committed
// VGXNESS state transitions. It is not a durable event log.
package hooks

import (
	"context"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	MaxArtifactDigests = 32
	maxIdentityBytes   = 240
	maxHandlers        = 64
	maxHandlerTimeout  = 5 * time.Second
	maxDepth           = 16
	maxDedupeCapacity  = 65_536
	maxCount           = 1 << 20
)

var validIdentity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)

type Name string

const (
	TaskStartedName         Name = "task.started"
	TaskSucceededName       Name = "task.succeeded"
	TaskFailedName          Name = "task.failed"
	CandidateFrozenName     Name = "candidate.frozen"
	ValidationCompletedName Name = "validation.completed"
	DeliveryInstalledName   Name = "delivery.installed"
)

type Mode string

const (
	ModeForeground Mode = "foreground"
	ModeBackground Mode = "background"
)

// Metadata identifies one notification. ID must be stable for the committed
// transition so replay is suppressed within a dispatcher process.
type Metadata struct {
	ID string
	At time.Time
}

// Event is closed to the bounded event types declared by this package.
type Event interface {
	Name() Name
	metadata() Metadata
	clone() Event
	validatePayload() bool
}

type TaskStarted struct {
	Meta   Metadata
	RunID  string
	TaskID string
	Mode   Mode
}

func (event TaskStarted) Name() Name         { return TaskStartedName }
func (event TaskStarted) metadata() Metadata { return event.Meta }
func (event TaskStarted) clone() Event       { return event }
func (event TaskStarted) validatePayload() bool {
	return validID(event.RunID) && validID(event.TaskID) && validMode(event.Mode)
}

type TaskSucceeded struct {
	Meta         Metadata
	RunID        string
	TaskID       string
	Mode         Mode
	ResultID     string
	ResultDigest string
}

func (event TaskSucceeded) Name() Name         { return TaskSucceededName }
func (event TaskSucceeded) metadata() Metadata { return event.Meta }
func (event TaskSucceeded) clone() Event       { return event }
func (event TaskSucceeded) validatePayload() bool {
	return validID(event.RunID) && validID(event.TaskID) && validMode(event.Mode) && validID(event.ResultID) && validDigest(event.ResultDigest)
}

type TaskFailed struct {
	Meta          Metadata
	RunID         string
	TaskID        string
	Mode          Mode
	ResultID      string
	FailureDigest string
	ExitCode      int
}

func (event TaskFailed) Name() Name         { return TaskFailedName }
func (event TaskFailed) metadata() Metadata { return event.Meta }
func (event TaskFailed) clone() Event       { return event }
func (event TaskFailed) validatePayload() bool {
	return validID(event.RunID) && validID(event.TaskID) && validMode(event.Mode) &&
		(event.ResultID == "" || validID(event.ResultID)) && (event.FailureDigest == "" || validDigest(event.FailureDigest)) && validExitCode(event.ExitCode)
}

type CandidateFrozen struct {
	Meta            Metadata
	TicketID        string
	TaskID          string
	ManifestDigest  string
	ArtifactDigests []string
	ChangeCount     int
}

func (event CandidateFrozen) Name() Name         { return CandidateFrozenName }
func (event CandidateFrozen) metadata() Metadata { return event.Meta }
func (event CandidateFrozen) clone() Event {
	event.ArtifactDigests = append([]string(nil), event.ArtifactDigests...)
	return event
}
func (event CandidateFrozen) validatePayload() bool {
	if !validID(event.TicketID) || !validID(event.TaskID) || !validDigest(event.ManifestDigest) || !validCount(event.ChangeCount) || len(event.ArtifactDigests) > MaxArtifactDigests {
		return false
	}
	for _, digest := range event.ArtifactDigests {
		if !validDigest(digest) {
			return false
		}
	}
	return true
}

type ValidationCompleted struct {
	Meta          Metadata
	TicketID      string
	ReceiptDigest string
	Operation     string
	Success       bool
	ExitCode      int
	PackageCount  int
	ChangeCount   int
}

func (event ValidationCompleted) Name() Name         { return ValidationCompletedName }
func (event ValidationCompleted) metadata() Metadata { return event.Meta }
func (event ValidationCompleted) clone() Event       { return event }
func (event ValidationCompleted) validatePayload() bool {
	return validID(event.TicketID) && validDigest(event.ReceiptDigest) &&
		(event.Operation == "format" || event.Operation == "test" || event.Operation == "vet") &&
		validExitCode(event.ExitCode) && validCount(event.PackageCount) && validCount(event.ChangeCount)
}

type DeliveryInstalled struct {
	Meta          Metadata
	ReceiptID     string
	ReceiptDigest string
	ChangeCount   int
}

func (event DeliveryInstalled) Name() Name         { return DeliveryInstalledName }
func (event DeliveryInstalled) metadata() Metadata { return event.Meta }
func (event DeliveryInstalled) clone() Event       { return event }
func (event DeliveryInstalled) validatePayload() bool {
	return validDigest(event.ReceiptID) && validDigest(event.ReceiptDigest) && validCount(event.ChangeCount)
}

type Handler func(context.Context, Event) error

type Options struct {
	HandlerTimeout time.Duration
	MaxDepth       int
	DedupeCapacity int
}

type DiagnosticKind string

const (
	DiagnosticInvalid DiagnosticKind = "invalid"
	DiagnosticTimeout DiagnosticKind = "timeout"
	DiagnosticPanic   DiagnosticKind = "panic"
	DiagnosticError   DiagnosticKind = "error"
	DiagnosticDepth   DiagnosticKind = "recursion-depth"
)

// Diagnostic deliberately excludes handler-provided error and panic values.
type Diagnostic struct {
	Handler int
	Kind    DiagnosticKind
}

type Dispatcher struct {
	handlers []Handler
	slots    []chan struct{}
	timeout  time.Duration
	maxDepth int
	dedupe   dedupe
}

type dedupe struct {
	mu       sync.Mutex
	capacity int
	seen     map[string]struct{}
	order    []string
	next     int
}

type depthKey struct{}

// New creates an isolated dispatcher. Zero option fields select bounded
// defaults; negative or excessive values are rejected.
func New(options Options, handlers ...Handler) (*Dispatcher, error) {
	if options.HandlerTimeout < 0 || options.HandlerTimeout > maxHandlerTimeout || options.MaxDepth < 0 || options.MaxDepth > maxDepth || options.DedupeCapacity < 0 || options.DedupeCapacity > maxDedupeCapacity || len(handlers) > maxHandlers {
		return nil, errors.New("invalid hook dispatcher options")
	}
	for _, handler := range handlers {
		if handler == nil {
			return nil, errors.New("invalid hook handler")
		}
	}
	if options.HandlerTimeout == 0 {
		options.HandlerTimeout = 100 * time.Millisecond
	}
	if options.MaxDepth == 0 {
		options.MaxDepth = 4
	}
	if options.DedupeCapacity == 0 {
		options.DedupeCapacity = 1024
	}
	dispatcher := &Dispatcher{
		handlers: append([]Handler(nil), handlers...), timeout: options.HandlerTimeout, maxDepth: options.MaxDepth,
		slots:  make([]chan struct{}, len(handlers)),
		dedupe: dedupe{capacity: options.DedupeCapacity, seen: make(map[string]struct{}, options.DedupeCapacity), order: make([]string, options.DedupeCapacity)},
	}
	for index := range dispatcher.slots {
		dispatcher.slots[index] = make(chan struct{}, options.MaxDepth)
	}
	return dispatcher, nil
}

// Dispatch notifies every handler in registration order. Invalid events,
// duplicate identities, observer failures, panics, and timeouts never become
// operation errors.
func (dispatcher *Dispatcher) Dispatch(ctx context.Context, event Event) []Diagnostic {
	if dispatcher == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !validEvent(event) {
		return []Diagnostic{{Handler: -1, Kind: DiagnosticInvalid}}
	}
	depth, _ := ctx.Value(depthKey{}).(int)
	if depth >= dispatcher.maxDepth {
		return []Diagnostic{{Handler: -1, Kind: DiagnosticDepth}}
	}
	identity := string(event.Name()) + "\x00" + event.metadata().ID
	if !dispatcher.dedupe.admit(identity) {
		return nil
	}
	if len(dispatcher.handlers) == 0 {
		return nil
	}
	handlerContext := context.WithValue(ctx, depthKey{}, depth+1)
	diagnostics := make([]Diagnostic, 0)
	for index, handler := range dispatcher.handlers {
		if kind := dispatcher.call(handlerContext, index, handler, event.clone()); kind != "" {
			diagnostics = append(diagnostics, Diagnostic{Handler: index, Kind: kind})
		}
	}
	return diagnostics
}

func (dispatcher *Dispatcher) call(parent context.Context, index int, handler Handler, event Event) DiagnosticKind {
	ctx, cancel := context.WithTimeout(parent, dispatcher.timeout)
	defer cancel()
	select {
	case dispatcher.slots[index] <- struct{}{}:
	case <-ctx.Done():
		return DiagnosticTimeout
	}
	result := make(chan DiagnosticKind, 1)
	go func() {
		kind := DiagnosticKind("")
		defer func() {
			<-dispatcher.slots[index]
			if recover() != nil {
				kind = DiagnosticPanic
			}
			result <- kind
		}()
		if handler(ctx, event) != nil {
			kind = DiagnosticError
		}
	}()
	select {
	case kind := <-result:
		return kind
	case <-ctx.Done():
		return DiagnosticTimeout
	}
}

func (set *dedupe) admit(identity string) bool {
	set.mu.Lock()
	defer set.mu.Unlock()
	if _, exists := set.seen[identity]; exists {
		return false
	}
	if len(set.seen) == set.capacity {
		delete(set.seen, set.order[set.next])
	} else if len(set.seen) < set.capacity {
		set.next = len(set.seen)
	}
	set.seen[identity] = struct{}{}
	set.order[set.next] = identity
	set.next = (set.next + 1) % set.capacity
	return true
}

func validEvent(event Event) bool {
	switch event.(type) {
	case TaskStarted, TaskSucceeded, TaskFailed, CandidateFrozen, ValidationCompleted, DeliveryInstalled:
	default:
		return false
	}
	meta := event.metadata()
	return validID(meta.ID) && !meta.At.IsZero() && meta.At.Location() == time.UTC && event.validatePayload()
}

func validMode(mode Mode) bool { return mode == ModeForeground || mode == ModeBackground }

func validExitCode(value int) bool { return value >= -1 && value <= 255 }

func validCount(value int) bool { return value >= 0 && value <= maxCount }

func validID(value string) bool {
	return len(value) > 0 && len(value) <= maxIdentityBytes && validIdentity.MatchString(value)
}

func validDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
