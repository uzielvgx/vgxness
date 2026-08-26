package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/vgxness/vgxness/internal/buildinfo"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/inspection"
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/mcp"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/secrets"
	"github.com/vgxness/vgxness/internal/selfinstall"
	setupflow "github.com/vgxness/vgxness/internal/setup"
	"github.com/vgxness/vgxness/internal/skills"
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
	flags.StringVar(&opts.StorageRoot, "storage-root", "", "storage root")
	flags.BoolVar(&opts.ProjectLocal, "project-local", false, "use project-local storage")
	flags.BoolVar(&full, "full", false, "enable explicitly requested local memory mutations")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || launch == nil {
		fmt.Fprintln(stderr, "usage: vgxness mcp [--storage-root <path>] [--project-local] [--full]")
		return 2
	}
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

func RunProductSDDRuntime(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, inspector Inspector, memories MemoryRuntime, opencodeIntegration, codexIntegration integration.Runtime, installer selfinstall.Runtime, setup setupflow.Runtime, sdds SDDRuntime) int {
	if len(args) > 0 && args[0] == "version" {
		return RunVersion(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "memory" {
		return runMemory(ctx, args[1:], stdin, stdout, stderr, memories)
	}
	if len(args) > 0 && args[0] == "sdd" {
		return runSDD(ctx, args[1:], stdin, stdout, stderr, sdds)
	}
	if len(args) > 0 && args[0] == "integrate" {
		return runIntegration(ctx, args[1:], stdout, stderr, opencodeIntegration, codexIntegration)
	}
	if len(args) > 0 && args[0] == "self" {
		return runSelfInstall(ctx, args[1:], stdout, stderr, installer)
	}
	if len(args) > 0 && args[0] == "setup" {
		return runSetup(ctx, args[1:], stdin, stdout, stderr, setup, codexIntegration)
	}
	if len(args) == 0 || (args[0] != "status" && args[0] != "doctor") {
		fmt.Fprintln(stderr, "usage: vgxness <version|status|doctor|tui|memory|sdd|integrate|self|skills|setup>")
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
	case errors.Is(err, selfinstall.ErrNoInstallation):
		return 1, "not_found: no managed self-installation is available"
	case errors.Is(err, selfinstall.ErrStaleGCPlan):
		return 1, "conflict: self-install garbage-collection plan is stale; rerun `vgxness self gc preview`"
	case errors.Is(err, selfinstall.ErrGCRecovery):
		return 1, "recovery: self-install garbage collection is incomplete; run `vgxness self gc recover` without deleting retained evidence"
	case errors.Is(err, integration.ErrRecovery):
		return 1, "recovery: integration rollback failed; inspect managed artifacts and backups"
	case errors.Is(err, skills.ErrRecovery):
		return 1, "recovery: skills rollback failed; inspect managed artifacts and backups"
	case errors.Is(err, selfinstall.ErrRecovery):
		return 1, "recovery: self-install activation is incomplete; run `vgxness self status` and retry `vgxness self install` without deleting retained evidence"
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
	case errors.Is(err, integration.ErrInvalid):
		return 2, "invalid: integration request is invalid"
	case errors.Is(err, integration.ErrConflict):
		return 1, "conflict: integration artifact already exists"
	case errors.Is(err, integration.ErrDrift):
		return 1, "drift: integration artifact differs from the managed version"
	case errors.Is(err, selfinstall.ErrInvalid):
		return 2, "invalid: self-install request is invalid"
	case errors.Is(err, selfinstall.ErrConflict):
		return 1, "conflict: self-install target contains unmanaged content"
	case errors.Is(err, selfinstall.ErrDrift):
		return 1, "drift: managed self-install differs from its manifest"
	case errors.Is(err, skills.ErrInvalid):
		return 2, "invalid: skills request is invalid"
	case errors.Is(err, skills.ErrConflict):
		return 1, "conflict: skills target contains unmanaged content"
	case errors.Is(err, skills.ErrDrift):
		return 1, "drift: managed skills content differs from its bundle"
	case errors.Is(err, selfinstall.ErrNoRollback):
		return 1, "not_found: no previous managed version is available"
	case errors.Is(err, setupflow.ErrInvalid):
		return 2, "invalid: setup request is invalid"
	case errors.Is(err, setupflow.ErrPrerequisite):
		return 1, "unavailable: setup prerequisites are not ready"
	case errors.Is(err, setupflow.ErrVerification):
		return 1, "operational: setup verification failed"
	case errors.Is(err, config.ErrInvalid):
		return 2, "invalid: storage configuration is invalid"
	default:
		return 1, "io: inspection failed"
	}
}
