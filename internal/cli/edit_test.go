package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/bridge"
)

type fakeEditLifecycleRuntime struct {
	fakeBridgeRuntime
	result       bridge.NativeEditLifecycleResult
	reviewResult bridge.NativeEditReviewResult
	inspect      bridge.NativeEditInspectRequest
	review       bridge.NativeEditReviewRequest
	approval     bridge.NativeEditApprovalRequest
	action       bridge.NativeEditActionRequest
	method       string
}

func (runtime *fakeEditLifecycleRuntime) InspectNativeEdit(_ context.Context, _ string, request bridge.NativeEditInspectRequest) (bridge.NativeEditLifecycleResult, error) {
	runtime.inspect, runtime.method = request, "inspect"
	return runtime.result, runtime.err
}

func (runtime *fakeEditLifecycleRuntime) IssueNativeEditReview(_ context.Context, _ string, request bridge.NativeEditReviewRequest) (bridge.NativeEditReviewResult, error) {
	runtime.review, runtime.method = request, "review"
	return runtime.reviewResult, runtime.err
}

func (runtime *fakeEditLifecycleRuntime) recordEditAction(method string, request bridge.NativeEditActionRequest) (bridge.NativeEditLifecycleResult, error) {
	runtime.action, runtime.method = request, method
	return runtime.result, runtime.err
}

func (runtime *fakeEditLifecycleRuntime) ApproveNativeEdit(_ context.Context, _ string, request bridge.NativeEditApprovalRequest) (bridge.NativeEditLifecycleResult, error) {
	runtime.approval, runtime.method = request, "approve"
	return runtime.result, runtime.err
}

func (runtime *fakeEditLifecycleRuntime) IntegrateNativeEdit(_ context.Context, _ string, request bridge.NativeEditActionRequest) (bridge.NativeEditLifecycleResult, error) {
	return runtime.recordEditAction("integrate", request)
}

func (runtime *fakeEditLifecycleRuntime) RetireNativeEdit(_ context.Context, _ string, request bridge.NativeEditActionRequest) (bridge.NativeEditLifecycleResult, error) {
	return runtime.recordEditAction("retire", request)
}

func (runtime *fakeEditLifecycleRuntime) DiscardNativeEdit(_ context.Context, _ string, request bridge.NativeEditActionRequest) (bridge.NativeEditLifecycleResult, error) {
	return runtime.recordEditAction("discard", request)
}

func TestEditLifecycleRoutesExactCommands(t *testing.T) {
	manifest := "sha256-" + strings.Repeat("a", 64)
	receipt := strings.Repeat("b", 64)
	runtime := &fakeEditLifecycleRuntime{result: bridge.NativeEditLifecycleResult{
		TicketID: "ticket-1", State: "approved",
	}, reviewResult: bridge.NativeEditReviewResult{TicketID: "ticket-1", ReceiptID: receipt, State: "active"}}
	workspace := t.TempDir()

	var stdout, stderr bytes.Buffer
	if code := runEditLifecycle(context.Background(), []string{
		"inspect", "--workspace", workspace, "--ticket", "ticket-1",
	}, &stdout, &stderr, runtime); code != 0 || runtime.method != "inspect" || runtime.inspect.TicketID != "ticket-1" ||
		!strings.Contains(stdout.String(), `"state":"approved"`) || stderr.Len() != 0 {
		t.Fatalf("inspect code=%d method=%q request=%#v stdout=%q stderr=%q", code, runtime.method, runtime.inspect, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	runtime.method = ""
	if code := runEditLifecycle(context.Background(), []string{
		"review", "--workspace", workspace, "--ticket", "ticket-1", "--review-manifest", deliveryManifestFixture(t),
	}, &stdout, &stderr, runtime); code != 0 || runtime.method != "review" || runtime.review.TicketID != "ticket-1" ||
		!strings.Contains(stdout.String(), receipt) || stderr.Len() != 0 {
		t.Fatalf("review code=%d method=%q request=%#v stdout=%q stderr=%q", code, runtime.method, runtime.review, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	runtime.method = ""
	if code := runEditLifecycle(context.Background(), []string{
		"approve", "--workspace", workspace, "--ticket", "ticket-1", "--manifest", manifest, "--receipt", receipt, "--actor", "maintainer",
	}, &stdout, &stderr, runtime); code != 0 || runtime.method != "approve" || runtime.approval.TicketID != "ticket-1" ||
		runtime.approval.ManifestSHA != manifest || runtime.approval.ReviewReceiptID != receipt || runtime.approval.Actor != "maintainer" || stderr.Len() != 0 {
		t.Fatalf("approve code=%d method=%q request=%#v stdout=%q stderr=%q", code, runtime.method, runtime.approval, stdout.String(), stderr.String())
	}

	for _, method := range []string{"integrate", "retire", "discard"} {
		stdout.Reset()
		stderr.Reset()
		runtime.method = ""
		if code := runEditLifecycle(context.Background(), []string{
			method, "--workspace", workspace, "--ticket", "ticket-1", "--manifest", manifest, "--actor", "maintainer",
		}, &stdout, &stderr, runtime); code != 0 || runtime.method != method || runtime.action.TicketID != "ticket-1" ||
			runtime.action.ManifestSHA != manifest || runtime.action.Actor != "maintainer" || stderr.Len() != 0 {
			t.Fatalf("%s code=%d method=%q request=%#v stdout=%q stderr=%q", method, code, runtime.method, runtime.action, stdout.String(), stderr.String())
		}
	}
}

func TestEditLifecycleRejectsAmbiguousOrUnavailableCommands(t *testing.T) {
	workspace := t.TempDir()
	runtime := &fakeEditLifecycleRuntime{}
	var stdout, stderr bytes.Buffer
	if code := runEditLifecycle(context.Background(), []string{
		"inspect", "--workspace", workspace, "--ticket", "ticket-1", "--actor", "maintainer",
	}, &stdout, &stderr, runtime); code != 2 || runtime.method != "" {
		t.Fatalf("inspect accepted action flags: code=%d method=%q stderr=%q", code, runtime.method, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runEditLifecycle(context.Background(), []string{
		"review", "--workspace", workspace, "--ticket", "ticket-1",
	}, &stdout, &stderr, runtime); code != 2 || runtime.method != "" {
		t.Fatalf("review accepted a missing manifest: code=%d method=%q stderr=%q", code, runtime.method, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runEditLifecycle(context.Background(), []string{
		"approve", "--workspace", workspace, "--ticket", "ticket-1",
		"--manifest", "sha256-" + strings.Repeat("a", 64), "--actor", "maintainer",
	}, &stdout, &stderr, runtime); code != 2 || runtime.method != "" {
		t.Fatalf("approval accepted a missing receipt: code=%d method=%q stderr=%q", code, runtime.method, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	baseRuntime := &fakeBridgeRuntime{}
	if code := runEditLifecycle(context.Background(), []string{
		"inspect", "--workspace", workspace, "--ticket", "ticket-1",
	}, &stdout, &stderr, baseRuntime); code != 1 || !strings.Contains(stderr.String(), "not configured") {
		t.Fatalf("missing runtime was not reported: code=%d stderr=%q", code, stderr.String())
	}
}
