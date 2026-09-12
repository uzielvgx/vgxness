# Portable Pi TypeScript package

`@vgxness/pi` runs its VGXNESS services inside Node. It requires Node **22.19.0 or newer**, the Pi host `@earendil-works/pi-coding-agent` compatible with `^0.84.4`, and the host's `typebox` 1.3.7. These host requirements are explicit peer dependencies. Pi's extension loader resolves the host APIs; the offline installer does not fetch npm dependencies. The runtime package contains no Go executable, native addon, CLI/MCP launcher, platform sidecar, or compiler/install script.

## Assemble and provision

Run `vgxness-release pi --output /absolute/new-directory` from the repository. The new directory must be outside the repository. Assembly produces exactly:

- `vgxness-pi-0.1.0.tgz`
- `SHA256SUMS`
- `PROVENANCE.json`

The tarball includes TypeScript source, all 23 hash-bound SQL migrations and their manifest, prompt resources, the read-only fallback skill catalog, and the license. Assembly verifies source identity before and after packing, reads back the artifact and checksums, and publishes without overwriting an existing output directory. Go is a repository build/provisioning tool; it is not a Pi runtime dependency. Source-tree `npm pack` is not the release assembly path because the managed skill fallback is supplied by the assembler.

Preview with `vgxness setup pi --preview --pi-release-dir /absolute/new-directory`, then apply with the same arguments without `--preview`. `--pi-agent-dir` selects Pi's settings root; `--pi-root` selects a separate managed package root. The installer validates the portable artifact and preserves foreign settings, object entry filters, user data, and previous managed package directories. Existing ownership, no-overwrite publication, retained reservation recovery, and activation rollback rules still apply.

A managed legacy Go package remains intact. Status reports that it needs an update; provisioning a new portable release activates the new managed path. There is no automatic deletion of the old package or memory database.

## Model selection

Pi supports one model for all agents or an explicit model per agent, including the Manager. See [Choose models during installation](model-selection.md). The native `model_resolve` tool takes an empty object and returns the configured role assignments; it no longer selects plans. Settings and worker execution remain native to Pi.

## Native tools and session state

The extension opens the shared `~/.vgxness/memory.db` directly. An embedding host can select another absolute `storageRoot` through `createPiExtension`; worker missions cannot override workspace, project, mode, or role. Model-facing memory tools keep the existing Go result contract (`ID`, `Content`, `Preview`, and other capitalized entry fields). Only a full Manager can mutate memory. Structured SDD is retired; no SDD tools or worker roles are exposed. Historical records remain preserved in the shared database.

The following additive Pi commands show read-only local views without adding conversation memory:

- `/vgx-status`: native runtime, current session, worker transport, and model-catalog availability.
- `/vgx-workers`: bounded recent worker execution status and elapsed time.
- `/vgx-memory`: ten recent project-memory previews, marked as untrusted data.

These commands preserve the underlying tools. Missing storage is reported as unavailable without creating it, and a context without interactive UI receives a structured command result. RPC contexts with notification support receive the same text through Pi's notification channel.

`session_handoff` saves only the explicitly supplied summary. Compaction and tree navigation checkpoint the current lease; reload detaches and reacquires local authority. An active session renews its lease hourly, including while waiting for model work. Quit publishes a completed-session memory only when an explicit draft or summary exists. Fork, tree entries, and resumed transcripts never grant worker authority or create memory automatically.

`apply_patch` uses Pi's shared per-file mutation queues and sequential execution mode, coordinating with built-in write/edit. A failed recovery throws `recovery_pending` with `retrySafe=false` and retained recovery paths; it must not be retried as an ordinary patch failure.

## Foreground sync credentials

For file-backed foreground sync, provide an existing private credential file to `createPiExtension({ credentialFile: "/absolute/private/sync-token" })`, or start Pi with `VGXNESS_PI_CREDENTIAL_FILE` set to that absolute path. The file contains the device's existing `vgx1.<device UUID>.<secret>` bearer, optionally followed by one newline. On Unix it must belong to the current user and have no group or other permission bits, for example mode `0600`.

Then use the native `memory` tool with `operation: "sync.configure"`, the HTTPS `endpoint`, and its matching `deviceId`. Configuration validates the bearer against the device before storing a profile. Only the fixed reference `secret://keychain/sync/file` is persisted: the file path and bearer stay out of SQLite and tool output. An existing profile with a different credential reference is not silently converted. `memory` operations `sync.status` and `sync` inspect configuration and perform bounded foreground synchronization. Startup and the status views do not run background network sync.

The dispatcher supplies the bounded native Fetch transport and supports injected HTTP and credential ports for host integrations. It does not retrieve OS-keychain credentials by itself. Linux credential reads pin directory descriptors; other platforms check regular files, path identity, and metadata before and after reading. Node exposes neither a portable atomic `openat` operation nor Windows ACL inspection, so Windows access control remains the host's responsibility.

## Scoped exploration and workers

Pi discovers its worker CLI from the current Pi process or a verified Pi executable on `PATH`. `workerCli` and `VGXNESS_PI_CLI` provide explicit overrides. Workers receive only the selected model's authentication through the private bootstrap pipe and have isolated Pi profiles and resource sets.

An `explore` mission can include an `exploration` object with up to 16 accepted workspace-relative `roots`, `maxFiles` (1–10,000), `maxBytes` (1–16,777,216), and `maxTokens` (1–65,536). Its native `worker_list`, `worker_search`, and `worker_read_page` tools enforce these cumulative budgets and reject path escapes, symbolic links, binary reads, and stale cursors. Search is a literal query within one authorized file; paged reads default to 2,048 characters and permit up to 4,096. Directory pages contain at most 50 entries; search pages contain at most 20 matches. Every page reports a continuation cursor where applicable and budget/truncation diagnostics. Output uses UTF-8 bytes as a conservative token upper bound; rereading a page charges the complete bounded file again.

Explore has no shell or write authority. Writable workers retain exact target hashes, accepted role/mode and nonce bindings. A full worker also receives a host-issued, non-extendable grant captured before launch. Its child checks the fixed expiry and a descriptor-pinned host revocation byte immediately before patch commits and check launches; an absent, expired, invalid, or revoked grant prevents a new submission. Renewal issues a fresh grant and does not extend an already captured one. Cancellation or revocation cannot undo an OS operation that was already submitted. OpenSpec-only bindings verify the exact canonical external files and reject drift or symlinks. Worker results accumulate text, usage and diagnostics through retries and resolve only at Pi's `agent_settled` event. Windows worker process-tree ownership remains unsupported; package and storage support are separate from that limitation.

## Health and validation

`vgxness setup pi --status` inspects the installed package independently of the original release directory. It runs `node src/probe.ts` with an isolated temporary home and storage. The probe applies all migrations to a temporary database and checks schema version 23, foreign keys, FTS5, exact 64-bit integers, and SQLite backup, then removes its temporary files. It does not open configured user memory or authentication state.

Local Linux arm64 checks cover Node 22.19.0 and Node 24, package extraction, the Pi SDK loader, and temporary memory read/write. A portable artifact does not by itself establish native macOS or Windows runtime support; those claims require observed target-native checks. Worker process support has its own platform constraints, separate from the package and SQLite probe.

## Manager orchestration and delegated skills

The Manager prompt defines task framing, skill selection, bounded delegation, independent verification, memory assessment, session handoff and explicit delivery authority. Pi's native skill catalog supplies descriptions; the Manager reads the applicable SKILL.md and necessary references before applying a workflow. The bundled `skills-creator` is discoverable in Pi; its optional structural validators require Python 3, not a Pi runtime service.

`task.skills` accepts up to eight managed skill selections, each with `name`, the SKILL.md `sha256` returned by the Manager-only `vgx_skill` list/read tool, and optional `resources` (up to seven relative text paths). SKILL.md is always included. Shared compatible skills take precedence over package fallbacks. Unknown names, path escapes, symlinks, binary resources, altered digests and excess budgets fail before launch. A resource is limited to 64 KiB and the mission's skill text to 128 KiB. Discovery markers identify catalog eligibility; they are not cryptographic proof of a third-party publisher's trustworthiness.

The host supplies immutable text snapshots with per-file hashes inside the mission digest. Workers see the complete goal, criteria, role/mode, allowed targets/commands, return budget and selected resources through their RPC prompt. They do not inherit ambient skills, the conversation, memory access or Manager authority. Snapshot resources are guidance, not executable permissions or live file handles; missing referenced resources must be reported. A later skill edit does not rewrite a mission already issued.

These contracts are covered by resource validation, native task loading and RPC transport tests. Instruction delivery does not prove a model will follow every workflow correctly; open-ended task quality still requires behavioral evaluation with the selected model. Existing Windows worker and authentication limitations remain applicable.

The Manager and six worker definitions now come from the shared registry,
with native Pi tool guidance supplied by its adapter. See
[Shared Manager and native adapters](architecture/shared-manager-contract.md)
for regeneration, exact predecessor preservation and evidence limits.

## Known Manager evaluation limitations

Pi keeps its current Manager workflow; it does not automatically add an independent model review of every proposed evaluation. Loading a skill successfully does not establish that the resulting proposal follows all of its requirements.

In the six-case public development evaluation described in [V1 readiness](v1-readiness.md#pi-manager-behavioral-evidence), the five direct scenarios passed, but the generated-evaluation scenario failed. The proposal made independent grading optional and included an expected action that contradicted its own example request. Prompt-only follow-up changes did not establish a correction.

When using a Manager-generated evaluation, check every example against the action actually requested and check whether its grading procedure satisfies the selected skill's independence requirements before relying on the conclusion. An explicitly requested action can still need execution details; missing details do not erase authorization for that action. This is guidance for reviewing a proposal, not an additional automatic runtime gate. The selected model, skill and task can affect the result; these development cases do not certify general reliability.

## Portable release and setup behavior

Pi uses the TypeScript package and the shared SQLite data model; its runtime does not require the old Go Pi package, MCP, or a VGXNESS process. Use an existing pinned release tag with `vgxness setup pi --yes --pi-release-version <vSemVer>`. A release binary defaults to its own build tag; a development or non-release build requires an explicit pin. There is no implicit `latest`. `--pi-release-dir PATH` supplies an offline release and cannot be combined with the version flag. Preview and status do not acquire or activate a release. `setup all` includes OpenCode, Codex, and Pi.

Node and the `pi` command are presence prerequisites. The isolated package probe is separate evidence of runtime compatibility. Installation preserves foreign Pi settings and unrelated user data. Worker support is unavailable on Windows. Views expose loading, available, stale or unavailable snapshots. Stale views retain the last successful observation time and do not write conversation memory. Worker results expose a single bounded terminal envelope, including requested/selected/effective model and effort (unknown when unobserved), usage, diagnostics and retained recovery references. A lost or expired lease prevents further mutations; cancellation checks do not undo writes already completed by an authorized worker. Inspect retained recovery paths before retrying. Validate locally with `npm ci --ignore-scripts`, `npm run typecheck --workspace packages/pi`, and the full Pi test runner.

Use `npm test --workspace packages/pi` for the complete discovered test set. The checkout pins TypeScript and Pi SDK development dependencies in its lockfile; those development dependencies are stripped from the assembled runtime package. The configured CI matrix covers Node 22.19.0 and 24 on Linux, macOS and Windows. Local Linux ARM64 evidence does not establish execution on the other targets. This product assumes trusted repositories and hosts; tool allowlists and mission checks are not an operating-system sandbox.

The worker allows up to 10 seconds for the initial SDK/RPC handshake. Subsequent control requests keep a two-second timeout; the prompt retains its 30-second default deadline. Session grant expiry and cancellation can stop startup earlier and never gain extra mutation time from these transport limits.
