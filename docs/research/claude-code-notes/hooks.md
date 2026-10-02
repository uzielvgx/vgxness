# Claude Code hooks system (state as of 2026-10-01, Claude Code v2.1.287)

Primary sources: the official hooks reference (`https://code.claude.com/docs/en/hooks`, raw markdown at `https://code.claude.com/docs/en/hooks.md`), the hooks guide (`https://code.claude.com/docs/en/hooks-guide`), the plugin manifest reference (`https://code.claude.com/docs/en/plugins-reference`), plugin components (`https://code.claude.com/docs/en/plugins/components`), and `anthropics/claude-code` CHANGELOG.md (latest entry v2.1.287). No third-party sources were needed. VGXNESS-side facts come from the local repo (`internal/cli/memory.go`, `internal/providers/opencode/integration.go`).

Short URLs used below:
- REF = https://code.claude.com/docs/en/hooks
- GUIDE = https://code.claude.com/docs/en/hooks-guide
- MANIFEST = https://code.claude.com/docs/en/plugins-reference
- COMP = https://code.claude.com/docs/en/plugins/components
- CL = https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md

## Full list of hook events, when they fire, matcher semantics

### Takeaway
There are now about 30 events, well beyond the classic dozen. Newer ones include Setup, UserPromptExpansion, StopFailure, PostToolBatch, PermissionDenied, PostCompact, Pre/PostModelSwitch, MessageDisplay, InstructionsLoaded, ConfigChange, CwdChanged, FileChanged, DirectoryAdded, Worktree*, Task*, TeammateIdle and Elicitation*. A matcher is an exact string (or a `|`/`,` list) when it contains only `[A-Za-z0-9_\- ,|]`. Any other character makes it an unanchored JS regex. `*`, `""` or an omitted matcher matches everything.

### Cited Findings
- Event table and matcher values ([REF](https://code.claude.com/docs/en/hooks)):
  - **SessionStart**. Matchers: `startup` (new session), `resume` (`--resume`, `--continue`, `/resume`), `clear` (`/clear`), `compact` (auto or manual compaction), `fork` (`--fork-session`, `/fork`, `/branch`, moving a conversation to the background). Before v2.1.214, forks reported `"resume"`.
  - **SessionEnd**. Matchers/reasons: `clear`, `resume`, `logout`, `prompt_input_exit`, `other`. `bypass_permissions_disabled` was removed in v2.1.234.
  - **Setup**. Fires with `--init-only`, or with `-p` plus `--init`/`--maintenance`. Matchers: `init`, `maintenance`. It doesn't fire on every launch.
  - **UserPromptSubmit**: no matcher. **UserPromptExpansion**: matches the slash command/skill name.
  - **PreToolUse / PostToolUse / PostToolUseFailure / PermissionRequest / PermissionDenied**: match on tool name, e.g. `Bash`, `Edit|Write`, `mcp__memory__.*`. Plugin MCP tools are named `mcp__plugin_<plugin>_<server>__<tool>`.
  - **PostToolBatch**: no matcher. It fires once the full batch of parallel tool calls resolves.
  - **Stop**: no matcher. Doesn't fire on user interrupt. API errors fire **StopFailure** instead, with matchers `rate_limit`, `overloaded`, `authentication_failed`, `billing_error`, `invalid_request`, `model_not_found`, `server_error`, `max_output_tokens`, `unknown`, etc.
  - **SubagentStart / SubagentStop**: match on agent type (`general-purpose`, `Explore`, `Plan`, custom names, plugin-scoped `^my-plugin:reviewer$`).
  - **PreCompact / PostCompact**: `manual` (`/compact`) or `auto` (auto-compact window reached).
  - **Notification**: `permission_prompt`, `idle_prompt`, `auth_success`, `elicitation_dialog`, `elicitation_url_dialog`, `elicitation_complete`, `elicitation_response`, `agent_needs_input`, `agent_completed`, `quota_auto_resume_*`.
  - **InstructionsLoaded**: `session_start`, `nested_traversal`, `path_glob_match`, `include`, `compact`.
  - **ConfigChange**: `user_settings`, `project_settings`, `local_settings`, `policy_settings`, `skills`.
  - **FileChanged**: literal filenames. **CwdChanged**: none. **DirectoryAdded**: `slash_command`, `register_repo_root`.
  - **WorktreeCreate / WorktreeRemove, TaskCreated, TaskCompleted, TeammateIdle**: none.
  - **PreModelSwitch / PostModelSwitch**: model canonical name. **Elicitation / ElicitationResult**: MCP server name. **MessageDisplay**: none (display-only).
- Matcher rules ([REF](https://code.claude.com/docs/en/hooks)):
  - "A matcher on the regular-expression path is tested with JavaScript's `RegExp.prototype.test`". So `Edit.*` also matches `NotebookEdit`; use `^Edit$` for a whole-string match. Matchers are case-sensitive ([GUIDE](https://code.claude.com/docs/en/hooks-guide)).
  - FileChanged and StopFailure use a narrower exact set (letters, digits, `_`, `|` only).
- Lifecycle order: optional Setup, then SessionStart, then a per-turn loop (UserPromptSubmit, then the tool loop PreToolUse → PermissionRequest → PostToolUse/Failure → PostToolBatch, then Stop/StopFailure), then TeammateIdle, PreCompact, PostCompact, and finally SessionEnd. The rest are standalone async events ([REF](https://code.claude.com/docs/en/hooks)).
- History of the events VGXNESS cares about ([CL](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md)):
  - PreCompact added in 1.0.48. SessionStart in 1.0.62. SessionEnd in 1.0.85. PostCompact in 2.1.76.
  - PreCompact gained blocking (exit 2 or `{"decision":"block"}`) in 2.1.105.
  - Pre/PostModelSwitch added in 2.1.251.
  - `fork` source added in 2.1.214.
- `/goal` is a built-in, session-scoped, prompt-based Stop hook ([REF](https://code.claude.com/docs/en/hooks)).

### Inferences
- The VGXNESS lifecycle needs only SessionStart (all sources), SessionEnd, PreCompact/PostCompact and possibly Stop. PreToolUse is only needed if the plugin guards writes.
- Use exact-string matchers (`startup|resume|clear|compact`) so you don't accidentally take the regex path.

### Gaps
- I didn't read every niche event schema (TaskCreated, Elicitation, MessageDisplay) in depth. VGXNESS doesn't need them.

## Input JSON fields: common and per event

### Takeaway
Every event gets `session_id`, `transcript_path`, `cwd`, `hook_event_name`, plus `prompt_id`, `scratchpad_dir`, `permission_mode` and `effort` where they apply. Subagent context adds `agent_id`/`agent_type`. The per-event fields that matter for VGXNESS:
- SessionStart: `source`, `model`, `session_title`, plus resume-cost fields.
- SessionEnd: `reason`.
- PreCompact: `trigger`, `custom_instructions`.
- PostCompact: `trigger`, `compact_summary`.
- Stop: `stop_hook_active`, `last_assistant_message`, `background_tasks`, `session_crons`.

### Cited Findings
- Common fields ([REF](https://code.claude.com/docs/en/hooks)):
  - `session_id`, `transcript_path`, `cwd`, `hook_event_name`.
  - `prompt_id`: UUID. Absent until the first user input. v2.1.196+.
  - `scratchpad_dir`: v2.1.257+.
  - `permission_mode`: `default|plan|acceptEdits|auto|dontAsk|bypassPermissions`. Not every event gets it.
  - `effort.level`: tool-context events only.
  - Inside a subagent: `agent_id` and `agent_type`.
- The transcript file "is written asynchronously and may lag the in-memory conversation". Hooks that need the final text should use `last_assistant_message` ([REF](https://code.claude.com/docs/en/hooks)).
- SessionStart input: `source`, plus optional `model` (can be omitted, e.g. after `/clear`), `agent_type` and `session_title`. On resume/fork with at least one prior response it also gets `seconds_since_last_response`, `context_tokens`, `prompt_cache_likely_expired` and `estimated_cache_write_usd` (v2.1.251+) ([REF](https://code.claude.com/docs/en/hooks)).
- SessionEnd input: `reason` ([REF](https://code.claude.com/docs/en/hooks)).
- PreCompact input: `trigger` and `custom_instructions`. It's `null` for `auto`, and for `manual` it holds the `/compact` argument (or `null`) ([REF](https://code.claude.com/docs/en/hooks)).
- PostCompact input: `trigger` and `compact_summary`, "the conversation summary generated by the compact operation" ([REF](https://code.claude.com/docs/en/hooks)).
- Stop input: `stop_hook_active`, `last_assistant_message`, `background_tasks[]` and `session_crons[]` ([REF](https://code.claude.com/docs/en/hooks)).
  - `last_assistant_message` was added in 2.1.47. The background/cron arrays were added in 2.1.145 ([CL](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md)).
- PreToolUse input: `tool_name`, `tool_input` (tool-specific: Bash `command`; Write/Edit `file_path`, etc.) and `tool_use_id`. MCP tools also get an `mcp_server {name, source}` field (v2.1.274+). PostToolUse adds `tool_response` ([REF](https://code.claude.com/docs/en/hooks)).
- UserPromptSubmit input: `prompt`. Notification input: `type` and `message` ([REF](https://code.claude.com/docs/en/hooks)).
- Env vars ([REF](https://code.claude.com/docs/en/hooks)):
  - Hooks inherit the parent environment, minus `OTEL_*`.
  - `CLAUDE_PROJECT_DIR`, `CLAUDE_PLUGIN_ROOT`, `CLAUDE_PLUGIN_DATA`, `CLAUDE_EFFORT`.
  - `CLAUDE_CODE_REMOTE="true"` on the web. `CLAUDE_CODE_BRIDGE_SESSION_ID` (v2.1.199+).
  - `CLAUDE_ENV_FILE`: SessionStart, Setup, CwdChanged and FileChanged only. `export` lines written there are run before each Bash *tool* command.
  - There is no `$CLAUDE_MODEL`.

### Inferences
- `session_id` is the stable key to map a Claude session to a VGXNESS `external_id`. Use `cwd` (or `CLAUDE_PROJECT_DIR`, which "stays put in worktrees") as the absolute workspace path for canonical identity resolution.
- PostCompact's `compact_summary` is model-generated conversation content. Storing it would amount to transcript capture, which violates the VGXNESS "never capture transcripts" rule. Don't persist it raw.

### Gaps
- I couldn't confirm the exact PostToolUseFailure error field name (`error`?). It isn't needed.

## Output contract: exit codes, stdout, JSON fields, size limits

### Takeaway
Exit 0 means success, and stdout is parsed as JSON only if it starts with `{` and ends with `}`. Plain-text stdout becomes context only for SessionStart, UserPromptSubmit, UserPromptExpansion and PostModelSwitch. Exit 2 blocks on blockable events; for SessionStart and SessionEnd it only shows stderr to the user. Any other exit code is a non-blocking error and the action proceeds. `additionalContext` goes inside `hookSpecificOutput` and is hard-capped at 10,000 chars per string: anything over is spilled to a file and replaced by the path plus a 2,000-char preview, and the cap can't be raised.

### Cited Findings
- **Exit 0** ([REF](https://code.claude.com/docs/en/hooks)):
  - stdout goes to the debug log, except for UserPromptSubmit, UserPromptExpansion, SessionStart and PostModelSwitch, where plain text is added as context.
  - Output that starts with `{` and ends with `}` is parsed as JSON. Anything else, "a JSON array or a quoted JSON string included", is plain text.
  - If the output looks like JSON but fails to parse, that's a non-blocking error, and on the context events the text is not added (since v2.1.248).
  - stderr on exit 0 goes to the debug log only, so Claude never sees it.
- **Exit 2** ([REF](https://code.claude.com/docs/en/hooks)):
  - Blocks: PreToolUse, UserPromptSubmit, UserPromptExpansion, Stop, SubagentStop, TeammateIdle, TaskCreated, TaskCompleted, ConfigChange (except policy), PostToolBatch, PreCompact, PreModelSwitch, Elicitation(Result), Worktree*.
  - Doesn't block: SessionStart, SessionEnd, SubagentStart, PostCompact, CwdChanged and FileChanged show stderr to the user only. PostToolUse and PostToolUseFailure show stderr to Claude. Notification, Setup, StopFailure, PermissionDenied and InstructionsLoaded ignore it.
  - PermissionRequest ignores exit 2; deny via the decision object instead.
  - On SessionStart, SubagentStart and PostModelSwitch, the exit-2 stderr renders as a `<hook name> hook error` notice; "Claude doesn't see it".
  - JSON is still read on exit 2, but it can't override the block.
- **Other exit codes**: if the JSON validates, it alone decides the outcome and no error is reported. Otherwise it's a non-blocking error: "the transcript shows a `<hook name> hook error` notice, then the first line of stderr prefixed with `Failed with non-blocking status code:`" ([GUIDE](https://code.claude.com/docs/en/hooks-guide)).
- **Universal JSON fields** ([REF](https://code.claude.com/docs/en/hooks)):
  - `continue` (default `true`; `false` stops Claude entirely and takes precedence).
  - `stopReason`: shown to the user and stays in the conversation.
  - `suppressOutput`: "Has no effect". The field is accepted but ignored.
  - `systemMessage`: a warning shown to the user.
  - `terminalSequence`: OSC 0/1/2/9/99/777 and BEL only, interactive sessions only.
- **Decision patterns** ([REF](https://code.claude.com/docs/en/hooks)):
  - Top-level `decision:"block"` plus `reason`: UserPromptSubmit, UserPromptExpansion, PostToolUse, PostToolUseFailure, PostToolBatch, Stop, SubagentStop, ConfigChange, PreCompact.
  - PreToolUse uses `hookSpecificOutput.permissionDecision`: `allow|deny|ask|defer`.
    - Precedence across hooks: `deny` > `defer` > `ask` > `allow`.
    - `permissionDecisionReason` is shown to Claude on deny and to the user on ask.
    - `updatedInput` replaces the entire input object.
    - Top-level `approve`/`block` for PreToolUse is deprecated.
  - PermissionRequest uses `hookSpecificOutput.decision.behavior` (`allow|deny`) plus optional `updatedInput`.
  - PostToolUse can return `hookSpecificOutput.updatedToolOutput`.
  - Setup, Notification, SessionEnd, PostCompact, InstructionsLoaded, StopFailure, CwdChanged, DirectoryAdded and FileChanged have no decision control.
- **SessionStart output** ([REF](https://code.claude.com/docs/en/hooks)):
  - `hookSpecificOutput.additionalContext`, `initialUserMessage` (`-p` only), `sessionTitle` (ignored on `clear`/`compact`), `watchPaths` and `reloadSkills`.
  - "Since plain stdout already reaches Claude for this event, a hook that only loads context can print to stdout directly without building JSON."
- **Stop output**: `decision:"block"` requires `reason`. Alternatively, `hookSpecificOutput.additionalContext` gives non-error feedback, but it also continues the turn. There's an 8-consecutive-continuation cap (`CLAUDE_CODE_STOP_HOOK_BLOCK_CAP`) ([REF](https://code.claude.com/docs/en/hooks)).
- **PreCompact and PostCompact discard `systemMessage` and `continue`. SessionEnd discards all JSON output fields** ([REF](https://code.claude.com/docs/en/hooks)).
- **Size limit**: `additionalContext`, `systemMessage`, `initialUserMessage` and plain stdout are each capped at 10,000 chars ([REF](https://code.claude.com/docs/en/hooks)).
  - Over the cap, the text is saved to a file in the session dir, and Claude gets the path plus a 2,000-char preview.
  - "This cap has no setting or environment variable to raise it". "Claude Code doesn't ask Claude to read the file".
  - Introduced in 2.1.89 ([CL](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md)).
- **How `additionalContext` is delivered** ([REF](https://code.claude.com/docs/en/hooks)):
  - Wrapped in a system reminder. For SessionStart it lands "at the start of the conversation, before the first prompt".
  - When several hooks return it, "Claude receives all of the values".
  - Docs advise: "Write the text as factual statements rather than imperative system instructions… Text framed as out-of-band system commands can trigger Claude's prompt-injection defenses."
- Misplaced fields (e.g. top-level `additionalContext`) are silently ignored. The debug log shows `Hook JSON output had unrecognized keys` ([GUIDE](https://code.claude.com/docs/en/hooks-guide)).
- Shell profiles that `echo` can prepend text and break JSON parsing ([GUIDE](https://code.claude.com/docs/en/hooks-guide)).

### Inferences
- VGXNESS should emit exactly one JSON object built with a real JSON encoder (Go `encoding/json`), and keep the handoff block well under 10,000 chars (say 6–8k, after the wrapper) so it isn't spilled to a file.
- The existing OpenCode wrapper includes an imperative line ("Before your terminal response, use the existing MCP memory_session_summary…"). That phrasing risks tripping the prompt-injection defenses the docs describe. Prefer factual framing (e.g. "Prior same-project handoff (untrusted data, may be stale):").

### Gaps
- None material.

## Hook types, timeouts, async, parallelism, dedup

### Takeaway
There are five handler types: `command`, `http`, `mcp_tool`, `prompt` and `agent` (agent is experimental). **SessionStart and Setup support only `command` and `mcp_tool`**, and `mcp_tool` is skipped at launch. Defaults are 600 s for command/http/mcp_tool, 30 s for prompt and 60 s for agent. SessionEnd shares a 1.5 s budget. All matching hooks run in parallel, and identical handlers across settings files are deduped.

### Cited Findings
- **Command hooks** ([REF](https://code.claude.com/docs/en/hooks)):
  - Fields: `command`, `args`, `async`, `asyncRewake`, `shell` (`bash|powershell`), `if`, `timeout`, `statusMessage`, `once`.
  - **Exec form** (`args` present): `command` is resolved on PATH and spawned directly with no shell. Placeholders are substituted as plain strings. On Windows it needs a real `.exe`.
  - **Shell form**: `sh -c` (Git Bash or PowerShell on Windows).
  - `args` was added in 2.1.139 ([CL](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md)).
- **`if` field**: a permission-rule filter such as `Bash(git *)` or `Edit(*.ts)`, so a handler spawns only when both the matcher and `if` match ([REF](https://code.claude.com/docs/en/hooks)).
- **Other handler types** ([REF](https://code.claude.com/docs/en/hooks)):
  - HTTP: POSTs the JSON. A 2xx response body is parsed like stdout; non-2xx is a non-blocking error. Header env interpolation is limited to `allowedEnvVars`. Added in 2.1.63 ([CL](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md)).
  - `mcp_tool` (added 2.1.118): calls a configured MCP server's tool (`server`, `tool`, `input` with `${tool_input.x}` templating). On SessionStart at launch (including `--continue`/`--resume`) and Setup, these hooks are skipped with the debug message "no MCP client context". They do run on SessionStart after `/clear` or compaction.
  - Prompt and agent hooks return `{"ok": bool, "reason": ...}`. `$ARGUMENTS` is replaced by the input JSON. Agent hooks get up to 50 tool-use turns. Both use "the session's background-function model" unless `model` is set ([REF](https://code.claude.com/docs/en/hooks); [GUIDE](https://code.claude.com/docs/en/hooks-guide)).
- **Event × type support** ([REF](https://code.claude.com/docs/en/hooks)):
  - All five types: PreToolUse, PostToolUse(Failure), PostToolBatch, PermissionDenied, Stop, SubagentStop, Task*, TeammateIdle, UserPromptSubmit/Expansion.
  - command/http/mcp_tool only: PreCompact, PostCompact, SessionEnd, SubagentStart, Notification and others.
  - command/mcp_tool only: SessionStart and Setup.
  - Configuring prompt/agent hooks on SessionStart, Setup or SubagentStart shows an explicit error (since 2.1.142) ([CL](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md)).
- **Timeouts** ([REF](https://code.claude.com/docs/en/hooks)):
  - Defaults are 600 s (command/http/mcp_tool), 30 s (prompt), 60 s (agent). The command/http/mcp default drops to 30 s on UserPromptSubmit and Pre/PostModelSwitch, and to 10 s on MessageDisplay.
  - On timeout, output is discarded, and most events don't block.
  - The tool-hook timeout went from 60 s to 10 min in 2.1.3 ([CL](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md)).
- **SessionEnd budget** ([REF](https://code.claude.com/docs/en/hooks)):
  - "SessionEnd hooks have a default timeout of 1.5 seconds", applying on exit, `/clear` and interactive `/resume`.
  - The budget rises to the highest per-hook `timeout` in your settings files, up to 60 s. But "**Timeouts set on plugin-provided hooks don't raise the budget.**"
  - The override is `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS` (since v2.1.268 it also sets each hook's default).
- **Async** ([REF](https://code.claude.com/docs/en/hooks)):
  - `async: true` (command hooks only) runs in the background. Decision fields have no effect and `timeout` isn't enforced.
  - `additionalContext`/`systemMessage` are delivered on the next turn.
  - `-p` teardown kills still-running async hooks (outcome `cancelled`), so "if your hook's work must outlive a `claude -p` session, start a fully detached process from it". Async firings aren't deduplicated.
  - `asyncRewake: true` wakes Claude on exit 2, and its `timeout` *is* enforced.
- **Parallelism and dedup**: "All matching hooks run in parallel. If you define the same handler in more than one settings file, it runs once. A plugin's or skill's copy of the same handler stays separate." With multiple `updatedInput`s, the last to finish wins, non-deterministically ([REF](https://code.claude.com/docs/en/hooks); [GUIDE](https://code.claude.com/docs/en/hooks-guide)).
- **SessionStart runs in the background at interactive launch, `--continue`/`--resume` and `/clear`**: the user can type right away, "Claude's first response still waits for the hooks to finish". In-session `/resume` waits. If the user runs `/clear` or switches conversations while hooks are still running, "nothing they return applies". "SessionStart runs on every session, so keep these hooks fast." ([REF](https://code.claude.com/docs/en/hooks)).

### Inferences
- An HTTP or MCP handoff injection isn't an option on SessionStart. It has to be `type: "command"`, which fits a Go binary well.
- A slow `vgxness` (SQLite open, migrations, FTS query) directly delays Claude's first response. Target well under 1 s.

### Gaps
- The docs don't say whether parallel hooks for the same event start in a defined order. Assume no ordering.

## Configuration: settings schema, plugin hooks.json, placeholders, frontmatter, disable/managed

### Takeaway
Hooks use three levels: event → matcher group → handlers. They live in user, project and local settings, managed policy, plugin `hooks/hooks.json` (plus the manifest `hooks` key), and skill/subagent frontmatter, and all levels merge additively. Plugins reference bundled files via `${CLAUDE_PLUGIN_ROOT}` and keep state in `${CLAUDE_PLUGIN_DATA}`. `disableAllHooks` and managed `allowManagedHooksOnly` can turn plugin hooks off.

### Cited Findings
- Schema: `{"hooks": {"<Event>": [{"matcher": "...", "hooks": [{"type": "command", "command": "...", "args": [], "timeout": 30, "statusMessage": "..."}]}]}, "disableAllHooks": false}` ([REF](https://code.claude.com/docs/en/hooks)).
- Locations: `~/.claude/settings.json`, `.claude/settings.json`, `.claude/settings.local.json`, managed policy, plugin `hooks/hooks.json`, skill frontmatter (rest of session after invocation) and subagent frontmatter (while it runs). "Hook entries merge across settings levels rather than replacing each other." Cloud sessions don't read `~/.claude/settings.json` ([REF](https://code.claude.com/docs/en/hooks)).
- **Plugin hooks** ([COMP](https://code.claude.com/docs/en/plugins/components)):
  - They live in `hooks/hooks.json` at the plugin root, under a top-level `"hooks"` key, with an optional `"description"`. The manifest `hooks` key (path, inline object or array) merges with that file ([MANIFEST](https://code.claude.com/docs/en/plugins-reference)).
  - Plugin hooks "don't wait for one of the plugin's skills or commands to be used. Claude Code registers them when a session loads the plugin".
  - After changes, run `/reload-plugins` or start a new session.
  - Plugin agent files ignore `hooks` frontmatter.
- **Placeholders** ([MANIFEST](https://code.claude.com/docs/en/plugins-reference)):
  - `${CLAUDE_PLUGIN_ROOT}` is the installed version's path and "changes when the plugin updates, so don't write state there".
  - `${CLAUDE_PLUGIN_DATA}` = `~/.claude/plugins/data/<id>/`. It persists across updates and is deleted on uninstall unless `--keep-data`.
  - `${CLAUDE_PROJECT_DIR}` is the project root.
  - All three are substituted in hook `command`/`args` and exported to the hook process, but they are *not* present in the Bash tool's environment.
- **Shell-form quoting**: shell-form hooks must quote `"${CLAUDE_PLUGIN_ROOT}"`. `claude plugin validate` warns when it's unquoted (2.1.281). `${user_config.*}` is rejected in shell form (2.1.207 shell-injection fix); use exec-form `args` or `$CLAUDE_PLUGIN_OPTION_<KEY>` ([MANIFEST](https://code.claude.com/docs/en/plugins-reference); [CL](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md)).
- **Plugin `bin/`**: "Files here are on the Bash tool's `PATH` while the plugin is enabled". claude.ai and Cowork don't install plugins that ship a top-level `bin/` ([MANIFEST](https://code.claude.com/docs/en/plugins-reference)).
- `userConfig` options get prompted on enable. `sensitive` values go to the secure store, and every option is exported to hooks as `CLAUDE_PLUGIN_OPTION_<KEY>` ([MANIFEST](https://code.claude.com/docs/en/plugins-reference)).
- **Frontmatter hooks** ([REF](https://code.claude.com/docs/en/hooks); [CL](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md)):
  - Skill and subagent frontmatter accept a `hooks:` YAML block in the same shape.
  - `once: true` is honored only in skill frontmatter.
  - In a subagent, `Stop` is converted to `SubagentStop`.
  - Project subagent frontmatter hooks require workspace trust (since 2.1.218).
- **`disableAllHooks: true`** ([REF](https://code.claude.com/docs/en/hooks)):
  - It's resolved by settings precedence, so a project `false` overrides a user `true`. Use `--settings '{"disableAllHooks": true}'` for a single run.
  - It can't disable managed hooks unless it's set at the managed level.
  - Individual hooks can't be disabled; delete the entry instead.
- **`allowManagedHooksOnly`** (managed) ([REF](https://code.claude.com/docs/en/hooks)):
  - Blocks user, project, local and plugin hooks, except plugins force-enabled via managed `enabledPlugins`.
  - Also disables `command`-source plugins unless `disableCommandPluginSources: false`.
  - HTTP allowlists `allowedHttpHookUrls`/`httpHookAllowedEnvVars` apply to every source.
- Settings edits are normally picked up live by a file watcher; restart if `/hooks` doesn't show the change ([REF](https://code.claude.com/docs/en/hooks); [GUIDE](https://code.claude.com/docs/en/hooks-guide)).

### Inferences
- Since `${CLAUDE_PLUGIN_ROOT}` changes on update, any VGXNESS per-session state (session_handle, lease_token) should go in `${CLAUDE_PLUGIN_DATA}` or, better, VGXNESS's own storage keyed by Claude `session_id`.
- Shipping `vgxness` inside plugin `bin/` puts it on the Bash tool PATH, but the docs don't say hook processes get that PATH. Reference it explicitly as `${CLAUDE_PLUGIN_ROOT}/bin/vgxness`, or rely on the user's PATH. A plugin with `bin/` is also blocked from claude.ai/Cowork distribution.

### Gaps
- It's unconfirmed whether plugin `bin/` is prepended to the PATH of hook processes (docs mention only the Bash tool).
- Unclear whether a hook's `CLAUDE_ENV_FILE` exports reach later hook processes; the docs only say Bash commands.

## Security, trust model, debugging

### Takeaway
Command hooks run with the user's full permissions. Interactive sessions hold back settings-file hooks until the workspace trust dialog is accepted, while `-p`/SDK sessions treat the folder as trusted. There's no per-hook approval or "review changed hooks" prompt in today's docs. Debug with `/hooks` (read-only), `Ctrl+O`, `claude --debug` / `--debug-file`, and `/debug`.

### Cited Findings
- "Command hooks execute shell commands with your full user permissions… Review and test all hook commands before adding them" ([REF](https://code.claude.com/docs/en/hooks)).
- **Workspace trust** ([REF](https://code.claude.com/docs/en/hooks)):
  - Interactive: "Claude Code holds back hooks from every settings file, including your own `~/.claude/settings.json`, until you accept the workspace trust dialog".
  - `-p`/SDK: "never shows the dialog and treats the folder as trusted, so hooks committed in a repository's `.claude/settings.json` run in a folder you've never trusted". The docs recommend `--bare` or `--settings '{"disableAllHooks": true}'` for unreviewed repos.
- Best practices: validate inputs, quote variables, block `..` traversal, use absolute paths, and skip `.env`, `.git/` and keys ([REF](https://code.claude.com/docs/en/hooks)).
- **Permission interplay** ([GUIDE](https://code.claude.com/docs/en/hooks-guide)):
  - A PreToolUse `deny` blocks even under `bypassPermissions`/`--dangerously-skip-permissions`.
  - A hook `allow` doesn't bypass settings deny rules. Hooks in settings and plugins "can tighten restrictions but not loosen them past what permission rules allow".
  - An installed *mod* (JS in-process plugin hook on `tool.check`) can override a non-managed PreToolUse block.
- A missing script yields a non-blocking error, e.g. `Failed with non-blocking status code: /bin/sh: /path/to/hook.sh: No such file or directory` (exit 127), and "a mistyped path in `settings.json` leaves the gate silently disabled" ([REF](https://code.claude.com/docs/en/hooks)).
- `"ask"` prompts are labeled by source: `[settings]`, `[plugin:<name>]` or `[skill]` ([REF](https://code.claude.com/docs/en/hooks)).
- **Debugging** ([REF](https://code.claude.com/docs/en/hooks); [GUIDE](https://code.claude.com/docs/en/hooks-guide)):
  - `claude --debug` writes to `~/.claude/debug/<session-id>.txt` and doesn't print to the terminal. `--debug-file <path>`. `/debug` enables it mid-session.
  - `CLAUDE_CODE_DEBUG_LOG_LEVEL=verbose` adds matcher counts.
  - `Ctrl+O` opens the transcript view. Async completions are visible with `--verbose`.
  - `/hooks` lists all five types with sources (User/Project/Local/Plugin/Session Hooks).
- Relevant fixes ([CL](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md)):
  - 2.1.51: statusLine/fileSuggestion ran without trust.
  - 2.1.281: `claude --bg` ran project hooks before trust.
  - 2.1.281: plugin hook-failure errors now name the plugin.

### Inferences
- A VGXNESS plugin must assume it will run in `-p`/SDK contexts on untrusted repos too. Handoff text from the DB has to be treated as untrusted and bounded, which VGXNESS already does.

### Gaps
- The docs don't say whether *plugin* hooks are subject to the workspace trust hold-back (the text says "any hook from a settings file"). Treat it as unverified.

## Does SessionStart context persist after compaction? Re-injection via `compact`

### Takeaway
The docs never say startup `additionalContext` survives compaction. They explicitly recommend a `SessionStart` hook with matcher `compact` "to re-inject critical context after every compaction", because compaction "can lose important details". SessionStart fires again with `source: "compact"`, so VGXNESS should register for `compact` as well as `startup|resume|clear`.

### Cited Findings
- "When Claude's context window fills up, compaction summarizes the conversation to free space. This can lose important details. Use a `SessionStart` hook with a `compact` matcher to re-inject critical context after every compaction." ([GUIDE](https://code.claude.com/docs/en/hooks-guide)).
- SessionStart's `compact` source fires on "Auto or manual compaction". `sessionTitle` is ignored there ([REF](https://code.claude.com/docs/en/hooks)).
- `mcp_tool` SessionStart hooks do run on post-compaction SessionStart, unlike at launch ([REF](https://code.claude.com/docs/en/hooks)).
- Injected text is saved in the transcript. Mid-session hook output is replayed on resume rather than re-run, while "SessionStart hooks run again on resume" ([REF](https://code.claude.com/docs/en/hooks)).
- CLAUDE.md files are reloaded after compaction (InstructionsLoaded `load_reason: "compact"`) ([REF](https://code.claude.com/docs/en/hooks)).
- 2.1.277 fixed continued sessions after `/clear` "missing part of their first message when a SessionStart hook printed output, causing a full prompt-cache miss" ([CL](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md)).

### Inferences
- Startup-injected system reminders are part of the pre-compaction conversation and get summarized away, or at least aren't guaranteed verbatim. Re-inject on `compact`, and make the command idempotent and cheap.
- On `resume`, the original injected text is already in the transcript and SessionStart runs again. VGXNESS should either skip re-injection on `resume` when the handoff is unchanged, or accept duplicates; re-injecting adds tokens.

### Gaps
- No official statement on exactly how the compaction summarizer treats prior system reminders.

## Version-dependent behavior and recent changes

### Takeaway
Hooks change fast. The latest release is v2.1.287. Several contract details are version-gated, so a VGXNESS plugin should document a minimum version (around v2.1.139 for exec-form `args`, and v2.1.268 for sane SessionEnd timeouts).

### Cited Findings ([CL](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md) unless noted)
- 1.0.48 PreCompact. 1.0.62 SessionStart. 1.0.68 `disableAllHooks`. 1.0.85 SessionEnd. 2.0.30 prompt-based Stop hooks.
- 2.1.0: `once: true`, agent-frontmatter hooks, prompt/agent hook types in plugins.
- 2.1.3: tool hook timeout went from 60 s to 10 min.
- 2.1.47: SessionStart deferred for faster startup; `last_assistant_message` added.
- 2.1.49: `disableAllHooks` respects the managed hierarchy.
- 2.1.63: HTTP hooks.
- 2.1.69: fixed plugin Stop/SessionEnd hooks not firing after `/plugin` ops, and plugin hooks dropped when two plugins share a command template.
- 2.1.73: fixed SessionStart firing twice on `--resume`/`--continue`.
- 2.1.74: SessionEnd killed at 1.5 s; `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS` added. 2.1.76: PostCompact. 2.1.79: SessionEnd now fires on interactive `/resume`.
- 2.1.89: 10,000-char cap with spill-to-file. 2.1.91: plugin `bin/`. 2.1.94: fixed plugin hooks failing when `CLAUDE_PLUGIN_ROOT` was unset.
- 2.1.105: PreCompact blocking. 2.1.118: `mcp_tool` hooks. 2.1.139: exec-form `args`. 2.1.143: 8-block Stop cap. 2.1.152: `reloadSkills`, `sessionTitle`. 2.1.163: Stop `additionalContext`.
- 2.1.199: SessionStart exit-2 stderr now shown. 2.1.207: `${user_config}` rejected in shell form. 2.1.214: `fork` source. 2.1.218: subagent frontmatter trust.
- 2.1.248: invalid-JSON stdout on context events is no longer injected as plain text ([REF](https://code.claude.com/docs/en/hooks)).
- 2.1.251: model-switch events; resume cost fields.
- 2.1.268: `--continue`/`--resume` no longer wait for SessionStart to render; SessionEnd env override fix.
- 2.1.271: spinner shows the running SessionStart/SessionEnd hook; Esc cancels a prompt waiting on SessionStart.
- 2.1.281: `mcp_tool` waits for connecting servers on blocking events.
- 2.1.285–2.1.287: fixed sync hooks hanging when a background child keeps stdout open; fixed `asyncRewake` with a missing script; fixed "SessionStart hooks from synced plugins not running in new cloud sessions" (2.1.287).

### Inferences
- **The 2.1.285 fix matters for VGXNESS:** if `vgxness` ever spawns a background daemon from a hook, it must close or redirect inherited stdout/stderr, or older versions hang.

### Gaps
- I didn't find official version-compat guidance for plugins, nor any `minClaudeCodeVersion` manifest field.

## Mapping for VGXNESS

### Takeaway
Port the lifecycle as a Claude Code plugin whose `hooks/hooks.json` calls a new, Claude-specific adapter subcommand (suggested: `vgxness memory hook claude-code`). The existing `vgxness memory hook --stdin` is strict: it rejects any unknown key and expects its own `{schemaVersion, operation, workspace, ...}` document, and it emits VGXNESS JSON, not Claude hook JSON. So Claude's stdin can't be piped into it directly. The adapter maps Claude input to VGXNESS operations and emits `hookSpecificOutput.additionalContext`.

### Cited Findings (local repo)
- `runMemoryHook` in `internal/cli/memory.go`:
  - Requires `--stdin` and accepts only the keys `schemaVersion, operation, workspace, provider, external_id, session_handle, lease_token, state, summary, expected_updated_at`.
  - Requires `schemaVersion==1` and an absolute `workspace`.
  - Operations: `start`, `checkpoint`, `renew`, `end` (states `completed|interrupted|cancelled`), `context` (returns `{schemaVersion, session_handle, state, handoff}`) and `summary` (saves a draft).
- The OpenCode integration (`internal/providers/opencode/integration.go`, around line 1843) wraps the handoff in `<VGXNESS LIFECYCLE session_handle=…>` … `<UNTRUSTED DATA>…</UNTRUSTED DATA>`.

### Sketch: plugin layout
```
vgxness-memory/
├── .claude-plugin/plugin.json   # {"name":"vgxness-memory","version":"0.1.0","description":"..."}
└── hooks/hooks.json
```
Don't ship `bin/`; rely on `vgxness` from the user's PATH. A bundled `bin/` blocks claude.ai/Cowork distribution, and it isn't documented to be on the hook PATH.

### Sketch: `hooks/hooks.json`
```json
{
  "description": "VGXNESS memory lifecycle: inject prior same-project handoff; record session end",
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup|resume|clear|compact",
        "hooks": [
          {
            "type": "command",
            "command": "vgxness",
            "args": ["memory", "hook", "claude-code", "session-start"],
            "timeout": 5,
            "statusMessage": "Loading VGXNESS handoff"
          }
        ]
      }
    ],
    "PreCompact": [
      {
        "matcher": "manual|auto",
        "hooks": [
          { "type": "command", "command": "vgxness",
            "args": ["memory", "hook", "claude-code", "pre-compact"], "timeout": 5 }
        ]
      }
    ],
    "SessionEnd": [
      {
        "hooks": [
          { "type": "command", "command": "vgxness",
            "args": ["memory", "hook", "claude-code", "session-end"], "timeout": 2 }
        ]
      }
    ]
  }
}
```
Exec form (`args`) avoids shell quoting and profile-echo corruption ([REF](https://code.claude.com/docs/en/hooks)).

What each adapter does:
- **session-start**
  1. Read the Claude JSON from stdin.
  2. Use `cwd` (fall back to `CLAUDE_PROJECT_DIR`) as `workspace`, `"claude-code"` as `provider`, and `session_id` as `external_id`.
  3. Run `start` (idempotent per external_id), then `context`.
  4. Persist `session_handle` and `lease_token` in VGXNESS storage keyed by `session_id`, not in `CLAUDE_PLUGIN_ROOT`.
  5. Print exactly one JSON object: `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"<factual header>\n<UNTRUSTED DATA>…≤~6000 chars…</UNTRUSTED DATA>"}}`.
  6. If there's no prior completed handoff, print nothing and exit 0.
  7. On `source=="resume"`, consider skipping when the handoff hasn't changed (it's already in the transcript).
- **pre-compact**: `checkpoint`/`renew` the lease only. Never block (always exit 0, no `decision`). Blocking auto-compaction near the context limit makes the request fail ([REF](https://code.claude.com/docs/en/hooks)).
- **session-end**: `end` with a state derived from `reason`. Suggested mapping: `prompt_input_exit`/`logout`/`other` → `completed`; `clear`/`resume` → `interrupted`, or `completed`, per VGXNESS semantics. Output is discarded anyway.

### Where the handoff *summary* comes from
- Command hooks can't call tools or generate text, so a SessionEnd/PreCompact hook can't author a summary without reading the transcript, which violates the no-transcript rule ([GUIDE](https://code.claude.com/docs/en/hooks-guide)).
- Options:
  - **(a)** Keep the OpenCode model: the model saves the summary through the VGXNESS MCP tool (`memory_session_summary`) during the session, and SessionEnd only finalizes. If you nudge it, do so with factual `additionalContext` rather than imperatives.
  - **(b)** A Stop hook that passes `last_assistant_message` into a bounded draft. That's transcript-adjacent capture of model output, so it's not recommended given the VGXNESS privacy constraints, and Stop fires on *every* turn, not only at task completion ([GUIDE](https://code.claude.com/docs/en/hooks-guide)).
  - **(c)** A prompt-type Stop hook. It costs an extra LLM call per turn, and there's the loop risk (`stop_hook_active`, 8-block cap). Not recommended.
- Do *not* persist PostCompact `compact_summary`; it's a model summary of the transcript.

### Optional write guard
- A `PreToolUse` hook with matcher `Write|Edit` (and `Bash` with `if` filters) can return `permissionDecision:"deny"`, which holds even under bypassPermissions ([GUIDE](https://code.claude.com/docs/en/hooks-guide)).
- It runs on every tool call, so keep it in-process fast.
- A missing binary silently disables the guard (non-blocking error), so don't rely on it as a security boundary ([REF](https://code.claude.com/docs/en/hooks)).

### Cost and latency risks
- SessionStart blocks Claude's first response, and the docs say "keep these hooks fast". The default 600 s timeout is far too generous, so set `timeout` to about 5 s ([REF](https://code.claude.com/docs/en/hooks)).
- Each SessionStart/compact injection adds up to about 2.5k tokens (10k chars) per occurrence. Re-injection on `resume` duplicates the text. Text over 10k chars spills to a file, and Claude isn't told to read it, so the handoff would be effectively lost ([REF](https://code.claude.com/docs/en/hooks)).
- SessionEnd has a 1.5 s shared budget, and **plugin hook `timeout`s don't raise it**. The `end` write must finish in well under 1.5 s, unless the user sets `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS` ([REF](https://code.claude.com/docs/en/hooks)).
- **Recommendation:** make `end` a tiny SQLite write. Alternatively, fork a detached process with stdio closed (the 2.1.285 hang fix shows inherited stdout can hang hooks) ([CL](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md)).
- Lease expiry is a backstop: if SessionEnd is killed or never fires (crash), the VGXNESS lease must expire to `interrupted` on its own.
- In `-p` runs, async hooks are killed at teardown, so don't use `async` for the end write ([REF](https://code.claude.com/docs/en/hooks)).

### Failure behavior when `vgxness` isn't on PATH
- Exec form resolves `command` on PATH. Shell form gives exit 127 ("No such file or directory"/"command not found"). Either way it's a **non-blocking error**: the session proceeds, the transcript shows `SessionStart hook error … Failed with non-blocking status code: …`, and nothing is injected ([REF](https://code.claude.com/docs/en/hooks); [GUIDE](https://code.claude.com/docs/en/hooks-guide)).
- This is noisy on every session and every compaction.
- Mitigation 1: a tiny wrapper (`sh -c 'command -v vgxness >/dev/null || exit 0; exec vgxness …'`) exits 0 silently. It costs shell form, quoting care and profile-echo risk.
- Mitigation 2: have the adapter itself fail closed. Any internal error → exit 0 with empty stdout, plus a debug-log-only stderr line. Never exit 2 (on SessionStart that only shows a user-facing error anyway).
- Also handle `disableAllHooks`/`allowManagedHooksOnly`: the plugin silently does nothing. Document this so users don't assume memory is active.

### Inferences
- The cleanest port is a dedicated `claude-code` adapter in the Go CLI, which keeps the strict `memory hook --stdin` contract intact, plus a plugin with exec-form hooks on SessionStart (all four sources), PreCompact (checkpoint only) and SessionEnd (fast finalize). Summary authoring stays model-driven via MCP.

### Gaps
- Unverified whether hook processes see plugin `bin/` on PATH.
- Unverified whether workspace trust gates plugin hooks.
- No official guidance on exact token cost of system reminders or how compaction treats them.
