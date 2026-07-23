package bridge

import (
	"context"
	"strings"
	"unicode/utf8"
)

const MaxNativeEditActorBytes = 256

type NativeEditInspectRequest struct {
	TicketID string `json:"ticketId"`
}

type NativeEditActionRequest struct {
	TicketID    string `json:"ticketId"`
	ManifestSHA string `json:"manifestSha256"`
	Actor       string `json:"actor"`
}

type NativeEditApproval struct {
	ManifestSHA  string `json:"manifestSha256"`
	BaseRevision string `json:"baseRevision"`
	Actor        string `json:"actor"`
	ApprovedAt   string `json:"approvedAt"`
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
	ApproveNativeEdit(context.Context, string, NativeEditActionRequest) (NativeEditLifecycleResult, error)
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

func ValidateNativeEditAction(request NativeEditActionRequest) error {
	actor := strings.TrimSpace(request.Actor)
	if !validNativeIdentity(request.TicketID) || !validSHA256(request.ManifestSHA) || actor == "" ||
		len(actor) > MaxNativeEditActorBytes || !utf8.ValidString(actor) || strings.ContainsRune(actor, '\x00') {
		return ErrInvalid
	}
	return nil
}
