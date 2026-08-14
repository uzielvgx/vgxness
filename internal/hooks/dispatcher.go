package hooks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

const (
	maxNamesPerListener = 16
	maxListenersPerName = 64
)

var (
	ErrInvalidListenerID = errors.New("invalid hook listener ID")
	ErrNilListener       = errors.New("hook listener is nil")
	ErrInvalidName       = errors.New("invalid hook event name")
	ErrListenerNameLimit = errors.New("hook listener name limit reached")
	ErrListenerLimit     = errors.New("hook event listener limit reached")
	ErrClosed            = errors.New("hook dispatcher is closed")
)

type ListenerID string
type Listener func(context.Context, Event) error
type Emitter interface{ Emit(context.Context, Draft) }

type listenerRecord struct {
	listener Listener
	names    map[Name]struct{}
}

// Dispatcher is synchronous and must not be copied after first use.
type Dispatcher struct {
	mu         sync.Mutex
	clock      func() time.Time
	eventID    func() (string, error)
	listeners  map[ListenerID]*listenerRecord
	byName     map[Name][]ListenerID
	sequence   uint64
	delivering bool
	closed     bool
}

func New() *Dispatcher { return NewForTest(time.Now, randomEventID) }
func NewForTest(clock func() time.Time, eventID func() (string, error)) *Dispatcher {
	if clock == nil {
		clock = time.Now
	}
	if eventID == nil {
		eventID = randomEventID
	}
	return &Dispatcher{clock: clock, eventID: eventID, listeners: make(map[ListenerID]*listenerRecord), byName: make(map[Name][]ListenerID)}
}

func (d *Dispatcher) Register(id ListenerID, listener Listener, names ...Name) error {
	if !validID(string(id)) {
		return ErrInvalidListenerID
	}
	if listener == nil {
		return ErrNilListener
	}
	if len(names) > maxNamesPerListener {
		return ErrListenerNameLimit
	}
	unique := make(map[Name]struct{}, len(names))
	for _, name := range names {
		if !knownName(name) {
			return ErrInvalidName
		}
		unique[name] = struct{}{}
	}
	if len(unique) == 0 {
		return ErrInvalidName
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return ErrClosed
	}
	d.ensureMaps()
	record := d.listeners[id]
	current := 0
	if record != nil {
		current = len(record.names)
	}
	newNames := make([]Name, 0, len(unique))
	for name := range unique {
		if record == nil || !hasName(record.names, name) {
			newNames = append(newNames, name)
		}
	}
	if current+len(newNames) > maxNamesPerListener {
		return ErrListenerNameLimit
	}
	for _, name := range newNames {
		if len(d.byName[name]) >= maxListenersPerName {
			return ErrListenerLimit
		}
	}
	if record == nil {
		record = &listenerRecord{listener: listener, names: make(map[Name]struct{})}
		d.listeners[id] = record
	}
	for _, name := range newNames {
		record.names[name] = struct{}{}
		d.byName[name] = append(d.byName[name], id)
	}
	return nil
}

func (d *Dispatcher) Unregister(id ListenerID) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	record := d.listeners[id]
	if record == nil {
		return false
	}
	for name := range record.names {
		listeners := d.byName[name]
		for i, listenerID := range listeners {
			if listenerID == id {
				d.byName[name] = append(listeners[:i], listeners[i+1:]...)
				break
			}
		}
	}
	delete(d.listeners, id)
	return true
}

// Emit delivers a sealed event synchronously. Invalid input and listener failures are suppressed.
func (d *Dispatcher) Emit(ctx context.Context, draft Draft) {
	returned := false
	defer func() {
		if !returned {
			// Hook sealing must fail open; discard any panic value.
			recover()
		}
	}()
	if ctx == nil || !validDraft(draft) {
		return
	}
	d.mu.Lock()
	if d.closed || d.delivering || d.sequence == ^uint64(0) {
		d.mu.Unlock()
		return
	}
	d.ensureMaps()
	ids := d.byName[draft.name]
	if len(ids) == 0 {
		d.mu.Unlock()
		return
	}
	listeners := make([]Listener, 0, len(ids))
	for _, listenerID := range ids {
		listeners = append(listeners, d.listeners[listenerID].listener)
	}
	d.delivering = true
	nextSequence := d.sequence + 1
	d.mu.Unlock()
	defer func() { d.mu.Lock(); d.delivering = false; d.mu.Unlock() }()

	id, err := d.eventID()
	if err != nil {
		return
	}
	event, err := newEvent(draft, id, d.clock(), nextSequence, correlationID(ctx))
	if err != nil {
		return
	}
	d.mu.Lock()
	d.sequence = nextSequence
	d.mu.Unlock()
	listenerCtx := listenerContext(ctx)
	for _, listener := range listeners {
		if listenerCtx.Err() != nil || !d.listenerAdmitted() {
			return
		}
		callListener(listener, listenerCtx, event)
	}
	returned = true
}

func (d *Dispatcher) Close() { d.mu.Lock(); d.closed = true; d.mu.Unlock() }

// listenerAdmitted atomically admits the next listener. An admitted listener is
// already started for Close purposes; Close remains deliberately non-preemptive.
func (d *Dispatcher) listenerAdmitted() bool { d.mu.Lock(); defer d.mu.Unlock(); return !d.closed }
func (d *Dispatcher) ensureMaps() {
	if d.listeners == nil {
		d.listeners = make(map[ListenerID]*listenerRecord)
	}
	if d.byName == nil {
		d.byName = make(map[Name][]ListenerID)
	}
	if d.clock == nil {
		d.clock = time.Now
	}
	if d.eventID == nil {
		d.eventID = randomEventID
	}
}
func hasName(names map[Name]struct{}, name Name) bool { _, ok := names[name]; return ok }
func knownName(name Name) bool {
	switch name {
	case NameChangeCreated, NameRevisionAccepted, NameChangeTransitioned, NameProjectionRecorded, NameMemorySaved, NameMemoryForgotten, NameMemorySyncCompleted, NameIntegrationPreviewCompleted, NameIntegrationInstallCompleted, NameIntegrationStatusCompleted, NameIntegrationUninstallCompleted:
		return true
	}
	return false
}

func callListener(listener Listener, ctx context.Context, event Event) {
	returned := false
	defer func() {
		if !returned {
			recover()
		}
	}()
	_ = listener(ctx, event)
	returned = true
}
func randomEventID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	return hex.EncodeToString(raw[:]), nil
}
