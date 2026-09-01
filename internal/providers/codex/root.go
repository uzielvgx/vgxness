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
const activationPendingName = ".vgxness-activation-pending"

type Root struct {
	Path                  string
	fs                    *os.Root
	syncHook, afterRename func(string) error
	stat                  func(string) (os.FileInfo, error)
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
	if err := safeAncestors(path); err != nil {
		return nil, err
	}
	if create {
		if err = ensureRoot(path); err != nil {
			return nil, err
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !ownedDir(path, info) {
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
		if !ownedDir(path, info) {
			return invalid("root is not private directory")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(path)
	parentInfo, err := os.Lstat(parent)
	if errors.Is(err, os.ErrNotExist) {
		return invalid("missing root parent")
	}
	if err != nil {
		return err
	}
	if !safeAncestor(parent, parentInfo) {
		return invalid("unsafe root parent")
	}
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !ownedDir(path, info) {
		return invalid("created unsafe root")
	}
	if err := syncPath(path); err != nil {
		return err
	}
	return syncPath(parent)
}
func safeAncestors(path string) error {
	for p := path; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err == nil && !safeAncestor(p, info) {
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
func invalid(s string) error   { return fmt.Errorf("%w: %s", integration.ErrInvalid, s) }
func conflict(s string) error  { return fmt.Errorf("%w: %s", integration.ErrConflict, s) }
func drift(s string) error     { return fmt.Errorf("%w: %s", integration.ErrDrift, s) }
func recovery(err error) error { return errors.Join(integration.ErrRecovery, err) }
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
	current := r.fs
	parts := strings.Split(name, "/")
	created := false
	for index, component := range parts {
		info, err := current.Lstat(component)
		if errors.Is(err, os.ErrNotExist) {
			if err = current.Mkdir(component, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			created = true
			parent := "."
			if index > 0 {
				parent = strings.Join(parts[:index], "/")
			}
			if err := r.sync(parent); err != nil {
				return err
			}
			info, err = current.Lstat(component)
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return invalid("unsafe subdirectory")
		}
		next, err := current.OpenRoot(component)
		if err != nil {
			return err
		}
		opened, err := next.Stat(".")
		if err != nil || !os.SameFile(info, opened) {
			_ = next.Close()
			return conflict("subdirectory changed while opening")
		}
		if created {
			dir, syncErr := next.Open(".")
			if syncErr != nil {
				_ = next.Close()
				return syncErr
			}
			syncErr = syncHeldDirectory(dir)
			closeErr := dir.Close()
			if syncErr != nil || closeErr != nil {
				_ = next.Close()
				return errors.Join(syncErr, closeErr)
			}
		}
		if current != r.fs && current.Close() != nil {
			_ = next.Close()
			return errors.New("close held subdirectory")
		}
		current = next
	}
	if current != r.fs {
		defer current.Close()
	}
	if !created {
		return nil
	}
	return nil
}
func (r *Root) sync(name string) error {
	if r.syncHook != nil {
		return r.syncHook(name)
	}
	// Sync through the held root handle; the pathname comparison is only the
	// replacement guard, never the object acknowledged for durability.
	held, heldErr := r.fs.Lstat(".")
	visible, visibleErr := os.Lstat(r.Path)
	if heldErr != nil {
		return heldErr
	}
	if visibleErr != nil || !os.SameFile(held, visible) {
		return conflict("root replaced before durability sync")
	}
	dir, err := r.openDir(name)
	if err != nil {
		return err
	}
	file, err := dir.Open(".")
	if err == nil {
		err = syncHeldDirectory(file)
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}
	closeErr := dir.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	visible, visibleErr = os.Lstat(r.Path)
	if visibleErr != nil || !os.SameFile(held, visible) {
		return conflict("root replaced during durability sync")
	}
	return nil
}

// parent holds the containing directory open and returns only a basename for
// every artifact operation. os.Root deliberately does not create intermediate
// directories for an OpenFile path; using a held parent also prevents a nested
// directory replacement from redirecting a transaction sidecar.
func (r *Root) parent(name string) (*os.Root, string, error) {
	if err := r.name(name); err != nil {
		return nil, "", err
	}
	dir, base := filepath.Dir(name), filepath.Base(name)
	parent, err := r.openDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", fmt.Errorf("parent %q: %w", dir, os.ErrNotExist)
		}
		return nil, "", fmt.Errorf("open parent %q: %w", dir, err)
	}
	return parent, base, nil
}

func (r *Root) openDir(dir string) (*os.Root, error) {
	if dir == "." {
		return r.fs.OpenRoot(".")
	}
	current := r.fs
	for _, component := range strings.Split(dir, "/") {
		info, err := current.Lstat(component)
		if err != nil {
			if current != r.fs {
				_ = current.Close()
			}
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			if current != r.fs {
				_ = current.Close()
			}
			return nil, invalid("unsafe subdirectory")
		}
		next, err := current.OpenRoot(component)
		if err != nil {
			if current != r.fs {
				_ = current.Close()
			}
			return nil, err
		}
		opened, err := next.Stat(".")
		if err != nil || !os.SameFile(info, opened) {
			if current != r.fs {
				_ = current.Close()
			}
			_ = next.Close()
			return nil, conflict("subdirectory changed while opening")
		}
		if current != r.fs {
			if err := current.Close(); err != nil {
				_ = next.Close()
				return nil, err
			}
		}
		current = next
	}
	return current, nil
}
func (r *Root) Read(name string, limit int) ([]byte, os.FileInfo, error) {
	parent, base, err := r.parent(name)
	if err != nil {
		return nil, nil, err
	}
	defer parent.Close()
	info, err := parent.Lstat(base)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, drift("not regular")
	}
	if info.Size() > int64(limit) {
		return nil, nil, drift("read bound")
	}
	f, err := parent.Open(base)
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
	parent, base, err := r.parent(name)
	if err != nil {
		return Anchor{}, err
	}
	defer parent.Close()
	tmp := r.side(name, ".vgxness-stage")
	tmpBase := filepath.Base(tmp)
	f, err := parent.OpenFile(tmpBase, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
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
		_ = parent.Remove(tmpBase)
		return Anchor{}, err
	}
	info, err := parent.Lstat(tmpBase)
	if err != nil || !info.Mode().IsRegular() {
		return Anchor{}, recovery(errors.Join(err, drift("stage")))
	}
	a := Anchor{Name: name, Temp: tmp, Info: info, Bytes: append([]byte(nil), data...)}
	if err = parent.Link(tmpBase, base); err != nil {
		_ = parent.Remove(tmpBase)
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
	pa, ba, e := r.parent(a)
	if e != nil {
		return false
	}
	defer pa.Close()
	pb, bb, e := r.parent(b)
	if e != nil {
		return false
	}
	defer pb.Close()
	ai, e := pa.Lstat(ba)
	if e != nil || !ai.Mode().IsRegular() || !os.SameFile(ai, want) {
		return false
	}
	bi, e := pb.Lstat(bb)
	if e != nil || !os.SameFile(ai, bi) {
		return false
	}
	got, _, e := r.Read(a, len(data)+1)
	return e == nil && bytes.Equal(got, data)
}
func (r *Root) Backup(name string, data []byte) (Backup, error) {
	return r.BackupExact(name, data, nil)
}

func (r *Root) BackupExact(name string, data []byte, expected os.FileInfo) (Backup, error) {
	b, info, err := r.Read(name, len(data)+1)
	if err != nil {
		return Backup{}, err
	}
	if expected != nil && !os.SameFile(info, expected) {
		return Backup{}, conflict("backup target replaced")
	}
	if !bytes.Equal(b, data) {
		return Backup{}, drift("backup bytes")
	}
	s := r.side(name, ".vgxness-remove")
	parent, base, err := r.parent(name)
	if err != nil {
		return Backup{}, err
	}
	defer parent.Close()
	if err := parent.Link(base, filepath.Base(s)); err != nil {
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
	parent, base, err := r.parent(b.Name)
	if err != nil {
		return err
	}
	defer parent.Close()
	if _, err := parent.Lstat(base); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return conflict("restore target exists")
		}
		return err
	}
	if err := parent.Link(filepath.Base(b.Sidecar), base); err != nil {
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
	parent, base, err := r.parent(a.Name)
	if err != nil {
		return Backup{}, err
	}
	defer parent.Close()
	if err := parent.Rename(base, filepath.Base(s)); err != nil {
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
	info, err := parent.Lstat(filepath.Base(s))
	if err != nil || !r.same(s, s, a.Bytes, a.Info) {
		return Backup{}, recovery(conflict("quarantine identity"))
	}
	return Backup{Name: a.Name, Sidecar: s, Info: info, Bytes: a.Bytes}, nil
}
func (r *Root) lstat(name string) (os.FileInfo, error) {
	if r.stat != nil {
		return r.stat(name)
	}
	parent, base, err := r.parent(name)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	return parent.Lstat(base)
}
func (r *Root) RemoveBackup(b Backup) error {
	if !r.same(b.Sidecar, b.Sidecar, b.Bytes, b.Info) {
		return recovery(conflict("backup replaced"))
	}
	parent, base, err := r.parent(b.Sidecar)
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := parent.Remove(base); err != nil {
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
	parent, base, err := r.parent(a.Name)
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := parent.Remove(base); err != nil {
		return recovery(err)
	}
	if err := r.sync(filepath.Dir(a.Name)); err != nil {
		return recovery(err)
	}
	if !r.same(a.Temp, a.Temp, a.Bytes, a.Info) {
		return recovery(conflict("anchor replaced"))
	}
	if err := parent.Remove(filepath.Base(a.Temp)); err != nil {
		return recovery(err)
	}
	return r.sync(filepath.Dir(a.Name))
}
func (r *Root) CommitAnchor(a Anchor) error {
	if !r.same(a.Name, a.Temp, a.Bytes, a.Info) {
		return recovery(conflict("anchor replaced"))
	}
	parent, base, err := r.parent(a.Temp)
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := parent.Remove(base); err != nil {
		return recovery(err)
	}
	if err := r.sync(filepath.Dir(a.Name)); err != nil {
		return recovery(err)
	}
	parent, base, err = r.parent(a.Name)
	if err != nil {
		return recovery(err)
	}
	defer parent.Close()
	info, err := parent.Lstat(base)
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(info, a.Info) {
		return recovery(errors.Join(err, conflict("committed target replaced")))
	}
	data, _, err := r.Read(a.Name, len(a.Bytes)+1)
	if err != nil || !bytes.Equal(data, a.Bytes) {
		return recovery(errors.Join(err, drift("committed target bytes")))
	}
	return nil
}

func (r *Root) RemoveTarget(b Backup) error {
	if !r.same(b.Name, b.Sidecar, b.Bytes, b.Info) {
		return recovery(conflict("target replaced"))
	}
	parent, base, err := r.parent(b.Name)
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := parent.Remove(base); err != nil {
		return recovery(err)
	}
	if err := r.sync(filepath.Dir(b.Name)); err != nil {
		return recovery(err)
	}
	if _, err := parent.Lstat(base); !errors.Is(err, os.ErrNotExist) {
		return recovery(errors.Join(conflict("target retained"), err))
	}
	return nil
}

func pendingEvidence(sha string) []byte { return []byte("codex-pending-v2\nsha256=" + sha + "\n") }

func (r *Root) MarkPending(body []byte) error {
	if !validPendingEvidence(body) || string(body) == "codex-pending\n" {
		return recovery(drift("pending"))
	}
	f, err := r.fs.OpenFile(pendingName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			got, info, readErr := r.Read(pendingName, len(body)+1)
			if readErr == nil && info.Mode().IsRegular() && bytes.Equal(got, body) {
				return nil
			}
			return recovery(conflict("pending"))
		}
		return err
	}
	if _, err = f.Write(body); err == nil {
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
	_, present, err := r.PendingEvidence()
	return present, err
}
func validPendingEvidence(b []byte) bool {
	if string(b) == "codex-pending\n" {
		return true
	}
	const prefix = "codex-pending-v2\nsha256="
	if len(b) != len(prefix)+65 || !bytes.HasPrefix(b, []byte(prefix)) || b[len(b)-1] != '\n' {
		return false
	}
	for _, c := range b[len(prefix) : len(prefix)+64] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func (r *Root) PendingEvidence() ([]byte, bool, error) {
	b, _, err := r.Read(pendingName, 256)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, recovery(err)
	}
	if !validPendingEvidence(b) {
		return b, true, recovery(drift("pending"))
	}
	return b, true, nil
}
func (r *Root) ClearPending() error {
	b, info, err := r.Read(pendingName, 256)
	if err != nil {
		return err
	}
	if !validPendingEvidence(b) || !info.Mode().IsRegular() {
		return recovery(drift("pending"))
	}
	if err = r.fs.Remove(pendingName); err != nil {
		return recovery(err)
	}
	if err = r.sync("."); err != nil {
		if again := r.restorePending(b); again != nil {
			return recovery(errors.Join(err, again))
		}
		return recovery(err)
	}
	return nil
}

func (r *Root) restorePending(body []byte) error {
	f, err := r.fs.OpenFile(pendingName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return recovery(err)
	}
	if _, err = f.Write(body); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return recovery(err)
	}
	return r.sync(".")
}

func (r *Root) MarkActivationPending(body []byte) error {
	f, err := r.fs.OpenFile(activationPendingName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		got, info, readErr := r.Read(activationPendingName, len(body)+1)
		if readErr != nil || !info.Mode().IsRegular() || !bytes.Equal(got, body) {
			return recovery(errors.Join(readErr, drift("activation pending")))
		}
		return nil
	}
	if err != nil {
		return recovery(err)
	}
	if _, err = f.Write(body); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return recovery(err)
	}
	return r.sync(".")
}

func (r *Root) ActivationPending() ([]byte, bool, error) {
	body, info, err := r.Read(activationPendingName, 256)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return nil, true, recovery(errors.Join(err, drift("activation pending")))
	}
	return body, true, nil
}

func (r *Root) ClearActivationPending(body []byte) error {
	got, info, err := r.Read(activationPendingName, len(body)+1)
	if err != nil || !info.Mode().IsRegular() || !bytes.Equal(got, body) {
		return recovery(errors.Join(err, drift("activation pending")))
	}
	if err = r.fs.Remove(activationPendingName); err != nil {
		return recovery(err)
	}
	if err = r.sync("."); err != nil {
		if again := r.MarkActivationPending(body); again != nil {
			return recovery(errors.Join(err, again))
		}
		return recovery(err)
	}
	return nil
}
