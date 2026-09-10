# Guided OpenCode, Codex and Pi setup

Structured SDD is retired: no lifecycle tools, SDD worker profiles, or active `sdd-lifecycle` skill are installed. Historical records remain in SQLite; `vgxness sdd-archive` permits only list/get and revision reads. Model-plan schemas retain inactive legacy slots for compatibility. Use a short plan, one writer, independent verification and proportional CARE review. See [the current workflow](orchestration-flow.md).

Setup installs OpenCode Manager62 with seven agents across 11 provider artifacts, plus the global catalog of 18 skills across 46 files. Restart the host after installation.


Current identities are OpenCode Manager62 and Codex Manager21 (parity OpenCode-v62), rendered from `internal/orchestration/manager_contract.json`. Complete Manager61 and Manager20 packages are the immediate compatibility predecessors. Older Manager59/18, CARE-v2 Manager58/17, CARE-v1 Manager58/16 and deeper packages remain lifecycle identities only.

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

`setup codex --status`, `setup pi --status` and `setup all --status` apply the same shared launcher and global-skills health requirements, then require every selected provider to be installed and healthy. If a provider fails after a possible mutation, setup reports its partial outcome and directs the user to `vgxness integrate <provider> status` before retrying; shared recovery guidance remains separate.

For Pi, provide an offline `--pi-release-dir` or a pinned `--pi-release-version <vSemVer>`; a release binary may use its own build tag. Applying Pi setup can download that exact portable package; preview and status never download it. Node and `pi` must already be present. Pi status checks its installed identity and isolated package probe; Windows worker execution remains unavailable. See [Pi setup](pi-typescript.md).

The wizard never edits `PATH`, silently initializes CodeGraph, overwrites foreign content, commits, pushes, or performs destructive Git cleanup. Setup installs the shared skills catalog explicitly as part of the selected operation. It updates `opencode.json` only through the semantic merge above, leaves existing `opencode.jsonc` bytes unchanged, and owns only the bounded `default-agent.json` restoration metadata.

Setup verifies exact bytes and static policy ordering only. It does not run a network delivery test or claim that OpenCode globs prove argv semantics, that `git` or `gh` will accept a command, or that credentials, hooks, GitHub, or branch protection permit delivery.

Install and uninstall rollback is conservative and never overwrites concurrent content. If durable rollback or restoration cannot complete, setup reports an explicit recovery failure and preserves available backups for inspection. An interrupted exact old/new model-plan switch can be resumed; unrelated drift must be repaired first. The shared pack classifies an exact desired/predecessor subset as partial: `install` resumes it and `uninstall` backs up and removes its exact present subset; unknown bytes remain drift. On Windows atomic rename/readback/backups are used, but directory fsync is unavailable and crash durability is therefore weaker.

Restart OpenCode Desktop after setup, an artifact upgrade, or any plan/slot change. Running sessions retain the previously loaded agent files, MCP configuration, and model bindings.

## CARE setup boundary

Setup installs the current CARE identities but does not establish an evaluation outcome. See [CARE architecture](care.md) and [CARE evaluation](care-evaluation.md). Historical runtime evidence was recorded on macOS; current-candidate native/model evidence must be recorded separately.


Current integration contract: OpenCode Manager62 and Codex Manager21 use one workspace writer and the same frozen candidate for verification and applicable CARE review. SQLite schema v23 is preserved. OpenCode installs 11 managed artifacts and Codex installs nine; each exposes six delegated profiles. The auto-discovered `plugins/vgxness-memory-lifecycle.ts` has no `opencode.json` plugin entry; missing it is partial. `vgxness mcp --full` exposes eight memory tools. Complete Manager61 and Manager20 packages are recognized predecessors. During retirement, modified, malformed, foreign, unknown, or newer bytes block without removal.
