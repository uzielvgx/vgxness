package delivery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/vgxness/vgxness/internal/sensitivepaths"
)

const maxGitOutput = 256 << 10

func captureTarget(ctx context.Context, workspace, baseRef string) (TargetSnapshot, error) {
	root, err := gitText(ctx, workspace, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return TargetSnapshot{}, fmt.Errorf("%w: workspace is not a Git repository", ErrInvalid)
	}
	root, err = filepath.Abs(root)
	if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
		root = resolved
	}
	if err != nil || filepath.Clean(root) != filepath.Clean(workspace) {
		return TargetSnapshot{}, fmt.Errorf("%w: workspace must be the Git project root", ErrInvalid)
	}
	if err := rejectDirtySubmodules(ctx, workspace); err != nil {
		return TargetSnapshot{}, err
	}
	if baseRef == "" {
		baseRef = "HEAD"
	}
	if strings.HasPrefix(baseRef, "-") || strings.ContainsAny(baseRef, "\x00\r\n") || len(baseRef) > 512 {
		return TargetSnapshot{}, fmt.Errorf("%w: invalid base reference", ErrInvalid)
	}
	baseRevision, err := gitText(ctx, workspace, nil, "rev-parse", "--verify", "--end-of-options", baseRef+"^{commit}")
	if err != nil {
		return TargetSnapshot{}, fmt.Errorf("%w: base reference is not a commit", ErrInvalid)
	}
	baseTree, err := gitText(ctx, workspace, nil, "rev-parse", "--verify", baseRevision+"^{tree}")
	if err != nil {
		return TargetSnapshot{}, fmt.Errorf("capture base tree: %w", err)
	}

	temporary, err := os.MkdirTemp("", "vgxness-delivery-target-")
	if err != nil {
		return TargetSnapshot{}, fmt.Errorf("create delivery target snapshot: %w", err)
	}
	defer os.RemoveAll(temporary)
	objectFormat, err := gitText(ctx, workspace, nil, "rev-parse", "--show-object-format")
	if err != nil || objectFormat != "sha1" && objectFormat != "sha256" {
		return TargetSnapshot{}, fmt.Errorf("resolve Git object format")
	}
	if _, err := gitRun(ctx, workspace, nil, "init", "--bare", "--quiet", "--object-format="+objectFormat, temporary); err != nil {
		return TargetSnapshot{}, fmt.Errorf("initialize delivery target snapshot: %w", err)
	}
	objects, err := gitText(ctx, workspace, nil, "rev-parse", "--git-path", "objects")
	if err != nil {
		return TargetSnapshot{}, fmt.Errorf("resolve Git objects: %w", err)
	}
	if !filepath.IsAbs(objects) {
		objects = filepath.Join(workspace, objects)
	}
	env := cleanGitEnv(map[string]string{
		"GIT_DIR": temporary, "GIT_COMMON_DIR": temporary, "GIT_WORK_TREE": workspace,
		"GIT_INDEX_FILE":                   filepath.Join(temporary, "index"),
		"GIT_OBJECT_DIRECTORY":             filepath.Join(temporary, "objects"),
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": filepath.Clean(objects),
		"GIT_ATTR_NOSYSTEM":                "1", "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_SYSTEM": os.DevNull,
	})
	if _, err := gitRun(ctx, workspace, env, "read-tree", baseRevision); err != nil {
		return TargetSnapshot{}, fmt.Errorf("seed delivery target: %w", err)
	}
	if _, err := gitRun(ctx, workspace, env, "-c", "core.attributesFile="+os.DevNull, "add", "-A", "--", "."); err != nil {
		return TargetSnapshot{}, fmt.Errorf("project delivery target: %w", err)
	}
	candidateTree, err := gitText(ctx, workspace, env, "write-tree")
	if err != nil {
		return TargetSnapshot{}, fmt.Errorf("write delivery target tree: %w", err)
	}
	relations, err := gitRun(ctx, workspace, env, "diff-tree", "-r", "--name-status", "-z", "--no-commit-id", baseTree, candidateTree)
	if err != nil {
		return TargetSnapshot{}, fmt.Errorf("list delivery target paths: %w", err)
	}
	paths, err := changedPaths(relations)
	if err != nil {
		return TargetSnapshot{}, err
	}
	encoded := strings.Join(paths, "\x00")
	digest := sha256.Sum256([]byte(encoded))
	return TargetSnapshot{
		BaseRevision: baseRevision, BaseTree: baseTree, CandidateTree: candidateTree,
		Paths: paths, PathsSHA256: hex.EncodeToString(digest[:]),
	}, nil
}

func changedPaths(output []byte) ([]string, error) {
	if len(output) == 0 {
		return []string{}, nil
	}
	if output[len(output)-1] != 0 {
		return nil, fmt.Errorf("%w: malformed Git path relation", ErrCorrupt)
	}
	parts := bytes.Split(output[:len(output)-1], []byte{0})
	seen := map[string]struct{}{}
	for index := 0; index < len(parts); {
		status := parts[index]
		index++
		if len(status) == 0 {
			return nil, fmt.Errorf("%w: malformed Git path relation", ErrCorrupt)
		}
		count := 1
		if status[0] == 'R' || status[0] == 'C' {
			count = 2
		}
		if index+count > len(parts) {
			return nil, fmt.Errorf("%w: malformed Git path relation", ErrCorrupt)
		}
		for _, raw := range parts[index : index+count] {
			if len(raw) == 0 || !utf8.Valid(raw) {
				return nil, fmt.Errorf("%w: malformed Git path relation", ErrCorrupt)
			}
			path := filepath.ToSlash(filepath.Clean(string(raw)))
			if sensitivepaths.IsSensitive(path) {
				return nil, fmt.Errorf("%w: %s", ErrSensitive, path)
			}
			seen[path] = struct{}{}
		}
		index += count
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func rejectDirtySubmodules(ctx context.Context, workspace string) error {
	status, err := gitRun(ctx, workspace, nil, "status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignore-submodules=none", "--", ".")
	if err != nil {
		return fmt.Errorf("inspect submodule scope: %w", err)
	}
	records := bytes.Split(status, []byte{0})
	for index := 0; index < len(records); index++ {
		record := records[index]
		if len(record) == 0 || record[0] != '1' && record[0] != '2' && record[0] != 'u' {
			continue
		}
		fields := bytes.Fields(record)
		if len(fields) < 3 {
			return fmt.Errorf("%w: malformed Git status", ErrCorrupt)
		}
		submodule := fields[2]
		if len(submodule) == 4 && submodule[0] == 'S' && (submodule[2] != '.' || submodule[3] != '.') {
			return fmt.Errorf("%w: dirty submodule content is outside the superproject tree", ErrUnbound)
		}
		if record[0] == '2' {
			if index+1 >= len(records) || len(records[index+1]) == 0 {
				return fmt.Errorf("%w: malformed Git rename status", ErrCorrupt)
			}
			index++
		}
	}
	return nil
}

func gitText(ctx context.Context, workspace string, env []string, args ...string) (string, error) {
	output, err := gitRun(ctx, workspace, env, args...)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(output))
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf("Git returned an invalid identity")
	}
	return value, nil
}

func gitRun(ctx context.Context, workspace string, env []string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = workspace
	if env == nil {
		env = cleanGitEnv(nil)
	}
	command.Env = env
	var stdout, stderr boundedBuffer
	stdout.limit = maxGitOutput
	stderr.limit = 4096
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("bounded Git command failed: %s", strings.TrimSpace(stderr.String()))
	}
	if stdout.overflow {
		return nil, fmt.Errorf("bounded Git command output exceeded its limit")
	}
	return bytes.Clone(stdout.Bytes()), nil
}

func cleanGitEnv(overrides map[string]string) []string {
	if overrides == nil {
		overrides = map[string]string{}
	}
	defaults := map[string]string{"GIT_OPTIONAL_LOCKS": "0", "GIT_PAGER": "cat", "GIT_EXTERNAL_DIFF": "", "LC_ALL": "C"}
	for key, value := range defaults {
		overrides[key] = value
	}
	result := make([]string, 0, len(os.Environ())+len(overrides))
	for _, item := range os.Environ() {
		name, _, ok := strings.Cut(item, "=")
		_, replaced := overrides[name]
		if ok && (replaced || strings.HasPrefix(name, "GIT_")) {
			continue
		}
		result = append(result, item)
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	return result
}

type boundedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (buffer *boundedBuffer) Write(value []byte) (int, error) {
	requested := len(value)
	remaining := buffer.limit - buffer.Len()
	if remaining <= 0 {
		buffer.overflow = buffer.overflow || requested > 0
		return requested, nil
	}
	if len(value) > remaining {
		buffer.overflow = true
		value = value[:remaining]
	}
	_, _ = buffer.Buffer.Write(value)
	return requested, nil
}
