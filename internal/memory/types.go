package memory

import (
	"errors"
	"time"
)

var (
	ErrInvalid   = errors.New("invalid")
	ErrConflict  = errors.New("conflict")
	ErrNotFound  = errors.New("not_found")
	ErrMigration = errors.New("migration")
	ErrCorrupt   = errors.New("corrupt")
	// ErrSyncProjectRepairPending prevents pull materialization until the
	// operator-confirmed project-create repair reaches a terminal state.
	ErrSyncProjectRepairPending = errors.New("sync_project_repair_pending")
)

type Scope string

const (
	ScopeProject  Scope = "project"
	ScopePersonal Scope = "personal"
)

type State string

const (
	StateActive      State = "active"
	StateNeedsReview State = "needs_review"
	StateArchived    State = "archived"
)

type Provenance struct{ Producer, SourceProvider, SourceID string }

type Observation struct {
	ID          string
	Title       string
	Project     string
	Session     string
	Scope       Scope
	Type        string
	Content     string
	TopicKey    string
	Provenance  Provenance
	State       State
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ReviewAfter *time.Time
	References  []string
}

type ProviderSessionState string

const (
	ProviderSessionActive      ProviderSessionState = "active"
	ProviderSessionCompleted   ProviderSessionState = "completed"
	ProviderSessionInterrupted ProviderSessionState = "interrupted"
	ProviderSessionCancelled   ProviderSessionState = "cancelled"
)

// Provider sessions are local-only. ExternalID is accepted only to bind a
// request to its local session; it is hashed immediately and never retained.
type ProviderSession struct {
	Handle, Project, Provider, FinalObservationID string
	State                                         ProviderSessionState
	Checkpointed                                  bool
	LeaseToken                                    string
	LeaseUntil                                    *time.Time
	DraftPresent                                  bool
	CreatedAt, UpdatedAt                          time.Time
	CompletedAt                                   *time.Time
}
type ProviderSessionStart struct{ Project, Provider, ExternalID string }
type ProviderSessionEnd struct {
	Project, Handle, ExternalID, Summary, LeaseToken string
	State                                            ProviderSessionState
}
type ProviderSessionContext struct {
	Session ProviderSession
	Handoff string
}

// ProviderSessionDraft is local host-supplied text pending final validation.
// Its summary is intentionally never returned or synchronized.
type ProviderSessionDraft struct {
	Handle, Project string
	UpdatedAt       time.Time
}
type ProviderSessionDraftSave struct {
	Project, Handle, Summary string
	ExpectedUpdatedAt        time.Time
}

// ObservationUpdate is an optimistic content update. ExpectedUpdatedAt must
// equal the persisted timestamp to prevent a stale writer from winning.
type ObservationUpdate struct {
	ID, Project, Content string
	ExpectedUpdatedAt    time.Time
}

// SyncBackfillResult describes a local-only, idempotent sync queue backfill.
type SyncBackfillResult struct {
	SchemaVersion int  `json:"schemaVersion"`
	Limit         int  `json:"limit"`
	Remaining     bool `json:"remaining"`
	Projects      int  `json:"projects"`
	Sessions      int  `json:"sessions"`
	Observations  int  `json:"observations"`
	Queued        int  `json:"queued"`
}

// SyncProjectRepairResult reports only local repair state and queue count.
type SyncProjectRepairResult struct {
	SchemaVersion int    `json:"schemaVersion"`
	Status        string `json:"status"`
	Queued        int    `json:"queued"`
}

type SyncProjectTransitionMode string

const (
	SyncProjectTransitionReseedSource SyncProjectTransitionMode = "reseed_source"
	SyncProjectTransitionRejoinMerge  SyncProjectTransitionMode = "rejoin_merge"
)

const (
	SyncProjectTransitionPulling    = "pulling"
	SyncProjectTransitionPublishing = "publishing"
	SyncProjectTransitionCompleted  = "completed"
)

// SyncProjectTransitionResult reports only local transition state and counts.
type SyncProjectTransitionResult struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Mode          SyncProjectTransitionMode `json:"mode"`
	Status        string                    `json:"status"`
	// TransitionIdentity is a durable, opaque generation used only to bind a
	// plan to the exact transition it selected.
	TransitionIdentity int64 `json:"-"`
	Projects           int   `json:"projects"`
	Sessions           int   `json:"sessions"`
	Observations       int   `json:"observations"`
	Queued             int   `json:"queued"`
}

type Search struct {
	Query, Project string
	TopicKey       string
	Scope          Scope
	Types          []string
	States         []State
	Limit          int
}
