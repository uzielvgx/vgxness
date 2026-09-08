package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/providers/pi"
	"github.com/vgxness/vgxness/internal/providers/pi/tools"
)

func main() {
	protocol := flag.String("protocol", "", "private protocol")
	workspace := flag.String("workspace", "", "workspace")
	mode := flag.String("mode", "", "mode")
	role := flag.String("role", "", "role")
	storageRoot := flag.String("storage-root", "", "storage root")
	credentialFile := flag.String("credential-file", "", "credential file")
	flag.Parse()
	if *protocol != pi.Protocol {
		fail("protocol mismatch")
	}
	canonicalWorkspace, _, err := pi.CanonicalWorkspace(*workspace)
	if err != nil {
		fail("invalid workspace")
	}
	dispatcher := tools.New(config.Options{ProjectDir: canonicalWorkspace, StorageRoot: *storageRoot, CredentialFile: *credentialFile}, pi.Mode(*mode) == pi.ReadOnly)
	server, err := pi.NewServer(pi.Binding{Workspace: canonicalWorkspace, Mode: pi.Mode(*mode), Role: *role}, 4096, dispatcher.Dispatch)
	if err != nil {
		fail("invalid binding")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() { <-ctx.Done(); _ = os.Stdin.Close() }()
	if err := server.Serve(ctx, os.Stdin, os.Stdout); err != nil {
		fail("protocol failure")
	}
}

func fail(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
