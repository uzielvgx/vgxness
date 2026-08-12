package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"sort"

	"github.com/vgxness/vgxness/internal/skills"
)

func RunSkills(ctx context.Context, args []string, stdout, stderr io.Writer, runtime skills.Runtime) int {
	if len(args) == 0 || runtime == nil {
		fmt.Fprintln(stderr, "usage: vgxness skills <preview|install|status|uninstall> [--skills-dir PATH]")
		return 2
	}
	command := args[0]
	flags := flag.NewFlagSet("skills "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options skills.Options
	var compatibility bool
	flags.StringVar(&options.Dir, "skills-dir", "", "absolute shared skills directory")
	flags.BoolVar(&compatibility, "compatibility", false, "inventory declared legacy portable skills (status only)")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || compatibility && command != "status" {
		fmt.Fprintln(stderr, "invalid skills arguments")
		return 2
	}
	if compatibility {
		report, err := compatibilityReport(ctx, options, runtime)
		if err != nil {
			code, message := failure(err)
			fmt.Fprintln(stderr, message)
			return code
		}
		for _, entry := range report {
			fmt.Fprintf(stdout, "compatibility[%s]=%s sha256=%s\n", terminalSafe(entry.Path), entry.State, entry.Digest)
		}
		return 0
	}
	var result skills.Result
	var err error
	switch command {
	case "preview":
		result, err = runtime.Preview(ctx, options)
	case "install":
		result, err = runtime.Install(ctx, options)
	case "status":
		result, err = runtime.Status(ctx, options)
	case "uninstall":
		result, err = runtime.Uninstall(ctx, options)
	default:
		fmt.Fprintln(stderr, "usage: vgxness skills <preview|install|status|uninstall> [--skills-dir PATH]")
		return 2
	}
	if err != nil {
		code, message := failure(err)
		fmt.Fprintln(stderr, message)
		return code
	}
	fmt.Fprintf(stdout, "state=%s\npath=%s\nfiles=%d\nchanged=%t\nupdate_needed=%t\n", result.State, terminalSafe(result.Path), result.FileCount, result.Changed, result.UpdateNeeded)
	if result.BackupPath != "" {
		fmt.Fprintf(stdout, "backup_path=%s\n", terminalSafe(result.BackupPath))
	}
	names := make([]string, 0, len(result.Hashes))
	for name := range result.Hashes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(stdout, "sha256[%s]=%s\n", name, result.Hashes[name])
	}
	return 0
}

type compatibilityRuntime interface {
	Compatibility(context.Context, skills.Options) ([]skills.CompatibilityEntry, error)
}

func compatibilityReport(ctx context.Context, options skills.Options, runtime skills.Runtime) ([]skills.CompatibilityEntry, error) {
	compatible, ok := runtime.(compatibilityRuntime)
	if !ok {
		return nil, skills.ErrInvalid
	}
	return compatible.Compatibility(ctx, options)
}
