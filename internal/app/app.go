package app

import (
	"context"
	"io"

	"github.com/vgxness/vgxness/internal/cli"
	"github.com/vgxness/vgxness/internal/inspection"
	"github.com/vgxness/vgxness/internal/memory"
)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return cli.Run(ctx, args, stdout, stderr, inspection.Service{Health: memory.HealthFile})
}
