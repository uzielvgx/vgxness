package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/vgxness/vgxness/internal/sensitivepaths"
)

const (
	maxGitCommandBytes  = 128 << 10
	maxGitEvidenceBytes = 192 << 10
)

type gitCommandRunner func(context.Context, string, []string) ([]byte, error)

func collectGitEvidence(ctx context.Context, workspace string) (GitEvidence, error) {
	snapshot, err := prepareGitSnapshot(ctx, workspace)
	if err != nil {
		return GitEvidence{}, err
	}
	defer os.RemoveAll(snapshot.directory)
	return collectGitEvidenceWith(ctx, workspace, snapshot.run)
}

func collectGitEvidenceWith(ctx context.Context, workspace string, run gitCommandRunner) (GitEvidence, error) {
	if run == nil {
		return GitEvidence{}, fmt.Errorf("Git runner is unavailable")
	}
	base := []string{"-c", "color.ui=false", "-c", "core.attributesFile=" + os.DevNull}
	sensitiveGitPathspecs := sensitivepaths.GitExcludePathspecs()
	relationBase := append(append([]string{}, base...), "diff", "--name-status", "-z", "--find-renames", "--find-copies", "--find-copies-harder", "--no-ext-diff", "--no-textconv", "--ignore-submodules=all", "--no-color")
	worktreeRelationsArgs := append(append([]string{}, relationBase...), "--", ".")
	stagedRelationsArgs := append(append([]string{}, relationBase...), "--cached", "--", ".")
	statusArgs := append(append([]string{}, base...), "status", "--short", "--untracked-files=all", "--ignore-submodules=all", "--", ".")
	statusArgs = append(statusArgs, sensitiveGitPathspecs...)
	worktreeArgs := append(append([]string{}, base...), "diff", "--no-ext-diff", "--no-textconv", "--ignore-submodules=all", "--no-color", "--", ".")
	worktreeArgs = append(worktreeArgs, sensitiveGitPathspecs...)
	stagedArgs := append(append([]string{}, base...), "diff", "--cached", "--no-ext-diff", "--no-textconv", "--ignore-submodules=all", "--no-color", "--", ".")
	stagedArgs = append(stagedArgs, sensitiveGitPathspecs...)
	worktreeRelations, err := run(ctx, workspace, worktreeRelationsArgs)
	if err != nil {
		return GitEvidence{}, fmt.Errorf("inspect Git worktree relations: %w", err)
	}
	if err := rejectSensitiveRelations(worktreeRelations); err != nil {
		return GitEvidence{}, err
	}
	stagedRelations, err := run(ctx, workspace, stagedRelationsArgs)
	if err != nil {
		return GitEvidence{}, fmt.Errorf("inspect Git staged relations: %w", err)
	}
	if err := rejectSensitiveRelations(stagedRelations); err != nil {
		return GitEvidence{}, err
	}
	status, err := run(ctx, workspace, statusArgs)
	if err != nil {
		return GitEvidence{}, fmt.Errorf("collect Git status: %w", err)
	}
	worktree, err := run(ctx, workspace, worktreeArgs)
	if err != nil {
		return GitEvidence{}, fmt.Errorf("collect Git worktree diff: %w", err)
	}
	staged, err := run(ctx, workspace, stagedArgs)
	if err != nil {
		return GitEvidence{}, fmt.Errorf("collect Git staged diff: %w", err)
	}
	evidence := GitEvidence{StatusShort: string(status), WorktreeDiff: string(worktree), StagedDiff: string(staged)}
	encoded, err := json.Marshal(evidence)
	if err != nil || len(encoded) > maxGitEvidenceBytes {
		return GitEvidence{}, fmt.Errorf("Git evidence exceeded its bound")
	}
	return evidence, nil
}

func rejectSensitiveRelations(output []byte) error {
	if len(output) == 0 {
		return nil
	}
	if output[len(output)-1] != 0 {
		return fmt.Errorf("Git change relations are malformed")
	}
	parts := bytes.Split(output[:len(output)-1], []byte{0})
	for index := 0; index < len(parts); {
		status := parts[index]
		index++
		if len(status) == 0 {
			return fmt.Errorf("Git change relations are malformed")
		}
		paths := 1
		if status[0] == 'R' || status[0] == 'C' {
			paths = 2
		}
		if index+paths > len(parts) {
			return fmt.Errorf("Git change relations are malformed")
		}
		for _, raw := range parts[index : index+paths] {
			if len(raw) == 0 || !utf8.Valid(raw) {
				return fmt.Errorf("Git change relations are malformed")
			}
			if paths == 2 && sensitivepaths.IsSensitive(filepath.Clean(string(raw))) {
				return fmt.Errorf("Git rename or copy crosses the sensitive-path boundary")
			}
		}
		index += paths
	}
	return nil
}

type gitSnapshot struct {
	directory   string
	environment []string
}

func prepareGitSnapshot(ctx context.Context, workspace string) (gitSnapshot, error) {
	indexPath, err := discoverGitPath(ctx, workspace, "index")
	if err != nil {
		return gitSnapshot{}, err
	}
	objectPath, err := discoverGitPath(ctx, workspace, "objects")
	if err != nil {
		return gitSnapshot{}, err
	}
	if info, statErr := os.Stat(objectPath); statErr != nil || !info.IsDir() {
		return gitSnapshot{}, fmt.Errorf("Git object directory is unavailable")
	}
	directory, err := os.MkdirTemp("", "vgxness-git-snapshot-")
	if err != nil {
		return gitSnapshot{}, fmt.Errorf("create Git snapshot")
	}
	fail := func(cause error) (gitSnapshot, error) { _ = os.RemoveAll(directory); return gitSnapshot{}, cause }
	objectFormat, err := runGitCommand(ctx, workspace, []string{"rev-parse", "--show-object-format"}, cleanGitEnvironment(nil))
	if err != nil {
		return fail(fmt.Errorf("resolve Git object format"))
	}
	format := strings.TrimSpace(string(objectFormat))
	if format != "sha1" && format != "sha256" {
		return fail(fmt.Errorf("unsupported Git object format"))
	}
	if _, err := runGitCommand(ctx, workspace, []string{"init", "--bare", "--quiet", "--object-format=" + format, directory}, cleanGitEnvironment(nil)); err != nil {
		return fail(fmt.Errorf("initialize Git snapshot: %w", err))
	}
	head, headErr := runGitCommand(ctx, workspace, []string{"rev-parse", "--verify", "HEAD"}, cleanGitEnvironment(nil))
	headData := []byte("ref: refs/heads/vgxness-unborn\n")
	if headErr == nil {
		headData = []byte(strings.TrimSpace(string(head)) + "\n")
	} else if _, symbolicErr := runGitCommand(ctx, workspace, []string{"symbolic-ref", "-q", "HEAD"}, cleanGitEnvironment(nil)); symbolicErr != nil {
		return fail(fmt.Errorf("resolve Git HEAD"))
	}
	if err := os.WriteFile(filepath.Join(directory, "HEAD"), headData, 0o600); err != nil {
		return fail(fmt.Errorf("write Git snapshot HEAD"))
	}
	if err := copyGitIndex(indexPath, filepath.Join(directory, "index")); err != nil {
		return fail(err)
	}
	temporaryObjects := filepath.Join(directory, "objects")
	overrides := map[string]string{
		"GIT_DIR": directory, "GIT_COMMON_DIR": directory, "GIT_WORK_TREE": workspace,
		"GIT_INDEX_FILE": filepath.Join(directory, "index"), "GIT_OBJECT_DIRECTORY": temporaryObjects,
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": objectPath, "GIT_ATTR_NOSYSTEM": "1",
		"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_SYSTEM": os.DevNull,
	}
	environment := cleanGitEnvironment(overrides)
	emptyTree, err := runGitCommand(ctx, workspace, []string{"hash-object", "-t", "tree", "-w", "--stdin"}, environment)
	if err != nil {
		return fail(fmt.Errorf("create empty Git attribute source: %w", err))
	}
	if strings.TrimSpace(string(emptyTree)) == "" {
		return fail(fmt.Errorf("create empty Git attribute source"))
	}
	overrides["GIT_ATTR_SOURCE"] = strings.TrimSpace(string(emptyTree))
	environment = cleanGitEnvironment(overrides)
	return gitSnapshot{directory: directory, environment: environment}, nil
}

func (snapshot gitSnapshot) run(ctx context.Context, workspace string, args []string) ([]byte, error) {
	return runGitCommand(ctx, workspace, args, snapshot.environment)
}

func discoverGitPath(ctx context.Context, workspace, name string) (string, error) {
	output, err := runGitCommand(ctx, workspace, []string{"rev-parse", "--git-path", name}, cleanGitEnvironment(nil))
	if err != nil || strings.TrimSpace(string(output)) == "" {
		return "", fmt.Errorf("resolve Git %s path", name)
	}
	path := strings.TrimSpace(string(output))
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	return filepath.Clean(path), nil
}

func copyGitIndex(source, destination string) error {
	info, err := os.Lstat(source)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Git index is not a regular file")
	}
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open Git index")
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create Git index snapshot")
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		return fmt.Errorf("copy Git index snapshot")
	}
	return nil
}

func cleanGitEnvironment(overrides map[string]string) []string {
	if overrides == nil {
		overrides = map[string]string{}
	}
	for name, value := range map[string]string{"GIT_OPTIONAL_LOCKS": "0", "GIT_PAGER": "cat", "GIT_EXTERNAL_DIFF": "", "LC_ALL": "C"} {
		overrides[name] = value
	}
	environment := make([]string, 0, len(os.Environ())+len(overrides)+4)
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		_, replaced := overrides[name]
		if found && (replaced || strings.HasPrefix(name, "GIT_CONFIG") || strings.HasPrefix(name, "GIT_DIR") || strings.HasPrefix(name, "GIT_WORK_TREE") || strings.HasPrefix(name, "GIT_INDEX_FILE") || strings.HasPrefix(name, "GIT_OBJECT_DIRECTORY") || strings.HasPrefix(name, "GIT_COMMON_DIR") || strings.HasPrefix(name, "GIT_ALTERNATE_OBJECT_DIRECTORIES") || strings.HasPrefix(name, "GIT_ATTR_")) {
			continue
		}
		environment = append(environment, entry)
	}
	for name, value := range overrides {
		environment = append(environment, name+"="+value)
	}
	return environment
}

func runGitCommand(ctx context.Context, workspace string, args []string, environment []string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = workspace
	command.Env = environment
	var output cappedBuffer
	output.limit = maxGitCommandBytes
	diagnostic := cappedBuffer{limit: 4096}
	command.Stdout = &output
	command.Stderr = &diagnostic
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(diagnostic.String())
		if detail != "" {
			return nil, fmt.Errorf("bounded Git command failed: %s", detail)
		}
		return nil, fmt.Errorf("bounded Git command failed")
	}
	if output.overflow {
		return nil, fmt.Errorf("bounded Git command output exceeded its limit")
	}
	return bytes.Clone(output.Bytes()), nil
}

type cappedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (buffer *cappedBuffer) Write(value []byte) (int, error) {
	written := len(value)
	remaining := buffer.limit - buffer.Len()
	if remaining <= 0 {
		buffer.overflow = buffer.overflow || written > 0
		return written, nil
	}
	if len(value) > remaining {
		buffer.overflow = true
		value = value[:remaining]
	}
	_, _ = buffer.Buffer.Write(value)
	return written, nil
}
