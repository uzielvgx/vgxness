package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/vgxness/vgxness/internal/selfinstall"
)

func runSelfInstall(ctx context.Context, args []string, stdout, stderr io.Writer, runtime selfinstall.Runtime) int {
	if len(args) == 0 || !selfInstallAction(args[0]) {
		fmt.Fprintln(stderr, "usage: vgxness self <preview|install|status|rollback> [--bin-dir PATH] [--data-dir PATH]")
		return 2
	}
	action := args[0]
	flags := flag.NewFlagSet("self "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options selfinstall.Options
	flags.StringVar(&options.BinDir, "bin-dir", "", "stable launcher directory")
	flags.StringVar(&options.DataDir, "data-dir", "", "version data directory")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "invalid self-install arguments")
		return 2
	}
	if runtime == nil {
		fmt.Fprintln(stderr, "operational: self-install runtime is unavailable")
		return 1
	}
	var (
		result selfinstall.Result
		err    error
	)
	switch action {
	case "preview":
		result, err = runtime.Preview(ctx, options)
	case "install":
		result, err = runtime.Install(ctx, options)
	case "status":
		result, err = runtime.Status(ctx, options)
	case "rollback":
		result, err = runtime.Rollback(ctx, options)
	}
	if err != nil {
		code, message := failure(err)
		fmt.Fprintln(stderr, message)
		return code
	}
	var output strings.Builder
	fmt.Fprintf(&output, "state=%s\nlauncher=%s\nmanifest=%s\ndata_dir=%s\nsource_sha256=%s\nactive_sha256=%s\nprevious_sha256=%s\nupdate_available=%t\nrollback_available=%t\nchanged=%t\n",
		result.State, terminalSafe(result.LauncherPath), terminalSafe(result.ManifestPath), terminalSafe(result.DataDir),
		result.SourceSHA256, result.ActiveSHA256, result.PreviousSHA256, result.UpdateAvailable, result.RollbackAvailable, result.Changed,
	)
	_, _ = io.WriteString(stdout, output.String())
	return 0
}

func selfInstallAction(value string) bool {
	return value == "preview" || value == "install" || value == "status" || value == "rollback"
}
