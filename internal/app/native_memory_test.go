package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryRuntimeNativeForgetSurvivesRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"memory", "save", "--stdin", "--storage-root", root, "--json"}, strings.NewReader(`{"schemaVersion":1,"content":"forget restart token","project":"p"}`), &out, &stderr)
	if code != 0 {
		t.Fatalf("remember: code=%d stderr=%q", code, stderr.String())
	}
	marker := `"ID":"`
	start := strings.Index(out.String(), marker)
	if start < 0 {
		t.Fatalf("missing id: %q", out.String())
	}
	start += len(marker)
	end := strings.Index(out.String()[start:], `"`)
	id := out.String()[start : start+end]
	out.Reset()
	stderr.Reset()
	payload := `{"schemaVersion":1,"id":"` + id + `","project":"p","scope":"project"}`
	code = Run(context.Background(), []string{"memory", "forget", "--stdin", "--storage-root", root}, strings.NewReader(payload), &out, &stderr)
	if code != 0 {
		t.Fatalf("forget: code=%d stderr=%q", code, stderr.String())
	}
	out.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"memory", "search", "--stdin", "--storage-root", root}, strings.NewReader(`{"schemaVersion":1,"query":"restart","project":"p","scope":"project"}`), &out, &stderr)
	if code != 0 || out.Len() != 0 {
		t.Fatalf("forgotten memory recalled after restart: code=%d out=%q stderr=%q", code, out.String(), stderr.String())
	}
}

func TestMemoryRuntimeResolvesCanonicalWorkspaceForPluginCalls(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"memory", "save", "--stdin", "--workspace", workspace, "--storage-root", root, "--json"}, strings.NewReader(`{"schemaVersion":1,"title":"Decision","content":"use owned memory","type":"decision","topic":"architecture/memory"}`), &out, &stderr)
	if code != 0 || !strings.Contains(out.String(), `"Project":"workspace-`) {
		t.Fatalf("workspace remember: code=%d out=%q stderr=%q", code, out.String(), stderr.String())
	}
	out.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"memory", "search", "--stdin", "--workspace", workspace, "--storage-root", root, "--json"}, strings.NewReader(`{"schemaVersion":1,"query":"owned memory","limit":5}`), &out, &stderr)
	if code != 0 || !strings.Contains(out.String(), `"Title":"Decision"`) {
		t.Fatalf("workspace recall: code=%d out=%q stderr=%q", code, out.String(), stderr.String())
	}
}
