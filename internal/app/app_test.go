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
