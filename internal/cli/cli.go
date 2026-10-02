package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/uzielvgx/vgxness/internal/buildinfo"
	"github.com/uzielvgx/vgxness/internal/config"
	"github.com/uzielvgx/vgxness/internal/inspection"
	"github.com/uzielvgx/vgxness/internal/mcp"
	"github.com/uzielvgx/vgxness/internal/memory"
	"github.com/uzielvgx/vgxness/internal/secrets"
)

type Inspector interface {
	Status(context.Context, config.Options) (inspection.Result, error)
	Doctor(context.Context, config.Options) (inspection.Result, error)
}

type mcpLauncher func(context.Context, string, config.Options, bool) error

// RunMCP serves the MCP protocol over the supplied standard streams. Mutations
// require the explicit --full capability flag.
func RunMCP(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, workspace string) int {
	return runMCP(ctx, args, stdin, stdout, stderr, workspace, mcp.RunStdioWithMode)
}

func runMCP(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, workspace string, launch mcpLauncher) int {
	flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var opts config.Options
	var full bool
	var explicitWorkspace string
	flags.StringVar(&opts.StorageRoot, "storage-root", "", "storage root")
	flags.BoolVar(&opts.ProjectLocal, "project-local", false, "use project-local storage")
	flags.BoolVar(&full, "full", false, "enable explicitly requested local memory mutations")
	flags.StringVar(&explicitWorkspace, "workspace", "", "absolute workspace to bind (defaults to CLAUDE_PROJECT_DIR, then the current directory)")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || launch == nil {
		fmt.Fprintln(stderr, "usage: vgxness mcp [--workspace <path>] [--storage-root <path>] [--project-local] [--full]")
		return 2
	}
	workspace = resolveWorkspace(explicitWorkspace, workspace)
	opts.ProjectDir = workspace
	if err := launch(ctx, workspace, opts, full); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			fmt.Fprintln(stderr, "cancelled: operation cancelled")
			return 130
		}
		fmt.Fprintln(stderr, "operational: MCP server unavailable")
		return 1
	}
	return 0
}

func RunProductRuntime(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, inspector Inspector, memories MemoryRuntime) int {
	return withCheckedOutput(stdout, stderr, func(out io.Writer) int {
		return runProductRuntime(ctx, args, stdin, out, stderr, inspector, memories)
	})
}

func runProductRuntime(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, inspector Inspector, memories MemoryRuntime) int {
	if len(args) > 0 && args[0] == "version" {
		return RunVersion(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "memory" {
		return runMemory(ctx, args[1:], stdin, stdout, stderr, memories)
	}
	if len(args) > 0 && args[0] == "claude-code" {
		return runClaudeCode(ctx, args[1:], stdin, stdout, stderr, memories)
	}
	if len(args) == 0 || (args[0] != "status" && args[0] != "doctor") {
		fmt.Fprintln(stderr, "usage: vgxness <version|status|doctor|memory|mcp|claude-code|tui>")
		return 2
	}
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var opts config.Options
	flags.StringVar(&opts.StorageRoot, "storage-root", "", "storage root")
	flags.StringVar(&opts.ProjectDir, "workspace", "", "absolute workspace")
	flags.BoolVar(&opts.ProjectLocal, "project-local", false, "use project-local storage")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "invalid command arguments")
		return 2
	}
	var result inspection.Result
	var err error
	if command == "status" {
		result, err = inspector.Status(ctx, opts)
	} else {
		result, err = inspector.Doctor(ctx, opts)
	}
	if err != nil {
		code, message := failure(err)
		fmt.Fprintln(stderr, message)
		return code
	}
	doctor := ""
	if command == "doctor" {
		doctor = "doctor=healthy\n"
	}
	fmt.Fprintf(stdout, "storage_root=%s\ndatabase=%s\nmigration=%d\n%s", terminalSafe(result.Root), terminalSafe(result.Database), result.Migration, doctor)
	return 0
}

// outputWriter records the first failed write to a command's stdout.
type outputWriter struct {
	w   io.Writer
	err error
}

func (o *outputWriter) Write(p []byte) (int, error) {
	n, err := o.w.Write(p)
	if err != nil && o.err == nil {
		o.err = err
	}
	return n, err
}

// withCheckedOutput turns a successful exit into a failure when command output
// could not be written. Callers such as host hooks depend on that output (for
// example a new session's lease token), so a lost result must not report
// success.
func withCheckedOutput(stdout, stderr io.Writer, run func(io.Writer) int) int {
	out := &outputWriter{w: stdout}
	code := run(out)
	if code == 0 && out.err != nil {
		fmt.Fprintln(stderr, "io: write command output")
		return 1
	}
	return code
}

// RunVersion renders build metadata without requiring any application services.
func RunVersion(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "usage: vgxness version")
		return 2
	}
	if _, err := io.WriteString(stdout, buildinfo.Render(buildinfo.Current())); err != nil {
		fmt.Fprintln(stderr, "io: write version")
		return 1
	}
	return 0
}

func terminalSafe(value string) string {
	var safe strings.Builder
	for _, character := range value {
		switch character {
		case '\n':
			safe.WriteString(`\n`)
		case '\r':
			safe.WriteString(`\r`)
		case '\t':
			safe.WriteString(`\t`)
		default:
			if character < ' ' || character == 0x7f {
				fmt.Fprintf(&safe, `\x%02x`, character)
			} else {
				safe.WriteRune(character)
			}
		}
	}
	return safe.String()
}

func failure(err error) (int, string) {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return 130, "cancelled: operation cancelled"
	case errors.Is(err, memory.ErrInvalid):
		return 2, "invalid: memory request is invalid"
	case errors.Is(err, secrets.ErrUnsupported):
		return 1, "unavailable: credential files are unsupported on this platform"
	case errors.Is(err, memory.ErrConflict):
		return 1, "conflict: memory already exists"
	case errors.Is(err, memory.ErrNotFound):
		return 1, "not_found: memory was not found"
	case errors.Is(err, memory.ErrCorrupt), errors.Is(err, memory.ErrMigration):
		return 1, "operational: memory storage failed"
	case errors.Is(err, inspection.ErrCorrupt):
		return 1, "corrupt: storage inspection failed"
	case errors.Is(err, config.ErrInvalid):
		return 2, "invalid: storage configuration is invalid"
	default:
		return 1, "io: inspection failed"
	}
}
