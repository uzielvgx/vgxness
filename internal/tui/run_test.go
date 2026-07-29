package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunRejectsNonInteractiveStreams(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), strings.NewReader(""), &stdout, &stderr, fakeBackend{}, Options{Workspace: "/workspace"})
	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "must be interactive terminals") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
