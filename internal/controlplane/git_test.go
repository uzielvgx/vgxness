package controlplane

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCollectGitEvidenceUsesOnlyFixedReadOnlyCommands(t *testing.T) {
	var calls [][]string
	run := func(_ context.Context, workspace string, args []string) ([]byte, error) {
		if workspace != "/workspace" {
			t.Fatalf("workspace=%q", workspace)
		}
		calls = append(calls, append([]string(nil), args...))
		if containsArg(args, "--name-status") {
			return []byte{}, nil
		}
		return []byte("evidence\n"), nil
	}
	evidence, err := collectGitEvidenceWith(context.Background(), "/workspace", run)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 5 || !containsArg(calls[0], "--name-status") || !containsArg(calls[1], "--name-status") || !containsArg(calls[1], "--cached") || !containsArg(calls[2], "status") || !containsArg(calls[3], "diff") || !containsArg(calls[4], "diff") || !containsArg(calls[4], "--cached") {
		t.Fatalf("unexpected commands: %#v", calls)
	}
	for index, call := range calls {
		if containsArg(call, "commit") || containsArg(call, "push") || !containsArg(call, "core.attributesFile="+os.DevNull) {
			t.Fatalf("unsafe Git command: %#v", call)
		}
		joined := strings.Join(call, " ")
		requiredValues := []string{}
		if index >= 2 {
			requiredValues = append(requiredValues, ":(icase,exclude)**/.env", ":(icase,exclude)**/*.key", ":(icase,exclude)**/secrets/**", ":(icase,exclude)**/.npmrc", ":(icase,exclude)**/id_ed25519")
		}
		if index < 5 {
			requiredValues = append(requiredValues, "--ignore-submodules=all")
		}
		for _, required := range requiredValues {
			if !strings.Contains(joined, required) {
				t.Fatalf("command does not exclude %q: %#v", required, call)
			}
		}
	}
	if evidence.StatusShort != "evidence\n" || evidence.WorktreeDiff != "evidence\n" || evidence.StagedDiff != "evidence\n" {
		t.Fatalf("unexpected evidence: %#v", evidence)
	}
}

func TestCollectGitEvidenceStopsOnFailureAndBoundsCombinedOutput(t *testing.T) {
	calls := 0
	_, err := collectGitEvidenceWith(context.Background(), "/workspace", func(context.Context, string, []string) ([]byte, error) {
		calls++
		return nil, errors.New("failed")
	})
	if err == nil || calls != 1 {
		t.Fatalf("failure was not fail-closed: calls=%d err=%v", calls, err)
	}
	large := bytes.Repeat([]byte("x"), maxGitEvidenceBytes/3+1)
	_, err = collectGitEvidenceWith(context.Background(), "/workspace", func(_ context.Context, _ string, args []string) ([]byte, error) {
		if containsArg(args, "--name-status") {
			return []byte{}, nil
		}
		return large, nil
	})
	if err == nil {
		t.Fatal("oversized combined evidence was accepted")
	}
	escaped := bytes.Repeat([]byte("<"), maxGitEvidenceBytes/6)
	_, err = collectGitEvidenceWith(context.Background(), "/workspace", func(_ context.Context, _ string, args []string) ([]byte, error) {
		if containsArg(args, "--name-status") {
			return []byte{}, nil
		}
		return escaped, nil
	})
	if err == nil {
		t.Fatal("evidence whose JSON encoding exceeds the prompt budget was accepted")
	}
}

func TestCappedBufferReportsOverflowWithoutGrowingPastLimit(t *testing.T) {
	buffer := cappedBuffer{limit: 4}
	if written, err := buffer.Write([]byte("abcdef")); err != nil || written != 6 || buffer.String() != "abcd" || !buffer.overflow {
		t.Fatalf("unexpected capped write: written=%d value=%q overflow=%v err=%v", written, buffer.String(), buffer.overflow, err)
	}
}

func TestCollectGitEvidenceDoesNotExecuteRepositoryFilters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("filter probe uses a POSIX helper")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is unavailable")
	}
	root := t.TempDir()
	marker := filepath.Join(root, "filter-ran")
	filter := filepath.Join(root, "filter.sh")
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("sample.txt filter=probe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sample.txt"), []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filter, []byte("#!/bin/sh\ntouch "+marker+"\ncat\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "init", "-q")
	runGitTest(t, root, "add", ".gitattributes", "sample.txt")
	runGitTest(t, root, "-c", "user.name=Probe", "-c", "user.email=probe@example.invalid", "commit", "-qm", "initial")
	runGitTest(t, root, "config", "filter.probe.clean", filter)
	if err := os.WriteFile(filepath.Join(root, ".git", "info", "attributes"), []byte("sample.txt filter=probe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sample.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	evidence, err := collectGitEvidence(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(evidence.WorktreeDiff, "+changed") {
		t.Fatalf("missing raw worktree change: %s", evidence.WorktreeDiff)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("repository clean filter executed: %v", err)
	}
}

func TestCollectGitEvidenceExcludesExpandedSensitivePathsCaseInsensitively(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is unavailable")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"safe.go": "package safe\n", ".NPMRC": "token=tracked-secret\n",
		"nested/ID_ED25519": "tracked-private-key\n", "nested/CREDENTIALS.JSON": "tracked-credential\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runGitTest(t, root, "init", "-q")
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "-c", "user.name=Probe", "-c", "user.email=probe@example.invalid", "commit", "-qm", "initial")
	for name, content := range map[string]string{
		"safe.go": "package safe // changed\n", ".NPMRC": "token=changed-secret\n",
		"nested/ID_ED25519": "changed-private-key\n", "nested/CREDENTIALS.JSON": "changed-credential\n",
		"nested/.PyPiRc": "untracked-secret\n", "nested/visible.txt": "UNTRACKED_SENTINEL_DO_NOT_READ\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	evidence, err := collectGitEvidence(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	combined := evidence.StatusShort + evidence.WorktreeDiff + evidence.StagedDiff
	for _, forbidden := range []string{".NPMRC", "ID_ED25519", "CREDENTIALS.JSON", ".PyPiRc", "changed-secret", "changed-private-key", "changed-credential", "untracked-secret", "UNTRACKED_SENTINEL_DO_NOT_READ"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("sensitive evidence leaked %q:\n%s", forbidden, combined)
		}
	}
	for _, required := range []string{"safe.go", "+package safe // changed", "nested/visible.txt"} {
		if !strings.Contains(combined, required) {
			t.Fatalf("safe evidence omitted %q:\n%s", required, combined)
		}
	}
}

func TestCollectGitEvidenceRejectsSensitiveRenameOrCopy(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is unavailable")
	}
	for _, operation := range []string{"rename", "copy"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, ".env"), []byte("SENTINEL_DO_NOT_LEAK\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			runGitTest(t, root, "init", "-q")
			runGitTest(t, root, "add", ".env")
			runGitTest(t, root, "-c", "user.name=Probe", "-c", "user.email=probe@example.invalid", "commit", "-qm", "initial")
			if operation == "rename" {
				runGitTest(t, root, "mv", ".env", "safe.txt")
			} else {
				if err := os.WriteFile(filepath.Join(root, "safe.txt"), []byte("SENTINEL_DO_NOT_LEAK\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				runGitTest(t, root, "add", "safe.txt")
			}
			if _, err := collectGitEvidence(context.Background(), root); err == nil || !strings.Contains(err.Error(), "sensitive-path boundary") {
				t.Fatalf("sensitive %s was not rejected: %v", operation, err)
			}
		})
	}
}

func TestRejectSensitiveRelationsFailsClosedOnMalformedOutput(t *testing.T) {
	for _, value := range [][]byte{[]byte("R100\x00.env\x00"), []byte("R100\x00.env\x00safe.txt"), []byte("\x00")} {
		if err := rejectSensitiveRelations(value); err == nil {
			t.Fatalf("malformed relation accepted: %q", value)
		}
	}
}

func runGitTest(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func containsArg(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
