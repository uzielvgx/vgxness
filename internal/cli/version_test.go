package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/buildinfo"
)

func TestCLI_VersionDispatchesWithoutInspector(t *testing.T) {
	inspector := &fakeInspector{}
	var stdout, stderr bytes.Buffer
	code := runBasicCLI(context.Background(), []string{"version"}, &stdout, &stderr, inspector)
	if code != 0 || stdout.String() != buildinfo.Render(buildinfo.Current()) || stderr.Len() != 0 || inspector.calls != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q inspector_calls=%d", code, stdout.String(), stderr.String(), inspector.calls)
	}
}

func TestCLI_VersionRejectsExtraArgumentsWithoutStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runBasicCLI(context.Background(), []string{"version", "extra"}, &stdout, &stderr, &fakeInspector{})
	if code != 2 || stdout.Len() != 0 || stderr.String() != "usage: vgxness version\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCLI_GeneralUsageIncludesVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runBasicCLI(context.Background(), nil, &stdout, &stderr, &fakeInspector{})
	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "version") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
