//go:build darwin || linux

package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
)

type ancestorInfo struct {
	os.FileInfo
	mode os.FileMode
	uid  uint32
}

func (i ancestorInfo) Mode() os.FileMode { return i.mode }
func (i ancestorInfo) Sys() any          { return &syscall.Stat_t{Uid: i.uid} }
func (ancestorInfo) IsDir() bool         { return true }
func TestRootCases(t *testing.T) {
	foreign := ancestorInfo{mode: 0o755, uid: 1}
	if safeAncestor(foreign) || safeAncestor(ancestorInfo{mode: 0o777 | os.ModeSticky, uid: 1}) || !safeAncestor(ancestorInfo{mode: 0o755, uid: 0}) {
		t.Fatal("ancestor ownership")
	}
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T) (string, error)
		want  error
	}{
		{"canonical", func(t *testing.T) (string, error) {
			d := t.TempDir()
			l := filepath.Join(d, "link")
			return filepath.Join(l, "codex"), os.Symlink(d, l)
		}, nil},
		{"0755", func(t *testing.T) (string, error) {
			d := filepath.Join(t.TempDir(), "codex")
			return d, os.MkdirAll(d, 0o755)
		}, nil},
		{"writable", func(t *testing.T) (string, error) {
			d := filepath.Join(t.TempDir(), "codex")
			if err := os.MkdirAll(d, 0o700); err != nil {
				return d, err
			}
			return d, os.Chmod(d, 0o775)
		}, integration.ErrInvalid},
		{"symlink", func(t *testing.T) (string, error) {
			d := t.TempDir()
			return filepath.Join(d, "codex"), os.Symlink(t.TempDir(), filepath.Join(d, "codex"))
		}, integration.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, err := tc.setup(t)
			if err != nil {
				t.Fatal(err)
			}
			r, err := OpenRoot(context.Background(), integration.Options{ConfigDir: path}, true)
			if !errors.Is(err, tc.want) {
				t.Fatalf("OpenRoot() error = %v", err)
			}
			if r != nil {
				defer r.Close()
				if filepath.Base(r.Path) != "codex" {
					t.Fatal(r.Path)
				}
				if tc.name == "canonical" && filepath.Clean(r.Path) == filepath.Clean(path) {
					t.Fatal("root was not canonical")
				}
			}
		})
	}
}
func TestRootPrimitives(t *testing.T) {
	r, err := OpenRoot(context.Background(), integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.Mkdir("agents"); err != nil {
		t.Fatal(err)
	}
	if err := r.fs.Symlink("../../", "agents/escape"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Read("agents/escape/x", 8); err == nil {
		t.Fatal("symlink escaped root")
	}
	if _, _, err := r.Read("missing", 8); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if _, err := r.Publish(context.Background(), "agents/a", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Publish(context.Background(), "agents/a", []byte("two")); !errors.Is(err, integration.ErrConflict) {
		t.Fatal(err)
	}
	if err := testWrite(r, "agents/b", make([]byte, 9)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Read("agents/b", 8); !errors.Is(err, integration.ErrDrift) {
		t.Fatal(err)
	}
	if err := testWrite(r, ".vgxness-custom", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := testWrite(r, ".vgxness-custom", []byte("x")); !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}
	pending, err := r.Pending()
	if err != nil || pending {
		t.Fatalf("pending = %v, %v", pending, err)
	}
	if err := r.MarkPending(); err != nil {
		t.Fatal(err)
	}
	pending, err = r.Pending()
	if err != nil || !pending {
		t.Fatal(err)
	}
	if err := r.ClearPending(); err != nil {
		t.Fatal(err)
	}
	if err := r.MarkPending(); err != nil {
		t.Fatal(err)
	}
	n := 0
	r.syncHook = func(string) error {
		n++
		if n == 1 {
			return errors.New("sync")
		}
		return nil
	}
	if err := r.ClearPending(); !errors.Is(err, integration.ErrRecovery) {
		t.Fatal(err)
	}
	if pending, _ := r.Pending(); !pending {
		t.Fatal("pending not recreated")
	}
}
func TestRootOwnershipAndCancellation(t *testing.T) {
	r, err := OpenRoot(context.Background(), integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	owned, err := r.Publish(context.Background(), "owned", []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveAnchor(owned); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Read("owned", 8); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	a, err := r.Publish(context.Background(), "a", []byte("same"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r.Path, "a")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.Path, "a"), []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Quarantine(a); !errors.Is(err, integration.ErrConflict) {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(r.Path, "a")); err != nil || string(data) != "same" {
		t.Fatalf("foreign replacement = %q, %v", data, err)
	}
	if err := r.RemoveAnchor(a); !errors.Is(err, integration.ErrRecovery) {
		t.Fatal(err)
	}
	b, err := r.Backup("a", []byte("same"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r.Path, b.Sidecar)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.Path, b.Sidecar), []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.Restore(b); !errors.Is(err, integration.ErrRecovery) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Publish(ctx, "c", []byte("x")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestRootAncestorAndQuarantineIdentity(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(parent, "missing")
	if _, err := OpenRoot(context.Background(), integration.Options{ConfigDir: filepath.Join(missing, "codex")}, true); !errors.Is(err, integration.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := os.Lstat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if r, err := OpenRoot(context.Background(), integration.Options{ConfigDir: filepath.Join(parent, "codex")}, true); err != nil {
		t.Fatal(err)
	} else if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o775); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenRoot(context.Background(), integration.Options{ConfigDir: filepath.Join(parent, "codex", "unsafe")}, true); !errors.Is(err, integration.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(parent, "codex", "unsafe")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	r, err := OpenRoot(context.Background(), integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	a, err := r.Publish(context.Background(), "q", []byte("q"))
	if err != nil {
		t.Fatal(err)
	}
	r.afterRename = func(name string) error {
		if err := r.fs.Remove(name); err != nil {
			return err
		}
		return testWrite(r, name, []byte("q"))
	}
	if _, err := r.Quarantine(a); !errors.Is(err, integration.ErrRecovery) {
		t.Fatal(err)
	}
}
func TestRootOwnershipAndPostLinkAnchor(t *testing.T) {
	r, err := OpenRoot(context.Background(), integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	info, err := r.fs.Lstat(".")
	if err != nil || !owned(info) {
		t.Fatalf("owner = %v, %v", info, err)
	}
	r.syncHook = func(string) error { return errors.New("sync") }
	a, err := r.Publish(context.Background(), "a", []byte("a"))
	if !errors.Is(err, integration.ErrRecovery) || a.Name == "" {
		t.Fatalf("publish = %#v, %v", a, err)
	}
	r.syncHook = nil
	if err := r.RemoveAnchor(a); err != nil {
		t.Fatal(err)
	}
	a, err = r.Publish(context.Background(), "b", []byte("b"))
	if err != nil {
		t.Fatal(err)
	}
	r.stat = func(string) (os.FileInfo, error) { return nil, errors.New("stat") }
	if _, err := r.Quarantine(a); err == nil {
		t.Fatal("sidecar stat accepted")
	}
}
