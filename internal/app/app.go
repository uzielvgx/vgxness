// Package app wires the vgxness command line: memory storage, the MCP server,
// the Claude Code hook adapter, storage diagnostics, and the console.
package app

import (
	"context"
	"fmt"
	"io"
	"os"

	appruntime "github.com/uzielvgx/vgxness/internal/app/runtime"
	"github.com/uzielvgx/vgxness/internal/cli"
	"github.com/uzielvgx/vgxness/internal/inspection"
	"github.com/uzielvgx/vgxness/internal/memory"
	"github.com/uzielvgx/vgxness/internal/tui"
)

type mcpLauncher func(context.Context, []string, io.Reader, io.Writer, io.Writer, string) int
type tuiLauncher func(context.Context, io.Reader, io.Writer, io.Writer, tui.Options) int

func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return run(ctx, args, stdin, stdout, stderr, tui.Run, cli.RunMCP)
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, launchTUI tuiLauncher, launchMCP mcpLauncher) int {
	if len(args) > 0 && args[0] == "version" {
		return cli.RunVersion(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "tui" {
		if len(args) != 1 {
			fmt.Fprintln(stderr, "usage: vgxness tui")
			return 2
		}
		if launchTUI == nil {
			fmt.Fprintln(stderr, "operational: console is unavailable")
			return 1
		}
		return launchTUI(ctx, stdin, stdout, stderr, tui.Options{Workspace: mustWorkspace()})
	}
	if len(args) > 0 && args[0] == "mcp" {
		if launchMCP == nil {
			fmt.Fprintln(stderr, "operational: MCP launcher is unavailable")
			return 1
		}
		return launchMCP(ctx, args[1:], stdin, stdout, stderr, mustWorkspace())
	}
	return cli.RunProductRuntime(ctx, args, stdin, stdout, stderr, inspection.Service{Health: memory.HealthFile}, appruntime.NewMemory("cli", false))
}

func mustWorkspace() string {
	workspace, err := os.Getwd()
	if err != nil {
		return "."
	}
	return workspace
}
