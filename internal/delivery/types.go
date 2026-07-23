package delivery

import (
	"context"
	"errors"

	"github.com/vgxness/vgxness/internal/config"
)

const SchemaVersion = "1"

var (
	ErrInvalid     = errors.New("invalid delivery request")
	ErrNotFound    = errors.New("delivery receipt not found")
	ErrConflict    = errors.New("delivery receipt conflict")
	ErrInvalidated = errors.New("delivery receipt invalidated")
	ErrCorrupt     = errors.New("delivery state corrupt")
	ErrSensitive   = errors.New("delivery target contains a sensitive path")
	ErrUnbound     = errors.New("delivery target contains unbound changes")
)

type Gate string

const (
	GatePostApply Gate = "post-apply"
	GatePreCommit Gate = "pre-commit"
	GatePrePush   Gate = "pre-push"
	GatePrePR     Gate = "pre-pr"
)

type Identity struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type ContextManifest struct {
	Policy   Identity `json:"policy"`
	Prompt   Identity `json:"prompt"`
	Registry Identity `json:"registry"`
	Provider Identity `json:"provider"`
	Model    Identity `json:"model"`
}

type EvidenceCheck struct {
	ID           string     `json:"id"`
	Command      string     `json:"command"`
	ExitCode     int        `json:"exitCode"`
	OutputSHA256 string     `json:"outputSha256"`
	StartedAt    string     `json:"startedAt"`
	FinishedAt   string     `json:"finishedAt"`
	Toolchain    []Identity `json:"toolchain"`
}

type EvidenceManifest struct {
	Checks []EvidenceCheck `json:"checks"`
}

type ReviewFinding struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Summary  string `json:"summary"`
}

type ReviewManifest struct {
	Risk             string          `json:"risk"`
	Lenses           []string        `json:"lenses"`
	Verdict          string          `json:"verdict"`
	Findings         []ReviewFinding `json:"findings"`
	RollbackBoundary string          `json:"rollbackBoundary"`
}

type Manifest struct {
	SchemaVersion string           `json:"schemaVersion"`
	Context       ContextManifest  `json:"context"`
	Evidence      EvidenceManifest `json:"evidence"`
	Review        ReviewManifest   `json:"review"`
}

type TargetSnapshot struct {
	BaseRevision  string   `json:"baseRevision"`
	BaseTree      string   `json:"baseTree"`
	CandidateTree string   `json:"candidateTree"`
	Paths         []string `json:"paths"`
	PathsSHA256   string   `json:"pathsSha256"`
}

type Bindings struct {
	ContextSHA256  string `json:"contextSha256"`
	EvidenceSHA256 string `json:"evidenceSha256"`
	ReviewSHA256   string `json:"reviewSha256"`
}

type Receipt struct {
	Kind          string         `json:"kind"`
	SchemaVersion string         `json:"schemaVersion"`
	ReceiptID     string         `json:"receiptId"`
	IssuedAt      string         `json:"issuedAt"`
	Target        TargetSnapshot `json:"target"`
	Bindings      Bindings       `json:"bindings"`
	Manifest      Manifest       `json:"manifest"`
}

type Current struct {
	SchemaVersion string `json:"schemaVersion"`
	ReceiptID     string `json:"receiptId"`
	ReceiptSHA256 string `json:"receiptSha256"`
	State         string `json:"state"`
	UpdatedAt     string `json:"updatedAt"`
	Reason        string `json:"reason,omitempty"`
}

type IssueRequest struct {
	BaseRef  string
	Manifest Manifest
}

type ValidateRequest struct {
	Gate      Gate
	BaseRef   string
	ReceiptID string
	Manifest  Manifest
}

type Validation struct {
	Gate      Gate           `json:"gate"`
	ReceiptID string         `json:"receiptId"`
	State     string         `json:"state"`
	Target    TargetSnapshot `json:"target"`
}

type Status struct {
	Current Current `json:"current"`
	Receipt Receipt `json:"receipt"`
}

type Runtime interface {
	Issue(context.Context, config.Options, IssueRequest) (Receipt, error)
	Status(context.Context, config.Options) (Status, error)
	Validate(context.Context, config.Options, ValidateRequest) (Validation, error)
	Invalidate(context.Context, config.Options, string) (Current, error)
}
