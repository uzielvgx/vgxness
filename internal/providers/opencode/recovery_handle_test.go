package opencode

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteSharingWriterSurvivesQuarantine(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	quarantine := filepath.Join(directory, "target.quarantine")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	writer, err := openDeleteSharingWriter(target)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	if err := os.Rename(target, quarantine); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteAt([]byte("mutated!"), 0); err != nil {
		t.Fatal(err)
	}
	if err := writer.Sync(); err != nil {
		t.Fatal(err)
	}

	contents, err := os.ReadFile(quarantine)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "mutated!" {
		t.Fatalf("quarantined contents = %q, want %q", contents, "mutated!")
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("original path error = %v, want not exist", err)
	}
}
