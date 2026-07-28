package controlplane

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/providers"
)

func TestNativeValidationFormatsThroughEditReceiptsAndTestsInDisposableCopy(t *testing.T) {
	workspace := nativeEditRepository(t)
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.invalid/native-validation\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "internal", "obsolete.go"), []byte("package internal\n\nconst obsolete = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nativeEditGit(t, workspace, "add", "go.mod", "internal/obsolete.go")
	nativeEditGit(t, workspace, "commit", "--quiet", "-m", "add module")

	now := time.Now().UTC()
	service := New(Options{
		StorageRoot: filepath.Join(t.TempDir(), "storage"),
		Now:         func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) {
			return nativeTestAdapter(now), nil
		},
	})
	_, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-validation", Model: "openai/gpt-5.6-sol",
		Operation: bridge.WriteFiles, Goal: "Change and validate the bounded implementation",
		ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := readNativeTicket(service.storageRoot, "ticket-validation")
	if err != nil || document.Edit == nil {
		t.Fatalf("missing edit workspace: %#v err=%v", document.Edit, err)
	}
	t.Cleanup(func() { removeNativeEditWorkspace(workspace, document.Edit) })

	read, err := service.ReadNative(context.Background(), workspace, bridge.NativeReadRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-validation",
		ChildSessionID: "ses_child", Path: "internal/app.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.EditNative(context.Background(), workspace, bridge.NativeEditRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-validation", ChildSessionID: "ses_child",
		Path: "internal/app.go", Content: "package internal\n\nconst Value=\"changed\"\n",
		ExpectedSHA256: read.Read.SHA256,
	}); err != nil {
		t.Fatal(err)
	}
	obsolete, err := service.ReadNative(context.Background(), workspace, bridge.NativeReadRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-validation",
		ChildSessionID: "ses_child", Path: "internal/obsolete.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.EditNative(context.Background(), workspace, bridge.NativeEditRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-validation", ChildSessionID: "ses_child",
		Path: "internal/obsolete.go", ExpectedSHA256: obsolete.Read.SHA256, Delete: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.EditNative(context.Background(), workspace, bridge.NativeEditRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-validation", ChildSessionID: "ses_child",
		Path: "internal/app_test.go", Create: true,
		Content: "package internal\nimport(\"os\";\"testing\")\nfunc TestValue(t *testing.T){if Value!=\"changed\"{t.Fatal(Value)};if err:=os.WriteFile(\"validation-marker\",[]byte(\"temporary\"),0600);err!=nil{t.Fatal(err)}}\n",
	}); err != nil {
		t.Fatal(err)
	}

	formatted, err := service.ValidateNative(context.Background(), workspace, bridge.NativeValidationRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-validation",
		ChildSessionID: "ses_child", Operation: bridge.NativeValidationFormat,
	})
	if err != nil || formatted.Validation == nil || !formatted.Validation.Success ||
		len(formatted.Validation.Changes) != 2 {
		t.Fatalf("format failed: %#v err=%v", formatted.Validation, err)
	}
	formattedApp, err := os.ReadFile(filepath.Join(document.Edit.Root, "internal", "app.go"))
	if err != nil || string(formattedApp) != "package internal\n\nconst Value = \"changed\"\n" {
		t.Fatalf("format was not applied through the edit worktree: %q err=%v", formattedApp, err)
	}

	tested, err := service.ValidateNative(context.Background(), workspace, bridge.NativeValidationRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-validation",
		ChildSessionID: "ses_child", Operation: bridge.NativeValidationTest,
		Packages: []string{"./internal"},
	})
	if err != nil || tested.Validation == nil || !tested.Validation.Success ||
		tested.Validation.ExitCode != 0 || !strings.Contains(tested.Validation.Output, "ok") {
		t.Fatalf("test validation failed: %#v err=%v", tested.Validation, err)
	}
	if _, err := os.Stat(filepath.Join(document.Edit.Root, "internal", "validation-marker")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("test side effect escaped disposable validation copy: %v", err)
	}

	vetted, err := service.ValidateNative(context.Background(), workspace, bridge.NativeValidationRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-validation",
		ChildSessionID: "ses_child", Operation: bridge.NativeValidationVet,
		Packages: []string{"./internal"},
	})
	if err != nil || vetted.Validation == nil || !vetted.Validation.Success {
		t.Fatalf("vet validation failed: %#v err=%v", vetted.Validation, err)
	}
	persisted, err := readNativeTicket(service.storageRoot, "ticket-validation")
	if err != nil || len(persisted.Validations) != 3 || len(persisted.Edits) != 3 ||
		persisted.Edits[0].SHA256 != nativeSHA256(formattedApp) {
		t.Fatalf("validation receipts were not persisted: validations=%#v edits=%#v err=%v", persisted.Validations, persisted.Edits, err)
	}
}

func TestNativeValidationSnapshotsPreserveBaseExistenceAcrossDeleteAndRecreate(t *testing.T) {
	snapshots := nativeValidationSnapshots([]bridge.NativeEditResult{
		{Path: "existing.go", PreviousSHA256: "sha256-old", Deleted: true},
		{Path: "existing.go", SHA256: "sha256-new", Created: true},
		{Path: "temporary.go", SHA256: "sha256-temp", Created: true},
		{Path: "temporary.go", PreviousSHA256: "sha256-temp", Deleted: true},
	})
	if len(snapshots) != 1 || snapshots[0].Path != "existing.go" || snapshots[0].Created ||
		snapshots[0].Deleted || snapshots[0].SHA256 != "sha256-new" {
		t.Fatalf("validation snapshots lost base-relative semantics: %#v", snapshots)
	}
}

func TestNativeValidationRejectsNonWriteTicket(t *testing.T) {
	workspace := nativeEditRepository(t)
	now := time.Now().UTC()
	service := New(Options{
		StorageRoot: filepath.Join(t.TempDir(), "storage"),
		Now:         func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) {
			return nativeTestAdapter(now), nil
		},
	})
	_, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-read-validation", Model: "openai/gpt-5.6-sol",
		Operation: bridge.ReadFiles, Goal: "Inspect only", ParentSessionID: "ses_parent",
		ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ValidateNative(context.Background(), workspace, bridge.NativeValidationRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-read-validation",
		ChildSessionID: "ses_child", Operation: bridge.NativeValidationTest,
	}); !errors.Is(err, bridge.ErrDenied) {
		t.Fatalf("read ticket received validation authority: %v", err)
	}
}
