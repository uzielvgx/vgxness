# Guided OpenCode, Codex and Pi setup

Structured SDD is retired: no lifecycle tools, SDD worker profiles, or active `sdd-lifecycle` skill are installed. Historical records remain inert in SQLite for data preservation; there is no SDD runtime or archive command. Model-plan schemas retain inactive legacy slots for compatibility. Use a short plan, one writer, independent verification and proportional CARE review. See [the current workflow](orchestration-flow.md).

Setup installs OpenCode Manager62 with seven agents across 12 provider artifacts, plus the global catalog of 18 skills across 46 files. Restart the host after installation.


Current identities are OpenCode Manager62 and Codex Manager21 (parity OpenCode-v62), rendered from `internal/orchestration/manager_contract.json`. Each agent has one current definition. Installed file hashes are tracked in a local installation receipt; older unreceipted agents require the bridge procedure in `docs/agent-template-migration.md`.

## Commands

```sh
vgxness setup opencode --preview
vgxness setup opencode
vgxness setup opencode --yes
vgxness setup opencode --status
vgxness setup codex --preview --codex-home /absolute/path/to/home
vgxness setup all --preview --config-dir /absolute/path/to/opencode --codex-home /absolute/path/to/codex-home --pi-release-dir /absolute/path/to/pi-release
```

`vgxness setup opencode` supports `--config-dir` and the model flags. Use `--workspace`, `--bin-dir`, `--data-dir`, or `--config-dir` to select explicit OpenCode destinations. Use `--codex-home` for the independent Codex home; it is never inferred from or routed through `--config-dir`. Setup publishes portable skills to OpenCode's discoverable global root; use the lower-level `vgxness skills --skills-dir PATH` lifecycle only for isolated custom roots. OpenCode and Pi use explicit single-model or per-agent choices; only Codex accepts `--model-plan low|medium|high|ultra`.

```sh
vgxness setup opencode --preview --model-mode single --model provider/model
```

Replace `provider/model` with a configured model, review the preview, then repeat
without `--preview` to apply. For seven independent assignments, use
`--model-mode per-agent` and repeat `--agent-model ROLE=provider/model` for every
role. See [model selection](model-selection.md) for the full example and TUI.
With no new selection, installed choices remain. A fresh no-flag OpenCode setup
uses `openai/gpt-5.6-terra` for all seven roles with provider-default effort.
Existing v1/v2/v3 manifests remain readable. Setup does not authenticate models;
custom identifiers remain availability `unknown`. Legacy slot flags are rejected.

## Readiness

Preview is ready to apply when OpenCode responds healthily and no managed destination is drifted. Status is healthy when:

`setup codex --status`, `setup pi --status` and `setup all --status` apply the same shared launcher and global-skills health requirements, then require every selected provider to be installed and healthy. If a provider fails after a possible mutation, setup reports its partial outcome and directs the user to `vgxness integrate <provider> status` before retrying; shared recovery guidance remains separate.

For Pi, provide an offline `--pi-release-dir` or a pinned `--pi-release-version <vSemVer>`; a release binary may use its own build tag. Applying Pi setup can download that exact portable package; preview and status never download it. Node and `pi` must already be present. Pi status checks its installed identity and isolated package probe; Windows worker execution remains unavailable. See [Pi setup](pi-typescript.md).

The wizard never edits `PATH`, silently initializes CodeGraph, overwrites foreign content, commits, pushes, or performs destructive Git cleanup. Setup installs the shared skills catalog explicitly as part of the selected operation. It updates `opencode.json` only through the semantic merge above, leaves existing `opencode.jsonc` bytes unchanged, and owns only the bounded `default-agent.json` restoration metadata.

Setup verifies exact bytes and static policy ordering only. It does not run a network delivery test or claim that OpenCode globs prove argv semantics, that `git` or `gh` will accept a command, or that credentials, hooks, GitHub, or branch protection permit delivery.

Install and uninstall rollback is conservative and never overwrites concurrent content. If durable rollback or restoration cannot complete, setup reports an explicit recovery failure and preserves available backups for inspection. An interrupted exact old/new model-plan switch can be resumed; unrelated drift must be repaired first. The shared pack classifies an exact desired/predecessor subset as partial: `install` resumes it and `uninstall` backs up and removes its exact present subset; unknown bytes remain drift. On Windows atomic rename/readback/backups are used, but directory fsync is unavailable and crash durability is therefore weaker.

Restart OpenCode Desktop after setup, an artifact upgrade, or any model selection change. Running sessions retain the previously loaded agent files, MCP configuration, and model bindings.

## CARE setup boundary

Setup installs the current CARE identities but does not establish an evaluation outcome. See [CARE architecture](care.md) and [CARE evaluation](care-evaluation.md). Historical runtime evidence was recorded on macOS; current-candidate native/model evidence must be recorded separately.


Current integration contract: OpenCode Manager62 and Codex Manager21 use one workspace writer and the same frozen candidate for verification and applicable CARE review. SQLite schema v23 is preserved. OpenCode installs 12 managed artifacts and Codex installs ten; each exposes six delegated profiles. The auto-discovered `plugins/vgxness-memory-lifecycle.ts` has no `opencode.json` plugin entry; missing it is partial. `vgxness mcp --full` exposes eight memory tools. Receipt-backed prior installations are recognized by exact local bytes. During retirement, modified, malformed, foreign, unknown, or newer bytes block without removal.
