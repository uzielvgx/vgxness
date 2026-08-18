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
	Projects      int                       `json:"projects"`
	Sessions      int                       `json:"sessions"`
	Observations  int                       `json:"observations"`
	Queued        int                       `json:"queued"`
}

type Search struct {
	Query, Project string
	TopicKey       string
	Scope          Scope
	Types          []string
	States         []State
	Limit          int
}
