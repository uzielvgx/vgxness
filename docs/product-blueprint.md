# VGXNESS Product Blueprint

> **Blueprint version:** 1.0  
> **Document status:** Current  
> **Authority:** Canonical English product blueprint  
> **Companion:** [Español — traducción no canónica](product-blueprint.es.md)

This document is the sole canonical source for the VGXNESS product vision, vocabulary, boundaries, and roadmap. Supporting documents describe narrower contracts and workflows; they do not redefine this blueprint. If the Spanish companion differs, this English version controls.

Capability delivery labels have precise, evidence-backed meanings:

- **Implemented** — delivered behavior is present and verifiable in the current repository candidate. Publication still requires the normal review and merge gates.
- **Partial** — a bounded subset is delivered; the named lifecycle or service is not complete.
- **Contracts-only** — schemas or semantic rules exist, but they do not provide the runtime behavior they describe.
- **Planned** — approved product direction has no delivered runtime behavior yet.

**Deferred** and **Non-goal** remain scope labels, not delivery claims. Deferred work is approved but outside the current horizon; a non-goal requires a new scope decision before entering the roadmap. “Current” describes this document's freshness only.

## 1. Purpose and audience

VGXNESS is for developers who want AI-assisted work to remain understandable, bounded, and recoverable. The current candidate contains a working local control-plane foundation for exact identities, policy, Chronicle evidence, bounded OpenCode execution, semantic memory, installation, and recovery-aware operation. The broader Navigator/SDD product is still being built.

**Implemented:** The repository builds a `vgxness` binary with application wiring, a permanent versioned launcher with atomic activation and rollback, storage-root resolution, `status`/`doctor`, owned SQLite/FTS5 memory, runtime contract validation, Chronicle event/snapshot/task-state services, Registry, Gatekeeper, prompt composition, bounded coordination, OpenCode provider execution, a native-first OpenCode manager/reviewer projection, a bounded memory-only OpenCode plugin, guided setup, focused tests, and Go CI. The legacy execution bridge CLI remains available for compatibility but is not projected through the plugin.

**Implemented:** Chronicle appends and verifies JSONL events, derives task state, publishes active snapshots through immutable SHA-256 files plus one atomic pointer replacement, and recovers an interrupted terminal-pointer removal. **Partial:** its recovery scope remains narrower than the future general checkpoint/artifact lifecycle.

**Contracts-only:** Independently authored Draft 2020-12 schemas for future SDD, routing, artifact, and continuity paths define records that the current bounded runtime does not yet exercise. Schemas used by the current control plane are validated at runtime.

**Implemented/Partial:** The bounded control-plane, orchestration, isolated-edit, validation, and Delivery Authority services remain implemented as a legacy CLI compatibility surface. They are not projected into the native OpenCode manager. The interactive path now uses direct OpenCode tools, native Task delegation, native skills by name, optional SDD, and evidence-based review profiles. Final removal or redesign of the legacy services, complete SDD artifacts, automatic delivery wiring, richer autonomy UX, keyboard TUI, additional adapters, and advanced semantic retrieval remain planned.

### Document and translation contract

- English is the decision source and resolves ambiguity.
- The Spanish companion is complete for navigation and comprehension but explicitly non-canonical.
- Both documents carry the same blueprint version, status model, numbered-section inventory, decisions, gates, roadmap meanings, and traceability topics.
- A material canonical edit must update both documents atomically. A mismatch blocks publication.
- Supporting documents link here instead of maintaining competing roadmaps.

## 2. Status at a glance

| Status | Scope |
| --- | --- |
| **Implemented** | Go binary/composition; launcher and self-install/update/rollback; storage and owned SQLite/FTS5 memory; runtime contract validation; Chronicle events, immutable crash-atomic snapshot publication, task-state and terminal recovery; Registry/Gatekeeper; prompt/provider runner; bounded coordinator/control plane; native OpenCode manager/review projection with a memory-only plugin; guided setup; hermetic clean-checkout setup/dispatch E2E; tests and Go CI. |
| **Partial** | Navigator's manager-facing native planner/wave/join runtime is bounded to supported operations and OpenCode; Delivery Authority has explicit receipt/gate CLI but no automatic delivery integration; Chronicle's broader plan/checkpoint/artifact continuity and native Windows runtime/distribution smoke remain incomplete. |
| **Contracts-only** | Draft 2020-12 shapes and semantic rules for future SDD, routing, artifacts, and continuity paths not yet exercised by the bounded runtime. |
| **Planned** | Navigator routing, complete SDD workflows and artifact backends, richer autonomy/approval UX, keyboard-first TUI, optional adapters, native Windows validation, and advanced semantic retrieval/lifecycle. |
| **Deferred** | Additional runtime adapters, optional local MCP exposure, broader external MCP clients, richer semantic retrieval, and graphical product surfaces. |
| **Non-goal** | Copied third-party artifacts, silent destructive autonomy, hidden operational truth, runtime or tool lock-in, Engram integration, unbounded agent loops, automatic prototype-to-production promotion, or a UI that owns business policy. |

**Evidence boundary:** The implemented runtime includes the task state machine, Gatekeeper/Registry services, OpenCode provider execution, bounded coordination, a headless binary self-installer, and the native manager/reviewer projection. The installed VGXNESS OpenCode plugin exposes only owned-memory search, get, save, and explicit forget; bridge commands remain in the CLI only as legacy compatibility behavior. No setup TUI, product-owned MCP server/client, Engram integration, Git automation, additional runtime adapter, or silent product configuration mutation is delivered.

**Contracts-only limitation:** The schemas under [`schemas/`](schemas/README.md) mix delivered runtime contracts with future and partial behaviors. Runtime validation is evidence only for the schemas and paths actually invoked; `$schema` declarations alone do not prove complete product enforcement.

## 3. Product principles

1. **The human leads.** The user owns goals, scope, and approval of consequential actions.
2. **Teach, do not obscure.** Explain important facts, recommendations, tradeoffs, and uncertainty.
3. **Verify before agreeing.** Claims about code, tools, state, and completion require evidence.
4. **Coordinate through boundaries.** Orchestration, execution, review, storage, skills, permissions, and adapters remain separable.
5. **Keep state inspectable.** Chronicle records operational truth; semantic memory preserves durable meaning.
6. **Use the fastest sufficient path.** Simple work stays direct; one bounded concern defaults to one task; independent evidence overlaps only when it saves time; complexity and risk receive proportionate planning and validation.
7. **Fail closed at unsafe boundaries.** Missing capabilities, invalid contracts, stale adapters, or absent approvals stop advancement.
8. **Preserve continuity, not transcript bulk.** Pass thin packets and durable references rather than entire conversations.
9. **Design for recovery.** Every bounded operation has visible failure, cancellation, and a safe next action.
10. **Keep adapters thin and optional.** External tools translate capabilities without owning VGXNESS policy.
11. **Default technical artifacts to English.** Explicit user or project policy may authorize another artifact language.
12. **Protect review attention.** Work units are independently understandable, verifiable, and reversible.

## 4. Clean-room boundary

VGXNESS may study systems such as Gentle AI, Engram, OpenCode, CodeGraph, OpenPencil, and other agent runtimes or tools to understand capability categories and interoperability needs. Comparable outcomes do not permit copied implementation.

| Boundary | Decision |
| --- | --- |
| **Planned capability parity** | Independently provide useful orchestration, memory, skill, structural-analysis, design, review, recovery, and delivery outcomes. |
| **Implemented authorship rule** | Product prose and schemas in this repository are VGXNESS-owned contracts and remain independently authored. |
| **Non-goal: copied artifacts** | Do not copy third-party code, prompts, schemas, names, skills, manifests, database layouts, or exact workflows. |
| **Non-goal: disguised coupling** | Do not make a third-party runtime, tool, or store's private behavior the VGXNESS domain model. |
| **Planned interoperability** | Integrate through documented adapters, capability contracts, stable references, and explicit provenance. |

External systems remain optional inspiration or adapters, never the source of VGXNESS identity. Clean-room design also applies when an adapter is preferred: preference cannot become a hidden dependency.

## 5. Product vocabulary

VGXNESS uses four distinct concept groups. They must not be collapsed into a single list of “agents.”

| Group | Meaning | Members |
| --- | --- | --- |
| Product capabilities | LLM-backed roles that provide a bounded kind of reasoning or work. | Navigator, Scout, Blueprint, Forge, Sentinel, optional Challenger. |
| Deterministic services | Code-owned policy and persistence boundaries; they do not improvise decisions. | Registry, Chronicle, Gatekeeper. |
| Operating modes | Named responsibilities used while a capability performs SDD or recovery work. | explore, propose, spec, design, tasks, apply, verify, archive, fix, recovery. |
| Adapters | Replaceable translations between the control plane and external runtimes, tools, protocols, or stores. | OpenCode, CodeGraph, OpenPencil, MCP, and future adapters. |

**Planned:** Capability names describe stable product responsibilities, not one process per name and not provider-specific prompt files. `sdd-design` is a Blueprint operating mode, `sdd-apply` is a Forge operating mode, and `sdd-verify` is a Sentinel operating mode.

**Contracts-only:** Existing schema terms remain normative for machine-readable records. This blueprint owns the human-facing taxonomy, not schema field definitions.

The delivery split is intentional: owned memory, Registry, Gatekeeper, contract-validated bounded execution, crash-atomic Chronicle snapshot publication, and the OpenCode provider path are **Implemented**; Chronicle remains **Partial** only for the broader checkpoint/artifact continuity model; the named product capabilities and complete SDD operating modes remain **Planned**.

## 6. System model

VGXNESS has an **Implemented, bounded** local Go control plane with explicit package and dependency boundaries. Navigator's contract and deterministic scheduling foundation is **Partial**; runtime routing and the complete SDD product remain **Planned**. The Go architecture is detailed in [`go-implementation.md`](go-implementation.md).

```text
planned TUI / implemented CLI + native OpenCode projection
                         |
 implemented composition, storage, contracts, Chronicle
                         |
 implemented Registry/Gatekeeper + bounded coordinator
                         |
       implemented OpenCode provider execution
                         |
 implemented MemoryStore + bounded OpenCode memory projection
```

| Boundary | Status and responsibility |
| --- | --- |
| Go binary and composition | **Implemented:** Build a local binary and wire configuration, inspection, memory, Chronicle, Registry/Gatekeeper, providers, coordinator, setup, and bridge commands. |
| CLI, self-installation, and storage roots | **Implemented:** Install immutable binary versions behind a permanent launcher, activate updates atomically, roll back one level, resolve project/user storage, and provide status, doctor, memory, guided setup, integration, and bounded bridge commands. |
| Owned MemoryStore | **Implemented:** SQLite/FTS5 semantic storage, migrations, lifecycle fields, filtered search, provenance-backed records, bounded automatic retrieval, and governed agent-proposed candidates with contradiction review. |
| Chronicle | **Implemented/Partial:** Implements strict current-run reading, verified JSONL events, immutable content-addressed active snapshots, atomic pointer commits, terminal repair, task-state replay, cancellation evidence, and bounded recovery reconstruction. General checkpoint/artifact continuity remains planned. |
| Schemas and semantic rules | **Implemented/Contracts-only:** Current packets, events, snapshots, registry records, prompts, and results are validated at runtime; future SDD/continuity shapes remain contracts-only. |
| Keyboard-first TUI | **Planned:** Provide setup and focused interaction without owning installation or orchestration policy. |
| Bounded coordination | **Implemented, narrow:** Exact Registry/Gatekeeper policy, provider selection, finite foreground/background coordination, cancellation, receipts, and bounded status/read/write/review operations are delivered. Navigator/SDD routing remains planned. |
| OpenCode adapter | **Implemented:** Hardened runtime adapter, one native-first `vgxness-manager`, five read-only review profiles, native skill-name routing, one bounded owned-memory plugin, and confirmation-gated explanatory setup are delivered without a fixed child model or execution bridge. |
| CodeGraph adapter | **Implemented, optional:** The manager and reviewers may use the read-only `codegraph_explore` MCP tool for bounded structural evidence when a healthy project index exists; exact source and diff inspection remain authoritative fallbacks. |
| OpenPencil adapter | **Planned, optional:** Design and prototyping path; artifacts remain proposals until separately implemented and verified. |
| Other runtime/MCP adapters | **Deferred:** Additional integrations may be added without changing core contracts. |

**Non-goal:** No adapter may bypass Gatekeeper, redefine taxonomy, become operational truth, or embed policy that belongs in the control plane.

## 7. User experience

### Setup wizard

**Implemented, headless:** The OpenCode wizard detects prerequisites and paths, explains all six steps and their limits, shows proposed changes, requests approval, installs through application services, reads results back, verifies the live handshake, and reports bounded rollback or repair guidance. A richer keyboard-first TUI and optional-adapter setup remain planned as separate extensions.

The wizard may detect OpenCode, CodeGraph, and OpenPencil. It may offer to install an absent optional adapter only after disclosing source, version, command, destination, network use, configuration changes, and rollback. Declining an optional adapter preserves a supported fallback. Detection never authorizes installation or initialization.

**Non-goal:** Setup will not silently install packages, initialize repositories, mutate configuration, overwrite files, or claim success without readback evidence.

### Navigator interaction

**Implemented/Partial:** The OpenCode manager matches the user's language and selects direct inline work, bounded native Task delegation, or optional user-approved SDD. It loads applicable skills through OpenCode's native registry, may consult bounded CodeGraph evidence, performs edits and validation itself, and uses the five read-only review profiles against a frozen candidate. Complete durable SDD artifact routing remains planned.

### Autonomy profiles

Profiles configure interruption level; they do not remove hard safety boundaries.

| Profile | Ordinary scoped work | Approval posture |
| --- | --- | --- |
| **Safe** | Reads and analysis proceed; edits, tests, and local commands usually ask. | Most cautious; appropriate for unfamiliar or regulated environments. |
| **Balanced (default)** | Scoped reads, edits, focused tests, and non-destructive local commands proceed without repeated prompts. | Consequential operations always ask; ambiguous risk asks once. |
| **Autonomous** | Broader pre-approved ordinary work may proceed within explicit roots, tools, and risk ceilings. | Hard gates remain; the profile cannot authorize them implicitly. |
| **Custom** | The user defines policy from named capabilities and limits. | Invalid, incomplete, or self-expanding policy fails closed. |

Approval is always required for destructive file actions; Git history, remotes, commits, pushes, releases, or PRs; package installation; network or external side effects; secrets; global/runtime configuration mutation; and permission expansion.

### Capability leases

A capability lease is an explicit, revocable grant for one work unit. It records the approving identity and wording, allowed roots and tools, operation classes, risk ceiling, deadline, and correlation ID. It uses least privilege, expires on completion, cancellation, deadline, or context change, and cannot grant or renew itself. Exceeding scope is denied or requires fresh approval. Every use is attributable in Chronicle.

### Approval decision path

For each proposed operation, Gatekeeper evaluates the same ordered questions:

1. Is the operation inside the active work unit and allowed roots?
2. Does the selected capability, adapter, and tool declare the required operation?
3. Does the autonomy profile allow this ordinary operation without interruption?
4. Is there a valid lease when policy requires one?
5. Does a hard gate require fresh human approval regardless of profile or lease?

A denial includes the failed condition and the smallest safe next action. VGXNESS does not turn repeated denials into implicit consent, and prior approval for one work unit is not precedent for another.

## 8. Routing and SDD

**Planned:** Navigator classifies each request into the smallest adequate route and persists an explainable decision.

| Route | Use |
| --- | --- |
| `direct` | Answer or perform a small, low-risk action without a multi-phase workflow. |
| `explore` | Investigate unknowns, current state, constraints, or feasibility. |
| `plan` | Classifier route for a bounded approach when implementation is not authorized or full SDD is unnecessary. It is not the SDD `tasks` mode. |
| `sdd` | Run proposal, spec, design, `tasks`, apply, verify, and archive modes as required by risk and policy. |
| `recovery` | Reconcile interrupted, inconsistent, blocked, or failed work from durable evidence. |

The `tasks` operating mode belongs inside SDD and converts approved requirements and design into implementation work units. A `plan` route may end with advice or a lightweight breakdown and does not imply approved SDD artifacts.

### Preflight and artifact stores

**Planned:** Automatic preflight uses policy and repository evidence and asks only on consequential ambiguity. Interactive preflight explains the meaningful choice before phase work.

**Contracts-only:** The owned-backend contract uses `memory`. Engram is not a runtime backend or planned adapter. No memory-backend schema migration remains as an implementation prerequisite. Runtime preflight resolution and SDD artifact persistence are still planned.

| User-facing store | Contract mapping | Delivery and behavior |
| --- | --- | --- |
| `memory` | `memory` → configured `MemoryStore` | **Contracts-only for SDD artifacts:** use immutable owned-memory references; the general MemoryStore is implemented, but SDD artifact-store integration is not. |
| `openspec` | `openspec` | **Contracts-only:** persist artifacts in the repository's OpenSpec structure when runtime preflight is implemented. |
| `both` | `hybrid` | **Contracts-only:** keep memory and filesystem artifacts synchronized when runtime preflight is implemented. |
| disabled | `none` | **Contracts-only:** perform no SDD artifact access when policy permits skipping it. |

Phase order is evidence-driven rather than ceremonial. A phase may be skipped only when its required artifact or decision exists and remains valid. Apply follows approved requirements and design; verify proves the result; archive closes and synchronizes final state.

## 9. Context and persistence

### Thin context

**Partial:** The bounded provider path constructs and enforces thin execution packets with goals, scope, allowed paths/tools, exact skill references, acceptance criteria, approval state, and return contracts. General Navigator/SDD continuity capsules and artifact fetching remain **Planned**.

### Semantic authority and operational truth

| Concern | Status and owner |
| --- | --- |
| Chronicle | **Implemented/Partial:** Reads the active pointer; validates, appends, rolls back, and rereads JSONL events; derives task/cancellation state; publishes active snapshots atomically through immutable content-addressed files; and repairs interrupted terminal publication. General checkpoints and richer artifact recovery remain planned. |
| VGXNESS MemoryStore | **Implemented:** Semantic authority for typed durable observations with identity, scope, topic, provenance, lifecycle state, references, save/search/get, session metadata, automatic task retrieval/capture, and governed `memoryCandidates`. Conflicting candidate updates transition to `needs_review`; broader human review UX remains planned. |
| SQLite/FTS5 | **Implemented:** One default user database, canonical workspace bindings, project-isolated records, four migrations, safe legacy imports, deterministic filtering, and lexical retrieval behind `MemoryStore`; richer semantic indexing remains planned. |
| Engram integration | **Non-goal:** VGXNESS does not install, invoke, import from, or depend on Engram. |
| Project/user roots | **Implemented:** Resolve project-local `.vgxness/` or user-global `~/.vgxness/projects/<project-id>/` operational storage; default semantic memory is shared at `~/.vgxness/memory.db`. |

Memory entries carry stable IDs, type, topic, content, provenance, timestamps, scope, lifecycle state, and references. Search starts with deterministic filters and FTS5; summaries and embeddings may supplement retrieval later without replacing source records.

Chronicle and semantic memory may cross-reference each other but never substitute for each other. If semantic context conflicts with an event, receipt, or execution state, Chronicle controls the operational decision and the inconsistency is surfaced. The owned `MemoryStore` is the sole semantic-memory authority.

### Semantic-memory lifecycle

| Stage | Owned-store behavior |
| --- | --- |
| Capture | **Implemented:** Accept a typed durable observation with source, scope, and evidence; every terminal bounded task also writes one idempotent result observation linked to the observations it used. |
| Normalize | **Implemented:** Assign stable identity, topic, timestamps, lifecycle metadata, and source references. |
| Retrieve | **Implemented:** Apply deterministic filters before FTS5 ranking, hydrate at most three records into a fixed execution-context budget, and return provenance. |
| Compare | **Planned:** Preserve compatible, related, scoped, conflicting, or superseding relationships without deleting history silently. |
| Review | **Planned:** Surface stale or review-due knowledge before it is trusted as current fact. |
| Summarize | **Planned:** Create derived summaries that reference source entries rather than replacing them. |
| Import | **Non-goal for Engram:** The active runtime does not probe, import, or synchronize Engram data. |

Retention, redaction, export, backup, and migration remain explicit application services. Memory writes respect project/user scope and secret policy. Deleting or rewriting durable knowledge is consequential and cannot be smuggled through an ordinary edit lease.

### Recovery and authority conflicts

**Implemented/Partial:** Chronicle recovery reconstructs bounded operational state from the current pointer, its digest-bound immutable run snapshot, validated event history, task results, and cancellations. It completes a staged terminal snapshot after an interrupted pointer removal and fails closed on digest or authority inconsistencies. General checkpoints/artifacts and semantic-context recovery remain planned. Semantic memory never repairs or advances a run by itself.

## 10. Capabilities, services, and optional adapters

### Product capabilities

| Capability | Planned responsibility | Hard boundary |
| --- | --- | --- |
| Navigator | Coordinate interaction, teaching, intent, routing, approvals, capability selection, and summary. | Does not override Gatekeeper or silently absorb substantial specialist work. |
| Scout | Inspect code, documents, tools, prior decisions, and unknowns; return sourced findings. | Read-only unless a separately approved mode grants narrow writes. |
| Blueprint | Produce proposals, requirements, scenarios, designs, prototypes, tasks, and work units. | Does not claim plans or prototypes are implemented. |
| Forge | Implement or correct an approved work unit with focused evidence and rollback boundaries. | Writes only inside allowed roots and cannot approve itself. |
| Sentinel | Verify requirements, contracts, tests, scope, safety, resilience, readability, and design evidence. | Does not silently rewrite what it judges. |
| Challenger | Optionally perform adversarial review for consequential claims. | Advisory; cannot expand scope, approve, or replace acceptance evidence. |

### Deterministic services

| Service | Planned responsibility | Hard boundary |
| --- | --- | --- |
| Registry | **Implemented, bounded:** Resolve exact agents, skills, prompts, providers, versions, sources, provenance, checksums, capabilities, permissions, and scopes used by the current runtime. | Rejects unresolved, ambiguous, stale, or out-of-scope references. |
| Chronicle | **Implemented/Partial:** Record and validate events, digest-bound snapshots, task/cancellation state, atomic active pointers, terminal repair, and recovery projections. Broader checkpoint/artifact continuity remains open. | Never invents missing state or becomes semantic memory. |
| Gatekeeper | **Implemented, bounded:** Enforce adapter eligibility/health, operations, capabilities, permissions, roots/tools, risk ceilings, leases, approvals, and task transitions on the provider path. | Fails closed and never asks an LLM to improvise policy. |

### CodeGraph structural-intelligence adapter

CodeGraph is Scout, Blueprint, and the route classifier's preferred optional adapter for repository maps, symbols, references, call paths, dependencies, and blast-radius evidence. Registry detects availability and version; Gatekeeper validates the project root and lease. Initialization is lazy, limited to real projects, and creates a separate index per worktree. Queries record adapter/version, root, index freshness, and fallback provenance.

Missing, declined, invalid, or stale CodeGraph never blocks supported work by itself. VGXNESS discloses the condition and falls back to bounded filesystem inspection. It does not silently install, initialize, reindex, or reuse another worktree's index. The setup wizard may offer installation and validation under the approval contract in section 7.

### OpenPencil design and prototyping adapter

OpenPencil is an optional Blueprint and Sentinel adapter for design briefs, flows, wireframes, prototypes, token/layout inspection, and design evidence. Supported integration may be live-editor, headless, CLI, or MCP, selected through declared capabilities rather than assumptions.

Gatekeeper restricts allowed design-file roots, exports, network use, and writes. Registry records tool/version and artifact provenance. Sentinel reviews accessibility, consistency, tokens, layout, responsive intent, interaction states, and traceability to requirements. An approved prototype is not production code: Forge needs separate implementation scope, approval, tests, and verification. The wizard may offer disclosed, approval-gated installation; absence preserves a text/design-spec fallback.

### Common adapter health contract

| Check | Required evidence |
| --- | --- |
| Detection | Executable/service identity, version, source, and supported interface. |
| Eligibility | Required capabilities, policy compatibility, allowed roots, and active lease. |
| Freshness | Index, document, or session state appropriate to the current worktree and task. |
| Invocation | Exact adapter mode, bounded inputs, provenance, and correlation to the work unit. |
| Readback | Structured result or artifact exists and can be inspected independently. |
| Fallback | A disclosed supported path when the optional adapter is declined or unhealthy. |

Adapter failure is categorized as unavailable, incompatible, stale, permission-denied, invalid-result, or interrupted. Optional-adapter failure does not become permission to install, mutate, or widen scope automatically.

## 11. Delivery safeguards and background agents

### Skills, permissions, and provenance

**Partial:** Registry resolves exact skill identity, version, source, provenance, checksum, and allowed scope before bounded dispatch. A complete first-party skill authoring/lifecycle service and Navigator-driven selection remain planned; memory paraphrases never become authority.

Gatekeeper evaluates active profile, capability lease, adapter health, allowed roots, and operation risk before execution. A profile cannot bypass a hard gate, and an optional adapter cannot broaden the underlying work unit.

### Review, design, and delivery

Every work unit records a focused validation result, a real runtime scenario when one exists, and an exact rollback boundary. Git/worktree automation is explicit and approval-gated. Review budgets trigger autonomous work-unit boundaries or a recorded size exception. Chained delivery preserves focused diffs, correct targets, independent verification, and safe rollback.

Sentinel review lenses include risk, reliability, resilience, readability, and—when design artifacts exist—accessibility, consistency, tokens, layout, states, and requirement traceability. Correction and adversarial loops have finite attempt and evidence budgets.

### Work-unit evidence

| Evidence | Requirement |
| --- | --- |
| Focused validation | Record the smallest command or deterministic check that proves the unit and its exact result. |
| Runtime scenario | Exercise a real integration boundary when one exists; otherwise state why it is not applicable. |
| Scope proof | List changed paths, approvals used, and any generated or external effects. |
| Rollback boundary | Name the exact files, state, and behavior removable without reverting unrelated work. |
| Review identity | Correlate requirements, implementation or documentation, findings, and final verdict. |

Evidence belongs to the same work unit as the change. A passing broad suite does not replace a failed focused check, and a size exception does not waive correctness, bilingual parity, or safety gates.

### Background-agent supervision

Navigator may start concurrent work only when safe. Every background task is manager-owned, tied to one run and purpose, independently cancelable, deadline/iteration bounded, read-only, non-delegating, unable to approve or advance the foreground run, and advisory until validated and deliberately incorporated.

**Non-goal:** No agent may hide failure, run indefinitely, widen authority, approve its own risky action, or convert advisory output into accepted state automatically.

## 12. Roadmap and deferred scope

### Delivery horizons

The dependency sequence through **adaptive native delegation → content-bound Delivery Authority receipts** is implemented for the bounded OpenCode control-plane path. The next product sequence is **automatic delivery integration → broader checkpoint/artifact continuity**. Native Windows runtime/distribution smoke is deferred and does not block this sequence.

| Horizon | Status | Outcome |
| --- | --- | --- |
| Local product foundation | **Implemented** | Go composition, versioned self-install/update/rollback, storage resolution, owned SQLite/FTS5 memory, CLI/bridge, status/doctor, tests, and CI. |
| Runtime contract foundation | **Implemented/Contracts-only** | Validate contracts used by the bounded runtime; retain future SDD/continuity shapes as contracts-only until their paths exist. |
| Chronicle operational state | **Implemented/Partial** | Deliver verified events, task/cancellation replay, digest-bound snapshots, atomic pointer publication, terminal repair, and recovery projection; expand general checkpoint/artifact continuity. |
| Bounded orchestration foundation | **Implemented/Partial** | Registry/Gatekeeper, prompt composition, provider selection, OpenCode execution, finite coordination, strict bridge operations, a native Navigator, deterministic waves, durable prerequisite-gated admissions, logical-slot/epoch fencing, checkpoint adoption, plan/result storage, recovery controls, and authority-accepted joins are delivered. Delivery receipts and full SDD/Chronicle artifacts remain planned. |
| Structural and design adapters | **Planned** | Add optional CodeGraph and OpenPencil detection, wizard installation, provenance, safe fallbacks, and focused Sentinel validation. |
| Safe delivery | **Partial** | Bounded work units, policy receipts, cancellation, read-only background constraints, strict review evidence, setup rollback, and recovery checks exist; broader skill lifecycle, Git/worktree automation, and product-level review budgets remain planned. |
| Ecosystem expansion | **Deferred** | Add eligible runtimes beyond OpenCode, optional local MCP, broader clients, richer semantic retrieval, and graphical surfaces when contracts are stable. |

### Explicit non-goals

- Copying another system's code, prompts, schemas, skills, names, layouts, or exact workflows.
- Silent destructive actions, installs, commits, pushes, releases, external side effects, or configuration mutation.
- Treating a capability profile or lease as unbounded standing permission.
- Requiring CodeGraph, OpenPencil, or one runtime for core VGXNESS operation.
- Treating prototypes as production or semantic memory as operational truth.
- Multi-user synchronization or distributed scheduling without a future scope decision.
- Making a dashboard, wizard, or TUI the owner of orchestration, installation, memory, or permissions.

### Vision traceability

This is a review map, not a substitute for the definitions above.

| Agreed area | Blueprint authority | Classification |
| --- | --- | --- |
| Product outcome, status, canonicality, and bilingual contract | Sections 1-2 | **Implemented**, **Partial**, **Contracts-only**, and **Planned** are evidence-backed; English is canonical. |
| Human control, pedagogy, critical guidance, and language | Sections 3 and 7 | **Planned**. |
| Clean-room parity, provenance, and prohibited copying | Section 4 | **Implemented** documentation rule; copying is a **Non-goal**. |
| Capabilities, services, operating modes, and adapters | Sections 5, 6, and 10 | Registry/Gatekeeper/Chronicle and OpenCode execution are delivered in a narrow runtime; Navigator's deterministic planning/scheduling foundation is **Partial**, while its native runtime role, other named capabilities, and complete SDD modes remain **Planned** taxonomy. |
| Go control plane, local-first state, TUI, CLI, and OpenCode | Sections 6-7 | Go/CLI/storage/orchestration and the OpenCode bridge **Implemented**; TUI **Planned**. |
| Safe, Balanced, Autonomous, Custom, and capability leases | Sections 7 and 11 | **Planned**; hard gates remain. |
| `direct`, `explore`, `plan`, `sdd`, `recovery`; `plan` versus `tasks` | Section 8 | **Planned** and explicitly distinct. |
| Automatic/interactive preflight and artifact backends | Section 8 | Backend identity **Contracts-only**; runtime preflight **Planned**. |
| Thin packets and continuity capsules | Section 9 | Bounded execution packets are **Implemented**; general continuity capsules remain **Planned**. |
| Chronicle operational truth and readable JSON/JSONL | Sections 6 and 9 | Events, digest-bound snapshots, task replay, atomic pointers, terminal repair, and recovery projection are delivered; broader checkpoint/artifact continuity keeps Chronicle **Partial**. |
| Owned SQLite/FTS5 semantic authority and durable knowledge scope | Section 9 | Core save/search/get foundation **Implemented**; advanced lifecycle **Planned**. |
| No Engram runtime integration | Sections 2, 6, and 9 | **Non-goal**. |
| Optional CodeGraph structural intelligence and wizard install | Sections 7 and 10 | **Planned, optional**, with fallback. |
| Optional OpenPencil design/prototyping and wizard install | Sections 7, 10, and 11 | **Planned, optional**; no automatic promotion. |
| Skills, exact resolution, approvals, reviews, and delivery | Section 11 | Exact bounded resolution/approval/review evidence is **Partial**; full skill lifecycle and delivery automation remain **Planned**. |
| Failure, cancellation, recovery, and background supervision | Sections 3, 9, and 11 | Bounded provider cancellation, recovery projection, and read-only background constraints are **Implemented/Partial**; full Navigator supervision remains **Planned**. |
| Draft 2020-12 schemas and validation limitation | Sections 1-2 and [`schemas/README.md`](schemas/README.md) | Current runtime paths are validated; future-only shapes are **Contracts-only** and full release validation is not claimed. |
| Delivered runtime foundation | Sections 1-2 and this map | Binary, storage, memory, Chronicle, Registry/Gatekeeper, provider runner, bounded coordinator, native Navigator/task sessions, durable owner/epoch authority, OpenCode bridge/setup, content-bound Delivery Authority receipts, tests, and CI are **Implemented** within the stated limits; broader SDD/Chronicle continuity and automatic delivery integration remain **Partial/Planned**. |

## Supporting documents

- [`../README.md`](../README.md) — repository status and bilingual documentation entry point.
- [`go-implementation.md`](go-implementation.md) — delivered Go control-plane packages, partial Chronicle/native-Windows boundaries, planned extensions, interfaces, storage, and testing evidence.
- [`orchestration-flow.md`](orchestration-flow.md) — delivered bounded execution lifecycle versus planned Navigator/SDD routing, artifact, continuity, and recovery extensions.
- [`schemas/README.md`](schemas/README.md) — machine-readable contract index and available validation guidance; contracts do not imply runtime delivery.
