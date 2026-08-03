package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/vgxness/vgxness/internal/syncpg"
)

const compensationTimeout = 5 * time.Second

type deviceRepository interface {
	IssueDevice(context.Context, string) (syncpg.DeviceCredential, error)
	RevokeDevice(context.Context, uuid.UUID) error
}

var (
	run      = runDevice
	getenv   = os.Getenv
	setup    = defaultSetup
	terminal = func(value any) bool {
		file, ok := value.(interface{ Fd() uintptr })
		return ok && term.IsTerminal(file.Fd())
	}
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func runDevice(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) < 2 || args[0] != "device" {
		return usage(stderr, "usage: vgxness-syncd device <issue|revoke>")
	}
	switch args[1] {
	case "issue":
		return runIssue(ctx, args[2:], stdin, stdout, stderr)
	case "revoke":
		return runRevoke(ctx, args[2:], stderr)
	default:
		return usage(stderr, "usage: vgxness-syncd device <issue|revoke>")
	}
}

func runIssue(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("device issue", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("name", "", "device name")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *name == "" {
		return usage(stderr, "usage: vgxness-syncd device issue --name NAME")
	}
	if !terminal(stdin) || !terminal(stdout) {
		return usage(stderr, "device issue requires terminal stdin and stdout")
	}
	repository, cleanup, ok := configuredRepository(ctx)
	if cleanup != nil {
		defer cleanup()
	}
	if !ok {
		fmt.Fprintln(stderr, "device setup failed")
		return 1
	}
	credential, err := repository.IssueDevice(ctx, *name)
	if err != nil {
		fmt.Fprintln(stderr, "device issue failed")
		return 1
	}
	if ctx.Err() != nil {
		return captureFailure(repository, credential.ID, stderr)
	}
	written, err := io.WriteString(stdout, credential.Bearer+"\n")
	if err == nil && written == len(credential.Bearer)+1 {
		return 0
	}
	return captureFailure(repository, credential.ID, stderr)
}

func captureFailure(repository deviceRepository, id uuid.UUID, stderr io.Writer) int {
	compensation, cancel := context.WithTimeout(context.Background(), compensationTimeout)
	defer cancel()
	if repository.RevokeDevice(compensation, id) == nil {
		fmt.Fprintln(stderr, "device credential capture failed; device revoked")
	} else {
		fmt.Fprintf(stderr, "device credential capture failed; manually revoke device %s\n", id)
	}
	return 1
}

func runRevoke(ctx context.Context, args []string, stderr io.Writer) int {
	if len(args) != 1 {
		return usage(stderr, "usage: vgxness-syncd device revoke CANONICAL_UUID")
	}
	id, err := uuid.Parse(args[0])
	if err != nil || id == uuid.Nil || id.String() != args[0] {
		return usage(stderr, "usage: vgxness-syncd device revoke CANONICAL_UUID")
	}
	repository, cleanup, ok := configuredRepository(ctx)
	if cleanup != nil {
		defer cleanup()
	}
	if !ok {
		fmt.Fprintln(stderr, "device setup failed")
		return 1
	}
	if repository.RevokeDevice(ctx, id) != nil {
		fmt.Fprintln(stderr, "device revoke failed")
		return 1
	}
	return 0
}

func configuredRepository(ctx context.Context) (deviceRepository, func(), bool) {
	dsn := getenv("VGXNESS_SYNC_POSTGRES_DSN")
	ownerText := getenv("VGXNESS_SYNC_OWNER_ID")
	owner, err := uuid.Parse(ownerText)
	if dsn == "" || err != nil || owner == uuid.Nil || owner.String() != ownerText {
		return nil, nil, false
	}
	repository, cleanup, err := setup(ctx, dsn, owner)
	return repository, cleanup, err == nil
}

func defaultSetup(ctx context.Context, dsn string, owner uuid.UUID) (deviceRepository, func(), error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, nil, err
	}
	var closed bool
	cleanup := func() {
		if !closed {
			closed = true
			conn.Close(context.Background())
		}
	}
	if err := syncpg.Migrate(ctx, conn); err != nil {
		return nil, cleanup, err
	}
	repository, err := syncpg.NewRepository(conn, owner)
	if err != nil {
		return nil, cleanup, err
	}
	if err := repository.EnsureOwner(ctx); err != nil {
		return nil, cleanup, err
	}
	return repository, cleanup, nil
}

func usage(stderr io.Writer, message string) int {
	fmt.Fprintln(stderr, message)
	return 2
}
