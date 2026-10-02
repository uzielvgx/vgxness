package app

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uzielvgx/vgxness/internal/testutil"
	"github.com/uzielvgx/vgxness/internal/tui"
)

func TestVersionUsesLightweightPathWithoutWorkingDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(missing, 0o755); err != nil {
		t.Fatal(err)
	}
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(missing); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(original) }()
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"version"}, strings.NewReader(""), &out, &stderr)
	testutil.Require(t, code == 0 && strings.Contains(out.String(), "version=") && stderr.Len() == 0, "code=%d out=%q stderr=%q", code, out.String(), stderr.String())
}

func TestRunDispatchesMCPWithWorkingDirectoryAsWorkspace(t *testing.T) {
	var gotArgs []string
	var gotWorkspace string
	code := run(context.Background(), []string{"mcp", "--full"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, nil, func(_ context.Context, args []string, _ io.Reader, _, _ io.Writer, workspace string) int {
		gotArgs, gotWorkspace = args, workspace
		return 7
	})
	wd, _ := os.Getwd()
	testutil.Require(t, code == 7 && len(gotArgs) == 1 && gotArgs[0] == "--full" && gotWorkspace == wd, "code=%d args=%v workspace=%q", code, gotArgs, gotWorkspace)
	var stderr bytes.Buffer
	code = run(context.Background(), []string{"mcp"}, strings.NewReader(""), &bytes.Buffer{}, &stderr, nil, nil)
	testutil.Require(t, code == 1 && strings.Contains(stderr.String(), "operational:"), "code=%d stderr=%q", code, stderr.String())
}

func TestRunRejectsRetiredCommands(t *testing.T) {
	for _, command := range []string{"setup", "integrate", "self", "skills", "sdd", "agent"} {
		var out, stderr bytes.Buffer
		code := Run(context.Background(), []string{command}, strings.NewReader(""), &out, &stderr)
		testutil.Require(t, code == 2 && out.Len() == 0 && strings.HasPrefix(stderr.String(), "usage: vgxness "), "%s code=%d out=%q stderr=%q", command, code, out.String(), stderr.String())
	}
}

func TestMemoryRuntime_ReadAbsentStorageOperationalAndNonMutating(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"memory", "search", "--stdin", "--storage-root", root}, strings.NewReader(`{"schemaVersion":1,"query":"x","project":"p","scope":"project"}`), &out, &stderr)
	_, statErr := os.Stat(root)
	testutil.Require(t, code == 1 && out.Len() == 0 && os.IsNotExist(statErr), "code=%d out=%q stderr=%q", code, out.String(), stderr.String())
}

func TestMemoryRuntime_SaveCloseAndOfflineRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"memory", "save", "--stdin", "--storage-root", root}, strings.NewReader(`{"schemaVersion":1,"content":"durable fact","project":"p"}`), &out, &stderr)
	testutil.Require(t, code == 0, "save code=%d stderr=%q", code, stderr.String())
	out.Reset()
	code = Run(context.Background(), []string{"memory", "search", "--stdin", "--storage-root", root}, strings.NewReader(`{"schemaVersion":1,"query":"durable","project":"p","scope":"project"}`), &out, &stderr)
	testutil.Require(t, code == 0 && strings.Contains(out.String(), "durable fact"), "search code=%d out=%q stderr=%q", code, out.String(), stderr.String())
}

func TestRunDispatchesConsoleWithWorkingDirectory(t *testing.T) {
	var got tui.Options
	code := run(context.Background(), []string{"tui"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, func(_ context.Context, _ io.Reader, _, _ io.Writer, backend tui.Backend, options tui.Options) int {
		if backend == nil {
			t.Fatal("console launched without a backend")
		}
		got = options
		return 0
	}, nil)
	wd, _ := os.Getwd()
	testutil.Require(t, code == 0 && got.Workspace == wd, "code=%d options=%+v", code, got)
	var stderr bytes.Buffer
	code = run(context.Background(), []string{"tui", "extra"}, strings.NewReader(""), &bytes.Buffer{}, &stderr, nil, nil)
	testutil.Require(t, code == 2 && strings.HasPrefix(stderr.String(), "usage: vgxness tui"), "code=%d stderr=%q", code, stderr.String())
	stderr.Reset()
	code = Run(context.Background(), []string{"tui"}, strings.NewReader(""), &bytes.Buffer{}, &stderr)
	testutil.Require(t, code == 2 && strings.Contains(stderr.String(), "interactive terminals"), "code=%d stderr=%q", code, stderr.String())
}
