# Guided OpenCode, Codex and Pi setup

Current identities are OpenCode Manager61 and Codex Manager20 (parity OpenCode-v61), rendered from `internal/orchestration/manager_contract.json`. Complete Manager60 and Manager19 packages are the immediate compatibility predecessors. Older Manager59/18, CARE-v2 Manager58/17, CARE-v1 Manager58/16 and deeper packages remain lifecycle identities only.

The unified `setup opencode|codex|pi|all` flow explains and verifies selected providers before changing anything. OpenCode owns 17 managed artifacts, including one exact auto-discovered lifecycle plugin with no `opencode.json` plugin entry; Codex retains ownership of its `config.toml` and VGXNESS manages only its documented profiles. Shared setup work covers the launcher and separate global 47-file, 19-skill catalog, which adds `memory-sync` and `sdd-lifecycle`; the latter activates only after explicit SDD request/acceptance and fails closed when unavailable. The legacy provider skill is not an active artifact.

1. Inspect the candidate binary, destinations, workspace, and OpenCode compatibility.
2. Install or update the permanent versioned launcher.
3. Retire only exact legacy OpenCode plugin `vgxness.ts` v1-v10 bytes and provider skill `vgxness-autonomous-stacked-pr` v1/v2/v3 bytes before global publication; modified, malformed, foreign, unknown, or newer bytes block without removal.
4. Install `vgxness-manager`, managed `general` and verifier profiles, the read-only `explore` override, three hidden read-only CARE profiles, and six hidden SDD profiles.
5. Install `<config-dir>/vgxness/model-plan.json`, the auto-discovered `<config-dir>/plugins/vgxness-memory-lifecycle.ts` lifecycle plugin, the `opencode.json` default-agent selection and bounded `<config-dir>/vgxness/default-agent.json` restoration metadata, configure `vgxness mcp --full`, then publish the global 47-file, 19-skill catalog listed above. The plugin needs no `opencode.json` entry.
6. Read back all managed identities and perform the live OpenCode handshake.
7. Report recovery guidance if any step fails.

The resulting 17 OpenCode artifacts are 13 agents, the exact auto-discovered `plugins/vgxness-memory-lifecycle.ts` plugin, model-plan manifest, default-agent selection, and restoration metadata; the plugin has no `opencode.json` plugin entry. The agents include Manager61, managed `general` v10, `explore` v4, verifier v7, three CARE v2 roles/profiles, and six SDD roles/profiles including `vgxness-sdd-apply` v7. Current policy comes from the shared registry; exact complete Manager60 packages are immediate predecessors, followed by Manager59 and older CARE identities. Modified or mixed packages remain drift. `vgxness mcp --full` exposes eight memory and 13 SDD tools. Official setup publishes the global 47-file, 19-skill catalog including `memory-sync`; only exact historical `vgxness.ts` v1-v10 plugin, `vgxness-autonomous-stacked-pr` v1/v2/v3 provider-skill, and `stacked-pr` v3 bytes are removable, while modified, malformed, foreign, unknown, or newer bytes block without removal. OpenCode uninstall does not own global skills.

The shared Manager owns authorization, scope, candidate identity, lifecycle and final acceptance. It classifies direct questions, bounded reads, implementation and explicitly accepted SDD; selects applicable skills; delegates bounded independent work; and preserves one workspace writer. Missions bind exact targets, hashes, commands, criteria and skill resources. Significant work reports an outcome, approach and observable milestone. Independent verification and applicable reviews use the same frozen candidate. These are instructions and evidence contracts, not host enforcement or a live-model evaluation result. See [the shared Manager contract](architecture/shared-manager-contract.md).

## Commands

```sh
vgxness setup opencode --preview
vgxness setup opencode
vgxness setup opencode --yes
vgxness setup opencode --status
vgxness setup codex --preview --codex-home /absolute/path/to/home
vgxness setup all --preview --config-dir /absolute/path/to/opencode --codex-home /absolute/path/to/codex-home --pi-release-dir /absolute/path/to/pi-release
```

`vgxness setup opencode` supports `--config-dir` and the model flags. Use `--workspace`, `--bin-dir`, `--data-dir`, or `--config-dir` to select explicit OpenCode destinations. Use `--codex-home` for the independent Codex home; it is never inferred from or routed through `--config-dir`. Setup publishes portable skills to OpenCode's discoverable global root; use the lower-level `vgxness skills --skills-dir PATH` lifecycle only for isolated custom roots. Use `--model-plan low|medium|high|ultra` for a homogeneous preset, or set the efficient, balanced, and frontier provider/model slots independently. A mixed-provider setup must include all three `--model-efficient`, `--model-balanced`, and `--model-frontier` references plus all three `--model-efficient-effort`, `--model-balanced-effort`, and `--model-frontier-effort` values. For example:

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
- all 13 agent identities (manager, `general`, verifier, `explore`, three CARE profiles, and six SDD profiles) are installed with the resolved model and variant;
- MCP is configured as `vgxness mcp --full`, and the exact auto-discovered `plugins/vgxness-memory-lifecycle.ts` plugin is installed without an `opencode.json` plugin entry;
- the canonical non-secret model-plan manifest binds all model-aware agent digests;
- only exact `vgxness.ts` v1-v10 plugin, `vgxness-autonomous-stacked-pr` v1/v2/v3 provider-skill, and `stacked-pr` v3 retirement bytes are absent; modified, malformed, foreign, unknown, or newer bytes block without removal, and global `git-delivery` is installed without drift;
- the separate global 47-file, 19-skill `skills-creator`, `git-delivery`, `cross-platform`, `installer-lifecycle`, `agent-evaluation`, `ci-triage`, `security-boundary`, `documentation-strategy`, `product-requirements`, `software-architecture-docs`, `user-documentation`, `api-documentation`, `quality-test-documentation`, `operations-runbooks`, `governance-compliance-docs`, `release-lifecycle-docs`, `end-to-end-testing`, `memory-sync`, and `sdd-lifecycle` catalog is installed without drift; OpenCode uninstall does not own it;
- `opencode.json` semantically selects `vgxness-manager` as the default agent while preserving unrelated JSON values, existing `opencode.jsonc` bytes unchanged, and bounded `default-agent.json` restoration metadata recording whether the config existed and any prior explicit default;
- the bounded OpenCode handshake succeeds in the workspace.

`setup codex --status`, `setup pi --status` and `setup all --status` apply the same shared launcher and global-skills health requirements, then require every selected provider to be installed and healthy. If a provider fails after a possible mutation, setup reports its partial outcome and directs the user to `vgxness integrate <provider> status` before retrying; shared recovery guidance remains separate.

For Pi, provide an offline `--pi-release-dir` or a pinned `--pi-release-version <vSemVer>`; a release binary may use its own build tag. Applying Pi setup can download that exact portable package; preview and status never download it. Node and `pi` must already be present. Pi status checks its installed identity and isolated package probe; Windows worker execution remains unavailable. See [Pi setup](pi-typescript.md).

The wizard never edits `PATH`, silently initializes CodeGraph, overwrites foreign content, commits, pushes, or performs destructive Git cleanup. Setup installs the shared skills catalog explicitly as part of the selected operation. It updates `opencode.json` only through the semantic merge above, leaves existing `opencode.jsonc` bytes unchanged, and owns only the bounded `default-agent.json` restoration metadata.

Setup verifies exact bytes and static policy ordering only. It does not run a network delivery test or claim that OpenCode globs prove argv semantics, that `git` or `gh` will accept a command, or that credentials, hooks, GitHub, or branch protection permit delivery.

Install and uninstall rollback is conservative and never overwrites concurrent content. If durable rollback or restoration cannot complete, setup reports an explicit recovery failure and preserves available backups for inspection. An interrupted exact old/new model-plan switch can be resumed; unrelated drift must be repaired first. The shared pack classifies an exact desired/predecessor subset as partial: `install` resumes it and `uninstall` backs up and removes its exact present subset; unknown bytes remain drift. On Windows atomic rename/readback/backups are used, but directory fsync is unavailable and crash durability is therefore weaker.

Immediately after upgrading a binary, `--status` may fail when its database has an older schema because read-only status cannot migrate it. Run one write-capable memory or SDD operation to atomically apply the required migration, then rerun `--status`. After a forward migration, older binaries fail closed and cannot use the database. Never delete the database; see [Native memory](memory.md#upgrade-migration-caveat).

Restart OpenCode Desktop after setup, an artifact upgrade, or any plan/slot change. Running sessions retain the previously loaded agent files, MCP configuration, and model bindings.

## CARE setup boundary

Setup installs the current CARE identities but does not establish an evaluation outcome. See [CARE architecture](care.md) and [CARE evaluation](care-evaluation.md). Historical runtime evidence was recorded on macOS; current-candidate native/model evidence must be recorded separately.
