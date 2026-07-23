package codegraph

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/sensitivepaths"
)

const (
	DefaultMaxOutputBytes = 512 << 10
	defaultDepth          = 3
	defaultMaxFiles       = 8
)

var (
	ErrInvalid     = errors.New("invalid CodeGraph request")
	ErrUnavailable = errors.New("CodeGraph unavailable")
	ErrExecution   = errors.New("CodeGraph execution failed")
)

type Operation string

const (
	Status   Operation = "status"
	Explore  Operation = "explore"
	Impact   Operation = "impact"
	Affected Operation = "affected"
)

type Request struct {
	Operation Operation
	Query     string
	Symbol    string
	Files     []string
	Depth     int
	MaxFiles  int
}

type Result struct {
	Operation    Operation
	Format       string
	Content      string
	OutputSHA256 string
	StartedAt    time.Time
	FinishedAt   time.Time
}

type Runtime interface {
	Query(context.Context, string, Request) (Result, error)
}

type commandResult struct {
	stdout   []byte
	overflow bool
	err      error
}

type commandExecutor interface {
	Run(context.Context, string, []string, string, int) commandResult
}

type Adapter struct {
	executable     string
	maxOutputBytes int
	executor       commandExecutor
	now            func() time.Time
}

func New(executable string) (*Adapter, error) {
	return newAdapter(executable, osCommandExecutor{}, exec.LookPath)
}

func newAdapter(executable string, executor commandExecutor, lookPath func(string) (string, error)) (*Adapter, error) {
	if executor == nil || lookPath == nil {
		return nil, ErrInvalid
	}
	executable = strings.TrimSpace(executable)
	if executable == "" {
		executable = "codegraph"
	}
	resolved, err := lookPath(executable)
	if err != nil {
		return nil, ErrUnavailable
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, ErrUnavailable
	}
	if evaluated, evalErr := filepath.EvalSymlinks(resolved); evalErr == nil {
		resolved = evaluated
	}
	return &Adapter{executable: resolved, maxOutputBytes: DefaultMaxOutputBytes, executor: executor, now: time.Now}, nil
}

func (adapter *Adapter) Query(ctx context.Context, workspace string, request Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if adapter == nil || adapter.executor == nil || adapter.now == nil {
		return Result{}, ErrUnavailable
	}
	root, err := filepath.Abs(workspace)
	if err != nil || strings.TrimSpace(workspace) == "" {
		return Result{}, ErrInvalid
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return Result{}, ErrUnavailable
	}
	indexInfo, err := os.Lstat(filepath.Join(root, ".codegraph"))
	if err != nil || !indexInfo.IsDir() || indexInfo.Mode()&os.ModeSymlink != 0 {
		return Result{}, ErrUnavailable
	}
	args, format, err := commandArguments(root, request)
	if err != nil {
		return Result{}, err
	}
	startedAt := adapter.now().UTC()
	execution := adapter.executor.Run(ctx, adapter.executable, args, root, adapter.maxOutputBytes)
	finishedAt := adapter.now().UTC()
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if execution.err != nil {
		return Result{}, ErrExecution
	}
	if execution.overflow || len(execution.stdout) == 0 || len(execution.stdout) > adapter.maxOutputBytes {
		return Result{}, ErrExecution
	}
	content := strings.TrimSpace(string(execution.stdout))
	if content == "" || format == "json" && !json.Valid([]byte(content)) || sensitivepaths.ContainsSensitiveReference(content, root) {
		return Result{}, ErrExecution
	}
	digest := sha256.Sum256([]byte(content))
	return Result{
		Operation: request.Operation, Format: format, Content: content,
		OutputSHA256: "sha256-" + hex.EncodeToString(digest[:]),
		StartedAt:    startedAt, FinishedAt: finishedAt,
	}, nil
}

func commandArguments(root string, request Request) ([]string, string, error) {
	depth := request.Depth
	if depth == 0 {
		depth = defaultDepth
	}
	maxFiles := request.MaxFiles
	if maxFiles == 0 {
		maxFiles = defaultMaxFiles
	}
	if depth < 1 || depth > 5 || maxFiles < 1 || maxFiles > 12 {
		return nil, "", ErrInvalid
	}
	switch request.Operation {
	case Status:
		if strings.TrimSpace(request.Query) != "" || strings.TrimSpace(request.Symbol) != "" || len(request.Files) != 0 {
			return nil, "", ErrInvalid
		}
		return []string{"status", root, "--json"}, "json", nil
	case Explore:
		query := strings.TrimSpace(request.Query)
		if query == "" || len(query) > 4096 || strings.ContainsRune(query, '\x00') || strings.TrimSpace(request.Symbol) != "" || len(request.Files) != 0 {
			return nil, "", ErrInvalid
		}
		return []string{"explore", "--path", root, "--max-files", strconv.Itoa(maxFiles), query}, "text", nil
	case Impact:
		symbol := strings.TrimSpace(request.Symbol)
		if symbol == "" || len(symbol) > 512 || strings.ContainsRune(symbol, '\x00') || strings.ContainsAny(symbol, "\r\n") || strings.TrimSpace(request.Query) != "" || len(request.Files) != 0 {
			return nil, "", ErrInvalid
		}
		return []string{"impact", "--path", root, "--depth", strconv.Itoa(depth), "--json", symbol}, "json", nil
	case Affected:
		if strings.TrimSpace(request.Query) != "" || strings.TrimSpace(request.Symbol) != "" || len(request.Files) == 0 || len(request.Files) > 32 {
			return nil, "", ErrInvalid
		}
		args := []string{"affected", "--path", root, "--depth", strconv.Itoa(depth), "--json"}
		for _, path := range request.Files {
			if !validRelativePath(path) {
				return nil, "", ErrInvalid
			}
			args = append(args, path)
		}
		return args, "json", nil
	default:
		return nil, "", ErrInvalid
	}
}

func validRelativePath(path string) bool {
	return path != "" && len(path) <= 4096 && !strings.ContainsRune(path, '\x00') &&
		!filepath.IsAbs(path) && filepath.IsLocal(path) && filepath.Clean(path) == path &&
		path != "." && !sensitivepaths.IsSensitive(path)
}

type boundedBuffer struct {
	buffer   bytes.Buffer
	max      int
	overflow bool
}

func (buffer *boundedBuffer) Write(input []byte) (int, error) {
	accepted := len(input)
	remaining := buffer.max - buffer.buffer.Len()
	if remaining <= 0 {
		buffer.overflow = true
		return accepted, nil
	}
	if len(input) > remaining {
		input = input[:remaining]
		buffer.overflow = true
	}
	_, _ = buffer.buffer.Write(input)
	return accepted, nil
}

type osCommandExecutor struct{}

func (osCommandExecutor) Run(ctx context.Context, executable string, args []string, directory string, maxBytes int) commandResult {
	stdout := &boundedBuffer{max: maxBytes}
	stderr := &boundedBuffer{max: 32 << 10}
	command := exec.CommandContext(ctx, executable, args...)
	configureProcessTree(command)
	command.Dir = directory
	command.Env = minimalEnvironment()
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	return commandResult{stdout: append([]byte(nil), stdout.buffer.Bytes()...), overflow: stdout.overflow || stderr.overflow, err: err}
}

func minimalEnvironment() []string {
	environment := []string{"NO_COLOR=1"}
	for _, key := range []string{"PATH", "HOME", "TMPDIR", "LANG", "LC_ALL"} {
		if value := os.Getenv(key); value != "" {
			environment = append(environment, key+"="+value)
		}
	}
	return environment
}

var _ Runtime = (*Adapter)(nil)
