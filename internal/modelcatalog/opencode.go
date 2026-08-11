package modelcatalog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

const (
	defaultTimeout        = 15 * time.Second
	defaultMaxStdoutBytes = 1 << 20
	defaultMaxStderrBytes = 64 << 10
	// defaultProcessWaitDelay bounds waiting for descendant-held output pipes
	// after the command exits or its context is cancelled.
	defaultProcessWaitDelay = time.Second
)

var ErrDiscovery = errors.New("model catalog discovery failed")

type ProcessRunner interface {
	Run(ctx context.Context, executable string, args []string, stdout, stderr io.Writer) error
}

type Options struct {
	Timeout        time.Duration
	MaxStdoutBytes int
	MaxStderrBytes int
}

type OpenCode struct {
	executable string
	runner     ProcessRunner
	options    Options
}

func NewOpenCode(executable string, runner ProcessRunner, options Options) *OpenCode {
	if executable == "" {
		executable = "opencode"
	}
	if runner == nil {
		runner = execRunner{}
	}
	if options.Timeout <= 0 {
		options.Timeout = defaultTimeout
	}
	if options.MaxStdoutBytes <= 0 {
		options.MaxStdoutBytes = defaultMaxStdoutBytes
	}
	if options.MaxStderrBytes <= 0 {
		options.MaxStderrBytes = defaultMaxStderrBytes
	}
	return &OpenCode{executable: executable, runner: runner, options: options}
}

func (discovery *OpenCode) Discover(ctx context.Context) (Snapshot, error) {
	return discovery.run(ctx, SourceLocal, []string{"models", "--pure"})
}

func (discovery *OpenCode) Refresh(ctx context.Context) (Snapshot, error) {
	return discovery.run(ctx, SourceRefreshed, []string{"models", "--pure", "--refresh"})
}

func (discovery *OpenCode) run(ctx context.Context, source Source, args []string) (Snapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, discovery.options.Timeout)
	defer cancel()

	stdout := newLimitedBuffer(discovery.options.MaxStdoutBytes)
	stderr := newLimitedBuffer(discovery.options.MaxStderrBytes)
	runErr := discovery.runner.Run(runCtx, discovery.executable, args, stdout, stderr)
	if err := ctx.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %w", ErrDiscovery, err)
	}
	if err := runCtx.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %w", ErrDiscovery, err)
	}
	if runErr != nil || stdout.Exceeded() || stderr.Exceeded() {
		return Snapshot{}, ErrDiscovery
	}

	snapshot, err := parseSnapshot(stdout.Bytes(), source)
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, executable string, args []string, stdout, stderr io.Writer) error {
	command := newExecCommand(ctx, executable, args, stdout, stderr)
	return command.Run()
}

func newExecCommand(ctx context.Context, executable string, args []string, stdout, stderr io.Writer) *exec.Cmd {
	command := exec.CommandContext(ctx, executable, args...)
	command.Stdout = stdout
	command.Stderr = stderr
	command.WaitDelay = defaultProcessWaitDelay
	return command
}

type limitedBuffer struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func newLimitedBuffer(limit int) *limitedBuffer {
	return &limitedBuffer{limit: limit}
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	remaining := buffer.limit - buffer.buffer.Len()
	if remaining < len(data) {
		buffer.exceeded = true
		if remaining > 0 {
			_, _ = buffer.buffer.Write(data[:remaining])
		}
		return len(data), nil
	}
	_, _ = buffer.buffer.Write(data)
	return len(data), nil
}

func (buffer *limitedBuffer) Bytes() []byte {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return bytes.Clone(buffer.buffer.Bytes())
}

func (buffer *limitedBuffer) Exceeded() bool {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.exceeded
}
