//go:build darwin || linux

package selfinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type ownershipTestInfo struct {
	mode os.FileMode
	dir  bool
	sys  any
}

func (i ownershipTestInfo) Name() string       { return "fixture" }
func (i ownershipTestInfo) Size() int64        { return 0 }
func (i ownershipTestInfo) Mode() os.FileMode  { return i.mode }
func (i ownershipTestInfo) ModTime() time.Time { return time.Time{} }
func (i ownershipTestInfo) IsDir() bool        { return i.dir }
func (i ownershipTestInfo) Sys() any           { return i.sys }

func TestInstallRejectsUnsafeExistingDirectories(t *testing.T) {
	root := t.TempDir()
	source := writeSource(t, root, "source", "vgxness")
	for _, test := range []struct {
		name    string
		prepare func(bin, data string) error
	}{
		{"ancestor", func(bin, data string) error { return os.Chmod(filepath.Dir(bin), 0o777) }},
		{"bin", func(bin, data string) error { return os.Chmod(bin, 0o777) }},
		{"data", func(bin, data string) error { return os.Chmod(data, 0o777) }},
		{"versions", func(bin, data string) error { return os.Chmod(filepath.Join(data, "versions"), 0o777) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			bin, data := filepath.Join(root, test.name, "parent", "bin"), filepath.Join(root, test.name, "parent", "data")
			if err := os.MkdirAll(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(data, "versions"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := test.prepare(bin, data); err != nil {
				t.Fatal(err)
			}
			if _, err := New(Config{SourceExecutable: source}).Install(context.Background(), Options{BinDir: bin, DataDir: data}); !errors.Is(err, ErrDrift) {
				t.Fatalf("Install error=%v, want ErrDrift", err)
			}
			if _, err := os.Stat(filepath.Join(bin, executableName())); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unsafe target mutated: %v", err)
			}
			if _, err := os.Stat(filepath.Join(data, ".install.lock")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unsafe target mutated: %v", err)
			}
		})
	}
}

func TestInstallOwnershipPredicates(t *testing.T) {
	current := uint32(os.Geteuid())
	for _, test := range []struct {
		name            string
		info            ownershipTestInfo
		ancestor, final bool
	}{
		{"current-safe", ownershipTestInfo{mode: 0o700, dir: true, sys: &syscall.Stat_t{Uid: current}}, true, true},
		{"root-ancestor", ownershipTestInfo{mode: 0o755, dir: true, sys: &syscall.Stat_t{}}, true, current == 0},
		{"sticky-ancestor", ownershipTestInfo{mode: os.ModeSticky | 0o777, dir: true, sys: &syscall.Stat_t{Uid: current}}, true, false},
		{"foreign", ownershipTestInfo{mode: 0o700, dir: true, sys: &syscall.Stat_t{Uid: current + 1}}, false, false},
		{"writable", ownershipTestInfo{mode: 0o722, dir: true, sys: &syscall.Stat_t{Uid: current}}, false, false},
		{"unknown", ownershipTestInfo{mode: 0o700, dir: true}, false, false},
		{"nondir", ownershipTestInfo{mode: 0o700, sys: &syscall.Stat_t{Uid: current}}, false, false},
		{"symlink", ownershipTestInfo{mode: os.ModeSymlink | 0o700, dir: true, sys: &syscall.Stat_t{Uid: current}}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if safeInstallAncestor(test.info) != test.ancestor || ownedInstallDirectory(test.info) != test.final {
				t.Fatalf("ancestor=%v final=%v", safeInstallAncestor(test.info), ownedInstallDirectory(test.info))
			}
		})
	}
}
