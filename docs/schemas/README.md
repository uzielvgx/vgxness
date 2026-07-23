# VGXNESS Contract Schemas

These Draft 2020-12 schemas define runtime-neutral orchestration, execution, registries, run snapshots, and append-only events. Canonical writers emit `schemaVersion: "1"`; readers may accept only the legacy artifact forms documented below.

## Schema index

| Schema | File | Stable URI | Validates |
| --- | --- | --- | --- |
| Common library | `common.schema.json` | `https://vgxness.dev/schemas/common.schema.json` | Provider/artifact refs, provenance, skill sources, checksums, language policy, and `contract.invalid`. |
| OpenCode bridge input | `bridge.schema.json` | `https://vgxness.dev/schemas/bridge.schema.json` | Agent-controlled bounded dispatch and high-level orchestration arguments before trusted OpenCode context is added. |
| Orchestration library | `orchestration.schema.json` | `https://vgxness.dev/schemas/orchestration.schema.json` | Capability selection, routing, delegation requests/plans, execution waves, provider-neutral memory SDD preflight, questions, and answers. |
| Execution library | `execution.schema.json` | `https://vgxness.dev/schemas/execution.schema.json` | Packets, capsules, bounded loops, cancellation, background tasks, exact skill refs, and agent results. |
| Prompt library | `prompts.schema.json` | `https://vgxness.dev/schemas/prompts.schema.json` | Versioned system-prompt identities and bounded prompt receipts. |
| Current run | `current-run.schema.json` | `https://vgxness.dev/schemas/current-run.schema.json` | Lightweight active-run projection and correlated IDs. |
| Run snapshot | `run.schema.json` | `https://vgxness.dev/schemas/run.schema.json` | Authoritative run record and composed durable contracts. |
| Run event | `run-event.schema.json` | `https://vgxness.dev/schemas/run-event.schema.json` | One append-only JSONL event object. |
| Skills registry | `skills.schema.json` | `https://vgxness.dev/schemas/skills.schema.json` | Exact, versioned, provenanced skill entries and scopes. |
| Agents registry | `agents.schema.json` | `https://vgxness.dev/schemas/agents.schema.json` | Provider, capability, execution, permission, skill, and provenance contracts. |
| Delivery manifest | `delivery-manifest.schema.json` | `https://vgxness.dev/schemas/delivery-manifest.schema.json` | Exact policy/prompt/registry/provider/model identities, focused check evidence, and bounded review verdict. |
| Delivery receipt | `delivery-receipt.schema.json` | `https://vgxness.dev/schemas/delivery-receipt.schema.json` | Immutable content-bound target, manifest bindings, and issued review receipt. |
| Current delivery receipt | `delivery-current.schema.json` | `https://vgxness.dev/schemas/delivery-current.schema.json` | Atomic active/invalidated receipt pointer and receipt-file digest. |

Storage schemas apply equally under project-local `.vgxness/` and user-global `~/.vgxness/projects/<project-id>/` roots.

## Use a stable fragment

The four libraries expose reusable contracts through `$defs`. Reference the exact fragment instead of copying a shape:

| Contract | Reference |
| --- | --- |
| Canonical artifact | `common.schema.json#/$defs/artifactReference` |
| Canonical-or-legacy artifact input | `common.schema.json#/$defs/artifactReferenceInput` |
| Provenance | `common.schema.json#/$defs/provenance` |
| Language policy | `common.schema.json#/$defs/languagePolicy` |
| Contract failure | `common.schema.json#/$defs/contractError` |
| Capability declaration | `orchestration.schema.json#/$defs/capabilityDeclaration` |
| Provider selection | `orchestration.schema.json#/$defs/providerSelection` |
| Routing decision | `orchestration.schema.json#/$defs/routingDecision` |
| Delegation request and validated plan | `orchestration.schema.json#/$defs/delegationRequest` and `#/$defs/delegationPlan` |
| Planned task and execution wave | `orchestration.schema.json#/$defs/delegationTask` and `#/$defs/executionWave` |
| Native task join and aggregate result | `orchestration.schema.json#/$defs/joinedTask` and `#/$defs/delegationJoin` |
| SDD preflight | `orchestration.schema.json#/$defs/sddPreflight` |
| Structured question/answer | `orchestration.schema.json#/$defs/structuredQuestion` and `#/$defs/structuredAnswer` |
| Exact skill ref | `execution.schema.json#/$defs/exactSkillReference` |
| Context/execution packet | `execution.schema.json#/$defs/contextPacket` and `#/$defs/executionPacket` |
| Loop/background/cancellation | `execution.schema.json#/$defs/loopControl`, `#/$defs/backgroundTask`, and `#/$defs/cancellation` |
| Agent result | `execution.schema.json#/$defs/agentResult` |
| Continuity capsule | `execution.schema.json#/$defs/continuityCapsule` |
| System prompt and exact reference | `prompts.schema.json#/$defs/prompt` and `#/$defs/promptReference` |
| Bounded dispatch input | `bridge.schema.json#/$defs/dispatchInput` |
| High-level orchestration input | `bridge.schema.json#/$defs/orchestrateInput` |

Relative references resolve against each schema's stable `$id`. A schema loader should register every stable URI before validating composed schemas.

`joinedTask.dispatchStatus` is required and preserves the authority's `confirmed`, `not_started`, or `uncertain` classification. A consumer must not infer confirmed native execution merely from a terminal task status. The Go scheduler and file-backed production authority validate this shape, persist admission/replay/terminal state, and linearize publication through atomic `AcceptJoin`; JSON Schema alone does not provide those runtime guarantees.

## Validation order

Validate at four boundaries:

1. **Registry and configuration ingestion** — reject unknown fields, unresolved sources, missing provenance, unsupported versions, and out-of-scope skills.
2. **Before delegation** — validate selection, routing, preflight, question/answer correlation, packet shape, exact skills, permissions, and loop budget.
3. **Before append/write** — validate the event or snapshot before mutating durable state; read the write back and compare key IDs.
4. **On recovery/readback** — validate each JSONL object independently, then compare snapshot, current projection, events, artifacts, results, and capsules.

Delivery gates additionally recompute the candidate Git tree and every manifest binding. A mismatch atomically invalidates the current receipt; validation never issues a replacement receipt or starts another review.

Schema failure returns `contract.invalid` with the schema URI, failing JSON Pointer, message, and recoverability. It performs no delegation or state advance and preserves the last valid state.

A Draft 2020-12 validator is required for release verification. JSON parsing and ad hoc structural checks can prove syntax and local reference resolution, but they do not replace meta-schema, format, conditional, or complete vocabulary validation.

## Semantic checks after schema validation

JSON Schema proves record shape. The manager or run-store service must additionally enforce:

- selected provider is eligible and satisfies every required capability/version/constraint;
- routing candidate, selected agent, policy, override, and SDD decision agree;
- answer ID, shape, and bounded choice correlate to the originating question;
- artifact and skill provider references resolve and checksums match when present;
- skill scope permits the target agent and dispatch context;
- foreground advancement remains sequential and manager-owned;
- background work is read-only, non-delegating, non-advancing, and independently cancelable;
- current iteration does not exceed the loop budget and exhausted/deadline loops are terminal;
- current-run, snapshot, event, task, decision, cancellation, result, capsule, and artifact IDs agree;
- lifecycle transitions and event order are legal;
- each dependency admitted into a later wave has a current authority checkpoint showing `completed` with `dispatchStatus: confirmed`;
- a published delegation join is the authoritative snapshot returned by the same owner/epoch authority transition that validated its revocation-aware terminal checkpoint.

## Compatibility

Canonical writers always emit provider-neutral objects with `schemaVersion: "1"`.

Owned semantic-memory records use backend `memory` and retain provenance. Legacy backend `engram` is invalid for owned memory; external references may still identify Engram with `provider: "engram"`. No Engram import or compatibility adapter is implied.

Readers may temporarily accept only these deprecated artifact alternatives through `common.schema.json#/$defs/artifactReferenceInput`:

1. A non-empty legacy artifact path string.
2. The previous path-required object with `id`, `kind`, `path`, `backend`, and `writtenAt`.

A future reader normalizes either form to a filesystem-provider artifact reference and marks it deprecated. There is no runtime migration branch in this documentation/schema change. Updated examples use canonical objects. Memory artifacts require provider, ID, artifact type, and provenance—not a filesystem path.

Skill delegation has no bare-name, alias, version-range, or bare-path compatibility form. It requires exact identity, exact version, source, provenance, and an allowed `user`, `project`, `runtime`, or `shared` scope.

## Documentation examples

Every `json` or `jsonl` fence in `README.md` and `docs/**/*.md` must have a nearby `<!-- schema: <URI-or-fragment> -->` marker. Validation parses every JSON block, validates each JSONL line independently, and reports the source file and pointer on failure.

Current examples map to:

| Document example | Schema |
| --- | --- |
| `current-run.json` | `current-run.schema.json` |
| Memory artifact reference | `common.schema.json#/$defs/artifactReference` |
| Operational JSONL events | `run-event.schema.json` |
| OpenCode bounded dispatch input | `bridge.schema.json#/$defs/dispatchInput` |

## Why events use JSONL

Events are append-only operational facts. One object per line avoids rewriting a large document after every transition and preserves earlier complete events if a final line is interrupted. A partial final line is reported or ignored by explicit recovery policy; it is never silently interpreted as a valid event.

## Verification checklist

- [ ] Every schema and documentation example parses as JSON.
- [ ] Every schema declares Draft 2020-12 and a stable `https://vgxness.dev/schemas/` ID.
- [ ] Every local `$ref` and JSON Pointer resolves.
- [ ] Valid canonical and documented legacy fixtures pass.
- [ ] Invalid fixtures fail at their expected pointers.
- [ ] Semantic decision-table fixtures cover capability mismatch, invalid answers, background mutation/delegation, loop exhaustion, and inconsistent state IDs.
- [ ] A real Draft 2020-12 validator checks meta-schema, formats, composition, and conditionals before release.
- [ ] Unknown top-level fields fail unless a contract explicitly defines extension data.
