package controlplane

import (
	"context"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/hooks"
)

func (service *Service) dispatchCandidateFrozen(ctx context.Context, ticketID string, response bridge.Response) {
	if response.Status != "completed" || response.EditArtifact == nil {
		return
	}
	manifestDigest := strings.TrimPrefix(response.EditArtifact.ManifestSHA, "sha256-")
	at := service.now().UTC()
	if response.Receipt != nil && response.Receipt.FinishedAt != "" {
		if finished, err := time.Parse(time.RFC3339Nano, response.Receipt.FinishedAt); err == nil {
			at = finished.UTC()
		}
	}
	identity := nativeCompletionDigest(ticketID, manifestDigest)
	service.dispatcher.Dispatch(context.WithoutCancel(ctx), hooks.CandidateFrozen{
		Meta: hooks.Metadata{ID: "candidate-frozen-" + identity, At: at}, TicketID: ticketID, TaskID: response.TaskID,
		ManifestDigest: manifestDigest, ChangeCount: len(response.EditArtifact.Changes),
	})
}

func (service *Service) dispatchValidationCompleted(ctx context.Context, ticketID string, receipt bridge.NativeValidationReceipt, changeCount int) {
	receiptDigest := nativeCompletionDigest(receipt)
	at, _ := time.Parse(time.RFC3339Nano, receipt.FinishedAt)
	service.dispatcher.Dispatch(context.WithoutCancel(ctx), hooks.ValidationCompleted{
		Meta:     hooks.Metadata{ID: "validation-completed-" + nativeCompletionDigest(ticketID, receiptDigest), At: at.UTC()},
		TicketID: ticketID, ReceiptDigest: receiptDigest, Operation: string(receipt.Operation), Success: receipt.Success,
		ExitCode: receipt.ExitCode, PackageCount: len(receipt.Packages), ChangeCount: changeCount,
	})
}
