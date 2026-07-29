package sdd

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalid           = errors.New("invalid SDD request")
	ErrConflict          = errors.New("SDD conflict")
	ErrNotFound          = errors.New("SDD record not found")
	ErrStaleState        = errors.New("stale SDD state version")
	ErrDigestMismatch    = errors.New("SDD digest mismatch")
	ErrInputsChanged     = errors.New("SDD input revisions changed")
	ErrImmutable         = errors.New("accepted SDD revision is immutable")
	ErrIllegalTransition = errors.New("illegal SDD lifecycle transition")
	ErrChangeCancelled   = errors.New("SDD change is cancelled")
	ErrProviderMismatch  = errors.New("SDD model provider mismatch")
)

type Backend string

const (
	BackendOpenSpec Backend = "openspec"
	BackendMemory   Backend = "memory"
	BackendHybrid   Backend = "hybrid"
)

func (value Backend) Valid() bool {
	return value == BackendOpenSpec || value == BackendMemory || value == BackendHybrid
}

type InteractionMode string

const (
	InteractionAutomatic   InteractionMode = "automatic"
	InteractionInteractive InteractionMode = "interactive"
)

func (value InteractionMode) Valid() bool {
	return value == InteractionAutomatic || value == InteractionInteractive
}

type Phase string

const (
	PhaseExplore  Phase = "explore"
	PhaseProposal Phase = "proposal"
	PhaseSpec     Phase = "spec"
	PhaseDesign   Phase = "design"
	PhaseTasks    Phase = "tasks"
	PhaseApply    Phase = "apply"
	PhaseVerify   Phase = "verify"
	PhaseComplete Phase = "complete"
)

var phases = []Phase{PhaseExplore, PhaseProposal, PhaseSpec, PhaseDesign, PhaseTasks, PhaseApply, PhaseVerify, PhaseComplete}

func (value Phase) Valid() bool { return phaseIndex(value) >= 0 }

func phaseIndex(value Phase) int {
	for index, phase := range phases {
		if value == phase {
			return index
		}
	}
	return -1
}

func ValidatePhaseTransition(from, to Phase) error {
	fromIndex, toIndex := phaseIndex(from), phaseIndex(to)
	if fromIndex < 0 || toIndex < 0 || toIndex != fromIndex+1 {
		return fmt.Errorf("%w: %s to %s", ErrIllegalTransition, from, to)
	}
	return nil
}

func IsDownstream(upstream, candidate Phase) bool {
	upstreamIndex, candidateIndex := phaseIndex(upstream), phaseIndex(candidate)
	return upstreamIndex >= 0 && candidateIndex > upstreamIndex
}

type ChangeStatus string

const (
	ChangeActive    ChangeStatus = "active"
	ChangeCompleted ChangeStatus = "completed"
	ChangeCancelled ChangeStatus = "cancelled"
)

func (value ChangeStatus) Valid() bool {
	return value == ChangeActive || value == ChangeCompleted || value == ChangeCancelled
}

type ArtifactStatus string

const (
	ArtifactDraft    ArtifactStatus = "draft"
	ArtifactAccepted ArtifactStatus = "accepted"
	ArtifactStale    ArtifactStatus = "stale"
)

func (value ArtifactStatus) Valid() bool {
	return value == ArtifactDraft || value == ArtifactAccepted || value == ArtifactStale
}

type RevisionStatus string

const (
	RevisionCandidate RevisionStatus = "candidate"
	RevisionAccepted  RevisionStatus = "accepted"
)

func (value RevisionStatus) Valid() bool {
	return value == RevisionCandidate || value == RevisionAccepted
}

type ProjectionStatus string

const (
	ProjectionAbsent  ProjectionStatus = "absent"
	ProjectionCurrent ProjectionStatus = "current"
	ProjectionStale   ProjectionStatus = "stale"
	ProjectionDrift   ProjectionStatus = "drift"
	ProjectionFailed  ProjectionStatus = "failed"
)

func (value ProjectionStatus) Valid() bool {
	return value == ProjectionAbsent || value == ProjectionCurrent || value == ProjectionStale || value == ProjectionDrift || value == ProjectionFailed
}

type Digest string

func ContentDigest(content []byte) Digest {
	digest := sha256.Sum256(content)
	return Digest(hex.EncodeToString(digest[:]))
}

func (value Digest) Valid() bool {
	if len(value) != sha256.Size*2 || strings.ToLower(string(value)) != string(value) {
		return false
	}
	decoded, err := hex.DecodeString(string(value))
	return err == nil && len(decoded) == sha256.Size
}

type RevisionBinding struct {
	ArtifactID string `json:"artifactId"`
	RevisionID string `json:"revisionId"`
	Digest     Digest `json:"digest"`
}

func InputRevisionDigest(inputs []RevisionBinding) Digest {
	canonical := append([]RevisionBinding(nil), inputs...)
	sort.Slice(canonical, func(i, j int) bool {
		if canonical[i].ArtifactID == canonical[j].ArtifactID {
			return canonical[i].RevisionID < canonical[j].RevisionID
		}
		return canonical[i].ArtifactID < canonical[j].ArtifactID
	})
	hash := sha256.New()
	for _, input := range canonical {
		hash.Write([]byte(input.ArtifactID))
		hash.Write([]byte{0})
		hash.Write([]byte(input.RevisionID))
		hash.Write([]byte{0})
		hash.Write([]byte(input.Digest))
		hash.Write([]byte{0})
	}
	return Digest(hex.EncodeToString(hash.Sum(nil)))
}

type Change struct {
	ID              string          `json:"id"`
	Project         string          `json:"project"`
	Title           string          `json:"title"`
	Backend         Backend         `json:"backend"`
	InteractionMode InteractionMode `json:"interactionMode"`
	Plan            Plan            `json:"plan"`
	Phase           Phase           `json:"phase"`
	Status          ChangeStatus    `json:"status"`
	StateVersion    int64           `json:"stateVersion"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
}

type Artifact struct {
	ID                string         `json:"id"`
	Project           string         `json:"project"`
	ChangeID          string         `json:"changeId"`
	Phase             Phase          `json:"phase"`
	Status            ArtifactStatus `json:"status"`
	CurrentRevisionID string         `json:"currentRevisionId,omitempty"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
}

type Revision struct {
	ID               string            `json:"id"`
	Project          string            `json:"project"`
	ChangeID         string            `json:"changeId"`
	ArtifactID       string            `json:"artifactId"`
	Artifact         Phase             `json:"artifact"`
	ArtifactStatus   ArtifactStatus    `json:"artifactStatus"`
	Status           RevisionStatus    `json:"status"`
	Content          []byte            `json:"content,omitempty"`
	ExternalLocation string            `json:"externalLocation,omitempty"`
	Digest           Digest            `json:"digest"`
	InputDigest      Digest            `json:"inputDigest"`
	Inputs           []RevisionBinding `json:"inputs"`
	StateVersion     int64             `json:"stateVersion"`
	CreatedAt        time.Time         `json:"createdAt"`
	AcceptedAt       *time.Time        `json:"acceptedAt,omitempty"`
}

type Projection struct {
	Project      string           `json:"project"`
	ChangeID     string           `json:"changeId"`
	ArtifactID   string           `json:"artifactId"`
	RevisionID   string           `json:"revisionId,omitempty"`
	Status       ProjectionStatus `json:"status"`
	Digest       Digest           `json:"digest,omitempty"`
	Location     string           `json:"location,omitempty"`
	StateVersion int64            `json:"stateVersion"`
	RecordedAt   *time.Time       `json:"recordedAt,omitempty"`
}

type CreateChangeRequest struct {
	Project         string          `json:"project"`
	IdempotencyKey  string          `json:"idempotencyKey"`
	Title           string          `json:"title"`
	Backend         Backend         `json:"backend"`
	InteractionMode InteractionMode `json:"interactionMode"`
	Plan            Plan            `json:"plan"`
}

func (request CreateChangeRequest) Validate() error {
	if !validText(request.Project, 256) || !validText(request.IdempotencyKey, 256) || !validText(request.Title, 512) || !request.Backend.Valid() || !request.InteractionMode.Valid() || !request.Plan.Valid() {
		return ErrInvalid
	}
	return nil
}

type ListChangesRequest struct {
	Project string       `json:"project"`
	Status  ChangeStatus `json:"status,omitempty"`
	Limit   int          `json:"limit,omitempty"`
}

func (request ListChangesRequest) Validate() error {
	if !validText(request.Project, 256) || request.Status != "" && !request.Status.Valid() || request.Limit < 0 || request.Limit > 100 {
		return ErrInvalid
	}
	return nil
}

type GetChangeRequest struct {
	Project string `json:"project"`
	ID      string `json:"id"`
}

type UpdateInteractionModeRequest struct {
	Project              string          `json:"project"`
	ChangeID             string          `json:"changeId"`
	InteractionMode      InteractionMode `json:"interactionMode"`
	ExpectedStateVersion int64           `json:"expectedStateVersion"`
}

func (request UpdateInteractionModeRequest) Validate() error {
	if !validText(request.Project, 256) || !validText(request.ChangeID, 256) || !request.InteractionMode.Valid() || request.ExpectedStateVersion < 1 {
		return ErrInvalid
	}
	return nil
}

func (request GetChangeRequest) Validate() error {
	if !validText(request.Project, 256) || !validText(request.ID, 256) {
		return ErrInvalid
	}
	return nil
}

type SaveRevisionRequest struct {
	Project              string            `json:"project"`
	ChangeID             string            `json:"changeId"`
	Artifact             Phase             `json:"artifact"`
	Content              []byte            `json:"content"`
	ExternalLocation     string            `json:"externalLocation,omitempty"`
	Digest               Digest            `json:"digest,omitempty"`
	Inputs               []RevisionBinding `json:"inputs,omitempty"`
	InputDigest          Digest            `json:"inputDigest,omitempty"`
	ExpectedStateVersion int64             `json:"expectedStateVersion"`
}

func (request SaveRevisionRequest) Validate() error {
	if !validText(request.Project, 256) || !validText(request.ChangeID, 256) || !request.Artifact.Valid() || request.Artifact == PhaseComplete || len(request.Content) == 0 || len(request.Content) > 4<<20 || request.ExpectedStateVersion < 1 {
		return ErrInvalid
	}
	if request.Digest != "" && !request.Digest.Valid() || request.InputDigest != "" && !request.InputDigest.Valid() {
		return ErrInvalid
	}
	if request.ExternalLocation != "" && !validText(request.ExternalLocation, 1024) {
		return ErrInvalid
	}
	seen := make(map[string]bool, len(request.Inputs))
	for _, input := range request.Inputs {
		if !validText(input.ArtifactID, 256) || !validText(input.RevisionID, 256) || !input.Digest.Valid() || seen[input.ArtifactID] {
			return ErrInvalid
		}
		seen[input.ArtifactID] = true
	}
	return nil
}

type GetRevisionRequest struct {
	Project    string `json:"project"`
	ChangeID   string `json:"changeId"`
	RevisionID string `json:"revisionId"`
}

func (request GetRevisionRequest) Validate() error {
	if !validText(request.Project, 256) || !validText(request.ChangeID, 256) || !validText(request.RevisionID, 256) {
		return ErrInvalid
	}
	return nil
}

type ListRevisionsRequest struct {
	Project  string `json:"project"`
	ChangeID string `json:"changeId"`
	Artifact Phase  `json:"artifact,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

func (request ListRevisionsRequest) Validate() error {
	if !validText(request.Project, 256) || !validText(request.ChangeID, 256) || request.Artifact != "" && (!request.Artifact.Valid() || request.Artifact == PhaseComplete) || request.Limit < 0 || request.Limit > 100 {
		return ErrInvalid
	}
	return nil
}

type AcceptRevisionRequest struct {
	Project              string `json:"project"`
	ChangeID             string `json:"changeId"`
	RevisionID           string `json:"revisionId"`
	ExpectedStateVersion int64  `json:"expectedStateVersion"`
}

func (request AcceptRevisionRequest) Validate() error {
	if err := (GetRevisionRequest{Project: request.Project, ChangeID: request.ChangeID, RevisionID: request.RevisionID}).Validate(); err != nil || request.ExpectedStateVersion < 1 {
		return ErrInvalid
	}
	return nil
}

type TransitionChangeRequest struct {
	Project              string `json:"project"`
	ChangeID             string `json:"changeId"`
	TargetPhase          Phase  `json:"targetPhase,omitempty"`
	Cancel               bool   `json:"cancel,omitempty"`
	ExpectedStateVersion int64  `json:"expectedStateVersion"`
}

func (request TransitionChangeRequest) Validate() error {
	if !validText(request.Project, 256) || !validText(request.ChangeID, 256) || request.ExpectedStateVersion < 1 || request.Cancel == (request.TargetPhase != "") || !request.Cancel && !request.TargetPhase.Valid() {
		return ErrInvalid
	}
	return nil
}

type ProjectionStatusRequest struct {
	Project    string `json:"project"`
	ChangeID   string `json:"changeId"`
	ArtifactID string `json:"artifactId"`
}

func (request ProjectionStatusRequest) Validate() error {
	if !validText(request.Project, 256) || !validText(request.ChangeID, 256) || !validText(request.ArtifactID, 256) {
		return ErrInvalid
	}
	return nil
}

type RecordProjectionRequest struct {
	Project              string           `json:"project"`
	ChangeID             string           `json:"changeId"`
	ArtifactID           string           `json:"artifactId"`
	RevisionID           string           `json:"revisionId"`
	Status               ProjectionStatus `json:"status"`
	Digest               Digest           `json:"digest"`
	Location             string           `json:"location"`
	ExpectedStateVersion int64            `json:"expectedStateVersion"`
}

func (request RecordProjectionRequest) Validate() error {
	if err := (ProjectionStatusRequest{Project: request.Project, ChangeID: request.ChangeID, ArtifactID: request.ArtifactID}).Validate(); err != nil || !validText(request.RevisionID, 256) || !request.Status.Valid() || request.Status == ProjectionAbsent || !request.Digest.Valid() || !validText(request.Location, 1024) || request.ExpectedStateVersion < 1 {
		return ErrInvalid
	}
	return nil
}

func validText(value string, limit int) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len([]rune(trimmed)) > limit || trimmed != value {
		return false
	}
	for _, character := range value {
		if character < ' ' || character == 0x7f {
			return false
		}
	}
	return true
}
