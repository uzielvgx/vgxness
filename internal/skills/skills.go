package skills

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

//go:embed pack/**
var bundled embed.FS

var (
	ErrInvalid  = errors.New("invalid skills request")
	ErrConflict = errors.New("skills target contains unmanaged content")
	ErrDrift    = errors.New("managed skills content differs from bundle")
	ErrRecovery = errors.New("skills transaction recovery failed")
)

type State string

const (
	StateAbsent    State = "absent"
	StatePartial   State = "partial"
	StateInstalled State = "installed"
	StateDrifted   State = "drifted"
	StateConflict  State = "conflict"
)

type Options struct{ Dir string }
type Result struct {
	State        State             `json:"state"`
	Path         string            `json:"path"`
	BackupPath   string            `json:"backupPath,omitempty"`
	FileCount    int               `json:"fileCount"`
	Changed      bool              `json:"changed"`
	UpdateNeeded bool              `json:"updateNeeded"`
	Hashes       map[string]string `json:"hashes"`
}

type Runtime interface {
	Preview(context.Context, Options) (Result, error)
	Install(context.Context, Options) (Result, error)
	Status(context.Context, Options) (Result, error)
	Uninstall(context.Context, Options) (Result, error)
}

// Service deliberately keeps failure injection private; it is used only by
// package tests to prove that the transaction recovers after publication.
type Service struct {
	catalog *catalog
	// beforePublish and beforeRollback are deterministic test checkpoints for
	// replacement races at the two no-overwrite boundaries.
	beforePublish  func(string) error
	beforeRollback func(string) error
	afterPublish   func(string) error
	afterRename    func(string) error
	beforeMove     func(source, destination string) error
	beforeBackup   func(string) error
	beforePrune    func() error
	afterInspect   func()
}

func New() *Service { return &Service{} }

func skillsRoot(options Options) (string, error) {
	dir := options.Dir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		// Home directories may intentionally be symlinked. Anchor the default at
		// their resolved location before opening the selected root.
		home, err = filepath.EvalSymlinks(home)
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".agents", "skills")
	}
	if !filepath.IsAbs(dir) || strings.IndexByte(dir, 0) >= 0 {
		return "", ErrInvalid
	}
	return filepath.Clean(dir), nil
}

func files() (map[string][]byte, error) {
	return bundledFiles("agent-skill-engineer")
}

func bundledFiles(skill string) (map[string][]byte, error) {
	if !validSkillName(skill) {
		return nil, ErrInvalid
	}
	entries := map[string][]byte{}
	prefix := "pack/" + skill + "/"
	err := fs.WalkDir(bundled, "pack/"+skill, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative := strings.TrimPrefix(name, prefix)
		if relative == name || !validRelative(relative) {
			return ErrInvalid
		}
		content, err := bundled.ReadFile(name)
		if err != nil {
			return err
		}
		entries[relative] = content
		return nil
	})
	return entries, err
}

func validRelative(relative string) bool {
	return relative != "" && relative != "." && fs.ValidPath(relative) && !path.IsAbs(relative) && path.Clean(relative) == relative && !strings.ContainsAny(relative, "\\:\x00")
}
func digest(content []byte) string { sum := sha256.Sum256(content); return fmt.Sprintf("%x", sum) }
func hashes(entries map[string][]byte) map[string]string {
	out := make(map[string]string, len(entries))
	for n, b := range entries {
		out[n] = digest(b)
	}
	return out
}
func sorted(entries map[string][]byte) []string {
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
func publishOrder(entries map[string][]byte) []string {
	names := sorted(entries)
	sort.SliceStable(names, func(i, j int) bool {
		leftSkill, leftRelative, _ := strings.Cut(names[i], "/")
		rightSkill, rightRelative, _ := strings.Cut(names[j], "/")
		if leftSkill != rightSkill {
			return leftSkill < rightSkill
		}
		return leftRelative != "SKILL.md" && rightRelative == "SKILL.md"
	})
	return names
}

func openExisting(options Options) (*os.Root, string, error) {
	path, err := skillsRoot(options)
	if err != nil {
		return nil, "", err
	}
	return openRoot(context.Background(), path, false)
}

// openWritableRoot creates the missing selected-root chain through the volume
// root. Each component is identity-bound before advancing so a pathname
// replacement cannot redirect later writes.
func openWritableRoot(ctx context.Context, options Options) (*os.Root, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	path, err := skillsRoot(options)
	if err != nil {
		return nil, "", err
	}
	return openRoot(ctx, path, true)
}

// openRoot walks from the filesystem volume root without reopening an
// untrusted pathname. Its returned Root is owned by the caller.
func openRoot(ctx context.Context, selected string, writable bool) (*os.Root, string, error) {
	volumeRoot := filepath.VolumeName(selected) + string(filepath.Separator)
	current, err := os.OpenRoot(volumeRoot)
	if err != nil {
		return nil, selected, err
	}
	closeCurrent := func(cause error) (*os.Root, string, error) {
		return nil, selected, errors.Join(cause, current.Close())
	}
	components := strings.Split(strings.TrimPrefix(selected, volumeRoot), string(filepath.Separator))
	for _, component := range components {
		if component == "" || component == "." {
			continue
		}
		if writable {
			if err := ctx.Err(); err != nil {
				return closeCurrent(err)
			}
		}
		info, err := current.Lstat(component)
		if errors.Is(err, os.ErrNotExist) {
			if !writable {
				return closeCurrent(nil)
			}
			if err := current.Mkdir(component, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
				return closeCurrent(err)
			}
			info, err = current.Lstat(component)
		}
		if err != nil {
			return closeCurrent(err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return closeCurrent(ErrConflict)
		}
		child, err := current.OpenRoot(component)
		if err != nil {
			return closeCurrent(err)
		}
		childInfo, err := child.Stat(".")
		if err != nil {
			_ = child.Close()
			return closeCurrent(err)
		}
		if !os.SameFile(info, childInfo) {
			_ = child.Close()
			return closeCurrent(ErrConflict)
		}
		if err := current.Close(); err != nil {
			_ = child.Close()
			return nil, selected, err
		}
		current = child
	}
	return current, selected, nil
}

func (s *Service) inspect(options Options) (Result, map[string][]byte, error) {
	rootPath, err := skillsRoot(options)
	if err != nil {
		return Result{}, nil, err
	}
	entries, err := s.entries()
	if err != nil {
		return Result{}, nil, err
	}
	result := Result{State: StateAbsent, Path: rootPath, FileCount: len(entries), Hashes: hashes(entries)}
	r, _, err := openExisting(options)
	if err != nil {
		return result, entries, err
	}
	if r == nil {
		return result, entries, nil
	}
	defer r.Close()
	for _, skill := range skillNames(entries) {
		info, err := r.Lstat(native(skill))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return result, entries, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			result.State = StateConflict
			return result, entries, ErrConflict
		}
	}
	present, predecessors, missing := 0, 0, 0
	for _, identity := range sorted(entries) {
		actual, _, err := regular(r, native(identity))
		if errors.Is(err, os.ErrNotExist) {
			pending, pendingErr := transactionPending(r, native(identity))
			if pendingErr != nil {
				return result, entries, pendingErr
			}
			if pending {
				return result, entries, ErrRecovery
			}
			missing++
			continue
		}
		if err != nil {
			result.State = StateConflict
			return result, entries, ErrConflict
		}
		pending, pendingErr := transactionPending(r, native(identity))
		if pendingErr != nil {
			return result, entries, pendingErr
		}
		if pending {
			return result, entries, ErrRecovery
		}
		present++
		if bytes.Equal(actual, entries[identity]) {
			continue
		}
		if s.predecessor(identity, actual) {
			predecessors++
			continue
		}
		result.State = StateDrifted
		return result, entries, ErrDrift
	}
	if present == 0 {
		return result, entries, nil
	}
	if missing > 0 {
		result.State = StatePartial
		return result, entries, nil
	}
	result.State, result.UpdateNeeded = StateInstalled, predecessors > 0
	return result, entries, nil
}

func regular(r *os.Root, name string) ([]byte, os.FileInfo, error) {
	info, err := r.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, info, ErrConflict
	}
	data, err := r.ReadFile(name)
	return data, info, err
}

// predecessor recognizes only exact SHA-256 identities from the complete external v0.2.0 package.
func predecessor(relative string, content []byte) bool {
	return predecessorDigest(relative, digest(content))
}

var predecessorDigests = map[string]string{
	"SKILL.md": "c327cbe5604210494c40d413b03fa5f4c26462785dc3c2926aa291500908d35d", "LICENSE.txt": "1efd6091b70b35e21e7eb2ac1db17892a22ea60d32074e3f613c973833ca6e24", "skill-manifest.json": "6a1d59a957de5d6cbd74500b2c24cae2f3e166886a15219077095a6f531c0bd3", "agents/openai.yaml": "8b438047a165e0d562bda9670bfb46db643bd3dff27c63d71a4005a2873bbbc6", "assets/SKILL.template.md": "4700c62c712bd1409c796a04564f1386d49ecc8c8bae98e24ca739c2269d1d6a", "assets/eval-cases.template.json": "a65222a4a57a0c64db5d1b40da070ced6796fe38e5449e983ebb68a0b6e18f05", "references/authoring-methodology.md": "05d63276f6fa728cbbba6bc8154d5c19094505b8778b8587eeb78f747a1eb0b0", "references/evaluation-methodology.md": "092a7e740cd4fd726cd4da16d3015f33873fac992208b5b166901783d0602904", "references/forward-testing.md": "7935b58f939924b751c5bbd0cada648175bf77ce91dc2dba63b8676c6e3bac12", "references/security-review.md": "a8ed9556520ce6678ed5ca5b3e9268aeb48a48e3849b69120c1bd3b07e73ee95", "scripts/generate_openai_yaml.py": "1ee4de86048f6731081e205b0a0211722dc6aef9198400c6d3fb8e133d550ef5", "scripts/init_skill.py": "1a43b96f60d8f542b945c4121f598ce99c115500e8b71753bbc9e041476694a8", "scripts/run_evals.py": "d7a23dc13113866a169120586670a09d14b3c6cff97343e84f05564c65e4b5c2", "scripts/skill_utils.py": "c6b8364928a67ec7c2b8d5d7fe1fcd07f8402840116c8473dad84b9a56500e6c", "scripts/validate_skill.py": "b6171b38c4c624a45f8c8a48e9a20ba7f52529f1b16f2b4b685b9e3182c8fe1d",
}

func predecessorDigest(relative, actual string) bool { return predecessorDigests[relative] == actual }

func (s *Service) Preview(ctx context.Context, options Options) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	result, _, err := s.inspect(options)
	if result.State == StateAbsent || result.State == StatePartial || result.UpdateNeeded {
		result.Changed = true
	}
	return result, err
}
func (s *Service) Status(ctx context.Context, options Options) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	result, _, err := s.inspect(options)
	return result, err
}

type original struct {
	name            string
	data, published []byte
	mode            os.FileMode
	existed         bool
}

func (s *Service) Install(ctx context.Context, options Options) (Result, error) {
	rootPath, err := skillsRoot(options)
	if err != nil {
		return Result{}, err
	}
	entries, err := s.entries()
	if err != nil {
		return Result{}, err
	}
	result := Result{State: StateAbsent, Path: rootPath, FileCount: len(entries), Hashes: hashes(entries)}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result, entries, err = s.inspect(options)
	if err != nil && !errors.Is(err, ErrRecovery) {
		return result, err
	}
	if s != nil && s.afterInspect != nil {
		s.afterInspect()
	}
	if result.State == StateInstalled && !result.UpdateNeeded {
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	r, _, err := openWritableRoot(ctx, options)
	if err != nil {
		return result, err
	}
	defer r.Close()
	if err := recoverTransactions(ctx, r, entries); err != nil {
		return result, errors.Join(ErrRecovery, err)
	}
	result, entries, err = s.inspect(options)
	if err != nil {
		return result, err
	}
	if result.State == StateInstalled && !result.UpdateNeeded {
		return result, nil
	}
	published := []original{}
	var beforeRollback func(string) error
	var beforeMove func(string, string) error
	if s != nil {
		beforeRollback = s.beforeRollback
		beforeMove = s.beforeMove
	}
	fail := func(cause error) (Result, error) {
		if recovery := rollback(r, published, beforeRollback, beforeMove); recovery != nil {
			return result, errors.Join(cause, ErrRecovery, recovery)
		}
		return result, cause
	}
	for _, identity := range publishOrder(entries) {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		name := native(identity)
		if err := ensureDirectory(ctx, r, filepath.Dir(name)); err != nil {
			return fail(err)
		}
		data, info, err := regular(r, name)
		old := original{name: name, mode: 0o644}
		if err == nil {
			old.existed, old.data, old.mode = true, data, info.Mode().Perm()
		} else if !errors.Is(err, os.ErrNotExist) {
			return fail(err)
		}
		if old.existed && bytes.Equal(old.data, entries[identity]) {
			continue
		}
		if old.existed && !s.predecessor(identity, old.data) {
			return fail(ErrDrift)
		}
		committed, err := publish(r, name, entries[identity], old.mode, old.data, old.existed, func() error {
			if s != nil && s.beforePublish != nil {
				return s.beforePublish(identity)
			}
			return nil
		}, func() error {
			if s != nil && s.afterRename != nil {
				return s.afterRename(identity)
			}
			return nil
		}, beforeMove)
		if committed {
			old.published = append([]byte(nil), entries[identity]...)
			published = append(published, old)
		}
		if err != nil {
			return fail(err)
		}
		if s != nil && s.afterPublish != nil {
			if err := s.afterPublish(identity); err != nil {
				return fail(err)
			}
		}
	}
	verified, err := s.Status(context.Background(), options)
	if err != nil || verified.State != StateInstalled || verified.UpdateNeeded {
		return fail(fmt.Errorf("%w: readback", ErrDrift))
	}
	verified.Changed = len(published) > 0
	return verified, nil
}

func ensureDirectory(ctx context.Context, r *os.Root, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name = filepath.Clean(name)
	if name == "." {
		return nil
	}
	if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
		return ErrInvalid
	}
	current := "."
	for _, component := range strings.Split(name, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := r.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := r.Mkdir(current, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			if err := syncDirectory(r, filepath.Dir(current)); err != nil {
				return err
			}
			info, err = r.Lstat(current)
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrConflict
		}
	}
	return nil
}
func publish(r *os.Root, name string, data []byte, mode os.FileMode, expected []byte, existed bool, beforePublish, afterRename func() error, beforeMove func(string, string) error) (bool, error) {
	for i := 0; i < 128; i++ {
		tmp := filepath.Join(filepath.Dir(name), fmt.Sprintf(".vgxness-skill-%d.tmp", i))
		f, err := r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		_, err = f.Write(data)
		if err == nil {
			err = f.Sync()
		}
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = r.Remove(tmp)
			return false, err
		}
		actual, _, err := regular(r, tmp)
		if err != nil || !bytes.Equal(actual, data) {
			_ = r.Remove(tmp)
			return false, fmt.Errorf("%w: temporary readback", ErrDrift)
		}
		if beforePublish != nil {
			if err := beforePublish(); err != nil {
				_ = removeExpectedFile(r, tmp, data)
				return false, err
			}
		}
		if existed {
			previous := transactionPath(name, i)
			if _, err := r.Lstat(previous); err == nil {
				_ = r.Remove(tmp)
				continue
			} else if !errors.Is(err, os.ErrNotExist) {
				_ = r.Remove(tmp)
				return false, err
			}
			if beforeMove != nil {
				if err := beforeMove(name, previous); err != nil {
					_ = removeExpectedFile(r, tmp, data)
					return false, err
				}
			}
			if err := linkAndRemoveExpected(r, name, previous, expected); err != nil {
				_ = removeExpectedFile(r, tmp, data)
				return false, errors.Join(ErrRecovery, ErrConflict, err)
			}
		}
		if err := linkAndRemoveExpected(r, tmp, name, data); err != nil {
			_ = removeExpectedFile(r, tmp, data)
			if existed {
				return false, errors.Join(ErrRecovery, ErrConflict, err)
			}
			return false, ErrConflict
		}
		if existed {
			if err := removeExpectedFile(r, transactionPath(name, i), expected); err != nil {
				return true, errors.Join(ErrRecovery, err)
			}
		}
		if afterRename != nil {
			if err := afterRename(); err != nil {
				return true, err
			}
		}
		return true, syncDirectory(r, filepath.Dir(name))
	}
	return false, ErrConflict
}
func transactionPath(name string, index int) string {
	return filepath.Join(filepath.Dir(name), fmt.Sprintf(".vgxness-skill-%d.previous", index))
}

// recoverTransactions restores only an orphaned former managed file when the
// target is absent. A replacement at the target is never overwritten; keeping
// both paths is deliberate recovery evidence.
func recoverTransactions(ctx context.Context, r *os.Root, entries map[string][]byte) error {
	for _, identity := range sorted(entries) {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := native(identity)
		for i := 0; i < 128; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			previous := transactionPath(name, i)
			if _, err := r.Lstat(previous); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return err
			}
			if _, err := r.Lstat(name); err == nil {
				return fmt.Errorf("%s and %s remain after interrupted publication", name, previous)
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := restoreWithoutOverwrite(r, previous, name); err != nil {
				return err
			}
		}
	}
	return nil
}

func transactionPending(r *os.Root, name string) (bool, error) {
	for i := 0; i < 128; i++ {
		if _, err := r.Lstat(transactionPath(name, i)); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	return false, nil
}

func restoreWithoutOverwrite(r *os.Root, source, target string) error {
	data, _, err := regular(r, source)
	if err != nil {
		return err
	}
	return linkAndRemoveExpected(r, source, target, data)
}

// linkAndRemoveExpected is the only move primitive: Link fails if the target
// appeared concurrently, and both names must still be the exact same file
// before the managed source is removed.
func linkAndRemoveExpected(r *os.Root, source, target string, expected []byte) error {
	data, sourceInfo, err := regular(r, source)
	if err != nil || !bytes.Equal(data, expected) {
		return errors.Join(ErrRecovery, ErrConflict, err)
	}
	if err := r.Link(source, target); err != nil {
		return err
	}
	linked, targetInfo, linkErr := regular(r, target)
	current, currentInfo, currentErr := regular(r, source)
	if linkErr != nil || currentErr != nil || !bytes.Equal(linked, expected) || !bytes.Equal(current, expected) || !os.SameFile(sourceInfo, targetInfo) || !os.SameFile(sourceInfo, currentInfo) {
		return errors.Join(ErrRecovery, ErrConflict, linkErr, currentErr)
	}
	if err := removeExpectedFile(r, source, expected); err != nil {
		return err
	}
	return syncParents(r, source, target)
}

func removeExpectedFile(r *os.Root, name string, expected []byte) error {
	data, _, err := regular(r, name)
	if err != nil || !bytes.Equal(data, expected) {
		return errors.Join(ErrRecovery, ErrConflict, err)
	}
	if err := r.Remove(name); err != nil {
		return errors.Join(ErrRecovery, err)
	}
	return nil
}

func rollback(r *os.Root, published []original, beforeRollback func(string) error, beforeMove func(string, string) error) error {
	var errs []error
	for i := len(published) - 1; i >= 0; i-- {
		old := published[i]
		current, _, err := regular(r, old.name)
		if err != nil || !bytes.Equal(current, old.published) {
			errs = append(errs, fmt.Errorf("%s changed during rollback", old.name))
			continue
		}
		if beforeRollback != nil {
			if err := beforeRollback(old.name); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		if old.existed {
			_, err := publish(r, old.name, old.data, old.mode, old.published, true, nil, nil, beforeMove)
			errs = append(errs, err)
		} else {
			errs = append(errs, removeExpected(r, old.name, old.published, beforeMove))
		}
	}
	return errors.Join(errs...)
}

func removeExpected(r *os.Root, name string, expected []byte, beforeMove func(string, string) error) error {
	for i := 0; i < 128; i++ {
		previous := transactionPath(name, i)
		if _, err := r.Lstat(previous); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if beforeMove != nil {
			if err := beforeMove(name, previous); err != nil {
				return err
			}
		}
		if err := linkAndRemoveExpected(r, name, previous, expected); err != nil {
			return errors.Join(ErrRecovery, ErrConflict, err)
		}
		if err := removeExpectedFile(r, previous, expected); err != nil {
			return err
		}
		return syncDirectory(r, filepath.Dir(name))
	}
	return ErrRecovery
}

func (s *Service) Uninstall(ctx context.Context, options Options) (Result, error) {
	result, entries, err := s.inspect(options)
	if err != nil {
		return result, err
	}
	if result.State == StateAbsent {
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	r, rootPath, err := openExisting(options)
	if err != nil || r == nil {
		return result, err
	}
	defer r.Close()
	if err := ensureDirectory(ctx, r, ".vgxness-backups"); err != nil {
		return result, err
	}
	session := filepath.Join(".vgxness-backups", "uninstall-0")
	allocated := false
	for i := 0; i < 128; i++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		session = filepath.Join(".vgxness-backups", fmt.Sprintf("uninstall-%d", i))
		if err := r.Mkdir(session, 0o755); errors.Is(err, os.ErrExist) {
			continue
		} else if err != nil {
			return result, err
		}
		if err := syncDirectory(r, filepath.Dir(session)); err != nil {
			if cleanupErr := removeEmptyUninstallSession(r, session, nil); cleanupErr != nil {
				return result, errors.Join(err, ErrRecovery, cleanupErr)
			}
			return result, err
		}
		allocated = true
		break
	}
	if !allocated {
		return result, ErrConflict
	}
	backupRoot := session
	type backup struct {
		name, stored string
		data         []byte
	}
	backups := []backup{}
	fail := func(cause error) (Result, error) {
		var recovery []error
		for i := len(backups) - 1; i >= 0; i-- {
			b := backups[i]
			if s != nil && s.beforeRollback != nil {
				if err := s.beforeRollback(b.stored); err != nil {
					recovery = append(recovery, err)
					continue
				}
			}
			stored, _, err := regular(r, b.stored)
			if err != nil || !bytes.Equal(stored, b.data) {
				recovery = append(recovery, fmt.Errorf("%s backup changed during rollback", b.stored))
				continue
			}
			if _, err := r.Lstat(b.name); err == nil {
				recovery = append(recovery, fmt.Errorf("%s changed during rollback", b.name))
				continue
			}
			if err := restoreWithoutOverwrite(r, b.stored, b.name); err != nil {
				recovery = append(recovery, err)
				continue
			}
		}
		if err := errors.Join(recovery...); err != nil {
			return result, errors.Join(cause, ErrRecovery, err)
		}
		if err := removeEmptyUninstallSession(r, session, entries); err != nil {
			result.BackupPath = filepath.Join(rootPath, backupRoot)
			return result, errors.Join(cause, ErrRecovery, err)
		}
		return result, cause
	}
	for _, identity := range sorted(entries) {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		name := native(identity)
		data, _, err := regular(r, name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fail(err)
		}
		if !bytes.Equal(data, entries[identity]) && !s.predecessor(identity, data) {
			return fail(ErrDrift)
		}
		stored := filepath.Join(backupRoot, native(identity))
		if err := ensureDirectory(ctx, r, filepath.Dir(stored)); err != nil {
			return fail(err)
		}
		if s != nil && s.beforeBackup != nil {
			if err := s.beforeBackup(stored); err != nil {
				return fail(err)
			}
		}
		if err := linkAndRemoveExpected(r, name, stored, data); err != nil {
			return fail(errors.Join(ErrConflict, err))
		}
		backups = append(backups, backup{name, stored, data})
		if s != nil && s.afterPublish != nil {
			if err := s.afterPublish(identity); err != nil {
				return fail(err)
			}
		}
	}
	result.BackupPath = filepath.Join(rootPath, backupRoot)
	if s != nil && s.beforePrune != nil {
		if err := s.beforePrune(); err != nil {
			return result, errors.Join(ErrRecovery, err)
		}
	}
	if err := pruneManagedDirectories(r, entries); err != nil {
		return result, errors.Join(ErrRecovery, err)
	}
	result.State, result.Changed, result.UpdateNeeded = StateAbsent, len(backups) > 0, false
	return result, nil
}

// removeEmptyUninstallSession removes only directories created for a fully
// rolled-back uninstall. Any unexpected content is retained as recovery evidence.
func removeEmptyUninstallSession(r *os.Root, session string, entries map[string][]byte) error {
	dirs, err := uninstallSessionDirectories(session, session)
	if err != nil {
		return err
	}
	for identity := range entries {
		parents, err := uninstallSessionDirectories(filepath.Dir(filepath.Join(session, native(identity))), session)
		if err != nil {
			return err
		}
		dirs = append(dirs, parents...)
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	seen := map[string]bool{}
	for _, name := range dirs {
		if seen[name] {
			continue
		}
		seen[name] = true
		info, err := r.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.Join(ErrRecovery, fmt.Errorf("unexpected uninstall session content at %s", name))
		}
		if err := r.Remove(name); err != nil {
			return errors.Join(ErrRecovery, err)
		}
		if err := syncDirectory(r, filepath.Dir(name)); err != nil {
			return errors.Join(ErrRecovery, err)
		}
	}
	return nil
}

func uninstallSessionDirectories(directory, session string) ([]string, error) {
	directory, session = filepath.Clean(directory), filepath.Clean(session)
	if directory == "." || session == "." || filepath.IsAbs(directory) || filepath.IsAbs(session) {
		return nil, ErrRecovery
	}
	dirs := []string{}
	for {
		dirs = append(dirs, directory)
		if directory == session {
			return dirs, nil
		}
		next := filepath.Dir(directory)
		if next == directory {
			return nil, ErrRecovery
		}
		directory = next
	}
}

func pruneManagedDirectories(r *os.Root, entries map[string][]byte) error {
	dirs := map[string]bool{}
	for identity := range entries {
		name := native(identity)
		parts := strings.Split(filepath.Dir(name), string(filepath.Separator))
		for i := len(parts); i > 0; i-- {
			dirs[filepath.Join(parts[:i]...)] = true
		}
	}
	names := make([]string, 0, len(dirs))
	for n := range dirs {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	var errs []error
	for _, n := range names {
		if err := r.Remove(n); err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrExist) {
			errs = append(errs, err)
			continue
		}
		errs = append(errs, syncDirectory(r, filepath.Dir(n)))
	}
	return errors.Join(errs...)
}

func syncParents(r *os.Root, names ...string) error {
	parents := map[string]bool{}
	for _, name := range names {
		parents[filepath.Dir(name)] = true
	}
	var errs []error
	for parent := range parents {
		errs = append(errs, syncDirectory(r, parent))
	}
	return errors.Join(errs...)
}
func syncDirectory(r *os.Root, name string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	f, err := r.Open(name)
	if err != nil {
		return err
	}
	err = f.Sync()
	return errors.Join(err, f.Close())
}
