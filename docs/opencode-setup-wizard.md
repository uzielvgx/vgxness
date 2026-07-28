# Guided OpenCode setup

The wizard explains and verifies the complete native OpenCode setup before changing anything:

1. Inspect the candidate binary, destinations, workspace, and OpenCode compatibility.
2. Install or update the permanent versioned launcher.
3. Install `vgxness-manager` and the five hidden read-only review profiles.
4. Validate the native skill-name and bounded `codegraph_explore` contracts.
5. Read back managed identities and perform the live OpenCode handshake.
6. Report recovery guidance if any step fails.

The wizard does not install a VGXNESS plugin, expose `vgxness_*` tools, create governed ticket-bound agents, choose a child execution model, install skills, or create a CodeGraph index.

## Commands

Preview without writes:

```sh
vgxness setup opencode --preview
```

Apply after interactive confirmation:

```sh
vgxness setup opencode
```

Apply non-interactively after the plan is printed:

```sh
vgxness setup opencode --yes
```

Inspect the complete setup:

```sh
vgxness setup opencode --status
```

Use `--workspace`, `--bin-dir`, `--data-dir`, or `--config-dir` to select explicit absolute destinations. `--model` is no longer required; it is accepted only as a temporary no-op compatibility flag.

## Readiness

Preview is ready to apply when OpenCode responds healthily and no managed destination is drifted. Status is healthy when:

- the permanent launcher identity is installed;
- the manager and all five reviewer identities are installed;
- the OpenCode adapter handshake succeeds in the workspace.

No plugin or bridge projection is required.

The wizard never edits `PATH`, downloads packages, silently initializes skills or CodeGraph, overwrites foreign content, commits, pushes, or performs destructive Git cleanup.

Restart OpenCode Desktop after setup so it reloads the native profiles.
