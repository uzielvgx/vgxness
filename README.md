# VGXNESS

VGXNESS sets up a coordinated team of AI coding agents in **OpenCode**, **Codex** and **Pi**: one Manager that owns scope and decisions, plus six specialist roles (explore, general, verifier and three CARE reviewers). All three hosts share one Manager policy and one durable, project-scoped memory stored locally in SQLite. Setup is previewed before anything changes, installs are recoverable, and the `status` and `doctor` commands tell you exactly what is installed and healthy.

## Install

**macOS or Linux** (Homebrew):

```sh
brew install uzielvgx/tap/vgxness
```

**Windows** (Scoop):

```powershell
scoop bucket add vgxness https://github.com/uzielvgx/scoop-bucket
scoop install vgxness/vgxness
```

Homebrew and Scoop verify the pinned SHA-256 of what they download and own the executable; neither modifies OpenCode, Codex or Pi on its own. Releases also ship unsigned archives for Linux, macOS and Windows plus `SHA256SUMS`: verify the archive before running the extracted binary. `vgxness self preview` and `vgxness self install` optionally place a permanent launcher in `~/.local/bin` with versioned, rollback-capable activation; self-installation does not download releases or edit `PATH` (see [Versioned self-installation](docs/self-install.md)). See [Releases and installation](docs/release.md) for artifact names, checksums and platform support, and the [tap](https://github.com/uzielvgx/homebrew-tap) and [bucket](https://github.com/uzielvgx/scoop-bucket) documentation for update and uninstall boundaries.

## Quickstart

```sh
# 1. See exactly what would change for all three hosts. Nothing is written.
vgxness setup all --preview

# 2. Apply it. You confirm the reviewed plan before anything is written.
vgxness setup all

# 3. Check storage, the shared installation and every provider.
vgxness doctor --all
```

Use `setup opencode`, `setup codex` or `setup pi` to configure a single host, and `setup <host> --status` to inspect it later. Prefer a guided screen? Run `vgxness tui`, which walks through host selection and model choice (below). Restart OpenCode or Codex after setup so they reload their agents. Setup never overwrites settings or files it does not manage; drift is reported with repair guidance instead.

## Choose models in Setup

Run `vgxness tui` and select the coding hosts to configure. After confirming the
hosts, each OpenCode and Pi screen first asks whether you want one model for all
seven agents or one model per agent, then shows the configuration. Select an
agent with the arrow keys, then press `m` to open the model picker: choose a
provider, then a model (type to filter, arrow keys to move, Enter to assign,
Escape to go back or close). Press `i` to type a reference manually. Press `e` to
cycle the efforts the scanned model reports. Codex alone uses the low, medium,
high and ultra plans.

Press Enter to preview, then review the exact assignments before applying. Existing
choices are preserved until edited. A model identifier does not prove account
access or runtime availability. See [model selection](docs/model-selection.md)
for CLI examples and the complete screen flow. If setup reports drift or recovery,
follow its status guidance; unknown settings and files are not overwritten.

## Documentation

| Document | Responsibility |
| --- | --- |
| [Product Blueprint — English](docs/product-blueprint.md) | Canonical source-linked capability inventory, current status, boundaries, limitations, and evidence gaps. |
| [Plan maestro de producto — Español](docs/product-blueprint.es.md) | Spanish companion to the capability inventory; non-canonical, with English controlling conflicts. |
| [Go Implementation Architecture](docs/go-implementation.md) | Current Go packages, storage, setup, integration, and testing boundaries. |
| [Orchestration Flow](docs/orchestration-flow.md) | Native Manager workflow, proportional CARE review, and delivery labels. |
| [Implementation plans](docs/implementations/README.md) | Durable in-repository Markdown plan tracking and native session projections. |
| [Native Memory and Structured Storage](docs/memory.md) | SQLite schema v23 domains, isolation, memory lifecycle, and upgrade migration caveat. |
| [Synchronization service boundary](docs/sync.md) | Loopback-only daemon operation, HTTPS termination boundary, and runtime configuration. |
| [Versioned Self-installation](docs/self-install.md) | Permanent launcher, immutable SHA-256 versions, atomic activation, rollback, and safety behavior. |
| [Releases and installation](docs/release.md) | Release artifacts, support matrix, checksum verification, installation, and release rollback boundaries. |
| [Guided OpenCode Setup](docs/opencode-setup-wizard.md) | Unified `setup opencode|codex|all` entrypoint, confirmation boundary, verification, status, and recovery behavior. |
| [OpenCode Integration](docs/opencode-integration.md) | Persistent manager installation, managed identities, storage tools, and health. |
| [Codex Integration](docs/codex-integration.md) | Standalone Codex agent lifecycle and user-owned `config.toml` contract. |
| [Portable Pi](docs/pi-typescript.md) | Pi packaging, prerequisites, setup and health checks. |
| [System diagnostics](docs/diagnostics.md) | `status` and `doctor` results and exit codes. |
| [Safe Hooks](docs/hooks.md) | No installed hook surface; historical plugin retirement context. |
| [Legacy Compatibility Matrix](docs/legacy-compatibility.md) | Evidence-bound legacy formats, migrations, and retirement boundaries. |
| [Local agent-evaluation runner](docs/agent-evaluation-runner.md) | Opt-in development trace transport with offline regression coverage; it does not call models in CI or grade behavior automatically. |
| [Evaluation results](docs/agent-evaluation-results.md) | Sanitized, bounded results from provider, PostgreSQL, and two-client development evaluations. |

## Hosts in detail

### OpenCode and Codex

OpenCode Manager62 installs 12 managed artifacts: seven agents (Manager, Explore, General, Verifier and three CARE roles), the memory lifecycle plugin, model manifest, two configuration artifacts and an installation receipt. Codex Manager21 installs ten artifacts: AGENTS.md, six delegated profiles, two plugin lifecycle manifests and an installation receipt. Both share the single current policy in `internal/orchestration/manager_contract.json`; their native adapters supply tools, model configuration and transport. Local installation receipts support future updates without retaining old agent templates in the source tree. Installation does not prove model behavior or runtime authentication.

The auto-discovered `plugins/vgxness-memory-lifecycle.ts` plugin has no `opencode.json` plugin entry. `vgxness mcp --full` exposes eight memory tools and no SDD tools. MCP has no caller identity; host permissions and scoped user authorization remain authoritative.

### Pi

Pi runs entirely in TypeScript/Node and uses the shared SQLite memory database directly; it runs independently of the VGXNESS CLI, MCP and Go. VGXNESS provisions it from a pinned portable release or an explicit offline release directory, and `setup all` includes OpenCode, Codex and Pi. The provisioned package is discovered through Pi's normal `settings.json` package list; no `pi -e` path or loader override is needed. It runs in Node 22.19.0 or newer, with Pi `^0.84.4` and `typebox` 1.3.7 supplied by the host, and contains no Go sidecar, native addon, or runtime compiler/install script. Its 23 SQL migrations, prompts, and fallback skills travel in the same tarball. This package intentionally has no Node `main` or `exports` entrypoint.

The extension activates an explicitly configured Manager model, or uses the host's selected model when no selection is configured. Worker execution uses the native Pi transport on supported hosts. Worker processes are unavailable on Windows because owned process-tree reaping is not implemented there. Local Linux arm64 validation covers Node 22.19.0 and Node 24; native macOS and Windows support claims require observed target-native validation. See [portable Pi packaging and health checks](docs/pi-typescript.md) and the [v1 readiness audit](docs/v1-readiness.md).

To provision a local, unpublished Pi build, run `vgxness-release pi --output /absolute/new-directory`. The output directory must be new and outside this repository; it contains one `vgxness-pi-0.1.0.tgz` TypeScript tarball, `SHA256SUMS`, and `PROVENANCE.json`, and the command neither installs nor publishes anything. Then run `vgxness setup pi --preview --pi-release-dir /absolute/new-directory`, review the preview, and rerun without `--preview`. `--pi-agent-dir` selects Pi's settings root (otherwise `PI_CODING_AGENT_DIR`, then `~/.pi/agent`) and `--pi-root` selects VGXNESS's separate managed package root. Setup validates the complete portable release before preserving existing Pi settings and adding its absolute package path. `vgxness setup pi --status` inspects that installed state and does not require the original release directory.

## Global portable skills

`vgxness skills <preview|install|status|uninstall> [--skills-dir PATH]` manages the portable 46-file, 18-skill `skills-creator`, `git-delivery`, `cross-platform`, `installer-lifecycle`, `agent-evaluation`, `ci-triage`, `security-boundary`, `documentation-strategy`, `product-requirements`, `software-architecture-docs`, `user-documentation`, `api-documentation`, `quality-test-documentation`, `operations-runbooks`, `governance-compliance-docs`, `release-lifecycle-docs`, `end-to-end-testing`, `memory-sync` catalog in `~/.agents/skills` by default (or an isolated absolute destination). Setup retires only exact `vgxness.ts` v1-v10 plugin bytes, provider-owned `vgxness-autonomous-stacked-pr` v1/v2/v3 bytes, and declared `stacked-pr` v3 bytes before publishing `git-delivery`; canonical `git-delivery` bytes at the legacy path and modified, malformed, foreign, unknown, or newer bytes block without removal. Portable skills are shared across hosts, and OpenCode uninstall never removes this global catalog.

The skills transaction anchors mutations in the selected root. An interrupted exact partial pack resumes with `install` or is safely backed up and removed with `uninstall`; unknown bytes remain drift. Windows uses atomic rename, readback, and backups, but cannot fsync directories, so its crash-durability guarantee is weaker.

## Memory and synchronization

VGXNESS provides durable project memory in SQLite/FTS5 schema v23. By default, every workspace uses the project-isolated semantic memory in `~/.vgxness/memory.db`; canonical workspace identity keeps same-named projects distinct. Older project-level databases are retained and are not imported automatically; explicit `--storage-root` and `--project-local` modes remain isolated overrides.

Memory recall is limited to relevant prior context: search before exact-ID reads and use recent recall only for explicit recent-work or recovery requests. Durable, evidence-backed knowledge is assessed under a stable topic; secrets, raw transcripts, logs and transient status are excluded. There is no automatic cloud synchronization. The OpenCode lifecycle plugin can inject a bounded prior completed same-project handoff as untrusted data and does not capture transcripts.

For a new cloud, reset it first and run `vgxness memory sync reseed --workspace /absolute/workspace --confirm-cloud-empty` on the Mac/source device. Each later Linux or Windows device uses `vgxness memory sync rejoin --workspace /absolute/workspace --confirm-merge`. These operations are per-project, never run `git pull`, require their exact confirmation, and resume safely on retry; see [Synchronization service boundary](docs/sync.md).

## How the Manager works

The shared Manager owns authorization, scope, lifecycle decisions and final acceptance. It distinguishes direct questions, bounded reads, implementation and delivery, delegates bounded independent work, and keeps one workspace writer. Significant work reports the outcome, approach and next observable milestone. Substantial work keeps a durable Markdown plan under `docs/implementations/`; the plan file is canonical and a native session task view is a projection only. Skills are loaded by applicability and never grant authority. Independent verification and review inspect the same frozen candidate. See [the shared contract](docs/architecture/shared-manager-contract.md) and [Orchestration Flow](docs/orchestration-flow.md), which also defines the observed delivery labels.

## Status, diagnostics and safety

The read-only `status` and `doctor` commands report storage root, database, and schema health. Use `vgxness doctor --all` to inspect storage, shared installation, and all three providers together; runtime/model evidence that is not observed is reported explicitly. See [system diagnostics](docs/diagnostics.md) for results and exit codes. Compatibility execution commands and subsystems are not part of the product.

Exact legacy plugin and provider-skill bytes are retirement identities; modified, foreign, mixed or unknown packages remain protected. Structured SDD is retired: it has no runtime, CLI archive, MCP or Pi tool, worker binding, or bundled skill, and historical database records and immutable migration files remain for data preservation. Model configuration compatibility is retained; historical model-plan schemas may contain inactive SDD slots, which do not install or authorize workers.

**Non-goal:** VGXNESS will not copy third-party code, prompts, schemas, names, skills, or exact workflows, and it will not permit silent destructive autonomy or hide operational truth behind a CLI. For complete definitions and status classifications, read the [Product Blueprint](docs/product-blueprint.md).

## Development

The provisioning CLI uses Go 1.26. Run `make fast` during iteration. It checks formatting and runs `go test -short ./...`; short mode omits only filesystem-heavy installation and durability lifecycles, not unit, security, drift, parsing, authorization, or repository contract tests.

Run `make verify` before submitting or tagging. It exposes the standard Go commands directly: ordinary coverage with a fresh test run, race, vet, formatting, non-mutating tidy diff, whitespace, module verification, build, focused Linux E2E, and Windows cross-build/test compilation. The Windows test commands compile and link packages behind `/usr/bin/true`; they do not execute Windows binaries. Native Windows installer and self-install tests remain required CI evidence. The network-dependent vulnerability scan is intentionally excluded from `make verify`; run `make vuln` separately when network access is available to execute the pinned `golang.org/x/vuln/cmd/govulncheck@v1.6.0` scanner.

The offline evaluation tooling requires Python 3.11 or newer. Run `make eval-check PYTHON=python3.11` and `make pi-check` for the Python and Pi lanes; these are separate from the Go `make verify` lane.

CI runs the standard lanes independently, including a separate vulnerability scan, and joins them at the stable `quality` gate required by branch protection. Releases validate the exact tagged SHA in parallel with artifact construction; publication remains blocked until validation and native artifact smokes both pass.
