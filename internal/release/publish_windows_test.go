//go:build windows

package release

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStagingIdentitySurvivesRename(t *testing.T) {
	parent := t.TempDir()
	staging := filepath.Join(parent, "staging")
	output := filepath.Join(parent, "output")
	if err := os.Mkdir(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	identity, err := captureStagingIdentity(staging)
	if err != nil {
		t.Fatal(err)
	}
	defer identity.close()
	if err := os.Rename(staging, output); err != nil {
		t.Fatal(err)
	}
	matches, err := identity.matches(output)
	if err != nil || !matches {
		t.Fatalf("identity after rename = %v, %v", matches, err)
	}
}
