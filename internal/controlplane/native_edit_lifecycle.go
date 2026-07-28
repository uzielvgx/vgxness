package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/delivery"
	"github.com/vgxness/vgxness/internal/orchestrator"
)

const nativeEditRetirementTimeout = 15 * time.Second

type nativeEditLifecycleDocument struct {
	State       string                        `json:"state"`
	Approval    *bridge.NativeEditApproval    `json:"approval,omitempty"`
	Integration *bridge.NativeEditIntegration `json:"integration,omitempty"`
	Retirement  *bridge.NativeEditRetirement  `json:"retirement,omitempty"`
}

func (service *Service) InspectNativeEdit(ctx context.Context, workspace string, request bridge.NativeEditInspectRequest) (bridge.NativeEditLifecycleResult, error) {
	if err := bridge.ValidateNativeEditInspect(request); err != nil {
		return bridge.NativeEditLifecycleResult{}, err
	}
	_, _, document, release, err := service.openNativeTicket(ctx, workspace, request.TicketID)
	if err != nil {
		return bridge.NativeEditLifecycleResult{}, err
	}
	defer release()
	if _, err := nativeTicketEditArtifact(document); err != nil {
		return bridge.NativeEditLifecycleResult{}, err
	}
	if nativeEditWorktreeMustExist(document.EditLifecycle) {
		if _, err := validateLiveNativeEditArtifact(ctx, document, ""); err != nil {
			return bridge.NativeEditLifecycleResult{}, err
		}
	}
	return nativeEditLifecycleResult(document)
}

func (service *Service) IssueNativeEditReview(ctx context.Context, workspace string, request bridge.NativeEditReviewRequest) (bridge.NativeEditReviewResult, error) {
	if err := bridge.ValidateNativeEditReview(request); err != nil {
		return bridge.NativeEditReviewResult{}, err
	}
	manifest, err := decodeNativeEditReviewManifest(request.Manifest)
	if err != nil {
		return bridge.NativeEditReviewResult{}, err
	}
	root, paths, document, release, err := service.openNativeTicket(ctx, workspace, request.TicketID)
	if err != nil {
		return bridge.NativeEditReviewResult{}, err
	}
	defer release()
	integrationLock, err := acquireNativeEditIntegrationLock(ctx, paths.Root)
	if err != nil {
		return bridge.NativeEditReviewResult{}, err
	}
	defer integrationLock.Release()
	artifact, err := validateLiveNativeEditArtifact(ctx, document, "")
	if err != nil {
		return bridge.NativeEditReviewResult{}, err
	}
	if document.EditLifecycle != nil && document.EditLifecycle.State != "approved" {
		return bridge.NativeEditReviewResult{}, bridge.ErrDenied
	}
	reviewer, err := delivery.New(document.Edit.Root, service.dispatcher)
	if err != nil {
		return bridge.NativeEditReviewResult{}, bridge.ErrExecution
	}
	receipt, err := reviewer.Issue(ctx, nativeEditDeliveryOptions(service, root), delivery.IssueRequest{
		BaseRef: artifact.BaseRevision, Manifest: manifest,
	})
	if err != nil {
		return bridge.NativeEditReviewResult{}, normalizeNativeEditDeliveryError(ctx, err)
	}
	if err := validateNativeEditReviewTarget(artifact, receipt.Target); err != nil {
		return bridge.NativeEditReviewResult{}, err
	}
	return bridge.NativeEditReviewResult{
		TicketID: request.TicketID, Artifact: artifact, ReceiptID: receipt.ReceiptID, State: "active",
		CandidateTree: receipt.Target.CandidateTree, ReviewSHA256: receipt.Bindings.ReviewSHA256,
	}, nil
}

func (service *Service) ApproveNativeEdit(ctx context.Context, workspace string, request bridge.NativeEditApprovalRequest) (bridge.NativeEditLifecycleResult, error) {
	if err := bridge.ValidateNativeEditApproval(request); err != nil {
		return bridge.NativeEditLifecycleResult{}, err
	}
	root, paths, document, release, err := service.openNativeTicket(ctx, workspace, request.TicketID)
	if err != nil {
		return bridge.NativeEditLifecycleResult{}, err
	}
	defer release()
	integrationLock, err := acquireNativeEditIntegrationLock(ctx, paths.Root)
	if err != nil {
		return bridge.NativeEditLifecycleResult{}, err
	}
	defer integrationLock.Release()
	artifact, err := nativeTicketEditArtifact(document)
	if err != nil || artifact.ManifestSHA != request.ManifestSHA {
		return bridge.NativeEditLifecycleResult{}, bridge.ErrDenied
	}
	if document.EditLifecycle != nil {
		if document.EditLifecycle.State == "retiring" || document.EditLifecycle.State == "retired" {
			if document.EditLifecycle.Approval != nil &&
				document.EditLifecycle.Approval.ManifestSHA == request.ManifestSHA &&
				document.EditLifecycle.Approval.BaseRevision == artifact.BaseRevision &&
				document.EditLifecycle.Approval.ReviewReceiptID == request.ReviewReceiptID {
				return nativeEditLifecycleResult(document)
			}
			return bridge.NativeEditLifecycleResult{}, bridge.ErrDenied
		}
		if _, err := validateLiveNativeEditArtifact(ctx, document, request.ManifestSHA); err != nil {
			return bridge.NativeEditLifecycleResult{}, err
		}
		switch document.EditLifecycle.State {
		case "approved":
		case "applying", "integrated":
			if document.EditLifecycle.Approval != nil &&
				document.EditLifecycle.Approval.ManifestSHA == request.ManifestSHA &&
				document.EditLifecycle.Approval.ReviewReceiptID == request.ReviewReceiptID {
				return nativeEditLifecycleResult(document)
			}
			return bridge.NativeEditLifecycleResult{}, bridge.ErrDenied
		default:
			return bridge.NativeEditLifecycleResult{}, bridge.ErrDenied
		}
	}
	if err := validateNativeEditSourceClean(ctx, root, document, artifact); err != nil {
		return bridge.NativeEditLifecycleResult{}, err
	}
	review, err := validateNativeEditReviewReceipt(ctx, service, root, document, artifact, request.ReviewReceiptID)
	if err != nil {
		return bridge.NativeEditLifecycleResult{}, err
	}
	if document.EditLifecycle != nil && document.EditLifecycle.Approval != nil &&
		document.EditLifecycle.Approval.ManifestSHA == request.ManifestSHA &&
		document.EditLifecycle.Approval.BaseRevision == artifact.BaseRevision &&
		document.EditLifecycle.Approval.ReviewReceiptID == request.ReviewReceiptID {
		return nativeEditLifecycleResult(document)
	}
	now := service.now().UTC().Format(time.RFC3339Nano)
	if document.EditLifecycle == nil {
		document.EditLifecycle = &nativeEditLifecycleDocument{State: "approved"}
	}
	document.EditLifecycle.Approval = &bridge.NativeEditApproval{
		ManifestSHA: artifact.ManifestSHA, BaseRevision: artifact.BaseRevision,
		ReviewReceiptID: review.ReceiptID, CandidateTree: review.Target.CandidateTree, ReviewSHA256: review.ReviewSHA256,
		Actor: strings.TrimSpace(request.Actor), ApprovedAt: now,
	}
	if err := writeNativeTicket(paths.Root, document); err != nil {
		return bridge.NativeEditLifecycleResult{}, fmt.Errorf("%w: persist native edit approval", bridge.ErrExecution)
	}
	return nativeEditLifecycleResult(document)
}

func (service *Service) IntegrateNativeEdit(ctx context.Context, workspace string, request bridge.NativeEditActionRequest) (bridge.NativeEditLifecycleResult, error) {
	if err := bridge.ValidateNativeEditAction(request); err != nil {
		return bridge.NativeEditLifecycleResult{}, err
	}
	root, paths, document, release, err := service.openNativeTicket(ctx, workspace, request.TicketID)
	if err != nil {
		return bridge.NativeEditLifecycleResult{}, err
	}
	defer release()
	integrationLock, err := acquireNativeEditIntegrationLock(ctx, paths.Root)
	if err != nil {
		return bridge.NativeEditLifecycleResult{}, err
	}
	defer integrationLock.Release()
	artifact, err := nativeTicketEditArtifact(document)
	if err != nil || artifact.ManifestSHA != request.ManifestSHA {
		return bridge.NativeEditLifecycleResult{}, bridge.ErrDenied
	}
	lifecycle := document.EditLifecycle
	if lifecycle == nil || lifecycle.Approval == nil || lifecycle.Approval.ManifestSHA != request.ManifestSHA ||
		lifecycle.Approval.BaseRevision != artifact.BaseRevision {
		return bridge.NativeEditLifecycleResult{}, bridge.ErrDenied
	}
	if lifecycle.State == "retiring" || lifecycle.State == "retired" {
		if lifecycle.Integration == nil {
			return bridge.NativeEditLifecycleResult{}, bridge.ErrDenied
		}
		return nativeEditLifecycleResult(document)
	}
	if _, err := validateLiveNativeEditArtifact(ctx, document, request.ManifestSHA); err != nil {
		return bridge.NativeEditLifecycleResult{}, err
	}
	switch lifecycle.State {
	case "integrated":
		if lifecycle.Integration == nil {
			return bridge.NativeEditLifecycleResult{}, bridge.ErrDenied
		}
		return nativeEditLifecycleResult(document)
	case "approved":
		if lifecycle.Approval.ReviewReceiptID == "" || lifecycle.Approval.CandidateTree == "" ||
			lifecycle.Approval.ReviewSHA256 == "" {
			return bridge.NativeEditLifecycleResult{}, bridge.ErrDenied
		}
		review, err := validateNativeEditReviewReceipt(ctx, service, root, document, artifact, lifecycle.Approval.ReviewReceiptID)
		if err != nil {
			return bridge.NativeEditLifecycleResult{}, err
		}
		if review.Target.CandidateTree != lifecycle.Approval.CandidateTree ||
			review.ReviewSHA256 != lifecycle.Approval.ReviewSHA256 {
			return bridge.NativeEditLifecycleResult{}, bridge.ErrDenied
		}
		if err := validateNativeEditSourceClean(ctx, root, document, artifact); err != nil {
			return bridge.NativeEditLifecycleResult{}, err
		}
		lifecycle.State = "applying"
		lifecycle.Integration = &bridge.NativeEditIntegration{
			Actor: strings.TrimSpace(request.Actor), StartedAt: service.now().UTC().Format(time.RFC3339Nano),
		}
		if err := writeNativeTicket(paths.Root, document); err != nil {
			return bridge.NativeEditLifecycleResult{}, fmt.Errorf("%w: persist native edit integration start", bridge.ErrExecution)
		}
	case "applying":
		if lifecycle.Integration == nil {
			return bridge.NativeEditLifecycleResult{}, bridge.ErrDenied
		}
	default:
		return bridge.NativeEditLifecycleResult{}, bridge.ErrDenied
	}
	if err := applyNativeEditArtifact(ctx, root, document, artifact); err != nil {
		return bridge.NativeEditLifecycleResult{}, err
	}
	lifecycle.State = "integrated"
	lifecycle.Integration.IntegratedAt = service.now().UTC().Format(time.RFC3339Nano)
	if err := writeNativeTicket(paths.Root, document); err != nil {
		return bridge.NativeEditLifecycleResult{}, fmt.Errorf("%w: persist native edit integration", bridge.ErrExecution)
	}
	return nativeEditLifecycleResult(document)
}

func (service *Service) RetireNativeEdit(ctx context.Context, workspace string, request bridge.NativeEditActionRequest) (bridge.NativeEditLifecycleResult, error) {
	return service.retireNativeEdit(ctx, workspace, request, "retired")
}

func (service *Service) DiscardNativeEdit(ctx context.Context, workspace string, request bridge.NativeEditActionRequest) (bridge.NativeEditLifecycleResult, error) {
	return service.retireNativeEdit(ctx, workspace, request, "discarded")
}

func (service *Service) retireNativeEdit(ctx context.Context, workspace string, request bridge.NativeEditActionRequest, disposition string) (bridge.NativeEditLifecycleResult, error) {
	if err := bridge.ValidateNativeEditAction(request); err != nil || disposition != "retired" && disposition != "discarded" {
		return bridge.NativeEditLifecycleResult{}, bridge.ErrInvalid
	}
	root, paths, document, release, err := service.openNativeTicket(ctx, workspace, request.TicketID)
	if err != nil {
		return bridge.NativeEditLifecycleResult{}, err
	}
	defer release()
	integrationLock, err := acquireNativeEditIntegrationLock(ctx, paths.Root)
	if err != nil {
		return bridge.NativeEditLifecycleResult{}, err
	}
	defer integrationLock.Release()
	artifact, err := nativeTicketEditArtifact(document)
	if err != nil || artifact.ManifestSHA != request.ManifestSHA {
		return bridge.NativeEditLifecycleResult{}, bridge.ErrDenied
	}
	lifecycle := document.EditLifecycle
	transitioning := "discarding"
	if disposition == "retired" {
		transitioning = "retiring"
		if lifecycle == nil || lifecycle.Integration == nil {
			return bridge.NativeEditLifecycleResult{}, bridge.ErrDenied
		}
		switch lifecycle.State {
		case "retired":
			return nativeEditLifecycleResult(document)
		case "integrated":
			if _, err := validateLiveNativeEditArtifact(ctx, document, request.ManifestSHA); err != nil {
				return bridge.NativeEditLifecycleResult{}, err
			}
			if err := validateIntegratedNativeEditSource(ctx, root, document, artifact); err != nil {
				return bridge.NativeEditLifecycleResult{}, err
			}
		case "retiring":
			if err := validateIntegratedNativeEditSource(ctx, root, document, artifact); err != nil {
				return bridge.NativeEditLifecycleResult{}, err
			}
		default:
			return bridge.NativeEditLifecycleResult{}, bridge.ErrDenied
		}
	} else {
		if lifecycle != nil && lifecycle.Integration != nil {
			return bridge.NativeEditLifecycleResult{}, bridge.ErrDenied
		}
		if lifecycle != nil && lifecycle.State == "discarded" {
			return nativeEditLifecycleResult(document)
		}
		if lifecycle == nil || lifecycle.State == "approved" {
			if _, err := validateLiveNativeEditArtifact(ctx, document, request.ManifestSHA); err != nil {
				return bridge.NativeEditLifecycleResult{}, err
			}
		} else if lifecycle.State != "discarding" {
			return bridge.NativeEditLifecycleResult{}, bridge.ErrDenied
		}
	}
	if lifecycle == nil {
		lifecycle = &nativeEditLifecycleDocument{}
		document.EditLifecycle = lifecycle
	}
	if lifecycle.State != transitioning {
		now := service.now().UTC().Format(time.RFC3339Nano)
		lifecycle.State = transitioning
		lifecycle.Retirement = &bridge.NativeEditRetirement{
			Disposition: disposition, Actor: strings.TrimSpace(request.Actor), StartedAt: now,
		}
		if err := writeNativeTicket(paths.Root, document); err != nil {
			return bridge.NativeEditLifecycleResult{}, fmt.Errorf("%w: persist native edit retirement start", bridge.ErrExecution)
		}
	}
	if err := removeNativeEditWorkspaceChecked(ctx, root, document.Edit); err != nil {
		return bridge.NativeEditLifecycleResult{}, err
	}
	lifecycle.State = disposition
	lifecycle.Retirement.RetiredAt = service.now().UTC().Format(time.RFC3339Nano)
	if err := writeNativeTicket(paths.Root, document); err != nil {
		return bridge.NativeEditLifecycleResult{}, fmt.Errorf("%w: persist native edit retirement", bridge.ErrExecution)
	}
	return nativeEditLifecycleResult(document)
}

func acquireNativeEditIntegrationLock(ctx context.Context, storageRoot string) (orchestrator.FileLock, error) {
	lock, err := acquireBoundedControlPlaneLock(ctx, filepath.Join(storageRoot, "native-edit-integration.lock"))
	if err == nil {
		return lock, nil
	}
	if errors.Is(err, orchestrator.ErrCoordinatorBusy) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return orchestrator.FileLock{}, bridge.ErrUnavailable
	}
	return orchestrator.FileLock{}, bridge.ErrDenied
}

type nativeEditReviewValidation struct {
	ReceiptID    string
	Target       delivery.TargetSnapshot
	ReviewSHA256 string
}

func decodeNativeEditReviewManifest(raw json.RawMessage) (delivery.Manifest, error) {
	if len(raw) == 0 || len(raw) > bridge.MaxNativeEditReviewManifestBytes {
		return delivery.Manifest{}, bridge.ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var manifest delivery.Manifest
	if decoder.Decode(&manifest) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return delivery.Manifest{}, bridge.ErrInvalid
	}
	return manifest, nil
}

func validateNativeEditReviewReceipt(
	ctx context.Context,
	service *Service,
	root string,
	document nativeTicketDocument,
	artifact bridge.NativeEditArtifact,
	receiptID string,
) (nativeEditReviewValidation, error) {
	reviewer, err := delivery.New(document.Edit.Root, service.dispatcher)
	if err != nil {
		return nativeEditReviewValidation{}, bridge.ErrExecution
	}
	options := nativeEditDeliveryOptions(service, root)
	status, err := reviewer.Status(ctx, options)
	if err != nil {
		return nativeEditReviewValidation{}, normalizeNativeEditDeliveryError(ctx, err)
	}
	if status.Current.State != "active" || status.Receipt.ReceiptID != receiptID {
		return nativeEditReviewValidation{}, bridge.ErrDenied
	}
	validation, err := reviewer.Validate(ctx, options, delivery.ValidateRequest{
		Gate: delivery.GatePostApply, BaseRef: artifact.BaseRevision,
		ReceiptID: receiptID, Manifest: status.Receipt.Manifest,
	})
	if err != nil {
		return nativeEditReviewValidation{}, normalizeNativeEditDeliveryError(ctx, err)
	}
	if validation.State != "valid" || validation.ReceiptID != receiptID ||
		!reflect.DeepEqual(validation.Target, status.Receipt.Target) {
		return nativeEditReviewValidation{}, bridge.ErrDenied
	}
	if err := validateNativeEditReviewTarget(artifact, validation.Target); err != nil {
		return nativeEditReviewValidation{}, err
	}
	return nativeEditReviewValidation{
		ReceiptID: receiptID, Target: validation.Target, ReviewSHA256: status.Receipt.Bindings.ReviewSHA256,
	}, nil
}

func validateNativeEditReviewTarget(artifact bridge.NativeEditArtifact, target delivery.TargetSnapshot) error {
	if target.BaseRevision != artifact.BaseRevision {
		return bridge.ErrDenied
	}
	paths := make([]string, len(artifact.Changes))
	for index, change := range artifact.Changes {
		paths[index] = change.Path
	}
	sort.Strings(paths)
	if !reflect.DeepEqual(paths, target.Paths) || target.CandidateTree == "" {
		return bridge.ErrDenied
	}
	return nil
}

func nativeEditDeliveryOptions(service *Service, root string) config.Options {
	return config.Options{StorageRoot: service.storageRoot, ProjectDir: root}
}

func normalizeNativeEditDeliveryError(ctx context.Context, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	switch {
	case errors.Is(err, delivery.ErrInvalid), errors.Is(err, delivery.ErrNotFound),
		errors.Is(err, delivery.ErrConflict), errors.Is(err, delivery.ErrInvalidated),
		errors.Is(err, delivery.ErrSensitive), errors.Is(err, delivery.ErrUnbound):
		return bridge.ErrDenied
	default:
		return bridge.ErrExecution
	}
}

func nativeTicketEditArtifact(document nativeTicketDocument) (bridge.NativeEditArtifact, error) {
	if document.State != "completed" || document.Input.Operation != bridge.WriteFiles && document.Input.Operation != bridge.RepairSystem || document.Edit == nil ||
		document.Response == nil || document.Response.EditArtifact == nil {
		return bridge.NativeEditArtifact{}, bridge.ErrDenied
	}
	artifact := *document.Response.EditArtifact
	artifact.Changes = append([]bridge.NativeEditResult(nil), artifact.Changes...)
	if artifact.Worktree != document.Edit.Root || artifact.BaseRevision != document.Edit.BaseRevision ||
		artifact.ManifestSHA == "" || len(artifact.Changes) == 0 {
		return bridge.NativeEditArtifact{}, bridge.ErrDenied
	}
	return artifact, nil
}

func validateLiveNativeEditArtifact(ctx context.Context, document nativeTicketDocument, expectedManifest string) (bridge.NativeEditArtifact, error) {
	stored, err := nativeTicketEditArtifact(document)
	if err != nil {
		return bridge.NativeEditArtifact{}, err
	}
	if expectedManifest != "" && stored.ManifestSHA != expectedManifest {
		return bridge.NativeEditArtifact{}, bridge.ErrDenied
	}
	live, err := finalizeNativeEditArtifact(ctx, document)
	if err != nil || !reflect.DeepEqual(live, stored) {
		return bridge.NativeEditArtifact{}, bridge.ErrDenied
	}
	return stored, nil
}

func nativeEditLifecycleResult(document nativeTicketDocument) (bridge.NativeEditLifecycleResult, error) {
	artifact, err := nativeTicketEditArtifact(document)
	if err != nil {
		return bridge.NativeEditLifecycleResult{}, err
	}
	result := bridge.NativeEditLifecycleResult{TicketID: document.TicketID, State: "pending-approval", Artifact: artifact}
	if document.EditLifecycle == nil {
		return result, nil
	}
	result.State = document.EditLifecycle.State
	if document.EditLifecycle.Approval != nil {
		value := *document.EditLifecycle.Approval
		result.Approval = &value
	}
	if document.EditLifecycle.Integration != nil {
		value := *document.EditLifecycle.Integration
		result.Integration = &value
	}
	if document.EditLifecycle.Retirement != nil {
		value := *document.EditLifecycle.Retirement
		result.Retirement = &value
	}
	return result, nil
}

func nativeEditWorktreeMustExist(lifecycle *nativeEditLifecycleDocument) bool {
	return lifecycle == nil || lifecycle.State != "retired" && lifecycle.State != "discarded"
}

func validateNativeEditSourceClean(ctx context.Context, root string, document nativeTicketDocument, artifact bridge.NativeEditArtifact) error {
	if err := validateNativeEditSourceIdentity(root, document); err != nil {
		return err
	}
	head, err := nativeEditSourceHead(ctx, root)
	if err != nil || head != artifact.BaseRevision {
		return bridge.ErrDenied
	}
	status, err := nativeEditSourceStatus(ctx, root)
	if err != nil {
		return err
	}
	if len(status) != 0 {
		return bridge.ErrDenied
	}
	return nil
}

func applyNativeEditArtifact(ctx context.Context, root string, document nativeTicketDocument, artifact bridge.NativeEditArtifact) error {
	if err := validateNativeEditSourceIdentity(root, document); err != nil {
		return err
	}
	head, err := nativeEditSourceHead(ctx, root)
	if err != nil || head != artifact.BaseRevision {
		return bridge.ErrDenied
	}
	changed, err := nativeEditSourceChangedPaths(ctx, root)
	if err != nil {
		return err
	}
	expected := make(map[string]bridge.NativeEditResult, len(artifact.Changes))
	for _, change := range artifact.Changes {
		expected[change.Path] = change
	}
	if len(changed) > len(expected) {
		return bridge.ErrDenied
	}
	for _, path := range changed {
		if _, ok := expected[path]; !ok {
			return bridge.ErrDenied
		}
	}
	for _, change := range artifact.Changes {
		var desired bridge.NativeReadResult
		desired, readErr := secureNativeRead(document.Edit.Root, document.Edit.RootIdentity, bridge.NativeReadRequest{
			Path: change.Path, Limit: bridge.MaxNativeEditBytes,
		})
		if change.Deleted {
			if !errors.Is(readErr, os.ErrNotExist) {
				return bridge.ErrDenied
			}
		} else if readErr != nil || desired.Truncated || nativeSHA256([]byte(desired.Content)) != change.SHA256 {
			return bridge.ErrDenied
		}
		if containsString(changed, change.Path) {
			current, currentErr := secureNativeRead(root, document.WorkspaceID, bridge.NativeReadRequest{
				Path: change.Path, Limit: bridge.MaxNativeEditBytes,
			})
			if change.Deleted {
				if !errors.Is(currentErr, os.ErrNotExist) {
					return bridge.ErrDenied
				}
			} else if currentErr != nil || current.Truncated || nativeSHA256([]byte(current.Content)) != change.SHA256 {
				return bridge.ErrDenied
			}
			continue
		}
		_, editErr := secureNativeEdit(root, document.WorkspaceID, bridge.NativeEditRequest{
			Path: change.Path, Content: desired.Content, ExpectedSHA256: change.PreviousSHA256, Create: change.Created, Delete: change.Deleted,
		})
		if editErr != nil {
			return editErr
		}
		changed = append(changed, change.Path)
	}
	return validateIntegratedNativeEditSourceAtBase(ctx, root, document, artifact)
}

func validateIntegratedNativeEditSource(ctx context.Context, root string, document nativeTicketDocument, artifact bridge.NativeEditArtifact) error {
	if err := validateNativeEditSourceIdentity(root, document); err != nil {
		return err
	}
	head, err := nativeEditSourceHead(ctx, root)
	if err != nil {
		return err
	}
	if head == artifact.BaseRevision {
		return validateIntegratedNativeEditSourceAtBase(ctx, root, document, artifact)
	}
	if _, err := runGitCommand(ctx, root, nativeGitArgs("merge-base", "--is-ancestor", artifact.BaseRevision, head), cleanGitEnvironment(nil)); err != nil {
		return bridge.ErrDenied
	}
	status, err := nativeEditSourceStatus(ctx, root)
	if err != nil || len(status) != 0 {
		return bridge.ErrDenied
	}
	return validateNativeEditSourceContents(root, document, artifact)
}

func validateIntegratedNativeEditSourceAtBase(ctx context.Context, root string, document nativeTicketDocument, artifact bridge.NativeEditArtifact) error {
	changed, err := nativeEditSourceChangedPaths(ctx, root)
	if err != nil || len(changed) != len(artifact.Changes) {
		return bridge.ErrDenied
	}
	expected := make(map[string]struct{}, len(artifact.Changes))
	for _, change := range artifact.Changes {
		expected[change.Path] = struct{}{}
	}
	for _, path := range changed {
		if _, ok := expected[path]; !ok {
			return bridge.ErrDenied
		}
	}
	return validateNativeEditSourceContents(root, document, artifact)
}

func validateNativeEditSourceContents(root string, document nativeTicketDocument, artifact bridge.NativeEditArtifact) error {
	for _, change := range artifact.Changes {
		current, err := secureNativeRead(root, document.WorkspaceID, bridge.NativeReadRequest{
			Path: change.Path, Limit: bridge.MaxNativeEditBytes,
		})
		if change.Deleted {
			if !errors.Is(err, os.ErrNotExist) {
				return bridge.ErrDenied
			}
		} else if err != nil || current.Truncated || nativeSHA256([]byte(current.Content)) != change.SHA256 {
			return bridge.ErrDenied
		}
	}
	return nil
}

func validateNativeEditSourceIdentity(root string, document nativeTicketDocument) error {
	info, err := os.Lstat(root)
	identity, ok := nativeFileIdentity(info)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !ok || identity != document.WorkspaceID {
		return bridge.ErrDenied
	}
	return nil
}

func nativeEditSourceHead(ctx context.Context, root string) (string, error) {
	output, err := runGitCommand(ctx, root, nativeGitArgs("rev-parse", "--verify", "HEAD^{commit}"), cleanGitEnvironment(nil))
	if err != nil {
		return "", fmt.Errorf("%w: inspect native edit source revision", bridge.ErrExecution)
	}
	return strings.TrimSpace(string(output)), nil
}

func nativeEditSourceStatus(ctx context.Context, root string) ([]byte, error) {
	status, err := runGitCommand(ctx, root, nativeGitArgs(
		"status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=none", "--", ".",
	), cleanGitEnvironment(nil))
	if err != nil {
		return nil, fmt.Errorf("%w: inspect native edit source", bridge.ErrExecution)
	}
	return status, nil
}

func nativeEditSourceChangedPaths(ctx context.Context, root string) ([]string, error) {
	status, err := nativeEditSourceStatus(ctx, root)
	if err != nil {
		return nil, err
	}
	paths, err := nativeChangedPaths(status)
	if err != nil {
		return nil, bridge.ErrDenied
	}
	return paths, nil
}

func removeNativeEditWorkspaceChecked(ctx context.Context, repository string, edit *nativeEditWorkspace) error {
	if edit == nil || edit.Root == "" {
		return bridge.ErrDenied
	}
	expectedContainer := filepath.Join(filepath.Dir(repository), filepath.Base(repository)+"-worktrees")
	if filepath.Dir(edit.Root) != expectedContainer || !strings.HasPrefix(filepath.Base(edit.Root), "vgxness-") {
		return bridge.ErrDenied
	}
	info, err := os.Lstat(edit.Root)
	if errors.Is(err, os.ErrNotExist) {
		registered, checkErr := nativeEditWorktreeRegistered(ctx, repository, edit.Root)
		if checkErr != nil || registered {
			return bridge.ErrDenied
		}
		return nil
	}
	identity, ok := nativeFileIdentity(info)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !ok || identity != edit.RootIdentity {
		return bridge.ErrDenied
	}
	removeContext, cancel := context.WithTimeout(ctx, nativeEditRetirementTimeout)
	defer cancel()
	if _, err := runGitCommand(removeContext, repository, nativeGitArgs("worktree", "remove", "--force", edit.Root), cleanGitEnvironment(nil)); err != nil {
		return fmt.Errorf("%w: retire native edit worktree", bridge.ErrExecution)
	}
	if _, err := os.Lstat(edit.Root); !errors.Is(err, os.ErrNotExist) {
		return bridge.ErrDenied
	}
	registered, err := nativeEditWorktreeRegistered(ctx, repository, edit.Root)
	if err != nil || registered {
		return bridge.ErrDenied
	}
	return nil
}

func nativeEditWorktreeRegistered(ctx context.Context, repository, worktree string) (bool, error) {
	output, err := runGitCommand(ctx, repository, nativeGitArgs("worktree", "list", "--porcelain", "-z"), cleanGitEnvironment(nil))
	if err != nil {
		return false, fmt.Errorf("%w: inspect native edit worktree registration", bridge.ErrExecution)
	}
	for _, record := range strings.Split(string(output), "\x00") {
		if strings.TrimPrefix(record, "worktree ") == worktree {
			return true, nil
		}
	}
	return false, nil
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

var _ bridge.EditLifecycleRuntime = (*Service)(nil)
