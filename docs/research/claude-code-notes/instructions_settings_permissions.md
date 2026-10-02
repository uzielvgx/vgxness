# Claude Code instruction, memory, settings and permission layers (as of Oct 2026, v2.1.28x)

Scope note: primary sources are the official docs at code.claude.com/docs, fetched 2026-10-01. The local CLI on the researcher's machine reports `2.1.283 (Claude Code)`. Many behaviors are version-gated; version notes from the docs are kept inline. Some pages (sub-agents, hooks, mcp, settings-reference) came back as model summaries rather than verbatim text; where a claim rests only on such a summary it is flagged "(summary-sourced)". Third-party sources are labeled.

## 1. CLAUDE.md hierarchy, imports, rules, /memory, /init, plugins

### Takeaway
CLAUDE.md files (managed, user, project, local) plus `.claude/rules/` are concatenated, never override each other, and are delivered as a user message after the system prompt, so they are advisory context, not enforced config. Plugins cannot ship a CLAUDE.md; a plugin's always-on instructions must come from skills, hooks, output styles, an agent, or MCP server instructions.

### Cited Findings
- Locations, in load order (broadest first): Managed policy (`/Library/Application Support/ClaudeCode/CLAUDE.md` on macOS, `/etc/claude-code/CLAUDE.md` on Linux/WSL, `C:\Program Files\ClaudeCode\CLAUDE.md` on Windows); User `~/.claude/CLAUDE.md`; Project `./CLAUDE.md` or `./.claude/CLAUDE.md`; Local `./CLAUDE.local.md` (personal, gitignore it). — [Memory docs](https://code.claude.com/docs/en/memory)
- Files in the directory tree above the working directory load at launch. Files in subdirectories load on demand when Claude reads files in those subdirectories. — [Memory docs](https://code.claude.com/docs/en/memory)
- All discovered files are concatenated, not overriding. Order runs from filesystem root down to cwd, so instructions closer to the launch dir are read last. Within a directory, `CLAUDE.local.md` comes after `CLAUDE.md`. — [Memory docs](https://code.claude.com/docs/en/memory)
- Delivery: "CLAUDE.md content is delivered as a user message after the system prompt, not as part of the system prompt itself", and there is "no guarantee of strict compliance". For system-prompt-level instructions the docs point to `--append-system-prompt`. — [Memory docs](https://code.claude.com/docs/en/memory)
- Size: target under 200 lines per file. A CLAUDE.md up to 4 MiB loads in full; a larger file is skipped. Startup warns when a file is over the recommended length or when files add up past a combined limit. — [Memory docs](https://code.claude.com/docs/en/memory)
- Block-level HTML comments (`<!-- ... -->`) are stripped before injection (kept inside code blocks). — [Memory docs](https://code.claude.com/docs/en/memory)
- `@path` imports: relative paths resolve from the importing file; recursive imports allowed with "a maximum depth of four hops". Imports inside code spans/fences are skipped. Spaces need backslash escaping. Imported files load at launch, so they don't reduce context cost. — [Memory docs](https://code.claude.com/docs/en/memory)
- External imports (outside cwd) in project-level memory files trigger a one-time approval dialog; if declined they stay disabled. User-scope files (`~/.claude/CLAUDE.md`, `~/.claude/rules/`) load imports without the dialog, except in Cowork desktop sessions. — [Memory docs](https://code.claude.com/docs/en/memory)
- `.claude/rules/*.md` are discovered recursively. Rules without `paths` frontmatter load at launch "with the same priority as `.claude/CLAUDE.md`". Rules with `paths:` (YAML list or comma string, globs, brace expansion capped at 1,000 expanded patterns / 4 MiB) load when Claude reads a matching file, not on every tool use. `paths` is the only frontmatter field read. — [Memory docs](https://code.claude.com/docs/en/memory)
- User-level rules `~/.claude/rules/` load before project rules; neither overrides the other, and on conflict "Claude may follow either one". — [Memory docs](https://code.claude.com/docs/en/memory)
- Symlinked rules pointing outside the working directory are treated like external imports (need approval, and then only rules without `paths` load). — [Memory docs](https://code.claude.com/docs/en/memory)
- `claudeMdExcludes` (glob on absolute paths, arrays merge across user/project/local/managed) skips CLAUDE.md/rules files; managed CLAUDE.md cannot be excluded. Managed settings can also inline content via a `claudeMd` key (honored only in managed/policy settings). — [Memory docs](https://code.claude.com/docs/en/memory)
- `--add-dir` directories don't load CLAUDE.md unless `CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1`. — [Memory docs](https://code.claude.com/docs/en/memory)
- AGENTS.md: since v2.1.277, read automatically when there is no `CLAUDE.md`/`.claude/CLAUDE.md`/`CLAUDE.local.md` in cwd or above (user and managed CLAUDE.md and rules don't count for that check). Configurable via `/config` "Project instructions" or `pluginConfigs["agents-md@builtin"].options.instructionFiles` = `claude-md-or-agents-md` (default) | `claude-md-and-agents-md` | `claude-md` | `managed-only`; ignored in project/local settings. — [Memory docs](https://code.claude.com/docs/en/memory)
- `/memory` lists CLAUDE.md, CLAUDE.local.md and other memory locations (including ones that don't exist yet), toggles auto memory, and opens the auto memory folder. `/context` shows which memory files loaded. — [Memory docs](https://code.claude.com/docs/en/memory)
- `/init` generates a starting CLAUDE.md (suggests improvements if one exists). `CLAUDE_CODE_NEW_INIT=1` makes it an interactive multi-phase flow that can also set up skills and hooks. `/doctor prompt-audit` (v2.1.283+) audits instruction files for conflicts and staleness. — [Memory docs](https://code.claude.com/docs/en/memory)
- `InstructionsLoaded` hook fires when CLAUDE.md or rules load; `load_reason` values `session_start`, `nested_traversal`, `path_glob_match`, `include`, `compact`; observational only (summary-sourced). — [Hooks docs](https://code.claude.com/docs/en/hooks)
- Plugins: "A `CLAUDE.md` at the plugin root isn't loaded as context, and `claude plugin validate` warns when it finds one. To include instructions that load into Claude's context, put them in a skill." — [Plugin manifest reference](https://code.claude.com/docs/en/plugins-reference); also [Plugin components](https://code.claude.com/docs/en/plugins/components)
- A managed CLAUDE.md is for behavioral guidance; enforcement (deny rules, sandbox) belongs in managed settings: "CLAUDE.md instructions shape Claude's behavior but are not a hard enforcement layer." — [Memory docs](https://code.claude.com/docs/en/memory)

### Inferences
- A VGXNESS policy written into the user's CLAUDE.md would sit at the same authority level as the user's own instructions and could conflict with them silently ("Claude may pick one arbitrarily"). Writing to user files is also intrusive. CLAUDE.md is the wrong primary home for a product-shipped policy.
- If VGXNESS ever writes a project file, an unscoped `.claude/rules/vgxness.md` is cleaner than editing CLAUDE.md: it's isolated, survives compaction (re-injected like root CLAUDE.md), and is removable. But `.claude/` is a protected path (see section 5), so this needs explicit user approval.

### Gaps
- No documented way for a plugin to add an entry to the CLAUDE.md chain itself. Exact combined-size warning threshold isn't stated.

## 2. Auto memory and how a third-party memory MCP should coexist

### Takeaway
Auto memory is on by default, stores per-repo markdown notes under `~/.claude/projects/<project>/memory/` with a `MEMORY.md` index (first 200 lines or 25KB loaded every session), and can be turned off with `autoMemoryEnabled: false` or `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1`. Its `project` note type overlaps directly with VGXNESS's durable project memory, so the two need an explicit division of labor.

### Cited Findings
- Claude writes four note types, recorded as a `type` frontmatter field: `user` (role, expertise, preferences), `feedback` (corrections, confirmed approaches), `project` (ongoing work, deadlines, decisions not derivable from code/git), `reference` (where external info lives). It skips what's derivable from the codebase and "anything your CLAUDE.md files already say". — [Memory docs](https://code.claude.com/docs/en/memory)
- Toggle: `/memory` toggle writes `autoMemoryEnabled` to `~/.claude/settings.json`. Per project: `{"autoMemoryEnabled": false}` in project settings. Env: `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1`. — [Memory docs](https://code.claude.com/docs/en/memory)
- Location: `~/.claude/projects/<project>/memory/`, `<project>` derived from the git repo, so all worktrees and subdirs share one directory. It's machine-local and not synced. `autoMemoryDirectory` (absolute or `~/`) relocates it; from project settings it's subject to workspace trust. `CLAUDE_CODE_PROJECT_DIR_NAME` (v2.1.234+) overrides the project dir name. — [Memory docs](https://code.claude.com/docs/en/memory)
- Structure: a `MEMORY.md` index (one line per memory) plus one topic file per memory. Only the first 200 lines or 25KB of `MEMORY.md` load at session start; topic files are read on demand. Near or over the limit, Claude Code reminds or errors so Claude rewrites the index. — [Memory docs](https://code.claude.com/docs/en/memory)
- Memory files are excluded from the `cleanupPeriodDays` transcript sweep. Writes to files with frontmatter get a `modified` ISO timestamp (v2.1.214+). — [Memory docs](https://code.claude.com/docs/en/memory)
- "When you ask Claude to remember something ... Claude saves it to auto memory." To add to CLAUDE.md instead, ask explicitly. — [Memory docs](https://code.claude.com/docs/en/memory)
- Main-thread auto memory isn't loaded into non-fork subagents. Subagents can have their own via a `memory: user|project|local` frontmatter field (`~/.claude/agent-memory/<name>/`, `.claude/agent-memory/<name>/`, `.claude/agent-memory-local/<name>/`), which also follows `autoMemoryEnabled`. — [Memory docs](https://code.claude.com/docs/en/memory); [Sub-agents docs](https://code.claude.com/docs/en/sub-agents) (summary-sourced)
- Auto memory is re-injected from disk after compaction. — [Context window docs](https://code.claude.com/docs/en/context-window)
- `--bare` and `--safe-mode` skip auto memory. — [CLI reference](https://code.claude.com/docs/en/cli-reference)
- Plugin `settings.json` honors only `agent` and `subagentStatusLine`, so a plugin can't set `autoMemoryEnabled`. — [Plugin manifest reference](https://code.claude.com/docs/en/plugins-reference)

### Inferences
- Collision risk: when a user says "remember X" or Claude records a project decision, the native behavior goes to auto memory, not VGXNESS. Without guidance, decisions end up split or duplicated across two stores, and the MEMORY.md index (loaded every session) can contradict VGXNESS state.
- Auto memory dedupes against CLAUDE.md but not against MCP-provided memory, so VGXNESS can't count on native dedup.
- Turning auto memory off is the user's call (user authority) and VGXNESS can't do it from a plugin anyway. The workable approach is routing guidance, not disabling.

### Gaps
- No documented hook or API to intercept or redirect auto-memory writes, short of a PreToolUse hook on Write/Edit matching the memory path. That's feasible in principle but undocumented as a pattern, and intrusive.

## 3. Output styles, system-prompt flags, main-thread `agent`, and other injection options

### Takeaway
Output styles, `--append-system-prompt`, the `agent` setting and MCP server instructions operate at system-prompt level and survive compaction. CLAUDE.md, rules and hook context are message-level. Plugins can ship an output style (optionally force-applied), a main-thread `agent`, SessionStart hooks, skills and MCP instructions. No option is both system-level and non-overriding of user choices, except a user-selected (non-forced) output style with `keep-coding-instructions: true`.

### Cited Findings
- Output style format: a Markdown file with optional frontmatter `name`, `description`, `keep-coding-instructions` (default `false`), and `force-for-plugin` (plugin-only, default `false`). Locations: `~/.claude/output-styles`, `.claude/output-styles` (every one between cwd and repo root, closest wins), managed settings dir, and plugins (`output-styles/` or the `outputStyles` manifest key). — [Output styles docs](https://code.claude.com/docs/en/output-styles)
- "Custom output styles leave out Claude Code's built-in software engineering instructions ... unless `keep-coding-instructions` is set to `true`." The style's instructions are sent with every request. — [Output styles docs](https://code.claude.com/docs/en/output-styles)
- `force-for-plugin: true` applies the style "automatically whenever the plugin is enabled, without requiring users to select it. Overrides the user's `outputStyle` setting. If multiple enabled plugins set this, Claude Code uses the first one loaded." — [Output styles docs](https://code.claude.com/docs/en/output-styles)
- Plugin styles appear in `/output-style` as `<plugin>:<name>`. — [Plugin components](https://code.claude.com/docs/en/plugins/components)
- Selection: `/output-style <name>`, `/config`, or `outputStyle` in settings (case-sensitive; mismatch falls back to Default). The command and menus save to `.claude/settings.local.json`. A mid-session switch applies from the next message (v2.1.251+). Style files are read at startup, so edits need a restart. — [Output styles docs](https://code.claude.com/docs/en/output-styles)
- Built-ins: Default, Proactive, Concise (v2.1.237+), Explanatory, Learning. Each non-default built-in keeps the default instructions. — [Output styles docs](https://code.claude.com/docs/en/output-styles)
- Output styles apply to the main conversation and forks, not to other subagents. — [Output styles docs](https://code.claude.com/docs/en/output-styles)
- System prompt flags: `--system-prompt` / `--system-prompt-file` replace the whole default prompt; `--append-system-prompt` / `--append-system-prompt-file` append. All work interactive and `-p`. The prompt is recorded on the first request and reused, including across `--resume`/`--continue`, until compaction; `--system-prompt-snapshot off` rebuilds per request (v2.1.257+). — [CLI reference](https://code.claude.com/docs/en/cli-reference)
- `--append-subagent-system-prompt[-file]` appends to every subagent's prompt, `-p` only (v2.1.205+/v2.1.261+). — [CLI reference](https://code.claude.com/docs/en/cli-reference)
- `agent` setting / `--agent`: "Start every session as a named subagent with its prompt, tools, and model". — [Settings reference](https://code.claude.com/docs/en/settings-reference). When the main thread runs as an agent, the agent's system prompt replaces the default Claude Code system prompt entirely (like `--system-prompt`). CLAUDE.md still loads; `omitClaudeMd` is ignored for the main thread; `initialPrompt` auto-submits the first turn (summary-sourced). — [Sub-agents docs](https://code.claude.com/docs/en/sub-agents)
- Plugin `settings.json` `{"agent": "<plugin-agent>"}` runs that agent as the main thread. Plugin defaults are the lowest layer ("a user's own `agent` in `~/.claude/settings.json` overrides yours"), and if two plugins set it, the last loaded wins. Plugin agents ignore `permissionMode`, `hooks`, `mcpServers` and `initialPrompt`. — [Plugin components](https://code.claude.com/docs/en/plugins/components)
- SessionStart hooks: matchers `startup`, `resume`, `clear`, `compact`, `fork`. They can return `hookSpecificOutput.additionalContext`, and plain stdout is also added to context. Cap is 10,000 characters per field; over that, the content is saved to a file and replaced with the path plus a 2,000-char preview. `additionalContext` reaches Claude as a system reminder and isn't shown as a chat message (summary-sourced). — [Hooks docs](https://code.claude.com/docs/en/hooks). Plugin hooks register when the plugin loads and fire on their events from then on. — [Plugin components](https://code.claude.com/docs/en/plugins/components)
- `UserPromptSubmit` and `SubagentStart` also accept `additionalContext` (10,000-char cap) (summary-sourced). — [Hooks docs](https://code.claude.com/docs/en/hooks)
- MCP server instructions go into the system prompt (summary-sourced). — [MCP docs](https://code.claude.com/docs/en/mcp). Third-party reports quote the client docs as saying Claude Code truncates tool descriptions and server instructions "at 2KB each", with the model receiving the first 2,048 chars plus a `[truncated]` marker. — [anthropics/claude-code #43474 (third-party issue)](https://github.com/anthropics/claude-code/issues/43474); [briandconnelly/skills #184 (third-party)](https://github.com/briandconnelly/skills/issues/184). Observed in this research session: a long MCP server instruction block ended in `… [truncated]`, which is consistent with that cap.
- Skills load on demand. After compaction, the skill listing is not re-injected; invoked skill bodies are, capped at 5,000 tokens per skill and 25,000 total, oldest dropped first. — [Context window docs](https://code.claude.com/docs/en/context-window)

Comparison (derived from the findings above):

| Mechanism | Level | Survives compaction | Replaces default coding prompt | Plugin-shippable | Overrides user choice | User-visible |
|---|---|---|---|---|---|---|
| CLAUDE.md / unscoped rules | user msg | yes (root + unscoped re-injected) | no | no | no (but can conflict) | file on disk |
| Path-scoped rules / nested CLAUDE.md | user msg | only when re-triggered | no | no | no | file |
| Output style, `keep-coding-instructions: true`, user-selected | system | yes | no | yes | no | in `/output-style` |
| Output style, `force-for-plugin` | system | yes | no if keep=true | yes | **yes** (overrides `outputStyle`; first plugin wins) | picker |
| `--append-system-prompt[-file]` | system | yes (recorded; changes apply after compaction) | no | no (launch flag; needs wrapper) | no | shell command |
| `agent` setting (plugin `settings.json`) | system | yes | **yes, entirely** | yes | user's `agent` wins | `@name` header |
| SessionStart hook `additionalContext` | system-reminder in history | re-run with `compact` matcher | no | yes | no | not shown as chat |
| MCP server `instructions` | system | yes (summary-sourced) | no | yes (via plugin MCP) | no | no |
| Skill | on demand | invoked body re-injected (5k tok cap) | no | yes | no | `/skill` |

### Inferences
- `force-for-plugin` collides with user authority, since it silently overrides the user's own `outputStyle` and only one plugin can win. Avoid it as the default.
- A plugin `agent` drops Claude Code's default system prompt (tool guidance, safety, coding conventions). That's a poor fit for a coding Manager policy unless VGXNESS rewrites all of that itself.
- The SessionStart hook is the most compatible always-on channel: it ships in a plugin, re-runs after compaction (`compact` matcher), stacks with the user's CLAUDE.md and output style, and doesn't override anything. Its weakness is message-level authority, the same as CLAUDE.md.
- The 2KB MCP instruction cap means the server instructions can only carry a pointer or summary ("call X at start; policy lives in Y"), with the first ~512 chars carrying the critical rules.

### Gaps
- Official verbatim text on MCP `instructions` limits and compaction behavior wasn't retrievable (the fetch tool returned "not on page"). The 2KB figure relies on third-party issues plus session observation. Whether SessionStart output from `startup` persists without the `compact` matcher after compaction: docs say hook-added context "is summarized with the rest of the conversation", so it is not preserved verbatim.

## 4. Settings hierarchy and keys relevant to a plugin product

### Takeaway
Precedence from highest: Managed, then CLI args (`--settings` and flags), then `.claude/settings.local.json`, then `.claude/settings.json`, then `~/.claude/settings.json`. Array keys like `permissions.allow` merge across scopes. A plugin's own `settings.json` can only set `agent` and `subagentStatusLine`, so every permission, env, hook-config or memory setting must come from the user's or project's settings files.

### Cited Findings
- Precedence order 1–5 as above. Managed comes via `managed-settings.json`, MDM, or server-managed settings from the claude.ai console. Nothing overrides managed except a few security-sensitive "stricter value" keys. — [Settings docs](https://code.claude.com/docs/en/settings)
- "Lists merge instead of overriding" (e.g. `permissions.allow`), with exceptions for `fallbackModel`, `modelPicker`, `availableModels`, `modelSettings`. — [Settings docs](https://code.claude.com/docs/en/settings)
- Env vars aren't a precedence level; resolution is decided per variable/key pair. — [Settings docs](https://code.claude.com/docs/en/settings)
- Settings files are watched and hot-reloaded (permissions, hooks, apiKeyHelper). `model` and `effortLevel` are read only at start. `/status` shows the loaded setting sources. A `ConfigChange` hook fires on settings-file changes. — [Settings docs](https://code.claude.com/docs/en/settings)
- `.claude/settings.local.json` loads from the git repo root even when Claude Code starts in a subdirectory (since v2.1.211). Project `.claude/settings.json` and hooks load from cwd's `.claude/` with no parent fallback. — [Permissions docs](https://code.claude.com/docs/en/permissions)
- Trust-gated in project files: `permissions.allow`, `additionalDirectories`, `extraKnownMarketplaces`, and most `env` apply only after workspace trust. `deny`/`ask` apply immediately. Some keys are ignored in repo files (Scope "User, local, or managed", etc.). — [Settings docs](https://code.claude.com/docs/en/settings)
- Key settings (scope "Any file" unless noted): `agent`, `model`, `outputStyle`, `env`, `autoMemoryEnabled`, `autoMemoryDirectory`, `claudeMdExcludes`, `cleanupPeriodDays` (transcript retention days), `permissions.{allow,ask,deny,defaultMode,additionalDirectories,disableBypassPermissionsMode,blockReadsOutsideWorkingDirectories}`, `allowManagedPermissionRulesOnly` (Managed), `autoMode` (User or managed), `sandbox.{enabled,autoAllowBashIfSandboxed,filesystem,network,network.allowedDomains}`, `enabledPlugins`, `pluginConfigs` (User or managed), `extraKnownMarketplaces`, `strictKnownMarketplaces` (Managed), `skillOverrides`, `hooks`, `disableAllHooks`, `allowManagedHooksOnly` (Managed), `statusLine`, `attribution.{commit,pr,sessionUrl}`, `includeGitInstructions`. — [Settings reference](https://code.claude.com/docs/en/settings-reference) (summary-sourced table)
- `defaultMode: "auto"` doesn't take effect from `.claude/settings.json` or `.claude/settings.local.json`; it must be in `~/.claude/settings.json`. — [Permission modes docs](https://code.claude.com/docs/en/permission-modes)
- Plugin `settings` / `settings.json`: "Only `agent` and `subagentStatusLine` take effect; other keys are dropped at load." `userConfig` values are stored under `pluginConfigs` in the user's settings, and sensitive ones go in the OS keychain. Paths available: `${CLAUDE_PLUGIN_ROOT}`, `${CLAUDE_PLUGIN_DATA}` (`~/.claude/plugins/data/<id>/`, survives updates and is deleted on last uninstall unless `--keep-data`), `${CLAUDE_PROJECT_DIR}`. — [Plugin manifest reference](https://code.claude.com/docs/en/plugins-reference)
- Contradiction: the settings-reference summary described a "PLUGIN.json manifest with a `settings` array defining custom keys". The manifest reference contradicts this (only `agent`/`subagentStatusLine`). Treat the former as an artifact of the summarizer. — [Settings reference](https://code.claude.com/docs/en/settings-reference) vs [Plugin manifest reference](https://code.claude.com/docs/en/plugins-reference)
- `--setting-sources user,project,local` limits sources; `--bare` skips hooks, skills, plugins, MCP, auto memory and CLAUDE.md; `--safe-mode` disables all customizations except managed policy. — [CLI reference](https://code.claude.com/docs/en/cli-reference)

### Inferences
- VGXNESS must document copy-paste settings snippets (permissions, optional `autoMemoryEnabled`) for users. It can't ship them through the plugin. `extraKnownMarketplaces` + `enabledPlugins` in a project's `.claude/settings.json` is the team-onboarding path, and it's trust-gated.
- Durable plugin-local state (e.g. the SQLite DB, if per-user) belongs in `${CLAUDE_PLUGIN_DATA}`, not `${CLAUDE_PLUGIN_ROOT}`. A per-project DB in the repo needs protection from direct edits (see section 5).

### Gaps
- Full settings-reference scopes per key were summarized, not verbatim. Verify any "Any file" scope before relying on it from project settings.

## 5. Permissions: modes, rule syntax, MCP, additional directories, sandboxing, subagents

### Takeaway
Rules are evaluated deny, then ask, then allow (first match wins; specificity doesn't matter). They merge across all scopes, and a deny anywhere beats an allow anywhere. Permissions are enforced by the client, not the model. Since v2.1.283, auto mode (classifier-reviewed) is the built-in starting mode for interactive terminal and VS Code sessions. Subagents inherit `bypassPermissions`, `acceptEdits` and `auto` from the parent.

### Cited Findings
- Modes: `default` (labeled Manual, alias `manual` v2.1.200+; reads only without prompt), `acceptEdits` (edits plus `mkdir/touch/mv/cp` in working dirs), `plan` (read/explore, no source edits; classifier-approved commands when auto is available), `auto` (classifier reviews actions), `dontAsk` (auto-denies anything that would prompt; allow rules still work; for CI), `bypassPermissions` (skips prompts, except the "actions no mode auto-approves"; refused as root/sudo and with `--restricted`). — [Permissions docs](https://code.claude.com/docs/en/permissions); [Permission modes docs](https://code.claude.com/docs/en/permission-modes)
- "With Claude Code v2.1.283 or later, auto mode is the built-in starting permission mode for interactive terminal and VS Code sessions." Earlier, only on Pro/Max/Team. — [Permission modes docs](https://code.claude.com/docs/en/permission-modes)
- Auto mode decision order: (1) allow/ask/deny rules resolve first, with exceptions (protected-path writes still go to the classifier; critical-path `rm` is never auto-approved; `requiresUserInteraction` MCP tools prompt; content-scoped ask rules prompt). (2) Reads and in-workdir edits are auto-approved. (3) Everything else goes to the classifier. It pauses after 3 consecutive or 20 total blocks (not configurable). Boundaries you state in conversation ("don't push") are honored but "can be lost if context compaction removes the message"; for a hard guarantee use a deny rule. — [Permission modes docs](https://code.claude.com/docs/en/permission-modes)
- Never auto-approved in any mode: explicit ask-rule matches, org `ask` connector tools, `AskUserQuestion` and MCP tools marked `requiresUserInteraction`, critical-path `rm/rmdir`, cross-session messaging safeguards, and outside-workdir reads under `blockReadsOutsideWorkingDirectories`. — [Permission modes docs](https://code.claude.com/docs/en/permission-modes)
- Protected paths (writes never auto-approved except bypass): dirs `.git`, `.config/git`, `.vscode`, `.idea`, `.husky`, `.cargo`, `.devcontainer`, `.yarn`, `.mvn`, `.claude` (except `.claude/worktrees`), `--plugin-dir` dirs. Files include shell rc files, `.gitconfig`, `.npmrc`, `.mcp.json`, `.claude.json`, etc. Allow rules such as `Edit(.claude/**)` do not pre-approve them. — [Permission modes docs](https://code.claude.com/docs/en/permission-modes)
- Rule order: "deny, then ask, then allow. The first match in that order determines the outcome, and rule specificity doesn't change the order." A bare-name deny (`Bash`) removes the tool from context; a scoped deny blocks matching calls. — [Permissions docs](https://code.claude.com/docs/en/permissions)
- Bash patterns: `*` anywhere; a trailing ` *` also matches the bare command; the space matters (`Bash(ls *)` doesn't match `lsof`); `:*` suffix equals ` *`. Rules match command text, so `git -C . push` isn't matched by `Bash(git push *)`. — [Permissions docs](https://code.claude.com/docs/en/permissions)
- Tool-name globs: deny/ask accept `"*"` and `"mcp__*"`. Allow globs only after a literal `mcp__<server>__` prefix (e.g. `mcp__puppeteer__*`); unanchored allow globs are skipped with a warning. — [Permissions docs](https://code.claude.com/docs/en/permissions)
- MCP: `mcp__server` and `mcp__server__*` match all tools of a server; `mcp__server__tool` matches one. Plugin-bundled servers are named `mcp__plugin_<plugin>_<server>__<tool>`; a matcher on the bare server key "never fires for a plugin-bundled server". — [Permissions docs](https://code.claude.com/docs/en/permissions); [MCP docs](https://code.claude.com/docs/en/mcp)
- `Agent(Name)` rules control subagents (e.g. deny `Agent(Explore)`; `Agent(fork)` deny blocks forks). — [Permissions docs](https://code.claude.com/docs/en/permissions); [Sub-agents docs](https://code.claude.com/docs/en/sub-agents)
- PreToolUse hooks run before the prompt and can deny/ask/allow, but can't bypass deny or ask rules. An exit-2 hook blocks even when an allow rule matches. — [Permissions docs](https://code.claude.com/docs/en/permissions)
- Working dirs: `--add-dir`, `/add-dir`, `permissions.additionalDirectories`. Settings-file additional dirs grant file access only (no config). Flag-added dirs also load skills, commands, agents and `enabledPlugins`/`extraKnownMarketplaces`. `/cd` moves the session and applies the new dir's config (v2.1.246+). — [Permissions docs](https://code.claude.com/docs/en/permissions)
- Workspace trust: project `permissions.allow` and `additionalDirectories` apply only after the trust dialog; `-p`/SDK never shows it, and those rules aren't used there. — [Permissions docs](https://code.claude.com/docs/en/permissions)
- Sandboxing: OS-level (Seatbelt on macOS; bubblewrap-based packages on Linux/WSL2; no native Windows). It covers only Bash, PowerShell and Monitor commands and their children; Read/Edit/Write and MCP servers are governed by permissions, not the sandbox. Default: write to cwd, added dirs and per-user TMPDIR; read everywhere except denied paths (credentials readable unless `sandbox.credentials`/`denyRead`). Network goes through a proxy with no domains pre-allowed (prompts; `allowedDomains`, `WebFetch(domain:)` allow rules merge in; `strictAllowlist`). Auto-allow mode runs sandboxed commands without prompts but still honors deny, content-scoped ask, and critical paths. `dangerouslyDisableSandbox` retry escape hatch can be disabled with `allowUnsandboxedCommands: false`. Sandbox-protected paths include `.claude` settings, skills, agents, commands and hooks dirs, `.mcp.json`, and `~/.claude`. — [Sandboxing docs](https://code.claude.com/docs/en/sandboxing)
- Subagent permissions: if the main conversation is `bypassPermissions`, `acceptEdits` or `auto`, the subagent runs in that mode and its own `permissionMode` is ignored. If the main conversation is `default`, `dontAsk` or `plan`, the subagent uses its own `permissionMode`, except that a declared `bypassPermissions` is replaced by the parent's mode. Plugin subagents ignore `permissionMode`. Subagents share the parent's sandbox config. — [Sub-agents docs](https://code.claude.com/docs/en/sub-agents) (summary-sourced); [Sandboxing docs](https://code.claude.com/docs/en/sandboxing); [Plugin components](https://code.claude.com/docs/en/plugins/components)

### Inferences
- With auto mode as the default, VGXNESS MCP tool calls not covered by allow rules will be classifier-reviewed. Tools that mutate durable memory or plans may get blocked or flagged. Explicit allow rules for read-only VGXNESS tools reduce friction, and `requiresUserInteraction` on genuinely consent-requiring tools guarantees a human prompt in every mode.
- "Single writer" can't rely on prompt instructions alone. Use deny rules on direct edits to the DB file plus PreToolUse hooks (plugin-shippable) for anything rule syntax can't express.

### Gaps
- Exact Linux sandbox package names weren't extracted. The read-only Bash command list wasn't extracted.

## 6. Context management: compaction, what survives, /context, window sizes

### Takeaway
Compaction replaces history with a summary. The system prompt and output style persist; root CLAUDE.md, unscoped rules, auto memory and the plan-mode plan are re-injected from disk; path-scoped rules, nested CLAUDE.md and earlier hook-added context are summarized away unless re-triggered or re-run by a `compact`-matched SessionStart hook.

### Cited Findings
- Survival table: system prompt and output style still apply; project-root CLAUDE.md and unscoped rules re-injected; auto memory re-injected; fresh git status; plan-mode plan re-injected; path rules and nested CLAUDE.md reload on matching reads; up to 5 most recently modified read or edited files re-read (over 5,000 tokens becomes a reference); invoked skills re-injected (5k/skill, 25k total); "Context that hooks added earlier: Summarized with the rest of the conversation"; SessionStart hooks matching `compact` re-run and their output is added. — [Context window docs](https://code.claude.com/docs/en/context-window)
- The skill listing isn't re-injected after `/compact`. — [Context window docs](https://code.claude.com/docs/en/context-window)
- Auto-compact runs as the limit nears. `/compact <focus>`, `/rewind` summarize-from/up-to, `/autocompact <tokens>` and `--autocompact` (v2.1.221+) adjust it. `/context` gives a live breakdown, including which memory files loaded. — [Context window docs](https://code.claude.com/docs/en/context-window); [CLI reference](https://code.claude.com/docs/en/cli-reference)
- Window sizes: the interactive explainer uses a 200K window. Fable, Sonnet 5+, Opus 4.6+ and Sonnet 4.6 support 1M. Sonnet 5.5 and Sonnet 5 always run 1M with no `[1m]` variant. Thresholds per model are documented in model-config. — [Context window docs](https://code.claude.com/docs/en/context-window)
- System prompt flags: the recorded prompt is reused until compaction, so changed flag text takes effect after compaction or in a new conversation. — [CLI reference](https://code.claude.com/docs/en/cli-reference)
- PreCompact (can block) and PostCompact (observational) hooks, matcher `manual`/`auto` (summary-sourced). — [Hooks docs](https://code.claude.com/docs/en/hooks)

### Inferences
- A Manager policy injected only at `startup` decays after the first compaction. The SessionStart hook must match `startup|resume|clear|compact`, or the policy must live at system level (output style, MCP instructions).
- Plans in `docs/` aren't the plan-mode plan, so they aren't auto re-injected. VGXNESS should re-surface the active plan's pointer via the compact-matched hook or the MCP.

### Gaps
- Exact auto-compact thresholds per model weren't extracted (they're on the model-config page).

## Mapping for VGXNESS

### Takeaway
Primary: deliver the Manager policy through a plugin SessionStart hook (matchers `startup|resume|clear|compact`) that injects a compact policy (target 2–4K chars, hard cap 10K), backed by a 2KB-safe MCP `instructions` summary, skills for the detailed protocols, and PreToolUse hooks plus user-added permission rules for anything that must be enforced. Opt-in fallback for users who want system-prompt authority: a plugin output style "VGXNESS Manager" with `keep-coding-instructions: true`, not forced. Don't use `force-for-plugin`, the plugin `agent` setting, or writes to the user's CLAUDE.md as the default path.

### Cited Findings
- SessionStart `additionalContext` (10,000-char cap), re-run on `compact`, shippable in plugin `hooks/hooks.json`, fires once the plugin loads. — [Hooks docs](https://code.claude.com/docs/en/hooks); [Plugin components](https://code.claude.com/docs/en/plugins/components); [Context window docs](https://code.claude.com/docs/en/context-window)
- Output styles persist through compaction and can keep the coding prompt; `force-for-plugin` overrides the user's `outputStyle`. — [Output styles docs](https://code.claude.com/docs/en/output-styles); [Context window docs](https://code.claude.com/docs/en/context-window)
- Plugin `agent` replaces the default system prompt; the user's `agent` overrides plugin defaults. — [Sub-agents docs](https://code.claude.com/docs/en/sub-agents); [Plugin components](https://code.claude.com/docs/en/plugins/components)
- Plugins can't ship CLAUDE.md or permission/env/memory settings. — [Plugin manifest reference](https://code.claude.com/docs/en/plugins-reference)
- Output styles and main-thread auto memory don't reach non-fork subagents; `SubagentStart` supports `additionalContext`. — [Output styles docs](https://code.claude.com/docs/en/output-styles); [Memory docs](https://code.claude.com/docs/en/memory); [Hooks docs](https://code.claude.com/docs/en/hooks)
- MCP instructions are truncated around 2,048 chars (third-party plus observed). — [anthropics/claude-code #43474](https://github.com/anthropics/claude-code/issues/43474)

### Inferences
**Recommended mechanism (Manager policy into the main thread)**
1. `hooks/hooks.json` in the VGXNESS plugin: a SessionStart command hook, matcher `startup|resume|clear|compact`, that prints JSON `additionalContext` with the policy (authorization, scope, delegation, single writer, durable plans in `docs/`, delivery labels) plus a live pointer: the active plan path and VGXNESS project id from the MCP/DB. Keep it well under 10K so it's never spilled to a file. The policy should begin with a precedence clause: "explicit user instructions in chat and the user's CLAUDE.md take precedence; this policy fills gaps". That respects user authority and avoids the "Claude picks one arbitrarily" conflict.
2. MCP server `instructions` of 2KB or less: first ~512 chars state "VGXNESS is the durable project memory; read state via X at session start; Manager policy is injected at session start; never write the DB except via VGXNESS tools." It's system-level, survives compaction, and holds a minimal policy even if hooks are disabled (`disableAllHooks`, `--bare`).
3. Skills (e.g. `vgxness:delegate`, `vgxness:plan`, `vgxness:deliver`) carry the long procedures. Put the critical rules at the top, since only 5K tokens per skill is re-injected after compaction.
4. A `SubagentStart` hook injects a short worker contract (single writer: subagents report back, the Manager writes) into delegated subagents, which otherwise miss the main-thread policy and output style.
5. Enforcement: PreToolUse hooks (plugin-shipped) block direct Write/Edit/Bash access to the VGXNESS DB path and, if desired, block VGXNESS write tools when called from a subagent. Check the hook input's agent fields, which needs verification.
6. Fallback / power-user option: ship `output-styles/manager.md` with `keep-coding-instructions: true`, without `force-for-plugin`. The user opts in via `/output-style vgxness:manager` and gets system-prompt authority plus guaranteed persistence. Document the trade-off: it replaces the user's chosen style, and only one style can be active. For scripted/headless use, document `--append-system-prompt-file` via a `vgxness` launcher.
7. Avoid: plugin `settings.json` `agent` (drops Claude Code's coding/safety prompt, and the user's `agent` silently overrides it); `force-for-plugin` (overrides user choice, first plugin wins); auto-editing CLAUDE.md. If a project-file anchor is wanted, offer an explicit `/vgxness:init` that creates `.claude/rules/vgxness.md` (unscoped, so it's re-injected after compaction) only after user approval. `.claude/` is protected, so a prompt appears anyway.

**Coexistence with auto memory and CLAUDE.md**
- Division of labor stated in the policy: VGXNESS stores project-scoped durable state (decisions, plans, task status, architecture facts, delivery records), which is shared, queryable and tenant-scoped. Auto memory keeps the personal `user`/`feedback` types (how this user likes to work). Instruct Claude: "When recording a project decision or plan state, write it to VGXNESS, not auto memory; don't mirror VGXNESS content into MEMORY.md." Auto memory dedupes only against CLAUDE.md, so this has to be said explicitly.
- Never disable auto memory from VGXNESS (it can't from a plugin anyway). Document an opt-in snippet for users who want VGXNESS as the single project store: `{"autoMemoryEnabled": false}` in `.claude/settings.local.json` (personal) or `.claude/settings.json` (team).
- Treat the user's CLAUDE.md as higher authority than the Manager policy. Don't restate CLAUDE.md content in VGXNESS memory.
- After compaction, auto memory and root CLAUDE.md come back from disk. VGXNESS's equivalent "comes back" only via the compact-matched SessionStart hook and MCP instructions, so the hook should re-surface a 1–3 line state digest (active plan, current scope, writer).

**Permission rules a plugin user would add** (replace `<srv>` with the actual server key; plugin servers are named `mcp__plugin_vgxness_<srv>__<tool>`)
```json
{
  "permissions": {
    "allow": [
      "mcp__plugin_vgxness_<srv>__memory_read*",
      "mcp__plugin_vgxness_<srv>__plan_get*",
      "mcp__plugin_vgxness_<srv>__search*"
    ],
    "ask": [
      "mcp__plugin_vgxness_<srv>__memory_delete*",
      "mcp__plugin_vgxness_<srv>__project_reset*"
    ],
    "deny": [
      "Edit(**/.vgxness/**)",
      "Write(**/.vgxness/**)",
      "Bash(sqlite3 *vgxness*)"
    ]
  }
}
```
- Allow globs must be anchored on the literal `mcp__plugin_vgxness_<srv>__` prefix; unanchored ones are ignored. A blanket `mcp__plugin_vgxness_<srv>__*` allow is acceptable only if no VGXNESS tool is destructive.
- Mark truly consent-requiring tools with `_meta["anthropic/requiresUserInteraction"]` server-side. They then prompt in every mode, including auto and bypass, and are denied in `dontAsk`.
- The Bash deny is best-effort (rules match command text). Back it with a plugin PreToolUse hook. If the DB lives in `${CLAUDE_PLUGIN_DATA}` rather than the repo, it's outside the working dirs and naturally harder to touch.
- Optional `Agent(...)` deny rules to keep delegation to approved worker agents.
- Put personal rules in `~/.claude/settings.json` or `.claude/settings.local.json`. Team rules go in `.claude/settings.json`, where `allow` waits for workspace trust but `deny`/`ask` apply immediately.

### Gaps
- Not verified: whether PreToolUse hook input reliably identifies the calling subagent (needed to enforce "single writer" mechanically). Exact precedence or interaction when both a SessionStart `additionalContext` and a user CLAUDE.md give conflicting rules (docs only say Claude may pick either). Official verbatim statement of the MCP instructions cap. All three should be checked against the hooks reference and a live test on v2.1.283 before finalizing the design.
