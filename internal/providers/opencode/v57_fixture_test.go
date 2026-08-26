package opencode

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/vgxness/vgxness/internal/sdd"
)

func TestV57E2EFixtureMatchesBinder(t *testing.T) {
	want, err := bindManagerV57(sdd.OpenCodeRoleAssignment{Model: "acme/frontier", Variant: sdd.VariantXHigh})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join("..", "..", "e2e", "testdata", "opencode-manager.v57.acme-frontier-xhigh.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("v57 E2E fixture differs from bindManagerV57 output")
	}
}
