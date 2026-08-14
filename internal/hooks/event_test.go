package hooks

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEventMaterializesClosedChangeCreatedEvent(t *testing.T) {
	draft, err := NewChangeCreated("project-1", "change-1", "apply", "active", 16)
	if err != nil {
		t.Fatalf("NewChangeCreated() error = %v", err)
	}

	event, err := newEvent(draft, "event-1", time.Date(2026, 8, 14, 12, 0, 0, 123, time.FixedZone("offset", -6*60*60)), 1, "correlation-1")
	if err != nil {
		t.Fatalf("newEvent() error = %v", err)
	}
	if got, want := event.SchemaVersion(), "1.0"; got != want {
		t.Errorf("SchemaVersion() = %q, want %q", got, want)
	}
	if got, want := event.Name(), Name("change.created"); got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
	if got, want := event.Operation(), Operation("CreateChange"); got != want {
		t.Errorf("Operation() = %q, want %q", got, want)
	}
	if got, want := event.Outcome(), "completed"; got != want {
		t.Errorf("Outcome() = %q, want %q", got, want)
	}
	if got, ok := event.ProjectID(); !ok || got != "project-1" {
		t.Errorf("ProjectID() = %q, %t", got, ok)
	}
	if got := event.Subject(); got.Kind() != "change" || got.ID() != "change-1" {
		t.Errorf("Subject() = %q/%q", got.Kind(), got.ID())
	}
	if got, ok := event.Change(); !ok || got.Phase() != "apply" || got.Status() != "active" || got.StateVersion() != 16 {
		t.Errorf("Change() = %#v, %t", got, ok)
	}

	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	const want = `{"schemaVersion":1.0,"name":"change.created","eventId":"event-1","occurredAt":"2026-08-14T18:00:00.000000123Z","sequence":1,"correlationId":"correlation-1","operation":"CreateChange","outcome":"completed","projectId":"project-1","subject":{"kind":"change","id":"change-1"},"result":{"phase":"apply","status":"active","stateVersion":16}}`
	if got := string(encoded); got != want {
		t.Errorf("json.Marshal() = %s\nwant %s", got, want)
	}
}

func TestDraftRejectsInvalidSourceValues(t *testing.T) {
	if _, err := NewChangeCreated("project-1", "change-1", "apply", "active", 0); err == nil {
		t.Error("NewChangeCreated accepted zero state version")
	}
	if _, err := NewRevisionAccepted("project-1", "change-1", "artifact-1", "revision-1", "task", "accepted", "bad", "", 1); err == nil {
		t.Error("NewRevisionAccepted accepted malformed digest")
	}
	if _, err := NewMemoryForgotten("project-1", "entry-1", "project", "fact", "active", time.Now(), time.Now()); err == nil {
		t.Error("NewMemoryForgotten accepted non-archived state")
	}
	if _, err := NewMemorySyncCompleted("project-1", "completed", -1, 0, 0, 0, 0, 0); err == nil {
		t.Error("NewMemorySyncCompleted accepted negative count")
	}
}

func TestDraftConstructorsUseClosedNamesAndOperations(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	hash := strings.Repeat("a", 64)
	tests := []struct {
		name        string
		draft       Draft
		err         error
		wantName    Name
		wantOp      Operation
		wantProject bool
	}{
		{"revision", mustRevision(t, hash), nil, "artifact.revision.accepted", "AcceptRevision", true},
		{"transition", mustTransition(t), nil, "change.transitioned", "TransitionChange", true},
		{"projection", mustProjection(t, hash), nil, "projection.recorded", "RecordProjection", true},
		{"saved", mustMemorySaved(t, now), nil, "memory.saved", "Remember", true},
		{"forgotten", mustMemoryForgotten(t, now), nil, "memory.forgotten", "Forget", true},
		{"sync", mustMemorySync(t), nil, "memory.sync.completed", "Sync", true},
		{"preview", mustIntegration(t, NameIntegrationPreviewCompleted, hash), nil, "integration.preview.completed", "Preview", false},
		{"install", mustIntegration(t, NameIntegrationInstallCompleted, hash), nil, "integration.install.completed", "Install", false},
		{"status", mustIntegration(t, NameIntegrationStatusCompleted, hash), nil, "integration.status.completed", "Status", false},
		{"uninstall", mustIntegration(t, NameIntegrationUninstallCompleted, hash), nil, "integration.uninstall.completed", "Uninstall", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.err != nil {
				t.Fatal(test.err)
			}
			if test.draft.name != test.wantName || test.draft.operation != test.wantOp {
				t.Errorf("draft = %q/%q, want %q/%q", test.draft.name, test.draft.operation, test.wantName, test.wantOp)
			}
			if got := test.draft.projectID != ""; got != test.wantProject {
				t.Errorf("has project = %t, want %t", got, test.wantProject)
			}
		})
	}
}

func mustRevision(t *testing.T, hash string) Draft {
	t.Helper()
	d, err := NewRevisionAccepted("project-1", "change-1", "artifact-1", "revision-1", "task", "accepted", hash, hash, 1)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func mustTransition(t *testing.T) Draft {
	t.Helper()
	d, err := NewChangeTransitioned("project-1", "change-1", "apply", "active", 1)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func mustProjection(t *testing.T, hash string) Draft {
	t.Helper()
	d, err := NewProjectionRecorded("project-1", "change-1", "artifact-1", "revision-1", "accepted", hash, 1)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func mustMemorySaved(t *testing.T, now time.Time) Draft {
	t.Helper()
	d, err := NewMemorySaved("project-1", "entry-1", "project", "fact", "active", now, now)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func mustMemoryForgotten(t *testing.T, now time.Time) Draft {
	t.Helper()
	d, err := NewMemoryForgotten("project-1", "entry-1", "project", "fact", "archived", now, now)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func mustMemorySync(t *testing.T) Draft {
	t.Helper()
	d, err := NewMemorySyncCompleted("project-1", "completed", 1, 2, 3, 4, 5, 6)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func mustIntegration(t *testing.T, name Name, hash string) Draft {
	t.Helper()
	var d Draft
	var err error
	switch name {
	case NameIntegrationPreviewCompleted:
		d, err = NewIntegrationPreviewCompleted("integration-1", "installed", true, hash, 1, false)
	case NameIntegrationInstallCompleted:
		d, err = NewIntegrationInstallCompleted("integration-1", "installed", true, hash, 1, false)
	case NameIntegrationStatusCompleted:
		d, err = NewIntegrationStatusCompleted("integration-1", "installed", true, hash, 1, false)
	case NameIntegrationUninstallCompleted:
		d, err = NewIntegrationUninstallCompleted("integration-1", "installed", true, hash, 1, false)
	}
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestCorrectionRejectsForgedValuesAndUnsafeText(t *testing.T) {
	valid, err := NewChangeCreated("project-1", "change-1", "apply", "active", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"bad\u2028value", "bad\u200fvalue", string([]byte{'b', 0xff})} {
		if _, err := NewChangeCreated(value, "change-1", "apply", "active", 1); err == nil {
			t.Errorf("NewChangeCreated accepted %q", value)
		}
		if _, err := NewChangeCreated("project-1", "change-1", value, "active", 1); err == nil {
			t.Errorf("NewChangeCreated accepted label %q", value)
		}
	}
	if _, err := NewChangeCreated(strings.Repeat("p", 257), "change-1", "apply", "active", 1); err == nil {
		t.Error("NewChangeCreated accepted oversized project")
	}
	if _, err := newEvent(Draft{name: NameChangeCreated, operation: OperationCreateChange, projectID: "project-1", subject: Subject{"change", "change-1"}, kind: resultChange}, "event-1", time.Now(), 1, ""); err == nil {
		t.Error("newEvent accepted forged empty change result")
	}
	if _, err := newEvent(valid, "event-1", time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), 1, ""); err == nil {
		t.Error("newEvent accepted year 10000")
	}
	for _, event := range []Event{
		{name: NameChangeCreated, operation: OperationCreateChange, projectID: "project-1", subject: Subject{"change", "change-1"}, kind: resultChange, eventID: "event-1", occurredAt: time.Now(), sequence: 1},
		{name: NameChangeCreated, operation: OperationCreateChange, projectID: "project-1", subject: Subject{"change", "change-1"}, kind: resultChange, change: valid.change, eventID: "event-1", occurredAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), sequence: 1},
		{name: NameChangeCreated, operation: OperationCreateChange, projectID: "project-1", subject: Subject{"change", "change-1"}, kind: resultChange, change: valid.change, eventID: "event-1", sequence: 1},
	} {
		if _, err := json.Marshal(event); err == nil {
			t.Error("MarshalJSON accepted forged invalid event")
		}
	}
	if _, err := NewMemorySaved("project-1", "entry-1", "project", "fact", "active", time.Time{}, time.Now()); err == nil {
		t.Error("NewMemorySaved accepted zero timestamp")
	}
	if _, err := NewMemorySaved("project-1", "entry-1", "project", "fact", "active", time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), time.Now()); err == nil {
		t.Error("NewMemorySaved accepted year 10000")
	}
}

func TestCorrectionUsesAcceptedSubjects(t *testing.T) {
	hash := strings.Repeat("a", 64)
	projection := mustProjection(t, hash)
	if got := projection.subject; got.Kind() != "projection" || got.ID() != "artifact-1" {
		t.Errorf("projection subject = %q/%q", got.Kind(), got.ID())
	}
	if got := mustMemorySaved(t, time.Now()).subject.Kind(); got != "memoryEntry" {
		t.Errorf("memory subject kind = %q", got)
	}
	if got := mustMemorySync(t).subject; got.Kind() != "memorySync" || got.ID() != "project-1" {
		t.Errorf("sync subject = %q/%q", got.Kind(), got.ID())
	}
	if got := mustIntegration(t, NameIntegrationInstallCompleted, hash).subject.Kind(); got != "integrationProvider" {
		t.Errorf("integration subject kind = %q", got)
	}
}

func TestEventRejectsInvalidCorrelationAndOversizeJSON(t *testing.T) {
	draft, err := NewIntegrationInstallCompleted("integration-1", "installed", true, strings.Repeat("a", 64), 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newEvent(draft, "event-1", time.Now(), 1, "bad\ncorrelation"); err == nil {
		t.Error("newEvent accepted invalid correlation")
	}
	if _, err := NewIntegrationInstallCompleted(strings.Repeat("a", 4090), "installed", false, "", 0, false); err == nil {
		t.Error("NewIntegrationInstallCompleted accepted oversized provider ID")
	}
}
