# Guided OpenCode setup

The wizard explains and verifies the complete OpenCode setup before changing anything:

1. Inspect the candidate binary, destinations, workspace, and OpenCode compatibility.
2. Install or update the permanent versioned launcher.
3. Install `vgxness-manager` and the five hidden read-only review profiles.
4. Install the bounded VGXNESS-owned memory plugin.
5. Read back all managed identities and perform the live OpenCode handshake.
6. Report recovery guidance if any step fails.

The memory plugin exposes only search, get, save, and explicit forget operations. The wizard does not install governed agents, orchestration tools, ticket brokers, a child execution model, skills, CodeGraph indexes, or Engram.

## Commands

```sh
vgxness setup opencode --preview
vgxness setup opencode
vgxness setup opencode --yes
vgxness setup opencode --status
```

Use `--workspace`, `--bin-dir`, `--data-dir`, or `--config-dir` to select explicit absolute destinations. `--model` is no longer required and is accepted only as a temporary no-op compatibility flag.

## Readiness

Preview is ready to apply when OpenCode responds healthily and no managed destination is drifted. Status is healthy when:

- the permanent launcher identity is installed;
- the manager and all five reviewer identities are installed;
- the exact memory-only plugin is installed;
- the OpenCode adapter handshake succeeds in the workspace.

The wizard never edits `PATH`, downloads packages, silently initializes skills or CodeGraph, overwrites foreign content, commits, pushes, or performs destructive Git cleanup.

Restart OpenCode Desktop after setup.
