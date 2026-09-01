package testutil

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// CodexRunner is an in-memory Codex CLI model for integration tests. State is
// isolated by CODEX_HOME and protected for parallel test use.
type CodexRunner struct {
	mu    sync.Mutex
	roots map[string]codexState
}

type codexState struct {
	market, plugin bool
	config         []byte
}

func NewCodexRunner() *CodexRunner { return &CodexRunner{roots: make(map[string]codexState)} }

func (r *CodexRunner) Run(_ context.Context, _ string, args, env []string) ([]byte, error) {
	root := codexHome(env)
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.roots[root]
	switch strings.Join(args, " ") {
	case "plugin marketplace list --json":
		markets := []any{}
		if state.market {
			markets = append(markets, map[string]any{"name": "vgxness", "root": root})
		}
		return json.Marshal(map[string]any{"marketplaces": markets})
	case "plugin list --json":
		installed := []any{}
		if state.plugin {
			installed = append(installed, map[string]any{"pluginId": "vgxness@vgxness", "name": "vgxness", "marketplaceName": "vgxness", "version": pluginVersion(root), "installed": true, "enabled": true})
		}
		return json.Marshal(map[string]any{"installed": installed})
	case "plugin add vgxness@vgxness --json":
		state.plugin = true
		appendCodexConfig(root, &state, "\n[plugins.\"vgxness@vgxness\"]\nenabled = true\n")
	case "plugin remove vgxness@vgxness --json":
		state.plugin = false
	case "plugin marketplace remove vgxness --json":
		state.market = false
		if state.config != nil {
			_ = os.WriteFile(filepath.Join(root, "config.toml"), state.config, 0o600)
		}
	default:
		if strings.HasPrefix(strings.Join(args, " "), "plugin marketplace add ") {
			state.market = true
			appendCodexConfig(root, &state, "\n[marketplaces.vgxness]\npath = \""+root+"\"\n")
		}
	}
	r.roots[root] = state
	return []byte(`{}`), nil
}

func appendCodexConfig(root string, state *codexState, addition string) {
	path := filepath.Join(root, "config.toml")
	if state.config == nil {
		state.config, _ = os.ReadFile(path)
	}
	if !strings.Contains(string(state.config), "[mcp_servers") {
		return
	}
	current, err := os.ReadFile(path)
	if err == nil {
		_ = os.WriteFile(path, append(current, addition...), 0o600)
	}
}

func codexHome(env []string) string {
	for _, value := range env {
		if strings.HasPrefix(value, "CODEX_HOME=") {
			return strings.TrimPrefix(value, "CODEX_HOME=")
		}
	}
	return ""
}

func pluginVersion(root string) string {
	body, err := os.ReadFile(filepath.Join(root, "plugins", "vgxness", ".codex-plugin", "plugin.json"))
	if err != nil {
		return ""
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(body, &manifest) != nil {
		return ""
	}
	return manifest.Version
}
