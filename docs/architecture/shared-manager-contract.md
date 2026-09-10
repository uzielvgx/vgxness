# Shared Manager and native adapters

`internal/orchestration/manager_contract.json` is the maintained Manager and
worker policy source. Its versioned schema defines ordered Manager instructions,
six canonical worker roles, aliases, write authority, role instructions and
model-plan role identifiers. Provider adapters supply native tool names,
transport, permissions and model configuration. Each agent has exactly one maintained definition; historical prompt bodies and
reconstruction chains are removed from the source tree.

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
