# Guided OpenCode setup

The wizard explains and verifies the complete OpenCode setup before changing anything:

1. Inspect the candidate binary, destinations, workspace, and OpenCode compatibility.
2. Install or update the permanent versioned launcher.
3. Install or verify the shared global 15-file `skills-creator` pack.
4. Install `vgxness-manager`, managed `general` and verifier profiles, the read-only `explore` override, five hidden read-only review profiles, and six hidden SDD profiles.
5. Install the bounded VGXNESS-owned storage plugin, `<config-dir>/vgxness/model-plan.json`, the `opencode.json` default-agent selection and bounded `<config-dir>/vgxness/default-agent.json` restoration metadata, and the independent autonomous stacked-PR skill.
6. Read back all managed identities and perform the live OpenCode handshake.
7. Report recovery guidance if any step fails.

The resulting 20 managed artifacts are 15 agents, storage plugin v5, the model-plan manifest, one default-agent selection, one restoration record, and one skill. The agents are manager v37, managed `general` and verifier profiles, one CodeGraph-first read-only `explore` override, five read-only reviewers, and six read-only SDD profiles. The v3 skill's exact v1/v2 predecessors alone are upgradeable. Manager v37 gates source writes on a clean checkout, identity, intended paths, sizing/slice plan, and fresh branch; only explicit reauthorization can recover a bounded unpublished local slice, never a remote branch or PR. The selection uses a semantic merge to set `default_agent="vgxness-manager"` in `opencode.json` while preserving unrelated JSON values; existing `opencode.jsonc` bytes remain unchanged. Bounded `<config-dir>/vgxness/default-agent.json` metadata records whether `opencode.json` existed and any prior explicit default, so uninstall can restore that default or remove a config created by setup. Manager, `general`, and verifier use global `allow` permissions while retaining orchestration, delegated implementation, and non-mutating verification roles. The plugin exposes exactly 18 tools: five semantic-memory operations and 13 structured SDD storage/projection operations. SDD mutations fail closed outside a tracked top-level manager session. The plugin never reads or writes OpenSpec files, invokes agents, routes work, edits, delegates, or runs lifecycle orchestration. The wizard installs only the named skill, not a child execution model, CodeGraph index, or Engram.

## Commands

```sh
vgxness setup opencode --preview
vgxness setup opencode
vgxness setup opencode --yes
vgxness setup opencode --status
```

Use `--workspace`, `--bin-dir`, `--data-dir`, `--config-dir`, or `--skills-dir` to select explicit absolute destinations. Use `--model-plan low|medium|high` and the optional `--model-efficient`, `--model-balanced`, and `--model-frontier` exact provider/model slots to configure the installed profiles. These flags overlay the verified installed manifest; omitted values remain unchanged. Fresh no-flag setup selects `medium` with `openai/gpt-5.6-luna-fast`, `openai/gpt-5.6-terra`, and `openai/gpt-5.6-sol`. Setup validates configuration and managed identities; it does not claim to probe runtime model availability. `--model` is accepted only as a temporary no-op compatibility flag.

## Readiness

Preview is ready to apply when OpenCode responds healthily and no managed destination is drifted. Status is healthy when:

- the permanent launcher identity is installed;
- all 15 agent identities (manager, `general`, verifier, `explore`, five reviewers, and six SDD profiles) are installed with the resolved model and variant;
- the exact storage-only plugin is installed;
- the canonical non-secret model-plan manifest binds all model-aware agent digests;
- the exact independent stacked-PR skill is installed outside the model plan;
- the separate global 15-file `skills-creator` pack is installed without drift; OpenCode uninstall does not own it;
- `opencode.json` semantically selects `vgxness-manager` as the default agent while preserving unrelated JSON values, existing `opencode.jsonc` bytes unchanged, and bounded `default-agent.json` restoration metadata recording whether the config existed and any prior explicit default;
- the bounded OpenCode handshake succeeds in the workspace.

The wizard never edits `PATH`, downloads packages, silently initializes skills or CodeGraph, overwrites foreign content, commits, pushes, or performs destructive Git cleanup. It updates `opencode.json` only through the semantic merge above, leaves existing `opencode.jsonc` bytes unchanged, and owns only the bounded `default-agent.json` restoration metadata.

Setup verifies exact bytes and static policy ordering only. It does not run a network delivery test or claim that OpenCode globs prove argv semantics, that `git` or `gh` will accept a command, or that credentials, hooks, GitHub, or branch protection permit delivery.

Install and uninstall rollback is conservative and never overwrites concurrent content. If durable rollback or restoration cannot complete, setup reports an explicit recovery failure and preserves available backups for inspection. An interrupted exact old/new model-plan switch can be resumed; unrelated drift must be repaired first. The shared pack classifies an exact desired/predecessor subset as partial: `install` resumes it and `uninstall` backs up and removes its exact present subset; unknown bytes remain drift. On Windows atomic rename/readback/backups are used, but directory fsync is unavailable and crash durability is therefore weaker.

Immediately after upgrading a binary whose database is still schema v4, `--status` may fail because read-only opens cannot migrate. Run one write-capable memory or SDD operation to atomically apply schema v5, then rerun `--status`. Never delete the database; see [Native memory](memory.md#upgrade-migration-caveat).

Restart OpenCode Desktop after setup, an artifact upgrade, or any plan/slot change. Running sessions retain the previously loaded agent files, plugin, and model bindings.
