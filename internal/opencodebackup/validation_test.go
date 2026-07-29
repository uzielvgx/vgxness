package opencodebackup

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManifestValidationBounds(t *testing.T) {
	root, err := filepath.Abs("source")
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		SchemaVersion: SchemaVersion,
		SnapshotID:    "20260729T120000.000000000Z-0123456789abcdef",
		CreatedAt:     time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC),
		Mode:          ModeFull,
		SourceRoot:    root,
		Entries:       make([]Entry, MaxEntries+1),
	}
	if err := validateManifest(manifest, manifest.SnapshotID); err == nil {
		t.Fatal("entry count overflow was accepted")
	}

	manifest.Entries = make([]Entry, MaxTotalBytes/MaxFileSize+1)
	for index := range manifest.Entries {
		manifest.Entries[index] = Entry{
			Path:   fmt.Sprintf("%03d", index),
			Size:   MaxFileSize,
			Mode:   0o600,
			SHA256: strings.Repeat("0", 64),
		}
	}
	if err := validateManifest(manifest, manifest.SnapshotID); err == nil {
		t.Fatal("total size overflow was accepted")
	}
}

func TestManifestValidationRequiresAllFields(t *testing.T) {
	if err := requireManifestFields([]byte(`{"schemaVersion":"1","entries":[]}`)); err == nil {
		t.Fatal("manifest with missing fields was accepted")
	}
	if err := requireManifestFields([]byte(`{"schemaVersion":"1","snapshotId":"id","createdAt":"2026-07-29T12:00:00Z","mode":"full","sourceRoot":"/root","entries":[{"path":"file","size":0,"mode":384}],"totalBytes":0}`)); err == nil {
		t.Fatal("manifest entry with missing fields was accepted")
	}
}
