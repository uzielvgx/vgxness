# Guided OpenCode setup

The wizard explains and verifies the complete OpenCode setup before changing anything. Current ownership is 18 OpenCode-managed artifacts plus the separate global 47-file, 19-skill catalog, which adds `memory-sync` and `sdd-lifecycle`; the latter activates only after explicit SDD request/acceptance and fails closed when unavailable. The legacy provider skill is not an active artifact.

1. Inspect the candidate binary, destinations, workspace, and OpenCode compatibility.
2. Install or update the permanent versioned launcher.
3. Retire only exact legacy OpenCode plugin `vgxness.ts` v1-v10 bytes and provider skill `vgxness-autonomous-stacked-pr` v1/v2/v3 bytes before global publication; modified, malformed, foreign, unknown, or newer bytes block without removal.
4. Install `vgxness-manager`, managed `general` and verifier profiles, the read-only `explore` override, five hidden read-only review profiles, and six hidden SDD profiles.
5. Install `<config-dir>/vgxness/model-plan.json`, the `opencode.json` default-agent selection and bounded `<config-dir>/vgxness/default-agent.json` restoration metadata, configure `vgxness mcp --full`, then publish the global 47-file, 19-skill catalog listed above. No plugin is installed.
6. Read back all managed identities and perform the live OpenCode handshake.
7. Report recovery guidance if any step fails.

The resulting 18 OpenCode artifacts are 15 agents, model-plan manifest, default-agent selection, and restoration metadata. The agents include manager v49; managed `general` v6, verifier v4, and five reviewers v3; `explore`; and six SDD profiles. Exact historical manager v48 and older supported identities can be upgraded, while modified or unknown bytes remain drift. `vgxness mcp --full` exposes five memory and 13 SDD tools. Official setup publishes the global 47-file, 19-skill catalog including `memory-sync`; only exact historical `vgxness.ts` v1-v10 plugin and `vgxness-autonomous-stacked-pr` v1/v2/v3 provider-skill bytes are removable, while modified, malformed, foreign, unknown, or newer bytes block without removal. OpenCode uninstall does not own global skills.

Manager v49 embeds a prompt-level adaptive contract, not a runtime broker: no-effect conversation, writing, translation, summarization, brainstorming, and planning take a zero-execution-tool fast path; bounded exact reads allow at most three tool attempts without delegation or todos; complex evidence research may use one read-only delegation. Failed attempts and retries count, and the manager must stop before exceeding the selected budget. Recall remains intent-triggered. Independently of the execution route, the prompt permits at most one autonomous memory save only for durable, evidence-backed, safely assessed project decisions, preferences, constraints, or learnings—never transient state, logs, secrets, or personal data—and does not require engineering ceremony or enable automatic cloud sync.

## Commands

```sh
vgxness setup opencode --preview
vgxness setup opencode
vgxness setup opencode --yes
vgxness setup opencode --status
```

Use `--workspace`, `--bin-dir`, `--data-dir`, or `--config-dir` to select explicit absolute destinations. Setup publishes portable skills to OpenCode's discoverable global root; use the lower-level `vgxness skills --skills-dir PATH` lifecycle only for isolated custom roots. Use `--model-plan low|medium|high|ultra` for a homogeneous preset, or set the efficient, balanced, and frontier provider/model slots independently. A mixed-provider setup must include all three `--model-efficient`, `--model-balanced`, and `--model-frontier` references plus all three `--model-efficient-effort`, `--model-balanced-effort`, and `--model-frontier-effort` values. For example:

```sh
vgxness setup opencode --yes \
  --model-efficient openai/gpt-5.6-luna --model-efficient-effort low \
  --model-balanced anthropic/claude-sonnet --model-balanced-effort high \
  --model-frontier acme/frontier --model-frontier-effort ultra
```

With no model override flags, planning can retain the installed configuration or default selection. Once any slot reference or effort override is supplied, the public setup command requires all three slot references; if those references use mixed providers, it also requires all three effort values. Mixed profiles are recorded in manifest v2; homogeneous presets remain v1. Fresh no-flag setup selects `medium` with `openai/gpt-5.6-luna`, `openai/gpt-5.6-terra`, and `openai/gpt-5.6-sol`. Setup validates configuration and managed identities but does not authenticate or probe runtime availability, so custom slots display availability as `unknown`. Restart OpenCode Desktop whenever installed artifacts, a plan, a slot, or an effort changes. `--model` is accepted only as a temporary no-op compatibility flag.

## Readiness

Preview is ready to apply when OpenCode responds healthily and no managed destination is drifted. Status is healthy when:

- the permanent launcher identity is installed;
- all 15 agent identities (manager, `general`, verifier, `explore`, five reviewers, and six SDD profiles) are installed with the resolved model and variant;
- MCP is configured as `vgxness mcp --full`; no plugin is installed;
- the canonical non-secret model-plan manifest binds all model-aware agent digests;
- only exact `vgxness.ts` v1-v10 plugin and `vgxness-autonomous-stacked-pr` v1/v2/v3 provider-skill retirement bytes are absent; modified, malformed, foreign, unknown, or newer bytes block without removal, and global `stacked-pr` is installed without drift;
- the separate global 47-file, 19-skill `skills-creator`, `stacked-pr`, `cross-platform`, `installer-lifecycle`, `agent-evaluation`, `ci-triage`, `security-boundary`, `documentation-strategy`, `product-requirements`, `software-architecture-docs`, `user-documentation`, `api-documentation`, `quality-test-documentation`, `operations-runbooks`, `governance-compliance-docs`, `release-lifecycle-docs`, `end-to-end-testing`, `memory-sync`, and `sdd-lifecycle` catalog is installed without drift; OpenCode uninstall does not own it;
- `opencode.json` semantically selects `vgxness-manager` as the default agent while preserving unrelated JSON values, existing `opencode.jsonc` bytes unchanged, and bounded `default-agent.json` restoration metadata recording whether the config existed and any prior explicit default;
- the bounded OpenCode handshake succeeds in the workspace.

The wizard never edits `PATH`, downloads packages, silently initializes skills or CodeGraph, overwrites foreign content, commits, pushes, or performs destructive Git cleanup. It updates `opencode.json` only through the semantic merge above, leaves existing `opencode.jsonc` bytes unchanged, and owns only the bounded `default-agent.json` restoration metadata.

Setup verifies exact bytes and static policy ordering only. It does not run a network delivery test or claim that OpenCode globs prove argv semantics, that `git` or `gh` will accept a command, or that credentials, hooks, GitHub, or branch protection permit delivery.

Install and uninstall rollback is conservative and never overwrites concurrent content. If durable rollback or restoration cannot complete, setup reports an explicit recovery failure and preserves available backups for inspection. An interrupted exact old/new model-plan switch can be resumed; unrelated drift must be repaired first. The shared pack classifies an exact desired/predecessor subset as partial: `install` resumes it and `uninstall` backs up and removes its exact present subset; unknown bytes remain drift. On Windows atomic rename/readback/backups are used, but directory fsync is unavailable and crash durability is therefore weaker.

Immediately after upgrading a binary, `--status` may fail when its database has an older schema because read-only status cannot migrate it. Run one write-capable memory or SDD operation to atomically apply the required migration, then rerun `--status`. After a forward migration, older binaries fail closed and cannot use the database. Never delete the database; see [Native memory](memory.md#upgrade-migration-caveat).

Restart OpenCode Desktop after setup, an artifact upgrade, or any plan/slot change. Running sessions retain the previously loaded agent files, MCP configuration, and model bindings.
