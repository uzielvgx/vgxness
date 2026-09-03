package syncservice

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func newValidObservation() *Observation {
	now := time.Now().UTC()
	return &Observation{ID: "observation-1", Title: "Title", ProjectID: "project-1", SessionID: "session-1", Scope: "project", Type: "note", Content: "normal\nmultiline content", TopicKey: "topic-1", Provenance: Provenance{Producer: "client", SourceProvider: "manual", SourceID: "source-1"}, Lifecycle: LifecycleActive, Review: ReviewClear, CreatedAt: now, UpdatedAt: now, References: []string{"reference-1"}}
}

func TestValidateDiscoveryRequiresCanonicalHistoryAndCapabilities(t *testing.T) {
	valid := Discovery{ProtocolVersion: 1, HistoryID: "123e4567-e89b-12d3-a456-426614174000", Capabilities: []Capability{CapabilityBootstrapDiscovery}}
	if err := ValidateDiscovery(valid); err != nil {
		t.Fatalf("valid discovery: %v", err)
	}
	for _, value := range []Discovery{
		{ProtocolVersion: 2, HistoryID: valid.HistoryID, Capabilities: valid.Capabilities},
		{ProtocolVersion: 1, HistoryID: "123E4567-E89B-12D3-A456-426614174000", Capabilities: valid.Capabilities},
		{ProtocolVersion: 1, HistoryID: valid.HistoryID, Capabilities: []Capability{"unknown"}},
		{ProtocolVersion: 1, HistoryID: valid.HistoryID, Capabilities: []Capability{CapabilityBootstrapDiscovery, CapabilityBootstrapDiscovery}},
		{ProtocolVersion: 1, HistoryID: valid.HistoryID},
		{ProtocolVersion: 1, HistoryID: valid.HistoryID, Capabilities: []Capability{}},
		{ProtocolVersion: 1, HistoryID: valid.HistoryID, Capabilities: []Capability{"", CapabilityBootstrapDiscovery}},
		{ProtocolVersion: 1, HistoryID: valid.HistoryID, Capabilities: []Capability{CapabilityBootstrapDiscovery, CapabilityBootstrapDiscovery, CapabilityBootstrapDiscovery, CapabilityBootstrapDiscovery, CapabilityBootstrapDiscovery, CapabilityBootstrapDiscovery, CapabilityBootstrapDiscovery, CapabilityBootstrapDiscovery, CapabilityBootstrapDiscovery}},
	} {
		if ValidateDiscovery(value) == nil {
			t.Fatalf("accepted invalid discovery: %#v", value)
		}
	}
}
func validMutation(kind MutationKind) Mutation {
	return Mutation{MutationID: "8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91", RecordID: "observation-1", RecordKind: RecordKindObservation, Kind: kind, BaseVersion: 1, Observation: newValidObservation()}
}

func TestTaggedPayloadLimitsUseLimitError(t *testing.T) {
	p := Mutation{MutationID: "8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91", RecordID: "project-1", RecordKind: RecordKindProject, Kind: MutationCreate, Project: &Project{ID: strings.Repeat("x", MaxRecordIDBytes+1)}}
	s := Mutation{MutationID: "8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91", RecordID: "session-1", RecordKind: RecordKindSession, Kind: MutationCreate, Session: &Session{ID: "session-1", ProjectID: strings.Repeat("x", MaxRecordIDBytes+1)}}
	o := validMutation(MutationCreate)
	o.BaseVersion, o.Observation.Title = 0, strings.Repeat("x", MaxFieldBytes+1)
	r := validMutation(MutationResolve)
	r.Observation = nil
	r.Resolution = &Resolution{ConflictIDs: make([]string, MaxReferences+1), Observation: newValidObservation()}
	r2 := validMutation(MutationResolve)
	r2.Observation = nil
	r2.Resolution = &Resolution{ConflictIDs: []string{"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91"}, Observation: newValidObservation()}
	r2.Resolution.Observation.Title = strings.Repeat("x", MaxFieldBytes+1)
	for _, m := range []Mutation{p, s, o, r, r2} {
		if !errors.Is(ValidateMutation(m), ErrLimitExceeded) {
			t.Fatal("tagged payload limit was not classified")
		}
	}
}

func TestValidateMutationObservationRules(t *testing.T) {
	c := validMutation(MutationCreate)
	c.BaseVersion = 0
	if err := ValidateMutation(c); err != nil {
		t.Fatalf("active clear create: %v", err)
	}
	n := c
	n.Observation = newValidObservation()
	n.Observation.Review = ReviewNeedsReview
	if err := ValidateMutation(n); err != nil {
		t.Fatalf("active needs-review create: %v", err)
	}
	for name, edit := range map[string]func(*Mutation){"tombstoned": func(m *Mutation) { m.Observation.Lifecycle = LifecycleTombstoned }, "content": func(m *Mutation) { m.Observation.Content = "bad\x00content" }, "id": func(m *Mutation) { m.RecordID = "bad id" }, "refs": func(m *Mutation) { m.Observation.References = make([]string, MaxReferences+1) }, "dupe": func(m *Mutation) { m.Observation.References = []string{"a", "a"} }, "title": func(m *Mutation) { m.Observation.Title = strings.Repeat("x", MaxFieldBytes+1) }, "long": func(m *Mutation) { m.Observation.Content = strings.Repeat("x", MaxContentBytes+1) }, "producer": func(m *Mutation) { m.Observation.Provenance.Producer = "" }, "title control": func(m *Mutation) { m.Observation.Title = "bad\ntitle" }, "provenance control": func(m *Mutation) { m.Observation.Provenance.Producer = "bad\nproducer" }} {
		t.Run(name, func(t *testing.T) {
			m := c
			o := *c.Observation
			m.Observation = &o
			edit(&m)
			if err := ValidateMutation(m); err == nil || strings.Contains(err.Error(), m.Observation.Content) {
				t.Fatalf("unsafe rejection: %v", err)
			}
		})
	}
}

func TestValidateMutationSpecialKinds(t *testing.T) {
	a := validMutation(MutationArchive)
	a.Observation.Lifecycle = LifecycleArchived
	if err := ValidateMutation(a); err != nil {
		t.Fatal(err)
	}
	tm := validMutation(MutationTombstone)
	tm.Observation = nil
	tm.Tombstone = &Tombstone{DeletedAt: time.Now().UTC()}
	if err := ValidateMutation(tm); err != nil {
		t.Fatal(err)
	}
	r := validMutation(MutationResolve)
	r.Observation = nil
	r.Resolution = &Resolution{ConflictIDs: []string{"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91", "d4a67751-a26c-4583-84a1-f2a1a3d92ebb"}, Observation: newValidObservation()}
	if err := ValidateMutation(r); err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(*Mutation){"project": func(m *Mutation) { m.RecordKind = RecordKindProject }, "tombstone": func(m *Mutation) { m.Observation = newValidObservation() }, "duplicate": func(m *Mutation) {
		m.Resolution.ConflictIDs = []string{"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91", "8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91"}
	}, "invalid": func(m *Mutation) { m.Resolution.ConflictIDs = []string{"bad"} }} {
		t.Run(name, func(t *testing.T) {
			m := a
			if name == "tombstone" {
				m = tm
			}
			if name == "duplicate" || name == "invalid" {
				m = r
				x := *r.Resolution
				x.ConflictIDs = append([]string(nil), x.ConflictIDs...)
				m.Resolution = &x
			}
			edit(&m)
			if ValidateMutation(m) == nil {
				t.Fatal("want rejection")
			}
		})
	}
}

func TestProjectAndSessionOnlyAllowCreateUpdate(t *testing.T) {
	for _, k := range []RecordKind{RecordKindProject, RecordKindSession} {
		m := validMutation(MutationArchive)
		m.RecordKind, m.Observation = k, newValidObservation()
		if ValidateMutation(m) == nil {
			t.Fatalf("%s archive accepted", k)
		}
	}
}
func TestValidateCursorAndResultFoundations(t *testing.T) {
	if ValidateCursor(Cursor{HistoryID: "8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91"}) != nil || ValidateCursor(Cursor{HistoryID: "bad", Position: -1}) == nil {
		t.Fatal("cursor validation")
	}
}
func TestResultTerminalClassification(t *testing.T) {
	for _, r := range []Result{{Disposition: DispositionAccepted}, {Disposition: DispositionPreviouslyAccepted}, {Disposition: DispositionConflict}, {Disposition: DispositionRejected}} {
		if !r.Terminal() {
			t.Fatal("terminal")
		}
	}
	if (Result{Disposition: DispositionRejected, Retryable: true}).Terminal() {
		t.Fatal("retryable")
	}
}
func TestObservationSemanticHardening(t *testing.T) {
	c := validMutation(MutationCreate)
	c.BaseVersion = 0
	for n, e := range map[string]func(*Observation){"pair": func(o *Observation) { o.Provenance.SourceProvider, o.Provenance.SourceID = "provider", "" }, "scope": func(o *Observation) { o.Scope = "workspace" }, "time": func(o *Observation) { o.UpdatedAt = o.CreatedAt.Add(-time.Second) }, "tombstone": func(o *Observation) { o.Lifecycle = LifecycleTombstoned }} {
		t.Run(n, func(t *testing.T) {
			m := c
			o := *c.Observation
			m.Observation = &o
			e(&o)
			if ValidateMutation(m) == nil {
				t.Fatal("want rejection")
			}
		})
	}
}
func TestLegacyIDsAndBootstrapCreates(t *testing.T) {
	m := validMutation(MutationCreate)
	m.BaseVersion = 0
	m.RecordID = strings.Repeat("x ", 400)
	m.Observation.ID = m.RecordID
	if ValidateMutation(m) != nil {
		t.Fatal("legacy ID")
	}
	m.RecordID = strings.Repeat("x", MaxRecordIDBytes+1)
	m.Observation.ID = m.RecordID
	if !errors.Is(ValidateMutation(m), ErrLimitExceeded) {
		t.Fatal("long ID")
	}
	m = validMutation(MutationCreate)
	m.BaseVersion = 0
	m.Observation.Title = ""
	m.Observation.Lifecycle = LifecycleArchived
	if ValidateMutation(m) != nil {
		t.Fatal("archived bootstrap")
	}
	m.Kind, m.BaseVersion = MutationUpdate, 1
	if ValidateMutation(m) == nil {
		t.Fatal("archived update")
	}
}

func TestProjectStateHistoryMatchesSupportedDurableStates(t *testing.T) {
	if err := ValidateProjectState(ProjectState{Status: ProjectStateAbsent}); err != nil {
		t.Fatalf("absent without history: %v", err)
	}
	if err := ValidateProjectState(ProjectState{Status: ProjectStateActive}); err == nil {
		t.Fatal("accepted active without history")
	}
}

func TestCanonicalChangeHashV1OrdinaryGoldenUnchanged(t *testing.T) {
	base := `{"sequence":1,"canonical_version":1,"mutation":{"mutation_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","record_id":"project","record_kind":"project","kind":"create","base_version":0,"project":{"id":"project"}}}`
	for _, wire := range []string{base, strings.TrimSuffix(base, "}") + `,"hash_version":1}`} {
		var decoded Change
		if err := json.Unmarshal([]byte(wire), &decoded); err != nil {
			t.Fatal(err)
		}
		hash, err := CanonicalChangeHash(decoded)
		if err != nil || hash != "11d715b99da25ca73ef871a74b0543901f57937632ca7ea47eee5ec4157bac08" {
			t.Fatalf("v1 hash = %q, %v", hash, err)
		}
	}
}

func TestCanonicalChangeHashV2SpecialEnvelope(t *testing.T) {
	ordinary := Change{Sequence: 1, CanonicalVersion: 1, Mutation: validMutation(MutationCreate)}
	ordinary.Mutation.BaseVersion = 0
	special := ordinary
	special.HashVersion, special.ChangeDisposition, special.ConflictID = hashVersion(2), ChangeDispositionConflict, "8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91"
	ordinaryHash, _ := CanonicalChangeHash(ordinary)
	specialHash, _ := CanonicalChangeHash(special)
	if specialHash == ordinaryHash {
		t.Fatal("v2 special envelope used the v1 hash")
	}
}

func TestCanonicalChangeHashBindsTombstoneProject(t *testing.T) {
	version := 2
	change := Change{Sequence: 1, CanonicalVersion: 1, HashVersion: &version, ChangeDisposition: ChangeDispositionAccepted, Mutation: validMutation(MutationTombstone)}
	change.Mutation.Observation = nil
	change.Mutation.Tombstone = &Tombstone{DeletedAt: time.Now().UTC(), ProjectID: "project-1"}
	change.ChangeHash, _ = CanonicalChangeHash(change)
	tampered := change
	tombstone := *change.Mutation.Tombstone
	tombstone.ProjectID = "project-2"
	tampered.Mutation.Tombstone = &tombstone
	if VerifyChangeHash(tampered) == nil {
		t.Fatal("accepted tombstone with a re-bound project")
	}
}

func TestVerifyChangeHashRejectsTamperedOrUnsupportedV2(t *testing.T) {
	change := Change{Sequence: 1, CanonicalVersion: 1, Mutation: Mutation{MutationID: "8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91", RecordID: "project", RecordKind: RecordKindProject, Kind: MutationCreate, Project: &Project{ID: "project"}}}
	special := change
	special.HashVersion, special.ChangeDisposition = hashVersion(2), ChangeDispositionAccepted
	special.ChangeHash, _ = CanonicalChangeHash(special)
	if VerifyChangeHash(special) == nil {
		t.Fatal("accepted unsupported v2 envelope")
	}
}

func hashVersion(value int) *int { return &value }
