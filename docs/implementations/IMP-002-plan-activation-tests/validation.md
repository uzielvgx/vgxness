# IMP-002 — Validation

Evidence actually obtained, with its limits. Nothing is claimed before it is
observed. This file is self-reported by the writer and is not independent
evidence. History is appended, never deleted.

## Status

- Active candidate: working tree on HEAD
  `96a153ee91ffedd23b64389c0151de622286a3b4` (dirty IMP-001 tree) plus the
  IMP-002 edits; the frozen candidate identity is recorded at T04 by the Manager.
- Generated Pi contract digest (`sourceDigest`) after regeneration:
  `a890f5453c72f97c30174041b27d28d62d5b60c1bd357b89a754b7d5c60f76ce`.
- Self-run development checks on this candidate: **run and passing** (below).
- Independent verification (T04): **pending**.
- CARE review (T04): **pending**.
- Installation readback (T06): **pending**; not performed by this worker.

## Checks (current candidate)

| Check | Command | Result | Limits |
| --- | --- | --- | --- |
| Go formatting | `gofmt -l <modified Go files>` | clean (exit 0) | Formatting only |
| Pi resources | `node packages/pi/scripts/generate-contract.mjs` then `--check` | regenerated; `--check` clean (exit 0) | Content equality, not runtime |
| Pi package | `node packages/pi/scripts/verify-package.mjs` | pass (exit 0) | Package/resource invariants |
| Orchestration + providers + e2e + cli + mcp + config | `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/orchestration/ ./internal/providers/opencode/ ./internal/providers/codex/ ./internal/e2e/ ./internal/cli/ ./internal/mcp/ ./internal/config/ -count=1` | `ok` ×7 | Deterministic projection and isolated-install tests |
| Broader Go | same env, `go test -short ./...` and `go vet ./...` | all `ok` / no failures; `vet` exit 0 | Dependencies already present; no network install |
| Pi typecheck | `npm run typecheck --workspace packages/pi` | pass | Types only |
| Pi tests | `npm test --workspace packages/pi` | `pass 174`, `fail 0` | Includes embedded-schema and negative assertions |
| CLI preflight | `opencode --version`; `opencode run --help`; `codex --version`; `codex exec --help`; `codex exec resume --help`; `codex login status` | opencode `1.18.32`; codex-cli `0.153.4`; login `Logged in using ChatGPT`; help captured | Capability/help evidence only; not installed-profile or live-model proof |

## Corpus and negative assertions

- `internal/orchestration/testdata/manager-scenarios.json`: the
  `plan-single-active` fragment is now `keep at most one active plan per
  session`; the case count is still **23** (no new cases).
- Negative assertions added for the ambiguous summary in the current prompts:
  `internal/providers/opencode/current_renderer_test.go`,
  `internal/providers/codex/render_test.go`,
  `internal/e2e/care_parity_test.go`, and
  `packages/pi/test/orchestration.test.ts` assert the corrected phrase is present
  and `exactly one active plan per session` is absent.
- Docs updated to the uniform wording: `docs/implementations/README.md`,
  `docs/orchestration-flow.md`, `docs/opencode-integration.md`,
  `docs/pi-typescript.md`, `docs/architecture/shared-manager-contract.md`.
- The embedded schema clause and the blocking-decision authority are unchanged;
  the pre-adaptive/bootstrap snapshot tests still pass.

## Intentional golden updates

- `internal/providers/opencode/current_renderer_test.go`: 28 per-agent hashes
  recomputed (seven agents per plan) because the shared contract digest and
  Manager text are embedded in every OpenCode agent. Manager per plan:
  high/ultra `710e4ca0…`, low `3ebfa01c…`, medium `fbf11b15…`.
- `internal/providers/codex/current_renderer_test.go`: current `AGENTS.md` hash
  recomputed to `38cfc2ef…` for all plans.
- `internal/providers/codex/bootstrap_contract_test.go` and the OpenCode
  pre-adaptive goldens: **unchanged**; historical snapshots stay byte-intact
  (the bootstrap/pre-adaptive tests pass).

## Limits

- Deterministic assertions establish declared policy and native wiring only; they
  do not establish live model routing, behavioral equivalence, or any
  protected-holdout result. The corpus remains a development partition.
- No Git delivery, install, lifecycle, memory, independent verification or CARE
  action is performed by this worker. The Manager owns T05/T06.
- The Codex native planning tool has no evidenced name and is referenced
  generically; the provider-neutral fallback is what is asserted.
