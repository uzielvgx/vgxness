package cli

import (
	"os"
	"strings"
)

// projectDirEnv is set by Claude Code for every hook and plugin MCP process.
// It names the project root, which is stable even when the process is
// launched elsewhere.
const projectDirEnv = "CLAUDE_PROJECT_DIR"

// resolveWorkspace picks the workspace an MCP or hook process is bound to:
// an explicit --workspace flag, then CLAUDE_PROJECT_DIR, then the current
// directory supplied by the caller.
func resolveWorkspace(explicit, cwd string) string {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return explicit
	}
	if fromEnv := strings.TrimSpace(os.Getenv(projectDirEnv)); fromEnv != "" {
		return fromEnv
	}
	return cwd
}
