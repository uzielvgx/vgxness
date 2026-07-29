package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/testutil"
)

func TestMemoryRuntime_ReadAbsentStorageOperationalAndNonMutating(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"memory", "search", "--stdin", "--storage-root", root}, strings.NewReader(`{"schemaVersion":1,"query":"token","project":"p","scope":"project"}`), &out, &stderr)
	_, err := os.Stat(root)
	testutil.Require(t, code == 1 && out.Len() == 0 && os.IsNotExist(err), "exit=%d out=%q stat=%v", code, out.String(), err)
}

func TestMemoryRuntime_SaveCloseAndOfflineRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"memory", "save", "--stdin", "--storage-root", root}, strings.NewReader(`{"schemaVersion":1,"title":"T","content":"restart token","project":"p"}`), &out, &stderr)
	testutil.Require(t, code == 0, "save exit=%d stderr=%q", code, stderr.String())
	out.Reset()
	code = Run(context.Background(), []string{"memory", "search", "--stdin", "--storage-root", root}, strings.NewReader(`{"schemaVersion":1,"query":"restart","project":"p","scope":"project"}`), &out, &stderr)
	testutil.Require(t, code == 0 && strings.Contains(out.String(), "T"), "restart exit=%d out=%q stderr=%q", code, out.String(), stderr.String())
}

func TestOpenCodeIntegrationRuntime_InstallStatusAndRecoverableUninstall(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"integrate", "opencode", "install", "--model", "openai/gpt-5.6-sol", "--config-dir", configDirectory}, strings.NewReader(""), &out, &stderr)
	testutil.Require(t, code == 0 && strings.Contains(out.String(), "state=installed") && stderr.Len() == 0, "install exit=%d out=%q stderr=%q", code, out.String(), stderr.String())
	out.Reset()
	code = Run(context.Background(), []string{"integrate", "opencode", "status", "--config-dir", configDirectory}, strings.NewReader(""), &out, &stderr)
	testutil.Require(t, code == 0 && strings.Contains(out.String(), "state=installed") && strings.Contains(out.String(), "changed=false"), "status exit=%d out=%q stderr=%q", code, out.String(), stderr.String())
	out.Reset()
	code = Run(context.Background(), []string{"integrate", "opencode", "uninstall", "--config-dir", configDirectory}, strings.NewReader(""), &out, &stderr)
	testutil.Require(t, code == 0 && strings.Contains(out.String(), "state=absent") && strings.Contains(out.String(), "backup="), "uninstall exit=%d out=%q stderr=%q", code, out.String(), stderr.String())
}

func TestVersionUsesLightweightPathWithoutWorkingDirectory(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	if err := os.Remove(directory); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"version"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 || !strings.HasPrefix(stdout.String(), "version=dev\ncommit=unknown\ndate=unknown\n") || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
