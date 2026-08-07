package codex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/vgxness/vgxness/internal/integration"
)

const pendingName = ".vgxness-pending"

// Root confines slice-3 filesystem work to one Go 1.26 os.Root descriptor.
// Same-UID namespace mutation or retained writable descriptors are residual.
type Root struct {
	Path        string
	fs          *os.Root
	syncHook    func(string) error
	afterRename func(string) error
	stat        func(string) (os.FileInfo, error)
}
type Anchor struct {
	Name, Temp string
	Info       os.FileInfo
	Bytes      []byte
}
type Backup struct {
	Name, Sidecar string
	Info          os.FileInfo
	Bytes         []byte
}

func OpenRoot(ctx context.Context, options integration.Options, create bool) (*Root, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := rootPath(options)
	if err != nil {
		return nil, err
	}
	if create {
		if err = ensureRoot(path); err != nil {
			return nil, err
		}
	}
	if err := safeAncestors(path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !ownedDir(info) {
		return nil, invalid("root is not private directory")
	}
	fs, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	if opened, err := fs.Lstat("."); err != nil || !os.SameFile(info, opened) {
		_ = fs.Close()
		return nil, conflict("root changed while opening")
	}
	return &Root{Path: path, fs: fs}, nil
}

func (r *Root) Close() error { return r.fs.Close() }

func rootPath(options integration.Options) (string, error) {
	c, h := strings.TrimSpace(options.ConfigDir), strings.TrimSpace(options.HomeDir)
	if c != "" && h != "" {
		return "", invalid("ambiguous root")
	}
	if c == "" {
		if h == "" {
			return "", invalid("missing root")
		}
		c = filepath.Join(h, ".codex")
	}
	if !filepath.IsAbs(c) || filepath.Clean(c) != c {
		return "", invalid("relative root")
	}
	if info, err := os.Lstat(c); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", invalid("symlink root")
	}
	parts := []string{}
	for p := c; ; p = filepath.Dir(p) {
		if _, err := os.Lstat(p); err == nil {
			real, err := filepath.EvalSymlinks(p)
			if err != nil {
				return "", err
			}
			for i := len(parts) - 1; i >= 0; i-- {
				real = filepath.Join(real, parts[i])
			}
			return real, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", invalid("no existing ancestor")
		}
		parts = append(parts, filepath.Base(p))
	}
}

func ensureRoot(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if !ownedDir(info) {
			return invalid("root is not private directory")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(path)
	if err := ensureParent(parent); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !ownedDir(info) {
		return invalid("created unsafe root")
	}
	if err := syncPath(path); err != nil {
		return err
	}
	return syncPath(parent)
}

func ensureParent(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return invalid("unsafe root parent")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return invalid("root parent")
	}
	if err := ensureParent(parent); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return syncPath(parent)
}

func safeAncestors(path string) error {
	for p := path; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err == nil && !safeDir(info) {
			return invalid("unsafe root ancestor")
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if next := filepath.Dir(p); next == p {
			return nil
		}
	}
}

func safeDir(info os.FileInfo) bool {
	return info.IsDir() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0o022 == 0
}
func ownedDir(info os.FileInfo) bool { return safeDir(info) && owned(info) }
func invalid(s string) error         { return fmt.Errorf("%w: %s", integration.ErrInvalid, s) }
func conflict(s string) error        { return fmt.Errorf("%w: %s", integration.ErrConflict, s) }
func drift(s string) error           { return fmt.Errorf("%w: %s", integration.ErrDrift, s) }
func recovery(err error) error       { return errors.Join(integration.ErrRecovery, err) }

func (r *Root) name(name string) error {
	if err := validateRelativePath(name); err != nil {
		return invalid(err.Error())
	}
	return nil
}
func (r *Root) Mkdir(name string) error {
	if err := r.name(name); err != nil {
		return err
	}
	if info, err := r.fs.Lstat(name); err == nil {
		if !ownedDir(info) {
			return invalid("unsafe subdirectory")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := r.fs.Mkdir(name, 0o700); err != nil {
		return err
	}
	if info, err := r.fs.Lstat(name); err != nil || !ownedDir(info) {
		return invalid("created unsafe subdirectory")
	}
	if err := r.sync(name); err != nil {
		return recovery(err)
	}
	if err := r.sync(filepath.Dir(name)); err != nil {
		return recovery(err)
	}
	return nil
}
func (r *Root) sync(name string) error {
	if r.syncHook != nil {
		return r.syncHook(name)
	}
	f, err := r.fs.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func (r *Root) Read(name string, limit int) ([]byte, os.FileInfo, error) {
	if err := r.name(name); err != nil {
		return nil, nil, err
	}
	info, err := r.fs.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, drift("not regular")
	}
	if info.Size() > int64(limit) {
		return nil, nil, drift("read bound")
	}
	f, err := r.fs.Open(name)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, nil, conflict("changed while opening")
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, nil, err
	}
	if len(b) > limit {
		return nil, nil, drift("read bound")
	}
	return b, opened, nil
}

func (r *Root) side(name, suffix string) string { return name + suffix }

func (r *Root) Publish(ctx context.Context, name string, data []byte) (Anchor, error) {
	if err := ctx.Err(); err != nil {
		return Anchor{}, err
	}
	if err := r.name(name); err != nil {
		return Anchor{}, err
	}
	tmp := r.side(name, ".vgxness-stage")
	f, err := r.fs.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return Anchor{}, recovery(conflict("stage exists"))
		}
		return Anchor{}, err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if close := f.Close(); err == nil {
		err = close
	}
	if err != nil {
		_ = r.fs.Remove(tmp)
		return Anchor{}, err
	}
	info, err := r.fs.Lstat(tmp)
	if err != nil || !info.Mode().IsRegular() {
		return Anchor{}, recovery(errors.Join(err, drift("stage")))
	}
	a := Anchor{Name: name, Temp: tmp, Info: info, Bytes: append([]byte(nil), data...)}
	if err = r.fs.Link(tmp, name); err != nil {
		_ = r.fs.Remove(tmp)
		if errors.Is(err, os.ErrExist) {
			return Anchor{}, conflict("target exists")
		}
		return Anchor{}, err
	}
	if err = r.sync(filepath.Dir(name)); err != nil {
		return a, recovery(err)
	}
	b, _, err := r.Read(name, len(data)+1)
	if err != nil {
		return a, recovery(err)
	}
	if !bytes.Equal(b, data) {
		return a, recovery(drift("publish readback"))
	}
	if ok := r.same(name, tmp, data, a.Info); !ok {
		return a, recovery(conflict("publish identity"))
	}
	return a, nil
}

func (r *Root) same(a, b string, data []byte, want os.FileInfo) bool {
	ai, e := r.fs.Lstat(a)
	if e != nil || !ai.Mode().IsRegular() || !os.SameFile(ai, want) {
		return false
	}
	bi, e := r.fs.Lstat(b)
	if e != nil || !os.SameFile(ai, bi) {
		return false
	}
	got, _, e := r.Read(a, len(data)+1)
	return e == nil && bytes.Equal(got, data)
}
func (r *Root) Backup(name string, data []byte) (Backup, error) {
	b, info, err := r.Read(name, len(data)+1)
	if err != nil {
		return Backup{}, err
	}
	if !bytes.Equal(b, data) {
		return Backup{}, drift("backup bytes")
	}
	s := r.side(name, ".vgxness-remove")
	if err := r.fs.Link(name, s); err != nil {
		if errors.Is(err, os.ErrExist) {
			return Backup{}, conflict("backup exists")
		}
		return Backup{}, err
	}
	if err := r.sync(filepath.Dir(name)); err != nil {
		return Backup{}, recovery(err)
	}
	if !r.same(name, s, data, info) {
		return Backup{}, recovery(conflict("backup identity"))
	}
	return Backup{Name: name, Sidecar: s, Info: info, Bytes: append([]byte(nil), data...)}, nil
}
func (r *Root) Restore(b Backup) error {
	if !r.same(b.Sidecar, b.Sidecar, b.Bytes, b.Info) {
		return recovery(conflict("backup replaced"))
	}
	if _, err := r.fs.Lstat(b.Name); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return conflict("restore target exists")
		}
		return err
	}
	if err := r.fs.Link(b.Sidecar, b.Name); err != nil {
		return recovery(err)
	}
	if err := r.sync(filepath.Dir(b.Name)); err != nil {
		return recovery(err)
	}
	return nil
}
func (r *Root) Quarantine(a Anchor) (Backup, error) {
	if !r.same(a.Name, a.Temp, a.Bytes, a.Info) {
		return Backup{}, conflict("target replaced")
	}
	s := r.side(a.Name, ".vgxness-remove")
	if _, err := r.lstat(s); err == nil {
		return Backup{}, conflict("sidecar exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Backup{}, err
	}
	if err := r.fs.Rename(a.Name, s); err != nil {
		return Backup{}, err
	}
	if r.afterRename != nil {
		if err := r.afterRename(s); err != nil {
			return Backup{}, recovery(err)
		}
	}
	if err := r.sync(filepath.Dir(a.Name)); err != nil {
		return Backup{}, recovery(err)
	}
	info, err := r.fs.Lstat(s)
	if err != nil || !r.same(s, s, a.Bytes, a.Info) {
		return Backup{}, recovery(conflict("quarantine identity"))
	}
	return Backup{Name: a.Name, Sidecar: s, Info: info, Bytes: a.Bytes}, nil
}
func (r *Root) lstat(name string) (os.FileInfo, error) {
	if r.stat != nil {
		return r.stat(name)
	}
	return r.fs.Lstat(name)
}
func (r *Root) RemoveBackup(b Backup) error {
	if !r.same(b.Sidecar, b.Sidecar, b.Bytes, b.Info) {
		return recovery(conflict("backup replaced"))
	}
	if err := r.fs.Remove(b.Sidecar); err != nil {
		return err
	}
	if err := r.sync(filepath.Dir(b.Name)); err != nil {
		return recovery(err)
	}
	return nil
}
func (r *Root) RemoveAnchor(a Anchor) error {
	if !r.same(a.Name, a.Temp, a.Bytes, a.Info) {
		return recovery(conflict("anchor replaced"))
	}
	if err := r.fs.Remove(a.Name); err != nil {
		return recovery(err)
	}
	if err := r.sync(filepath.Dir(a.Name)); err != nil {
		return recovery(err)
	}
	if !r.same(a.Temp, a.Temp, a.Bytes, a.Info) {
		return recovery(conflict("anchor replaced"))
	}
	if err := r.fs.Remove(a.Temp); err != nil {
		return recovery(err)
	}
	return r.sync(filepath.Dir(a.Name))
}

func (r *Root) MarkPending() error {
	f, err := r.fs.OpenFile(pendingName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return recovery(conflict("pending"))
		}
		return err
	}
	if _, err = f.Write([]byte("codex-pending\n")); err == nil {
		err = f.Sync()
	}
	if close := f.Close(); err == nil {
		err = close
	}
	if err != nil {
		return recovery(err)
	}
	return r.sync(".")
}
func (r *Root) Pending() (bool, error) {
	b, _, err := r.Read(pendingName, 32)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, recovery(err)
	}
	if string(b) != "codex-pending\n" {
		return true, recovery(drift("pending"))
	}
	return true, nil
}
func (r *Root) ClearPending() error {
	b, info, err := r.Read(pendingName, 32)
	if err != nil {
		return err
	}
	if string(b) != "codex-pending\n" || !info.Mode().IsRegular() {
		return recovery(drift("pending"))
	}
	if err = r.fs.Remove(pendingName); err != nil {
		return recovery(err)
	}
	if err = r.sync("."); err != nil {
		if again := r.MarkPending(); again != nil {
			return recovery(errors.Join(err, again))
		}
		return recovery(err)
	}
	return nil
}
func syncPath(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
