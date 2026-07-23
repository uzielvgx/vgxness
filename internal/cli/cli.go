package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/delivery"
	"github.com/vgxness/vgxness/internal/inspection"
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/selfinstall"
	setupflow "github.com/vgxness/vgxness/internal/setup"
)

type Inspector interface {
	Status(context.Context, config.Options) (inspection.Result, error)
	Doctor(context.Context, config.Options) (inspection.Result, error)
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer, inspector Inspector) int {
	return RunIO(ctx, args, strings.NewReader(""), stdout, stderr, inspector, nil)
}

func RunIO(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, inspector Inspector, memories MemoryRuntime) int {
	return RunRuntime(ctx, args, stdin, stdout, stderr, inspector, memories, nil)
}

func RunRuntime(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, inspector Inspector, memories MemoryRuntime, integrations integration.Runtime) int {
	return RunControlPlaneRuntime(ctx, args, stdin, stdout, stderr, inspector, memories, integrations, nil)
}

func RunControlPlaneRuntime(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, inspector Inspector, memories MemoryRuntime, integrations integration.Runtime, controlPlane bridge.Runtime) int {
	return RunAllRuntime(ctx, args, stdin, stdout, stderr, inspector, memories, integrations, controlPlane, nil)
}

func RunAllRuntime(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, inspector Inspector, memories MemoryRuntime, integrations integration.Runtime, controlPlane bridge.Runtime, installer selfinstall.Runtime) int {
	return RunProductRuntime(ctx, args, stdin, stdout, stderr, inspector, memories, integrations, controlPlane, installer, nil, nil)
}

func RunProductRuntime(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, inspector Inspector, memories MemoryRuntime, integrations integration.Runtime, controlPlane bridge.Runtime, installer selfinstall.Runtime, setup setupflow.Runtime, deliveries delivery.Runtime) int {
	if len(args) > 0 && args[0] == "delivery" {
		return runDelivery(ctx, args[1:], stdout, stderr, deliveries)
	}
	if len(args) > 0 && args[0] == "memory" {
		return runMemory(ctx, args[1:], stdin, stdout, stderr, memories)
	}
	if len(args) > 0 && args[0] == "integrate" {
		return runIntegration(ctx, args[1:], stdout, stderr, integrations)
	}
	if len(args) > 0 && args[0] == "bridge" {
		return runBridge(ctx, args[1:], stdin, stdout, stderr, controlPlane)
	}
	if len(args) > 0 && args[0] == "orchestrate" {
		return runOrchestration(ctx, args[1:], stdout, stderr, controlPlane)
	}
	if len(args) > 0 && args[0] == "self" {
		return runSelfInstall(ctx, args[1:], stdout, stderr, installer)
	}
	if len(args) > 0 && args[0] == "setup" {
		return runSetup(ctx, args[1:], stdin, stdout, stderr, setup)
	}
	if len(args) == 0 || (args[0] != "status" && args[0] != "doctor") {
		fmt.Fprintln(stderr, "usage: vgxness <status|doctor|memory|integrate|bridge|orchestrate|self|setup|delivery>")
		return 2
	}
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var opts config.Options
	flags.StringVar(&opts.StorageRoot, "storage-root", "", "storage root")
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
	chronicle := "absent"
	if result.ChroniclePresent {
		chronicle = "present run=" + terminalSafe(result.RunID)
	}
	doctor := ""
	if command == "doctor" {
		doctor = "doctor=healthy\n"
	}
	fmt.Fprintf(stdout, "storage_root=%s\ndatabase=%s\nmigration=%d\nchronicle=%s\n%s", terminalSafe(result.Root), terminalSafe(result.Database), result.Migration, chronicle, doctor)
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
	case errors.Is(err, selfinstall.ErrNoRollback):
		return 1, "not_found: no previous managed version is available"
	case errors.Is(err, setupflow.ErrInvalid):
		return 2, "invalid: setup request is invalid"
	case errors.Is(err, setupflow.ErrPrerequisite):
		return 1, "unavailable: setup prerequisites are not ready"
	case errors.Is(err, setupflow.ErrVerification):
		return 1, "operational: setup verification failed"
	case errors.Is(err, delivery.ErrInvalid):
		return 2, "invalid: delivery request is invalid"
	case errors.Is(err, delivery.ErrNotFound):
		return 1, "not_found: no delivery receipt exists"
	case errors.Is(err, delivery.ErrInvalidated):
		return 1, "invalidated: delivery receipt no longer matches its target"
	case errors.Is(err, delivery.ErrSensitive):
		return 1, "denied: delivery target contains a sensitive path"
	case errors.Is(err, delivery.ErrUnbound):
		return 1, "denied: delivery target contains unbound submodule changes"
	case errors.Is(err, delivery.ErrConflict):
		return 1, "conflict: delivery state conflicts with immutable evidence"
	case errors.Is(err, delivery.ErrCorrupt):
		return 1, "corrupt: delivery state failed verification"
	case errors.Is(err, config.ErrInvalid):
		return 2, "invalid: storage configuration is invalid"
	default:
		return 1, "io: inspection failed"
	}
}
