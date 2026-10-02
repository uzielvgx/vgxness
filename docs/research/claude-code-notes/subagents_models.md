# Claude Code subagents and model configuration (state as of 2026-10-01, Claude Code ~v2.1.287)

Primary sources (all official, fetched 2026-10-01):
- SA = https://code.claude.com/docs/en/sub-agents
- MC = https://code.claude.com/docs/en/model-config
- AT = https://code.claude.com/docs/en/agent-teams
- TR = https://code.claude.com/docs/en/tools-reference
- CO = https://code.claude.com/docs/en/costs
- FM = https://code.claude.com/docs/en/fast-mode
- PR = https://code.claude.com/docs/en/plugins-reference
- WT = https://code.claude.com/docs/en/worktrees
- SR = https://code.claude.com/docs/en/settings-reference
- CL = https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md (top entry: 2.1.287)

No third-party sources were needed; every claim below comes from the official docs or changelog.

---

## 1. Subagent file format, frontmatter fields, locations, precedence, plugin restrictions

### Takeaway
A subagent is a Markdown file: YAML frontmatter, then a body that becomes its whole system prompt (it does not get the Claude Code system prompt). Only `name` and `description` are required. Definitions come from five scopes: managed > `--agents` > project `.claude/agents/` > user `~/.claude/agents/` > plugin. Plugin agents silently ignore `hooks`, `mcpServers` and `permissionMode` (and `initialPrompt`).

### Cited Findings
**File format**
- The body is the system prompt. "Subagents receive only this system prompt plus basic environment details like the working directory, not the Claude Code system prompt." — [SA](https://code.claude.com/docs/en/sub-agents)
- Only `name` and `description` are required. Field names are camelCase and must match exactly, because "Claude Code ignores a field it doesn't recognize without reporting an error." — [SA](https://code.claude.com/docs/en/sub-agents)
- Files that are skipped without any error shown in the session: no `name` (treated as documentation); an opening `---` that isn't line 1; a `name` that starts with `-` or contains `:` (`:` is reserved for plugin scoping, and has been rejected since v2.1.218); a `name` with no `description`; YAML that doesn't parse. Check with `--debug`, or run `claude plugin validate .claude/agents` (v2.1.233+). — [SA](https://code.claude.com/docs/en/sub-agents)
- Hot reload: `~/.claude/agents/` and `.claude/agents/` are watched, and edits apply within seconds. A restart is still needed when the session created the scope's first `agents` dir, for `--add-dir` agent dirs, and under `--disable-slash-commands`. — [SA](https://code.claude.com/docs/en/sub-agents)
- In `-p` mode, `--append-subagent-system-prompt` (v2.1.205+) and `--append-subagent-system-prompt-file` (v2.1.261+) append text to every non-fork subagent's prompt. — [SA](https://code.claude.com/docs/en/sub-agents)

**Frontmatter fields (complete table from SA)**
| Field | Semantics |
|---|---|
| `name` | Unique id. Hooks receive it as `agent_type`. The filename doesn't need to match. No `:` |
| `description` | Used by Claude to decide when to delegate |
| `tools` | Allowlist, as a comma string or YAML list. If omitted, the agent inherits every tool available to subagents. If no entry resolves, the launch fails with "Agent would be spawned with zero tools" |
| `disallowedTools` | Denylist. An entry with a specifier such as `Bash(git push *)` still removes the whole tool |
| `model` | `sonnet`, `opus`, `haiku`, `fable`, a full ID (e.g. `claude-opus-5-5`), or `inherit` |
| `permissionMode` | `default`, `acceptEdits`, `auto`, `dontAsk`, `bypassPermissions`, `plan`; `manual` is an alias of `default` (v2.1.200+). Ignored for plugin agents |
| `maxTurns` | Cap on agentic turns. Output is returned marked partial (v2.1.246+) and can be resumed |
| `skills` | Skills preloaded with full content. This does not restrict which skills the agent can access |
| `mcpServers` | Name references or inline server definitions. Ignored for plugin agents |
| `hooks` | Hooks scoped to this subagent. Ignored for plugin agents |
| `memory` | `user`, `project` or `local` persistent memory |
| `background` | `true` forces background |
| `omitClaudeMd` | Skips user/project/local CLAUDE.md, but managed policy still loads (v2.1.271+). Ignored when the agent runs as the main thread |
| `effort` | `low`, `medium`, `high`, `xhigh`, `max`. Overrides the session effort while active |
| `isolation` | `worktree` gives the agent a temporary git worktree |
| `color` | `red`, `blue`, `green`, `yellow`, `purple`, `orange`, `pink`, `cyan` |
| `initialPrompt` | First user turn, auto-submitted only when the agent runs as the main session agent. Ignored for plugin agents |
| `experimental.cacheTtl` | `5m` or `1h` prompt cache TTL for this agent's requests (v2.1.248+) |
— [SA](https://code.claude.com/docs/en/sub-agents)

**Locations and precedence**
- Priority order: 1 managed settings (`.claude/agents/` inside the managed settings dir), 2 `--agents` CLI JSON, 3 project `.claude/agents/`, 4 user `~/.claude/agents/`, 5 plugin `agents/`. On a name collision the higher priority wins. — [SA](https://code.claude.com/docs/en/sub-agents)
- Project agents are found by walking up from the cwd, and the closest definition wins. Both project and user dirs are scanned recursively, but subfolders don't affect identity (only `name` does). Duplicate names inside one tree are resolved by "filesystem read order rather than a documented precedence", and `/doctor` reports them. `--add-dir` directories also contribute their `.claude/agents/`. — [SA](https://code.claude.com/docs/en/sub-agents)
- Plugin agent ids are scoped by subfolder: `agents/review/security.md` in plugin `my-plugin` becomes `my-plugin:review:security`. — [SA](https://code.claude.com/docs/en/sub-agents)
- `--agents` JSON: each top-level key is an agent name, and `prompt` holds the body (it may be empty since v2.1.281). It accepts every frontmatter field except `color` and `experimental`, which are ignored. With `-p` it also accepts a file path (v2.1.281+); interactive sessions refuse a path. — [SA](https://code.claude.com/docs/en/sub-agents)

**Plugin restrictions**
- "For security reasons, plugin subagents don't support the `hooks`, `mcpServers`, or `permissionMode` frontmatter fields. These fields are ignored when loading agents from a plugin." The documented workaround is to copy the file into `.claude/agents/` or `~/.claude/agents/`, or to add session-wide `permissions.allow` rules. — [SA](https://code.claude.com/docs/en/sub-agents)
- `initialPrompt` is also ignored for plugin subagents. — [SA](https://code.claude.com/docs/en/sub-agents)
- In a plugin manifest, `agents` takes `.md` file paths only (directories aren't accepted) and replaces the default `agents/` scan. A plugin's `settings.json` (or the `settings` key) supports only `agent` and `subagentStatusLine`, so a plugin can set a default main-thread agent. A `CLAUDE.md` at the plugin root is not loaded. — [PR](https://code.claude.com/docs/en/plugins-reference)
- Inline MCP servers and frontmatter hooks in project `.claude/agents/` files require workspace trust of the agent file's folder (MCP since v2.1.238, hooks since v2.1.218). Agents from user scope, `--agents` and managed scope load without the trust check. — [SA](https://code.claude.com/docs/en/sub-agents)

### Inferences
- Because unknown fields are ignored silently, a typo such as `disallowed_tools` would leave a "read-only" agent fully writable. Validate VGXNESS agent files in CI (with `claude plugin validate` plus a custom key check).
- If VGXNESS ships its roles as a plugin, it loses `permissionMode`, `hooks` and `mcpServers` on every role. Enforcement would then have to move to `tools`/`disallowedTools` and to session-level settings and hooks. Project or user scope keeps all fields.

### Gaps
- The docs give no formal JSON schema for frontmatter, and no list of tools that count as "read-only" for Explore beyond "Write and Edit are denied".

---

## 2. How delegation works: selection, invocation, context, return, nesting, parallel/background, resume, SendMessage, teams

### Takeaway
Claude delegates either automatically (by matching `description`) or explicitly (natural language, a guaranteed @-mention, or `--agent` for the whole session) through the `Agent` tool (formerly `Task`). A non-fork subagent starts with a fresh context and returns only its final report. Nesting is allowed up to depth 3, with 20 concurrent subagents by default. In interactive sessions fork mode is on by default, so subagents run in the background. Completed subagents can be resumed with `SendMessage`. Agent teams remain experimental and opt-in.

### Cited Findings
**Selection and invocation**
- Automatic delegation uses "the task description in your request, the `description` field..., and current context". Phrases like "use proactively" encourage delegation. Combined descriptions above 15,000 tokens trigger a startup warning, but every agent still loads. — [SA](https://code.claude.com/docs/en/sub-agents)
- Explicit invocation: natural language ("Use the test-runner subagent…") leaves the decision to Claude. An @-mention (`@"code-reviewer (agent)"`, `@agent-<name>`, `@agent-plugin:name`) "guarantees the subagent runs for one task", but Claude still writes the task prompt. For a whole session, use `--agent <name>` or the `agent` setting. — [SA](https://code.claude.com/docs/en/sub-agents)
- The Task tool was renamed `Agent` in v2.1.63, and `Task(...)` still works as an alias. — [SA](https://code.claude.com/docs/en/sub-agents)
- An Agent call without `subagent_type` fails ("subagent_type is required") when there is no `general-purpose` to fall back on. — [SA](https://code.claude.com/docs/en/sub-agents)
- "Launching the subagent doesn't itself prompt for permission. Claude Code checks the subagent's own tool calls against your permission rules as it runs." — [TR](https://code.claude.com/docs/en/tools-reference)

**What the subagent receives (non-fork)**
- It starts with: its own system prompt plus environment details; the delegation (task) message; the full CLAUDE.md hierarchy (skipped by Explore and Plan, and dropped by `omitClaudeMd`); a git status snapshot (Explore and Plan skip it, and this can't be configured per agent); preloaded `skills`; and a sibling roster for `SendMessage` (v2.1.206+). — [SA](https://code.claude.com/docs/en/sub-agents)
- It does not receive: conversation history, already-invoked skills, files already read, the output style, or the main conversation's auto memory. Its context window is sized by its own model. — [SA](https://code.claude.com/docs/en/sub-agents)
- Each subagent starts in the main cwd, and `cd` does not persist between Bash calls. — [SA](https://code.claude.com/docs/en/sub-agents)

**What it returns**
- "The parent doesn't see the subagent's intermediate tool calls or outputs, only that final result." — [TR](https://code.claude.com/docs/en/tools-reference)
- Output scanning (v2.1.210+) escapes imitations of `<system-reminder>`, `Human:` and `Assistant:` lines, and prepends a `[harness: subagent output matched instruction-shaped pattern(s):` marker. Reports arrive under a header saying the instructions inside them carry no user authority. — [SA](https://code.claude.com/docs/en/sub-agents)
- In auto mode, subagents report back through a dedicated `SubagentHandback` tool, which the classifier reviews (v2.1.271+). The run ends once the report is handed back. — [TR](https://code.claude.com/docs/en/tools-reference); [CL](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md)
- API errors: since v2.1.199, a failed subagent reports the failure instead of returning error text as if it were findings, and background failures include the last output. — [SA](https://code.claude.com/docs/en/sub-agents)

**Tool inheritance and filters**
- If neither `tools` nor `disallowedTools` is set, the agent inherits all tools available to subagents. With `tools` only, it gets just those. With `disallowedTools` only, it gets the parent's tools minus the listed ones. With both, `disallowedTools` is applied first. — [TR](https://code.claude.com/docs/en/tools-reference); [SA](https://code.claude.com/docs/en/sub-agents)
- Always removed: `Agent` (at the depth limit), `AskUserQuestion`, `EndConversation`, `EnterPlanMode`, `ExitPlanMode` (unless `permissionMode: plan`), `ScheduleWakeup`, `WaitForMcpServers`, `Workflow`. — [SA](https://code.claude.com/docs/en/sub-agents)
- Background subagents keep only these built-in tools: Read, Grep, Glob, LSP, Bash, PowerShell, Edit, Write, NotebookEdit, WebFetch, WebSearch, TodoWrite, Skill, ToolSearch, EnterWorktree, ExitWorktree, Monitor, TaskStop, SendMessage, Artifact (+SubagentHandback). They keep all MCP tools. "the same definition can resolve to different tools in the foreground and the background." — [SA](https://code.claude.com/docs/en/sub-agents)
- MCP patterns in `tools`/`disallowedTools`: `mcp__<server>` or `mcp__<server>__*`; `mcp__*` in `disallowedTools` removes all MCP tools. — [SA](https://code.claude.com/docs/en/sub-agents)
- Task tools (TaskCreate etc.) are on by default only for older models (Claude 3.x, Opus 4–4.7, Sonnet 4–4.6, Haiku 4.5), from v2.1.268. On Opus 5.5 / Sonnet 5.5 they're off unless `CLAUDE_CODE_ENABLE_TODO_TOOLS=1`. Subagents get them only if the session has them. — [TR](https://code.claude.com/docs/en/tools-reference)

**Nesting and concurrency**
- Subagents can spawn subagents up to 3 layers below main by default (v2.1.219+). `CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH` changes this, and `1` disables nesting. History: v2.1.172–216 allowed 5 fixed layers; v2.1.217–218 defaulted to 1. To keep a particular agent from spawning, omit `Agent` from its `tools`. — [SA](https://code.claude.com/docs/en/sub-agents)
- The `Agent(type1, type2)` allowlist syntax in `tools` works only for an agent running as the main thread with `--agent`. Inside a subagent definition the parenthesized list is ignored. — [SA](https://code.claude.com/docs/en/sub-agents)
- The concurrency limit defaults to 20 running subagents (`CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS`, v2.1.217+). It isn't enforced when ultracode is active, and resumes and `/subtask` forks can exceed it. There is no cap on the total spawned per session. — [SA](https://code.claude.com/docs/en/sub-agents)

**Foreground and background; forks**
- The first matching rule decides: (1) a subagent spawned by an in-process teammate runs in the foreground; (2) `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1` forces the foreground; (3) with fork mode on (the interactive default), everything runs in the background and Claude can't request the foreground; (4) with fork mode off (the `-p` and SDK default), the background is the default, but Claude uses the foreground when it needs the result. `background: true` forces the background. — [SA](https://code.claude.com/docs/en/sub-agents)
- Background permission prompts surface in the main session, naming the subagent (v2.1.186+). Esc denies that one call. A session-lasting grant given to a background subagent applies to the whole session, including main. — [SA](https://code.claude.com/docs/en/sub-agents)
- Forks (`/subtask`, or the `fork` subagent type) inherit the full conversation, system prompt, tools and model, and share the prompt cache. They can't spawn further forks. `CLAUDE_CODE_FORK_SUBAGENT=0/1` toggles fork mode, and `Agent(fork)` in deny blocks forks. — [SA](https://code.claude.com/docs/en/sub-agents)

**Resume and SendMessage**
- Completed subagents return an agent ID. Claude resumes one with `SendMessage` using `to: <id or name>`, and the subagent keeps its full history, original tool set and warm cache. Explore and Plan are one-shot (no ID). Agents the user stopped manually don't auto-resume. Transcripts live at `~/.claude/projects/{project}/{sessionId}/subagents/agent-{agentId}.jsonl` and are cleaned after `cleanupPeriodDays` (30). — [SA](https://code.claude.com/docs/en/sub-agents)
- `SendMessage` does not require agent teams. Since v2.1.198, a subagent treats messages from its launcher as task direction, but "no message from any agent counts as your approval for a pending permission prompt, and no agent message can change a subagent's permission settings, CLAUDE.md, or configuration." — [SA](https://code.claude.com/docs/en/sub-agents)
- Claude may name subagents on its own (via the Agent `name` param), which makes them addressable. — [SA](https://code.claude.com/docs/en/sub-agents)

**Agent teams (experimental)**
- "Agent teams are experimental and disabled by default", enabled with `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`. They work in interactive sessions only; under `-p` or the SDK no teammates are spawned. — [AT](https://code.claude.com/docs/en/agent-teams)
- Gotcha: once teams are enabled, any Agent call with a `name` from the main conversation launches a teammate unless it is a fork or passes `isolation` on the call. Frontmatter `isolation` does not prevent this, and such a teammate runs in the main cwd. — [AT](https://code.claude.com/docs/en/agent-teams); [SA](https://code.claude.com/docs/en/sub-agents)
- Structure: a lead, teammates (separate instances), a shared task list with file-locked claiming, and a mailbox at `~/.claude/teams/{team}/inboxes/{agent}.json`. One team per session, no nested teams, and in-process teammates can't be restored by `/resume`. Teammates start with the lead's permission mode (except `dontAsk`), and per-teammate modes can't be set at spawn. — [AT](https://code.claude.com/docs/en/agent-teams)
- Subagent definitions can serve as teammates. `tools`, `model` and the body apply (for in-process teammates the body is appended to the default prompt). `skills` is NOT applied, and in-process teammates ignore `mcpServers`. — [AT](https://code.claude.com/docs/en/agent-teams)
- Quality-gate hooks: `TeammateIdle`, `TaskCreated`, `TaskCompleted` (exit 2 blocks or sends feedback). — [AT](https://code.claude.com/docs/en/agent-teams)

### Inferences
- The VGXNESS Manager → worker pattern (delegate, get a summary, decide) maps directly to plain subagents. Agent teams add peer-to-peer coordination that VGXNESS doesn't need and that would weaken single-writer control. Keep teams off; if `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS` gets turned on anywhere, named spawns silently turn into teammates.
- Verifier and reviewer "frozen candidate" semantics fit fresh-context, non-fork subagents well: they start without the implementer's reasoning. Forks would leak that context, so the Manager should not use forks for verification.

### Gaps
- The docs don't publish the full Agent tool parameter schema; observed params include `subagent_type`, `prompt`, `description`, `model`, `name`, `isolation`, and `run_in_background` (removed in fork mode).

---

## 3. Built-in agents: models, tools, override and disable

### Takeaway
The built-in agents are Explore and Plan (read-only, inherited model, skip CLAUDE.md and git status), general-purpose (all tools), `claude` (catch-all), statusline-setup (Sonnet) and claude-code-guide (Haiku). Any of them can be overridden by a same-named user or project agent, or disabled through `permissions.deny`.

### Cited Findings
- Explore: read-only (Write and Edit denied). Its model inherits from main, "capped at Opus on the Claude API". Since v2.1.198 it no longer always uses Haiku. It accepts the thoroughness levels quick, medium and very thorough. — [SA](https://code.claude.com/docs/en/sub-agents)
- Plan: read-only, inherited model, used by plan mode. — [SA](https://code.claude.com/docs/en/sub-agents)
- general-purpose: all tools available to subagents. Its model is `CLAUDE_CODE_SUBAGENT_MODEL` if set, else main. — [SA](https://code.claude.com/docs/en/sub-agents)
- Other built-ins: `claude` is the catch-all with every tool and is the default agent for background sessions; `statusline-setup` runs on Sonnet; `claude-code-guide` runs on Haiku. — [SA](https://code.claude.com/docs/en/sub-agents)
- Explore and Plan skip CLAUDE.md files and the git status snapshot; every other agent loads both. — [SA](https://code.claude.com/docs/en/sub-agents)
- Override: "A user or project subagent named `Explore` overrides the built-in and keeps its own `model` field, so define one with `model: haiku`." — [SA](https://code.claude.com/docs/en/sub-agents)
- `CLAUDE_CODE_SUBAGENT_MODEL` alone doesn't change Explore or Plan; you also need `CLAUDE_CODE_SUBAGENT_MODEL_FORCE=1`. — [SA](https://code.claude.com/docs/en/sub-agents)
- Disable options: `permissions.deny: ["Agent(Explore)"]` or `--disallowedTools "Agent(Explore)"`; `CLAUDE_CODE_DISABLE_EXPLORE_PLAN_AGENTS=1` (v2.1.198+); `CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1` in `-p`/SDK removes all built-ins; `permissions.deny: ["Agent"]` disables delegation entirely. — [SA](https://code.claude.com/docs/en/sub-agents)
- Changelog: a fix makes Explore inherit an unrecognized custom model ID instead of switching to Opus. — [CL](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md)

### Inferences
- VGXNESS can either replace the built-in Explore with its own `Explore` (same name, so all automatic delegations get VGXNESS rules) or deny the built-in and use `vgx-explore`. Overriding by name is simpler and catches Claude's habit of delegating searches to Explore. Note that a custom Explore would load CLAUDE.md and git status unless `omitClaudeMd: true` is set.

### Gaps
- The exact read-only tool set of the built-in Explore (e.g. whether Bash is included) isn't spelled out; the session-level description seen in this environment says "All tools except Agent, Artifact…, ExitPlanMode, Edit, Write, NotebookEdit", which implies Bash is available.

---

## 4. Running a custom agent as the main thread (`--agent`, `agent` setting, plugin settings)

### Takeaway
`claude --agent <name>`, or `"agent": "<name>"` in settings (including a plugin's `settings.json`), makes the main thread become that agent. Its body replaces the Claude Code system prompt entirely, while CLAUDE.md still loads. This is the only context where `Agent(type, …)` spawn allowlists and `initialPrompt` apply.

### Cited Findings
- "Pass `--agent <name>` to start a session where the main thread itself takes on that subagent's tool restrictions and model." — [SA](https://code.claude.com/docs/en/sub-agents)
- "Unless the agent's prompt is empty, a custom subagent's system prompt replaces the default Claude Code system prompt entirely, the same way `--system-prompt` does. `CLAUDE.md` files and project memory still load through the normal message flow, even when the agent's definition sets `omitClaudeMd`." — [SA](https://code.claude.com/docs/en/sub-agents)
- An empty `prompt` with no `memory` leaves the session's system prompt unchanged (v2.1.281+). This is a way to apply only tools, model and permissions on top of the stock Claude Code prompt. — [SA](https://code.claude.com/docs/en/sub-agents)
- The setting form is `{"agent": "code-reviewer"}` in `.claude/settings.json`, and the CLI flag overrides it. The choice persists on resume; if the agent has been deleted, the session falls back to default tools with a warning. Plugin agents can be referenced by bare or scoped name. — [SA](https://code.claude.com/docs/en/sub-agents)
- `agent` is valid in "Any file" settings scope. — [SR](https://code.claude.com/docs/en/settings-reference)
- A plugin's `settings.json` supports only `agent` and `subagentStatusLine`. — [PR](https://code.claude.com/docs/en/plugins-reference)
- `initialPrompt` is auto-submitted as the first user turn when the agent runs as the main session agent (it is ignored for plugin agents). `omitClaudeMd` is ignored in this mode. — [SA](https://code.claude.com/docs/en/sub-agents)
- `tools: Agent(worker, researcher), Read, Bash` is an allowlist of spawnable types for a main-thread agent. Omitting `Agent` means it can't spawn at all. — [SA](https://code.claude.com/docs/en/sub-agents)
- `mcpServers` inline definitions also connect when the agent is the main session (with the same trust rule). — [SA](https://code.claude.com/docs/en/sub-agents)
- Changelog: a fix makes `claude agents` sessions keep `--agent`, `--model`, `--effort` and `--permission-mode` after auto-update relaunch. — [CL](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md)

### Inferences
- Replacing the system prompt removes Claude Code's built-in guidance on tool use, git safety and so on. A VGXNESS Manager body has to restate whatever of that it relies on, or use the empty-prompt trick and put Manager policy in CLAUDE.md or a skill instead (see Mapping).

### Gaps
- The docs don't say whether `permissionMode` in a main-thread agent sets the session's starting mode; this is implied by "takes on that subagent's tool restrictions and model" but not explicit. Verify empirically.

---

## 5. Per-subagent persistent memory (`memory:`)

### Takeaway
`memory: user|project|local` gives the agent its own directory with a `MEMORY.md`. The first 200 lines or 25KB are injected into its system prompt, and Read/Write/Edit are auto-enabled. It is part of auto memory, so disabling auto memory disables it.

### Cited Findings
- Scopes: `user` is `~/.claude/agent-memory/<name>/`; `project` is `.claude/agent-memory/<name>/` (committable); `local` is `.claude/agent-memory-local/<name>/` (not committed). `project` is the recommended default. — [SA](https://code.claude.com/docs/en/sub-agents)
- When enabled, the system prompt gets memory instructions plus the first 200 lines or 25KB of `MEMORY.md`, and "Read, Write, and Edit tools are automatically enabled so the subagent can manage its memory files." — [SA](https://code.claude.com/docs/en/sub-agents)
- It has no effect if `autoMemoryEnabled: false` or `CLAUDE_CODE_DISABLE_AUTO_MEMORY` is set. — [SA](https://code.claude.com/docs/en/sub-agents)
- Subagents never see the main conversation's auto memory. — [SA](https://code.claude.com/docs/en/sub-agents)

### Inferences
- **Important for VGXNESS read-only roles:** `memory:` auto-enables Write and Edit. Giving Explore, Verifier or Reviewer a `memory` field would break their read-only guarantee (the docs don't say whether those tools are path-restricted to the memory dir). Since VGXNESS already exposes memory through the `vgxness` MCP server, don't use `memory:` on read-only roles; grant `mcp__vgxness__*` read tools instead.

### Gaps
- The docs don't say whether the auto-enabled Write/Edit are limited to the memory directory or override a `disallowedTools: Write, Edit` entry. Test before relying on either behavior.

---

## 6. Models, aliases, IDs, env vars, effort, thinking, fast mode, costs

### Takeaway
On the Anthropic API, `opus` is Opus 5.5, `sonnet` is Sonnet 5.5, `haiku` is Haiku 4.5, and `fable` is Fable 5.1 (`best` picks fable where available). Subagent model order is: per-invocation param > frontmatter `model` > `CLAUDE_CODE_SUBAGENT_MODEL` > main model, and `_FORCE=1` overrides everything. Effort can be set per agent (`effort:`); thinking cannot (it is inherited, and it is always on for Opus 5.5, Sonnet 5.5 and Fable). Fast mode is Opus-only, a research preview, and billed to usage credits.

### Cited Findings
**Aliases and IDs**
- Aliases: `default` (Opus 5.5 on most plans), `best` (fable if available, else opus), `fable` (Fable 5.1), `opus` (Opus 5.5), `sonnet` (Sonnet 5.5), `haiku`, `opus[1m]`/`sonnet[1m]`, `opusplan` (Opus in plan mode, Sonnet for execution). — [MC](https://code.claude.com/docs/en/model-config)
- Per-provider mapping: Anthropic API is Opus 5.5 / Sonnet 5.5 / Haiku 4.5; Claude Platform on AWS is Opus 5.5 / Sonnet 4.6 / Haiku 4.5; Bedrock and Agent Platform are Opus 5.5 / Sonnet 4.5 / Haiku 4.5; Foundry is Opus 4.6 / Sonnet 4.5 / Haiku 4.5. — [MC](https://code.claude.com/docs/en/model-config)
- Example IDs: `claude-opus-5-5`, `claude-sonnet-5-5`, `claude-haiku-4-5`, `claude-fable-5-1`. Version gates: Sonnet 5.5 needs v2.1.284+, Opus 5.5 v2.1.280+, Fable 5.1 v2.1.257+. — [MC](https://code.claude.com/docs/en/model-config)
- The user's brief listed the Claude 5 family as "Opus 5.5, Sonnet 5.5, Fable 5.1, Haiku 4.5". This is confirmed by [MC](https://code.claude.com/docs/en/model-config).
- Native 1M context: Fable 5.1/5, Sonnet 5+, Opus 4.7+. `[1m]` is only needed for Opus 4.6 / Sonnet 4.6. `CLAUDE_CODE_DISABLE_1M_CONTEXT=1` disables it. — [MC](https://code.claude.com/docs/en/model-config)

**Selection precedence and env vars**
- Main session order: `/model` > `--model` > `ANTHROPIC_MODEL` > `model` setting > `ANTHROPIC_DEFAULT_MODEL` (v2.1.236+) > org default > account default. — [MC](https://code.claude.com/docs/en/model-config)
- `ANTHROPIC_DEFAULT_{OPUS,SONNET,HAIKU,FABLE}_MODEL` pin what each alias resolves to. — [MC](https://code.claude.com/docs/en/model-config)
- Subagent order: (1) per-invocation `model` param, (2) frontmatter `model` (`inherit` means main), (3) `CLAUDE_CODE_SUBAGENT_MODEL`, (4) main model. Before v2.1.251 the env var came first. The env var set to `inherit` counts as unset. — [SA](https://code.claude.com/docs/en/sub-agents)
- Family-alias gotcha: `model: opus` resolves to the main conversation's exact model (including `[1m]`) when main is in the Opus family. — [SA](https://code.claude.com/docs/en/sub-agents)
- `CLAUDE_CODE_SUBAGENT_MODEL_FORCE=1` (v2.1.257+) puts every subagent, teammate and workflow agent on that model. It ignores frontmatter `model`, including Explore and Plan, and Claude can't pass a per-call model. Forks and `context: fork` skills with `model: inherit` stay on main. — [SA](https://code.claude.com/docs/en/sub-agents)
- `availableModels` / `deniedModels` / `enforceAvailableModels` / `availableModelsMatch` also restrict subagents. A blocked family alias falls back to the newest allowed version of that family; other blocked values fall back to the inherited model. — [SA](https://code.claude.com/docs/en/sub-agents); [MC](https://code.claude.com/docs/en/model-config)
- `fallbackModel` chains (at most 3) also apply to subagents when their model is unavailable. — [SA](https://code.claude.com/docs/en/sub-agents); [MC](https://code.claude.com/docs/en/model-config)
- `/tasks` shows each running subagent's model, plus effort when the definition sets it (v2.1.242+). — [SA](https://code.claude.com/docs/en/sub-agents)

**Effort and thinking**
- Levels: `low`, `medium`, `high`, `xhigh`, `max`, plus the `ultracode` toggle. Opus 5.5 and Sonnet 5.5 default to `medium`. You can set effort with `/effort`, `--effort`, `CLAUDE_CODE_EFFORT_LEVEL`, `effortLevel`, per-model `modelSettings.<id>.effort`, `maxEffortLevel`, or `effort:` in skill/agent frontmatter. — [MC](https://code.claude.com/docs/en/model-config)
- The `effort` frontmatter field "Overrides the session effort level. Default: inherits from session… available levels depend on the model." — [SA](https://code.claude.com/docs/en/sub-agents)
- Thinking: "subagents also inherit the main conversation's extended thinking configuration… There is no per-subagent thinking setting" (v2.1.198+). Thinking can't be turned off on Opus 5.5, Sonnet 5.5 or Fable. — [SA](https://code.claude.com/docs/en/sub-agents); [CO](https://code.claude.com/docs/en/costs)
- Ultracode was changed into its own toggle and no longer forces xhigh. — [CL](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md)
- Teammates inherit the lead's effort. — [AT](https://code.claude.com/docs/en/agent-teams)

**Fast mode**
- Fast mode is a research preview: "Opus up to 2.5x faster at a higher cost per token". It isn't a different model, and it is supported on Opus 5.5, Opus 5 and Opus 4.8 only. Price is $8/$40 per MTok in/out on Opus 5.5. On subscriptions it is billed only to usage credits (never the included limits). Rate limits are separate. It isn't available on Bedrock, Agent Platform, Foundry or Claude Platform on AWS. — [FM](https://code.claude.com/docs/en/fast-mode)
- A teammate's fast mode is fixed at spawn. — [AT](https://code.claude.com/docs/en/agent-teams)
- The model-config page's line "`/fast on` switches to fast model (usually Sonnet)" contradicts the fast-mode page, which says it is Opus-only. Trust [FM](https://code.claude.com/docs/en/fast-mode); the conflicting line came from a summarized fetch of [MC](https://code.claude.com/docs/en/model-config).

**Costs and rate limits of fan-out**
- "every subagent… sends its own requests on top of the main conversation's", and `/usage` attributes usage to subagents. — [CO](https://code.claude.com/docs/en/costs)
- "Running many subagents that each return detailed results can consume significant context, and each subagent spends tokens of its own." — [SA](https://code.claude.com/docs/en/sub-agents)
- Agent teams use "approximately 7x more tokens than standard sessions when teammates run in plan mode". The recommendation is Sonnet for teammates and 3–5 teammates. — [CO](https://code.claude.com/docs/en/costs); [AT](https://code.claude.com/docs/en/agent-teams)
- Cost advice: Sonnet handles most coding; reserve Opus; use `model: haiku` for simple subagent tasks. "A switch to Opus also applies to the subagents that inherit your session's model." — [CO](https://code.claude.com/docs/en/costs)
- Cache: non-fork subagents have a separate prompt cache, while forks share main's cache. Subagent and teammate requests use a 5-minute TTL by default; set `subagentPromptCacheTtl: "1h"` or per-agent `experimental.cacheTtl` to extend it (1h writes are billed higher). — [SA](https://code.claude.com/docs/en/sub-agents); [AT](https://code.claude.com/docs/en/agent-teams)
- On subscriptions, the session and weekly limits are shared across all models; model-specific Opus/Sonnet limits exist. — [CO](https://code.claude.com/docs/en/costs)
- Enterprise average: about $13 per developer per active day, $150–250 per month. — [CO](https://code.claude.com/docs/en/costs)

### Inferences
- Because everything inherits by default, a Manager on Opus 5.5 makes every role without an explicit `model:` run on Opus too. Pin a model on each worker role explicitly.
- Putting `model: opus` on a role while the main session is on `opus[1m]` gives that role 1M context as well. That is useful for a General implementer, and wasteful for an Explore.

### Gaps
- No official per-model list prices were fetched for standard (non-fast) Opus 5.5, Sonnet 5.5 or Haiku 4.5; see https://platform.claude.com/docs/en/about-claude/pricing.
- Haiku 4.5's supported effort levels aren't enumerated (MC lists "Haiku" as effort-capable without levels).

---

## 7. Limits and gotchas

### Takeaway
The main risks are: silently ignored fields; permission-mode inheritance that overrides frontmatter; `memory:` granting writes; background tool-set shrinkage; parallel writers in the same checkout; and worktree isolation that branches from the default branch, not HEAD.

### Cited Findings
- **Permission inheritance:** if main is `bypassPermissions`, `acceptEdits` or `auto`, the subagent runs in that mode and its own `permissionMode` is ignored. If main is `default`, `dontAsk` or `plan`, the subagent's `permissionMode` applies, except that a subagent can never escalate to `bypassPermissions` (v2.1.267+). — [SA](https://code.claude.com/docs/en/sub-agents)
- **`disallowedTools` specifiers remove the whole tool.** For partial blocks, use `permissions.deny` rules such as `Bash(git push *)`, which apply to main and all subagents. — [SA](https://code.claude.com/docs/en/sub-agents)
- **Session-wide hooks fire inside subagents**, including `PreToolUse`/`PostToolUse`, and `SubagentStart`/`SubagentStop` can be matched by agent `name` (unanchored regex; anchor with `^…$`). — [SA](https://code.claude.com/docs/en/sub-agents)
- **Worktree isolation:** `isolation: worktree` creates a temp worktree "branched by default from your default branch rather than the parent session's `HEAD`". Set `worktree.baseRef: "head"` to branch from the current HEAD. A worktree with no changes is removed automatically; one with changes stays until the `cleanupPeriodDays` sweep. Bash and PowerShell are confined to the worktree, and git redirects into the main checkout are blocked. — [SA](https://code.claude.com/docs/en/sub-agents); [WT](https://code.claude.com/docs/en/worktrees)
- `ExitWorktree` isn't available to subagents that already run in their own worktree. — [TR](https://code.claude.com/docs/en/tools-reference)
- Changelog: a fix stops worktree-isolated subagents from loading the project CLAUDE.md twice. — [CL](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md)
- **Write conflicts:** "Two teammates editing the same file leads to overwrites. Break the work so each teammate owns a different set of files." The docs offer no built-in lock for plain subagents sharing a checkout; worktrees are the documented isolation mechanism. — [AT](https://code.claude.com/docs/en/agent-teams); [WT](https://code.claude.com/docs/en/worktrees)
- **Context:** the subagent window is sized by its own model, and subagents auto-compact like main (`CLAUDE_AUTOCOMPACT_PCT_OVERRIDE` applies). Main compaction doesn't affect subagent transcripts. — [SA](https://code.claude.com/docs/en/sub-agents)
- **Rules don't reach subagents through conversation context:** "If a rule must… restate it in the prompt you give Claude when delegating." CLAUDE.md does load, unless the agent is Explore or Plan or sets `omitClaudeMd`. — [SA](https://code.claude.com/docs/en/sub-agents)
- **Background grant leakage:** a session-lasting approval given to a background subagent's prompt applies to the whole session. — [SA](https://code.claude.com/docs/en/sub-agents)

### Inferences
- Single-writer cannot be enforced by Claude Code itself for subagents sharing a checkout. It has to come from role design: only one role (General) has write tools, and the Manager runs at most one General at a time, optionally backed by a `PreToolUse` lock hook.

### Gaps
- No documented atomic "one writer" primitive exists for plain subagents (teams have file-locked task claiming, but that covers tasks, not files).

---

## Mapping for VGXNESS

### Takeaway
Deliver the Manager as the main-thread agent (`"agent": "vgx-manager"` in project `.claude/settings.json`), with `tools: Agent(vgx-explore, vgx-general, vgx-verifier, vgx-reviewer), …` so it can only spawn VGXNESS roles. Define the workers as project-scope agents in `.claude/agents/` (not as a plugin, so `permissionMode`, `hooks` and `mcpServers` keep working). Read-only roles use a `tools` allowlist with no Edit/Write/NotebookEdit/Agent and no `memory:` field. Single-writer is enforced by having exactly one write-capable role, a policy of one General at a time, and a `PreToolUse` lock hook as a backstop.

### Cited Findings (mechanisms relied on)
- Main-thread agent plus a spawn allowlist via `Agent(a, b)` (works only for `--agent` / the `agent` setting). — [SA](https://code.claude.com/docs/en/sub-agents)
- Plugin agents drop `hooks`, `mcpServers` and `permissionMode`. — [SA](https://code.claude.com/docs/en/sub-agents)
- MCP tool patterns `mcp__<server>__*` in `tools`; `mcpServers: [vgxness]` references an already-configured server. — [SA](https://code.claude.com/docs/en/sub-agents)
- `memory:` auto-enables Write and Edit. — [SA](https://code.claude.com/docs/en/sub-agents)
- Permission mode inheritance rules (main `acceptEdits`/`auto` override worker frontmatter). — [SA](https://code.claude.com/docs/en/sub-agents)
- A same-named `Explore` overrides the built-in. — [SA](https://code.claude.com/docs/en/sub-agents)

### Inferences (recommendations; confidence noted)

**Manager (main thread). Confidence: high on the mechanism, medium on the prompt strategy.**
```yaml
# .claude/agents/vgx-manager.md   + .claude/settings.json: { "agent": "vgx-manager" }
---
name: vgx-manager
description: VGXNESS Manager. Owns authorization, scope, lifecycle and final acceptance.
tools: Agent(vgx-explore, vgx-general, vgx-verifier, vgx-reviewer), Read, Grep, Glob, Bash, Edit, Write, Skill, ToolSearch, SendMessage, TaskStop, mcp__vgxness__*
model: opus            # or omit and let /model decide; Manager benefits most from Opus 5.5
effort: high           # Opus 5.5 default is medium; raise for acceptance decisions
color: purple
---
<Manager policy: classify request (direct question / bounded read / implementation / delivery);
 answer direct questions and bounded reads itself; delegate bounded independent work;
 at most ONE vgx-general alive at a time; Manager itself does not edit code while a General runs
 (it edits only docs/plans/*.md); freeze candidate (commit/diff SHA) before spawning verifier/reviewer;
 restate any CLAUDE.md rules a worker needs in the delegation prompt.>
```
- The body replaces the Claude Code system prompt. Alternative: an empty `prompt` via `--agents` JSON keeps the stock prompt, with Manager policy living in CLAUDE.md or a preloaded skill. The trade-off: the full body gives the most control but loses built-in guidance; the empty prompt keeps the built-in guidance but makes the policy less authoritative. Recommendation: full body, with the parts of the default behavior VGXNESS depends on (git safety, verification) restated in it.
- Give the Manager `Edit`/`Write` only for `docs/plans/**`, using `permissions.deny: ["Edit(src/**)", …]` or a hook. Frontmatter can't scope Edit by path.
- Removing the built-in general-purpose/Explore/Plan from the allowlist means the Manager cannot bypass VGXNESS roles.

**Explore (read-only search). Confidence: high.**
```yaml
---
name: vgx-explore        # or name it "Explore" to also override the built-in
description: Read-only codebase search. Use proactively for locating code, symbols, call paths. Never edits.
tools: Read, Grep, Glob, LSP, mcp__vgxness__search, mcp__vgxness__get   # adjust to real vgxness read tools
model: haiku            # or sonnet for large monorepos
effort: low
permissionMode: dontAsk # only allowed tools run; anything else auto-denied
omitClaudeMd: true      # optional: cheaper; Manager restates needed rules
---
```
- No Bash means it can't write anything through a shell. If Bash is needed (e.g. `git log`), add it and pair it with a `PreToolUse` hook that allowlists read-only commands, or rely on `permissions.deny` rules. No `memory:` field.

**General (implementation, the only writer). Confidence: high.**
```yaml
---
name: vgx-general
description: Implements a bounded, Manager-approved change. Only workspace writer.
tools: Read, Grep, Glob, LSP, Edit, Write, Bash, Skill, mcp__vgxness__*
model: sonnet           # Sonnet 5.5; escalate per-invocation to opus for hard tasks
effort: medium
permissionMode: acceptEdits   # ignored if main is acceptEdits/auto/bypass (inherits)
hooks:
  PreToolUse:
    - matcher: "Edit|Write|NotebookEdit"
      hooks: [{ type: command, command: "./.claude/hooks/writer-lock.sh" }]
---
```
- Omitting `Agent` means it can't sub-delegate, which keeps it as one writer.
- Optional `isolation: worktree`: it gives a clean, reviewable diff and protects the main checkout, but it branches from the default branch unless `worktree.baseRef: "head"` is set, and the Manager must merge the result. Recommendation: no worktree while only one writer exists (simpler); use worktrees only if VGXNESS ever allows parallel Generals.

**Verifier (independent verification of a frozen candidate). Confidence: high.**
```yaml
---
name: vgx-verifier
description: Independently verifies a frozen candidate (given commit/diff). Runs tests/builds; never edits.
tools: Read, Grep, Glob, LSP, Bash, mcp__vgxness__get
model: sonnet
effort: high
permissionMode: default
hooks:
  PreToolUse:
    - matcher: "Bash"
      hooks: [{ type: command, command: "./.claude/hooks/verifier-bash-guard.sh" }]  # block git commit/checkout/reset, rm, redirects into src
---
```
- It needs Bash to run tests, so a hook is the read-only backstop, since `disallowedTools: Bash(git commit *)` would remove Bash entirely. Use a non-fork subagent so it doesn't inherit the implementer's reasoning. To freeze the candidate, the Manager passes a SHA, and the verifier could run in `isolation: worktree` with `worktree.baseRef: head`, so tests can't touch the writer's checkout.

**Reviewer (merged CARE roles). Confidence: medium-high.**
```yaml
---
name: vgx-reviewer
description: Reviews a frozen candidate for correctness, architecture, risk/security, ergonomics (CARE). Read-only; returns findings ranked by severity.
tools: Read, Grep, Glob, LSP, mcp__vgxness__search, mcp__vgxness__get
model: opus             # review quality benefits most from the strongest model
effort: high
permissionMode: dontAsk
skills: [care-checklist] # preload the CARE rubric as a skill
---
```
- Merging the three roles into one cuts token cost by roughly 3x versus three parallel reviewers. If you split them again later, run them in parallel (they're read-only, so there are no write conflicts).

**Enforcing read-only and single-writer (layered)**
1. Use the `tools` allowlist, not a `disallowedTools` denylist, for read-only roles. New tools or MCP servers then aren't inherited by accident. (high)
2. Never set `memory:` on read-only roles, because it auto-grants Write/Edit. Use the `vgxness` MCP for memory. (high, from the SA docs)
3. Only `vgx-general` has Edit/Write. The Manager policy allows at most one live General. Backstop: a `writer-lock.sh` `PreToolUse` hook that acquires a lockfile keyed by agent id, plus a `SubagentStop` hook (matcher `^vgx-general$`) in settings.json that releases it. (medium: the lock design is an inference, the hook mechanisms are documented)
4. Keep `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS` unset or `0`. Otherwise named spawns turn into teammates with lead-inherited permissions. (high)
5. Consider `CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH=1` so workers can never nest. They have no `Agent` tool anyway, but this guards against a misconfigured definition. (medium)
6. Watch the Manager's session mode: when the user runs in `acceptEdits` or `auto`, every worker inherits it and `permissionMode` in frontmatter is ignored. Read-only safety must therefore rest on `tools`, not on `permissionMode`. (high)
7. Use project scope `.claude/agents/`, committed, not a plugin, because a plugin would silently drop `permissionMode`, `hooks` and `mcpServers`. If you later distribute VGXNESS as a plugin, the hooks have to move into the plugin's `hooks/hooks.json`, matched by agent type. (high)
8. Validate the agent files in CI, because typos in field names fail silently. (high)

**Plans:** durable Markdown plans stay in `docs/plans/`, owned by the Manager. Workers receive the relevant plan excerpt in their delegation prompt, since they don't see the conversation.

### Gaps
- Needs a quick empirical test: whether `permissionMode` in a main-thread (`--agent`) definition sets the session mode, whether `memory:`-granted Write/Edit is path-limited, and the exact tool names exposed by the `vgxness` MCP server (`mcp__vgxness__<tool>`).
- The `writer-lock.sh` hook design is a proposal, not an official pattern.
