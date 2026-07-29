# Guided OpenCode setup

The wizard explains and verifies the complete OpenCode setup before changing anything:

1. Inspect the candidate binary, destinations, workspace, and OpenCode compatibility.
2. Install or update the permanent versioned launcher.
3. Install `vgxness-manager`, five hidden read-only review profiles, and six hidden SDD profiles.
4. Install the bounded VGXNESS-owned storage plugin and `<config-dir>/vgxness/model-plan.json`.
5. Read back all managed identities and perform the live OpenCode handshake.
6. Report recovery guidance if any step fails.

The resulting 14 managed artifacts are 12 agents, storage plugin v5, and the model-plan manifest. The agents are manager v30, five read-only reviewers, and six read-only SDD profiles. The plugin exposes exactly 18 tools: five semantic-memory operations and 13 structured SDD storage/projection operations. SDD mutations fail closed outside a tracked top-level manager session. The plugin never reads or writes OpenSpec files, invokes agents, routes work, edits, delegates, or runs lifecycle orchestration. The installed manager is the sole workspace and lifecycle writer; the wizard does not install a child execution model, skills, CodeGraph indexes, or Engram.

## Commands

```sh
vgxness setup opencode --preview
vgxness setup opencode
vgxness setup opencode --yes
vgxness setup opencode --status
```

Use `--workspace`, `--bin-dir`, `--data-dir`, or `--config-dir` to select explicit absolute destinations. Use `--model-plan low|medium|high` and the optional `--model-efficient`, `--model-balanced`, and `--model-frontier` exact provider/model slots to configure the installed profiles. These flags overlay the verified installed manifest; omitted values remain unchanged. Fresh no-flag setup selects `medium` with `openai/gpt-5.6-luna-fast`, `openai/gpt-5.6-terra`, and `openai/gpt-5.6-sol`. Setup validates configuration and managed identities; it does not claim to probe runtime model availability. `--model` is accepted only as a temporary no-op compatibility flag.

## Readiness

Preview is ready to apply when OpenCode responds healthily and no managed destination is drifted. Status is healthy when:

- the permanent launcher identity is installed;
- the manager, all five reviewer identities, and all six SDD identities are installed with the resolved model and variant;
- the exact storage-only plugin is installed;
- the canonical non-secret model-plan manifest binds all model-aware agent digests;
- the bounded OpenCode handshake succeeds in the workspace.

The wizard never edits `PATH` or `opencode.json`, downloads packages, silently initializes skills or CodeGraph, overwrites foreign content, commits, pushes, or performs destructive Git cleanup.

Install and uninstall rollback is conservative and never overwrites concurrent content. If durable rollback or restoration cannot complete, setup reports an explicit recovery failure and preserves available backups for inspection. An interrupted exact old/new model-plan switch can be resumed; unrelated drift must be repaired first.

Immediately after upgrading a binary whose database is still schema v4, `--status` may fail because read-only opens cannot migrate. Run one write-capable memory or SDD operation to atomically apply schema v5, then rerun `--status`. Never delete the database; see [Native memory](memory.md#upgrade-migration-caveat).

Restart OpenCode Desktop after setup, an artifact upgrade, or any plan/slot change. Running sessions retain the previously loaded agent files, plugin, and model bindings.
