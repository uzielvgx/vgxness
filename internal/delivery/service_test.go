package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/hooks"
)

func TestIssueDispatchesAfterReceiptIsPersistedAndActive(t *testing.T) {
	repository := newRepository(t)
	writeProjectFile(t, repository, "feature.txt", "delivery authority\n")
	options := config.Options{StorageRoot: filepath.Join(t.TempDir(), "state")}
	var service *Service
	var calls atomic.Int32
	var identity string
	dispatcher, err := hooks.New(hooks.Options{HandlerTimeout: time.Second},
		func(ctx context.Context, event hooks.Event) error {
			installed, ok := event.(hooks.DeliveryInstalled)
			if !ok {
				return nil
			}
			status, statusErr := service.Status(ctx, options)
			if statusErr != nil || status.Current.State != "active" || status.Receipt.ReceiptID != installed.ReceiptID {
				t.Fatalf("hook observed inactive receipt: status=%+v err=%v", status, statusErr)
			}
			identity = installed.Meta.ID
			calls.Add(1)
			return nil
		},
		func(context.Context, hooks.Event) error { panic("observer failure") },
	)
	if err != nil {
		t.Fatal(err)
	}
	service, err = New(repository, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.Issue(context.Background(), options, IssueRequest{Manifest: validManifest()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Issue(context.Background(), options, IssueRequest{Manifest: validManifest()}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || identity != "delivery-installed-"+receipt.ReceiptID {
		t.Fatalf("calls=%d identity=%q receipt=%q", calls.Load(), identity, receipt.ReceiptID)
	}
}

func TestIssueDoesNotDispatchBeforeStoreCommit(t *testing.T) {
	repository := newRepository(t)
	writeProjectFile(t, repository, "feature.txt", "delivery authority\n")
	var calls atomic.Int32
	dispatcher, err := hooks.New(hooks.Options{}, func(context.Context, hooks.Event) error { calls.Add(1); return nil })
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(repository, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	storageRoot := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(storageRoot, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Issue(context.Background(), config.Options{StorageRoot: storageRoot}, IssueRequest{Manifest: validManifest()}); err == nil {
		t.Fatal("expected store failure")
	}
	if calls.Load() != 0 {
		t.Fatalf("uncommitted delivery dispatched %d times", calls.Load())
	}
}

func TestReceiptSurvivesWorktreeStageAndCommitRepresentations(t *testing.T) {
	repository := newRepository(t)
	writeProjectFile(t, repository, "feature.txt", "delivery authority\n")
	service, err := New(repository)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC) }
	options := config.Options{StorageRoot: filepath.Join(t.TempDir(), "state")}
	manifest := validManifest()
	receipt, err := service.Issue(context.Background(), options, IssueRequest{Manifest: manifest})
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Target.Paths) != 1 || receipt.Target.Paths[0] != "feature.txt" {
		t.Fatalf("unexpected target: %+v", receipt.Target)
	}
	unstaged, err := service.Validate(context.Background(), options, ValidateRequest{Gate: GatePostApply, Manifest: manifest})
	if err != nil || unstaged.State != "valid" {
		t.Fatalf("unstaged validation: %+v %v", unstaged, err)
	}
	git(t, repository, "add", "feature.txt")
	staged, err := service.Validate(context.Background(), options, ValidateRequest{Gate: GatePreCommit, Manifest: manifest})
	if err != nil || staged.State != "valid" {
		t.Fatalf("staged validation: %+v %v", staged, err)
	}
	git(t, repository, "commit", "-m", "feature")
	committed, err := service.Validate(context.Background(), options, ValidateRequest{Gate: GatePrePush, Manifest: manifest})
	if err != nil || committed.State != "valid" || committed.Target.CandidateTree != receipt.Target.CandidateTree {
		t.Fatalf("committed validation: %+v %v", committed, err)
	}
	service.now = func() time.Time { return time.Date(2026, 7, 22, 13, 0, 0, 0, time.UTC) }
	second, err := service.Issue(context.Background(), options, IssueRequest{BaseRef: receipt.Target.BaseRevision, Manifest: manifest})
	if err != nil || second.IssuedAt != receipt.IssuedAt || second.ReceiptID != receipt.ReceiptID {
		t.Fatalf("idempotent issue: %+v %v", second, err)
	}
}

func TestValidationDriftDurablyInvalidatesReceipt(t *testing.T) {
	repository := newRepository(t)
	writeProjectFile(t, repository, "feature.txt", "first\n")
	service, _ := New(repository)
	options := config.Options{StorageRoot: filepath.Join(t.TempDir(), "state")}
	manifest := validManifest()
	receipt, err := service.Issue(context.Background(), options, IssueRequest{Manifest: manifest})
	if err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, repository, "feature.txt", "second\n")
	validation, err := service.Validate(context.Background(), options, ValidateRequest{Gate: GatePreCommit, Manifest: manifest})
	if !errors.Is(err, ErrInvalidated) || validation.ReceiptID != receipt.ReceiptID || validation.State != "invalidated" {
		t.Fatalf("validation=%+v err=%v", validation, err)
	}
	status, err := service.Status(context.Background(), options)
	if err != nil || status.Current.State != "invalidated" || status.Current.Reason == "" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if _, err := service.Validate(context.Background(), options, ValidateRequest{Gate: GatePrePush, Manifest: manifest}); !errors.Is(err, ErrInvalidated) {
		t.Fatalf("expected durable invalidation, got %v", err)
	}
}

func TestValidationReceiptMismatchDoesNotInvalidateCurrentReceipt(t *testing.T) {
	repository := newRepository(t)
	writeProjectFile(t, repository, "feature.txt", "reviewed\n")
	service, _ := New(repository)
	options := config.Options{StorageRoot: filepath.Join(t.TempDir(), "state")}
	manifest := validManifest()
	receipt, err := service.Issue(context.Background(), options, IssueRequest{Manifest: manifest})
	if err != nil {
		t.Fatal(err)
	}
	mismatched := receipt.ReceiptID
	if mismatched[0] == '0' {
		mismatched = "1" + mismatched[1:]
	} else {
		mismatched = "0" + mismatched[1:]
	}
	if _, err := service.Validate(context.Background(), options, ValidateRequest{
		Gate: GatePostApply, ReceiptID: mismatched, Manifest: manifest,
	}); !errors.Is(err, ErrInvalidated) {
		t.Fatalf("mismatched receipt was accepted: %v", err)
	}
	status, err := service.Status(context.Background(), options)
	if err != nil || status.Current.State != "active" || status.Receipt.ReceiptID != receipt.ReceiptID {
		t.Fatalf("mismatch mutated current receipt: %+v err=%v", status, err)
	}
}

func TestManifestDriftInvalidatesAndExplicitInvalidationIsIdempotent(t *testing.T) {
	repository := newRepository(t)
	writeProjectFile(t, repository, "feature.txt", "first\n")
	service, _ := New(repository)
	options := config.Options{StorageRoot: filepath.Join(t.TempDir(), "state")}
	manifest := validManifest()
	if _, err := service.Issue(context.Background(), options, IssueRequest{Manifest: manifest}); err != nil {
		t.Fatal(err)
	}
	drift := manifest
	drift.Context.Model.Version = "changed"
	if _, err := service.Validate(context.Background(), options, ValidateRequest{Gate: GatePrePR, Manifest: drift}); !errors.Is(err, ErrInvalidated) {
		t.Fatalf("expected manifest invalidation, got %v", err)
	}
	current, err := service.Invalidate(context.Background(), options, "maintainer withdrew approval")
	if err != nil || current.Reason != "maintainer withdrew approval" {
		t.Fatalf("current=%+v err=%v", current, err)
	}
}

func TestSensitiveTargetAndInvalidReviewFailClosed(t *testing.T) {
	repository := newRepository(t)
	writeProjectFile(t, repository, ".env", "TOKEN=secret\n")
	service, _ := New(repository)
	options := config.Options{StorageRoot: filepath.Join(t.TempDir(), "state")}
	if _, err := service.Issue(context.Background(), options, IssueRequest{Manifest: validManifest()}); !errors.Is(err, ErrSensitive) {
		t.Fatalf("expected sensitive path denial, got %v", err)
	}
	os.Remove(filepath.Join(repository, ".env"))
	writeProjectFile(t, repository, "feature.txt", "safe\n")
	manifest := validManifest()
	manifest.Review.Lenses = []string{"review-risk"}
	if _, err := service.Issue(context.Background(), options, IssueRequest{Manifest: manifest}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected risk/lens rejection, got %v", err)
	}
}

func TestDirtySubmoduleContentIsNeverSilentlyOmitted(t *testing.T) {
	repository := newRepository(t)
	child := newRepository(t)
	git(t, repository, "-c", "protocol.file.allow=always", "submodule", "add", "-q", child, "vendor/child")
	git(t, repository, "commit", "-q", "-m", "add submodule")
	writeProjectFile(t, repository, "feature.txt", "outer change\n")
	writeProjectFile(t, filepath.Join(repository, "vendor", "child"), "README.md", "dirty nested change\n")
	service, _ := New(repository)
	options := config.Options{StorageRoot: filepath.Join(t.TempDir(), "state")}
	if _, err := service.Issue(context.Background(), options, IssueRequest{Manifest: validManifest()}); !errors.Is(err, ErrUnbound) {
		t.Fatalf("expected dirty submodule denial, got %v", err)
	}
}

func TestStatusDoesNotCreateMissingDeliveryStorage(t *testing.T) {
	repository := newRepository(t)
	service, _ := New(repository)
	root := filepath.Join(t.TempDir(), "state")
	if _, err := service.Status(context.Background(), config.Options{StorageRoot: root}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected missing receipt, got %v", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("status created storage: %v", err)
	}
}

func TestCorruptReceiptIsRejected(t *testing.T) {
	repository := newRepository(t)
	writeProjectFile(t, repository, "feature.txt", "safe\n")
	service, _ := New(repository)
	root := filepath.Join(t.TempDir(), "state")
	options := config.Options{StorageRoot: root}
	receipt, err := service.Issue(context.Background(), options, IssueRequest{Manifest: validManifest()})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "delivery", "receipts", receipt.ReceiptID+".json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Status(context.Background(), options); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected corruption, got %v", err)
	}
}

func validManifest() Manifest {
	digest := sha256.Sum256([]byte("fixture"))
	sha := hex.EncodeToString(digest[:])
	identity := func(id string) Identity { return Identity{ID: id, Version: "1", SHA256: sha} }
	return Manifest{
		SchemaVersion: SchemaVersion,
		Context:       ContextManifest{Policy: identity("policy"), Prompt: identity("prompt"), Registry: identity("registry"), Provider: identity("provider"), Model: identity("model")},
		Evidence:      EvidenceManifest{Checks: []EvidenceCheck{{ID: "go-test", Command: "go test ./...", ExitCode: 0, OutputSHA256: sha, StartedAt: "2026-07-22T11:00:00Z", FinishedAt: "2026-07-22T11:01:00Z", Toolchain: []Identity{identity("go")}}}},
		Review:        ReviewManifest{Risk: "high", Lenses: []string{"review-readability", "review-reliability", "review-resilience", "review-risk"}, Verdict: "approved", Findings: []ReviewFinding{}, RollbackBoundary: "revert the delivery authority change"},
	}
}

func newRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	git(t, repository, "init", "-q")
	git(t, repository, "config", "user.name", "VGXNESS Test")
	git(t, repository, "config", "user.email", "test@vgxness.dev")
	writeProjectFile(t, repository, "README.md", "base\n")
	git(t, repository, "add", "README.md")
	git(t, repository, "commit", "-q", "-m", "base")
	return repository
}

func writeProjectFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, repository string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func TestManifestNormalizationIsDeterministic(t *testing.T) {
	manifest := validManifest()
	manifest.Review.Lenses[0], manifest.Review.Lenses[3] = manifest.Review.Lenses[3], manifest.Review.Lenses[0]
	normalized, err := normalizeManifest(context.Background(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	again, err := normalizeManifest(context.Background(), normalized)
	if err != nil || !reflect.DeepEqual(normalized, again) {
		t.Fatalf("normalization is not stable: %v", err)
	}
}
