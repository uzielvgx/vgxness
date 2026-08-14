package tui

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/vgxness/vgxness/internal/hooks"
)

const (
	listenerID              = hooks.ListenerID("tui-session-activity")
	sessionActivityCapacity = 64
)

var (
	sessionActivityNames = [...]hooks.Name{
		hooks.NameChangeCreated,
		hooks.NameRevisionAccepted,
		hooks.NameChangeTransitioned,
		hooks.NameProjectionRecorded,
		hooks.NameMemorySaved,
		hooks.NameMemoryForgotten,
		hooks.NameMemorySyncCompleted,
		hooks.NameIntegrationPreviewCompleted,
		hooks.NameIntegrationInstallCompleted,
		hooks.NameIntegrationStatusCompleted,
		hooks.NameIntegrationUninstallCompleted,
	}
	sessionActivityKinds = map[hooks.Name]string{
		hooks.NameChangeCreated:                 "change",
		hooks.NameRevisionAccepted:              "artifactRevision",
		hooks.NameChangeTransitioned:            "change",
		hooks.NameProjectionRecorded:            "projection",
		hooks.NameMemorySaved:                   "memoryEntry",
		hooks.NameMemoryForgotten:               "memoryEntry",
		hooks.NameMemorySyncCompleted:           "memorySync",
		hooks.NameIntegrationPreviewCompleted:   "integrationProvider",
		hooks.NameIntegrationInstallCompleted:   "integrationProvider",
		hooks.NameIntegrationStatusCompleted:    "integrationProvider",
		hooks.NameIntegrationUninstallCompleted: "integrationProvider",
	}
)

type activityRegistrar interface {
	Register(hooks.ListenerID, hooks.Listener, ...hooks.Name) error
	Unregister(hooks.ListenerID) bool
}

type activityMsg struct{ event hooks.Event }

type activitySubject struct{ kind, id string }

type activityRow struct {
	name       hooks.Name
	occurredAt time.Time
	subject    activitySubject
	alias      uint64
}

// sessionActivity transfers sealed events to the TUI without retaining or
// displaying any event fields beyond those explicitly accepted by Model.Update.
type sessionActivity struct {
	program *tea.Program
	events  chan hooks.Event
	stop    chan struct{}
	done    chan struct{}
	stopped atomic.Bool
	once    sync.Once
	mu      sync.Mutex
	started bool
}

func newSessionActivity(program *tea.Program) *sessionActivity {
	activity := &sessionActivity{
		program: program,
		events:  make(chan hooks.Event, sessionActivityCapacity),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	return activity
}

func (activity *sessionActivity) start() bool {
	activity.mu.Lock()
	defer activity.mu.Unlock()
	if activity.stopped.Load() || activity.started {
		return false
	}
	activity.started = true
	go activity.pump()
	return true
}

func (activity *sessionActivity) listen(_ context.Context, event hooks.Event) error {
	if activity.stopped.Load() {
		return nil
	}
	select {
	case activity.events <- event:
	default:
	}
	return nil
}

func (activity *sessionActivity) pump() {
	defer close(activity.done)
	for {
		select {
		case <-activity.stop:
			return
		case event := <-activity.events:
			if !activity.stopped.Load() {
				activity.program.Send(activityMsg{event: event})
			}
		}
	}
}

func (activity *sessionActivity) shutdown(registry activityRegistrar) {
	activity.once.Do(func() {
		activity.stopped.Store(true)
		if registry != nil {
			registry.Unregister(listenerID)
		}
		activity.mu.Lock()
		started := activity.started
		close(activity.stop)
		activity.mu.Unlock()
		if !started {
			return
		}
		<-activity.done
	})
}

func attachSessionActivity(backend Backend, program *tea.Program) func() {
	return attachSessionActivityWithFactory(backend, program, newSessionActivity)
}

func attachSessionActivityWithFactory(backend Backend, program *tea.Program, factory func(*tea.Program) *sessionActivity) func() {
	registry, ok := backend.(activityRegistrar)
	if !ok || program == nil || factory == nil {
		return func() {}
	}
	activity := factory(program)
	if activity == nil {
		return func() {}
	}
	if err := registry.Register(listenerID, activity.listen, sessionActivityNames[:]...); err != nil {
		activity.shutdown(nil)
		return func() {}
	}
	activity.start()
	return func() { activity.shutdown(registry) }
}

func (m *Model) addActivity(event hooks.Event) {
	name := event.Name()
	kind, known := sessionActivityKinds[name]
	if !known {
		return
	}
	occurredAt := event.OccurredAt().UTC()
	subject := event.Subject()
	if occurredAt.IsZero() || subject.Kind() != kind || subject.ID() == "" {
		return
	}
	key := activitySubject{kind: subject.Kind(), id: subject.ID()}
	alias, retained := m.activityAliases[key]
	if !retained {
		if m.activityAlias == ^uint64(0) {
			return
		}
		m.activityAlias++
		alias = m.activityAlias
		m.activityAliases[key] = alias
	}
	m.activity = append(m.activity, activityRow{name: name, occurredAt: occurredAt, subject: key, alias: alias})
	if len(m.activity) <= sessionActivityCapacity {
		return
	}
	evicted := m.activity[0]
	m.activity = m.activity[1:]
	for _, row := range m.activity {
		if row.subject == evicted.subject {
			return
		}
	}
	delete(m.activityAliases, evicted.subject)
}

func (m Model) renderActivity() []string {
	lines := []string{"SESSION ACTIVITY", "  This TUI session; last 64 lifecycle events. Best effort; events may be omitted."}
	if len(m.activity) == 0 {
		return append(lines, "! No session activity yet.")
	}
	for _, row := range m.activity {
		lines = append(lines, fmt.Sprintf("- %s %s %s#%d", row.occurredAt.Format("15:04:05"), row.name, row.subject.kind, row.alias))
	}
	return lines
}
