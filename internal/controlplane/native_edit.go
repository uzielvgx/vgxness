package controlplane

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/sensitivepaths"
)

const (
	nativeMaxEdits          = 32
	nativeArchiveMaxFiles   = 100_000
	nativeArchiveMaxBytes   = 512 << 20
	nativeArchiveMaxFile    = 64 << 20
	nativeArchiveDiagnostic = 4096
)

var errNativeSourceWorktreeDirty = errors.New("native write source worktree is not clean")

func prepareNativeEditWorkspace(ctx context.Context, workspace, ticketID string) (*nativeEditWorkspace, error) {
	return prepareNativeEditWorkspaceMode(ctx, workspace, ticketID, true)
}

func prepareNativeEditWorkspaceMode(ctx context.Context, workspace, ticketID string, requireClean bool) (*nativeEditWorkspace, error) {
	top, err := runGitCommand(ctx, workspace, nativeGitArgs("rev-parse", "--show-toplevel"), cleanGitEnvironment(nil))
	if err != nil {
		return nil, fmt.Errorf("%w: write-files requires a Git repository root", bridge.ErrDenied)
	}
	resolvedTop, err := filepath.EvalSymlinks(strings.TrimSpace(string(top)))
	if err != nil || filepath.Clean(resolvedTop) != workspace {
		return nil, fmt.Errorf("%w: write-files requires the canonical Git repository root", bridge.ErrDenied)
	}
	if requireClean {
		status, err := runGitCommand(ctx, workspace, nativeGitArgs("status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=none", "--", "."), cleanGitEnvironment(nil))
		if err != nil {
			return nil, fmt.Errorf("%w: inspect source worktree status", bridge.ErrExecution)
		}
		if len(status) != 0 {
			return nil, fmt.Errorf("%w: %w", bridge.ErrDenied, errNativeSourceWorktreeDirty)
		}
	}
	baseOutput, err := runGitCommand(ctx, workspace, nativeGitArgs("rev-parse", "--verify", "HEAD^{commit}"), cleanGitEnvironment(nil))
	if err != nil {
		return nil, fmt.Errorf("%w: write-files requires an existing HEAD commit", bridge.ErrDenied)
	}
	baseRevision := strings.TrimSpace(string(baseOutput))
	if !validGitObjectID(baseRevision) {
		return nil, fmt.Errorf("%w: invalid Git base revision", bridge.ErrExecution)
	}

	container := filepath.Join(filepath.Dir(workspace), filepath.Base(workspace)+"-worktrees")
	if err := prepareNativeWorktreeDirectory(container); err != nil {
		return nil, err
	}
	worktree := filepath.Join(container, "vgxness-"+ticketID)
	if _, err := os.Lstat(worktree); !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: edit worktree path is unavailable", bridge.ErrDenied)
	}
	worktreeArgs := nativeGitArgs("worktree", "add", "--detach", "--no-checkout", worktree, baseRevision)
	if _, err := runGitCommand(ctx, workspace, worktreeArgs, cleanGitEnvironment(nil)); err != nil {
		return nil, fmt.Errorf("%w: create isolated edit worktree", bridge.ErrExecution)
	}
	created := true
	defer func() {
		if created {
			removeNativeEditWorkspace(workspace, &nativeEditWorkspace{Root: worktree})
		}
	}()
	if _, err := runGitCommand(ctx, worktree, nativeGitArgs("read-tree", "--reset", baseRevision), cleanGitEnvironment(nil)); err != nil {
		return nil, fmt.Errorf("%w: initialize isolated edit index", bridge.ErrExecution)
	}
	if err := materializeNativeGitArchive(ctx, workspace, worktree, baseRevision); err != nil {
		return nil, err
	}
	editStatus, err := runGitCommand(ctx, worktree, nativeGitArgs("status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=none", "--", "."), cleanGitEnvironment(nil))
	if err != nil || len(editStatus) != 0 {
		return nil, fmt.Errorf("%w: isolated edit worktree did not materialize cleanly", bridge.ErrExecution)
	}
	resolvedWorktree, err := filepath.EvalSymlinks(worktree)
	if err != nil || filepath.Clean(resolvedWorktree) != worktree {
		return nil, fmt.Errorf("%w: isolated edit worktree moved outside its container", bridge.ErrDenied)
	}
	info, err := os.Lstat(worktree)
	identity, ok := nativeFileIdentity(info)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !ok {
		return nil, fmt.Errorf("%w: isolated edit worktree identity", bridge.ErrExecution)
	}
	created = false
	return &nativeEditWorkspace{Root: worktree, RootIdentity: identity, BaseRevision: baseRevision}, nil
}

func prepareNativeWorktreeDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0o700); err != nil {
			return fmt.Errorf("%w: create edit worktree container", bridge.ErrExecution)
		}
		parent, openErr := os.Open(filepath.Dir(path))
		if openErr == nil {
			_ = parent.Sync()
			_ = parent.Close()
		}
		info, err = os.Lstat(path)
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: edit worktree container is not a real directory", bridge.ErrDenied)
	}
	return nil
}

func materializeNativeGitArchive(ctx context.Context, repository, worktree, revision string) error {
	archiveContext, cancel := context.WithCancel(ctx)
	defer cancel()
	command := exec.CommandContext(archiveContext, "git", nativeGitArgs("archive", "--format=tar", revision)...)
	command.Dir = repository
	command.Env = cleanGitEnvironment(nil)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("%w: open isolated Git archive", bridge.ErrExecution)
	}
	diagnostic := cappedBuffer{limit: nativeArchiveDiagnostic}
	command.Stderr = &diagnostic
	if err := command.Start(); err != nil {
		return fmt.Errorf("%w: start isolated Git archive", bridge.ErrExecution)
	}
	extractErr := extractNativeGitArchive(worktree, tar.NewReader(stdout))
	if extractErr != nil {
		cancel()
	}
	waitErr := command.Wait()
	if extractErr != nil {
		return extractErr
	}
	if waitErr != nil {
		return fmt.Errorf("%w: materialize isolated Git archive", bridge.ErrExecution)
	}
	return nil
}

func extractNativeGitArchive(worktree string, archive *tar.Reader) error {
	root, err := os.OpenRoot(worktree)
	if err != nil {
		return fmt.Errorf("%w: open isolated edit root", bridge.ErrExecution)
	}
	defer root.Close()
	var files, total int64
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: read isolated Git archive", bridge.ErrExecution)
		}
		files++
		if files > nativeArchiveMaxFiles || header.Size < 0 || header.Size > nativeArchiveMaxFile || total+header.Size > nativeArchiveMaxBytes {
			return fmt.Errorf("%w: isolated Git archive exceeded its bound", bridge.ErrDenied)
		}
		total += header.Size
		name := filepath.FromSlash(strings.TrimSuffix(header.Name, "/"))
		if !validNativeArchivePath(name) {
			return fmt.Errorf("%w: isolated Git archive path", bridge.ErrDenied)
		}
		switch header.Typeflag {
		case tar.TypeXHeader, tar.TypeXGlobalHeader, tar.TypeGNULongName, tar.TypeGNULongLink:
			continue
		case tar.TypeDir:
			if err := secureNativeMkdirAll(root, name, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := secureNativeArchiveFile(root, name, os.FileMode(header.Mode), archive, header.Size); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if strings.ContainsRune(header.Linkname, '\x00') || !utf8.ValidString(header.Linkname) {
				return fmt.Errorf("%w: isolated Git symlink", bridge.ErrDenied)
			}
			if err := root.Symlink(header.Linkname, name); err != nil {
				return fmt.Errorf("%w: create isolated Git symlink", bridge.ErrExecution)
			}
		default:
			return fmt.Errorf("%w: unsupported isolated Git archive entry", bridge.ErrDenied)
		}
	}
	return nil
}

func validNativeArchivePath(name string) bool {
	return name != "" && name != "." && !filepath.IsAbs(name) && filepath.IsLocal(name) &&
		filepath.Clean(name) == name && !strings.ContainsRune(name, '\x00') && utf8.ValidString(name) && !sensitivepaths.IsSensitive(name)
}

func secureNativeMkdirAll(root *os.Root, name string, mode os.FileMode) error {
	current := ""
	for _, component := range strings.Split(name, string(filepath.Separator)) {
		if current == "" {
			current = component
		} else {
			current = filepath.Join(current, component)
		}
		info, err := root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := root.Mkdir(current, mode.Perm()&0o755); err != nil {
				return fmt.Errorf("%w: create isolated Git directory", bridge.ErrExecution)
			}
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%w: isolated Git directory collision", bridge.ErrDenied)
		}
	}
	return nil
}

func secureNativeArchiveFile(root *os.Root, name string, mode os.FileMode, source io.Reader, size int64) error {
	parent := filepath.Dir(name)
	if parent != "." {
		if err := secureNativeMkdirAll(root, parent, 0o755); err != nil {
			return err
		}
	}
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm()&0o755)
	if err != nil {
		return fmt.Errorf("%w: create isolated Git file", bridge.ErrExecution)
	}
	written, copyErr := io.CopyN(file, source, size)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || written != size {
		return fmt.Errorf("%w: write isolated Git file", bridge.ErrExecution)
	}
	return nil
}

func removeNativeEditWorkspace(repository string, edit *nativeEditWorkspace) {
	if edit == nil || edit.Root == "" || filepath.Base(edit.Root) == "." {
		return
	}
	cleanupContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _ = runGitCommand(cleanupContext, repository, nativeGitArgs("worktree", "remove", "--force", edit.Root), cleanGitEnvironment(nil))
}

func (service *Service) EditNative(ctx context.Context, workspace string, input bridge.NativeEditRequest) (bridge.Response, error) {
	if err := bridge.ValidateNativeEdit(input); err != nil {
		return bridge.Response{}, err
	}
	root, paths, document, release, err := service.openNativeTicket(ctx, workspace, input.TicketID)
	if err != nil {
		return bridge.Response{}, err
	}
	defer release()
	if document.State != "prepared" || document.Input.Operation != bridge.WriteFiles && document.Input.Operation != bridge.RepairSystem || document.Input.ChildSessionID != input.ChildSessionID ||
		document.Edit == nil || service.now().UTC().After(parseNativeDeadline(document.Deadline)) || sensitivepaths.IsSensitive(input.Path) ||
		len(document.Edits) >= nativeMaxEdits {
		return bridge.Response{}, bridge.ErrDenied
	}
	leaseGuard, err := acquireOwnedNativeLeaseGuard(paths.Root, document.TicketID)
	if err != nil {
		return bridge.Response{}, err
	}
	defer leaseGuard.Release()
	if crosses, err := nativePathCrossesGitlink(ctx, document.Edit, input.Path); err != nil {
		return bridge.Response{}, err
	} else if crosses {
		return bridge.Response{}, bridge.ErrDenied
	}
	edit, err := secureNativeEdit(document.Edit.Root, document.Edit.RootIdentity, input)
	if err != nil {
		return bridge.Response{}, err
	}
	document.Edits = append(document.Edits, edit)
	if document.Input.Operation == bridge.RepairSystem {
		// A repair may only complete with test and vet evidence collected after
		// its latest content change.
		document.Validations = nil
	}
	if err := writeNativeTicket(paths.Root, document); err != nil {
		return bridge.Response{}, fmt.Errorf("%w: persist native edit receipt", bridge.ErrExecution)
	}
	return bridge.Response{
		ProtocolVersion: bridge.ProtocolVersion, OK: true, Bridge: "healthy", Provider: "opencode", Workspace: root,
		RunID: document.RunID, TaskID: document.TaskID, Status: "editing", Edit: &edit,
	}, nil
}

func secureNativeEdit(workspace, expectedIdentity string, request bridge.NativeEditRequest) (bridge.NativeEditResult, error) {
	beforeRoot, err := os.Lstat(workspace)
	identity, ok := nativeFileIdentity(beforeRoot)
	if err != nil || beforeRoot.Mode()&os.ModeSymlink != 0 || !beforeRoot.IsDir() || !ok || identity != expectedIdentity {
		return bridge.NativeEditResult{}, bridge.ErrDenied
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return bridge.NativeEditResult{}, fmt.Errorf("%w: open edit root", bridge.ErrExecution)
	}
	defer func() { _ = root.Close() }()
	openedRoot, err := root.Stat(".")
	openedIdentity, openedOK := nativeFileIdentity(openedRoot)
	if err != nil || !os.SameFile(beforeRoot, openedRoot) || !openedOK || openedIdentity != expectedIdentity {
		return bridge.NativeEditResult{}, bridge.ErrDenied
	}
	parts := strings.Split(request.Path, string(filepath.Separator))
	for _, component := range parts[:len(parts)-1] {
		before, statErr := root.Lstat(component)
		if statErr != nil || before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
			return bridge.NativeEditResult{}, bridge.ErrDenied
		}
		next, openErr := root.OpenRoot(component)
		if openErr != nil {
			return bridge.NativeEditResult{}, bridge.ErrDenied
		}
		opened, openedErr := next.Stat(".")
		if openedErr != nil || !os.SameFile(before, opened) {
			next.Close()
			return bridge.NativeEditResult{}, bridge.ErrDenied
		}
		root.Close()
		root = next
	}
	name := parts[len(parts)-1]
	var previous string
	mode := os.FileMode(0o644)
	before, statErr := root.Lstat(name)
	if request.Create {
		if !errors.Is(statErr, os.ErrNotExist) {
			return bridge.NativeEditResult{}, bridge.ErrDenied
		}
	} else {
		if statErr != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || !nativeSingleLink(before) || before.Size() > bridge.MaxNativeEditBytes {
			return bridge.NativeEditResult{}, bridge.ErrDenied
		}
		file, openErr := root.Open(name)
		if openErr != nil {
			return bridge.NativeEditResult{}, bridge.ErrDenied
		}
		opened, openStatErr := file.Stat()
		if openStatErr != nil || !os.SameFile(before, opened) || !opened.Mode().IsRegular() || !nativeSingleLink(opened) {
			_ = file.Close()
			return bridge.NativeEditResult{}, bridge.ErrDenied
		}
		data, readErr := io.ReadAll(io.LimitReader(file, bridge.MaxNativeEditBytes+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || len(data) > bridge.MaxNativeEditBytes || !utf8.Valid(data) {
			return bridge.NativeEditResult{}, bridge.ErrDenied
		}
		previous = nativeSHA256(data)
		if previous != request.ExpectedSHA256 {
			return bridge.NativeEditResult{}, bridge.ErrDenied
		}
		mode = before.Mode().Perm()
	}
	if request.Delete {
		current, currentErr := root.Lstat(name)
		if currentErr != nil || !sameNativeFileSnapshot(before, current) || current.Mode()&os.ModeSymlink != 0 || !nativeSingleLink(current) {
			return bridge.NativeEditResult{}, bridge.ErrDenied
		}
		if err := root.Remove(name); err != nil {
			return bridge.NativeEditResult{}, bridge.ErrExecution
		}
		directory, err := root.Open(".")
		if err != nil {
			return bridge.NativeEditResult{}, bridge.ErrExecution
		}
		syncErr := directory.Sync()
		closeErr := directory.Close()
		if syncErr != nil || closeErr != nil {
			return bridge.NativeEditResult{}, bridge.ErrExecution
		}
		if _, err := root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			return bridge.NativeEditResult{}, bridge.ErrDenied
		}
		return bridge.NativeEditResult{
			Path: request.Path, PreviousSHA256: previous, Deleted: true,
		}, nil
	}
	temporary, err := nativeEditTemporaryName()
	if err != nil {
		return bridge.NativeEditResult{}, bridge.ErrExecution
	}
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return bridge.NativeEditResult{}, bridge.ErrExecution
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = root.Remove(temporary)
		}
	}()
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return bridge.NativeEditResult{}, bridge.ErrExecution
	}
	_, writeErr := io.WriteString(file, request.Content)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return bridge.NativeEditResult{}, bridge.ErrExecution
	}
	current, currentErr := root.Lstat(name)
	if request.Create {
		if !errors.Is(currentErr, os.ErrNotExist) {
			return bridge.NativeEditResult{}, bridge.ErrDenied
		}
	} else if currentErr != nil || !sameNativeFileSnapshot(before, current) || current.Mode()&os.ModeSymlink != 0 || !nativeSingleLink(current) {
		return bridge.NativeEditResult{}, bridge.ErrDenied
	}
	if err := root.Rename(temporary, name); err != nil {
		return bridge.NativeEditResult{}, bridge.ErrExecution
	}
	removeTemporary = false
	directory, err := root.Open(".")
	if err != nil {
		return bridge.NativeEditResult{}, bridge.ErrExecution
	}
	syncErr = directory.Sync()
	closeErr = directory.Close()
	if syncErr != nil || closeErr != nil {
		return bridge.NativeEditResult{}, bridge.ErrExecution
	}
	after, err := root.Lstat(name)
	if err != nil || after.Mode()&os.ModeSymlink != 0 || !after.Mode().IsRegular() || !nativeSingleLink(after) {
		return bridge.NativeEditResult{}, bridge.ErrDenied
	}
	return bridge.NativeEditResult{
		Path: request.Path, SHA256: nativeSHA256([]byte(request.Content)), PreviousSHA256: previous,
		Bytes: len(request.Content), Created: request.Create,
	}, nil
}

func sameNativeFileSnapshot(before, current os.FileInfo) bool {
	return before != nil && current != nil && os.SameFile(before, current) && before.Size() == current.Size() &&
		before.Mode() == current.Mode() && before.ModTime().Equal(current.ModTime())
}

func nativePathCrossesGitlink(ctx context.Context, edit *nativeEditWorkspace, path string) (bool, error) {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for index := 1; index < len(parts); index++ {
		prefix := strings.Join(parts[:index], "/")
		output, err := runGitCommand(ctx, edit.Root, nativeGitArgs("ls-tree", "-z", edit.BaseRevision, "--", prefix), cleanGitEnvironment(nil))
		if err != nil {
			return false, fmt.Errorf("%w: inspect Git link boundary", bridge.ErrExecution)
		}
		if bytes.HasPrefix(output, []byte("160000 ")) {
			return true, nil
		}
	}
	return false, nil
}

func finalizeNativeEditArtifact(ctx context.Context, document nativeTicketDocument) (bridge.NativeEditArtifact, error) {
	if document.Edit == nil || len(document.Edits) == 0 || len(document.Edits) > nativeMaxEdits {
		return bridge.NativeEditArtifact{}, bridge.ErrDenied
	}
	info, err := os.Lstat(document.Edit.Root)
	identity, ok := nativeFileIdentity(info)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !ok || identity != document.Edit.RootIdentity {
		return bridge.NativeEditArtifact{}, bridge.ErrDenied
	}
	head, err := runGitCommand(ctx, document.Edit.Root, nativeGitArgs("rev-parse", "--verify", "HEAD^{commit}"), cleanGitEnvironment(nil))
	if err != nil || strings.TrimSpace(string(head)) != document.Edit.BaseRevision {
		return bridge.NativeEditArtifact{}, bridge.ErrDenied
	}
	staged, err := runGitCommand(ctx, document.Edit.Root, nativeGitArgs("diff", "--cached", "--name-only", "-z", "--no-ext-diff", "--no-textconv", "--ignore-submodules=none", "--", "."), cleanGitEnvironment(nil))
	if err != nil || len(staged) != 0 {
		return bridge.NativeEditArtifact{}, bridge.ErrDenied
	}
	status, err := runGitCommand(ctx, document.Edit.Root, nativeGitArgs("status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored=matching", "--ignore-submodules=none", "--", "."), cleanGitEnvironment(nil))
	if err != nil {
		return bridge.NativeEditArtifact{}, fmt.Errorf("%w: inspect isolated edit result", bridge.ErrExecution)
	}
	paths, err := nativeChangedPaths(status)
	if err != nil || len(paths) == 0 {
		return bridge.NativeEditArtifact{}, bridge.ErrDenied
	}
	latest := make(map[string]bridge.NativeEditResult, len(document.Edits))
	created := make(map[string]bool, len(document.Edits))
	original := make(map[string]string, len(document.Edits))
	for _, edit := range document.Edits {
		if _, seen := latest[edit.Path]; !seen {
			original[edit.Path] = edit.PreviousSHA256
			created[edit.Path] = edit.Created
		}
		latest[edit.Path] = edit
	}
	if len(paths) != len(latest) {
		return bridge.NativeEditArtifact{}, bridge.ErrDenied
	}
	changes := make([]bridge.NativeEditResult, 0, len(paths))
	for _, path := range paths {
		expected, present := latest[path]
		if !present || sensitivepaths.IsSensitive(path) {
			return bridge.NativeEditArtifact{}, bridge.ErrDenied
		}
		if expected.Deleted {
			if _, readErr := secureNativeRead(document.Edit.Root, document.Edit.RootIdentity, bridge.NativeReadRequest{Path: path, Limit: bridge.MaxNativeEditBytes}); !errors.Is(readErr, os.ErrNotExist) {
				return bridge.NativeEditArtifact{}, bridge.ErrDenied
			}
			expected.SHA256 = ""
			expected.Bytes = 0
		} else {
			read, readErr := secureNativeRead(document.Edit.Root, document.Edit.RootIdentity, bridge.NativeReadRequest{Path: path, Limit: bridge.MaxNativeEditBytes})
			if readErr != nil || read.Truncated || nativeSHA256([]byte(read.Content)) != expected.SHA256 {
				return bridge.NativeEditArtifact{}, bridge.ErrDenied
			}
			expected.Bytes = len(read.Content)
		}
		expected.Created = created[path]
		expected.PreviousSHA256 = original[path]
		changes = append(changes, expected)
	}
	manifest, err := json.Marshal(struct {
		BaseRevision string                    `json:"baseRevision"`
		Changes      []bridge.NativeEditResult `json:"changes"`
	}{BaseRevision: document.Edit.BaseRevision, Changes: changes})
	if err != nil {
		return bridge.NativeEditArtifact{}, bridge.ErrExecution
	}
	return bridge.NativeEditArtifact{
		Worktree: document.Edit.Root, BaseRevision: document.Edit.BaseRevision, Changes: changes,
		ManifestSHA: nativeSHA256(manifest),
	}, nil
}

func nativeChangedPaths(status []byte) ([]string, error) {
	if len(status) == 0 {
		return nil, nil
	}
	if status[len(status)-1] != 0 {
		return nil, bridge.ErrDenied
	}
	records := bytes.Split(status[:len(status)-1], []byte{0})
	paths := make([]string, 0, len(records))
	for _, record := range records {
		if len(record) < 4 || record[2] != ' ' {
			return nil, bridge.ErrDenied
		}
		state := string(record[:2])
		if state != " M" && state != " D" && state != "??" {
			return nil, bridge.ErrDenied
		}
		path := filepath.Clean(string(record[3:]))
		if !utf8.ValidString(path) || !filepath.IsLocal(path) || path == "." || sensitivepaths.IsSensitive(path) {
			return nil, bridge.ErrDenied
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func nativeEditTemporaryName() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return ".vgxness-edit-" + hex.EncodeToString(value[:]) + ".tmp", nil
}

func nativeSHA256(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256-" + hex.EncodeToString(digest[:])
}

func validGitObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func nativeGitArgs(args ...string) []string {
	prefix := []string{
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "core.fsmonitor=false",
		"-c", "core.untrackedCache=false",
	}
	return append(prefix, args...)
}
