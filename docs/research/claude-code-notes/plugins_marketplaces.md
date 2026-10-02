# Claude Code plugins and plugin marketplaces (state as of Claude Code v2.1.287, 2026-10-01)

Research date: 2026-10-01. Latest Claude Code release at research time: **v2.1.287 (October 1, 2026)** — [Changelog](https://code.claude.com/docs/en/changelog). All primary sources are the official docs at code.claude.com, which were restructured into a `/docs/en/plugins/*` tree (the older `plugins-reference` / `plugin-marketplaces` URLs now serve the "Plugin manifest reference" and "Create a marketplace" pages) — [Docs index](https://code.claude.com/docs/llms.txt). No third-party sources were needed.

Short source keys used below:
- MANIFEST = https://code.claude.com/docs/en/plugins/manifest-reference
- COMPONENTS = https://code.claude.com/docs/en/plugins/components
- MKT = https://code.claude.com/docs/en/plugins/marketplace-reference
- LOADING = https://code.claude.com/docs/en/plugins/loading
- CLI = https://code.claude.com/docs/en/plugins/cli-reference
- DEPS = https://code.claude.com/docs/en/plugins/dependencies
- HOST = https://code.claude.com/docs/en/plugins/host-marketplace
- ORG = https://code.claude.com/docs/en/plugins/org
- SEC = https://code.claude.com/docs/en/plugins/security
- INSTALL = https://code.claude.com/docs/en/plugins/install
- PUBLISH = https://code.claude.com/docs/en/plugins/publish
- TROUBLE = https://code.claude.com/docs/en/plugins/troubleshooting
- SUBAGENTS = https://code.claude.com/docs/en/sub-agents
- HOOKS = https://code.claude.com/docs/en/hooks
- MCP = https://code.claude.com/docs/en/mcp
- SETTINGSREF = https://code.claude.com/docs/en/settings-reference
- HINTS = https://code.claude.com/docs/en/plugins/cli-hints
- CHANGELOG = https://code.claude.com/docs/en/changelog

---

## 1. Plugin directory layout and the full `.claude-plugin/plugin.json` schema

### Takeaway
A plugin is a directory; the manifest `.claude-plugin/plugin.json` is optional and only `name` is required. Every other file (skills/, agents/, hooks/, .mcp.json, bin/, settings.json, …) lives at the plugin root, never inside `.claude-plugin/`. All component paths must start with `./`, stay inside the plugin root, and exist.

### Cited Findings
- The manifest is optional; without it Claude Code loads components from the standard layout and takes the name from the marketplace entry (or directory name for `--plugin-dir`). Put every other plugin file at the plugin root, not inside `.claude-plugin/` — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference)
- `name` is the only required key; kebab-case; no spaces, `@`, `:`, path separators, control or bidi chars. Every component is namespaced under it (agent `reviewer` in plugin `deploy-tools` → `deploy-tools:reviewer`) — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference)
- Reserved-name rules (enforced by `validate`, `init`, `tag`): starting with `claude-`, `anthropic-`, `anthropics-`, `cc-plugin-` is an error; `claude`, `anthropic`, `claude-code`, `claude-mods` exact names error; `official` next to claude/anthropic errors; `claude`/`anthropic` as a whole word elsewhere (e.g. `mcp-for-claude`) is a warning. Claude Code still installs/loads such plugins — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference)
- Full top-level field list: `$schema`, `name`, `displayName`, `version`, `description`, `author` {name (required), email, url}, `homepage` (must parse as URL or plugin fails to load), `repository` (not validated), `license` (SPDX), `keywords`, `metadata` (free-form, ignored by Claude Code; v2.1.222+), `defaultEnabled` (default true), `dependencies`, `settings` (only `agent` and `subagentStatusLine` take effect), `userConfig`, `types` (mods), `channels`, `skills`, `commands`, `agents`, `hooks`, `mcpServers`, `lspServers`, `outputStyles`, `workflows`, `experimental` {`themes`, `monitors`, `evals`} — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference)
- Unknown top-level keys are stripped (warning in validate); unknown keys inside `userConfig` options, `channels` entries, `lspServers` configs and `monitors` entries are errors and the plugin does not load — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference)
- `version` is a free string (not checked against semver). Setting it pins users to that version until you change it — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference)
- Custom component paths: `commands`, `agents`, `outputStyles`, `workflows`, `experimental.themes`, `experimental.monitors` **replace** the default folder scan; `skills` **adds** to `skills/`; `hooks`, `mcpServers`, `lspServers` **merge** with the default file (later same-named server replaces earlier). If you set a replacing key while the default folder exists you get the warning `Default <folder>/ folder is ignored because the manifest sets "<key>"` — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference)
- `agents` entries must be `.md` files (directories not accepted in the manifest key); `skills` entries must be directories; `"."`/`"./"` names the plugin root for skills (`"."` failed validation before v2.1.221) — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference)
- `commands` also accepts an object map: name → `{source | content, description, argumentHint, model, allowedTools}` (exactly one of source/content) — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference)
- Path rules: must start with `./`; `..` is rejected ("Path contains '..' which could be a path traversal attempt"); paths resolving outside the root (incl. symlinks out, except intra-marketplace symlinks) are rejected; on macOS/Linux any backslash in a path is rejected — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference); [LOADING](https://code.claude.com/docs/en/plugins/loading)
- Standard layout: `.claude-plugin/plugin.json`, `skills/<name>/SKILL.md`, `commands/*.md` (legacy), `agents/*.md` (subfolders become name segments), `hooks/hooks.json`, `.mcp.json`, `.lsp.json`, `output-styles/`, `workflows/*.js`, `themes/*.json`, `monitors/monitors.json`, `bin/`, `settings.json` — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference)
- A `CLAUDE.md` at the plugin root is **not** loaded as context; `claude plugin validate` warns `CLAUDE.md at the plugin root is not loaded as project context`. Put instructions in a skill — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference); [COMPONENTS](https://code.claude.com/docs/en/plugins/components)
- A plugin with a root `SKILL.md`, no `skills/` and no `skills` key loads as a single skill (set `name` in frontmatter or a marketplace install names it after its cache dir) — [COMPONENTS](https://code.claude.com/docs/en/plugins/components)
- Anthropic's directory (claude.ai listing) requires a manifest even though Claude Code does not — [COMPONENTS](https://code.claude.com/docs/en/plugins/components)

### Inferences
- For VGXNESS, the plugin directory should be a dedicated subfolder (not the Go repo root), because whatever is the plugin root gets copied into the cache on install and `CLAUDE.md`/Go sources at the root would be useless or misleading there.

### Gaps
- The published JSON Schema URL for `plugin.json` editor autocomplete was not located (the marketplace uses `https://anthropic.com/claude-code/marketplace.schema.json` — [claude-plugins-official marketplace.json](https://raw.githubusercontent.com/anthropics/claude-plugins-official/main/.claude-plugin/marketplace.json)).

---

## 2. Every component type a plugin can ship (supported vs not)

### Takeaway
Supported in Claude Code: skills, commands (legacy), agents (subagents), hooks, MCP servers (stdio/http/sse/ws, inline or `.mcp.json`, or `.mcpb` bundles), LSP servers, output styles, themes, workflows, monitors (experimental), channels, mods (new in v2.1.287), a `bin/` folder put on the Bash tool PATH, and a plugin `settings.json` where **only `agent` and `subagentStatusLine`** take effect. Not supported: root `CLAUDE.md`, arbitrary settings (permissions, env, model…), per-agent hooks/MCP servers/permissionMode in plugin agents.

### Cited Findings
- **Skills**: `skills/<dir>/SKILL.md`; invoked as `/<plugin>:<dir>` (frontmatter `name` replaces last segment); Claude auto-loads by `description` — [COMPONENTS](https://code.claude.com/docs/en/plugins/components)
- **Commands**: `commands/<file>.md` → `/<plugin>:<file>`; subdirectory adds a segment; same frontmatter as skills; documented as the older format superseded by skills — [COMPONENTS](https://code.claude.com/docs/en/plugins/components)
- **Agents**: `agents/*.md`, name `<plugin>:<name>`, invoke with `@agent-<plugin>:<name>`; loaded recursively from subfolders (`agents/review/security.md` → `my-plugin:review:security`). Supported frontmatter: `name`, `description`, `model`, `effort`, `maxTurns`, `tools`, `disallowedTools`, `skills`, `memory`, `background`, `omitClaudeMd`, `isolation` (only `"worktree"`), `color`, `experimental.cacheTtl`. **Ignored** in plugin agents: `permissionMode`, `hooks`, `mcpServers`, `initialPrompt`. Unparseable frontmatter → agent loads with all fields ignored — [COMPONENTS](https://code.claude.com/docs/en/plugins/components)
- **Hooks**: `hooks/hooks.json` with top-level `"hooks"` key, same shape as settings.json hooks; handlers can be a shell command, HTTP request, MCP tool call, model prompt, or subagent. Plugin hooks are registered when the session loads the plugin and fire on their events regardless of whether the plugin's skills are used — [COMPONENTS](https://code.claude.com/docs/en/plugins/components)
- To match the plugin's own MCP tools in a hook matcher or permission rule use `mcp__plugin_<plugin>_<server>__<tool>`; a matcher on the server name alone never fires — [COMPONENTS](https://code.claude.com/docs/en/plugins/components)
- **MCP servers**: `.mcp.json` at plugin root (with or without `mcpServers` wrapper) or `mcpServers` in manifest (inline map, `.json` path, `.mcpb`/`.dxt` bundle path or https URL). Appear in `/mcp` as `plugin:<plugin>:<server>`. Claude Code starts them automatically when the plugin is enabled; users can toggle a plugin server off in `/mcp` — [COMPONENTS](https://code.claude.com/docs/en/plugins/components); [MCP](https://code.claude.com/docs/en/mcp)
- On `/reload-plugins`, servers with unchanged config keep their connection, changed ones reconnect, removed ones disconnect. A reload that adds/removes MCP servers is refused unless `--force` because it invalidates the prompt cache — [COMPONENTS](https://code.claude.com/docs/en/plugins/components); [CLI](https://code.claude.com/docs/en/plugins/cli-reference)
- A local stdio server runs in Claude Code and in local Cowork sessions, but not on claude.ai (needs a remote https server there) — [COMPONENTS](https://code.claude.com/docs/en/plugins/components)
- **LSP servers**: `.lsp.json` (map name → config, no wrapper) with required `command` and `extensionToLanguage`; strict object (fields: args, transport, env, initializationOptions, settings, workspaceFolder, startupTimeout, shutdownTimeout, restartOnCrash, maxRestarts, diagnostics). The plugin does not install the server binary: Claude Code starts `command` from the user's PATH — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference); [COMPONENTS](https://code.claude.com/docs/en/plugins/components)
- **Output styles**: `output-styles/<name>.md` with `name`/`description` frontmatter (e.g. `keep-coding-instructions: true`), shown in `/output-style` as `<plugin>:<name>` — [COMPONENTS](https://code.claude.com/docs/en/plugins/components)
- **Themes** (`themes/*.json`, `experimental.themes`), **workflows** (`workflows/*.js`, run as `/<plugin>:<name>`), **monitors** (`monitors/monitors.json`, `experimental.monitors`; background shell command whose output reaches Claude as notifications; interactive sessions only; not on Bedrock/Vertex/Foundry; cannot use `${user_config.*}`), **channels** (bind an MCP server to push messages into the session) — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference); [COMPONENTS](https://code.claude.com/docs/en/plugins/components)
- **Mods** (new): JavaScript hook modules listed under a `modules` key in `hooks/hooks.json`; "Added Claude Mods: plugins may now modify deeper behavior" in v2.1.287 — [COMPONENTS](https://code.claude.com/docs/en/plugins/components); [CHANGELOG](https://code.claude.com/docs/en/changelog)
- **Executables `bin/`**: files there are on the Bash tool shell's PATH while the plugin is enabled; plugin bin dirs come **after** the user's PATH so they can't shadow system commands. claude.ai and Cowork refuse to install a plugin that has a top-level `bin/` directory — [COMPONENTS](https://code.claude.com/docs/en/plugins/components)
- **Plugin `settings.json`**: only `agent` and `subagentStatusLine` take effect, every other key is dropped; `settings.json` file wins over manifest `settings`; plugin defaults are the lowest settings layer (a user's own `agent` overrides); if two plugins set the same key, the last loaded wins and `--debug` logs `overrides setting` — [COMPONENTS](https://code.claude.com/docs/en/plugins/components); [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference)
- **`agent` setting semantics**: runs the main thread as the named subagent, applying its system prompt, tool restrictions and model; `--agent` overrides per session — [SETTINGSREF](https://code.claude.com/docs/en/settings-reference)
- **Important**: unless the agent's prompt is empty, a custom agent's system prompt **replaces the default Claude Code system prompt entirely** (like `--system-prompt`); CLAUDE.md and project memory still load via the message flow — [SUBAGENTS](https://code.claude.com/docs/en/sub-agents)
- A main-thread agent can spawn subagents via the Agent tool; `tools: Agent(worker, researcher), Read, Bash` is an allowlist of spawnable types (only effective for main-thread agents). Task tool was renamed Agent in v2.1.63 (`Task(...)` still aliased) — [SUBAGENTS](https://code.claude.com/docs/en/sub-agents)
- For a plugin agent, `claude --agent security-reviewer` works by bare name; use `my-plugin:security-reviewer` to disambiguate — [SUBAGENTS](https://code.claude.com/docs/en/sub-agents)
- **SessionStart hook** can return `hookSpecificOutput.additionalContext`, added to Claude's context before the first prompt (wrapped as a system reminder); supports matchers `startup`, `resume`, `clear`, `compact`, `fork`; only `command` and `mcp_tool` hook types. `additionalContext` / stdout capped at **10,000 characters** per string (overflow saved to a file with a 2,000-char preview, Claude not told to read it) — [HOOKS](https://code.claude.com/docs/en/hooks)

### Inferences
- There is no "plugin CLAUDE.md". The three realistic ways to put an always-on policy (VGXNESS Manager) in front of the main model are: (a) plugin `settings.json` `"agent"` (replaces Claude Code's whole system prompt — heavy, and loses Claude Code's built-in coding guidance unless the Manager prompt reproduces it), (b) a `SessionStart` hook emitting `additionalContext` (additive, ≤10k chars, also re-fires on `compact`/`clear`), (c) an output style (user must choose it). (b) is the least invasive.
- Because plugin agents ignore `mcpServers` frontmatter, worker roles get VGXNESS tools from the plugin-level MCP server and reference them in `tools` by their `mcp__plugin_vgxness_<server>__<tool>` names.

### Gaps
- Whether `keep-coding-instructions: true` in an output style fully preserves the default prompt was not verified in this pass (see [output styles docs](https://code.claude.com/docs/en/output-styles)).

---

## 3. Environment variables, substitutions, user config and secrets

### Takeaway
Three path variables — `${CLAUDE_PLUGIN_ROOT}` (versioned install dir, changes on update), `${CLAUDE_PLUGIN_DATA}` (persistent `~/.claude/plugins/data/<id>/`, survives updates, deleted on final uninstall by default), `${CLAUDE_PROJECT_DIR}` — plus `userConfig` values (`${user_config.KEY}` and `CLAUDE_PLUGIN_OPTION_<KEY>`), with `sensitive: true` values stored in the OS credential store.

### Cited Findings
- `${CLAUDE_PLUGIN_ROOT}` = absolute path of the installed version; `${CLAUDE_PLUGIN_DATA}` = `~/.claude/plugins/data/<id>/` (non `[A-Za-z0-9_-]` chars → `-`, so `my-plugin@my-marketplace` → `my-plugin-my-marketplace`), created on first reference, kept across updates; `${CLAUDE_PROJECT_DIR}` = project root. Don't write state into PLUGIN_ROOT — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference); [COMPONENTS](https://code.claude.com/docs/en/plugins/components)
- Where they resolve: hook `command`/`args` (exported: ROOT, DATA, PROJECT_DIR, `CLAUDE_PLUGIN_OPTION_<KEY>`); monitor `command` (not exported); MCP stdio `command`/`args`/`env` (exported: ROOT, DATA); MCP http/sse/ws `url`/`headers`/`headersHelper`; LSP `command`/`args`/`env`/`workspaceFolder`; skill/command/agent Markdown bodies (inline substitution) — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference)
- The variables are **not** in the environment of commands Claude runs via the Bash tool (main or subagent); write `${...}` in skill/agent Markdown instead and it is substituted inline — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference)
- Quoting: in shell-form hooks wrap in double quotes (`"\"${CLAUDE_PLUGIN_ROOT}\"/scripts/x.sh"`), or use exec form with `args` (no quoting needed). Validate warns about unquoted variables. On Windows substituted paths use forward slashes — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference)
- `userConfig` option fields (strict): `type` (string|number|boolean|directory|file), `title`, `description` (required), `required`, `default`, `options` (v2.1.271+; older clients can't load the plugin), `multiple`, `sensitive`, `min`/`max`. Options also appear as rows in `/config` (v2.1.269+), except sensitive and multiple — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference)
- Storage: non-sensitive → `pluginConfigs` in user `settings.json`; sensitive → platform secure credential store — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference)
- References: `${user_config.KEY}` in MCP config, LSP config, exec-form hook `args`, and skill/agent content (sensitive values become a placeholder in skill/agent content); `CLAUDE_PLUGIN_OPTION_<KEY>` exported to hook processes. Shell-form hook commands, monitor commands and MCP `headersHelper` **reject** `${user_config.*}` (error) — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference)
- The config dialog appears on interactive install via `/plugin`, `/plugin install`, or enabling in the Installed tab; `/plugin configure <plugin>@<mkt>` reopens it. `claude plugin install` (shell) **never prompts**: use `--config KEY=VALUE` (v2.1.147+) or `claude plugin configure <id> --values-stdin` (v2.1.285+) — [COMPONENTS](https://code.claude.com/docs/en/plugins/components); [CLI](https://code.claude.com/docs/en/plugins/cli-reference)
- MCP servers also get "access to the same environment variables as manually configured servers" (e.g. `${DB_URL}` from the user's env) — [MCP](https://code.claude.com/docs/en/mcp)
- On uninstall from the last scope, Claude Code deletes stored options, secrets and the data dir unless `--keep-data` — [CLI](https://code.claude.com/docs/en/plugins/cli-reference)

### Inferences
- VGXNESS's durable memory database must **not** live in `${CLAUDE_PLUGIN_DATA}`: uninstalling the plugin would delete the user's project memory by default. Keep it where the `vgxness` binary already stores it (its own config/data dir), and use PLUGIN_DATA only for caches (e.g. a downloaded binary).

### Gaps
- None material.

---

## 4. Marketplaces: schema, hosting, sources, scopes, enable/disable, updates, caching, uninstall

### Takeaway
A marketplace is `.claude-plugin/marketplace.json` (required: `name`, `owner.name`, `plugins[]`) in any git repo; relative plugin sources like `./plugins/foo` resolve from the repo root. Users run `claude plugin marketplace add owner/repo` then `claude plugin install <plugin>@<marketplace>`. Third-party marketplaces have auto-update **off** by default; updates are detected only when the computed version changes (manifest `version` → entry `version` → commit SHA).

### Cited Findings
- File location `.claude-plugin/marketplace.json`; the directory containing `.claude-plugin/` is the marketplace root, relative sources resolve from it. One marketplace per `name` per user — [MKT](https://code.claude.com/docs/en/plugins/marketplace-reference)
- Top-level fields: `name` (letters/digits/`.`/`_`/`-`, start alnum), `owner` {name required, email, url}, `plugins`, `$schema`, `description` (warning if missing), `version`, `metadata.{description,version,pluginRoot}` (pluginRoot v2.1.239+), `forceRemoveDeletedPlugins`, `allowCrossMarketplaceDependenciesOn`, `renames` (v2.1.193+). Unknown keys are ignored (warning in validate) — [MKT](https://code.claude.com/docs/en/plugins/marketplace-reference)
- Reserved marketplace names include official ones (`claude-plugins-official`, `claude-code-marketplace`, …), community ones, impersonations, non-ASCII names, `inline`, `builtin`, `skills-dir`, `synced`, `npm`, `pip`, `uv`, `cargo`, `github`, `gh`, and the `claudeai-` prefix — [MKT](https://code.claude.com/docs/en/plugins/marketplace-reference)
- Plugin entry: required `name`, `source`; optional `description`, `version`, `category`, `tags`, `strict` (default true), `relevance`, `dependencies`, `defaultEnabled`, `displayName`, `metadata`, `headers`/`headersHelper` (archive only), plus any plugin.json field — [MKT](https://code.claude.com/docs/en/plugins/marketplace-reference)
- Plugin source types: relative path (`./…`, or `"."` for the root, or bare name under `metadata.pluginRoot`), `github` {repo, ref, sha}, `url` (git URL) {url, ref, sha}, `git-subdir` {url, path, ref, sha} (sparse partial clone), `npm` {package, version, registry} (no install scripts run), `archive` {url, sha256} (v2.1.224+), `command` {command, timeout, mode copy|link} (v2.1.229+) — [MKT](https://code.claude.com/docs/en/plugins/marketplace-reference)
- Relative paths only resolve when Claude Code has the marketplace files (github/git/file/directory marketplace sources); a `url` marketplace (bare marketplace.json URL) cannot use relative sources — [MKT](https://code.claude.com/docs/en/plugins/marketplace-reference); [HOST](https://code.claude.com/docs/en/plugins/host-marketplace)
- Strict mode: with `plugin.json` present and `strict: true` the entry's commands/agents/skills/outputStyles/themes are appended (entry hooks replace per event); with `strict: false` any component field in the entry is a conflict. Entry `mcpServers`, `lspServers`, `userConfig`, `channels` don't apply when plugin.json exists. Manifest `version` overrides entry `version`; entry display fields override manifest — [MKT](https://code.claude.com/docs/en/plugins/marketplace-reference); [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference)
- Before install, Claude Code can read plugin.json (for display) only for relative-path entries — [MKT](https://code.claude.com/docs/en/plugins/marketplace-reference)
- `claude plugin marketplace add <source>` accepts `owner/repo`, `owner/repo#ref`/`@ref` (github), SSH/`.git` URLs (git), other https URLs (url → marketplace.json), local dir/file paths; flags `--scope user|project|local` (default user), `--sparse <paths…>`, `--claudeai`. It writes `extraKnownMarketplaces` to settings plus `~/.claude/plugins/known_marketplaces.json` — [CLI](https://code.claude.com/docs/en/plugins/cli-reference); [LOADING](https://code.claude.com/docs/en/plugins/loading)
- Marketplace source `path` (github/git) defaults to `.claude-plugin/marketplace.json`; a file elsewhere requires users to declare it in `extraKnownMarketplaces` with `path` — [MKT](https://code.claude.com/docs/en/plugins/marketplace-reference)
- In-session: `/plugin install <plugin> --marketplace <source>` adds marketplace and installs in one step (v2.1.275+) — [CLI](https://code.claude.com/docs/en/plugins/cli-reference)
- `claude plugin install <plugin> [-s user|project|local] [--config k=v] [-y] [--accept-command sha] [--json]`; default scope `user` — [CLI](https://code.claude.com/docs/en/plugins/cli-reference)
- Scopes: user → `~/.claude/settings.json`; project → `.claude/settings.json` (committed; **does not download** for collaborators, each runs install `--scope project`, unless the entry is a relative-path source); local → `.claude/settings.local.json`; managed → admin settings (update-only scope for CLI). Precedence lowest→highest: `--add-dir`, user, project, local, `--settings` flag, managed — [INSTALL](https://code.claude.com/docs/en/plugins/install); [LOADING](https://code.claude.com/docs/en/plugins/loading)
- Project-scope plugins load only after workspace trust; their MCP servers go through per-server approval like a project `.mcp.json`; `.mcpb` servers and monitors are skipped for project scope. Personal-scope plugins have none of these restrictions — [LOADING](https://code.claude.com/docs/en/plugins/loading)
- Enable/disable: `claude plugin enable|disable <plugin> [-s scope] [--json]`, `disable --all`; scope auto-detected local→project→user — [CLI](https://code.claude.com/docs/en/plugins/cli-reference)
- Update: `claude plugin update <plugin>` (scope auto-detected since v2.1.281; earlier defaulted to user), `claude plugin marketplace update [name]`; new version loads next session or after `/reload-plugins` — [CLI](https://code.claude.com/docs/en/plugins/cli-reference)
- Version computation: manifest `version` → entry `version` → for github/url/git-subdir the 12-char commit SHA; relative path in a git-hosted marketplace → commit SHA of the dir; npm → `unknown`. A pinned `"version": "1.0.0"` keeps every user on the cached copy no matter how many commits you push. Don't set version in both places (plugin.json wins; validate warns) — [LOADING](https://code.claude.com/docs/en/plugins/loading); [HOST](https://code.claude.com/docs/en/plugins/host-marketplace)
- Auto-update: after the first message, random delay ≤10 min, refreshes marketplaces with auto-update on; running session keeps old version (`Plugin updated: <name> · Run /reload-plugins to apply`). Default on for Anthropic official marketplaces and claude.ai marketplaces, **off for every other marketplace**; `marketplace.json` has no field to turn it on — user toggles it in `/plugin` → Marketplaces, or an admin sets `autoUpdate` in `extraKnownMarketplaces`. `DISABLE_AUTOUPDATER`/`DISABLE_UPDATES`/`CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC` disable it unless `FORCE_AUTOUPDATE_PLUGINS=1` — [LOADING](https://code.claude.com/docs/en/plugins/loading); [HOST](https://code.claude.com/docs/en/plugins/host-marketplace)
- After a mid-session update, hooks/MCP/LSP keep the old version's path until `/reload-plugins`; monitors need a restart — [LOADING](https://code.claude.com/docs/en/plugins/loading)
- Caching: `~/.claude/plugins/cache/<marketplace>/<plugin>/<version>/` (= PLUGIN_ROOT), `data/<id>/`, `marketplaces/<name>/` (clone), `installed_plugins.json`, `known_marketplaces.json`; root overridable by `CLAUDE_CODE_PLUGIN_CACHE_DIR`. Old version dirs get `.orphaned_at` and are deleted 14 days later. Files outside the plugin dir aren't copied (`../shared` breaks) — [LOADING](https://code.claude.com/docs/en/plugins/loading)
- Relative-path plugins from a marketplace added as a **local directory** load in place (edits apply on next session or `/reload-plugins` without bumping version) — [LOADING](https://code.claude.com/docs/en/plugins/loading)
- Node deps: if plugin root has `package.json` + npm/bun lockfile, Claude Code runs `npm ci --ignore-scripts` / `bun install --frozen-lockfile --ignore-scripts` (60 s timeout) when caching; cannot be disabled — [LOADING](https://code.claude.com/docs/en/plugins/loading)
- Uninstall: `claude plugin uninstall <plugin> [-s scope] [--keep-data] [--prune] [-y] [--json]`; `marketplace remove` from the last scope uninstalls all its plugins and deletes their options/secrets/data — [CLI](https://code.claude.com/docs/en/plugins/cli-reference)
- Release channels: no built-in concept; host two marketplaces with different `name`s pointing at different refs. Renames via `renames` map; removal uninstalls on users' machines only with `forceRemoveDeletedPlugins` — [HOST](https://code.claude.com/docs/en/plugins/host-marketplace); [MKT](https://code.claude.com/docs/en/plugins/marketplace-reference)
- `claude plugin tag [--push]` creates `<name>--v<version>` annotated tags after checking plugin.json and marketplace entry agree, clean tree, tag not existing — [CLI](https://code.claude.com/docs/en/plugins/cli-reference)
- Local dev: `claude --plugin-dir <path|zip>` (repeatable; a folder of plugins loads each child with a manifest, v2.1.265+), `--plugin-url <zip-url>`, `CLAUDE_CODE_PLUGIN_DIRS` env; loads as `<name>@inline`, overrides a same-named installed plugin silently (only `--debug` log notes it); `claude plugin init <name>` scaffolds into `~/.claude/skills/<name>/` (loads as `@skills-dir`) — [CLI](https://code.claude.com/docs/en/plugins/cli-reference); [LOADING](https://code.claude.com/docs/en/plugins/loading)
- Anthropic's official marketplace itself uses relative `./plugins/...` sources plus `git-subdir` sources pinned with `ref` + `sha` for external plugins — [claude-plugins-official marketplace.json](https://raw.githubusercontent.com/anthropics/claude-plugins-official/main/.claude-plugin/marketplace.json)
- Distribution to claude.ai/Cowork users is via Anthropic's directory (developer portal at claude.ai/directory/manage, paid plan required); `claude-plugins-official` does not take submissions through the portal — [PUBLISH](https://code.claude.com/docs/en/plugins/publish)

### Inferences
- A single GitHub repo can be both the product repo and the marketplace; `--sparse .claude-plugin plugins` lets users avoid cloning the whole Go tree into `~/.claude/plugins/marketplaces/`.
- Because third-party auto-update is off by default, VGXNESS should tell users to enable auto-update or have `vgxness` run `claude plugin update vgxness@vgxness` as part of its own self-update.

### Gaps
- Exact git clone depth for `github` marketplace sources (full vs shallow) was not documented in the pages read.

---

## 5. Validation and debugging

### Takeaway
`claude plugin validate <dir> [--strict] [--json]` is the authoritative check (exit 0 pass / 1 fail / 2 validator error); since v2.1.281 it also checks `.mcp.json` entries. Runtime problems surface in the `/plugin` Errors tab, `claude plugin list [--json]`, `claude plugin details`, and `claude --debug` logs in `~/.claude/debug/<session-id>.txt`.

### Cited Findings
- `validate` picks `.claude-plugin/marketplace.json` if present in the dir, else `plugin.json`, else component dirs (v2.1.233+). A marketplace run does **not** open plugins' skill/agent/hook/MCP files — validate each plugin dir separately. `--strict` (v2.1.145+) turns warnings into failures; `--json` (v2.1.259+) — [CLI](https://code.claude.com/docs/en/plugins/cli-reference)
- MCP checks (v2.1.281+): errors for entries that would be dropped, undeclared `${user_config.KEY}`, invalid remote URL; warnings for non-loopback `http://`/`ws://` and literal-looking credentials in headers — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference)
- Path checks for `outputStyles`, `lspServers`, `monitors`, `themes` require v2.1.283+; `validate` doesn't read `.lsp.json` — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference); [COMPONENTS](https://code.claude.com/docs/en/plugins/components)
- Not caught by validate: entry `hooks` written as a path/array (fails at load with `not yet supported in a marketplace entry`), source fetch errors — [MKT](https://code.claude.com/docs/en/plugins/marketplace-reference)
- A `.mcp.json` entry that fails schema is dropped silently from the Errors tab and logged only in the debug log as `Invalid MCP server config for <server> in <path>`; `--debug` doesn't print to the terminal — [TROUBLE](https://code.claude.com/docs/en/plugins/troubleshooting)
- "Server works with `--plugin-dir` but fails after install" = path that only worked from source dir; use `${CLAUDE_PLUGIN_ROOT}` — [TROUBLE](https://code.claude.com/docs/en/plugins/troubleshooting)
- `claude plugin details <name>` prints component inventory and projected always-on token cost — [CLI](https://code.claude.com/docs/en/plugins/cli-reference)
- `claude plugin eval` (v2.1.269+) runs eval cases (in `evals/`) with/without the plugin and gates CI on a threshold — [CLI](https://code.claude.com/docs/en/plugins/cli-reference); [Evals](https://code.claude.com/docs/en/plugin-evals)
- `/plugin` is interactive only (not in `-p`); `/reload-plugins [--force]` applies pending changes; works in non-TTY sessions since v2.1.260 but doesn't connect/disconnect MCP there — [CLI](https://code.claude.com/docs/en/plugins/cli-reference)

### Inferences
- CI for VGXNESS: run `claude plugin validate . --strict` (marketplace) and `claude plugin validate ./plugins/vgxness --strict` (plugin), plus optionally `claude plugin eval` against a fixture.

### Gaps
- None material.

---

## 6. Native binaries, external CLIs on PATH, and plugin dependencies

### Takeaway
There is no per-OS/arch binary or "requires CLI" field. Options: (1) rely on the user's PATH (the documented pattern for LSP plugins), (2) ship executables in `bin/` (Bash-tool PATH only; blocks claude.ai/Cowork installs), (3) reference a bundled file via `${CLAUDE_PLUGIN_ROOT}`, (4) fetch/install into `${CLAUDE_PLUGIN_DATA}` from a `SessionStart` hook, or (5) package the server as an `.mcpb` bundle. Plugin-to-plugin dependencies exist (`dependencies`, semver ranges resolved against `<name>--v<version>` git tags).

### Cited Findings
- LSP plugins configure the connection but don't install the server binary; Claude Code starts `command` by name from the user's PATH; missing binary → Errors tab `Executable not found in $PATH: "<binary>"` and debug `LSP server <name> failed to start` — [COMPONENTS](https://code.claude.com/docs/en/plugins/components); [TROUBLE](https://code.claude.com/docs/en/plugins/troubleshooting)
- MCP stdio `command` may be `${CLAUDE_PLUGIN_ROOT}/servers/db-server` (a binary shipped in the plugin) — [MCP](https://code.claude.com/docs/en/mcp)
- `bin/` executables are on the Bash tool's PATH (after the user's PATH) while enabled; claude.ai/Cowork won't install plugins with top-level `bin/` — [COMPONENTS](https://code.claude.com/docs/en/plugins/components)
- Documented pattern for dependencies that the automatic Node install can't provide: install them from a `SessionStart` hook into `${CLAUDE_PLUGIN_DATA}` (example uses `diff -q` on package.json to reinstall only on change) — [COMPONENTS](https://code.claude.com/docs/en/plugins/components); [LOADING](https://code.claude.com/docs/en/plugins/loading)
- `.mcpb`/`.dxt` bundles can be referenced by path or https URL; Claude Code extracts to `.mcpb-cache/` under the plugin root; bundle `user_config` settable with `--config <server>.<key>=<value>` (v2.1.285+); bundled servers are skipped for project-scope plugins — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference); [COMPONENTS](https://code.claude.com/docs/en/plugins/components); [LOADING](https://code.claude.com/docs/en/plugins/loading)
- "Ship a plugin with your own tool": publish the plugin in a marketplace and have your CLI installer run or print `claude plugin marketplace add <source>` then `claude plugin install <name>@<marketplace>` — [PUBLISH](https://code.claude.com/docs/en/plugins/publish)
- CLI hints (`<claude-code-hint v="1" type="plugin" value="name@marketplace" />` on stderr when `CLAUDECODE` set) only work for plugins in official-name marketplaces, not third-party ones — [HINTS](https://code.claude.com/docs/en/plugins/cli-hints)
- `command` plugin source: a tool on the user's machine prints the plugin dir path; Claude Code shows and asks the user to accept the command; re-run once per session; admins can disable with `disableCommandPluginSources` — [MKT](https://code.claude.com/docs/en/plugins/marketplace-reference); [ORG](https://code.claude.com/docs/en/plugins/org)
- Plugin dependencies: `"name"`, `"name@marketplace"`, or `{name, version (semver range), marketplace}`; cross-marketplace deps need the root marketplace's `allowCrossMarketplaceDependenciesOn`; ranges resolve against `<plugin>--v<version>` tags; conflicting ranges fail install; `claude plugin prune` removes orphaned auto-installed deps — [DEPS](https://code.claude.com/docs/en/plugins/dependencies)
- Hook/server processes run with full user permissions and outside the sandbox; a Bash command running a `bin/` executable and MCP tool calls are subject to permission rules — [SEC](https://code.claude.com/docs/en/plugins/security)

### Inferences
- `bin/` does not help the MCP server: MCP `command` is resolved outside the Bash tool (MCP process env only receives ROOT/DATA exported). So for `vgxness mcp --full`, either the binary is on the user's PATH or the command is an absolute `${CLAUDE_PLUGIN_ROOT}`/`${CLAUDE_PLUGIN_DATA}` path (e.g. a small launcher script under `scripts/`).
- Committing multi-platform Go binaries into the plugin dir would bloat every user's cache copy with all platforms; a launcher that resolves `vgxness` on PATH and otherwise points to install instructions (or downloads a checksummed release into PLUGIN_DATA) is lighter. Self-downloading binaries from a hook is a supply-chain risk and should verify a SHA-256.

### Gaps
- Whether the plugin cache copy preserves executable bits for files committed with `+x` is implied (docs say `chmod +x bin/...`) but not explicitly stated for the copy step.
- Claude Code's support for MCPB platform-specific binary selection was not verified.

---

## 7. Enterprise / managed settings relevant to plugins

### Takeaway
Admins control plugins through managed `strictKnownMarketplaces` (allowlist; alias `allowedMarketplaces`), `blockedMarketplaces`, `extraKnownMarketplaces` (with `autoUpdate`), managed `enabledPlugins` (force-on / block), `disableSideloadFlags`, `disableCommandPluginSources`, `strictPluginOnlyCustomization`, `allowManagedHooksOnly`, `pluginTrustMessage`, `pluginSuggestionMarketplaces`, `syncClaudeAiPlugins`.

### Cited Findings
- Control matrix: `strictKnownMarketplaces` (`[]` blocks everything incl. official; doesn't block `--plugin-dir`), `blockedMarketplaces` (checked first), `syncClaudeAiPlugins` (v2.1.273+), `enabledPlugins` (managed true force-enables, false blocks & hides), `disableSideloadFlags` (rejects `--plugin-dir`, `--plugin-url`, `--agents`, SDK `plugins`, non-SDK `--mcp-config`, `CLAUDE_CODE_PLUGIN_DIRS`), `disableCommandPluginSources`, `allowManagedHooksOnly`, `strictPluginOnlyCustomization` (block non-plugin skills/agents/hooks/MCP), `pluginSuggestionMarketplaces`, `pluginTrustMessage`, `allowedChannelPlugins`, `CLAUDE_CODE_DISABLE_OFFICIAL_MARKETPLACE_AUTOINSTALL`, `allowManagedModsOnly` — [ORG](https://code.claude.com/docs/en/plugins/org)
- Allowlist entry types: `github` (incl. `owner/*` wildcard, v2.1.223+), `git`, `url`, `file`, `directory`, `hostPattern`, `pathPattern`, `skills-dir`. Matching is exact on repo/url + ref + path (an entry without `ref` doesn't cover a source with `ref: "main"`; github vs git URL to same repo don't match) — [ORG](https://code.claude.com/docs/en/plugins/org); [MKT](https://code.claude.com/docs/en/plugins/marketplace-reference)
- Lists are enforced before download (add/install/update/refresh/auto-update) and again at session start for already-installed plugins — [ORG](https://code.claude.com/docs/en/plugins/org)
- Managed `extraKnownMarketplaces` `autoUpdate` locks the user toggle — [ORG](https://code.claude.com/docs/en/plugins/org)
- Under `allowManagedPermissionRulesOnly`, only plugins from an official Anthropic source or a source managed settings allow may pre-approve their own tools via `allowed-tools` (fixed in v2.1.284) — [CHANGELOG](https://code.claude.com/docs/en/changelog)

### Inferences
- For enterprise VGXNESS adopters, the admin snippet is: `strictKnownMarketplaces: [{source:"github", repo:"uzielvgx/vgxness"}]` (exact match, no ref unless users pin one) + `extraKnownMarketplaces.vgxness` + `enabledPlugins["vgxness@vgxness"]: true`.

### Gaps
- None material.

---

## 8. Limitations, security model, version-dependent / recently changed behavior

### Takeaway
Trust is a single generic warning at install; plugins run hooks and MCP servers with full user privileges outside the sandbox. Many plugin behaviors are gated by very recent versions (v2.1.2xx), so a plugin using new fields (e.g. `userConfig.options`) breaks on older clients.

### Cited Findings
- Install-time trust warning text: "Make sure you trust a plugin before installing, updating, or using it. Anthropic does not control what MCP servers, files, or other software are included in plugins…" (admins can append `pluginTrustMessage`) — [SEC](https://code.claude.com/docs/en/plugins/security)
- Official/community marketplace names are only accepted from `github.com/anthropics/`; otherwise "Marketplace is registered from an untrusted source" — [SEC](https://code.claude.com/docs/en/plugins/security)
- The `/plugin` details pane shows a "Will install" list but not what hooks run; users are told to read `hooks/hooks.json`, `.mcp.json`, and `bin/` — [SEC](https://code.claude.com/docs/en/plugins/security)
- Auto-update means reviewed files can change on disk — [SEC](https://code.claude.com/docs/en/plugins/security)
- Installing a plugin also enables it unless `defaultEnabled: false` — [SEC](https://code.claude.com/docs/en/plugins/security)
- Name conflicts: managed > `--plugin-dir` > installed marketplace > skills-dir > synced — [LOADING](https://code.claude.com/docs/en/plugins/loading)
- Recent version gates: `metadata` v2.1.222; archive v2.1.224; command source v2.1.229; `pluginRoot` v2.1.239; `userConfig` `/config` rows v2.1.269, `options` v2.1.271; MCP checks in validate v2.1.281; update scope auto-detect v2.1.281; `plugin configure` and `--config server.key` v2.1.285; mods v2.1.287 — [MANIFEST](https://code.claude.com/docs/en/plugins/manifest-reference); [MKT](https://code.claude.com/docs/en/plugins/marketplace-reference); [CLI](https://code.claude.com/docs/en/plugins/cli-reference); [CHANGELOG](https://code.claude.com/docs/en/changelog)
- v2.1.285: `claude mcp get` now hides command/args/env values of plugin stdio servers — [CHANGELOG](https://code.claude.com/docs/en/changelog)
- v2.1.284: plugin `bin/` dirs that don't exist are no longer added to PATH (Windows fix) — [CHANGELOG](https://code.claude.com/docs/en/changelog)
- v2.1.283: fixed plugins without a version being restored at newest commit instead of installed one when cache files were missing — [CHANGELOG](https://code.claude.com/docs/en/changelog)

### Inferences
- VGXNESS should avoid bleeding-edge-only fields unless it documents a minimum Claude Code version; the core (skills, agents, hooks, .mcp.json, version) is long-stable.

### Gaps
- No documented minimum Claude Code version for the plugin system as a whole was found in the pages read.

---

## Mapping for VGXNESS

### Takeaway
Ship one plugin `vgxness` from a `plugins/vgxness/` subfolder of github.com/uzielvgx/vgxness, with a root `.claude-plugin/marketplace.json` named `vgxness`. The plugin declares the `vgxness mcp --full` stdio server, ships the worker roles as namespaced agents, injects the Manager policy via a `SessionStart` hook (not via the `agent` setting), and treats the `vgxness` binary as a PATH prerequisite checked by a hook. Claude Code's install records, versioned cache, update and uninstall replace the custom receipts/drift/backup installer for this host.

### Cited Findings
- (All supporting facts are in sections 1–7 above; key ones: relative source resolution [MKT](https://code.claude.com/docs/en/plugins/marketplace-reference), version computation [LOADING](https://code.claude.com/docs/en/plugins/loading), `agent` replaces system prompt [SUBAGENTS](https://code.claude.com/docs/en/sub-agents), SessionStart `additionalContext` 10k cap [HOOKS](https://code.claude.com/docs/en/hooks), PLUGIN_DATA deleted on uninstall [CLI](https://code.claude.com/docs/en/plugins/cli-reference), MCP tool naming [COMPONENTS](https://code.claude.com/docs/en/plugins/components), "ship a plugin with your own tool" [PUBLISH](https://code.claude.com/docs/en/plugins/publish).)

### Inferences (recommendations)

**Repo layout**
```text
vgxness/                         # Go repo root = marketplace root
├── .claude-plugin/
│   └── marketplace.json
├── plugins/
│   └── vgxness/                 # plugin root (only this is copied to cache)
│       ├── .claude-plugin/plugin.json
│       ├── .mcp.json
│       ├── hooks/hooks.json
│       ├── scripts/session-start.sh   # checks binary, prints Manager policy (not in bin/, keeps Cowork-compatible)
│       ├── agents/{explore,general,verifier,code-reviewer,manager}.md
│       └── skills/{memory,manager-policy,...}/SKILL.md
├── cmd/ internal/ ...           # Go sources, not part of the plugin
```
Users: `claude plugin marketplace add uzielvgx/vgxness --sparse .claude-plugin plugins` then `claude plugin install vgxness@vgxness`. The `vgxness` CLI can offer a `vgxness setup claude-code` command that just runs these two commands (and optionally enables auto-update guidance) instead of writing files itself.

**marketplace.json (repo root)**
```json
{
  "$schema": "https://anthropic.com/claude-code/marketplace.schema.json",
  "name": "vgxness",
  "description": "VGXNESS durable project memory and orchestration for Claude Code",
  "owner": { "name": "Uziel Vega", "url": "https://github.com/uzielvgx" },
  "plugins": [
    { "name": "vgxness", "source": "./plugins/vgxness", "category": "productivity" }
  ]
}
```
(Do not also set `version` here — keep it only in plugin.json.)

**plugin.json**
```json
{
  "name": "vgxness",
  "displayName": "VGXNESS",
  "version": "X.Y.Z",
  "description": "Durable SQLite/FTS5 project memory (MCP), cloud sync, and a Manager/worker orchestration policy",
  "author": { "name": "Uziel Vega", "url": "https://github.com/uzielvgx" },
  "homepage": "https://github.com/uzielvgx/vgxness",
  "repository": "https://github.com/uzielvgx/vgxness",
  "license": "<SPDX>",
  "keywords": ["memory", "mcp", "orchestration"]
}
```
Pin `version` and bump it in lockstep with CLI releases (`claude plugin tag plugins/vgxness --push` → `vgxness--vX.Y.Z`), because the plugin's prompts reference MCP tool names that are coupled to the binary. Omitting `version` (track commits) would push every commit on the default branch to users.

**.mcp.json**
```json
{
  "mcpServers": {
    "memory": { "command": "vgxness", "args": ["mcp", "--full"] }
  }
}
```
Tools become `mcp__plugin_vgxness_memory__<tool>`; use that form in agent `tools:` lists, hook matchers and permission rules. If PATH resolution proves unreliable (GUI-launched desktop app, Homebrew paths), switch `command` to `${CLAUDE_PLUGIN_ROOT}/scripts/vgxness-mcp.sh` which locates the binary (PATH, `~/go/bin`, `/opt/homebrew/bin`, or `${CLAUDE_PLUGIN_DATA}/bin`) and `exec`s it; log to stderr only.

**Binary dependency**
- Treat `vgxness` as a prerequisite, like LSP plugins do. A `SessionStart` (matcher `startup`) hook in exec form runs `scripts/session-start.sh`, which: if `vgxness` is missing, emits a `systemMessage`/`additionalContext` with install instructions; else runs a version check (`vgxness version` vs a minimum compatible version baked into the plugin) and warns on mismatch.
- Optional fallback: download a checksummed release asset into `${CLAUDE_PLUGIN_DATA}/bin/` (never into PLUGIN_ROOT). Flag as a supply-chain risk; verify SHA-256 and prefer explicit user install.
- Avoid a top-level `bin/` unless you want Claude's Bash tool to call `vgxness` subcommands directly; it makes the plugin uninstallable on claude.ai/Cowork.

**Manager instructions**
- Recommended: `SessionStart` hook (matchers `startup|clear|compact`) printing the Manager policy as `additionalContext`, ideally sourced from the binary (e.g. `vgxness policy --host claude-code`) so policy has one source of truth; keep it under 10,000 characters.
- Also ship `agents/manager.md` so power users can opt into `claude --agent vgxness:manager`, but do **not** set `"agent": "manager"` in plugin `settings.json` by default: it replaces Claude Code's entire default system prompt for every session of every user who enables the plugin.
- Ship worker roles as `agents/explore.md`, `general.md`, `verifier.md`, `code-reviewer.md` → `vgxness:explore`, etc. (namespacing avoids clashing with the built-in Explore/general-purpose agents). Plugin agents can't declare their own hooks/MCP servers/permissionMode, so give them MCP access via `tools` entries naming the plugin's tools.
- Put "how to use memory" guidance in skills (loaded on demand by description) rather than in the always-on context, to keep token cost down (`claude plugin details vgxness` shows the projected always-on cost).

**Data and lifecycle**
- Keep the memory DB in vgxness's own data dir, not `${CLAUDE_PLUGIN_DATA}` (deleted on uninstall by default).
- Drop receipts/drift/backups for this host: Claude Code tracks installs in `installed_plugins.json`, versions the cache, cleans old versions after 14 days, and uninstall removes the plugin cleanly. If cloud-sync needs a token, prefer the binary's own auth; if the plugin must collect it, use `userConfig` with `"sensitive": true` and pass it via `env` `${user_config.sync_token}`.
- Tell users that auto-update is off by default for third-party marketplaces (toggle in `/plugin` → Marketplaces), or have `vgxness update` also run `claude plugin update vgxness@vgxness`.

**CI**
- `claude plugin validate . --strict` and `claude plugin validate ./plugins/vgxness --strict` on every PR; optionally `claude plugin eval ./plugins/vgxness` with a small eval suite.

### Gaps
- Not verified: how reliably the desktop app (GUI launch) inherits the user's shell PATH for plugin MCP `command` resolution — worth testing before relying on bare `vgxness`.
- Not verified: whether `additionalContext` from SessionStart is re-injected on `compact` in a way equivalent to the original (docs say the hook fires on the `compact` matcher; exact placement after compaction wasn't checked).
