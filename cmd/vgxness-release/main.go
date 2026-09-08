package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/vgxness/vgxness/internal/release"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(os.Args) > 1 && os.Args[1] == "pi" {
		os.Exit(release.RunPi(ctx, os.Args[2:], os.Stdout, os.Stderr))
	}
	os.Exit(release.Run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
