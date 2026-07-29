# VGXNESS

VGXNESS is a local OpenCode-native manager product. The Go 1.26 repository delivers the `vgxness` executable, versioned self-installation, a keyboard-first TUI, SQLite/FTS5 schema v5, native memory and SDD storage, OpenSpec backends, guided setup, and release tooling.

The installed projection has 14 managed artifacts: `vgxness-manager` v30, five read-only reviewers, six read-only SDD profiles, storage plugin v5, and the model-plan manifest. The plugin exposes five semantic-memory tools and 13 SDD tools. OpenCode owns execution; the manager is the sole workspace and lifecycle writer. The storage plugin never executes, routes, edits, or delegates, and SDD mutations fail closed outside the tracked top-level manager session.

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

## Direction

Native SDD supports `memory`, `openspec`, and `hybrid` backends plus per-change `automatic` or `interactive` execution. Hybrid keeps memory canonical; OpenSpec projection is bounded to `openspec/changes/<safe-change-id>/`, and divergent repository content is never imported automatically. The default `medium` plan uses Luna Fast, Terra, and Sol slots; changing any installed plan or slot requires an OpenCode restart.

By default, every workspace uses the project-isolated semantic and SDD domains in `~/.vgxness/memory.db`. Canonical workspace identity keeps same-named projects distinct. Older project-level databases are retained and are not imported automatically; explicit `--storage-root` and `--project-local` modes remain isolated overrides.

The manager chooses direct work, bounded read-only delegation, or structured SDD. Up to four independent read-only subworks may overlap; synthesis, patch application, validation, acceptance, projection writes, and lifecycle transitions remain sequential and manager-owned.

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
