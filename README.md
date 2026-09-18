# VGXNESS

VGXNESS prepares the agent ecosystem. OpenCode Manager62, Codex Manager21 and the Pi Manager share the single current policy in `internal/orchestration/manager_contract.json`; their native adapters supply tools, model configuration and transport. Local installation receipts support future updates without retaining old agent templates in the source tree.

VGXNESS provisions native managers, tools and skills for OpenCode, Codex and standalone Pi. The provisioning CLI uses Go 1.26. It provides durable project memory in SQLite/FTS5 schema v23, guided setup and recoverable installation. Structured SDD is retired; historical database records are preserved.

OpenCode Manager62 installs 12 managed artifacts: seven agents (Manager, Explore, General, Verifier and three CARE roles), the memory lifecycle plugin, model manifest, two configuration artifacts and an installation receipt. Codex Manager21 installs ten artifacts: AGENTS.md, six delegated profiles, two plugin lifecycle manifests and an installation receipt. Pi uses the same six worker roles through its native TypeScript SDK/RPC adapter. MCP full mode exposes eight memory tools and no SDD tools. Installation does not prove model behavior or runtime authentication.

The shared Manager owns authorization, scope, lifecycle decisions and final acceptance. It distinguishes direct questions, bounded reads, implementation and delivery, delegates bounded independent work, and preserves one workspace writer. Significant work reports the outcome, approach and next observable milestone. Skills are loaded by applicability and never grant authority. The former fixed attempt budgets, Execution Brief and Candidate Capsule wording belong to previous prompts retained only in Git history; they are not current host enforcement. See [the shared contract](docs/architecture/shared-manager-contract.md).

Memory recall is limited to relevant prior context: search before exact-ID reads and use recent recall only for explicit recent-work or recovery requests. Durable, evidence-backed knowledge is assessed under a stable topic; secrets, raw transcripts, logs and transient status are excluded. There is no automatic cloud synchronization. The OpenCode lifecycle plugin can inject a bounded prior completed same-project handoff as untrusted data and does not capture transcripts. Exact legacy plugin and provider-skill bytes are retirement identities; modified, foreign, mixed or unknown packages remain protected.

The read-only `status` and `doctor` commands report storage root, database, and schema health. Use `vgxness doctor --all` to inspect storage, shared installation, and all three providers together; runtime/model evidence that is not observed is reported explicitly. See [system diagnostics](docs/diagnostics.md) for results and exit codes. Compatibility execution commands and subsystems are not part of the product.

## Documentation

| Document | Responsibility |
| --- | --- |
| [Product Blueprint — English](docs/product-blueprint.md) | Canonical source-linked capability inventory, current status, boundaries, limitations, and evidence gaps. |
| [Plan maestro de producto — Español](docs/product-blueprint.es.md) | Spanish companion to the capability inventory; non-canonical, with English controlling conflicts. |
| [Go Implementation Architecture](docs/go-implementation.md) | Current Go packages, storage, setup, integration, and testing boundaries. |
| [Orchestration Flow](docs/orchestration-flow.md) | Native Manager workflow and proportional CARE review. |
| [Native Memory and Structured Storage](docs/memory.md) | SQLite schema v23 domains, isolation, memory lifecycle, and upgrade migration caveat. |
| [Synchronization service boundary](docs/sync.md) | Loopback-only daemon operation, HTTPS termination boundary, and runtime configuration. |
| [Versioned Self-installation](docs/self-install.md) | Permanent launcher, immutable SHA-256 versions, atomic activation, rollback, and safety behavior. |
| [Releases and installation](docs/release.md) | Release artifacts, support matrix, checksum verification, installation, and release rollback boundaries. |
| [Guided OpenCode Setup](docs/opencode-setup-wizard.md) | Unified `setup opencode|codex|all` entrypoint, confirmation boundary, verification, status, and recovery behavior. |
| [OpenCode Integration](docs/opencode-integration.md) | Persistent manager installation, managed identities, storage tools, and health. |
| [Codex Integration](docs/codex-integration.md) | Standalone Codex agent lifecycle and user-owned `config.toml` contract. |
| [Safe Hooks](docs/hooks.md) | No installed hook surface; historical plugin retirement context. |
| [Legacy Compatibility Matrix](docs/legacy-compatibility.md) | Evidence-bound legacy formats, migrations, and retirement boundaries. |
| [Local agent-evaluation runner](docs/agent-evaluation-runner.md) | Opt-in development trace transport with offline regression coverage; it does not call models in CI or grade behavior automatically. |
| [Evaluation results](docs/agent-evaluation-results.md) | Sanitized, bounded results from provider, PostgreSQL, and two-client development evaluations. |

## Development

The offline evaluation tooling requires Python 3.11 or newer. Run `make eval-check PYTHON=python3.11` and `make pi-check` for the Python and Pi lanes; these are separate from the Go `make verify` lane.

Run `make fast` during iteration. It checks formatting and runs `go test -short ./...`; short mode omits only filesystem-heavy installation and durability lifecycles, not unit, security, drift, parsing, authorization, or repository contract tests.

Run `make verify` before submitting or tagging. It exposes the standard Go commands directly: ordinary coverage with a fresh test run, race, vet, formatting, non-mutating tidy diff, whitespace, module verification, build, focused Linux E2E, and Windows cross-build/test compilation. The Windows test commands compile and link packages behind `/usr/bin/true`; they do not execute Windows binaries. Native Windows installer and self-install tests remain required CI evidence. The network-dependent vulnerability scan is intentionally excluded from `make verify`; run `make vuln` separately when network access is available to execute the pinned `golang.org/x/vuln/cmd/govulncheck@v1.6.0` scanner.

CI runs the standard lanes independently, including a separate vulnerability scan, and joins them at the stable `quality` gate required by branch protection. Releases validate the exact tagged SHA in parallel with artifact construction; publication remains blocked until validation and native artifact smokes both pass.

## Direction

SDD has no runtime, CLI archive, MCP or Pi tool, worker binding, or bundled skill. Historical database records and immutable migration files remain for data preservation. Historical agent templates are removed; current model selection exposes seven agents.

By default, every workspace uses the project-isolated semantic memory in `~/.vgxness/memory.db`. Canonical workspace identity keeps same-named projects distinct. Older project-level databases are retained and are not imported automatically; explicit `--storage-root` and `--project-local` modes remain isolated overrides.

For a new cloud, reset it first and run `vgxness memory sync reseed --workspace /absolute/workspace --confirm-cloud-empty` on the Mac/source device. Each later Linux or Windows device uses `vgxness memory sync rejoin --workspace /absolute/workspace --confirm-merge`. These operations are per-project, never run `git pull`, require their exact confirmation, and resume safely on retry; see [Synchronization service boundary](docs/sync.md).

The Manager chooses direct work or bounded delegation. A short plan is used when scope or risk warrants it. Writers remain sequential. Model configuration compatibility is retained; historical model-plan schemas may contain inactive SDD slots, which do not install or authorize workers.

For authorized PR delivery the Manager loads global `git-delivery` and follows its current repository, candidate and publication checks. Delivery is a skill workflow, with native Git and host tools; the shared Manager is not a Git daemon or runtime enforcement layer. Local-only and no-push scopes remain local. A frozen candidate needs observed checks, independent verification and applicable review before claims of verification or delivery.

Observed delivery labels are strict: IMPLEMENTED means workspace changes and developmental checks are complete; VERIFIED means the exact frozen candidate also passed independent verification and review; DELIVERED means its commit was published and its new current-task PR was read back; MERGED means that PR and base containment were read back; INSTALLED additionally requires installation and handshake readback. Later labels are never inferred.

**Non-goal:** VGXNESS will not copy third-party code, prompts, schemas, names, skills, or exact workflows, and it will not permit silent destructive autonomy or hide operational truth behind a CLI.

For complete definitions and status classifications, read the [Product Blueprint](docs/product-blueprint.md). Supporting documents intentionally do not duplicate its roadmap.

## Choose models in Setup

Run `vgxness tui` and select the coding hosts to configure. On the Models screen,
use Tab to switch providers. OpenCode and Pi offer `1` for one model across all
seven agents or `2` for one per agent, including Manager. Select an agent with
the arrow keys, then press `m` to choose from the locally scanned provider/model
catalog (type to filter, Enter assigns) or `i` to type a reference manually.
Press `e` to cycle the efforts the scanned model reports. Codex alone uses the
low, medium, high and ultra plans.

Press Enter to preview, then review the exact assignments before applying. Existing
choices are preserved until edited. A model identifier does not prove account
access or runtime availability. See [model selection](docs/model-selection.md)
for CLI examples and the complete screen flow. If setup reports drift or recovery,
follow its status guidance; unknown settings and files are not overwritten.

## Installation and releases

### Local Pi package assembly

Run `vgxness-release pi --output /absolute/new-directory` to assemble an unpublished, local-only portable Pi package. The output directory must be new and outside this repository. It contains one `vgxness-pi-0.1.0.tgz` TypeScript tarball, `SHA256SUMS`, and `PROVENANCE.json`; the command neither installs nor publishes anything.

Provision that local release with `vgxness setup pi --preview --pi-release-dir /absolute/new-directory`, review the preview, then rerun without `--preview`. `--pi-agent-dir` selects Pi's settings root (otherwise `PI_CODING_AGENT_DIR`, then `~/.pi/agent`) and `--pi-root` selects VGXNESS's separate managed package root. Setup validates the complete portable release before preserving existing Pi settings and adding its absolute package path. `vgxness setup pi --status` inspects that installed state and does not require the original release directory. Pi then discovers that installed package through its normal settings package list; its native memory tools run without a VGXNESS CLI or MCP process.

The provisioned package is discovered through Pi's normal `settings.json` package list; no `pi -e` path or loader override is needed. It runs in Node 22.19.0 or newer, with Pi `^0.84.4` and `typebox` 1.3.7 supplied by the host. It contains no Go sidecar, native addon, or runtime compiler/install script. Its 23 SQL migrations, prompts, and fallback skills travel in the same tarball. This package intentionally has no Node `main` or `exports` entrypoint.

The extension activates an explicitly configured Manager model, or uses the host's selected model when no selection is configured. Worker execution uses the native Pi transport on supported hosts; see the Pi guide for its prerequisites. Worker processes are unavailable on Windows because owned process-tree reaping is not implemented there. Local Linux arm64 validation covers Node 22.19.0 and Node 24. Native macOS and Windows support claims require observed target-native validation. See [portable Pi packaging and health checks](docs/pi-typescript.md).

On macOS or Linux, install the release pinned by the official Homebrew tap:

```sh
brew install uzielvgx/tap/vgxness
```

Homebrew verifies the formula's pinned SHA-256 digest and owns the executable in its prefix. It does not modify OpenCode or install the separate `~/.local` launcher. Use the brewed executable to preview and explicitly apply that optional setup. See the [tap documentation](https://github.com/uzielvgx/homebrew-tap) for the exact commands and ownership boundaries.

On Windows, install through the official Scoop bucket:

```powershell
scoop bucket add vgxness https://github.com/uzielvgx/scoop-bucket
scoop install vgxness/vgxness
```

Scoop verifies the downloaded ZIP against the SHA-256 pinned in the manifest and owns its app directory and shim. It does not modify OpenCode or the separate managed installation. See the [bucket documentation](https://github.com/uzielvgx/scoop-bucket) for setup, updates, support, and uninstall boundaries.

Releases also provide unsigned archives for Linux, macOS, and Windows plus `SHA256SUMS`. Verify the downloaded archive before running it, then use the extracted `vgxness` or `vgxness.exe` binary to preview and perform self-installation. See [Releases and installation](docs/release.md) for acquisition, Homebrew, Scoop, exact artifact names, checksums, and platform support, and [Versioned self-installation](docs/self-install.md) for launcher, status, and rollback behavior. Self-installation does not download releases or edit `PATH`.
## Global portable skills

`vgxness skills <preview|install|status|uninstall> [--skills-dir PATH]` manages the portable 46-file, 18-skill `skills-creator`, `git-delivery`, `cross-platform`, `installer-lifecycle`, `agent-evaluation`, `ci-triage`, `security-boundary`, `documentation-strategy`, `product-requirements`, `software-architecture-docs`, `user-documentation`, `api-documentation`, `quality-test-documentation`, `operations-runbooks`, `governance-compliance-docs`, `release-lifecycle-docs`, `end-to-end-testing`, `memory-sync` catalog in `~/.agents/skills` by default (or an isolated absolute destination). Setup retires only exact `vgxness.ts` v1-v10 plugin bytes, provider-owned `vgxness-autonomous-stacked-pr` v1/v2/v3 bytes, and declared `stacked-pr` v3 bytes before publishing `git-delivery`; canonical `git-delivery` bytes at the legacy path and modified, malformed, foreign, unknown, or newer bytes block without removal. Portable skills are shared across hosts, and OpenCode uninstall never removes this global catalog.

The skills transaction anchors mutations in the selected root. An interrupted exact partial pack resumes with `install` or is safely backed up and removed with `uninstall`; unknown bytes remain drift. Windows uses atomic rename, readback, and backups, but cannot fsync directories, so its crash-durability guarantee is weaker.

## Pi portable setup

Pi runs entirely in TypeScript/Node and uses the shared SQLite memory database directly. VGXNESS provisions it from a pinned portable release or an explicit offline release directory; Pi then runs independently of the VGXNESS CLI, MCP and Go. `setup all` includes OpenCode, Codex and Pi. See [Pi setup](docs/pi-typescript.md) and the [v1 readiness audit](docs/v1-readiness.md) for prerequisites, validated behavior and outstanding evidence.

Independent verification and review inspect the same frozen candidate. `vgxness mcp --full` exposes eight memory tools. The OpenCode `plugins/vgxness-memory-lifecycle.ts` plugin has no `opencode.json` plugin entry. MCP has no caller identity; host permissions and scoped user authorization remain authoritative.
