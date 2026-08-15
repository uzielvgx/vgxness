//go:build darwin || linux

package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadCredentialFileRejectsUnsafePathsAndNormalizesOneLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bearer")
	if err := os.WriteFile(path, []byte("token\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadCredentialFile(path)
	if err != nil || got != "token" {
		t.Fatalf("got %q, %v", got, err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCredentialFile(path); !errors.Is(err, ErrInvalid) {
		t.Fatalf("permissive file error=%v", err)
	}
	if _, err := ReadCredentialFile("relative"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("relative error=%v", err)
	}
}

func TestReadCredentialFileRejectsLinksObjectsAndMultipleLines(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "bearer")
	if err := os.WriteFile(file, []byte("token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	ancestor := filepath.Join(t.TempDir(), "ancestor")
	if err := os.Symlink(dir, ancestor); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, filepath.Join(ancestor, "bearer"), dir} {
		if _, err := ReadCredentialFile(path); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unsafe %q error=%v", path, err)
		}
	}
	for _, data := range [][]byte{[]byte("one\ntwo\n"), []byte(""), []byte(strings.Repeat("x", maxCredentialFileBytes+1))} {
		if err := os.WriteFile(file, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadCredentialFile(file); !errors.Is(err, ErrInvalid) {
			t.Fatalf("data=%q error=%v", data, err)
		}
	}
}
