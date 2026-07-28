package controlplane

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/providers"
)

func TestNativeGitArgsDisableRepositorySideEffects(t *testing.T) {
	got := strings.Join(nativeGitArgs("status", "--porcelain=v1"), "\x00")
	want := strings.Join([]string{
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "core.fsmonitor=false",
		"-c", "core.untrackedCache=false",
		"status", "--porcelain=v1",
	}, "\x00")
	if got != want {
		t.Fatalf("unsafe Git command prefix: %q", got)
	}
}

func TestNativeEditBrokerIsolatesVersionedWritesAndPublishesManifest(t *testing.T) {
	workspace := nativeEditRepository(t)
	now := time.Now().UTC()
	service := New(Options{
		StorageRoot: filepath.Join(t.TempDir(), "storage"), Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
	})
	prepared, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-edit", Model: "openai/gpt-5.6-sol", Operation: bridge.WriteFiles,
		Goal: "Update the bounded implementation", ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Prepared == nil || prepared.Prepared.Agent != "vgxness-implementer" {
		t.Fatalf("write ticket did not select the implementer: %#v", prepared)
	}
	document, err := readNativeTicket(service.storageRoot, "ticket-edit")
	if err != nil || document.Edit == nil {
		t.Fatalf("edit workspace was not persisted: %#v err=%v", document.Edit, err)
	}
	t.Cleanup(func() { removeNativeEditWorkspace(workspace, document.Edit) })
	if document.Edit.Root == workspace || !strings.Contains(document.Edit.Root, filepath.Base(workspace)+"-worktrees") {
		t.Fatalf("edit was not isolated in a sibling worktree: %#v", document.Edit)
	}

	read, err := service.ReadNative(context.Background(), workspace, bridge.NativeReadRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-edit", ChildSessionID: "ses_child", Path: "internal/app.go",
	})
	if err != nil || read.Read == nil || read.Read.Content != "package internal\n\nconst Value = \"base\"\n" ||
		read.Read.SHA256 != nativeSHA256([]byte(read.Read.Content)) {
		t.Fatalf("write ticket could not read its isolated base: %#v err=%v", read, err)
	}
	if !read.Read.Exists {
		t.Fatalf("existing isolated file was reported absent: %#v", read.Read)
	}
	missing, err := service.ReadNative(context.Background(), workspace, bridge.NativeReadRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-edit", ChildSessionID: "ses_child", Path: "internal/new.go",
	})
	if err != nil || missing.Read == nil || missing.Read.Exists || missing.Read.Path != "internal/new.go" || missing.Read.Content != "" {
		t.Fatalf("missing creation target did not return an absent receipt: %#v err=%v", missing, err)
	}
	if missing.Read.SHA256 != "" {
		t.Fatalf("missing creation target returned a content digest: %#v", missing.Read)
	}
	first, err := service.EditNative(context.Background(), workspace, bridge.NativeEditRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-edit", ChildSessionID: "ses_child", Path: "internal/app.go",
		Content: "package internal\n\nconst Value = \"changed\"\n", ExpectedSHA256: read.Read.SHA256,
	})
	if err != nil || first.Edit == nil || first.Edit.Created || first.Edit.PreviousSHA256 == "" {
		t.Fatalf("versioned replacement failed: %#v err=%v", first, err)
	}
	if _, err := service.EditNative(context.Background(), workspace, bridge.NativeEditRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-edit", ChildSessionID: "ses_child", Path: "internal/app.go",
		Content: "package internal\n\nconst Value = \"stale\"\n", ExpectedSHA256: read.Read.SHA256,
	}); !errors.Is(err, bridge.ErrDenied) {
		t.Fatalf("stale expected digest was accepted: %v", err)
	}
	second, err := service.EditNative(context.Background(), workspace, bridge.NativeEditRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-edit", ChildSessionID: "ses_child", Path: "internal/new.go",
		Content: "package internal\n", Create: true,
	})
	if err != nil || second.Edit == nil || !second.Edit.Created {
		t.Fatalf("bounded creation failed: %#v err=%v", second, err)
	}
	readDelete, err := service.ReadNative(context.Background(), workspace, bridge.NativeReadRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-edit", ChildSessionID: "ses_child", Path: "README.md",
	})
	if err != nil || readDelete.Read == nil {
		t.Fatalf("deletion target could not be read: %#v err=%v", readDelete, err)
	}
	deleted, err := service.EditNative(context.Background(), workspace, bridge.NativeEditRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-edit", ChildSessionID: "ses_child", Path: "README.md",
		ExpectedSHA256: readDelete.Read.SHA256, Delete: true,
	})
	if err != nil || deleted.Edit == nil || !deleted.Edit.Deleted || deleted.Edit.SHA256 != "" ||
		deleted.Edit.PreviousSHA256 != readDelete.Read.SHA256 {
		t.Fatalf("content-bound deletion failed: %#v err=%v", deleted, err)
	}
	if _, err := service.EditNative(context.Background(), workspace, bridge.NativeEditRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-edit", ChildSessionID: "ses_child", Path: "internal/app.go",
		ExpectedSHA256: read.Read.SHA256, Delete: true,
	}); !errors.Is(err, bridge.ErrDenied) {
		t.Fatalf("stale content-bound deletion was accepted: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(workspace, "internal", "app.go")); err != nil || string(data) != "package internal\n\nconst Value = \"base\"\n" {
		t.Fatalf("source checkout was mutated: %q err=%v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(workspace, "README.md")); err != nil || string(data) != "base\n" {
		t.Fatalf("source deletion escaped the isolated worktree: %q err=%v", data, err)
	}

	completed, err := service.Complete(context.Background(), workspace, bridge.NativeCompletionRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-edit", ParentSessionID: "ses_parent",
		ChildSessionID: "ses_child", MessageID: "msg_child", Result: nativeResult(t, prepared.TaskID),
	})
	if err != nil || completed.Status != "completed" || completed.EditArtifact == nil || len(completed.EditArtifact.Changes) != 3 ||
		completed.EditArtifact.Worktree != document.Edit.Root || completed.EditArtifact.BaseRevision != document.Edit.BaseRevision ||
		!strings.HasPrefix(completed.EditArtifact.ManifestSHA, "sha256-") {
		t.Fatalf("completion did not publish bounded edit evidence: %#v err=%v", completed, err)
	}
	if change := completed.EditArtifact.Changes[0]; change.Path != "README.md" || !change.Deleted || change.SHA256 != "" || change.Bytes != 0 {
		t.Fatalf("deletion manifest entry is invalid: %#v", change)
	}
}

func TestNativeEditAcceptsNonSuccessResultWithoutChanges(t *testing.T) {
	workspace := nativeEditRepository(t)
	now := time.Now().UTC()
	service := New(Options{
		StorageRoot: filepath.Join(t.TempDir(), "storage"), Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
	})
	prepared, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-edit-blocked", Model: "openai/gpt-5.6-sol", Operation: bridge.WriteFiles,
		Goal: "Report a bounded write blocker", ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := readNativeTicket(service.storageRoot, "ticket-edit-blocked")
	if err != nil || document.Edit == nil {
		t.Fatalf("edit workspace was not persisted: %#v err=%v", document.Edit, err)
	}
	t.Cleanup(func() { removeNativeEditWorkspace(workspace, document.Edit) })

	completed, err := service.Complete(context.Background(), workspace, bridge.NativeCompletionRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-edit-blocked", ParentSessionID: "ses_parent",
		ChildSessionID: "ses_child", MessageID: "msg_child", Result: nativeResultWithStatus(t, prepared.TaskID, "needs_followup"),
	})
	if err != nil || completed.Status != "completed" || completed.EditArtifact != nil || completed.Receipt == nil {
		t.Fatalf("accepted no-change result was not preserved: %#v err=%v", completed, err)
	}
}

func TestNativeEditPrepareRequiresCleanRepositoryAndCompletionRejectsUnbrokeredChanges(t *testing.T) {
	t.Run("dirty source", func(t *testing.T) {
		workspace := nativeEditRepository(t)
		if err := os.WriteFile(filepath.Join(workspace, "untracked.txt"), []byte("dirty"), 0o600); err != nil {
			t.Fatal(err)
		}
		service := New(Options{StorageRoot: filepath.Join(t.TempDir(), "storage")})
		_, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
			ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-dirty", Model: "openai/gpt-5.6-sol", Operation: bridge.WriteFiles,
			Goal: "Edit safely", ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
		})
		if !errors.Is(err, bridge.ErrDenied) {
			t.Fatalf("dirty source repository was accepted: %v", err)
		}
		if !errors.Is(err, errNativeSourceWorktreeDirty) {
			t.Fatalf("dirty source denial lost its bounded discriminator: %v", err)
		}
		var preparation *nativePreparationError
		if !errors.As(err, &preparation) || preparation.stage != nativePreparationStageEditWorkspace {
			t.Fatalf("dirty source denial lost its preparation stage: %#v", preparation)
		}
	})

	t.Run("unbrokered worktree mutation", func(t *testing.T) {
		workspace := nativeEditRepository(t)
		now := time.Now().UTC()
		service := New(Options{
			StorageRoot: filepath.Join(t.TempDir(), "storage"), Now: func() time.Time { return now },
			AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
		})
		prepared, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
			ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-unbrokered", Model: "openai/gpt-5.6-sol", Operation: bridge.WriteFiles,
			Goal: "Edit safely", ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
		})
		if err != nil {
			t.Fatal(err)
		}
		document, err := readNativeTicket(service.storageRoot, "ticket-unbrokered")
		if err != nil || document.Edit == nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { removeNativeEditWorkspace(workspace, document.Edit) })
		if err := os.WriteFile(filepath.Join(document.Edit.Root, "README.md"), []byte("bypassed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err = service.Complete(context.Background(), workspace, bridge.NativeCompletionRequest{
			ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-unbrokered", ParentSessionID: "ses_parent",
			ChildSessionID: "ses_child", MessageID: "msg_child", Result: nativeResult(t, prepared.TaskID),
		})
		if !errors.Is(err, bridge.ErrDenied) {
			t.Fatalf("unbrokered mutation was accepted: %v", err)
		}
		if _, err := service.Fail(context.Background(), workspace, bridge.NativeFailureRequest{
			ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-unbrokered", ParentSessionID: "ses_parent",
			ChildSessionID: "ses_child", Category: "native-subagent-failed",
		}); err != nil {
			t.Fatalf("cleanup failed edit ticket: %v", err)
		}
	})

	t.Run("ignored broker creation", func(t *testing.T) {
		workspace := nativeEditRepository(t)
		now := time.Now().UTC()
		service := New(Options{
			StorageRoot: filepath.Join(t.TempDir(), "storage"), Now: func() time.Time { return now },
			AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
		})
		prepared, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
			ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-ignored", Model: "openai/gpt-5.6-sol", Operation: bridge.WriteFiles,
			Goal: "Edit safely", ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
		})
		if err != nil {
			t.Fatal(err)
		}
		document, err := readNativeTicket(service.storageRoot, "ticket-ignored")
		if err != nil || document.Edit == nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { removeNativeEditWorkspace(workspace, document.Edit) })
		if _, err := service.EditNative(context.Background(), workspace, bridge.NativeEditRequest{
			ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-ignored", ChildSessionID: "ses_child",
			Path: "ignored.txt", Content: "hidden\n", Create: true,
		}); err != nil {
			t.Fatal(err)
		}
		_, err = service.Complete(context.Background(), workspace, bridge.NativeCompletionRequest{
			ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-ignored", ParentSessionID: "ses_parent",
			ChildSessionID: "ses_child", MessageID: "msg_child", Result: nativeResult(t, prepared.TaskID),
		})
		if !errors.Is(err, bridge.ErrDenied) {
			t.Fatalf("ignored edit was accepted without manifest evidence: %v", err)
		}
		if _, err := service.Fail(context.Background(), workspace, bridge.NativeFailureRequest{
			ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-ignored", ParentSessionID: "ses_parent",
			ChildSessionID: "ses_child", Category: "native-subagent-failed",
		}); err != nil {
			t.Fatalf("cleanup ignored edit ticket: %v", err)
		}
	})
}

func TestSecureNativeEditRejectsAliasesAndMissingParents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the native edit broker requires Unix link-count evidence")
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "dir", "target.txt")
	if err := os.WriteFile(target, []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.txt", filepath.Join(root, "dir", "alias.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(target, filepath.Join(root, "dir", "hardlink.txt")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(root)
	identity, ok := nativeFileIdentity(info)
	if err != nil || !ok {
		t.Fatal(err)
	}
	for _, request := range []bridge.NativeEditRequest{
		{Path: "dir/alias.txt", Content: "changed", ExpectedSHA256: nativeSHA256([]byte("base"))},
		{Path: "dir/hardlink.txt", Content: "changed", ExpectedSHA256: nativeSHA256([]byte("base"))},
		{Path: "missing/new.txt", Content: "changed", Create: true},
	} {
		if _, err := secureNativeEdit(root, identity, request); !errors.Is(err, bridge.ErrDenied) {
			t.Fatalf("unsafe edit %#v was accepted: %v", request, err)
		}
	}
}

func nativeEditRepository(t *testing.T) string {
	t.Helper()
	workspace := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	nativeEditGit(t, workspace, "init", "--quiet")
	nativeEditGit(t, workspace, "config", "user.name", "VGXNESS Test")
	nativeEditGit(t, workspace, "config", "user.email", "vgxness@example.invalid")
	if err := os.Mkdir(filepath.Join(workspace, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "internal", "app.go"), []byte("package internal\n\nconst Value = \"base\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nativeEditGit(t, workspace, "add", ".")
	nativeEditGit(t, workspace, "commit", "--quiet", "-m", "base")
	return workspace
}

func nativeEditGit(t *testing.T, workspace string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = workspace
	command.Env = cleanGitEnvironment(nil)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
