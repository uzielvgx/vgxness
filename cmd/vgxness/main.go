package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/vgxness/vgxness/internal/app"
	"github.com/vgxness/vgxness/internal/launcher"
)

func main() {
	if handled, code := launcher.Forward(os.Args, os.Environ(), os.Stderr); handled {
		os.Exit(code)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(app.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
