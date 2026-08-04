package syncservice

import "time"

const MaxPullResponseBytes = 2 << 20

type RecordKind string

const (
	RecordKindProject     RecordKind = "project"
	RecordKindSession     RecordKind = "session"
	RecordKindObservation RecordKind = "observation"
)

type MutationKind string

const (
	MutationCreate    MutationKind = "create"
	MutationUpdate    MutationKind = "update"
	MutationArchive   MutationKind = "archive"
	MutationTombstone MutationKind = "tombstone"
	MutationResolve   MutationKind = "resolve"
)

type Lifecycle string

const (
	LifecycleActive     Lifecycle = "active"
	LifecycleArchived   Lifecycle = "archived"
	LifecycleTombstoned Lifecycle = "tombstoned"
)

type Review string

// Existing state: active→active/clear, needs_review→active/needs_review, archived→archived/clear; none→tombstone.
const (
	ReviewClear       Review = "clear"
	ReviewNeedsReview Review = "needs_review"
)

type Project struct {
	ID string `json:"id"`
}

type Session struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
}

type Observation struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	ProjectID   string     `json:"project_id"`
	SessionID   string     `json:"session_id,omitempty"`
	Scope       string     `json:"scope"`
	Type        string     `json:"type"`
	Content     string     `json:"content"`
	TopicKey    string     `json:"topic_key,omitempty"`
	References  []string   `json:"references,omitempty"`
	Provenance  Provenance `json:"provenance"`
	Lifecycle   Lifecycle  `json:"lifecycle"`
	Review      Review     `json:"review"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ReviewAfter *time.Time `json:"review_after,omitempty"`
}

type Provenance struct {
	Producer       string `json:"producer"`
	SourceProvider string `json:"source_provider,omitempty"`
	SourceID       string `json:"source_id,omitempty"`
}

type Resolution struct {
	ConflictIDs []string     `json:"conflict_ids"`
	Observation *Observation `json:"observation"`
}

type Tombstone struct {
	DeletedAt time.Time `json:"deleted_at"`
}

type Mutation struct {
	MutationID  string       `json:"mutation_id"`
	RecordID    string       `json:"record_id"`
	RecordKind  RecordKind   `json:"record_kind"`
	Kind        MutationKind `json:"kind"`
	BaseVersion int64        `json:"base_version"`
	Project     *Project     `json:"project,omitempty"`
	Session     *Session     `json:"session,omitempty"`
	Observation *Observation `json:"observation,omitempty"`
	Tombstone   *Tombstone   `json:"tombstone,omitempty"`
	Resolution  *Resolution  `json:"resolution,omitempty"`
}

type Change struct {
	Sequence          int64             `json:"sequence"`
	CanonicalVersion  int64             `json:"canonical_version"`
	HashVersion       *int              `json:"hash_version,omitempty"`
	ChangeDisposition ChangeDisposition `json:"change_disposition,omitempty"`
	ConflictID        string            `json:"conflict_id,omitempty"`
	Mutation          Mutation          `json:"mutation"`
	ChangeHash        string            `json:"change_hash"`
}

type ChangeDisposition string

const (
	ChangeDispositionAccepted ChangeDisposition = "accepted"
	ChangeDispositionConflict ChangeDisposition = "conflict"
)

type Cursor struct {
	HistoryID string `json:"history_id"`
	Position  int64  `json:"position"`
	Watermark int64  `json:"watermark,omitempty"`
}

// Capability identifies an optional sync protocol feature.
type Capability string

const CapabilityBootstrapDiscovery Capability = "bootstrap_discovery"

// Discovery is the authenticated owner-scoped bootstrap metadata response.
type Discovery struct {
	ProtocolVersion int          `json:"protocol_version"`
	HistoryID       string       `json:"history_id"`
	Capabilities    []Capability `json:"capabilities"`
}

type PullPage struct {
	Cursor  Cursor   `json:"cursor"`
	HasMore bool     `json:"has_more"`
	Changes []Change `json:"changes,omitempty"`
}

type Result struct {
	MutationID  string      `json:"mutation_id"`
	Disposition Disposition `json:"disposition"`
	Retryable   bool        `json:"retryable"`
	Code        string      `json:"code"`
	Sequence    *int64      `json:"sequence,omitempty"`
	Version     int64       `json:"version"`
}

type Disposition string

const (
	DispositionAccepted           Disposition = "accepted"
	DispositionPreviouslyAccepted Disposition = "previously_accepted"
	DispositionConflict           Disposition = "conflict"
	DispositionRejected           Disposition = "rejected"
)

func (r Result) Terminal() bool {
	return r.Disposition == DispositionAccepted || r.Disposition == DispositionPreviouslyAccepted || r.Disposition == DispositionConflict || r.Disposition == DispositionRejected && !r.Retryable
}
