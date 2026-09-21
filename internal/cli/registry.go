package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/skillregistry"
)

// RegistryRuntime is the bounded skill-registry surface used by the CLI. It
// returns metadata only: no skill bodies are ever loaded through this path.
type RegistryRuntime interface {
	Ensure(context.Context, skillregistry.Options) (skillregistry.Registry, error)
	Refresh(context.Context, skillregistry.Options) (skillregistry.Registry, error)
	Load(context.Context, skillregistry.Options) (skillregistry.Registry, bool, error)
	Search(context.Context, skillregistry.Options, string, int) (skillregistry.Registry, error)
	Resolve(context.Context, skillregistry.Options, string) (skillregistry.Entry, error)
	Unlock(context.Context, skillregistry.Options) (bool, error)
}

const registryUsage = "usage: vgxness skills registry <status|ensure|refresh|search|resolve|unlock> [--workspace PATH] [--home PATH] [--cache-path PATH] [--max-age DURATION] [--root ID=PATH] [--query Q] [--name ID] [--limit N] [--json]"

const maxRegistryDescriptionBytes = 512

type registryRootFlag []skillregistry.RootSpec

func (roots *registryRootFlag) String() string { return "" }

func (roots *registryRootFlag) Set(value string) error {
	id, path, found := strings.Cut(value, "=")
	id, path = strings.TrimSpace(id), strings.TrimSpace(path)
	if !found || id == "" || path == "" || strings.ContainsAny(id, " \t") {
		return fmt.Errorf("use --root ID=PATH")
	}
	for _, existing := range *roots {
		if existing.ID == id {
			return fmt.Errorf("duplicate root id")
		}
	}
	if len(*roots) >= skillregistry.DefaultMaxRoots {
		return fmt.Errorf("too many roots")
	}
	*roots = append(*roots, skillregistry.RootSpec{ID: id, Path: path, Scope: skillregistry.ScopeConfigured, Provenance: "cli"})
	return nil
}

// RunSkillRegistry implements `vgxness skills registry ...`. It never prints
// skill bodies and never presents a cache it could not verify as fresh.
func RunSkillRegistry(ctx context.Context, args []string, stdout, stderr io.Writer, runtime RegistryRuntime) int {
	if runtime == nil {
		fmt.Fprintln(stderr, "unavailable: skill registry runtime is unavailable")
		return 1
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, registryUsage)
		return 2
	}
	command := args[0]
	if command != "status" && command != "ensure" && command != "refresh" && command != "search" && command != "resolve" && command != "unlock" {
		fmt.Fprintln(stderr, registryUsage)
		return 2
	}
	flags := flag.NewFlagSet("skills registry "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var workspace, home, cachePath, query, name string
	var maxAge time.Duration
	var limit int
	var jsonOutput bool
	var roots registryRootFlag
	flags.StringVar(&workspace, "workspace", "", "absolute workspace bound to the registry")
	flags.StringVar(&home, "home", "", "home directory override")
	flags.StringVar(&cachePath, "cache-path", "", "absolute cache file outside the repository")
	flags.DurationVar(&maxAge, "max-age", 0, "freshness window")
	flags.StringVar(&query, "query", "", "search query")
	flags.StringVar(&name, "name", "", "entry id or name")
	flags.IntVar(&limit, "limit", 0, "maximum results")
	flags.BoolVar(&jsonOutput, "json", false, "emit JSON")
	flags.Var(&roots, "root", "ID=PATH; repeat to replace the default global+project roots")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, registryUsage)
		return 2
	}
	options := skillregistry.Options{Workspace: workspace, HomeDir: home, CachePath: cachePath, MaxAge: maxAge}
	if len(roots) > 0 {
		options.Roots = roots
	}
	cachePathResolved, cacheErr := skillregistry.CachePath(options)
	if cacheErr != nil {
		code, message := failure(cacheErr)
		fmt.Fprintln(stderr, message)
		return code
	}

	switch command {
	case "status":
		registry, usable, err := runtime.Load(ctx, options)
		if err != nil {
			code, message := failure(err)
			fmt.Fprintln(stderr, message)
			return code
		}
		if jsonOutput {
			return encodeRegistry(stdout, registry, usable, cachePathResolved)
		}
		present := registry.SchemaVersion == skillregistry.SchemaVersion
		fmt.Fprintf(stdout, "cache_state=%s\nfresh=%t\ncache_path=%s\n", presentState(present), usable, terminalSafe(cachePathResolved))
		if present {
			writeRegistrySummary(stdout, registry)
		}
		return 0
	case "ensure":
		registry, err := runtime.Ensure(ctx, options)
		if err != nil {
			code, message := failure(err)
			fmt.Fprintln(stderr, message)
			return code
		}
		if jsonOutput {
			return encodeRegistry(stdout, registry, true, cachePathResolved)
		}
		fmt.Fprintf(stdout, "state=ready\ncache_path=%s\n", terminalSafe(cachePathResolved))
		writeRegistrySummary(stdout, registry)
		return 0
	case "refresh":
		registry, err := runtime.Refresh(ctx, options)
		if err != nil {
			code, message := failure(err)
			fmt.Fprintln(stderr, message)
			return code
		}
		if jsonOutput {
			return encodeRegistry(stdout, registry, true, cachePathResolved)
		}
		fmt.Fprintf(stdout, "state=refreshed\ncache_path=%s\n", terminalSafe(cachePathResolved))
		writeRegistrySummary(stdout, registry)
		return 0
	case "unlock":
		recovered, err := runtime.Unlock(ctx, options)
		if err != nil {
			code, message := failure(err)
			fmt.Fprintln(stderr, message)
			return code
		}
		if jsonOutput {
			_ = json.NewEncoder(stdout).Encode(struct {
				SchemaVersion int  `json:"schemaVersion"`
				Recovered     bool `json:"recovered"`
			}{SchemaVersion: skillregistry.SchemaVersion, Recovered: recovered})
			return 0
		}
		fmt.Fprintf(stdout, "recovered=%t\ncache_path=%s\n", recovered, terminalSafe(cachePathResolved))
		return 0
	case "search":
		registry, err := runtime.Search(ctx, options, query, limit)
		if err != nil {
			code, message := failure(err)
			fmt.Fprintln(stderr, message)
			return code
		}
		if jsonOutput {
			return encodeRegistry(stdout, registry, true, cachePathResolved)
		}
		for _, entry := range registry.Entries {
			fmt.Fprintf(stdout, "entry[id=%s name=%s status=%s sha256=%s path=%s]\n", terminalSafe(entry.ID), terminalSafe(entry.Name), entry.Status, entry.SHA256, terminalSafe(entry.Path))
		}
		return 0
	default: // resolve
		entry, err := runtime.Resolve(ctx, options, name)
		if err != nil {
			code, message := failure(err)
			fmt.Fprintln(stderr, message)
			return code
		}
		if jsonOutput {
			_ = json.NewEncoder(stdout).Encode(struct {
				SchemaVersion int                 `json:"schemaVersion"`
				Entry         skillregistry.Entry `json:"entry"`
			}{SchemaVersion: skillregistry.SchemaVersion, Entry: entry})
			return 0
		}
		fmt.Fprintf(stdout, "id=%s\nname=%s\nsha256=%s\npath=%s\n", terminalSafe(entry.ID), terminalSafe(entry.Name), entry.SHA256, terminalSafe(entry.Path))
		if entry.Description != "" {
			fmt.Fprintf(stdout, "description=%s\n", terminalSafe(boundText(entry.Description, maxRegistryDescriptionBytes)))
		}
		return 0
	}
}

func writeRegistrySummary(stdout io.Writer, registry skillregistry.Registry) {
	duplicates, diagnostics := 0, len(registry.Diagnostics)
	for _, entry := range registry.Entries {
		if entry.Status == skillregistry.StatusDuplicate {
			duplicates++
		}
	}
	fmt.Fprintf(stdout, "schema_version=%d\ngenerated_at=%s\nworkspace=%s\nroots=%d\nentries=%d\nduplicates=%d\ndiagnostics=%d\ncomplete=%t\n", registry.SchemaVersion, registry.GeneratedAt, terminalSafe(registry.Workspace), len(registry.Roots), len(registry.Entries), duplicates, diagnostics, registry.Complete)
	for _, root := range registry.Roots {
		fmt.Fprintf(stdout, "root[id=%s scope=%s present=%t path=%s]\n", terminalSafe(root.ID), root.Scope, root.Present, terminalSafe(root.Path))
	}
}

func encodeRegistry(stdout io.Writer, registry skillregistry.Registry, usable bool, cachePath string) int {
	_ = json.NewEncoder(stdout).Encode(struct {
		SchemaVersion int                    `json:"schemaVersion"`
		Usable        bool                   `json:"usable"`
		CachePath     string                 `json:"cachePath"`
		Registry      skillregistry.Registry `json:"registry"`
	}{SchemaVersion: skillregistry.SchemaVersion, Usable: usable, CachePath: cachePath, Registry: registry})
	return 0
}

func presentState(usable bool) string {
	if usable {
		return "present"
	}
	return "absent"
}

func boundText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
