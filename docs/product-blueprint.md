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

VGXNESS is for developers who want AI-assisted work to remain understandable, bounded, and recoverable. The current candidate contains a working local control-plane foundation and an installed native OpenCode SDD lifecycle with exact identities, policy, Chronicle evidence, isolated semantic and structured storage, bounded delegation, and recovery-aware operation. Richer interrupted-SDD recovery, provider-neutral routing/catalog probes, and automatic delivery integration remain planned.

**Implemented:** The repository builds a `vgxness` binary with application wiring, a permanent versioned launcher with atomic activation and rollback, storage-root resolution, `status`/`doctor`, SQLite/FTS5 schema v5, runtime contract validation, Chronicle services, Registry, Gatekeeper, prompt composition, and OpenCode execution. The installed projection has 14 managed artifacts: manager v28, five read-only reviewers, six read-only SDD profiles, storage plugin v5, and a model-plan manifest. The plugin exposes 18 storage tools, split into five semantic-memory and 13 SDD tools. The legacy execution bridge CLI remains available for compatibility but is not projected through the plugin.

**Implemented:** Chronicle appends and verifies JSONL events, derives task state, publishes active snapshots through immutable SHA-256 files plus one atomic pointer replacement, and recovers an interrupted terminal-pointer removal. **Partial:** its recovery scope remains narrower than the future general checkpoint/artifact lifecycle.

**Contracts-only:** Independently authored Draft 2020-12 schemas for future provider-neutral routing, artifact, and continuity paths define records that delivered runtimes do not yet exercise. Native SDD invariants are implemented in the Go domain and SQLite schema v5; schemas used by current compatibility paths are validated at runtime.

**Implemented/Partial:** Native SDD supports `memory`, `openspec`, and `hybrid` backends plus per-change `automatic` and `interactive` modes. Memory is canonical in hybrid; OpenSpec content is bounded to `openspec/changes/<safe-change-id>/`; divergent content is never auto-imported. The manager is the sole lifecycle/workspace writer, all six SDD agents are read-only, apply composes a hash-bound patch, and all five reviewers are read-only. Bridge, control-plane orchestration, maintenance, isolated-edit, ticket/wave, and Delivery Authority services remain compatibility CLI/maintainer surfaces, not the active installed scheduler.

### Document and translation contract

- English is the decision source and resolves ambiguity.
- The Spanish companion is complete for navigation and comprehension but explicitly non-canonical.
- Both documents carry the same blueprint version, status model, numbered-section inventory, decisions, gates, roadmap meanings, and traceability topics.
- A material canonical edit must update both documents atomically. A mismatch blocks publication.
- Supporting documents link here instead of maintaining competing roadmaps.

## 2. Status at a glance

| Status | Scope |
| --- | --- |
| **Implemented** | Go binary/composition; launcher and self-install/update/rollback; schema-v5 semantic and structured SDD storage; native SDD lifecycle/backends/modes; manager-only writes; read-only SDD/review agents; OpenSpec projection; runtime contract validation; Chronicle events and bounded recovery; Registry/Gatekeeper; OpenCode execution; 14 managed artifacts and 18 storage tools; guided setup; tests and Go CI. |
| **Partial** | Chronicle does not yet provide rich interrupted-SDD reconstruction; Delivery Authority remains an explicit compatibility receipt/gate CLI without automatic integration; native Windows runtime/distribution smoke remains incomplete. |
| **Contracts-only** | Draft 2020-12 shapes and semantic rules for future provider-neutral routing, artifacts, and continuity paths not yet exercised by delivered runtimes. |
| **Planned** | Richer Chronicle-backed interrupted-SDD recovery, provider-neutral routing/catalog probes, automatic delivery integration, richer autonomy/approval UX, keyboard-first TUI, additional adapters, native Windows validation, and advanced semantic retrieval/lifecycle. |
| **Deferred** | Additional runtime adapters, optional local MCP exposure, broader external MCP clients, richer semantic retrieval, and graphical product surfaces. |
| **Non-goal** | Copied third-party artifacts, silent destructive autonomy, hidden operational truth, runtime or tool lock-in, Engram integration, unbounded agent loops, automatic prototype-to-production promotion, or a UI that owns business policy. |

**Evidence boundary:** The storage plugin v5 exposes five semantic-memory and 13 SDD tools; it has no filesystem, shell, scheduler, routing, editing, delegation, or lifecycle authority. Every SDD mutation fails closed unless trusted context identifies the tracked top-level manager. Model plans configure exact slots but do not prove runtime model availability. Bridge/control-plane/edit/delivery commands remain compatibility behavior. No setup TUI, product-owned MCP server/client, Engram integration, automatic delivery wiring, additional runtime adapter, or silent product configuration mutation is delivered.

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

**Implemented/Partial:** Capability names describe stable product responsibilities, not one process per name. The installed SDD profiles realize bounded operating roles for research/proposal/spec/design/tasks/apply, with manager-owned verification and lifecycle writes; broader provider-neutral capability routing remains planned.

**Contracts-only:** Existing schema terms remain normative for machine-readable records. This blueprint owns the human-facing taxonomy, not schema field definitions.

The delivery split is intentional: owned semantic memory, structured SDD storage/lifecycle, Registry, Gatekeeper, contract-validated bounded execution, crash-atomic Chronicle snapshot publication, and the OpenCode path are **Implemented**. Chronicle remains **Partial** for richer interrupted-SDD recovery, and provider-neutral capability routing remains **Planned**.

## 6. System model

VGXNESS has an **Implemented, bounded** local Go control plane and native OpenCode SDD lifecycle with explicit authority boundaries. Provider-neutral routing/catalog probes and richer interrupted recovery remain **Planned**. The Go architecture is detailed in [`go-implementation.md`](go-implementation.md).

```text
planned TUI / implemented CLI + native OpenCode manager
                         |
 manager-owned direct / read-only Task / SDD routing
                         |
 read-only SDD agents -> hash-bound apply patch
                         |
 manager-only workspace, validation, acceptance, transitions
                         |
 storage plugin v5 -> semantic memory + structured SDD v5
```

| Boundary | Status and responsibility |
| --- | --- |
| Go binary and composition | **Implemented:** Build a local binary and wire configuration, inspection, memory, Chronicle, Registry/Gatekeeper, providers, coordinator, setup, and bridge commands. |
| CLI, self-installation, and storage roots | **Implemented:** Install immutable binary versions behind a permanent launcher, activate updates atomically, roll back one level, resolve project/user storage, and provide status, doctor, memory, guided setup, integration, and bounded bridge commands. |
| Owned MemoryStore | **Implemented:** SQLite/FTS5 semantic storage, migrations, lifecycle fields, filtered search, provenance-backed records, bounded automatic retrieval, and governed agent-proposed candidates with contradiction review. |
| Chronicle | **Implemented/Partial:** Implements strict current-run reading, verified JSONL events, immutable content-addressed active snapshots, atomic pointer commits, terminal repair, task-state replay, cancellation evidence, and bounded recovery reconstruction. General checkpoint/artifact continuity remains planned. |
| Schemas and semantic rules | **Implemented/Contracts-only:** Current packets, events, snapshots, registry records, prompts, results, and native SDD domain rules are validated; future provider-neutral routing/continuity shapes remain contracts-only. |
| Keyboard-first TUI | **Planned:** Provide setup and focused interaction without owning installation or orchestration policy. |
| Bounded coordination | **Implemented, narrow:** The manager selects direct, bounded read-only, or native SDD work. At most four independent read-only subworks overlap; synthesis and all writes remain sequential. Compatibility coordinator services remain separate. |
| OpenCode adapter | **Implemented:** Manager v28, five read-only reviewers, six read-only SDD profiles, storage plugin v5, and a low/medium/high manifest form 14 managed artifacts. The default/current `medium` plan uses Luna Fast, Terra, and Sol slots; plan or slot changes require restart. |
| CodeGraph adapter | **Implemented, optional:** The manager and reviewers may use the read-only `codegraph_explore` MCP tool for bounded structural evidence when a healthy project index exists; exact source and diff inspection remain authoritative fallbacks. |
| OpenPencil adapter | **Planned, optional:** Design and prototyping path; artifacts remain proposals until separately implemented and verified. |
| Other runtime/MCP adapters | **Deferred:** Additional integrations may be added without changing core contracts. |

**Non-goal:** No adapter may bypass Gatekeeper, redefine taxonomy, become operational truth, or embed policy that belongs in the control plane.

## 7. User experience

### Setup wizard

**Implemented, headless:** The OpenCode wizard detects prerequisites and paths, explains all six steps and their limits, shows proposed changes, requests approval, installs the 14 managed artifacts through application services, reads results back, verifies the live handshake, and reports bounded rollback or repair guidance. It does not claim runtime model-availability probes. A richer keyboard-first TUI and optional-adapter setup remain planned.

The wizard may detect OpenCode, CodeGraph, and OpenPencil. It may offer to install an absent optional adapter only after disclosing source, version, command, destination, network use, configuration changes, and rollback. Declining an optional adapter preserves a supported fallback. Detection never authorizes installation or initialization.

**Non-goal:** Setup will not silently install packages, initialize repositories, mutate configuration, overwrite files, or claim success without readback evidence.

### Navigator interaction

**Implemented/Partial:** The OpenCode manager matches the user's language and selects direct inline work, bounded native read-only Task delegation, or optional user-approved SDD. It loads native skills, may consult bounded CodeGraph evidence, performs all edits and validation, and uses five read-only reviewers against a frozen candidate. Native SDD storage, backends, interaction modes, phase transitions, and projection checks are delivered; richer Chronicle-backed interruption recovery remains planned.

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

**Implemented/Partial:** The native manager classifies each request into the smallest adequate route. Structured SDD state is persisted; provider-neutral routing records and catalog probes remain planned.

| Route | Use |
| --- | --- |
| `direct` | Answer or perform a small, low-risk action without a multi-phase workflow. |
| `explore` | Investigate unknowns, current state, constraints, or feasibility. |
| `plan` | Classifier route for a bounded approach when implementation is not authorized or full SDD is unnecessary. It is not the SDD `tasks` mode. |
| `sdd` | Run proposal, spec, design, `tasks`, apply, verify, and archive modes as required by risk and policy. |
| `recovery` | Reconcile interrupted, inconsistent, blocked, or failed work from durable evidence. |

The `tasks` operating mode belongs inside SDD and converts approved requirements and design into implementation work units. A `plan` route may end with advice or a lightweight breakdown and does not imply approved SDD artifacts.

### Preflight and artifact stores

**Implemented/Partial:** The native OpenCode manager offers optional SDD and stores `automatic` or `interactive` execution per change. Automatic mode advances validated gates without routine pauses; interactive mode pauses at each candidate boundary. Provider-neutral preflight/catalog probes and richer recovery UX remain planned.

**Implemented:** The structured SDD store supports `memory`, `openspec`, and `hybrid` records in isolated SQLite tables. Six read-only phase profiles and the manager's sequential lifecycle contract are installed. The manager performs repository writes through ordinary OpenCode tools; the storage plugin remains filesystem-free. Engram is not a runtime backend or planned adapter.

| User-facing store | Contract mapping | Delivery and behavior |
| --- | --- | --- |
| `memory` | `memory` → structured SDD tables | **Implemented:** canonical bodies, changes, artifacts, immutable accepted revisions, and input bindings are isolated from semantic observations. |
| `openspec` | `openspec` | **Implemented, bounded:** the repository file under `openspec/changes/<safe-change-id>/` is canonical; SQLite retains external path, digest, revision identity, and bindings but not its body. The manager uses native workspace tools and records verified projection evidence. |
| `both` | `hybrid` | **Implemented, bounded:** memory is canonical; deterministic render/compare and explicit overwrite, inspect, or save-as-candidate reconciliation prevent automatic divergent imports. |
| disabled | `none` | **Contracts-only:** perform no SDD artifact access when policy permits skipping it. |

The delivered durable order is `explore -> proposal -> spec -> design -> tasks -> apply -> verify -> complete`. Every save and acceptance is limited to the current phase, and every transition requires that phase's accepted artifact; OpenSpec-backed transitions additionally require a current projection bound to the same revision and digest. All SDD profiles are read-only, including the hash-bound apply patch composer. At most four independent read-only phase investigations may overlap; the manager alone writes workspace files, runs validation, persists artifacts, and performs transitions sequentially.

## 9. Context and persistence

### Thin context

**Partial:** The bounded provider path constructs and enforces thin execution packets with goals, scope, allowed paths/tools, exact skill references, acceptance criteria, approval state, and return contracts. General Navigator/SDD continuity capsules and artifact fetching remain **Planned**.

### Semantic authority and operational truth

| Concern | Status and owner |
| --- | --- |
| Chronicle | **Implemented/Partial:** Reads the active pointer; validates, appends, rolls back, and rereads JSONL events; derives task/cancellation state; publishes active snapshots atomically through immutable content-addressed files; and repairs interrupted terminal publication. General checkpoints and richer artifact recovery remain planned. |
| VGXNESS MemoryStore | **Implemented:** Semantic authority for typed durable observations with identity, scope, topic, provenance, lifecycle state, references, save/search/get, session metadata, automatic task retrieval/capture, and governed `memoryCandidates`. Conflicting candidate updates transition to `needs_review`; broader human review UX remains planned. |
| SQLite/FTS5 | **Implemented:** One default schema-v5 user database, canonical workspace bindings, project-isolated semantic and SDD domains, safe legacy imports, deterministic filtering, and lexical retrieval. Read-only v4 opens cannot migrate; follow [Native memory](memory.md#upgrade-migration-caveat), never delete the database. |
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

The native sequence through **structured SDD storage → read-only phase agents → manager-owned apply/validation/lifecycle** is implemented. The next sequence is **richer Chronicle-backed interrupted-SDD recovery → provider-neutral routing/catalog probes → automatic delivery integration**. Native Windows runtime/distribution smoke is deferred.

| Horizon | Status | Outcome |
| --- | --- | --- |
| Local product foundation | **Implemented** | Go composition, versioned self-install/update/rollback, schema-v5 semantic/SDD storage, CLI, compatibility bridge, status/doctor, tests, and CI. |
| Runtime contract foundation | **Implemented/Contracts-only** | Validate contracts and SDD domain rules used by delivered paths; retain future provider-neutral routing/continuity shapes as contracts-only. |
| Chronicle operational state | **Implemented/Partial** | Deliver verified events, task/cancellation replay, digest-bound snapshots, atomic pointer publication, terminal repair, and recovery projection; expand general checkpoint/artifact continuity. |
| Native SDD foundation | **Implemented/Partial** | Deliver backends, modes, isolated revisions/bindings, OpenSpec projection, manager-only writes, read-only phase/review agents, and bounded parallel reads; add richer Chronicle-backed interruption recovery and provider-neutral routing/catalog probes. |
| Compatibility control plane | **Implemented** | Retain bridge/orchestrate/ticket/wave/edit/Delivery Authority CLI and maintainer services without projecting them as the active OpenCode scheduler. |
| Structural and design adapters | **Partial/Planned** | Bounded optional CodeGraph evidence is implemented; broader provider-neutral probes and OpenPencil integration remain planned. |
| Safe delivery | **Partial** | Compatibility receipts/gates and strict native review boundaries exist; automatic delivery integration remains planned. |
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
| Capabilities, services, operating modes, and adapters | Sections 5, 6, and 10 | Native manager routing and bounded SDD operating roles are **Implemented**; provider-neutral capability routing remains **Planned**. |
| Go control plane, local-first state, TUI, CLI, and OpenCode | Sections 6-7 | Go/CLI/storage/native OpenCode SDD are **Implemented**; compatibility orchestration remains separate; TUI is **Planned**. |
| Safe, Balanced, Autonomous, Custom, and capability leases | Sections 7 and 11 | **Planned**; hard gates remain. |
| `direct`, `explore`, `plan`, `sdd`, `recovery`; `plan` versus `tasks` | Section 8 | Native route selection is **Implemented/Partial**; provider-neutral persistence is planned; terms remain distinct. |
| Automatic/interactive preflight and artifact backends | Section 8 | Per-change modes and `memory`/`openspec`/`hybrid` behavior are **Implemented**. |
| Thin packets and continuity capsules | Section 9 | Bounded execution packets are **Implemented**; general continuity capsules remain **Planned**. |
| Chronicle operational truth and readable JSON/JSONL | Sections 6 and 9 | Events, digest-bound snapshots, task replay, atomic pointers, terminal repair, and recovery projection are delivered; broader checkpoint/artifact continuity keeps Chronicle **Partial**. |
| Owned SQLite/FTS5 semantic authority and durable knowledge scope | Section 9 | Core save/search/get foundation **Implemented**; advanced lifecycle **Planned**. |
| No Engram runtime integration | Sections 2, 6, and 9 | **Non-goal**. |
| Optional CodeGraph structural intelligence and wizard install | Sections 7 and 10 | Bounded read-only evidence is **Implemented, optional**; wizard installation remains **Planned**. |
| Optional OpenPencil design/prototyping and wizard install | Sections 7, 10, and 11 | **Planned, optional**; no automatic promotion. |
| Skills, exact resolution, approvals, reviews, and delivery | Section 11 | Exact bounded resolution/approval/review evidence is **Partial**; full skill lifecycle and delivery automation remain **Planned**. |
| Failure, cancellation, recovery, and background supervision | Sections 3, 9, and 11 | Bounded cancellation and read-only supervision are **Implemented**; richer Chronicle-backed interrupted-SDD recovery remains **Planned**. |
| Draft 2020-12 schemas and validation limitation | Sections 1-2 and [`schemas/README.md`](schemas/README.md) | Current runtime paths are validated; future-only shapes are **Contracts-only** and full release validation is not claimed. |
| Delivered runtime foundation | Sections 1-2 and this map | Binary, schema-v5 semantic/SDD storage, native manager lifecycle, read-only phase/review agents, storage plugin authorization, backends/modes/projection, OpenCode setup, compatibility CLI subsystems, tests, and CI are **Implemented** within stated limits; richer recovery, provider-neutral routing/catalog probes, and automatic delivery remain **Planned**. |

## Supporting documents

- [`../README.md`](../README.md) — repository status and bilingual documentation entry point.
- [`go-implementation.md`](go-implementation.md) — delivered Go control-plane packages, partial Chronicle/native-Windows boundaries, planned extensions, interfaces, storage, and testing evidence.
- [`orchestration-flow.md`](orchestration-flow.md) — active native SDD lifecycle, compatibility control-plane boundary, and planned recovery/routing/delivery extensions.
- [`schemas/README.md`](schemas/README.md) — machine-readable contract index and available validation guidance; contracts do not imply runtime delivery.
