# Guided OpenCode setup

The OpenCode setup wizard explains the complete plan before it changes anything. It combines the versioned VGXNESS self-installer, the persistent OpenCode manager, three native subagent profiles, the managed plugin bridge, and a live provider handshake without moving policy into the CLI.

## Prerequisites

- Run the wizard from a VGXNESS candidate binary you trust.
- OpenCode must be installed, compatible, and available to the current process.
- The verification workspace must exist. The current directory is used by default.
- Choose an explicit OpenCode execution model in `provider/model` form. The example below uses `openai/gpt-5.6-sol`.
- OpenCode CLI may host tools with Bun while OpenCode Desktop hosts them with Node; the installed bridge supports both runtimes.

The wizard does not download software, edit `PATH`, overwrite foreign content, enable arbitrary shell execution, or silently remove files during recovery.

## Preview first

```sh
vgxness setup opencode --preview --model openai/gpt-5.6-sol
```

Preview performs read-only inspection and explains all six steps:

1. Check the candidate, explicit execution model, destinations, workspace, and OpenCode compatibility.
2. Install or update the immutable VGXNESS version behind the stable launcher.
3. Install `vgxness-manager` plus the hidden `vgxness-explorer`, `vgxness-implementer`, and `vgxness-reviewer` native subagents with separate fail-closed permissions.
4. Install the bounded `vgxness_status` and `vgxness_dispatch` plugin with that model fixed outside the agent-controlled arguments. Dispatch uses `native child session → prepare → brokered reads → complete`; it does not launch a nested OpenCode worker. Repository reviews still receive only pre-collected Git evidence.
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

## Failure and recovery

- Preconditions or drift block the wizard before confirmation.
- Existing foreign or modified content is never overwritten.
- If a managed binary update succeeds but OpenCode integration fails before it is written, the wizard attempts one-level rollback to the previous verified version.
- A first binary installation has no previous version, so it is retained and reported instead of being deleted automatically.
- Once integration artifacts are written, verification failures retain them and print the exact status or recoverable-uninstall command. This avoids destructive cleanup against concurrently changed files.
