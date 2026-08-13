package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/vgxness/vgxness/internal/skillregistry"
)

// RunSkillRegistry lists discovered skills and their current registry status.
func RunSkillRegistry(ctx context.Context, args []string, stdout, stderr io.Writer, workspace string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: vgxness skill-registry <list|status|refresh> [--host common|codex|opencode]")
		return 2
	}
	command := args[0]
	flags := flag.NewFlagSet("skill-registry "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	host := flags.String("host", "common", "skill host")
	if (command != "list" && command != "status" && command != "refresh") || flags.Parse(args[1:]) != nil || flags.NArg() != 0 || !validSkillHost(*host) {
		fmt.Fprintln(stderr, "usage: vgxness skill-registry <list|status|refresh> [--host common|codex|opencode]")
		return 2
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		fmt.Fprintln(stderr, "operational: skill registry unavailable")
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, "operational: skill registry unavailable")
		return 1
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		fmt.Fprintln(stderr, "operational: skill registry unavailable")
		return 1
	}
	opts := skillregistry.Options{CWD: abs, Home: home, Host: *host, CachePath: filepath.Join(cache, "vgxness", "skill-registry", skillregistry.CacheKey(abs, *host)+".json")}
	var snapshot skillregistry.Snapshot
	if command == "refresh" {
		snapshot, err = skillregistry.Refresh(ctx, opts)
	} else {
		snapshot, err = skillregistry.Scan(ctx, opts)
	}
	if err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(stderr, "cancelled: operation cancelled")
			return 130
		}
		fmt.Fprintln(stderr, "operational: skill registry unavailable")
		return 1
	}
	if command == "status" {
		fmt.Fprintf(stdout, "status=%s\ndigest=%s\ncandidates=%d\nfrom_cache=%t\n", snapshot.Status, snapshot.Digest, len(snapshot.Candidates), snapshot.FromCache)
		for _, root := range snapshot.Roots {
			fmt.Fprintf(stdout, "root path=%s scope=%s source=%s status=%s\n", terminalSafe(root.Path), terminalSafe(root.Scope), terminalSafe(root.Source), terminalSafe(root.Status))
		}
		return 0
	}
	for _, candidate := range snapshot.Candidates {
		fmt.Fprintf(stdout, "name=%s path=%s scope=%s source=%s sha256=%s\n", terminalSafe(candidate.Name), terminalSafe(candidate.LogicalPath), terminalSafe(candidate.Scope), terminalSafe(candidate.Source), candidate.SHA256)
	}
	return 0
}

func validSkillHost(host string) bool {
	return host == "common" || host == "codex" || host == "opencode"
}
