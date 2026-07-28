# Guided OpenCode setup

The OpenCode setup wizard explains the complete plan before it changes anything. It combines the versioned VGXNESS self-installer, the governed OpenCode manager, an independent native maintenance manager, five ticket-bound subagent profiles, the managed plugin bridge, and a live provider handshake without moving policy into the CLI.

## Prerequisites

- Run the wizard from a VGXNESS candidate binary you trust.
- OpenCode must be installed, compatible, and available to the current process.
- The verification workspace must exist. The current directory is used by default.
- Choose an explicit OpenCode execution model in `provider/model` form. The example below uses `openai/gpt-5.6-sol`.
- OpenCode CLI may host tools with Bun while OpenCode Desktop hosts them with Node; the installed bridge supports both runtimes.
- CodeGraph is optional. Structural analysis uses it only when the local `codegraph` executable and a worktree-local `.codegraph/` index already exist; otherwise the explorer uses its bounded read fallback.

The wizard does not download software, edit `PATH`, install or administer CodeGraph indexes, overwrite foreign content, enable arbitrary shell execution, or silently remove files during recovery.

## Preview first

```sh
vgxness setup opencode --preview --model openai/gpt-5.6-sol
```

Preview performs read-only inspection and explains all six steps:

1. Check the candidate, explicit execution model, destinations, workspace, and OpenCode compatibility.
2. Install or update the immutable VGXNESS version behind the stable launcher.
3. Install `vgxness-manager`, the separately selectable `vgxness-native-manager`, plus the hidden `vgxness-navigator`, `vgxness-explorer`, `vgxness-implementer`, `vgxness-maintainer`, and `vgxness-reviewer` ticket-bound subagents. The governed manager keeps the existing VGXNESS authority model. The native manager deliberately bypasses VGXNESS orchestration and delegates only to OpenCode's built-in `explore` and `general` subagents; it can inspect, edit, and validate the current workspace directly for maintenance and product repair. It denies all `vgxness_*` tools, external-directory access, web access, push, destructive Git cleanup, and unapproved commits. Navigator has no workspace tools and only proposes candidate tasks; the VGXNESS runtime validates and schedules them. Maintainer remains restricted to isolated self-repair after a concrete VGXNESS failure.
4. Install the goal-first `vgxness_run` entrypoint plus the bounded `vgxness_status`, `vgxness_dispatch`, and `vgxness_orchestrate` controls with that model fixed outside agent-controlled arguments. A normal dispatch returns exactly one built-in Task directive and a later durable join; adaptive orchestration returns one or more approved Task directives per legal wave. OpenCode renders every one as a native, navigable child row. Visible children must claim their approved ticket through `vgxness_task_claim` and publish their terminal through `vgxness_task_complete`; path discovery is blocked until claim, exact file contents remain available only through `vgxness_native_read`, and structural analysis is exposed to the explorer only through the optional ticket-bound `vgxness_codegraph` broker. No nested OpenCode worker is launched.
5. Read everything back and run the live OpenCode handshake.
6. Explain recovery for the detected installation state.

It also prints the exact launcher, version store, manager, plugin, execution model, and workspace paths before approval. `--model` is required for preview and installation so native child sessions never fall through to an implicit OpenCode model. It is distinct from the model running the outer manager conversation.

## Interactive installation

```sh
vgxness setup opencode --model openai/gpt-5.6-sol
```

After displaying the full plan, the wizard asks:

```text
¿Aplicar exactamente este plan? [s/N]:
```

Anything except an explicit accepted answer leaves the filesystem unchanged. Automation may use `--yes`; the wizard still prints every step, limit, destination, and recovery rule before applying the plan.

## Status and controlled paths

```sh
vgxness setup opencode --status
vgxness setup opencode --preview --model openai/gpt-5.6-sol \
  --workspace /absolute/project \
  --bin-dir /absolute/bin \
  --data-dir /absolute/data \
  --config-dir /absolute/opencode-config
```

`--status` is read-only and reports the launcher state, active digest, integration state, configured execution model, bridge projection, and live handshake. Once installed, it infers the model from the exact managed projection, so `--status` does not require `--model`.

OpenCode Desktop should be fully quit and reopened after an installation or update. Test from a new Desktop session so its local server reloads the manager, native subagent profiles, and portable `vgxness.ts` plugin. See [OpenCode integration](opencode-integration.md#cli-and-desktop-runtime-compatibility) for the runtime contract and recoverable upgrade procedure.

## Direct native maintenance mode

Select `vgxness-native-manager` when VGXNESS itself needs development or repair and its normal control-plane policies prevent progress. This profile uses ordinary OpenCode workspace tools and native Task delegation rather than `vgxness_run`, tickets, claims, waves, or broker tools. It performs repository inspection itself, preserves unrelated changes, coordinates bounded `explore` and `general` subagents, edits directly, and owns final formatting, tests, static checks, and diff review.

This is an explicit maintenance escape hatch, not the default product path. It does not provide VGXNESS durable receipts or policy enforcement. It therefore blocks external paths, network tools, push, destructive Git cleanup, and unapproved commits, and it never asks the user to execute diagnostic commands on its behalf.

## Failure and recovery

- Preconditions or drift block the wizard before confirmation.
- Existing foreign or modified content is never overwritten.
- If a managed binary update succeeds but OpenCode integration fails before it is written, the wizard attempts one-level rollback to the previous verified version.
- A first binary installation has no previous version, so it is retained and reported instead of being deleted automatically.
- Once integration artifacts are written, verification failures retain them and print the exact status or recoverable-uninstall command. This avoids destructive cleanup against concurrently changed files.
