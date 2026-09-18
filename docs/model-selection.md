# Choose models during installation

This guide is for users installing or configuring the current VGXNESS integrations.
OpenCode and Pi offer two modes: **one model for all agents** or **one model per
agent**. Codex alone keeps the `low`, `medium`, `high`, and `ultra` plans.

Have the target application installed and its providers configured first. Model
references use `provider/model`; providers can differ between agents. Enter the
identifier used by that application: OpenCode and Pi may use different provider
names for the same account. Setup scans local model data; it does not
authenticate, invoke, or probe models.

## In the installation screen

1. Run `vgxness tui`, choose Install or Configure, then select providers.
2. On the Models screen, use Tab to move between providers. OpenCode and Pi are
   scanned automatically from their local model data: OpenCode reads its local
   model list and Pi reads its local model store. The scan proves that an
   identifier is configured locally, not that it is authorized or supported.
3. For OpenCode or Pi, choose `1` for a single model or `2` for per-agent models.
   Use the arrow keys to select an agent. Press `m` to open the scanned catalog,
   type to filter, and press Enter to assign the highlighted entry. Press `i`
   to type a provider/model reference manually instead. Press `r` to rescan the
   provider explicitly.
4. Press `e` to cycle the efforts that the selected model reports, starting from
   the provider default. OpenCode offers the variants its local model list
   reports, including exact tokens such as `max`; Pi offers the efforts its
   model store accepts.
5. For Codex, use the arrow keys to select its plan.
6. Press Enter to preview, then Enter again to review the exact assignments.
   Apply only after checking the review. Reopen the affected applications
   afterward.

Unedited installed selections are preserved. Changing to per-agent mode requires
all seven assignments: `manager`, `explore`, `general`, `verifier`,
`care-reviewer`, `care-specialist`, and `care-challenger`.

## From the command line

Single model for OpenCode:

```sh
vgxness setup opencode --preview --model-mode single --model provider/model
vgxness setup opencode --model-mode single --model provider/model
```

Per-agent selection:

```sh
vgxness setup opencode --preview --model-mode per-agent \
  --agent-model manager=provider/main \
  --agent-model explore=provider/fast \
  --agent-model general=provider/code \
  --agent-model verifier=provider/check \
  --agent-model care-reviewer=provider/review \
  --agent-model care-specialist=other/specialist \
  --agent-model care-challenger=other/challenger
```

Replace the example references with real configured models. Repeat the command
without `--preview` to apply. Pi accepts the same model flags with `setup pi`;
include the pinned release version or offline release directory described in
[Pi setup](pi-typescript.md). With `setup all`, the unprefixed model flags target
OpenCode; use `--pi-model-mode`, `--pi-model`, and repeated `--pi-agent-model`
for Pi. `--model-plan` targets Codex only.

`--model-effort` (or `--pi-model-effort`) optionally applies an effort to the
selection. The default `off` disables Pi thinking; for OpenCode it leaves the
variant unspecified, using the provider default. Other values are `minimal`,
`low`, `medium`, `high`, and `xhigh`. The screen offers the variants each
scanned OpenCode model reports and the efforts each scanned Pi model accepts.
A syntactically valid value does not establish provider support.

The old OpenCode slot flags and plan selection are rejected by the unified
installer. Existing model manifests remain readable so an update can preserve
the choices already installed.

## Readback and recovery

Use `vgxness setup opencode --status`, `vgxness setup pi --status`, or
`vgxness integrate codex status` for installation readback. Pi stores its choices
in its settings alongside the package entry; it needs no running VGXNESS process.
Its Manager activates the configured model at session start and its workers
reject model overrides. An installation without explicit Pi choices uses the
currently selected Pi model for all workers, with thinking off.

Incomplete selections fail before installation. If settings change after preview,
Pi refuses the stale update; obtain a fresh preview before retrying. If a model
is unavailable at runtime, configure a supported model and reopen the application.
Do not interpret package health as model authentication or successful delegation.

Maintenance: installer/provider maintainers should update this guide when the
model flags, Models screen, agent inventory, or Pi settings contract changes.
