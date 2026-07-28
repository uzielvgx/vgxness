package app

import (
	"bytes"
	"context"
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
