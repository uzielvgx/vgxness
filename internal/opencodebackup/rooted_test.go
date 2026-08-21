package opencodebackup

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestRootAnchorPinsAndReopensSameRoot(t *testing.T) {
	directory := t.TempDir()
	anchor, err := newRootAnchor(directory)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		root, err := anchor.open()
		if err != nil {
			t.Fatal(err)
		}
		if err := root.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRootAnchorRejectsReplacement(t *testing.T) {
	parent := t.TempDir()
	directory := filepath.Join(parent, "root")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	anchor, err := newRootAnchor(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(directory, filepath.Join(parent, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if root, err := anchor.open(); root != nil || !errors.Is(err, ErrConflict) {
		t.Fatalf("open() = (%v, %v), want nil, ErrConflict", root, err)
	}
}

func TestOpenSnapshotOwnsChildrenAndRejectsUnsafeNames(t *testing.T) {
	directory := t.TempDir()
	anchor, err := newRootAnchor(directory)
	if err != nil {
		t.Fatal(err)
	}
	id := "20260820T000000.000000000Z-0123456789abcdef"
	if err := os.Mkdir(filepath.Join(directory, id), 0o700); err != nil {
		t.Fatal(err)
	}
	ref, err := openSnapshot(anchor, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := ref.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ref.snapshot.Stat("."); err == nil {
		t.Fatal("snapshot root remains usable after Close")
	}
	if err := ref.Close(); err != nil {
		t.Fatal(err)
	}
	for _, unsafe := range []string{"../x", "not-an-id"} {
		if ref, err := openSnapshot(anchor, unsafe); ref != nil || !errors.Is(err, ErrInvalid) {
			t.Fatalf("openSnapshot(%q) = (%v, %v), want nil, ErrInvalid", unsafe, ref, err)
		}
	}
}

func TestOpenSnapshotRejectsSymlinkAndConcurrentOpens(t *testing.T) {
	directory := t.TempDir()
	anchor, err := newRootAnchor(directory)
	if err != nil {
		t.Fatal(err)
	}
	id := "20260820T000000.000000000Z-0123456789abcdef"
	if err := os.Symlink(t.TempDir(), filepath.Join(directory, id)); err == nil {
		if ref, err := openSnapshot(anchor, id); ref != nil || !errors.Is(err, ErrCorrupt) {
			t.Fatalf("symlink open = (%v, %v), want nil, ErrCorrupt", ref, err)
		}
	} else {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.Remove(filepath.Join(directory, id)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, id), 0o700); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			ref, err := openSnapshot(anchor, id)
			if err == nil {
				err = ref.Close()
			}
			if err != nil {
				t.Errorf("open snapshot: %v", err)
			}
		})
	}
	group.Wait()
}
