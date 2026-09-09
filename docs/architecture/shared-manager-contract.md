# Shared Manager and native adapters

`internal/orchestration/manager_contract.json` is the maintained Manager and
worker policy source. Its versioned schema defines ordered Manager instructions,
twelve canonical worker roles, aliases, write authority, role instructions and
model-plan role identifiers. Provider adapters supply native tool names,
transport, permissions and model configuration. Historical prompt bodies are
frozen compatibility data; they do not define current behavior.

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
bounded mission, including accepted SDD bindings where required.

## Native compatibility

Codex Manager20 and its actual native profiles render shared policy while
retaining native model, tool and sandbox bindings. Complete Manager19 packages,
including lifecycle artifacts, remain recognized for all four plans.

OpenCode Manager61 and every current worker body use the shared registry.
The adapter preserves the native frontmatter, model and permission bindings
from the frozen native renderer. Complete Manager60 model-plan packages remain
recognized for schema versions 1, 2 and 3, including configuration-specific
assignments. Historical transforms operate on frozen predecessor definitions.
Ownership is established by complete bytes and manifests; mixed or modified
packages are rejected. Existing installer recovery and preservation remain in
charge of local artifacts; no replacement installer framework was added.

Pi uses native SDK/RPC workers and host-provided model authentication. Windows
worker process ownership and worker continuation remain unsupported. Codex and
OpenCode retain their native delegation and configured memory/SDD transports;
the adapter does not imply that one host implements another host's features.

## Evidence and limits

The development corpus `internal/orchestration/testdata/manager-scenarios.json`
covers direct answers, research, implementation, skill selection/absence/drift,
SDD acceptance, frozen verification, unauthorized delivery, memory closure and
unsupported capabilities. Go and Pi tests assert those declared obligations in
actual native projections. Separate executable Pi tests exercise tool schemas,
skill snapshots, complete worker mission prompts, SDK/RPC and startup hooks.

These are deterministic contract and integration checks, not measured live
model routing or behavioral equivalence. Protected holdouts are unchanged. A
future live-model evaluation requires independent host/model runs and traces.
Native package goldens preserve the previous complete artifacts; current
projection checks prevent an unused shared helper from masquerading as an
integrated adapter. Validation must identify the exact tested candidate.

Tests creating private install roots need a restrictive process umask (for
example 077); a group-writable temporary root is correctly rejected by the
existing installer. Do not weaken artifact ownership checks to accommodate it.
