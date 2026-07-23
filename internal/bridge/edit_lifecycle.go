package bridge

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

const (
	MaxNativeEditActorBytes          = 256
	MaxNativeEditReviewManifestBytes = 1 << 20
)

type NativeEditInspectRequest struct {
	TicketID string `json:"ticketId"`
}

type NativeEditActionRequest struct {
	TicketID    string `json:"ticketId"`
	ManifestSHA string `json:"manifestSha256"`
	Actor       string `json:"actor"`
}

type NativeEditReviewRequest struct {
	TicketID string          `json:"ticketId"`
	Manifest json.RawMessage `json:"manifest"`
}

type NativeEditApprovalRequest struct {
	TicketID        string `json:"ticketId"`
	ManifestSHA     string `json:"manifestSha256"`
	ReviewReceiptID string `json:"reviewReceiptId"`
	Actor           string `json:"actor"`
}

type NativeEditReviewResult struct {
	TicketID      string             `json:"ticketId"`
	Artifact      NativeEditArtifact `json:"artifact"`
	ReceiptID     string             `json:"receiptId"`
	State         string             `json:"state"`
	CandidateTree string             `json:"candidateTree"`
	ReviewSHA256  string             `json:"reviewSha256"`
}

type NativeEditApproval struct {
	ManifestSHA     string `json:"manifestSha256"`
	BaseRevision    string `json:"baseRevision"`
	ReviewReceiptID string `json:"reviewReceiptId"`
	CandidateTree   string `json:"candidateTree"`
	ReviewSHA256    string `json:"reviewSha256"`
	Actor           string `json:"actor"`
	ApprovedAt      string `json:"approvedAt"`
}

type NativeEditIntegration struct {
	Actor        string `json:"actor"`
	StartedAt    string `json:"startedAt"`
	IntegratedAt string `json:"integratedAt,omitempty"`
}

type NativeEditRetirement struct {
	Disposition string `json:"disposition"`
	Actor       string `json:"actor"`
	StartedAt   string `json:"startedAt"`
	RetiredAt   string `json:"retiredAt,omitempty"`
}

type NativeEditLifecycleResult struct {
	TicketID    string                 `json:"ticketId"`
	State       string                 `json:"state"`
	Artifact    NativeEditArtifact     `json:"artifact"`
	Approval    *NativeEditApproval    `json:"approval,omitempty"`
	Integration *NativeEditIntegration `json:"integration,omitempty"`
	Retirement  *NativeEditRetirement  `json:"retirement,omitempty"`
}

type EditLifecycleRuntime interface {
	Runtime
	InspectNativeEdit(context.Context, string, NativeEditInspectRequest) (NativeEditLifecycleResult, error)
	IssueNativeEditReview(context.Context, string, NativeEditReviewRequest) (NativeEditReviewResult, error)
	ApproveNativeEdit(context.Context, string, NativeEditApprovalRequest) (NativeEditLifecycleResult, error)
	IntegrateNativeEdit(context.Context, string, NativeEditActionRequest) (NativeEditLifecycleResult, error)
	RetireNativeEdit(context.Context, string, NativeEditActionRequest) (NativeEditLifecycleResult, error)
	DiscardNativeEdit(context.Context, string, NativeEditActionRequest) (NativeEditLifecycleResult, error)
}

func ValidateNativeEditInspect(request NativeEditInspectRequest) error {
	if !validNativeIdentity(request.TicketID) {
		return ErrInvalid
	}
	return nil
}

func ValidateNativeEditReview(request NativeEditReviewRequest) error {
	if !validNativeIdentity(request.TicketID) || len(request.Manifest) == 0 ||
		len(request.Manifest) > MaxNativeEditReviewManifestBytes || !json.Valid(request.Manifest) {
		return ErrInvalid
	}
	return nil
}

func ValidateNativeEditApproval(request NativeEditApprovalRequest) error {
	if ValidateNativeEditAction(NativeEditActionRequest{
		TicketID: request.TicketID, ManifestSHA: request.ManifestSHA, Actor: request.Actor,
	}) != nil || !validRawSHA256(request.ReviewReceiptID) {
		return ErrInvalid
	}
	return nil
}

func ValidateNativeEditAction(request NativeEditActionRequest) error {
	actor := strings.TrimSpace(request.Actor)
	if !validNativeIdentity(request.TicketID) || !validSHA256(request.ManifestSHA) || actor == "" ||
		len(actor) > MaxNativeEditActorBytes || !utf8.ValidString(actor) || strings.ContainsRune(actor, '\x00') {
		return ErrInvalid
	}
	return nil
}

func validRawSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
