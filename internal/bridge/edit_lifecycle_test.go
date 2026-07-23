package bridge

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateNativeEditLifecycleRequests(t *testing.T) {
	manifest := "sha256-" + strings.Repeat("a", 64)
	receipt := strings.Repeat("b", 64)
	if err := ValidateNativeEditInspect(NativeEditInspectRequest{TicketID: "ticket-1"}); err != nil {
		t.Fatalf("valid inspection was rejected: %v", err)
	}
	if err := ValidateNativeEditAction(NativeEditActionRequest{
		TicketID: "ticket-1", ManifestSHA: manifest, Actor: " maintainer ",
	}); err != nil {
		t.Fatalf("valid lifecycle action was rejected: %v", err)
	}
	if err := ValidateNativeEditReview(NativeEditReviewRequest{
		TicketID: "ticket-1", Manifest: []byte(`{"schemaVersion":"1"}`),
	}); err != nil {
		t.Fatalf("valid review request was rejected: %v", err)
	}
	if err := ValidateNativeEditApproval(NativeEditApprovalRequest{
		TicketID: "ticket-1", ManifestSHA: manifest, ReviewReceiptID: receipt, Actor: "maintainer",
	}); err != nil {
		t.Fatalf("valid approval request was rejected: %v", err)
	}

	for name, request := range map[string]NativeEditActionRequest{
		"ticket":   {TicketID: "../ticket", ManifestSHA: manifest, Actor: "maintainer"},
		"manifest": {TicketID: "ticket-1", ManifestSHA: "sha256-bad", Actor: "maintainer"},
		"actor":    {TicketID: "ticket-1", ManifestSHA: manifest, Actor: " \t "},
		"nul":      {TicketID: "ticket-1", ManifestSHA: manifest, Actor: "maintainer\x00admin"},
		"oversize": {TicketID: "ticket-1", ManifestSHA: manifest, Actor: strings.Repeat("a", MaxNativeEditActorBytes+1)},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateNativeEditAction(request); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid request was accepted: %v", err)
			}
		})
	}
	if err := ValidateNativeEditInspect(NativeEditInspectRequest{TicketID: ""}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid inspection was accepted: %v", err)
	}
	if err := ValidateNativeEditReview(NativeEditReviewRequest{
		TicketID: "ticket-1", Manifest: []byte(`not-json`),
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid review was accepted: %v", err)
	}
	if err := ValidateNativeEditApproval(NativeEditApprovalRequest{
		TicketID: "ticket-1", ManifestSHA: manifest, ReviewReceiptID: "sha256-" + receipt, Actor: "maintainer",
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid receipt identity was accepted: %v", err)
	}
}
