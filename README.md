# VGXNESS

Current delivery is global `git-delivery` v1 (exact `stacked-pr` v3 migration), policy-only isolated worktrees, and no Go/runtime writer, daemon, or durable delivery state. OpenCode is CARE-v2 Manager59 with immediate CARE-v2 Manager58, then CARE-v1 Manager58/Manager57; Codex is Manager18 with immediate Manager17, then Manager16/15/14.

VGXNESS is a local OpenCode-native manager product. The Go 1.26 repository delivers the `vgxness` executable, versioned self-installation, a keyboard-first TUI, SQLite/FTS5 schema v22, native memory and SDD storage, OpenSpec backends, guided setup, and release tooling. The SQLite schema is storage infrastructure; semantic memory, structured SDD records, and OpenSpec projections remain distinct domains.

The installed OpenCode projection has 17 managed artifacts: 13 model-bound agents headed by `vgxness-manager` v59, `general` v10, verifier v7, three CARE v2 profiles, and six SDD profiles; the auto-discovered `plugins/vgxness-memory-lifecycle.ts` plugin; the model-plan manifest; an `opencode.json` default-agent selection; and bounded `<config-dir>/vgxness/default-agent.json` restoration metadata. The plugin has no `opencode.json` plugin entry. Generated Codex Manager18 provides the equivalent provider-native contract. Both require a complete Candidate Capsule for frozen, risky, verification, and SDD delegations. MCP is local stdio for a trusted host: it has no caller identity or session authentication, so host tool allowlists, operator permissions, user authorization, and task scope form the authorization boundary. `vgxness mcp` exposes `memory_recent`, `memory_search`, and `memory_context`; only an explicit `vgxness mcp --full` exposes the full eight memory and 13 SDD tools read/write set. `memory_search` accepts `match_mode` `all` or `any`, defaulting to all-term matching.

OpenCode manager v59 and Codex manager v18 embed a shared prompt-level adaptive contract. They silently classify the request without tools or delegation; no-effect conversation, writing, translation, summarization, brainstorming, and planning use zero execution tools, skills, todos or task lists, delegation, or review. Bounded simple exact reads allow at most three tool attempts without delegation or tracking, while complex evidence research may use one read-only delegation. Failures and retries count, and the manager stops before exceeding the selected budget. Reversible actions, repository engineering, and irreversible or high-risk work retain their authorization, TDD, freeze, verification, review, and delivery guarantees. These are prompt instructions, not runtime enforcement, and no external, NLP, or holdout result is claimed.

Recall remains intent-triggered: managers search all-term first, fall back to any-term only when needed, retrieve full content by exact ID after preview, and reserve recent recall for explicit recent-work, session, or compaction-recovery requests. Orthogonally, after any route the prompt permits at most one autonomous save only for a durable, evidence-backed, safely assessed project decision, preference, constraint, or learning; it excludes transient state, logs, secrets, and personal data, adds no engineering ceremony, and performs no automatic cloud sync. Read-only agent allowlists exclude every mutating MCP tool. The product does not broadly inject recent memories or transcripts into every prompt; at the first eligible top-level system transform, the managed lifecycle plugin's only automatic memory injection is one bounded same-project prior completed handoff as untrusted data, never instructions. Lifecycle events do not capture transcript content. The product does not provide automatic compaction, broad observability, plugin session identity, or a runtime-security guarantee. OpenCode current CARE-v2 Manager59 has immediate CARE-v2 Manager58, then CARE-v1 Manager58 and CARE-v1 Manager57 predecessors; OpenCode v56 is deeper. Codex current Manager18 has immediate Manager17, then Manager16 and deeper Manager15/v14 identities; none are current runtime markers. Exact `vgxness.ts` v1-v10 plugin bytes and `vgxness-autonomous-stacked-pr` v1/v2/v3 provider-skill bytes remain retirement identities. Modified, malformed, foreign, unknown, or newer bytes block without removal.

The read-only `status` and `doctor` commands report storage root, database, and schema health. Compatibility execution commands and subsystems are not part of the product.

## Documentation

| Document | Responsibility |
| --- | --- |
| [Product Blueprint — English](docs/product-blueprint.md) | Canonical version 1.0 product vision, status, taxonomy, boundaries, and roadmap. |
| [Plan maestro de producto — Español](docs/product-blueprint.es.md) | Complete version 1.0 Spanish companion; explicitly non-canonical, with English controlling conflicts. |
| [Go Implementation Architecture](docs/go-implementation.md) | Current Go packages, storage, setup, integration, and testing boundaries. |
| [Orchestration Flow](docs/orchestration-flow.md) | Active OpenCode-native manager and SDD lifecycle. |
| [Native Memory and Structured Storage](docs/memory.md) | SQLite schema v22 domains, isolation, memory lifecycle, and upgrade migration caveat. |
| [Synchronization service boundary](docs/sync.md) | Loopback-only daemon operation, HTTPS termination boundary, and runtime configuration. |
| [Versioned Self-installation](docs/self-install.md) | Permanent launcher, immutable SHA-256 versions, atomic activation, rollback, and safety behavior. |
| [Alpha releases](docs/release.md) | Release artifacts, support matrix, checksum verification, installation, and release rollback boundaries. |
| [Guided OpenCode Setup](docs/opencode-setup-wizard.md) | Unified `setup opencode|codex|all` entrypoint, confirmation boundary, verification, status, and recovery behavior. |
| [OpenCode Integration](docs/opencode-integration.md) | Persistent manager installation, managed identities, storage tools, and health. |
| [Codex Integration](docs/codex-integration.md) | Standalone Codex agent lifecycle and user-owned `config.toml` contract. |
| [Safe Hooks](docs/hooks.md) | No installed hook surface; historical plugin retirement context. |
| [Legacy Compatibility Matrix](docs/legacy-compatibility.md) | Evidence-bound legacy formats, migrations, and retirement boundaries. |

## Development

Run `make fast` during iteration. It checks formatting and runs `go test -short ./...`; short mode omits only filesystem-heavy installation and durability lifecycles, not unit, security, drift, parsing, authorization, or repository contract tests.

Run `make verify` before submitting or tagging. It exposes the standard Go commands directly: ordinary coverage with a fresh test run, race, vet, formatting, non-mutating tidy diff, whitespace, module verification, build, focused Linux E2E, and Windows cross-build/test compilation. The Windows test commands compile and link packages behind `/usr/bin/true`; they do not execute Windows binaries. Native Windows installer and self-install tests remain required CI evidence. The network-dependent vulnerability scan is intentionally excluded from `make verify`; run `make vuln` separately when network access is available to execute the pinned `golang.org/x/vuln/cmd/govulncheck@v1.6.0` scanner.

CI runs the standard lanes independently, including a separate vulnerability scan, and joins them at the stable `quality` gate required by branch protection. Releases validate the exact tagged SHA in parallel with artifact construction; publication remains blocked until validation and native artifact smokes both pass.

## Direction

Native SDD supports `memory`, `openspec`, and `hybrid` backends plus per-change `automatic` or `interactive` execution. Hybrid keeps memory canonical; OpenSpec projection is bounded to `openspec/changes/<safe-change-id>/`, and divergent repository content is never imported automatically. The `low`, `medium`, `high`, and `ultra` plans combine Luna, Terra, and Sol slots with different effort levels in OpenCode and delegated Codex profiles; changing an installed plan requires restarting the affected host.

By default, every workspace uses the project-isolated semantic and SDD domains in `~/.vgxness/memory.db`. Canonical workspace identity keeps same-named projects distinct. Older project-level databases are retained and are not imported automatically; explicit `--storage-root` and `--project-local` modes remain isolated overrides.

For a new cloud, reset it first and run `vgxness memory sync reseed --workspace /absolute/workspace --confirm-cloud-empty` on the Mac/source device. Each later Linux or Windows device uses `vgxness memory sync rejoin --workspace /absolute/workspace --confirm-merge`. These operations are per-project, never run `git pull`, require their exact confirmation, and resume safely on retry; see [Synchronization service boundary](docs/sync.md).

The manager chooses direct work, bounded read-only delegation, or structured SDD. Up to four independent read-only subworks may overlap; synthesis, patch application, validation, acceptance, projection writes, and lifecycle transitions remain sequential and manager-owned. OpenCode profiles can use provider/model references per efficient, balanced, and frontier slot; mixed providers require all three references and all three efforts, use manifest v2, and require an OpenCode restart after artifact changes. Homogeneous presets remain manifest v1; custom availability is reported as unknown without an auth or availability probe.

For eligible implementation tasks, manager v59 loads global `git-delivery` v1, migrated exactly from `stacked-pr` v3, and completes the clean-checkout, identity, intended-path, estimate/slice, and fresh-branch pre-write gate before branch creation and source writes. Slice 1 targets the original base; later slices initially target their immediate predecessor. After its predecessor merges and is proven contained in the original base, a later PR must be explicitly retargeted to the original base with validated option-free tokens and exact pre/post head/base readback before required checks and merge; wrong or premature bases, drift, failed checks, ambiguity, or host failure stop. Candidate validation, developmental checks, independent verification, and CARE review occur before delivery mutations. The policy permits dirty checkout only through explicitly reauthorized, bounded unpublished-local-slice recovery; first publication uses the create-only empty-expectation lease, and existing remote branches and PRs remain read-only. `local-only`, `no commit`, `no push`, `no PR`, `no merge`, and `no cleanup` narrow the flow. Command globs do not prove argv or host behavior.

Observed delivery labels are strict: IMPLEMENTED means workspace changes and developmental checks are complete; VERIFIED means the exact frozen candidate also passed independent verification and review; DELIVERED means its commit was published and its new current-task PR was read back; MERGED means that PR and base containment were read back; INSTALLED additionally requires installation and handshake readback. Later labels are never inferred.

**Non-goal:** VGXNESS will not copy third-party code, prompts, schemas, names, skills, or exact workflows, and it will not permit silent destructive autonomy or hide operational truth behind a CLI.

For complete definitions and status classifications, read the [Product Blueprint](docs/product-blueprint.md). Supporting documents intentionally do not duplicate its roadmap.

## Choose models in Setup

Run `vgxness tui`, press `g`, choose **Setup** with `j`/`k`, and press `Enter`. Press `m` to open the 13-agent assignment matrix. Use `j`/`k` or the arrow keys to move between agents, `h`/`l` or left/right to choose a model, and `[`/`]` to change requested effort. `Enter` returns to a fresh preview; `Esc` cancels the matrix edit.

Setup initially reads the local OpenCode model cache with the exact argv `opencode models --pure`. In the matrix, `r` is an explicit refresh and runs `opencode models --pure --refresh`; a refresh failure keeps the current assignments and cached choices so you can retry. Local discovery proves only that an identifier is present. It does not prove provider authorization, account access, model support, or runtime availability. Non-static discovered identifiers are therefore recorded as `custom` with `unknown` availability.

Review every agent's model, requested effort, source, and availability, plus the preview digest. Press `a`, then `y`, only when that exact preview is correct; no setup files change before `y`. The result reports requested and effective effort, variant, and any degradation reason. Opening or previewing an installed v1/v2 plan does not promote it to v3; editing the matrix does. An installed v3 plan re-enters with the same 13 explicit assignments.

If discovery fails, retry with `r` or keep the retained choices. If preview fails, correct the shown prerequisite and refresh it. If apply fails, follow the displayed recovery guidance and inspect status before retrying; VGXNESS does not silently discard retained installation or recovery evidence.

## Installation and releases

On macOS or Linux, install the published alpha through the official Homebrew tap:

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

Alpha releases also provide unsigned archives for Linux, macOS, and Windows plus `SHA256SUMS`. Verify the downloaded archive before running it, then use the extracted `vgxness` or `vgxness.exe` binary to preview and perform self-installation. See [Alpha releases](docs/release.md) for acquisition, Homebrew, Scoop, exact artifact names, checksums, and platform support, and [Versioned self-installation](docs/self-install.md) for launcher, status, and rollback behavior. Self-installation does not download releases or edit `PATH`.
## Global portable skills

`vgxness skills <preview|install|status|uninstall> [--skills-dir PATH]` manages the portable 47-file, 19-skill `skills-creator`, `git-delivery`, `cross-platform`, `installer-lifecycle`, `agent-evaluation`, `ci-triage`, `security-boundary`, `documentation-strategy`, `product-requirements`, `software-architecture-docs`, `user-documentation`, `api-documentation`, `quality-test-documentation`, `operations-runbooks`, `governance-compliance-docs`, `release-lifecycle-docs`, `end-to-end-testing`, `memory-sync`, and `sdd-lifecycle` catalog in `~/.agents/skills` by default (or an isolated absolute destination). Setup retires only exact `vgxness.ts` v1-v10 plugin bytes, provider-owned `vgxness-autonomous-stacked-pr` v1/v2/v3 bytes, and declared `stacked-pr` v3 bytes before publishing `git-delivery`; canonical `git-delivery` bytes at the legacy path and modified, malformed, foreign, unknown, or newer bytes block without removal. Portable skills are shared across hosts, and OpenCode uninstall never removes this global catalog.

The skills transaction anchors mutations in the selected root. An interrupted exact partial pack resumes with `install` or is safely backed up and removed with `uninstall`; unknown bytes remain drift. Windows uses atomic rename, readback, and backups, but cannot fsync directories, so its crash-durability guarantee is weaker.
