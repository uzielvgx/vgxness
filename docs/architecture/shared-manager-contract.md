# Shared Manager and native adapters

`internal/orchestration/manager_contract.json` is the maintained Manager and
worker policy source. Its versioned schema defines ordered Manager instructions,
six canonical worker roles, aliases, write authority, role instructions and
model-plan role identifiers. Provider adapters supply native tool names,
transport, permissions and model configuration. Each agent has exactly one maintained definition; historical prompt bodies and
reconstruction chains are removed from the source tree.

Project code exploration is delegated to the read-only Explore role when the
work is broad or multi-symbol, independently parallelizable, uncertain,
cross-cutting or higher risk, or needs independent review; delegation is a
choice, never a permission elevation, and a delegated mission carries an explicit
child nonce and testable criteria. For a simple, low-risk, authorized task the
Manager may itself perform the necessary bounded inspection and execution, using
available native inspection tools and preferring an indexed Codegraph query with
a plain document or configuration read as the fallback; a trivial routine task
needs no formal plan, task list, skill load, or CARE. The Manager also retains a
narrow, explicit operational-inspection authority for local Git queries,
delivery and candidate state, and reading only the active host configuration
binding used for registry bootstrap; it is not a general parallel-exploration
permission, never bypasses a native deny (including through Python or another
interpreter), and reports an unavailable capability rather than inventing one.
Before a potentially destructive local operation the Manager establishes the
exact workspace, environment, and database target and the documented reset
command; a known, explicitly authorized development target is executed with the
pertinent check, ambiguous, shared, or production targets stop dependent work and
ask one consequential question, and production or reset authorization is never
inferred from a development-scoped request. OpenCode read-only roles declare a
single scoped `external_directory` grant for the conventional
`~/.agents/skills/**` root (Explore additionally allows public `webfetch`); the
full home, provider configuration, and other external paths stay denied, and
custom roots require an explicit binding. Go and Pi project the same shared
Manager text; these declarations are configuration evidence, not runtime
sandbox proof.

A formal Markdown plan is required only for substantial work: a simple,
low-risk, bounded task needs no plan or task list. For substantial work the
shared contract requires a durable, in-repository
Markdown plan under `docs/implementations/`, with a stable plan identifier and
numbered tasks that own canonical status. The index, states and per-plan layout
documented in `docs/implementations/README.md` make the mechanism usable in a new
project without assuming this repository's files. A session task view is only an
operational projection: persist the plan before projecting it, consult the index
when starting or resuming, reconcile with repository evidence,
keep at most one active plan per session (zero when there is no work), and
preserve closed plans as history.
Resolve discoverable facts by inspection; ask consequential blocking decisions
before closing the plan or implementing any part that depends on them, continue
only with independent authorized work, and treat only minor reversible defaults
as explicit assumptions rather than turning a blocking decision into one. A plan
never overrides the user's instructions or authorization, stores no secrets or
raw logs, and grants no new permission. The shared text names no provider tool;
each adapter supplies the native projection (OpenCode and Pi `todowrite`, Codex a
native planning tool only when the host exposes one), and when that tool is
unavailable the work degrades honestly.
No runtime synchronizer or atomic cross-tool state is promised, and
implementation is not acceptance: a task completes only with its required
evidence, and a source change invalidates prior acceptance evidence.

Go embeds and validates the registry. Pi loads its generated JSON using
TypeScript/Node and rejects invalid identity, schema, role definitions and
content digest. Both implementations hash recursively sorted-key compact JSON
and render identical shared Manager/role text. Pi also checks its generated
Manager prompt against that shared content and its native adapter at startup.
This consistency check is not a cryptographic signature or host authorization.

## Provisioning and independent runtime

Regenerate and check from the repository root:

```sh
node packages/pi/scripts/generate-contract.mjs
node packages/pi/scripts/generate-contract.mjs --check
node packages/pi/scripts/verify-package.mjs
```

The generated resources are included in the standalone Pi package. Pi runtime
uses TypeScript/Node only: no Go binary, VGXNESS CLI or MCP is required. VGXNESS
can provision the package; Pi owns its conversation, models and tools. Memory
continues to use the existing shared SQLite database and migrations.

Pi derives worker roles, model aliases and write eligibility from the registry.
`vgx_skill` lists/reads managed SKILL.md files and selected relative resources;
`task.skills` requires the returned manifest SHA-256. Snapshots are bounded,
reject missing/drifted inputs, symlinks and traversal, and do not expand command
or target authority. Worker prompts include role instructions and the complete
bounded mission, without an active SDD phase workflow.

## Native compatibility

Codex Manager21 and OpenCode Manager62 render directly from the current registry
and native adapter metadata. Their local `vgxness/installation-receipt.json`
records provider, contract digest, managed paths and file hashes; Codex also
records its model plan. The provider anchors and rechecks the selected root.
The receipt is local integrity evidence, not a signature or protection against
a process running as the same user that edits both receipt and files.

Installers verify the exact receipt inventory and observed bytes before an
update. Current unreceipted files can bootstrap a receipt by exact regeneration.
Older unreceipted installations require the separate bridge procedure in
[Agent migration](../agent-template-migration.md). Configuration schema decoders
remain for model settings; they never reconstruct previous policy. The seven
active selections survive normalization of the former thirteen-role inventory.

OpenCode retains its transaction anchors and backup machinery. Codex retains
the actual predecessor under `vgxness/rollback/<package-sha256>/` before a
replacement; interruption reports the retained location. Existing pending
sidecars can reconstruct receipt-backed packages using exact local bytes.
Terminal readback follows CLI activation or transaction cleanup before success.
This is an observation, not a lock against subsequent same-user writes.
Unknown or modified content blocks mutation; missing old policy bytes cannot
be invented from a hash. Rollback data stays local and is never compiled into
the current binary.

Pi uses native SDK/RPC workers and host-provided model authentication. Windows
worker process ownership and worker continuation remain unsupported. Codex and
OpenCode retain their native delegation and configured memory transports;
the adapter does not imply that one host implements another host's features.

## Evidence and limits

The development corpus `internal/orchestration/testdata/manager-scenarios.json`
covers direct answers, research, implementation, skill selection/absence/drift,
retired SDD rejection, frozen verification, unauthorized delivery, memory closure and
unsupported capabilities. Go and Pi tests assert those declared obligations in
actual native projections. Separate executable Pi tests exercise tool schemas,
skill snapshots, complete worker mission prompts, SDK/RPC and startup hooks.

These are deterministic contract and integration checks, not measured live
model routing or behavioral equivalence. Protected holdouts are unchanged. A
future live-model evaluation requires independent host/model runs and traces.
Current projection digests and shared-contract tests bind the actual native
adapters. Synthetic predecessor tests exercise receipts without retaining
historical prompts. Validation must identify the exact tested candidate.

Tests creating private install roots need a restrictive process umask (for
example 077); a group-writable temporary root is correctly rejected by the
existing installer. Do not weaken artifact ownership checks to accommodate it.

SDD is retired. Previous agent registries and renderers are removed. Historical database records and published schema migrations remain intact. There is no SDD runtime, transport, or archive CLI command.
