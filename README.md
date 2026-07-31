# VGXNESS

VGXNESS is a local OpenCode-native manager product. The Go 1.26 repository delivers the `vgxness` executable, versioned self-installation, a keyboard-first TUI, SQLite/FTS5 schema v5, native memory and SDD storage, OpenSpec backends, guided setup, and release tooling.

The installed projection has 20 managed artifacts: 15 model-bound agents headed by `vgxness-manager` v36, storage plugin v5, the model-plan manifest, an `opencode.json` default-agent selection, bounded `<config-dir>/vgxness/default-agent.json` restoration metadata, and the independent `vgxness-autonomous-stacked-pr` skill. Setup uses a semantic merge to set `default_agent="vgxness-manager"` in `opencode.json` while preserving unrelated JSON values; existing `opencode.jsonc` bytes remain unchanged. The restoration metadata records whether `opencode.json` existed and any prior explicit default, so uninstall can restore that default or remove a config created by setup. The plugin exposes five semantic-memory tools and 13 SDD tools. OpenCode owns execution. Manager, managed `general`, and verifier have global tool permission; their prompts still assign orchestration, delegated implementation, and non-mutating verification roles. The storage plugin never executes, routes, edits, or delegates, and SDD mutations fail closed outside the tracked top-level manager session.

The read-only `status` and `doctor` commands report storage root, database, and schema health. Compatibility execution commands and subsystems are not part of the product.

## Documentation

| Document | Responsibility |
| --- | --- |
| [Product Blueprint — English](docs/product-blueprint.md) | Canonical version 1.0 product vision, status, taxonomy, boundaries, and roadmap. |
| [Plan maestro de producto — Español](docs/product-blueprint.es.md) | Complete version 1.0 Spanish companion; explicitly non-canonical, with English controlling conflicts. |
| [Go Implementation Architecture](docs/go-implementation.md) | Current Go packages, storage, setup, integration, and testing boundaries. |
| [Orchestration Flow](docs/orchestration-flow.md) | Active OpenCode-native manager and SDD lifecycle. |
| [Native Memory and Structured Storage](docs/memory.md) | SQLite schema v5 domains, isolation, memory lifecycle, and upgrade migration caveat. |
| [Versioned Self-installation](docs/self-install.md) | Permanent launcher, immutable SHA-256 versions, atomic activation, rollback, and safety behavior. |
| [Alpha releases](docs/release.md) | Release artifacts, support matrix, checksum verification, installation, and release rollback boundaries. |
| [Guided OpenCode Setup](docs/opencode-setup-wizard.md) | Complete explanatory wizard, confirmation boundary, verification, status, and recovery behavior. |
| [OpenCode Integration](docs/opencode-integration.md) | Persistent manager installation, managed identities, storage tools, and health. |
| [Safe Hooks](docs/hooks.md) | Active OpenCode plugin hooks and explicit shell/Git-hook exclusions. |

## Development

Run `make fast` during iteration. It checks formatting and runs `go test -short ./...`; short mode omits only filesystem-heavy installation and durability lifecycles, not unit, security, drift, parsing, authorization, or repository contract tests.

Run `make verify` before submitting or tagging. It exposes the standard Go commands directly: ordinary coverage with a fresh test run, race, vet, formatting, non-mutating tidy diff, whitespace, module verification, build, focused Linux E2E, and Windows cross-build/test compilation. The Windows test commands compile and link packages behind `/usr/bin/true`; they do not execute Windows binaries. Native Windows installer and self-install tests remain required CI evidence.

CI runs the standard lanes independently and joins them at the stable `quality` gate required by branch protection. Releases validate the exact tagged SHA in parallel with artifact construction; publication remains blocked until validation and native artifact smokes both pass.

## Direction

Native SDD supports `memory`, `openspec`, and `hybrid` backends plus per-change `automatic` or `interactive` execution. Hybrid keeps memory canonical; OpenSpec projection is bounded to `openspec/changes/<safe-change-id>/`, and divergent repository content is never imported automatically. The default `medium` plan uses Luna Fast, Terra, and Sol slots; changing any installed plan or slot requires an OpenCode restart.

By default, every workspace uses the project-isolated semantic and SDD domains in `~/.vgxness/memory.db`. Canonical workspace identity keeps same-named projects distinct. Older project-level databases are retained and are not imported automatically; explicit `--storage-root` and `--project-local` modes remain isolated overrides.

The manager chooses direct work, bounded read-only delegation, or structured SDD. Up to four independent read-only subworks may overlap; synthesis, patch application, validation, acceptance, projection writes, and lifecycle transitions remain sequential and manager-owned.

For eligible implementation tasks, manager v36 loads the managed skill and can create a fresh normalized branch, make normal commits, perform first pushes, and open non-draft stacked pull requests after freeze, verification, and review without a second routine approval. Explicit `local-only`, `no commit`, `no push`, and `no PR` restrictions narrow that flow. This is a static OpenCode policy: command globs do not prove argument semantics or guarantee Git, GitHub, credential, hook, network, or branch-protection behavior.

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

`vgxness skills <preview|install|status|uninstall> [--skills-dir PATH]` manages the separate 15-file `agent-skill-engineer` portable pack in `~/.agents/skills` by default (or an isolated absolute destination). Setup installs and verifies it automatically. Portable skills are shared across hosts; provider-specific skills remain owned by their provider integration, and OpenCode uninstall never removes this global pack.

The skills transaction anchors mutations in the selected root. An interrupted exact partial pack resumes with `install` or is safely backed up and removed with `uninstall`; unknown bytes remain drift. Windows uses atomic rename, readback, and backups, but cannot fsync directories, so its crash-durability guarantee is weaker.
