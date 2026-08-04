package syncservice

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	MaxRecordIDBytes = 1024 // Preserve legacy IDs without remapping.
	MaxFieldBytes    = 512
	MaxContentBytes  = 64 << 10
	MaxReferences    = 128
)

var (
	ErrInvalidMutation     = errors.New("invalid sync mutation")
	ErrInvalidChangeHash   = errors.New("invalid sync change hash")
	ErrInvalidCursor       = errors.New("invalid sync cursor")
	ErrLimitExceeded       = errors.New("sync limit exceeded")
	ErrUnsupportedSemantic = errors.New("unsupported sync semantic")
	uuidPattern            = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
)

type canonicalChange struct {
	HashVersion      int      `json:"hash_version"`
	Sequence         int64    `json:"sequence"`
	CanonicalVersion int64    `json:"canonical_version"`
	Mutation         Mutation `json:"mutation"`
}

type canonicalChangeV2 struct {
	HashVersion       int               `json:"hash_version"`
	Sequence          int64             `json:"sequence"`
	CanonicalVersion  int64             `json:"canonical_version"`
	ChangeDisposition ChangeDisposition `json:"change_disposition"`
	ConflictID        string            `json:"conflict_id"`
	Mutation          Mutation          `json:"mutation"`
}

// CanonicalChangeHash returns the replay-consistency hash for a pulled change.
func CanonicalChangeHash(change Change) (string, error) {
	if err := ValidateChangeEnvelope(change); err != nil {
		return "", err
	}
	var (
		encoded []byte
		err     error
	)
	if change.HashVersion != nil && *change.HashVersion == 2 {
		encoded, err = json.Marshal(canonicalChangeV2{HashVersion: *change.HashVersion, Sequence: change.Sequence, CanonicalVersion: change.CanonicalVersion, ChangeDisposition: change.ChangeDisposition, ConflictID: change.ConflictID, Mutation: change.Mutation})
	} else {
		encoded, err = json.Marshal(canonicalChange{HashVersion: 1, Sequence: change.Sequence, CanonicalVersion: change.CanonicalVersion, Mutation: change.Mutation})
	}
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// ValidateChangeEnvelope verifies the versioned pull envelope independently of
// the mutation payload validation performed at the protocol boundary.
func ValidateChangeEnvelope(change Change) error {
	if change.Sequence < 1 || change.CanonicalVersion < 1 {
		return ErrInvalidChangeHash
	}
	if change.HashVersion == nil || *change.HashVersion == 1 {
		if change.ChangeDisposition != "" || change.ConflictID != "" || change.Mutation.Kind == MutationTombstone || change.Mutation.Kind == MutationResolve {
			return ErrInvalidChangeHash
		}
		return nil
	}
	if *change.HashVersion != 2 {
		return ErrInvalidChangeHash
	}
	switch change.Mutation.Kind {
	case MutationCreate, MutationUpdate:
		if change.Mutation.RecordKind != RecordKindObservation || change.ChangeDisposition != ChangeDispositionConflict || !isUUID(change.ConflictID) {
			return ErrInvalidChangeHash
		}
	case MutationTombstone, MutationResolve:
		if change.ChangeDisposition != ChangeDispositionAccepted || change.ConflictID != "" {
			return ErrInvalidChangeHash
		}
	default:
		return ErrInvalidChangeHash
	}
	return nil
}

// VerifyChangeHash verifies a pulled change's replay-consistency hash.
func VerifyChangeHash(change Change) error {
	if len(change.ChangeHash) != sha256.Size*2 {
		return ErrInvalidChangeHash
	}
	for _, character := range change.ChangeHash {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return ErrInvalidChangeHash
		}
	}
	expected, err := CanonicalChangeHash(change)
	if err != nil || change.ChangeHash != expected {
		return ErrInvalidChangeHash
	}
	return nil
}

func ValidateMutation(m Mutation) error {
	if !validRecordKind(m.RecordKind) || !validMutationKind(m.RecordKind, m.Kind) {
		return ErrUnsupportedSemantic
	}
	if mutationExceedsLimits(m) {
		return ErrLimitExceeded
	}
	if !isUUID(m.MutationID) || !validID(m.RecordID) || !validBaseVersion(m.Kind, m.BaseVersion) || payloadCount(m) != 1 {
		return ErrInvalidMutation
	}
	switch m.Kind {
	case MutationCreate, MutationUpdate:
		switch m.RecordKind {
		case RecordKindProject:
			if m.Project == nil || m.Project.ID != m.RecordID || !validProject(*m.Project) {
				return ErrInvalidMutation
			}
		case RecordKindSession:
			if m.Session == nil || m.Session.ID != m.RecordID || !validSession(*m.Session) {
				return ErrInvalidMutation
			}
		case RecordKindObservation:
			valid := validActiveObservation
			if m.Kind == MutationCreate {
				valid = validCreateObservation
			}
			if m.Observation == nil || m.Observation.ID != m.RecordID || !valid(*m.Observation) {
				return ErrInvalidMutation
			}
		}
	case MutationArchive:
		if m.Observation == nil || m.Observation.ID != m.RecordID || !validObservation(*m.Observation) || m.Observation.Lifecycle != LifecycleArchived || m.Observation.Review != ReviewClear {
			return ErrInvalidMutation
		}
	case MutationTombstone:
		if m.Tombstone == nil || m.Tombstone.DeletedAt.IsZero() {
			return ErrInvalidMutation
		}
	case MutationResolve:
		if m.Resolution == nil || !validConflictIDs(m.Resolution.ConflictIDs) || m.Resolution.Observation == nil || m.Resolution.Observation.ID != m.RecordID || !validActiveClearObservation(*m.Resolution.Observation) {
			return ErrInvalidMutation
		}
	}
	return nil
}

func ValidateCursor(cursor Cursor) error {
	if !isUUID(cursor.HistoryID) || cursor.Position < 0 {
		return ErrInvalidCursor
	}
	return nil
}

func validRecordKind(kind RecordKind) bool {
	return kind == RecordKindProject || kind == RecordKindSession || kind == RecordKindObservation
}
func validMutationKind(record RecordKind, kind MutationKind) bool {
	return kind == MutationCreate || kind == MutationUpdate || record == RecordKindObservation && (kind == MutationArchive || kind == MutationTombstone || kind == MutationResolve)
}
func validBaseVersion(kind MutationKind, version int64) bool {
	return (kind == MutationCreate && version == 0) || (kind != MutationCreate && version > 0)
}
func payloadCount(m Mutation) int {
	n := 0
	for _, present := range []bool{m.Project != nil, m.Session != nil, m.Observation != nil, m.Tombstone != nil, m.Resolution != nil} {
		if present {
			n++
		}
	}
	return n
}
func validProject(p Project) bool { return validID(p.ID) }
func validSession(s Session) bool {
	return validID(s.ID) && validID(s.ProjectID)
}

func validObservation(o Observation) bool {
	if !validID(o.ID) || !validID(o.ProjectID) || (o.SessionID != "" && !validID(o.SessionID)) || !validOptionalText(o.Title, MaxFieldBytes) || (o.Scope != "project" && o.Scope != "personal") || !validRequiredText(o.Type, MaxFieldBytes) || !validRequiredContent(o.Content, MaxContentBytes) || !validOptionalText(o.TopicKey, MaxFieldBytes) || !validProvenance(o.Provenance) || !validSnapshotLifecycle(o.Lifecycle) || !validReview(o.Review) || o.CreatedAt.IsZero() || o.UpdatedAt.Before(o.CreatedAt) || (o.ReviewAfter != nil && o.ReviewAfter.IsZero()) || len(o.References) > MaxReferences {
		return false
	}
	seen := make(map[string]struct{}, len(o.References))
	for _, reference := range o.References {
		if !validID(reference) {
			return false
		}
		if _, ok := seen[reference]; ok {
			return false
		}
		seen[reference] = struct{}{}
	}
	return true
}
func validActiveClearObservation(o Observation) bool {
	return validObservation(o) && o.Lifecycle == LifecycleActive && o.Review == ReviewClear
}
func validActiveObservation(o Observation) bool {
	return validObservation(o) && o.Lifecycle == LifecycleActive
}
func validCreateObservation(o Observation) bool {
	return validActiveObservation(o) || validObservation(o) && o.Lifecycle == LifecycleArchived && o.Review == ReviewClear
}
func validProvenance(p Provenance) bool {
	return validRequiredText(p.Producer, MaxFieldBytes) && (p.SourceProvider == "" && p.SourceID == "" || validRequiredText(p.SourceProvider, MaxFieldBytes) && validRequiredText(p.SourceID, MaxFieldBytes))
}
func validConflictIDs(ids []string) bool {
	if len(ids) == 0 || len(ids) > MaxReferences {
		return false
	}
	seen := map[string]struct{}{}
	for _, id := range ids {
		if !isUUID(id) {
			return false
		}
		if _, ok := seen[id]; ok {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}
func validSnapshotLifecycle(value Lifecycle) bool {
	return value == LifecycleActive || value == LifecycleArchived
}
func validReview(value Review) bool { return value == ReviewClear || value == ReviewNeedsReview }
func validID(value string) bool {
	return validRequiredText(value, MaxRecordIDBytes)
}
func validRequiredText(value string, max int) bool {
	return strings.TrimSpace(value) != "" && validOptionalText(value, max)
}
func validOptionalText(value string, max int) bool {
	if len(value) > max || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
func validRequiredContent(value string, max int) bool {
	return strings.TrimSpace(value) != "" && validContent(value, max)
}
func validContent(value string, max int) bool {
	if len(value) > max || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r == 0 || (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f {
			return false
		}
	}
	return true
}
func isUUID(value string) bool { return uuidPattern.MatchString(value) }
func observationExceedsLimits(o *Observation) bool {
	if o == nil {
		return false
	}
	if len(o.Title) > MaxFieldBytes || len(o.Scope) > MaxFieldBytes || len(o.Type) > MaxFieldBytes || len(o.TopicKey) > MaxFieldBytes || len(o.Content) > MaxContentBytes || len(o.Provenance.Producer) > MaxFieldBytes || len(o.Provenance.SourceProvider) > MaxFieldBytes || len(o.Provenance.SourceID) > MaxFieldBytes || len(o.References) > MaxReferences {
		return true
	}
	for _, reference := range o.References {
		if len(reference) > MaxRecordIDBytes {
			return true
		}
	}
	return false
}
func mutationExceedsLimits(m Mutation) bool {
	if len(m.RecordID) > MaxRecordIDBytes || observationExceedsLimits(m.Observation) {
		return true
	}
	if m.Project != nil && len(m.Project.ID) > MaxRecordIDBytes {
		return true
	}
	if m.Session != nil && (len(m.Session.ID) > MaxRecordIDBytes || len(m.Session.ProjectID) > MaxRecordIDBytes) {
		return true
	}
	if m.Resolution != nil && (len(m.Resolution.ConflictIDs) > MaxReferences || observationExceedsLimits(m.Resolution.Observation)) {
		return true
	}
	return false
}
