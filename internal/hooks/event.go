// Package hooks defines the closed, listener-visible lifecycle event values.
package hooks

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxEventEnvelopeBytes = 4096
	maxIDBytes            = 128
	maxProjectBytes       = 256
	maxLabelBytes         = 64
)

const schemaVersionText = "1.0"

var errInvalidEvent = errors.New("invalid hook event")

type Name string
type Operation string

const (
	NameChangeCreated                 Name = "change.created"
	NameRevisionAccepted              Name = "artifact.revision.accepted"
	NameChangeTransitioned            Name = "change.transitioned"
	NameProjectionRecorded            Name = "projection.recorded"
	NameMemorySaved                   Name = "memory.saved"
	NameMemoryForgotten               Name = "memory.forgotten"
	NameMemorySyncCompleted           Name = "memory.sync.completed"
	NameIntegrationPreviewCompleted   Name = "integration.preview.completed"
	NameIntegrationInstallCompleted   Name = "integration.install.completed"
	NameIntegrationStatusCompleted    Name = "integration.status.completed"
	NameIntegrationUninstallCompleted Name = "integration.uninstall.completed"
)

const (
	OperationCreateChange     Operation = "CreateChange"
	OperationAcceptRevision   Operation = "AcceptRevision"
	OperationTransitionChange Operation = "TransitionChange"
	OperationRecordProjection Operation = "RecordProjection"
	OperationRemember         Operation = "Remember"
	OperationForget           Operation = "Forget"
	OperationSync             Operation = "Sync"
	OperationPreview          Operation = "Preview"
	OperationInstall          Operation = "Install"
	OperationStatus           Operation = "Status"
	OperationUninstall        Operation = "Uninstall"
)

// Subject identifies the single lifecycle entity affected by an event.
type Subject struct{ kind, id string }

func (s Subject) Kind() string { return s.kind }
func (s Subject) ID() string   { return s.id }

type ChangeResult struct {
	phase, status string
	stateVersion  int64
}

func (r ChangeResult) Phase() string       { return r.phase }
func (r ChangeResult) Status() string      { return r.status }
func (r ChangeResult) StateVersion() int64 { return r.stateVersion }

type RevisionResult struct {
	changeID, artifactID, artifact, status, digest, inputDigest string
	stateVersion                                                int64
}

func (r RevisionResult) ChangeID() string    { return r.changeID }
func (r RevisionResult) ArtifactID() string  { return r.artifactID }
func (r RevisionResult) Artifact() string    { return r.artifact }
func (r RevisionResult) Status() string      { return r.status }
func (r RevisionResult) Digest() string      { return r.digest }
func (r RevisionResult) InputDigest() string { return r.inputDigest }
func (r RevisionResult) StateVersion() int64 { return r.stateVersion }

type ProjectionResult struct {
	changeID, artifactID, revisionID, status, digest string
	stateVersion                                     int64
}

func (r ProjectionResult) ChangeID() string    { return r.changeID }
func (r ProjectionResult) ArtifactID() string  { return r.artifactID }
func (r ProjectionResult) RevisionID() string  { return r.revisionID }
func (r ProjectionResult) Status() string      { return r.status }
func (r ProjectionResult) Digest() string      { return r.digest }
func (r ProjectionResult) StateVersion() int64 { return r.stateVersion }

type MemoryEntryResult struct {
	scope, typ, state    string
	createdAt, updatedAt time.Time
}

func (r MemoryEntryResult) Scope() string        { return r.scope }
func (r MemoryEntryResult) Type() string         { return r.typ }
func (r MemoryEntryResult) State() string        { return r.state }
func (r MemoryEntryResult) CreatedAt() time.Time { return r.createdAt }
func (r MemoryEntryResult) UpdatedAt() time.Time { return r.updatedAt }

type MemorySyncResult struct {
	status                                                            string
	pushed, previouslyAccepted, rejected, retried, conflicts, batches int64
}

func (r MemorySyncResult) Status() string            { return r.status }
func (r MemorySyncResult) Pushed() int64             { return r.pushed }
func (r MemorySyncResult) PreviouslyAccepted() int64 { return r.previouslyAccepted }
func (r MemorySyncResult) Rejected() int64           { return r.rejected }
func (r MemorySyncResult) Retried() int64            { return r.retried }
func (r MemorySyncResult) Conflicts() int64          { return r.conflicts }
func (r MemorySyncResult) Batches() int64            { return r.batches }

type IntegrationResult struct {
	state, artifactSHA256    string
	changed, restartRequired bool
	artifactCount            int64
}

func (r IntegrationResult) State() string          { return r.state }
func (r IntegrationResult) Changed() bool          { return r.changed }
func (r IntegrationResult) ArtifactSHA256() string { return r.artifactSHA256 }
func (r IntegrationResult) ArtifactCount() int64   { return r.artifactCount }
func (r IntegrationResult) RestartRequired() bool  { return r.restartRequired }

type resultKind uint8

const (
	resultNone resultKind = iota
	resultChange
	resultRevision
	resultProjection
	resultMemoryEntry
	resultMemorySync
	resultIntegration
)

// Draft is a trusted, validated event source. It cannot expose or retain domain values.
type Draft struct {
	name        Name
	operation   Operation
	projectID   string
	subject     Subject
	kind        resultKind
	change      ChangeResult
	revision    RevisionResult
	projection  ProjectionResult
	memoryEntry MemoryEntryResult
	memorySync  MemorySyncResult
	integration IntegrationResult
}

type Event struct {
	name          Name
	eventID       string
	occurredAt    time.Time
	sequence      uint64
	correlationID string
	operation     Operation
	projectID     string
	subject       Subject
	kind          resultKind
	change        ChangeResult
	revision      RevisionResult
	projection    ProjectionResult
	memoryEntry   MemoryEntryResult
	memorySync    MemorySyncResult
	integration   IntegrationResult
}

func (e Event) SchemaVersion() string                { return schemaVersionText }
func (e Event) Name() Name                           { return e.name }
func (e Event) EventID() string                      { return e.eventID }
func (e Event) OccurredAt() time.Time                { return e.occurredAt }
func (e Event) Sequence() uint64                     { return e.sequence }
func (e Event) CorrelationID() string                { return e.correlationID }
func (e Event) Operation() Operation                 { return e.operation }
func (e Event) Outcome() string                      { return "completed" }
func (e Event) ProjectID() (string, bool)            { return e.projectID, e.projectID != "" }
func (e Event) Subject() Subject                     { return e.subject }
func (e Event) Change() (ChangeResult, bool)         { return e.change, e.kind == resultChange }
func (e Event) Revision() (RevisionResult, bool)     { return e.revision, e.kind == resultRevision }
func (e Event) Projection() (ProjectionResult, bool) { return e.projection, e.kind == resultProjection }
func (e Event) MemoryEntry() (MemoryEntryResult, bool) {
	return e.memoryEntry, e.kind == resultMemoryEntry
}
func (e Event) MemorySync() (MemorySyncResult, bool) { return e.memorySync, e.kind == resultMemorySync }
func (e Event) Integration() (IntegrationResult, bool) {
	return e.integration, e.kind == resultIntegration
}

func NewChangeCreated(projectID, changeID, phase, status string, stateVersion int64) (Draft, error) {
	return newChangeDraft(NameChangeCreated, OperationCreateChange, projectID, changeID, phase, status, stateVersion)
}
func NewChangeTransitioned(projectID, changeID, phase, status string, stateVersion int64) (Draft, error) {
	return newChangeDraft(NameChangeTransitioned, OperationTransitionChange, projectID, changeID, phase, status, stateVersion)
}
func newChangeDraft(name Name, operation Operation, projectID, changeID, phase, status string, stateVersion int64) (Draft, error) {
	if !validProjectID(projectID) || !validID(changeID) || !validLabel(phase) || !validLabel(status) || stateVersion <= 0 {
		return Draft{}, errInvalidEvent
	}
	return Draft{name: name, operation: operation, projectID: projectID, subject: Subject{"change", changeID}, kind: resultChange, change: ChangeResult{phase, status, stateVersion}}, nil
}
func NewRevisionAccepted(projectID, changeID, artifactID, revisionID, artifact, status, digest, inputDigest string, stateVersion int64) (Draft, error) {
	if !validProjectID(projectID) || !validID(changeID) || !validID(artifactID) || !validID(revisionID) || !validLabel(artifact) || !validLabel(status) || !validHash(digest) || !validHash(inputDigest) || stateVersion <= 0 {
		return Draft{}, errInvalidEvent
	}
	return Draft{name: NameRevisionAccepted, operation: OperationAcceptRevision, projectID: projectID, subject: Subject{"artifactRevision", revisionID}, kind: resultRevision, revision: RevisionResult{changeID, artifactID, artifact, status, digest, inputDigest, stateVersion}}, nil
}
func NewProjectionRecorded(projectID, changeID, artifactID, revisionID, status, digest string, stateVersion int64) (Draft, error) {
	if !validProjectID(projectID) || !validID(changeID) || !validID(artifactID) || !validID(revisionID) || !validLabel(status) || !validHash(digest) || stateVersion <= 0 {
		return Draft{}, errInvalidEvent
	}
	return Draft{name: NameProjectionRecorded, operation: OperationRecordProjection, projectID: projectID, subject: Subject{"projection", artifactID}, kind: resultProjection, projection: ProjectionResult{changeID, artifactID, revisionID, status, digest, stateVersion}}, nil
}
func NewMemorySaved(projectID, entryID, scope, typ, state string, createdAt, updatedAt time.Time) (Draft, error) {
	return newMemoryDraft(NameMemorySaved, OperationRemember, projectID, entryID, scope, typ, state, createdAt, updatedAt)
}
func NewMemoryForgotten(projectID, entryID, scope, typ, state string, createdAt, updatedAt time.Time) (Draft, error) {
	if state != "archived" {
		return Draft{}, errInvalidEvent
	}
	return newMemoryDraft(NameMemoryForgotten, OperationForget, projectID, entryID, scope, typ, state, createdAt, updatedAt)
}
func newMemoryDraft(name Name, operation Operation, projectID, entryID, scope, typ, state string, createdAt, updatedAt time.Time) (Draft, error) {
	if !validProjectID(projectID) || !validID(entryID) || !validLabel(scope) || !validLabel(typ) || !validLabel(state) || !validTimestamp(createdAt) || !validTimestamp(updatedAt) {
		return Draft{}, errInvalidEvent
	}
	return Draft{name: name, operation: operation, projectID: projectID, subject: Subject{"memoryEntry", entryID}, kind: resultMemoryEntry, memoryEntry: MemoryEntryResult{scope, typ, state, createdAt.UTC(), updatedAt.UTC()}}, nil
}
func NewMemorySyncCompleted(projectID, status string, pushed, previouslyAccepted, rejected, retried, conflicts, batches int64) (Draft, error) {
	if !validProjectID(projectID) || !validLabel(status) || pushed < 0 || previouslyAccepted < 0 || rejected < 0 || retried < 0 || conflicts < 0 || batches < 0 {
		return Draft{}, errInvalidEvent
	}
	return Draft{name: NameMemorySyncCompleted, operation: OperationSync, projectID: projectID, subject: Subject{"memorySync", projectID}, kind: resultMemorySync, memorySync: MemorySyncResult{status, pushed, previouslyAccepted, rejected, retried, conflicts, batches}}, nil
}
func NewIntegrationPreviewCompleted(integrationID, state string, changed bool, artifactSHA256 string, artifactCount int64, restartRequired bool) (Draft, error) {
	return newIntegrationDraft(NameIntegrationPreviewCompleted, OperationPreview, integrationID, state, changed, artifactSHA256, artifactCount, restartRequired)
}
func NewIntegrationInstallCompleted(integrationID, state string, changed bool, artifactSHA256 string, artifactCount int64, restartRequired bool) (Draft, error) {
	return newIntegrationDraft(NameIntegrationInstallCompleted, OperationInstall, integrationID, state, changed, artifactSHA256, artifactCount, restartRequired)
}
func NewIntegrationStatusCompleted(integrationID, state string, changed bool, artifactSHA256 string, artifactCount int64, restartRequired bool) (Draft, error) {
	return newIntegrationDraft(NameIntegrationStatusCompleted, OperationStatus, integrationID, state, changed, artifactSHA256, artifactCount, restartRequired)
}
func NewIntegrationUninstallCompleted(integrationID, state string, changed bool, artifactSHA256 string, artifactCount int64, restartRequired bool) (Draft, error) {
	return newIntegrationDraft(NameIntegrationUninstallCompleted, OperationUninstall, integrationID, state, changed, artifactSHA256, artifactCount, restartRequired)
}
func newIntegrationDraft(name Name, operation Operation, integrationID, state string, changed bool, artifactSHA256 string, artifactCount int64, restartRequired bool) (Draft, error) {
	if !validID(integrationID) || !validLabel(state) || (artifactSHA256 != "" && !validHash(artifactSHA256)) || artifactCount < 0 {
		return Draft{}, errInvalidEvent
	}
	return Draft{name: name, operation: operation, subject: Subject{"integrationProvider", integrationID}, kind: resultIntegration, integration: IntegrationResult{state, artifactSHA256, changed, restartRequired, artifactCount}}, nil
}

func newEvent(d Draft, eventID string, occurredAt time.Time, sequence uint64, correlationID string) (Event, error) {
	if !validID(eventID) || !validTimestamp(occurredAt) || sequence == 0 || (correlationID != "" && !validID(correlationID)) || !validDraft(d) {
		return Event{}, errInvalidEvent
	}
	return Event{name: d.name, eventID: eventID, occurredAt: occurredAt.UTC(), sequence: sequence, correlationID: correlationID, operation: d.operation, projectID: d.projectID, subject: d.subject, kind: d.kind, change: d.change, revision: d.revision, projection: d.projection, memoryEntry: d.memoryEntry, memorySync: d.memorySync, integration: d.integration}, nil
}

func validDraft(d Draft) bool {
	if !validClosedTuple(d.name, d.operation, d.projectID, d.subject, d.kind) {
		return false
	}
	switch d.kind {
	case resultChange:
		return d.subject.id != "" && validChange(d.change) && d.revision == (RevisionResult{}) && d.projection == (ProjectionResult{}) && d.memoryEntry == (MemoryEntryResult{}) && d.memorySync == (MemorySyncResult{}) && d.integration == (IntegrationResult{})
	case resultRevision:
		return validRevision(d.revision) && d.change == (ChangeResult{}) && d.projection == (ProjectionResult{}) && d.memoryEntry == (MemoryEntryResult{}) && d.memorySync == (MemorySyncResult{}) && d.integration == (IntegrationResult{})
	case resultProjection:
		return d.subject.id == d.projection.artifactID && validProjection(d.projection) && d.change == (ChangeResult{}) && d.revision == (RevisionResult{}) && d.memoryEntry == (MemoryEntryResult{}) && d.memorySync == (MemorySyncResult{}) && d.integration == (IntegrationResult{})
	case resultMemoryEntry:
		return validMemoryEntry(d.memoryEntry) && d.change == (ChangeResult{}) && d.revision == (RevisionResult{}) && d.projection == (ProjectionResult{}) && d.memorySync == (MemorySyncResult{}) && d.integration == (IntegrationResult{})
	case resultMemorySync:
		return validMemorySync(d.memorySync) && d.change == (ChangeResult{}) && d.revision == (RevisionResult{}) && d.projection == (ProjectionResult{}) && d.memoryEntry == (MemoryEntryResult{}) && d.integration == (IntegrationResult{})
	case resultIntegration:
		return validIntegration(d.integration) && d.change == (ChangeResult{}) && d.revision == (RevisionResult{}) && d.projection == (ProjectionResult{}) && d.memoryEntry == (MemoryEntryResult{}) && d.memorySync == (MemorySyncResult{})
	default:
		return false
	}
}
func validClosedTuple(name Name, operation Operation, projectID string, subject Subject, kind resultKind) bool {
	if !validID(subject.kind) || !validID(subject.id) || (projectID != "" && !validProjectID(projectID)) {
		return false
	}
	switch name {
	case NameChangeCreated:
		return operation == OperationCreateChange && projectID != "" && subject.kind == "change" && kind == resultChange
	case NameRevisionAccepted:
		return operation == OperationAcceptRevision && projectID != "" && subject.kind == "artifactRevision" && kind == resultRevision
	case NameChangeTransitioned:
		return operation == OperationTransitionChange && projectID != "" && subject.kind == "change" && kind == resultChange
	case NameProjectionRecorded:
		return operation == OperationRecordProjection && projectID != "" && subject.kind == "projection" && kind == resultProjection
	case NameMemorySaved:
		return operation == OperationRemember && projectID != "" && subject.kind == "memoryEntry" && kind == resultMemoryEntry
	case NameMemoryForgotten:
		return operation == OperationForget && projectID != "" && subject.kind == "memoryEntry" && kind == resultMemoryEntry
	case NameMemorySyncCompleted:
		return operation == OperationSync && projectID != "" && subject.kind == "memorySync" && subject.id == projectID && kind == resultMemorySync
	case NameIntegrationPreviewCompleted:
		return operation == OperationPreview && projectID == "" && subject.kind == "integrationProvider" && kind == resultIntegration
	case NameIntegrationInstallCompleted:
		return operation == OperationInstall && projectID == "" && subject.kind == "integrationProvider" && kind == resultIntegration
	case NameIntegrationStatusCompleted:
		return operation == OperationStatus && projectID == "" && subject.kind == "integrationProvider" && kind == resultIntegration
	case NameIntegrationUninstallCompleted:
		return operation == OperationUninstall && projectID == "" && subject.kind == "integrationProvider" && kind == resultIntegration
	default:
		return false
	}
}
func validID(value string) bool        { return validText(value, maxIDBytes, false) }
func validProjectID(value string) bool { return validText(value, maxProjectBytes, false) }
func validLabel(value string) bool     { return validText(value, maxLabelBytes, true) }
func validText(value string, maxBytes int, asciiOnly bool) bool {
	if value == "" || len(value) > maxBytes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 0x21 || r == 0x7f || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) || (asciiOnly && r > 0x7e) {
			return false
		}
	}
	return true
}
func validHash(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
func validTimestamp(value time.Time) bool {
	return !value.IsZero() && value.Year() >= 1 && value.Year() <= 9999
}
func validChange(value ChangeResult) bool {
	return validLabel(value.phase) && validLabel(value.status) && value.stateVersion > 0
}
func validRevision(value RevisionResult) bool {
	return validID(value.changeID) && validID(value.artifactID) && validLabel(value.artifact) && validLabel(value.status) && validHash(value.digest) && validHash(value.inputDigest) && value.stateVersion > 0
}
func validProjection(value ProjectionResult) bool {
	return validID(value.changeID) && validID(value.artifactID) && validID(value.revisionID) && validLabel(value.status) && validHash(value.digest) && value.stateVersion > 0
}
func validMemoryEntry(value MemoryEntryResult) bool {
	return validLabel(value.scope) && validLabel(value.typ) && validLabel(value.state) && validTimestamp(value.createdAt) && validTimestamp(value.updatedAt)
}
func validMemorySync(value MemorySyncResult) bool {
	return validLabel(value.status) && value.pushed >= 0 && value.previouslyAccepted >= 0 && value.rejected >= 0 && value.retried >= 0 && value.conflicts >= 0 && value.batches >= 0
}
func validIntegration(value IntegrationResult) bool {
	return validLabel(value.state) && (value.artifactSHA256 == "" || validHash(value.artifactSHA256)) && value.artifactCount >= 0
}

func (e Event) MarshalJSON() ([]byte, error) {
	if !validID(e.eventID) || !validTimestamp(e.occurredAt) || e.sequence == 0 || (e.correlationID != "" && !validID(e.correlationID)) || !validDraft(Draft{name: e.name, operation: e.operation, projectID: e.projectID, subject: e.subject, kind: e.kind, change: e.change, revision: e.revision, projection: e.projection, memoryEntry: e.memoryEntry, memorySync: e.memorySync, integration: e.integration}) {
		return nil, errInvalidEvent
	}
	result, err := e.marshalResult()
	if err != nil {
		return nil, err
	}
	type subjectJSON struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	}
	type eventJSON struct {
		SchemaVersion json.RawMessage `json:"schemaVersion"`
		Name          Name            `json:"name"`
		EventID       string          `json:"eventId"`
		OccurredAt    string          `json:"occurredAt"`
		Sequence      uint64          `json:"sequence"`
		CorrelationID string          `json:"correlationId"`
		Operation     Operation       `json:"operation"`
		Outcome       string          `json:"outcome"`
		ProjectID     string          `json:"projectId,omitempty"`
		Subject       subjectJSON     `json:"subject"`
		Result        json.RawMessage `json:"result"`
	}
	// The API getter is a string for Go callers while the closed JSON contract requires a numeric 1.0.
	encoded, err := json.Marshal(eventJSON{json.RawMessage(schemaVersionText), e.name, e.eventID, e.occurredAt.UTC().Format(time.RFC3339Nano), e.sequence, e.correlationID, e.operation, "completed", e.projectID, subjectJSON{e.subject.kind, e.subject.id}, result})
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxEventEnvelopeBytes {
		return nil, errInvalidEvent
	}
	return encoded, nil
}

func (e Event) marshalResult() (json.RawMessage, error) {
	var value []byte
	var err error
	switch e.kind {
	case resultChange:
		value, err = json.Marshal(struct {
			Phase        string `json:"phase"`
			Status       string `json:"status"`
			StateVersion int64  `json:"stateVersion"`
		}{e.change.phase, e.change.status, e.change.stateVersion})
	case resultRevision:
		value, err = json.Marshal(struct {
			ChangeID     string `json:"changeId"`
			ArtifactID   string `json:"artifactId"`
			Artifact     string `json:"artifact"`
			Status       string `json:"status"`
			Digest       string `json:"digest"`
			InputDigest  string `json:"inputDigest"`
			StateVersion int64  `json:"stateVersion"`
		}{e.revision.changeID, e.revision.artifactID, e.revision.artifact, e.revision.status, e.revision.digest, e.revision.inputDigest, e.revision.stateVersion})
	case resultProjection:
		value, err = json.Marshal(struct {
			ChangeID     string `json:"changeId"`
			ArtifactID   string `json:"artifactId"`
			RevisionID   string `json:"revisionId"`
			Status       string `json:"status"`
			Digest       string `json:"digest"`
			StateVersion int64  `json:"stateVersion"`
		}{e.projection.changeID, e.projection.artifactID, e.projection.revisionID, e.projection.status, e.projection.digest, e.projection.stateVersion})
	case resultMemoryEntry:
		value, err = json.Marshal(struct {
			Scope     string `json:"scope"`
			Type      string `json:"type"`
			State     string `json:"state"`
			CreatedAt string `json:"createdAt"`
			UpdatedAt string `json:"updatedAt"`
		}{e.memoryEntry.scope, e.memoryEntry.typ, e.memoryEntry.state, e.memoryEntry.createdAt.UTC().Format(time.RFC3339Nano), e.memoryEntry.updatedAt.UTC().Format(time.RFC3339Nano)})
	case resultMemorySync:
		value, err = json.Marshal(struct {
			Status             string `json:"status"`
			Pushed             int64  `json:"pushed"`
			PreviouslyAccepted int64  `json:"previouslyAccepted"`
			Rejected           int64  `json:"rejected"`
			Retried            int64  `json:"retried"`
			Conflicts          int64  `json:"conflicts"`
			Batches            int64  `json:"batches"`
		}{e.memorySync.status, e.memorySync.pushed, e.memorySync.previouslyAccepted, e.memorySync.rejected, e.memorySync.retried, e.memorySync.conflicts, e.memorySync.batches})
	case resultIntegration:
		value, err = json.Marshal(struct {
			State           string `json:"state"`
			Changed         bool   `json:"changed"`
			ArtifactSHA256  string `json:"artifactSHA256"`
			ArtifactCount   int64  `json:"artifactCount"`
			RestartRequired bool   `json:"restartRequired"`
		}{e.integration.state, e.integration.changed, e.integration.artifactSHA256, e.integration.artifactCount, e.integration.restartRequired})
	default:
		return nil, errInvalidEvent
	}
	return json.RawMessage(value), err
}
