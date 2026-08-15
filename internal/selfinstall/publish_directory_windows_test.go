//go:build windows

package selfinstall

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublishRootDirectoryNestedDestinationDoesNotReplaceExistingDirectory(t *testing.T) {
	rootPath := t.TempDir()
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.Mkdir("source", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := root.Mkdir("versions", 0o700); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join("versions", "destination")
	if err := publishRootDirectoryNoReplace(root, "source", destination); err != nil {
		t.Fatalf("publish nested destination: %v", err)
	}
	if _, err := root.Lstat(destination); err != nil {
		t.Fatalf("published destination missing: %v", err)
	}
	if _, err := root.Lstat("source"); !os.IsNotExist(err) {
		t.Fatalf("source remains after publish: %v", err)
	}
	marker := filepath.Join(destination, "original-marker")
	if err := root.WriteFile(marker, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := root.Mkdir("replacement-source", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := publishRootDirectoryNoReplace(root, "replacement-source", destination); err == nil {
		t.Fatal("publish replaced an existing nested directory")
	}
	for _, name := range []string{"replacement-source", destination} {
		if info, err := root.Lstat(name); err != nil || !info.IsDir() {
			t.Fatalf("%s missing after failed publish: info=%v err=%v", name, info, err)
		}
	}
	if contents, err := root.ReadFile(marker); err != nil || string(contents) != "original" {
		t.Fatalf("destination marker after failed publish: %q err=%v", contents, err)
	}
}
