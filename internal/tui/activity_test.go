package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/vgxness/vgxness/internal/hooks"
)

func TestSessionActivityRendersRedactedAlias(t *testing.T) {
	dispatcher := hooks.NewForTest(func() time.Time { return time.Date(2026, 8, 14, 10, 9, 8, 0, time.UTC) }, func() (string, error) { return "activity-test-event", nil })
	var event hooks.Event
	if err := dispatcher.Register("activity-test", func(_ context.Context, value hooks.Event) error { event = value; return nil }, hooks.NameMemorySaved); err != nil {
		t.Fatal(err)
	}
	draft, err := hooks.NewMemorySaved("project-test", "activity-test-subject", "project", "decision", "active", time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC), time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	dispatcher.Emit(context.Background(), draft)

	model := NewModel(context.Background(), fakeBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 100, Height: 40})
	model = updateModel(t, model, activityMsg{event: event})
	view := model.View().Content
	if !strings.Contains(view, "SESSION ACTIVITY") || !strings.Contains(view, "- 10:09:08 memory.saved memoryEntry#") {
		t.Fatalf("activity not rendered:\n%s", view)
	}
	if strings.Contains(view, "activity-test-subject") || strings.Contains(view, "activity-test-event") || strings.Contains(view, "project-test") {
		t.Fatalf("activity leaked sealed fields:\n%s", view)
	}
}

func TestSessionActivityRegistersExactNameKinds(t *testing.T) {
	if len(sessionActivityNames) != 11 || len(sessionActivityKinds) != 11 {
		t.Fatalf("names=%d kinds=%d", len(sessionActivityNames), len(sessionActivityKinds))
	}
	for _, name := range sessionActivityNames {
		if sessionActivityKinds[name] == "" {
			t.Fatalf("missing subject kind for %q", name)
		}
	}
}

func TestSessionActivityUsesSessionLocalAliasesAndRejectsExhaustion(t *testing.T) {
	event := activityTestEvent(t, hooks.NameMemorySaved, "subject-retained", time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC))
	first := NewModel(context.Background(), fakeBackend{}, Options{})
	first = updateModel(t, first, activityMsg{event: event})
	if first.activity[0].alias != 1 {
		t.Fatalf("first alias=%d want=1", first.activity[0].alias)
	}
	second := NewModel(context.Background(), fakeBackend{}, Options{})
	second = updateModel(t, second, activityMsg{event: event})
	if second.activity[0].alias != 1 {
		t.Fatalf("second-session alias=%d want=1", second.activity[0].alias)
	}
	for index := 0; index < sessionActivityCapacity; index++ {
		second = updateModel(t, second, activityMsg{event: activityTestEvent(t, hooks.NameMemorySaved, fmt.Sprintf("subject-%d", index), time.Date(2026, 8, 14, 10, 1, index, 0, time.UTC))})
	}
	second = updateModel(t, second, activityMsg{event: event})
	if got := second.activity[len(second.activity)-1].alias; got != 66 {
		t.Fatalf("re-added evicted alias=%d want=66", got)
	}
	exhausted := NewModel(context.Background(), fakeBackend{}, Options{})
	exhausted.activityAlias = ^uint64(0)
	exhausted = updateModel(t, exhausted, activityMsg{event: event})
	if len(exhausted.activity) != 0 {
		t.Fatalf("exhausted alias counter retained activity=%d", len(exhausted.activity))
	}
}

func TestSessionActivityAcceptsAllNamesInDeliveryOrder(t *testing.T) {
	model := NewModel(context.Background(), fakeBackend{}, Options{})
	base := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	for index, name := range sessionActivityNames {
		model = updateModel(t, model, activityMsg{event: activityTestEvent(t, name, fmt.Sprintf("subject-%d", index), base.Add(time.Duration(index)*time.Second))})
	}
	if len(model.activity) != len(sessionActivityNames) {
		t.Fatalf("activity length=%d want=%d", len(model.activity), len(sessionActivityNames))
	}
	for index, row := range model.activity {
		if row.name != sessionActivityNames[index] || row.subject.kind != sessionActivityKinds[row.name] {
			t.Fatalf("row %d = %q/%q", index, row.name, row.subject.kind)
		}
	}
}

func TestSessionActivityOverviewPlacementUTCAndRoutePersistence(t *testing.T) {
	model := NewModel(context.Background(), fakeBackend{}, Options{})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 120, Height: 40})
	zone := time.FixedZone("test", -7*60*60)
	model = updateModel(t, model, activityMsg{event: activityTestEvent(t, hooks.NameMemorySaved, "subject-zone", time.Date(2026, 8, 14, 10, 9, 8, 0, zone))})
	view := model.View().Content
	setupAt, activityAt, memoryAt := strings.Index(view, "SETUP"), strings.Index(view, "SESSION ACTIVITY"), strings.Index(view, "RECENT PROJECT MEMORY")
	if setupAt < 0 || activityAt < 0 || memoryAt < 0 || !(setupAt < activityAt && activityAt < memoryAt) || !strings.Contains(view, "  This TUI session; last 64 lifecycle events. Best effort; events may be omitted.") || !strings.Contains(view, "- 17:09:08 memory.saved memoryEntry#1") {
		t.Fatalf("overview activity placement/copy invalid:\n%s", view)
	}
	for _, current := range []route{routeSystem, routeMemory, routeSetup} {
		model.setRoute(current)
		if strings.Contains(model.View().Content, "SESSION ACTIVITY") {
			t.Fatalf("route %v rendered session activity", current)
		}
	}
	model.setRoute(routeOverview)
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: 'r', Text: "r"}))
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 100, Height: 40})
	if len(model.activity) != 1 || !strings.Contains(model.View().Content, "SESSION ACTIVITY") {
		t.Fatal("activity did not persist across navigation, refresh, and resize")
	}
	if view := NewModel(context.Background(), fakeBackend{}, Options{}).renderActivity(); !strings.Contains(strings.Join(view, "\n"), "! No session activity yet.") {
		t.Fatalf("fresh model activity=%q", view)
	}
}

func TestSessionActivityListenerDropsNewEventsAndStopsOnce(t *testing.T) {
	listener := &sessionActivity{events: make(chan hooks.Event, sessionActivityCapacity)}
	for index := 0; index < sessionActivityCapacity+1; index++ {
		if err := listener.listen(context.Background(), hooks.Event{}); err != nil {
			t.Fatalf("listen() = %v", err)
		}
	}
	if len(listener.events) != sessionActivityCapacity {
		t.Fatalf("queued events=%d want=%d", len(listener.events), sessionActivityCapacity)
	}
	activity := newSessionActivity(tea.NewProgram(NewModel(context.Background(), fakeBackend{}, Options{})))
	if !activity.start() || activity.start() {
		t.Fatal("pump start was not exactly once")
	}
	registry := &activityTestRegistry{}
	activity.shutdown(registry)
	activity.shutdown(registry)
	if registry.unregisters != 1 {
		t.Fatalf("unregisters=%d want=1", registry.unregisters)
	}
}

func TestAttachSessionActivityRegistersBridgeAndFailsOpen(t *testing.T) {
	program := tea.NewProgram(NewModel(context.Background(), fakeBackend{}, Options{}))
	registry := &activityTestRegistry{}
	var successful *sessionActivity
	stop := attachSessionActivityWithFactory(activityTestBackend{activityTestRegistry: registry}, program, func(program *tea.Program) *sessionActivity {
		successful = newSessionActivity(program)
		return successful
	})
	if registry.registrations != 1 || registry.id != listenerID || registry.listener == nil || len(registry.names) != len(sessionActivityNames) {
		t.Fatalf("registration=%d id=%q listener=%v names=%v", registry.registrations, registry.id, registry.listener != nil, registry.names)
	}
	if successful == nil || !successful.started || successful.start() {
		t.Fatal("successful registration did not start exactly one pump")
	}
	stop()
	stop()
	if registry.unregisters != 1 {
		t.Fatalf("unregisters=%d want=1", registry.unregisters)
	}
	attachSessionActivity(fakeBackend{}, program)()
	failing := &activityTestRegistry{registerErr: fmt.Errorf("registry unavailable")}
	var failed *sessionActivity
	attachSessionActivityWithFactory(activityTestBackend{activityTestRegistry: failing}, program, func(program *tea.Program) *sessionActivity {
		failed = newSessionActivity(program)
		return failed
	})()
	if failing.registrations != 1 || failing.unregisters != 0 || failed == nil || failed.started {
		t.Fatalf("fail-open registrations=%d unregisters=%d", failing.registrations, failing.unregisters)
	}
}

type activityTestBackend struct {
	fakeBackend
	*activityTestRegistry
}

type activityTestRegistry struct {
	registerErr   error
	registrations int
	unregisters   int
	id            hooks.ListenerID
	listener      hooks.Listener
	names         []hooks.Name
}

func (registry *activityTestRegistry) Register(id hooks.ListenerID, listener hooks.Listener, names ...hooks.Name) error {
	registry.registrations++
	registry.id, registry.listener = id, listener
	registry.names = append([]hooks.Name(nil), names...)
	return registry.registerErr
}
func (registry *activityTestRegistry) Unregister(hooks.ListenerID) bool {
	registry.unregisters++
	return true
}

func TestSessionActivityIgnoresInvalidEventsAndKeepsLastSixtyFour(t *testing.T) {
	model := NewModel(context.Background(), fakeBackend{}, Options{Workspace: "/workspace"})
	for index := 0; index < 65; index++ {
		event := activityTestEvent(t, hooks.NameMemorySaved, fmt.Sprintf("subject-%d", index), time.Date(2026, 8, 14, 10, 0, index, 0, time.UTC))
		model = updateModel(t, model, activityMsg{event: event})
	}
	if len(model.activity) != sessionActivityCapacity {
		t.Fatalf("activity length=%d want=%d", len(model.activity), sessionActivityCapacity)
	}
	if model.activity[len(model.activity)-1].alias-model.activity[0].alias != sessionActivityCapacity-1 {
		t.Fatalf("activity aliases first=%d last=%d", model.activity[0].alias, model.activity[len(model.activity)-1].alias)
	}
	for index, row := range model.activity {
		if want := fmt.Sprintf("subject-%d", index+1); row.subject.id != want || row.name != hooks.NameMemorySaved {
			t.Fatalf("FIFO row %d = %q/%q want %q/%q", index, row.subject.id, row.name, want, hooks.NameMemorySaved)
		}
	}
	model = updateModel(t, model, activityMsg{})
	if len(model.activity) != sessionActivityCapacity {
		t.Fatalf("invalid event changed activity length=%d", len(model.activity))
	}
}

func activityTestEvent(t *testing.T, name hooks.Name, id string, occurredAt time.Time) hooks.Event {
	t.Helper()
	dispatcher := hooks.NewForTest(func() time.Time { return occurredAt }, func() (string, error) { return "activity-test-event", nil })
	var event hooks.Event
	if err := dispatcher.Register("activity-test", func(_ context.Context, value hooks.Event) error { event = value; return nil }, name); err != nil {
		t.Fatal(err)
	}
	draft, err := activityTestDraft(name, id, occurredAt)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher.Emit(context.Background(), draft)
	return event
}

func activityTestDraft(name hooks.Name, id string, occurredAt time.Time) (hooks.Draft, error) {
	const hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	switch name {
	case hooks.NameChangeCreated:
		return hooks.NewChangeCreated("project-test", id, "apply", "active", 1)
	case hooks.NameRevisionAccepted:
		return hooks.NewRevisionAccepted("project-test", "change-test", "artifact-test", id, "tasks", "accepted", hash, hash, 1)
	case hooks.NameChangeTransitioned:
		return hooks.NewChangeTransitioned("project-test", id, "apply", "active", 1)
	case hooks.NameProjectionRecorded:
		return hooks.NewProjectionRecorded("project-test", "change-test", id, "revision-test", "recorded", hash, 1)
	case hooks.NameMemorySaved:
		return hooks.NewMemorySaved("project-test", id, "project", "decision", "active", occurredAt, occurredAt)
	case hooks.NameMemoryForgotten:
		return hooks.NewMemoryForgotten("project-test", id, "project", "decision", "archived", occurredAt, occurredAt)
	case hooks.NameMemorySyncCompleted:
		return hooks.NewMemorySyncCompleted("project-test", "completed", 0, 0, 0, 0, 0, 0)
	case hooks.NameIntegrationPreviewCompleted:
		return hooks.NewIntegrationPreviewCompleted(id, "installed", false, "", 0, false)
	case hooks.NameIntegrationInstallCompleted:
		return hooks.NewIntegrationInstallCompleted(id, "installed", false, "", 0, false)
	case hooks.NameIntegrationStatusCompleted:
		return hooks.NewIntegrationStatusCompleted(id, "installed", false, "", 0, false)
	case hooks.NameIntegrationUninstallCompleted:
		return hooks.NewIntegrationUninstallCompleted(id, "installed", false, "", 0, false)
	default:
		return hooks.Draft{}, fmt.Errorf("unsupported event name %q", name)
	}
}
