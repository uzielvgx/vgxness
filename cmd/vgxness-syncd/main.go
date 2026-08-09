package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgxness/vgxness/internal/syncapi"
	"github.com/vgxness/vgxness/internal/syncpg"
	"github.com/vgxness/vgxness/internal/syncservice"
)

const (
	compensationTimeout  = 5 * time.Second
	defaultListenAddress = "127.0.0.1:8787"
)

type deviceRepository interface {
	IssueDevice(context.Context, string) (syncpg.DeviceCredential, error)
	RevokeDevice(context.Context, uuid.UUID) error
}

var (
	run       = runCommand
	getenv    = os.Getenv
	setup     = defaultSetup
	listenTCP = net.Listen
	terminal  = func(value any) bool {
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

func runCommand(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "serve" {
		return runServe(ctx, args[1:], stderr)
	}
	return runDevice(ctx, args, stdin, stdout, stderr)
}

func runServe(ctx context.Context, args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	listen := flags.String("listen", defaultListenAddress, "listen address")
	legacyAllowInsecure := flags.Bool("development-allow-insecure-non-loopback", false, "retired; true is rejected")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "serve arguments are invalid")
		return 2
	}
	if *legacyAllowInsecure {
		fmt.Fprintln(stderr, "development insecure non-loopback override is retired; explicit false is accepted only for compatibility")
		return 2
	}
	if !validListenAddress(*listen) {
		fmt.Fprintln(stderr, "serve requires a literal loopback listen address")
		return 2
	}
	repository, cleanup, ok := configuredServeRepository(ctx)
	if !ok {
		fmt.Fprintln(stderr, "serve setup failed")
		return 1
	}
	defer cleanup()
	listener, err := listenTCP("tcp", *listen)
	if err != nil {
		fmt.Fprintln(stderr, "serve listen failed")
		return 1
	}
	defer listener.Close()
	server := newServer(repositoryAuthenticator{repository}, repositoryBackend{repository}, stderr)
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := server.Shutdown(shutdown)
		cancel()
		if err != nil {
			fmt.Fprintln(stderr, "serve shutdown failed")
			return 1
		}
		<-served
		return 0
	case err := <-served:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(stderr, "serve failed")
			return 1
		}
		return 0
	}
}

func newServer(authenticator syncapi.Authenticator, backend syncapi.SyncBackend, stderr io.Writer) *http.Server {
	return &http.Server{
		Handler:           syncapi.NewSyncServerHandler(authenticator, backend, responseFailureObserver(stderr)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}

func responseFailureObserver(stderr io.Writer) syncapi.FailureObserver {
	var mutex sync.Mutex
	return func(int, error) {
		mutex.Lock()
		defer mutex.Unlock()
		fmt.Fprintln(stderr, "serve response write failed")
	}
}

func validListenAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return false
	}
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type repositoryAuthenticator struct{ repository *syncpg.Repository }

type repositoryBackend struct{ repository *syncpg.Repository }

func (adapter repositoryBackend) Push(ctx context.Context, deviceID uuid.UUID, mutations []syncservice.Mutation) ([]syncservice.Result, error) {
	results, err := adapter.repository.Push(ctx, deviceID, mutations)
	return results, repositoryBackendError(err)
}

func (adapter repositoryBackend) Pull(ctx context.Context, deviceID uuid.UUID, cursor syncservice.Cursor, limit int) (syncservice.PullPage, error) {
	page, err := adapter.repository.Pull(ctx, deviceID, cursor, limit)
	return page, repositoryBackendError(err)
}

func (adapter repositoryBackend) Discover(ctx context.Context, deviceID uuid.UUID) (syncservice.Discovery, error) {
	discovery, err := adapter.repository.Discover(ctx, deviceID)
	return discovery, repositoryBackendError(err)
}

func repositoryBackendError(err error) error {
	if errors.Is(err, syncpg.ErrUnauthenticated) {
		return syncapi.ErrUnauthenticated
	}
	return err
}

func (adapter repositoryAuthenticator) Authenticate(ctx context.Context, bearer string) (syncapi.Identity, error) {
	identity, err := adapter.repository.AuthenticateDevice(ctx, bearer)
	if errors.Is(err, syncpg.ErrUnauthenticated) {
		return syncapi.Identity{}, syncapi.ErrUnauthenticated
	}
	if err != nil {
		return syncapi.Identity{}, err
	}
	return syncapi.Identity{OwnerID: identity.OwnerID, DeviceID: identity.ID}, nil
}

func configuredServeRepository(ctx context.Context) (*syncpg.Repository, func(), bool) {
	dsn, ownerText := getenv("VGXNESS_SYNC_POSTGRES_DSN"), getenv("VGXNESS_SYNC_OWNER_ID")
	owner, err := uuid.Parse(ownerText)
	if dsn == "" || err != nil || owner == uuid.Nil || owner.String() != ownerText {
		return nil, nil, false
	}
	repository, cleanup, err := setupServe(ctx, dsn, owner)
	return repository, cleanup, err == nil
}

func setupServe(ctx context.Context, dsn string, owner uuid.UUID) (*syncpg.Repository, func(), error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, nil, err
	}
	if err = syncpg.Migrate(ctx, conn); err != nil {
		conn.Close(context.Background())
		return nil, nil, err
	}
	if err = conn.Close(context.Background()); err != nil {
		return nil, nil, err
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, nil, err
	}
	repository, err := syncpg.NewRepository(pool, owner)
	if err == nil {
		err = repository.EnsureOwner(ctx)
	}
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	return repository, pool.Close, nil
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
