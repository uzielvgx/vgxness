package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/config"
)

func TestRunMCPDispatchesOnceWithWorkspaceAndConfig(t *testing.T) {
	called := 0
	var gotWorkspace string
	var gotOptions config.Options
	code := runMCP(context.Background(), []string{"--storage-root", "/store", "--project-local"}, strings.NewReader("request"), &bytes.Buffer{}, &bytes.Buffer{}, "/workspace", func(_ context.Context, workspace string, options config.Options, full bool) error {
		called++
		gotWorkspace, gotOptions = workspace, options
		if full {
			t.Fatal("default mode enabled full capability")
		}
		return nil
	})
	if code != 0 || called != 1 || gotWorkspace != "/workspace" || gotOptions.StorageRoot != "/store" || !gotOptions.ProjectLocal || gotOptions.ProjectDir != "/workspace" {
		t.Fatalf("code=%d called=%d workspace=%q options=%+v", code, called, gotWorkspace, gotOptions)
	}
}

func TestRunMCPRejectsArgumentsAndReportsStartupFailureToStderr(t *testing.T) {
	for _, tc := range []struct {
		args []string
		err  error
		code int
	}{
		{args: []string{"extra"}, code: 2},
		{args: []string{"--workspace", "/other"}, code: 2},
		{err: errors.New("unavailable"), code: 1},
	} {
		var stdout, stderr bytes.Buffer
		called := 0
		code := runMCP(context.Background(), tc.args, strings.NewReader(""), &stdout, &stderr, "/workspace", func(context.Context, string, config.Options, bool) error {
			called++
			return tc.err
		})
		if code != tc.code || stdout.Len() != 0 || (tc.code == 2 && called != 0) || (tc.err != nil && !strings.Contains(stderr.String(), "operational:")) {
			t.Fatalf("args=%v code=%d called=%d stdout=%q stderr=%q", tc.args, code, called, stdout.String(), stderr.String())
		}
	}
}

func TestRunMCPFullIsExplicit(t *testing.T) {
	var got bool
	code := runMCP(context.Background(), []string{"--full"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "/workspace", func(_ context.Context, _ string, _ config.Options, full bool) error { got = full; return nil })
	if code != 0 || !got {
		t.Fatalf("code=%d full=%v", code, got)
	}
}
