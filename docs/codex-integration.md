# Codex integration

This guide is for a local Codex user who wants VGXNESS-managed agent profiles while retaining ownership of Codex configuration. It applies to the standalone `integrate codex` lifecycle; there is no `setup codex` command and VGXNESS installs no Codex plugin.

## Prerequisites and ownership

Install VGXNESS and Codex locally. By default the integration uses `~/.codex`; use an absolute `--config-dir` to select another existing or creatable Codex root. VGXNESS owns only `AGENTS.md` and 14 files below `agents/`. It never parses, writes, or removes `config.toml`, including MCP blocks and other user settings.

Maintain this MCP block yourself in `config.toml` when you want Codex to invoke VGXNESS:

```toml
[mcp_servers.vgxness]
command = "vgxness"
args = ["mcp", "--full"]
```

The locally observed Codex 0.147.0 exposes strict configuration validation. All other versions must first confirm that `codex --help` lists `--strict-config`, then run:

```sh
codex --strict-config mcp list
```

Do not assume that option is available or run a bare interactive invocation.

## Lifecycle

Preview is read-only:

```sh
vgxness integrate codex preview
```

Install, inspect, repair an exact partial installation, and uninstall with:

```sh
vgxness integrate codex install
vgxness integrate codex status
vgxness integrate codex reinstall
vgxness integrate codex uninstall
```

Use `--config-dir /absolute/path/to/.codex` with any command when needed. Status reports drift rather than overwriting changed managed bytes. `reinstall` restores only an exact partial managed layout; drift or recovery evidence stops the operation. Uninstall removes only exact VGXNESS artifacts and leaves `config.toml`, plugins, and unrelated files untouched.

After install or repair, restart Codex so it reloads the managed profiles. On Windows, VGXNESS flushes regular files before publication; directory namespace durability is reported as `file-sync-namespace-best-effort` because Windows does not provide the POSIX directory-sync operation.
