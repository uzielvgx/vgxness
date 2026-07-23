package controlplane

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/providers"
)

func TestNativeEditLifecycleIntegratesAndRetiresExactArtifact(t *testing.T) {
	workspace, service, artifact := completedNativeEdit(t, "ticket-lifecycle", []nativeLifecycleChange{
		{path: "internal/app.go", content: "package internal\n\nconst Value = \"integrated\"\n"},
		{path: "internal/new.go", content: "package internal\n", create: true},
	})
	request := bridge.NativeEditActionRequest{
		TicketID: "ticket-lifecycle", ManifestSHA: artifact.ManifestSHA, Actor: "maintainer",
	}

	inspected, err := service.InspectNativeEdit(context.Background(), workspace, bridge.NativeEditInspectRequest{TicketID: request.TicketID})
	if err != nil || inspected.State != "pending-approval" || inspected.Artifact.ManifestSHA != artifact.ManifestSHA {
		t.Fatalf("unexpected pending lifecycle: %#v err=%v", inspected, err)
	}
	if _, err := service.ApproveNativeEdit(context.Background(), workspace, bridge.NativeEditActionRequest{
		TicketID: request.TicketID, ManifestSHA: "sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Actor: request.Actor,
	}); !errors.Is(err, bridge.ErrDenied) {
		t.Fatalf("mismatched manifest was approved: %v", err)
	}
	approved, err := service.ApproveNativeEdit(context.Background(), workspace, request)
	if err != nil || approved.State != "approved" || approved.Approval == nil ||
		approved.Approval.ManifestSHA != artifact.ManifestSHA || approved.Approval.BaseRevision != artifact.BaseRevision {
		t.Fatalf("artifact was not approved exactly: %#v err=%v", approved, err)
	}
	replayed, err := service.ApproveNativeEdit(context.Background(), workspace, request)
	if err != nil || replayed.State != "approved" || replayed.Approval.ApprovedAt != approved.Approval.ApprovedAt {
		t.Fatalf("approval was not idempotent: %#v err=%v", replayed, err)
	}

	integrated, err := service.IntegrateNativeEdit(context.Background(), workspace, request)
	if err != nil || integrated.State != "integrated" || integrated.Integration == nil || integrated.Integration.IntegratedAt == "" {
		t.Fatalf("artifact was not integrated: %#v err=%v", integrated, err)
	}
	assertNativeLifecycleFile(t, workspace, "internal/app.go", "package internal\n\nconst Value = \"integrated\"\n")
	assertNativeLifecycleFile(t, workspace, "internal/new.go", "package internal\n")
	if _, err := os.Stat(artifact.Worktree); err != nil {
		t.Fatalf("integration unexpectedly removed worktree: %v", err)
	}

	retired, err := service.RetireNativeEdit(context.Background(), workspace, request)
	if err != nil || retired.State != "retired" || retired.Retirement == nil ||
		retired.Retirement.Disposition != "retired" || retired.Retirement.RetiredAt == "" {
		t.Fatalf("integrated artifact was not retired: %#v err=%v", retired, err)
	}
	if _, err := os.Stat(artifact.Worktree); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retired worktree still exists: %v", err)
	}
	replayedRetirement, err := service.RetireNativeEdit(context.Background(), workspace, request)
	if err != nil || replayedRetirement.State != "retired" {
		t.Fatalf("retirement was not idempotent: %#v err=%v", replayedRetirement, err)
	}
	if replayedApproval, err := service.ApproveNativeEdit(context.Background(), workspace, request); err != nil || replayedApproval.State != "retired" {
		t.Fatalf("retired approval could not be replayed: %#v err=%v", replayedApproval, err)
	}
	if replayedIntegration, err := service.IntegrateNativeEdit(context.Background(), workspace, request); err != nil || replayedIntegration.State != "retired" {
		t.Fatalf("retired integration could not be replayed: %#v err=%v", replayedIntegration, err)
	}
	if inspected, err := service.InspectNativeEdit(context.Background(), workspace, bridge.NativeEditInspectRequest{TicketID: request.TicketID}); err != nil || inspected.State != "retired" {
		t.Fatalf("retired lifecycle could not be inspected: %#v err=%v", inspected, err)
	}
}

func TestNativeEditLifecycleDiscardsWithoutTouchingSource(t *testing.T) {
	workspace, service, artifact := completedNativeEdit(t, "ticket-discard", []nativeLifecycleChange{
		{path: "internal/app.go", content: "package internal\n\nconst Value = \"discarded\"\n"},
	})
	request := bridge.NativeEditActionRequest{TicketID: "ticket-discard", ManifestSHA: artifact.ManifestSHA, Actor: "maintainer"}
	discarded, err := service.DiscardNativeEdit(context.Background(), workspace, request)
	if err != nil || discarded.State != "discarded" || discarded.Retirement == nil || discarded.Retirement.Disposition != "discarded" {
		t.Fatalf("artifact was not discarded: %#v err=%v", discarded, err)
	}
	assertNativeLifecycleFile(t, workspace, "internal/app.go", "package internal\n\nconst Value = \"base\"\n")
	if _, err := os.Stat(artifact.Worktree); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("discarded worktree still exists: %v", err)
	}
	if _, err := service.IntegrateNativeEdit(context.Background(), workspace, request); !errors.Is(err, bridge.ErrDenied) {
		t.Fatalf("discarded artifact was integrated: %v", err)
	}
	if replayed, err := service.DiscardNativeEdit(context.Background(), workspace, request); err != nil || replayed.State != "discarded" {
		t.Fatalf("discard was not idempotent: %#v err=%v", replayed, err)
	}
}

func TestNativeEditLifecycleRejectsDivergedSource(t *testing.T) {
	workspace, service, artifact := completedNativeEdit(t, "ticket-diverged", []nativeLifecycleChange{
		{path: "internal/app.go", content: "package internal\n\nconst Value = \"approved\"\n"},
	})
	request := bridge.NativeEditActionRequest{TicketID: "ticket-diverged", ManifestSHA: artifact.ManifestSHA, Actor: "maintainer"}
	if _, err := service.ApproveNativeEdit(context.Background(), workspace, request); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("local divergence\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.IntegrateNativeEdit(context.Background(), workspace, request); !errors.Is(err, bridge.ErrDenied) {
		t.Fatalf("integration accepted a diverged source checkout: %v", err)
	}
	document, err := readNativeTicket(service.storageRoot, request.TicketID)
	if err != nil || document.EditLifecycle == nil || document.EditLifecycle.State != "approved" {
		t.Fatalf("rejected integration mutated lifecycle: %#v err=%v", document.EditLifecycle, err)
	}
}

func TestNativeEditLifecycleResumesPartialApplication(t *testing.T) {
	workspace, service, artifact := completedNativeEdit(t, "ticket-resume", []nativeLifecycleChange{
		{path: "internal/app.go", content: "package internal\n\nconst Value = \"resumed\"\n"},
		{path: "internal/resumed.go", content: "package internal\n", create: true},
	})
	request := bridge.NativeEditActionRequest{TicketID: "ticket-resume", ManifestSHA: artifact.ManifestSHA, Actor: "maintainer"}
	if _, err := service.ApproveNativeEdit(context.Background(), workspace, request); err != nil {
		t.Fatal(err)
	}
	document, err := readNativeTicket(service.storageRoot, request.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	first := artifact.Changes[0]
	desired, err := secureNativeRead(document.Edit.Root, document.Edit.RootIdentity, bridge.NativeReadRequest{
		Path: first.Path, Limit: bridge.MaxNativeEditBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := secureNativeEdit(workspace, document.WorkspaceID, bridge.NativeEditRequest{
		Path: first.Path, Content: desired.Content, ExpectedSHA256: first.PreviousSHA256, Create: first.Created,
	}); err != nil {
		t.Fatal(err)
	}
	document.EditLifecycle.State = "applying"
	document.EditLifecycle.Integration = &bridge.NativeEditIntegration{Actor: request.Actor, StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := writeNativeTicket(service.storageRoot, document); err != nil {
		t.Fatal(err)
	}

	result, err := service.IntegrateNativeEdit(context.Background(), workspace, request)
	if err != nil || result.State != "integrated" {
		t.Fatalf("partial integration did not resume: %#v err=%v", result, err)
	}
	for _, change := range artifact.Changes {
		content, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(change.Path)))
		if err != nil || nativeSHA256(content) != change.SHA256 {
			t.Fatalf("resumed path %s does not match artifact: %v", change.Path, err)
		}
	}
}

func TestNativeEditLifecycleRevalidatesSourceWhileRetiring(t *testing.T) {
	workspace, service, artifact := completedNativeEdit(t, "ticket-retiring", []nativeLifecycleChange{
		{path: "internal/app.go", content: "package internal\n\nconst Value = \"integrated\"\n"},
	})
	request := bridge.NativeEditActionRequest{TicketID: "ticket-retiring", ManifestSHA: artifact.ManifestSHA, Actor: "maintainer"}
	if _, err := service.ApproveNativeEdit(context.Background(), workspace, request); err != nil {
		t.Fatal(err)
	}
	if _, err := service.IntegrateNativeEdit(context.Background(), workspace, request); err != nil {
		t.Fatal(err)
	}
	document, err := readNativeTicket(service.storageRoot, request.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	document.EditLifecycle.State = "retiring"
	document.EditLifecycle.Retirement = &bridge.NativeEditRetirement{
		Disposition: "retired", Actor: request.Actor, StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := writeNativeTicket(service.storageRoot, document); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "internal", "app.go"), []byte("package internal\n\nconst Value = \"drifted\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RetireNativeEdit(context.Background(), workspace, request); !errors.Is(err, bridge.ErrDenied) {
		t.Fatalf("retirement accepted source drift: %v", err)
	}
	if _, err := os.Stat(artifact.Worktree); err != nil {
		t.Fatalf("rejected retirement removed worktree: %v", err)
	}
}

type nativeLifecycleChange struct {
	path    string
	content string
	create  bool
}

func completedNativeEdit(t *testing.T, ticketID string, changes []nativeLifecycleChange) (string, *Service, bridge.NativeEditArtifact) {
	t.Helper()
	workspace := nativeEditRepository(t)
	now := time.Now().UTC()
	service := New(Options{
		StorageRoot: filepath.Join(t.TempDir(), "storage"), Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
	})
	prepared, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: ticketID, Model: "openai/gpt-5.6-sol", Operation: bridge.WriteFiles,
		Goal: "Produce a bounded edit artifact", ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := readNativeTicket(service.storageRoot, ticketID)
	if err != nil || document.Edit == nil {
		t.Fatalf("edit workspace was not persisted: %#v err=%v", document.Edit, err)
	}
	t.Cleanup(func() {
		if _, err := os.Stat(document.Edit.Root); err == nil {
			removeNativeEditWorkspace(workspace, document.Edit)
		}
	})
	for _, change := range changes {
		request := bridge.NativeEditRequest{
			ProtocolVersion: bridge.ProtocolVersion, TicketID: ticketID, ChildSessionID: "ses_child",
			Path: change.path, Content: change.content, Create: change.create,
		}
		if !change.create {
			read, err := service.ReadNative(context.Background(), workspace, bridge.NativeReadRequest{
				ProtocolVersion: bridge.ProtocolVersion, TicketID: ticketID, ChildSessionID: "ses_child", Path: change.path,
			})
			if err != nil || read.Read == nil {
				t.Fatalf("read %s: %#v err=%v", change.path, read, err)
			}
			request.ExpectedSHA256 = nativeSHA256([]byte(read.Read.Content))
		}
		if _, err := service.EditNative(context.Background(), workspace, request); err != nil {
			t.Fatalf("edit %s: %v", change.path, err)
		}
	}
	completed, err := service.Complete(context.Background(), workspace, bridge.NativeCompletionRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: ticketID, ParentSessionID: "ses_parent",
		ChildSessionID: "ses_child", MessageID: "msg_child", Result: nativeResult(t, prepared.TaskID),
	})
	if err != nil || completed.EditArtifact == nil {
		t.Fatalf("complete edit: %#v err=%v", completed, err)
	}
	return workspace, service, *completed.EditArtifact
}

func assertNativeLifecycleFile(t *testing.T, workspace, path, expected string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(path)))
	if err != nil || string(data) != expected {
		t.Fatalf("%s=%q err=%v", path, data, err)
	}
}
