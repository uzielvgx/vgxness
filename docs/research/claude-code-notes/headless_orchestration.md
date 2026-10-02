# Claude Code headless/automation surface and native orchestration features (state as of 2026-10-01)

Scope note: all primary sources are the official docs at code.claude.com/docs (pages carry per-feature version notes up to roughly Claude Code v2.1.285). Version numbers below are the minimum versions the docs state. No third-party sources were needed. Base URL for citations: `https://code.claude.com/docs/en/`.

## Q1. `claude -p` / print mode: flags, JSON result schema, stream-json events

### Takeaway
`claude -p` is the CLI face of the Agent SDK. It has a full non-interactive control surface: output formats, budget/turn caps, tool allow/deny, permission modes (including a new `--permission-prompts none`), isolated config loading (`--bare`, `--setting-sources`, `--strict-mcp-config`, `--restricted`), plugin loading (`--plugin-dir`, `--plugin-url`), and a typed NDJSON stream whose `system/init` event reports loaded plugins and plugin load errors. That is enough to gate CI on whether a plugin loaded.

### Cited Findings
**Core flags (all from CLI reference):**
- `-p/--print` runs non-interactively. `--output-format text|json|stream-json`. `--input-format text|stream-json`. `--include-partial-messages` needs `-p` and `stream-json`. `--replay-user-messages` echoes stdin user messages and needs stream-json in and out. — [CLI reference](https://code.claude.com/docs/en/cli-reference)
- `--max-turns` works in print mode only and exits with an error at the limit. `--max-budget-usd` works in print mode only. Subagent spend counts toward the cap, and once it is hit, spawning a subagent fails with `Budget limit reached` and background subagents stop. That enforcement needs v2.1.217+. — [CLI reference](https://code.claude.com/docs/en/cli-reference)
- `--allowedTools` pre-approves tools using permission rule syntax. `--disallowedTools` given a bare name removes the tool from context; given a scoped rule such as `Bash(rm *)` it only denies matching calls. `--tools` limits the built-in tool set (`""` means none, `"default"` means the default set). `--tools` does not affect MCP tools, so use `--disallowedTools "mcp__*"` for those. — [CLI reference](https://code.claude.com/docs/en/cli-reference)
- `--permission-mode` accepts `default|acceptEdits|plan|auto|dontAsk|bypassPermissions|manual`. `--permission-prompt-tool` names an MCP tool that answers prompts. `--permission-prompts none` (v2.1.259+) denies anything that would prompt, removes `AskUserQuestion`, and tells Claude not to retry. Denials then show up as `permission_denied` system messages and in the result's `permission_denials`. — [CLI reference](https://code.claude.com/docs/en/cli-reference); [Headless](https://code.claude.com/docs/en/headless)
- Starting mode for `claude -p` and the SDK: `default` in sessions that fetch feature flags. Where flags aren't fetched (third-party provider, telemetry off), it is `auto` on v2.1.285+. Pass `--permission-mode` explicitly in CI. — [Permission modes](https://code.claude.com/docs/en/permission-modes)
- `dontAsk` denies every call that would prompt. Reads inside working directories, the read-only command set, and allow-rule matches still run. — [Headless](https://code.claude.com/docs/en/headless)
- `--mcp-config` takes files or JSON strings. With `-p` it waits up to `MCP_TIMEOUT` (30s default) for pending servers (v2.1.221+). `--strict-mcp-config` uses only `--mcp-config` servers. — [CLI reference](https://code.claude.com/docs/en/cli-reference)
- `--plugin-dir` loads a plugin directory or `.zip` for one session and can be repeated. A folder of plugins is accepted on v2.1.265+. `--plugin-url` fetches a plugin zip. — [CLI reference](https://code.claude.com/docs/en/cli-reference)
- `--agents` takes subagent JSON. With `--print` it can also be a JSON file path (v2.1.281+). It is validated at startup (v2.1.242+). `--agent` selects the session's agent. `--append-subagent-system-prompt[-file]` is `-p` only (v2.1.205+ / v2.1.261+). `--forward-subagent-text` emits subagent text and thinking blocks with `parent_tool_use_id` set (v2.1.211+; nested subagents v2.1.219+). — [CLI reference](https://code.claude.com/docs/en/cli-reference)
- System prompt flags: `--system-prompt[-file]` (replace), `--append-system-prompt[-file]`, `--system-prompt-snapshot on|off`. The prompt is recorded on the first request and reused on `--resume` until the conversation compacts. — [CLI reference](https://code.claude.com/docs/en/cli-reference)
- `--settings <file|json>` overrides settings keys. `--setting-sources user,project,local` limits which sources load. `--json-schema` returns validated output in `structured_output` and exits with an error on an invalid schema (v2.1.205+). `--no-session-persistence` is print-only. `--session-id <uuid>`, `--resume/-r <id|name|.jsonl path>`, `--continue/-c`, `--fork-session`. `--fallback-model a,b`. `--include-hook-events` (stream-json). `--init`/`--maintenance` run Setup hooks in print mode. — [CLI reference](https://code.claude.com/docs/en/cli-reference)
- `--bare` skips auto-discovery of hooks, skills, commands, subagents, installed plugins, MCP servers, auto memory and CLAUDE.md. It never reads OAuth or the keychain, so it needs `ANTHROPIC_API_KEY`, an `apiKeyHelper`, or cloud-provider creds. Load context explicitly with `--settings`, `--mcp-config`, `--agents`, `--plugin-dir`, `--append-system-prompt`. Docs: "`--bare` is the recommended mode for scripted and SDK calls, and will become the default for `-p` in a future release." — [Headless](https://code.claude.com/docs/en/headless)
- Without `--bare`, a `-p` session runs project `.claude/settings.json` hooks and `.mcp.json` servers, even in an untrusted folder, with no trust dialog. — [Headless](https://code.claude.com/docs/en/headless)
- `--restricted` (v2.1.248+) is aimed at evaluation harnesses on shared machines. It removes the command/code tools and WebFetch unless they are named in `--tools`, confines file tools to the working dirs, loads only managed settings plus `--settings`, and refuses `bypassPermissions`. `--safe-mode` disables all customizations but keeps auth and built-in tools. — [CLI reference](https://code.claude.com/docs/en/cli-reference)
- Exit behavior: 0 on success, non-zero on failure, 143 on SIGTERM (turn left unfinished). Background Bash is killed about 5s after the result. Background subagents and workflows keep `-p` open, with a 10-minute idle ceiling (`CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS`). Piped stdin is capped at 10MB. — [Headless](https://code.claude.com/docs/en/headless)
- In `-p`, user-invoked skills and commands work: put `/skill-name` (or `/plugin:skill`) in the prompt. Terminal-only commands like `/login` don't. `/model x`, `/effort`, `/config key=value` accept arguments (v2.1.205+). — [Headless](https://code.claude.com/docs/en/headless)

**JSON result schema (`--output-format json` and the final stream line; `SDKResultMessage`):**
- Success variant fields: `type:"result"`, `subtype:"success"`, `uuid`, `session_id`, `duration_ms`, `duration_api_ms`, `is_error`, `num_turns`, `result`, `stop_reason`, `total_cost_usd`, `usage`, `modelUsage{model→ModelUsage}`, `permission_denials[]`, `structured_output?`, `terminal_reason?`, plus timing fields (`ttft_ms` etc.). — [TS SDK reference](https://code.claude.com/docs/en/agent-sdk/typescript)
- Error subtypes: `error_max_turns`, `error_during_execution`, `error_max_budget_usd`, `error_max_structured_output_retries`. — [TS SDK reference](https://code.claude.com/docs/en/agent-sdk/typescript)
- `total_cost_usd` and `modelUsage` are client-side list-price estimates. They include subagent spend. Top-level `usage` undercounts when subagents nest, so use `modelUsage` for whole-tree accounting. A resumed run reports the conversation's cumulative total. — [Cost tracking](https://code.claude.com/docs/en/agent-sdk/cost-tracking); [Headless](https://code.claude.com/docs/en/headless)

**stream-json events:**
- The stream is NDJSON. It needs `--verbose` with stream-json, and the last line is the `result` message. `stream_event` lines carry partial deltas when `--include-partial-messages` is on. — [Headless](https://code.claude.com/docs/en/headless)
- `system/init` fields: `session_id`, `claude_code_version`, `cwd`, `tools[]`, `mcp_servers[{name,status}]`, `model`, `permissionMode`, `slash_commands[]`, `skills[]`, `agents[]`, `plugins[{name,path}]`, `plugin_errors[{plugin,type,message,path?}]`, `capabilities[]` (v2.1.205+), `effort`. — [TS SDK reference](https://code.claude.com/docs/en/agent-sdk/typescript)
- The docs explicitly cover "Fail CI when a plugin or MCP server doesn't load". Check `plugins` and `plugin_errors`, and `mcp_servers` and `mcp_server_errors` (v2.1.219+; the key is omitted when empty). When stderr is captured, invalid MCP entries are reported only in `mcp_server_errors`. — [Headless](https://code.claude.com/docs/en/headless)
- Other system events: `system/api_retry` (attempt, max_retries, retry_delay_ms, error_status, error category), `system/plugin_install` (with `CLAUDE_CODE_SYNC_PLUGIN_INSTALL`), `hook_started/hook_progress/hook_response`, `permission_denied`, `prompt_suggestion`. The TS reference also lists `SDKCompactBoundaryMessage`, `SDKTaskStarted/Progress/Updated/NotificationMessage`, `SDKToolProgressMessage`, `SDKToolUseSummaryMessage`, `SDKBackgroundTasksChangedMessage`, `SDKStatusMessage`, `SDKAuthStatusMessage` and others. — [Headless](https://code.claude.com/docs/en/headless); [TS SDK reference](https://code.claude.com/docs/en/agent-sdk/typescript)
- Subagent messages carry `parent_tool_use_id`, which is the ID of the spawning Agent/Skill tool call and lets you rebuild the nesting tree. By default only the subagent's tool_use/tool_result blocks are emitted, and text needs `--forward-subagent-text`. A worktree-resume refusal arrives as `result`/`error_during_execution` with `startup_failure_reason` (v2.1.274+). — [Headless](https://code.claude.com/docs/en/headless); [Worktrees](https://code.claude.com/docs/en/worktrees)

### Inferences
- A deterministic CI smoke test is possible without the SDK: `claude --bare -p --plugin-dir ./plugin --output-format stream-json --verbose --permission-mode dontAsk --max-turns N --max-budget-usd X`. Then use `jq` to assert `plugins` contains the plugin, `plugin_errors` is absent, a `tool_use` with `name:"Skill"` and the expected skill input appears, and `result.subtype=="success"`.
- `--bare` plus explicit flags is the right baseline for reproducibility, because it removes the host's `~/.claude` and project config from the run.

### Gaps
- I found no single doc table listing every stream-json `type`/`subtype` emitted by the CLI. The TS reference's `SDK*Message` types are the closest canonical list.

## Q2. Claude Agent SDK (TS/Python): plugins, settings sources, agents, hooks, MCP, canUseTool; fit for automated plugin tests

### Takeaway
The Agent SDK (`@anthropic-ai/claude-agent-sdk`, Python `claude_agent_sdk`) bundles the Claude Code binary and exposes the same loop. It loads plugins via `plugins: [{type:"local", path}]`, controls config loading via `settingSources`, takes in-process callbacks for hooks and permissions, supports in-process MCP servers, and returns typed messages. That makes it the better choice for richer assertions than shell+jq (callbacks that record tool calls, deny, or inject failures).

### Cited Findings
- Install: `npm install @anthropic-ai/claude-agent-sdk`. Platform-specific binaries are packaged separately, e.g. `@anthropic-ai/claude-agent-sdk-linux-x64`. — [TS SDK reference](https://code.claude.com/docs/en/agent-sdk/typescript)
- Plugins: `options.plugins = [{ type: "local", path }]`. `"local"` is the only accepted type, so marketplace plugins must be downloaded first. Relative paths resolve against `cwd`, and `~` is not expanded. A nonexistent path is skipped silently and the session continues, so check `plugins` and `plugin_errors` in the init message. Plugin skills are namespaced `plugin:skill` and appear in `skills` and `slash_commands`. Invoke one directly with the prompt `/plugin-name:skill-name`. — [SDK plugins](https://code.claude.com/docs/en/agent-sdk/plugins)
- `settingSources`/`setting_sources`: omitting it is equivalent to `["user","project","local"]`, which loads the same filesystem config as the CLI, including CLAUDE.md and `.claude/` skills, agents and commands. `[]` disables those sources. Managed policy settings and `~/.claude.json` are read regardless. For isolation the docs recommend a separate filesystem, `settingSources: []`, and `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1`. — [Claude Code features in SDK](https://code.claude.com/docs/en/agent-sdk/claude-code-features); [SDK configuration](https://code.claude.com/docs/en/agent-sdk/configuration)
- The `settings` option (file path, JSON string, or a TS object) overrides user, project and local settings. Only managed settings rank higher. — [SDK configuration](https://code.claude.com/docs/en/agent-sdk/configuration)
- Permission evaluation order is hooks → deny rules → ask rules → permission mode → allow rules → `canUseTool` callback. In `dontAsk` the callback is skipped and the call denied. In `plan` mode, file-edit and shell-write tools always go to `canUseTool`. The TS SDK warns with `CLAUDE_SDK_CAN_USE_TOOL_SHADOWED` when the configuration would auto-approve before the callback runs. — [SDK permissions](https://code.claude.com/docs/en/agent-sdk/permissions)
- Hooks as in-process callbacks: the TS `HookCallback` type, and Python `HookMatcher(matcher=..., hooks=[fn])`. Events available in both SDKs: PreToolUse, PostToolUse, PostToolUseFailure, UserPromptSubmit, Stop, SubagentStart, SubagentStop, PreCompact, PermissionRequest, Notification. TS-only events: PostToolBatch, StopFailure, PostCompact, SessionStart/End, Setup, TeammateIdle, TaskCreated, TaskCompleted, PermissionDenied, Elicitation, ConfigChange, InstructionsLoaded, Pre/PostModelSwitch, MessageDisplay, UserPromptExpansion. Hook inputs inside subagents carry `agent_id`/`agent_type`. — [SDK hooks](https://code.claude.com/docs/en/agent-sdk/hooks)
- MCP: stdio, HTTP/SSE, and in-process SDK servers (`createSdkMcpServer()`, with a `timeout` per server in TS). The first turn waits for pending servers up to `MCP_TIMEOUT`, or `CLAUDE_CODE_MCP_STARTUP_WAIT_MS` if set. — [SDK MCP](https://code.claude.com/docs/en/agent-sdk/mcp)
- Cost: per-step usage is on assistant messages (dedupe by message id), and `total_cost_usd`/`modelUsage` are on the result. These are estimates from a bundled price table or the `modelPricing` setting. — [SDK cost tracking](https://code.claude.com/docs/en/agent-sdk/cost-tracking)
- Sessions can be continued, resumed and forked. Transcripts can be mirrored to external storage so other hosts can resume them. — [SDK sessions](https://code.claude.com/docs/en/agent-sdk/sessions); [Session storage](https://code.claude.com/docs/en/agent-sdk/session-storage)
- Todo tracking: in sessions that have task tools, todos appear as `TaskCreate`/`TaskUpdate` `tool_use` blocks in the stream. The streamed input is the model's raw shape, so `task_id` is not yet normalized to `taskId`. — [SDK todo tracking](https://code.claude.com/docs/en/agent-sdk/todo-tracking)
- Agent teams don't spawn in `-p`/SDK sessions. A named subagent runs as an ordinary subagent there. — [Agent teams](https://code.claude.com/docs/en/agent-teams)

### Inferences
- For VGXNESS plugin tests, the SDK is the better harness when assertions need structure: recording every Agent/Skill/Edit call, asserting that only one agent wrote files (via PreToolUse with `agent_id`), and failing on `plugin_errors`. The CLI with stream-json is enough for smoke tests.
- Use the TS SDK if you want TS-only hooks such as `TaskCompleted`, `SubagentStart`, or `PostToolBatch`.

### Gaps
- I didn't verify the current SDK package version numbers or the Python package's PyPI name beyond the `claude_agent_sdk` import. The repos anthropics/claude-agent-sdk-typescript and -python were not fetched.

## Q3. GitHub Actions (claude-code-action), CI auth, cost controls

### Takeaway
`anthropics/claude-code-action@v1` wraps the SDK. It auto-detects interactive (`@claude`) vs automation (`prompt`) mode, passes raw CLI flags via `claude_args`, can install plugins from marketplaces, and authenticates with an API key, a `claude setup-token` OAuth token, OIDC workload-identity federation, or Bedrock/Vertex/Foundry. Cost controls are `--max-turns`/`--max-budget-usd` in `claude_args` plus workflow timeouts and concurrency.

### Cited Findings
- Inputs: `prompt`, `claude_args`, `anthropic_api_key`, `claude_code_oauth_token`, `github_token`, `plugin_marketplaces`, `plugins` (`name@marketplace`), `settings` (JSON or path), `trigger_phrase`, `use_bedrock`, `use_vertex`, `use_foundry`, `allowed_bots`, `allowed_non_write_users`. A `prompt` can be a skill such as `/plugin:skill`. — [GitHub Actions](https://code.claude.com/docs/en/github-actions)
- In automation mode with a plain prompt, Claude has no shell or GitHub API access until you grant tools via `--allowedTools` in `claude_args` or the `settings` input. — [GitHub Actions](https://code.claude.com/docs/en/github-actions)
- OIDC federation inputs: `anthropic_federation_rule_id`, `anthropic_organization_id`, optional `anthropic_service_account_id` and `anthropic_workspace_id`. The workflow needs `id-token: write`. — [GitHub Actions](https://code.claude.com/docs/en/github-actions)
- `claude setup-token` generates a one-year OAuth token, prints it without saving, and needs a Claude subscription. Use it as `CLAUDE_CODE_OAUTH_TOKEN`. Auth precedence includes `ANTHROPIC_API_KEY` (X-Api-Key) and then `CLAUDE_CODE_OAUTH_TOKEN`. For org-wide shared secrets, an API key is preferred because the OAuth token is tied to one person's subscription. — [Authentication](https://code.claude.com/docs/en/authentication); [GitHub Actions](https://code.claude.com/docs/en/github-actions)
- `--bare` never reads OAuth credentials, so `--bare` runs need an API key, `apiKeyHelper`, or cloud-provider creds. — [Headless](https://code.claude.com/docs/en/headless)
- Cost guidance from the docs: set `--max-turns` in `claude_args`, set workflow timeouts, use concurrency controls, and keep CLAUDE.md concise. — [GitHub Actions](https://code.claude.com/docs/en/github-actions)
- Migration from `@beta`: drop `mode`, rename `direct_prompt` to `prompt`, and move `max_turns`/`model` into `claude_args`. — [GitHub Actions](https://code.claude.com/docs/en/github-actions)

### Inferences
- For plugin tests you don't need the action. A plain job that installs Claude Code and runs `claude plugin eval` or `claude --bare -p` with an `ANTHROPIC_API_KEY` secret is simpler and avoids the GitHub App permissions. Use the action only for PR-comment workflows.

### Gaps
- I didn't fetch the action's full `docs/usage.md` input list.

## Q4. Native plugin evaluation: `claude plugin eval` (directly relevant to the old offline Python eval runner)

### Takeaway
Since v2.1.269 (announced the week of Sept 7–11, 2026), Claude Code ships a native plugin eval runner. It covers cases in `evals/`, deterministic and LLM graders, a with/without-plugin baseline (Δ), MCP mocks with record/replay, sandboxed isolated runs, versioned JSON results, and CI exit codes. It substantially replaces a custom offline evaluation runner for Claude Code as a host.

### Cited Findings
- Requirements: v2.1.269+, git 2.31+ if git is installed, and a plugin with `plugin.json` or `.claude-plugin/plugin.json`. It uses your normal credentials, and every run is billed. — [Plugin evals](https://code.claude.com/docs/en/plugin-evals); [What's new W37](https://code.claude.com/docs/en/whats-new/2026-w37)
- Layout: `evals/<case>/prompt.md` (frontmatter `max_turns`, `timeout_seconds`, `model`, `tags`, `allowed_tools`, `plugins`; body is the prompt), `graders/<name>.md`, optional `case.yaml` (`schema_version: "1.1"`, `context.scaffold_script`, `context.history_file` .jsonl, `context.add_dirs`), `mocks/<server>/<tool>.md`, and `results/<timestamp>/{aggregate-result.json, report.html}`. The eval dir can be changed via `"experimental": {"evals": "..."}` in plugin.json or `--eval-dir`. — [Plugin evals](https://code.claude.com/docs/en/plugin-evals)
- Six grader types. `regex` (pattern/flags/match/target), `tool_used` (tool, input_match, min/max; `min:0,max:0` asserts the tool was never called), `tool_order` (before/after), and `file_exists` are free. `llm` (majority of three judge votes) and `baseline` call a judge model. The docs say plainly: "There are no custom-code graders." — [Plugin evals](https://code.claude.com/docs/en/plugin-evals)
- Scoring: 3 runs per case per arm by default. Run score is the weighted fraction of graders passed, case score is the mean, and `--threshold` defaults to 1.0. The two-arm baseline excludes plugin-only indicators (`tool_used: Skill`, mock-call graders, `arm: with-only`) from the score. — [Plugin evals](https://code.claude.com/docs/en/plugin-evals)
- Isolation: each run is a `claude -p` child with a temporary HOME, cwd and config, and only the plugin loaded. No user or project settings, CLAUDE.md, MCP or memory are loaded. The Artifact tool is off. Case definitions are hidden from the agent. Built-in write and exec tools are removed unless granted with `--allow-tools`, and granted Bash runs under the OS sandbox. — [Plugin evals](https://code.claude.com/docs/en/plugin-evals)
- Tools available without a grant: Read, Glob, Grep, NotebookRead, Skill, AskUserQuestion, Agent, TodoWrite, TaskCreate/Get/List/Update/Stop. — [Plugin evals](https://code.claude.com/docs/en/plugin-evals)
- CI invocation from the docs: `claude plugin eval . --trust-plugin --json results.json --threshold 0.8 --model <pinned> --judge-model <pinned> --no-publish --max-cost-usd 20`. Exit codes: 0 pass; 1 below threshold, load failure, or untrusted; 2 partial (cost ceiling or auth); 130 interrupted; 143 terminated. Δ never affects the exit code. Other flags: `-j` (1–8), `--ablation none|with-without`, `--scaffold`, `--mocks record|off`, `--allow-real-servers`, `--keep-temp`, `--case`, `--tag`. — [Plugin evals](https://code.claude.com/docs/en/plugin-evals)
- JSON result (`schemaVersion: 1`, camelCase, additive): `partial`/`partialReason`, `aggregates.{overallScore,casesPassed,casesTotal,meanDelta}`, `cases[].aggregates.{score,delta}`, `cases[].arms.with[].{error,aborted,skippedPaidGraders}`, `costUsd`, `durationSeconds`, `claudeVersion`. — [Plugin evals](https://code.claude.com/docs/en/plugin-evals)
- `claude plugin eval init` interactively generates cases. `init --bare <name>` writes a blank template for CI. `claude plugin validate` checks schema and syntax, not behavior. — [Plugin evals](https://code.claude.com/docs/en/plugin-evals)

### Inferences
- The gap versus VGXNESS's old Python runner is custom-code graders. Workaround: have the prompt write an outcome file and grade it with `regex`/`file_exists`, or run an SDK harness alongside for assertions that need code (for example, "only one writer edited files").
- `context.history_file` (.jsonl transcripts) means recorded Claude Code transcripts can seed eval cases. Traces from other hosts would need converting to Claude Code's transcript format, which is not documented here.

### Gaps
- I didn't confirm whether `plugin eval` can run in `--bare` style with only an API key on Linux CI. The docs say credentials come from the env, e.g. `ANTHROPIC_API_KEY`, and that Bash grants need a sandbox backend (native Windows has none).

## Q5. Task/todo list tools: TodoWrite vs TaskCreate/TaskUpdate/TaskList, persistence, sharing, CLAUDE_CODE_TASK_LIST_ID

### Takeaway
The Task tools (`TaskCreate/TaskGet/TaskUpdate/TaskList`, plus `TaskStop`) replaced `TodoWrite` (legacy, via `CLAUDE_CODE_ENABLE_TASKS=0`). As of v2.1.268 they are not provided by default on newer models: they are on by default only for Claude 3.x, Opus 4–4.7, Sonnet 4–4.6 and Haiku 4.5. Task lists persist in `~/.claude/tasks/` and can be shared across sessions via `CLAUDE_CODE_TASK_LIST_ID`. This is session or user scoped state, not a repo artifact.

### Cited Findings
- `TodoWrite` is "Disabled by default in favor of `TaskCreate`, `TaskGet`, `TaskList`, and `TaskUpdate`". `CLAUDE_CODE_ENABLE_TASKS=0` re-enables it. — [Tools reference](https://code.claude.com/docs/en/tools-reference)
- Task tool availability: default on only for Claude 3.x, Opus 4–4.7, Sonnet 4–4.6 and Haiku 4.5. On other models, including newer ones and unrecognized IDs, they are off unless you opt in with `CLAUDE_CODE_ENABLE_TODO_TOOLS=1` (v2.1.233+), by naming a Task tool in `--allowedTools`/`--tools`, or via SDK `allowedTools`/`tools`. Background and cloud sessions always have them. The docs give the rationale: "On newer models, Claude keeps track of multi-step work without a written checklist." This default applies from v2.1.268. — [Tools reference](https://code.claude.com/docs/en/tools-reference)
- Subagents get the Task tools only when the parent session has them. — [Tools reference](https://code.claude.com/docs/en/tools-reference)
- Persistence: tasks survive compaction, and Ctrl+T toggles the view. "To share a task list across sessions, set `CLAUDE_CODE_TASK_LIST_ID` to use a named directory in `~/.claude/tasks/`". — [Interactive mode](https://code.claude.com/docs/en/interactive-mode); [Env vars](https://code.claude.com/docs/en/env-vars)
- Agent-team task lists live at `~/.claude/tasks/{team-name}/`, persist locally, are never uploaded, and are retained per `cleanupPeriodDays`. Statuses are pending, in_progress and completed. Dependencies block claiming, and claiming uses file locking. — [Agent teams](https://code.claude.com/docs/en/agent-teams)
- `claude project purge` deletes task lists along with transcripts. — [CLI reference](https://code.claude.com/docs/en/cli-reference)

### Inferences
- Native tasks are ephemeral working memory under `~/.claude`, not versioned. VGXNESS's durable Markdown plans with checklists in the repo are not duplicated by this and remain VGXNESS's job. Don't depend on Task tools being present: on current default models they're off unless opted in.

### Gaps
- I found no documented on-disk file format for `~/.claude/tasks/<id>/`.

## Q6. Plan mode, plan files, ExitPlanMode, opusplan

### Takeaway
Plan mode is a permission mode. It allows reads and exploration, blocks edits until approval, and approval goes through `ExitPlanMode`. Plan files go to `~/.claude/plans` by default, and the `plansDirectory` setting can move them inside the project. `opusplan` uses Opus while planning and Sonnet while executing.

### Cited Findings
- Plan mode: Claude reads files, runs exploratory shell commands, and writes a plan without editing source. Edits stay blocked until approval. Enter it with Shift+Tab, `/plan <prompt>`, or `--permission-mode plan`. Approval options: "Yes, and use auto mode" (or auto-accept edits), "Yes, manually approve edits", "No, keep planning". Ctrl+G edits the plan in your editor. — [Permission modes](https://code.claude.com/docs/en/permission-modes)
- With auto mode available and `useAutoModeDuringPlan` on (the default), a classifier reviews planning shell commands. Otherwise, non-read-only commands prompt. Plan mode keeps its blocks in `-p` and SDK sessions. — [Permission modes](https://code.claude.com/docs/en/permission-modes)
- `ExitPlanMode` "Presents a plan for approval and exits plan mode" and requires permission. `EnterPlanMode` switches into plan mode. — [Tools reference](https://code.claude.com/docs/en/tools-reference)
- `plansDirectory` is a path relative to the project root. The default is unset, which means `~/.claude/plans`, and a path resolving outside the project falls back to the default. — [Settings reference](https://code.claude.com/docs/en/settings-reference)
- In the SDK's plan mode, edit and shell-write tools always route to `canUseTool`. — [SDK permissions](https://code.claude.com/docs/en/agent-sdk/permissions)
- `opusplan` uses `opus` in plan mode and `sonnet` in execution, and `opusplan[1m]` is supported. — [Model config](https://code.claude.com/docs/en/model-config)
- Agent-team teammates spawned while the lead is in plan mode plan read-only, and the lead's session auto-approves their plan without human review. — [Agent teams](https://code.claude.com/docs/en/agent-teams)

### Inferences
- Setting `plansDirectory: "./docs/plans"` would land native plan-mode plans where VGXNESS keeps durable plans. But the native plan is a free-form proposal with no enforced checklist or task-by-task verification, so VGXNESS's plan format and discipline still add value. Teammate plan auto-approval does not satisfy a "human approved the plan" requirement.

### Gaps
- I couldn't determine the plan file naming scheme or whether plans are updated as work proceeds.

## Q7. Worktrees, background agents, agent teams, dynamic workflows, checkpoints, /loop and scheduling, sessions

### Takeaway
Claude Code now natively covers most parallel-execution plumbing: worktree isolation with enforced write boundaries, background subagents and sessions (agent view), experimental agent teams with a shared task list and mailboxes, dynamic workflows (script-orchestrated subagents with cross-checking), checkpoints/rewind, session-scoped cron (`/loop`), cloud routines, and resume, fork and branch.

### Cited Findings
**Worktrees:**
- `--worktree/-w <name>` creates `.claude/worktrees/<name>/` on branch `worktree-<name>`. It also accepts `#PR` or PR/MR URLs. `-p` skips the trust check, and `-p` runs don't clean up their worktrees. `EnterWorktree`/`ExitWorktree` tools exist, and entering a path outside `.claude/worktrees/` prompts (v2.1.206+). — [Worktrees](https://code.claude.com/docs/en/worktrees)
- Enforcement: while isolated, Claude Code blocks Edit/Write/NotebookEdit targeting the main checkout, Bash/Monitor whose cwd is the main checkout, git redirects (`-C`, `--git-dir`, `GIT_DIR`), and commands it can't verify. This applies to every subagent spawned from an isolated session. — [Worktrees](https://code.claude.com/docs/en/worktrees)
- Subagent frontmatter `isolation: worktree` gives a temp worktree, auto-removed if unchanged. The base is the default branch unless `worktree.baseRef: "head"`. `.worktreeinclude` copies gitignored files. `WorktreeCreate`/`WorktreeRemove` hooks handle non-git VCS. A periodic sweep removes old subagent and background worktrees that have no remaining work. — [Worktrees](https://code.claude.com/docs/en/worktrees)

**Subagents and background:**
- Frontmatter fields: `description, tools, disallowedTools, model, permissionMode, mcpServers, hooks, maxTurns, skills, initialPrompt, memory, effort, background, omitClaudeMd, isolation, color`. `permissionMode` and `hooks` are ignored for plugin subagents. — [Subagents](https://code.claude.com/docs/en/sub-agents)
- Ctrl+B backgrounds a running task. `/tasks` lists background work including finished subagents. Bash `run_in_background` gets 30 minutes by default and a 2-hour maximum. Forked subagents inherit the full context (`/subtask`, or `/fork` copies the session into a background session). `/branch` branches the conversation. — [Subagents](https://code.claude.com/docs/en/sub-agents); [Tools reference](https://code.claude.com/docs/en/tools-reference); [Commands](https://code.claude.com/docs/en/commands)
- Agent view (`claude agents`) is a research preview. It dispatches and monitors background sessions (`--bg`, `claude attach/logs/stop/respawn/rm`), and a dispatched session moves into its own worktree before editing. — [Run agents in parallel](https://code.claude.com/docs/en/agents); [CLI reference](https://code.claude.com/docs/en/cli-reference)
- Cross-session messaging: `ListAgents`/`SendMessage` reach other local sessions (v2.1.224+). An incoming message is marked as coming from another Claude session and cannot grant consent. — [Tools reference](https://code.claude.com/docs/en/tools-reference); [Agent teams](https://code.claude.com/docs/en/agent-teams)

**Agent teams (experimental, off by default):**
- Enable with `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`. Interactive only, so they don't run in `-p`/SDK. Components: a lead, teammates, a shared task list (`~/.claude/tasks/{team}`), and mailboxes (`~/.claude/teams/{team}/inboxes/{agent}.json`). Hooks `TeammateIdle`, `TaskCreated` and `TaskCompleted` can block with exit 2, which acts as a quality gate. — [Agent teams](https://code.claude.com/docs/en/agent-teams)
- Limitations: teammates are not isolated in worktrees ("partition the work"), in-process teammates aren't restored on resume, task status can lag, there is one team per session, nesting isn't allowed, and the lead is fixed. — [Agent teams](https://code.claude.com/docs/en/agent-teams); [Run agents in parallel](https://code.claude.com/docs/en/agents)

**Dynamic workflows:**
- A JS script Claude writes that orchestrates dozens to hundreds of subagents in the background, resumable in-session. It can "have independent agents adversarially review each other's findings". Available on paid plans, the API, Bedrock, Vertex and Foundry (Pro must enable it in `/config`). There is a `Workflow` tool and a `/workflows` view, and `/deep-research` is bundled. — [Workflows](https://code.claude.com/docs/en/workflows)
- `/batch` splits a large change into 5–30 worktree-isolated background subagents after you approve a plan. — [Commands](https://code.claude.com/docs/en/commands)

**Bundled quality skills:**
- `/code-review` (effort levels, `--fix`, `--comment`, `ultra` cloud review), `/simplify` (four parallel review agents), and `/verify` (build, run and observe the app; user-invoked only since v2.1.215). `/goal <condition>` keeps working across turns until a condition is met. — [Commands](https://code.claude.com/docs/en/commands)

**Checkpoints:**
- A checkpoint is taken before each prompt, covering edits made by file-editing tools only (Bash changes are not tracked). The last 100 are kept, and they persist with the session so `/rewind` works after resume. `/rewind` (Esc Esc) offers restore code and/or conversation, or summarize from or up to a point. The SDK has file checkpointing. — [Checkpointing](https://code.claude.com/docs/en/checkpointing); [SDK file checkpointing](https://code.claude.com/docs/en/agent-sdk/file-checkpointing)

**Scheduling:**
- `/loop [interval] [prompt]` uses CronCreate/CronList/CronDelete. It is session-scoped, restored on resume when unexpired, has a 7-day expiry, and a 1-minute minimum interval. Desktop scheduled tasks run locally and persist. Cloud routines (`/schedule`) need a 1-hour minimum and use a fresh clone. — [Scheduled tasks](https://code.claude.com/docs/en/scheduled-tasks)

**Sessions:**
- `--resume` accepts an ID, a name, or a .jsonl path, and searches all projects (v2.1.223+). `--fork-session` creates a new ID. `--continue` skips `-p`/SDK sessions unless itself run with `-p`. — [CLI reference](https://code.claude.com/docs/en/cli-reference)

### Inferences
- "Single workspace writer" can be enforced natively by putting delegated writers in `isolation: worktree`. Worktree enforcement blocks writes to the main checkout, so only the main session writes it. Alternatively, keep subagents read-only via `tools:` lists. Agent teams break the single-writer invariant because they have no isolation, so they are not a fit.
- `TaskCompleted`/`SubagentStop`/`Stop` hooks are native gating points for "don't claim done without verification", but they are mechanisms. The verification policy and labels stay with VGXNESS.

### Gaps
- I didn't fetch the agent-view or routines pages in depth, nor anthropics/claude-code CHANGELOG.md directly. Version facts come from the docs' inline notes.

## Q8. Observability: statusline, /cost, OpenTelemetry

### Takeaway
`/cost` is now an alias of `/usage`. The statusline script receives JSON on stdin with cost, duration, lines changed and context-window data. OpenTelemetry exports metrics, events (logs) and beta traces with spans for interaction, LLM request, tool and hook. Agent SDK and `-p` runs can be parented under a caller trace via `TRACEPARENT`.

### Cited Findings
- `/cost` is an alias for `/usage`. The Session block computes cost locally at list price. — [Commands](https://code.claude.com/docs/en/commands); [Costs](https://code.claude.com/docs/en/costs)
- Statusline stdin JSON includes `cost.total_cost_usd`, `cost.total_duration_ms`, `cost.total_api_duration_ms`, `cost.total_lines_added/removed`, `context_window.used_percentage`, `context_window.total_input_tokens/total_output_tokens`, and `model.display_name`. — [Statusline](https://code.claude.com/docs/en/statusline)
- OTel setup: `CLAUDE_CODE_ENABLE_TELEMETRY=1`, `OTEL_METRICS_EXPORTER`, `OTEL_LOGS_EXPORTER`, `OTEL_EXPORTER_OTLP_*`. Metrics: `claude_code.session.count`, `lines_of_code.count`, `pull_request.count`, `commit.count`, `cost.usage`, `token.usage`, `code_edit_tool.decision`, `active_time.total`. — [Monitoring](https://code.claude.com/docs/en/monitoring-usage)
- OTel events include `user_prompt`, `assistant_response`, `tool_result`, `api_request`, `api_error`, `tool_decision`, `permission_mode_changed`, `mcp_server_connection`, `plugin_loaded`, `plugin_installed`, `skill_activated`, `hook_execution_start/complete`, `hook_plugin_metrics`, `compaction` and `subagent_completed`, all prefixed `claude_code.`. — [Monitoring](https://code.claude.com/docs/en/monitoring-usage)
- Traces (beta): `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1` plus `OTEL_TRACES_EXPORTER`. The span tree is `claude_code.interaction` → `llm_request`, `tool` (`tool.blocked_on_user`, `tool.execution`), and `hook`. Content is redacted unless `OTEL_LOG_USER_PROMPTS`/`OTEL_LOG_TOOL_DETAILS`/`OTEL_LOG_TOOL_CONTENT` is set. In SDK and `-p` sessions the interaction span becomes a child of the caller's span when `TRACEPARENT` is set, and Bash subprocesses inherit `TRACEPARENT`. — [Monitoring](https://code.claude.com/docs/en/monitoring-usage)
- A plugin cost and usage measurement guide exists. — [Measure plugins](https://code.claude.com/docs/en/plugins/measure)

### Inferences
- For evaluation traces, OTel events and spans (`skill_activated`, `subagent_completed`, `tool_decision`) plus stream-json give a native trace source. VGXNESS doesn't need its own trace recorder for Claude Code as a host.

### Gaps
- I didn't read the attribute tables for each event.

## Mapping for VGXNESS

### Takeaway
Drop or delegate the mechanics: execution isolation, parallelism, background work, plugin-eval harnessing, tracing and cost accounting. Claude Code now does these natively. Keep the policy: durable repo plans with checklists, single-writer discipline as a rule, independent verification and review of a frozen candidate, and strict delivery labels. Nothing native implements those semantics. For CI, use `claude plugin eval` as the primary gate, plus a thin `claude --bare -p --plugin-dir … --output-format stream-json` smoke test or an Agent SDK harness for structural assertions that native graders can't express.

### Cited Findings
- Native coverage that overlaps the Manager:
  - Bounded delegation: subagents with tool lists, `maxTurns`, model and effort; `--max-budget-usd`; `isolation: worktree`. — [Subagents](https://code.claude.com/docs/en/sub-agents); [CLI reference](https://code.claude.com/docs/en/cli-reference)
  - Write isolation enforcement in worktrees. — [Worktrees](https://code.claude.com/docs/en/worktrees)
  - Background and parallel execution: Ctrl+B, `/tasks`, agent view, workflows, `/batch`. — [Run agents in parallel](https://code.claude.com/docs/en/agents)
  - In-session task tracking: the Task tools, `CLAUDE_CODE_TASK_LIST_ID`. — [Interactive mode](https://code.claude.com/docs/en/interactive-mode)
  - Plan-then-approve gate: plan mode, `ExitPlanMode`, `plansDirectory`. — [Permission modes](https://code.claude.com/docs/en/permission-modes); [Settings reference](https://code.claude.com/docs/en/settings-reference)
  - Completion gates: `TaskCompleted`/`SubagentStop`/`Stop` hooks with exit 2. — [Agent teams](https://code.claude.com/docs/en/agent-teams); [SDK hooks](https://code.claude.com/docs/en/agent-sdk/hooks)
  - Review and verification helpers: `/code-review`, `/simplify`, `/verify`, `/goal`. — [Commands](https://code.claude.com/docs/en/commands)
  - Rollback: checkpoints and `/rewind`. — [Checkpointing](https://code.claude.com/docs/en/checkpointing)
  - Eval harness: `claude plugin eval`. — [Plugin evals](https://code.claude.com/docs/en/plugin-evals)
  - Telemetry: OTel. — [Monitoring](https://code.claude.com/docs/en/monitoring-usage)
- Native gaps relevant to VGXNESS:
  - Task lists live in `~/.claude/tasks`, not the repo, and the tools are off by default on newer models. — [Tools reference](https://code.claude.com/docs/en/tools-reference)
  - Agent teams have no worktree isolation, are experimental, and are interactive-only. — [Agent teams](https://code.claude.com/docs/en/agent-teams)
  - Teammate plans are auto-approved by the lead. — [Agent teams](https://code.claude.com/docs/en/agent-teams)
  - `/verify` is user-invoked only. — [Commands](https://code.claude.com/docs/en/commands)
  - Plugin eval has no custom-code graders. — [Plugin evals](https://code.claude.com/docs/en/plugin-evals)
  - Bash edits aren't checkpointed. — [Checkpointing](https://code.claude.com/docs/en/checkpointing)

### Inferences
**Drop or delegate to native features:**
- **Execution plumbing:** VGXNESS shouldn't re-implement background agents, parallel fan-out, worktree management or cleanup. Its policy should say "delegate via subagents; writers use `isolation: worktree` or the main session is the sole writer" and rely on native enforcement.
- **Eval runner:** retire the custom offline runner for Claude Code as a host in favor of `claude plugin eval` cases under the plugin's `evals/`. Keep a custom runner only for other hosts.
- **Trace recording for Claude Code:** use stream-json (`--forward-subagent-text`, `--include-hook-events`) and OTel instead of bespoke capture.
- **Cost and turn caps:** use `--max-budget-usd` and `--max-turns`, plus `maxTurns` in subagent frontmatter.
- **Generic review helpers:** VGXNESS can call `/code-review` or `/simplify` as reviewer tools. They don't replace the frozen-candidate rule, though.

**Keep as VGXNESS's job:**
- Durable Markdown plans in the repo with checklists and resume-from-first-unchecked. Native tasks are ephemeral and model-gated, and native plans default to `~/.claude/plans` without checklist semantics. Optional alignment: set `plansDirectory` to the VGXNESS plans dir so plan-mode output lands there.
- The single-writer invariant as policy, plus detecting violations (for example a PreToolUse hook that denies Edit/Write when `agent_id` is set and the subagent isn't the designated writer).
- Independent verification and review of a frozen candidate (commit SHA or diff hash) before VERIFIED/DELIVERED, and the delivery labels themselves. Native hooks (`TaskCompleted`, `Stop`) can enforce "no completion without evidence" mechanically, but the evidence contract is VGXNESS's.
- Avoid agent teams for VGXNESS flows, since they are experimental, don't isolate writers, and don't run headless.

**Concrete CI approach for plugin tests (recommended layering):**
1. **Static:** `claude plugin validate <plugin-dir>`.
2. **Load smoke (cheap, deterministic):**
   ```
   claude --bare -p "/vgxness:<skill> <args>" --plugin-dir ./plugin \
     --output-format stream-json --verbose --permission-mode dontAsk \
     --permission-prompts none --max-turns 8 --max-budget-usd 1 \
     --model <pinned> --no-session-persistence > run.ndjson
   ```
   Then use jq to assert: the `system/init` `.plugins[].name` contains the plugin, `.plugin_errors` is absent, `.skills` contains `vgxness:<skill>`, `.mcp_server_errors` is absent, a `tool_use` `name=="Skill"` appears, there is no Edit/Write from a subagent (the `parent_tool_use_id` is non-null on those), and the final `result.subtype=="success"` with `total_cost_usd` under the budget. Auth comes from `ANTHROPIC_API_KEY`, since `--bare` ignores OAuth.
3. **Behavioral evals (primary gate):** `claude plugin eval ./plugin --trust-plugin --json results.json --threshold 0.8 --model <pinned> --judge-model <pinned> --no-publish --max-cost-usd 20 --ablation none` (use `with-without` on nightly runs for Δ). Graders: `tool_used` (Skill fired, Agent spawned, `min:0,max:0` on Edit for read-only roles), `tool_order` (plan file written before implementation; verification command before the "DELIVERED" output), `regex` on the final message for the strict delivery labels, `file_exists` for plan files. Mock MCP servers, and seed repos via `scaffold_script` with `--scaffold`.
4. **Code-level assertions that native graders can't express:** a small TS Agent SDK harness with `plugins:[{type:"local",path}]`, `settingSources: []`, PreToolUse/SubagentStart/SubagentStop/TaskCompleted callbacks that record events with `agent_id`, and `canUseTool` to deny or inject failures. Use it in a separate nightly job, since it costs more and is less stable.
5. Pin the Claude Code version in CI (`claude install <version>`) because many behaviors are version-gated. Record `claudeVersion` from eval JSON and `claude_code_version` from init.

### Gaps
- I didn't verify how `claude plugin eval` treats a plugin's own subagents that set `isolation: worktree` inside the empty-workspace sandbox (no git repo unless scaffolded). A scaffold script that runs `git init` is likely required.
- It's unclear whether `plugin eval` runs honor `--bare`-equivalent auth (API key only) on Linux runners without a sandbox backend when Bash is granted. The docs say runs are refused without a sandbox backend.
