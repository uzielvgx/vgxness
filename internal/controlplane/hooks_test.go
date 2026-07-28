package controlplane

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/hooks"
	"github.com/vgxness/vgxness/internal/providers"
)

func TestCandidateFrozenDispatchesAfterCompletedTicketPersistence(t *testing.T) {
	workspace := nativeEditRepository(t)
	now := time.Now().UTC()
	storage := filepath.Join(t.TempDir(), "storage")
	var calls atomic.Int32
	var identity string
	dispatcher, err := hooks.New(hooks.Options{HandlerTimeout: time.Second},
		func(_ context.Context, event hooks.Event) error {
			frozen, ok := event.(hooks.CandidateFrozen)
			if !ok {
				return nil
			}
			persisted, readErr := readNativeTicket(storage, frozen.TicketID)
			if readErr != nil || persisted.State != "completed" || persisted.Response == nil || persisted.Response.EditArtifact == nil ||
				persisted.Response.EditArtifact.ManifestSHA != "sha256-"+frozen.ManifestDigest {
				t.Fatalf("candidate hook preceded persistence: ticket=%#v err=%v", persisted, readErr)
			}
			identity = frozen.Meta.ID
			calls.Add(1)
			return nil
		},
		func(context.Context, hooks.Event) error { panic("observer failure") },
	)
	if err != nil {
		t.Fatal(err)
	}
	service := New(Options{
		StorageRoot: storage, Now: func() time.Time { return now }, Dispatcher: dispatcher,
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
	})
	prepared, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-hook-candidate", Model: "openai/gpt-5.6-sol", Operation: bridge.WriteFiles,
		Goal: "Update the bounded implementation", ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := readNativeTicket(storage, "ticket-hook-candidate")
	if err != nil || document.Edit == nil {
		t.Fatalf("prepared edit ticket=%#v err=%v", document, err)
	}
	t.Cleanup(func() { removeNativeEditWorkspace(workspace, document.Edit) })
	read, err := service.ReadNative(context.Background(), workspace, bridge.NativeReadRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-hook-candidate", ChildSessionID: "ses_child", Path: "internal/app.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.EditNative(context.Background(), workspace, bridge.NativeEditRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-hook-candidate", ChildSessionID: "ses_child", Path: "internal/app.go",
		Content: "package internal\n\nconst Value = \"hooked\"\n", ExpectedSHA256: read.Read.SHA256,
	}); err != nil {
		t.Fatal(err)
	}
	completion := bridge.NativeCompletionRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-hook-candidate", ParentSessionID: "ses_parent",
		ChildSessionID: "ses_child", MessageID: "msg_child", Result: nativeResult(t, prepared.TaskID),
	}
	first, err := service.Complete(context.Background(), workspace, completion)
	if err != nil || first.EditArtifact == nil {
		t.Fatalf("completion=%#v err=%v", first, err)
	}
	second, err := service.Complete(context.Background(), workspace, completion)
	if err != nil || second.EditArtifact == nil {
		t.Fatalf("replay=%#v err=%v", second, err)
	}
	if calls.Load() != 1 || identity == "" {
		t.Fatalf("candidate calls=%d identity=%q", calls.Load(), identity)
	}
}

func TestValidationCompletedIncludesPersistedFailedReceipt(t *testing.T) {
	workspace := nativeEditRepository(t)
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.invalid/hooks\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "internal", "app_test.go"), []byte("package internal\n\nimport \"testing\"\n\nfunc TestFails(t *testing.T) { if Value != \"never\" { t.Fatal(Value) } }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nativeEditGit(t, workspace, "add", ".")
	nativeEditGit(t, workspace, "commit", "--quiet", "-m", "add failing validation fixture")
	now := time.Now().UTC()
	storage := filepath.Join(t.TempDir(), "storage")
	var observed *hooks.ValidationCompleted
	dispatcher, err := hooks.New(hooks.Options{HandlerTimeout: time.Second}, func(_ context.Context, event hooks.Event) error {
		validation, ok := event.(hooks.ValidationCompleted)
		if !ok {
			return nil
		}
		persisted, readErr := readNativeTicket(storage, validation.TicketID)
		if readErr != nil || len(persisted.Validations) != 1 || nativeCompletionDigest(persisted.Validations[0]) != validation.ReceiptDigest {
			t.Fatalf("validation hook preceded receipt persistence: ticket=%#v err=%v", persisted, readErr)
		}
		observed = &validation
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	service := New(Options{
		StorageRoot: storage, Now: func() time.Time { return now }, Dispatcher: dispatcher,
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
	})
	_, err = service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-hook-validation", Model: "openai/gpt-5.6-sol", Operation: bridge.WriteFiles,
		Goal: "Validate a bounded candidate", ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := readNativeTicket(storage, "ticket-hook-validation")
	if err != nil || document.Edit == nil {
		t.Fatalf("prepared edit ticket=%#v err=%v", document, err)
	}
	t.Cleanup(func() { removeNativeEditWorkspace(workspace, document.Edit) })
	read, err := service.ReadNative(context.Background(), workspace, bridge.NativeReadRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-hook-validation", ChildSessionID: "ses_child", Path: "internal/app.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.EditNative(context.Background(), workspace, bridge.NativeEditRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-hook-validation", ChildSessionID: "ses_child", Path: "internal/app.go",
		Content: "package internal\n\nconst Value = \"changed\"\n", ExpectedSHA256: read.Read.SHA256,
	}); err != nil {
		t.Fatal(err)
	}
	response, err := service.ValidateNative(context.Background(), workspace, bridge.NativeValidationRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-hook-validation", ChildSessionID: "ses_child",
		Operation: bridge.NativeValidationTest, Packages: []string{"./internal"},
	})
	if err != nil || response.Validation == nil || response.Validation.Success {
		t.Fatalf("failed validation receipt=%#v err=%v", response.Validation, err)
	}
	if observed == nil || observed.Success || observed.ExitCode == 0 || observed.PackageCount != 1 {
		t.Fatalf("observed validation=%#v", observed)
	}
}
