//go:build darwin || linux

package secrets

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadCredentialFileRejectsUnsafePathsAndNormalizesOneLine(t *testing.T) {
	dir := canonicalTestDir(t)
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

func TestReadCredentialFileRejectsAliasedAncestorPath(t *testing.T) {
	alias := t.TempDir()
	canonical, err := filepath.EvalSymlinks(alias)
	if err != nil {
		t.Fatal(err)
	}
	if alias == canonical {
		t.Skip("temporary directory has no ancestor alias")
	}
	path := filepath.Join(alias, "bearer")
	if err := os.WriteFile(path, []byte("token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCredentialFile(path); !errors.Is(err, ErrInvalid) {
		t.Fatalf("aliased path error=%v", err)
	}
	if got, err := ReadCredentialFile(filepath.Join(canonical, "bearer")); err != nil || got != "token" {
		t.Fatalf("canonical path got=%q err=%v", got, err)
	}
}

func TestOpenCredentialFileBindsValidatedAncestor(t *testing.T) {
	root := canonicalTestDir(t)
	trusted := filepath.Join(root, "trusted")
	path := filepath.Join(trusted, "bearer")
	if err := os.Mkdir(trusted, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("stable\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(root, "replacement")
	if err := os.Mkdir(replacement, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replacement, "bearer"), []byte("redirected\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := openCredentialFile(path, func() {
		if err := os.Rename(trusted, filepath.Join(root, "displaced")); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(replacement, trusted); err != nil {
			t.Fatal(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil || string(data) != "stable\n" {
		t.Fatalf("read=%q err=%v", data, err)
	}
}

func TestReadCredentialFileRejectsLinksObjectsAndMultipleLines(t *testing.T) {
	dir := canonicalTestDir(t)
	file := filepath.Join(dir, "bearer")
	if err := os.WriteFile(file, []byte("token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	ancestor := filepath.Join(canonicalTestDir(t), "ancestor")
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

func canonicalTestDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
