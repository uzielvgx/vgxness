//go:build windows

package opencode

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSyncDirectoryIsExplicitBestEffortBoundary(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "marker"), []byte("staged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := directoryDurability(); got != "file-sync-namespace-best-effort" {
		t.Fatalf("directoryDurability() = %q", got)
	}
	if err := syncDirectory(directory); err != nil {
		t.Fatalf("syncDirectory() error = %v", err)
	}
}

func TestLinkAnchorWhilePredecessorHandleRemainsOpen(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "predecessor")
	anchor := filepath.Join(directory, "anchor")
	content := []byte("managed predecessor")
	if err := os.WriteFile(target, content, 0o600); err != nil {
		t.Fatal(err)
	}
	handle, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	if err := os.Link(target, anchor); err != nil {
		t.Fatal(err)
	}
	targetInfo, targetErr := os.Stat(target)
	anchorInfo, anchorErr := os.Stat(anchor)
	anchored, readErr := os.ReadFile(anchor)
	if targetErr != nil || anchorErr != nil || readErr != nil || !os.SameFile(targetInfo, anchorInfo) || !bytes.Equal(anchored, content) {
		t.Fatalf("targetErr=%v anchorErr=%v readErr=%v anchor=%q same=%t", targetErr, anchorErr, readErr, anchored, os.SameFile(targetInfo, anchorInfo))
	}
}
