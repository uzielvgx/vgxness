# VGXNESS

Go CLI and Claude Code plugin that gives a project durable memory (SQLite/FTS5,
schema v23), a session handoff, cloud sync and a terminal console. The product is
Claude Code only; the active plan is `docs/plans/claude-only.md` and every
change must respect it. The OpenCode, Codex and Pi providers are gone; the
console (`internal/tui`) is a shell until plan task 17 rebuilds it.

@docs/decisions.md

## Stack

- Go 1.26 (`go.mod` module `github.com/uzielvgx/vgxness`).
- TUI: Bubble Tea v2 + Lip Gloss v2 (`charm.land/...`); Bubbles v2 returns with task 17.
- Storage: `modernc.org/sqlite` (pure Go, FTS5); sync server: `pgx/v5` on Postgres.
- MCP: `github.com/modelcontextprotocol/go-sdk`.
- Plugin: `plugins/vgxness/` (agents, hooks, policy, skills, `.mcp.json`) with
  the marketplace manifest at `.claude-plugin/marketplace.json`.

## Commands

```sh
make fast      # gofmt check + go test -short ./...   (run before every commit)
make verify    # full lane: tests, race, vet, mod tidy, builds incl. windows, e2e
make vuln      # govulncheck
go run ./cmd/vgxness tui
claude plugin validate plugins/vgxness --strict
claude plugin validate .claude-plugin/marketplace.json --strict
```

CI: `.github/workflows/go-ci.yml` (tests, coverage, race, Postgres lane for
`internal/syncpg` and `cmd/vgxness-syncd`). Keep `make fast` green after every
slice.

## Structure

- `cmd/vgxness` main CLI (`version|status|doctor|memory|mcp|claude-code|tui`);
  `cmd/vgxness-syncd` sync server.
- `internal/cli` command handlers (`claudecode.go` is the Claude Code hook
  adapter, `workspace.go` the workspace resolution); `internal/app` wiring;
  `internal/memory` SQLite memory and migrations (never edit shipped
  migrations); `internal/mcp` MCP server (`instructions.go` holds the server
  instructions, ≤2,048 chars); `internal/tui` console; `internal/sync*` sync
  client, API, Postgres, admin; `internal/config`, `internal/secrets`,
  `internal/inspection`, `internal/buildinfo`, `internal/testutil`.
- `docs/` product and subsystem docs (rewritten in Spanish by plan task 15); `docs/plans/` plan
  files; `docs/research/` research behind the plan; `deploy/` syncd packaging.

## Conventions

- English for code, comments, commits (conventional commits), plan files and
  the decision log. User-facing docs and UI copy are Spanish (`ui-copy-es`).
- Tests are table-driven `_test.go` beside the code; bug fixes get a failing
  test first. `go test -short` must stay fast; e2e tests use the `e2e` tag.
- `.codegraph/` is indexed; use codegraph before grepping.

## Design

- Canvas: https://claude.ai/artifact/55i278YA64Yz3tKGM9zgvX (pages Índice,
  Consola, Arquitectura). Design system:
  https://claude.ai/artifact/5tnRiD2UwvqtFj58kyiKPf. Tokens and the rules the
  console code must follow are in `DESIGN.md`.
- Any TUI work must match the artboards (`tui-<module>-<screen>[-<state>]`);
  a deviation needs Uziel's OK and an update to the canvas.

## Known traps

- Memory DB lives in `~/.vgxness/`, never in `${CLAUDE_PLUGIN_DATA}`.
- Workspace identity: `--workspace` flag, then `CLAUDE_PROJECT_DIR`, then cwd;
  never trust `os.Getwd()` alone under Claude Code.
- `SessionEnd` hooks share a 1.5 s budget; hook adapters fail closed (exit 0,
  empty stdout).
- `make verify` builds for Windows and runs the deploy e2e tests; it is slow,
  use `make fast` while iterating.
- Hook `additionalContext` is capped at 10,000 chars; the policy file at 6,000.
- Plugin agents ignore `permissionMode`, `hooks` and `mcpServers`; read-only
  roles rely on their `tools` allowlist.
