package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/delivery"
)

type fakeDeliveryRuntime struct {
	issueRequest    delivery.IssueRequest
	validateRequest delivery.ValidateRequest
	err             error
}

func (fake *fakeDeliveryRuntime) Issue(_ context.Context, _ config.Options, request delivery.IssueRequest) (delivery.Receipt, error) {
	fake.issueRequest = request
	return delivery.Receipt{Kind: "delivery.review-receipt", ReceiptID: strings.Repeat("a", 64)}, fake.err
}
func (fake *fakeDeliveryRuntime) Status(context.Context, config.Options) (delivery.Status, error) {
	return delivery.Status{}, fake.err
}
func (fake *fakeDeliveryRuntime) Validate(_ context.Context, _ config.Options, request delivery.ValidateRequest) (delivery.Validation, error) {
	fake.validateRequest = request
	return delivery.Validation{Gate: request.Gate, State: "valid"}, fake.err
}
func (fake *fakeDeliveryRuntime) Invalidate(context.Context, config.Options, string) (delivery.Current, error) {
	return delivery.Current{State: "invalidated"}, fake.err
}

func TestDeliveryCLIReadsManifestAndForwardsExplicitGate(t *testing.T) {
	path := deliveryManifestFixture(t)
	fake := &fakeDeliveryRuntime{}
	var stdout, stderr bytes.Buffer
	code := runDelivery(context.Background(), []string{"issue", "--manifest", path, "--base-ref", "origin/main"}, &stdout, &stderr, fake)
	if code != 0 || stderr.Len() != 0 || fake.issueRequest.BaseRef != "origin/main" || fake.issueRequest.Manifest.SchemaVersion != "1" || !strings.Contains(stdout.String(), "delivery.review-receipt") {
		t.Fatalf("issue code=%d stdout=%q stderr=%q request=%+v", code, stdout.String(), stderr.String(), fake.issueRequest)
	}
	stdout.Reset()
	code = runDelivery(context.Background(), []string{"validate", "--manifest", path, "--gate", "pre-push"}, &stdout, &stderr, fake)
	if code != 0 || fake.validateRequest.Gate != delivery.GatePrePush || !strings.Contains(stdout.String(), `"state":"valid"`) {
		t.Fatalf("validate code=%d stdout=%q stderr=%q request=%+v", code, stdout.String(), stderr.String(), fake.validateRequest)
	}
}

func TestDeliveryCLIRejectsMissingManifestAndMapsInvalidation(t *testing.T) {
	fake := &fakeDeliveryRuntime{}
	var stdout, stderr bytes.Buffer
	if code := runDelivery(context.Background(), []string{"issue"}, &stdout, &stderr, fake); code != 2 || !strings.Contains(stderr.String(), "delivery request is invalid") {
		t.Fatalf("missing manifest code=%d stderr=%q", code, stderr.String())
	}
	stderr.Reset()
	fake.err = delivery.ErrInvalidated
	path := deliveryManifestFixture(t)
	if code := runDelivery(context.Background(), []string{"validate", "--manifest", path, "--gate", "pre-pr"}, &stdout, &stderr, fake); code != 1 || !strings.Contains(stderr.String(), "invalidated") {
		t.Fatalf("invalidated code=%d stderr=%q", code, stderr.String())
	}
}

func TestDeliveryCLIMapsUnboundSubmoduleScope(t *testing.T) {
	fake := &fakeDeliveryRuntime{err: delivery.ErrUnbound}
	var stdout, stderr bytes.Buffer
	path := deliveryManifestFixture(t)
	if code := runDelivery(context.Background(), []string{"issue", "--manifest", path}, &stdout, &stderr, fake); code != 1 || !strings.Contains(stderr.String(), "unbound submodule") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func deliveryManifestFixture(t *testing.T) string {
	t.Helper()
	sha := strings.Repeat("0", 64)
	identity := func(id string) delivery.Identity { return delivery.Identity{ID: id, Version: "1", SHA256: sha} }
	manifest := delivery.Manifest{
		SchemaVersion: "1",
		Context:       delivery.ContextManifest{Policy: identity("policy"), Prompt: identity("prompt"), Registry: identity("registry"), Provider: identity("provider"), Model: identity("model")},
		Evidence:      delivery.EvidenceManifest{Checks: []delivery.EvidenceCheck{{ID: "test", Command: "go test ./...", ExitCode: 0, OutputSHA256: sha, StartedAt: "2026-07-22T12:00:00Z", FinishedAt: "2026-07-22T12:01:00Z"}}},
		Review:        delivery.ReviewManifest{Risk: "low", Lenses: []string{}, Verdict: "approved", Findings: []delivery.ReviewFinding{}, RollbackBoundary: "revert"},
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
