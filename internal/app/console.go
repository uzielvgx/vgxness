package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"

	appruntime "github.com/uzielvgx/vgxness/internal/app/runtime"
	"github.com/uzielvgx/vgxness/internal/buildinfo"
	"github.com/uzielvgx/vgxness/internal/claudecli"
	"github.com/uzielvgx/vgxness/internal/config"
	"github.com/uzielvgx/vgxness/internal/mcp"
	"github.com/uzielvgx/vgxness/internal/memory"
	"github.com/uzielvgx/vgxness/internal/tui"
)

// claudeTimeout bounds each `claude` invocation so a hung CLI cannot freeze
// the console.
const claudeTimeout = 10 * time.Second

// consoleBackend feeds the console with real product state (decision D-004).
// Every read is non-mutating: the memory database is opened read-only and
// Claude Code is only queried.
type consoleBackend struct {
	workspace string
	memory    appruntime.Memory
	// writer is the write-capable runtime, used only to forget a memory
	// after the user confirms it in the console.
	writer     appruntime.Memory
	claude     claudecli.Client
	health     func(context.Context, string) (int, error)
	executable func() (string, error)
	now        func() time.Time
}

func newConsoleBackend(workspace string) consoleBackend {
	return consoleBackend{
		workspace:  workspace,
		memory:     appruntime.NewMemory("console", true),
		writer:     appruntime.NewMemory("console", false),
		claude:     claudecli.New(nil),
		health:     memory.HealthFile,
		executable: os.Executable,
		now:        time.Now,
	}
}

func (b consoleBackend) options() config.Options { return config.Options{ProjectDir: b.workspace} }

func (b consoleBackend) Overview(ctx context.Context) (tui.Overview, error) {
	if err := ctx.Err(); err != nil {
		return tui.Overview{}, err
	}
	overview := tui.Overview{Workspace: b.workspace, Binary: b.binary()}
	var group sync.WaitGroup
	group.Add(2)
	go func() { defer group.Done(); overview.Claude = b.claudeState(ctx) }()
	go func() { defer group.Done(); overview.Plugin = b.pluginState(ctx) }()
	project := ""
	overview.Storage, project = b.storageState(ctx)
	overview.Sync = b.syncState(ctx)
	if project != "" {
		if handoffs, err := b.memory.SessionHandoffs(ctx, b.options(), project, 3); err == nil {
			for _, item := range handoffs {
				overview.Handoffs = append(overview.Handoffs, tui.Handoff{Handle: item.Handle, Summary: item.Summary, Started: item.StartedAt, Completed: item.CompletedAt})
			}
		}
	}
	group.Wait()
	if err := ctx.Err(); err != nil {
		return tui.Overview{}, err
	}
	overview.At = b.now()
	return overview, nil
}

func (b consoleBackend) Diagnose(ctx context.Context) (tui.Diagnosis, error) {
	started := b.now()
	overview, err := b.Overview(ctx)
	if err != nil {
		return tui.Diagnosis{}, err
	}
	diagnosis := tui.Diagnosis{
		Overview:        overview,
		RootWritable:    writableDirectory(filepath.Dir(overview.Storage.Database)),
		MCPInstructions: utf8.RuneCountInString(mcp.Instructions()),
		MCPTools:        len(mcp.FullToolNames),
	}
	if overview.Plugin.Installed {
		policy, err := os.ReadFile(filepath.Join(overview.Plugin.InstallPath, "policy", "manager.md"))
		diagnosis.PolicyChars, diagnosis.PolicyErr = utf8.RuneCount(policy), err
	}
	diagnosis.At = b.now()
	diagnosis.Took = diagnosis.At.Sub(started)
	return diagnosis, nil
}

func (b consoleBackend) binary() tui.Binary {
	path, _ := b.executable()
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return tui.Binary{Version: buildinfo.Current().Version, Path: path}
}

func (b consoleBackend) claudeState(ctx context.Context) tui.Claude {
	ctx, cancel := context.WithTimeout(ctx, claudeTimeout)
	defer cancel()
	version, err := b.claude.Version(ctx)
	return tui.Claude{Version: version, Minimum: claudecli.MinimumVersion, Err: err}
}

func (b consoleBackend) pluginState(ctx context.Context) tui.Plugin {
	ctx, cancel := context.WithTimeout(ctx, claudeTimeout)
	defer cancel()
	var state tui.Plugin
	plugins, err := b.claude.Plugins(ctx)
	if err != nil {
		state.Err = err
	} else if installed, found := claudecli.FindPlugin(plugins); found {
		state.Installed, state.Enabled, state.Version, state.InstallPath = true, installed.Enabled, installed.Version, installed.InstallPath
		if agents, err := filepath.Glob(filepath.Join(installed.InstallPath, "agents", "*.md")); err == nil {
			state.Agents = len(agents)
		}
	}
	if marketplaces, err := b.claude.Marketplaces(ctx); err == nil {
		if marketplace, found := claudecli.FindMarketplace(marketplaces); found {
			state.MarketplaceAdded = true
			state.Offered, _ = claudecli.MarketplacePluginVersion(marketplace)
		}
	} else if state.Err == nil {
		state.Err = err
	}
	return state
}

// storageState reports the database and returns the project ID when the
// workspace is already known to it.
func (b consoleBackend) storageState(ctx context.Context) (tui.Storage, string) {
	state := tui.Storage{Expected: memory.SchemaVersion()}
	paths, err := config.PathsFor(b.options())
	if err != nil {
		state.Err = err
		return state, ""
	}
	state.Root, state.Database = paths.Root, paths.Database
	if _, err := os.Stat(paths.Database); errors.Is(err, os.ErrNotExist) {
		return state, ""
	} else if err != nil {
		state.Err = err
		return state, ""
	}
	state.Exists = true
	recorded, err := b.memory.SchemaVersion(ctx, b.options())
	if err != nil {
		state.Err = err
		return state, ""
	}
	state.Schema = recorded
	if recorded < state.Expected {
		// A pending migration is not damage; the next write applies it.
		return state, ""
	}
	if _, err := b.health(ctx, paths.Database); err != nil {
		state.Err = err
		return state, ""
	}
	project, err := b.memory.ResolveProject(ctx, b.options(), b.workspace)
	if err != nil {
		// A workspace the database has never seen has no memories yet.
		return state, ""
	}
	state.Memories, _ = b.memory.CountActive(ctx, b.options(), project)
	return state, project
}

func (b consoleBackend) syncState(ctx context.Context) tui.SyncState {
	status, err := b.memory.SyncStatus(ctx, b.options())
	if err != nil {
		return tui.SyncState{Err: err}
	}
	return tui.SyncState{Configured: status.Configured, Enabled: status.Enabled, Credential: string(status.Credential)}
}

// memoryLimit is the store's per-query cap.
const memoryLimit = 50

// project resolves the workspace's project without creating anything; it
// returns os.ErrNotExist while the database does not exist yet.
func (b consoleBackend) project(ctx context.Context) (string, error) {
	paths, err := config.PathsFor(b.options())
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(paths.Database); err != nil {
		return "", err
	}
	return b.memory.ResolveProject(ctx, b.options(), b.workspace)
}

func (b consoleBackend) SearchMemories(ctx context.Context, query tui.MemoryQuery) (tui.MemoryResults, error) {
	project, err := b.project(ctx)
	if errors.Is(err, os.ErrNotExist) {
		// No database yet: the project has no memories.
		return tui.MemoryResults{}, nil
	}
	if err != nil {
		return tui.MemoryResults{}, err
	}
	var results tui.MemoryResults
	if results.Total, err = b.memory.CountActive(ctx, b.options(), project); err != nil {
		return results, err
	}
	counts, err := b.memory.TypeCounts(ctx, b.options(), project)
	if err != nil {
		return results, err
	}
	for _, count := range counts {
		results.Types = append(results.Types, tui.TypeCount{Type: count.Type, Count: count.Count})
	}
	var entries []memory.Entry
	if query.Text == "" {
		entries, err = b.memory.Recent(ctx, b.options(), memory.Recent{Project: project, Scope: memory.ScopeProject, Limit: memoryLimit})
	} else {
		entries, err = b.memory.Recall(ctx, b.options(), memory.Recall{Query: query.Text, Project: project, Scope: memory.ScopeProject, Type: query.Type, Limit: memoryLimit, MatchAny: true})
	}
	if err != nil {
		return results, err
	}
	for _, entry := range entries {
		if query.Type != "" && entry.Type != query.Type {
			continue
		}
		results.Items = append(results.Items, memoryItem(entry))
	}
	return results, nil
}

func (b consoleBackend) GetMemory(ctx context.Context, id string) (tui.MemoryItem, error) {
	project, err := b.project(ctx)
	if err != nil {
		return tui.MemoryItem{}, err
	}
	entry, err := b.memory.Get(ctx, b.options(), memory.Lookup{ID: id, Project: project, Scope: memory.ScopeProject})
	if err != nil {
		return tui.MemoryItem{}, err
	}
	return memoryItem(entry), nil
}

func (b consoleBackend) ForgetMemory(ctx context.Context, id string) error {
	project, err := b.project(ctx)
	if err != nil {
		return err
	}
	_, err = b.writer.Forget(ctx, b.options(), memory.Forget{ID: id, Project: project, Scope: memory.ScopeProject})
	return err
}

func (b consoleBackend) Handoffs(ctx context.Context, limit int) ([]tui.Handoff, error) {
	project, err := b.project(ctx)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	items, err := b.memory.SessionHandoffs(ctx, b.options(), project, limit)
	if err != nil {
		return nil, err
	}
	handoffs := make([]tui.Handoff, len(items))
	for index, item := range items {
		handoffs[index] = tui.Handoff{Handle: item.Handle, Summary: item.Summary, Started: item.StartedAt, Completed: item.CompletedAt}
	}
	return handoffs, nil
}

func memoryItem(entry memory.Entry) tui.MemoryItem {
	return tui.MemoryItem{ID: entry.ID, Title: entry.Title, Type: entry.Type, Topic: entry.TopicKey, Producer: entry.Producer, Preview: entry.Preview, Content: entry.Content, References: entry.References, Created: entry.CreatedAt, Updated: entry.UpdatedAt}
}
