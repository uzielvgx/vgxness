# VGXNESS

VGXNESS is a local OpenCode-native manager product. The Go 1.26 repository delivers the `vgxness` executable, versioned self-installation, a keyboard-first TUI, SQLite/FTS5 schema v5, native memory and SDD storage, OpenSpec backends, guided setup, and release tooling.

The installed OpenCode projection has 18 managed artifacts: 15 model-bound agents headed by `vgxness-manager` v46, `general` v6, verifier v4, and reviewer profiles v3; the model-plan manifest; an `opencode.json` default-agent selection; and bounded `<config-dir>/vgxness/default-agent.json` restoration metadata. It installs no plugin. Managed OpenCode and generated Codex configure `vgxness mcp --full`, which exposes five memory and 13 SDD tools; the no-flag CLI MCP mode is read-only where documented as CLI behavior. MCP has no caller identity: authorization is owned by the host/operator permissions, user authorization, and task scope. It does not provide automatic memory injection, compaction, observability, plugin session identity, or a runtime-security guarantee. Historical retirement recognizes only exact `vgxness.ts` v1-v10 plugin bytes and `vgxness-autonomous-stacked-pr` v1/v2/v3 provider-skill bytes; modified, malformed, foreign, unknown, or newer bytes block without removal.

The read-only `status` and `doctor` commands report storage root, database, and schema health. Compatibility execution commands and subsystems are not part of the product.

## Documentation

| Document | Responsibility |
| --- | --- |
| [Product Blueprint — English](docs/product-blueprint.md) | Canonical version 1.0 product vision, status, taxonomy, boundaries, and roadmap. |
| [Plan maestro de producto — Español](docs/product-blueprint.es.md) | Complete version 1.0 Spanish companion; explicitly non-canonical, with English controlling conflicts. |
| [Go Implementation Architecture](docs/go-implementation.md) | Current Go packages, storage, setup, integration, and testing boundaries. |
| [Orchestration Flow](docs/orchestration-flow.md) | Active OpenCode-native manager and SDD lifecycle. |
| [Native Memory and Structured Storage](docs/memory.md) | SQLite schema v5 domains, isolation, memory lifecycle, and upgrade migration caveat. |
| [Synchronization service boundary](docs/sync.md) | Loopback-only daemon operation, HTTPS termination boundary, and runtime configuration. |
| [Versioned Self-installation](docs/self-install.md) | Permanent launcher, immutable SHA-256 versions, atomic activation, rollback, and safety behavior. |
| [Alpha releases](docs/release.md) | Release artifacts, support matrix, checksum verification, installation, and release rollback boundaries. |
| [Guided OpenCode Setup](docs/opencode-setup-wizard.md) | Complete explanatory wizard, confirmation boundary, verification, status, and recovery behavior. |
| [OpenCode Integration](docs/opencode-integration.md) | Persistent manager installation, managed identities, storage tools, and health. |
| [Codex Integration](docs/codex-integration.md) | Standalone Codex agent lifecycle and user-owned `config.toml` contract. |
| [Safe Hooks](docs/hooks.md) | No installed hook surface; historical plugin retirement context. |

## Development

Run `make fast` during iteration. It checks formatting and runs `go test -short ./...`; short mode omits only filesystem-heavy installation and durability lifecycles, not unit, security, drift, parsing, authorization, or repository contract tests.

Run `make verify` before submitting or tagging. It exposes the standard Go commands directly: ordinary coverage with a fresh test run, race, vet, formatting, non-mutating tidy diff, whitespace, module verification, build, focused Linux E2E, and Windows cross-build/test compilation. The Windows test commands compile and link packages behind `/usr/bin/true`; they do not execute Windows binaries. Native Windows installer and self-install tests remain required CI evidence. The network-dependent vulnerability scan is intentionally excluded from `make verify`; run `make vuln` separately when network access is available to execute the pinned `golang.org/x/vuln/cmd/govulncheck@v1.6.0` scanner.

CI runs the standard lanes independently, including a separate vulnerability scan, and joins them at the stable `quality` gate required by branch protection. Releases validate the exact tagged SHA in parallel with artifact construction; publication remains blocked until validation and native artifact smokes both pass.

## Direction

Native SDD supports `memory`, `openspec`, and `hybrid` backends plus per-change `automatic` or `interactive` execution. Hybrid keeps memory canonical; OpenSpec projection is bounded to `openspec/changes/<safe-change-id>/`, and divergent repository content is never imported automatically. The default `medium` plan uses Luna Fast, Terra, and Sol slots; changing any installed plan or slot requires an OpenCode restart.

By default, every workspace uses the project-isolated semantic and SDD domains in `~/.vgxness/memory.db`. Canonical workspace identity keeps same-named projects distinct. Older project-level databases are retained and are not imported automatically; explicit `--storage-root` and `--project-local` modes remain isolated overrides.

The manager chooses direct work, bounded read-only delegation, or structured SDD. Up to four independent read-only subworks may overlap; synthesis, patch application, validation, acceptance, projection writes, and lifecycle transitions remain sequential and manager-owned.

For eligible implementation tasks, manager v46 loads global `stacked-pr` v3 and completes the clean-checkout, identity, intended-path, estimate/slice, and fresh-branch pre-write gate before branch creation and source writes. Candidate validation, developmental checks, independent verification, and review occur before delivery mutations. The policy permits dirty checkout only through explicitly reauthorized, bounded unpublished-local-slice recovery; first publication uses the create-only empty-expectation lease, and existing remote branches and PRs remain read-only. `local-only`, `no commit`, `no push`, `no PR`, `no merge`, and `no cleanup` narrow the flow. Command globs do not prove argv or host behavior.

Observed delivery labels are strict: IMPLEMENTED means workspace changes and developmental checks are complete; VERIFIED means the exact frozen candidate also passed independent verification and review; DELIVERED means its commit was published and its new current-task PR was read back; MERGED means that PR and base containment were read back; INSTALLED additionally requires installation and handshake readback. Later labels are never inferred.

**Non-goal:** VGXNESS will not copy third-party code, prompts, schemas, names, skills, or exact workflows, and it will not permit silent destructive autonomy or hide operational truth behind a CLI.

For complete definitions and status classifications, read the [Product Blueprint](docs/product-blueprint.md). Supporting documents intentionally do not duplicate its roadmap.

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

`vgxness skills <preview|install|status|uninstall> [--skills-dir PATH]` manages the portable 42-file, 18-skill `skills-creator`, `stacked-pr`, `cross-platform`, `installer-lifecycle`, `agent-evaluation`, `ci-triage`, `security-boundary`, `documentation-strategy`, `product-requirements`, `software-architecture-docs`, `user-documentation`, `api-documentation`, `quality-test-documentation`, `operations-runbooks`, `governance-compliance-docs`, `release-lifecycle-docs`, `end-to-end-testing`, and `sdd-lifecycle` catalog in `~/.agents/skills` by default (or an isolated absolute destination). Setup retires only exact `vgxness.ts` v1-v10 plugin bytes and provider-owned `vgxness-autonomous-stacked-pr` v1/v2/v3 bytes before publishing `stacked-pr`; modified, malformed, foreign, unknown, or newer bytes block without removal. Portable skills are shared across hosts, and OpenCode uninstall never removes this global catalog.

The skills transaction anchors mutations in the selected root. An interrupted exact partial pack resumes with `install` or is safely backed up and removed with `uninstall`; unknown bytes remain drift. Windows uses atomic rename, readback, and backups, but cannot fsync directories, so its crash-durability guarantee is weaker.
