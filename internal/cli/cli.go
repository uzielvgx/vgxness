package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/inspection"
	"github.com/vgxness/vgxness/internal/memory"
)

type Inspector interface {
	Status(context.Context, config.Options) (inspection.Result, error)
	Doctor(context.Context, config.Options) (inspection.Result, error)
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer, inspector Inspector) int {
	return RunIO(ctx, args, strings.NewReader(""), stdout, stderr, inspector, nil)
}

func RunIO(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, inspector Inspector, memories MemoryRuntime) int {
	if len(args) > 0 && args[0] == "memory" {
		return runMemory(ctx, args[1:], stdin, stdout, stderr, memories)
	}
	if len(args) == 0 || (args[0] != "status" && args[0] != "doctor") {
		fmt.Fprintln(stderr, "usage: vgxness <status|doctor|memory>")
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
	case errors.Is(err, config.ErrInvalid):
		return 2, "invalid: storage configuration is invalid"
	default:
		return 1, "io: inspection failed"
	}
}
