# VGXNESS Product Blueprint

> **Blueprint version:** 1.0  
> **Document status:** Current  
> **Authority:** Canonical English product blueprint  
> **Companion:** [Español — traducción no canónica](product-blueprint.es.md)

This document is the sole canonical source for the VGXNESS product vision, vocabulary, boundaries, and roadmap. Supporting documents describe narrower contracts and workflows; they do not redefine this blueprint. If the Spanish companion differs, this English version controls.

Status labels have precise meanings:

- **Current** — present and verifiable in this repository now.
- **Planned** — approved product direction that is not implemented yet.
- **Deferred** — approved direction intentionally outside the first delivery horizon.
- **Non-goal** — excluded behavior that requires a new scope decision before it can enter the roadmap.

## 1. Purpose and audience

VGXNESS is for developers who want AI-assisted work to remain understandable, bounded, and recoverable. It is intended to become a local-first control plane that coordinates agents, skills, semantic memory, operational state, structural and design tools, and delivery safeguards without hiding consequential decisions inside prompts.

**Current:** The repository provides independently authored product documentation and Draft 2020-12 contract schemas. It does not provide an executable product. Schema conformance can be checked with compatible validators, but a complete VGXNESS release validator and end-to-end runtime enforcement do not exist yet.

**Planned:** VGXNESS will guide ordinary scoped work quietly while preserving evidence for routing, approvals, artifacts, validation, and recovery. The human sets intent and approves consequential actions; the system makes bounded work easier to plan, execute, review, and resume.

### Document and translation contract

- English is the decision source and resolves ambiguity.
- The Spanish companion is complete for navigation and comprehension but explicitly non-canonical.
- Both documents carry the same blueprint version, status model, numbered-section inventory, decisions, gates, roadmap meanings, and traceability topics.
- A material canonical edit must update both documents atomically. A mismatch blocks publication.
- Supporting documents link here instead of maintaining competing roadmaps.

## 2. Status at a glance

| Status | Scope |
| --- | --- |
| **Current** | Documentation plus runtime-neutral Draft 2020-12 JSON Schemas for orchestration, execution, registries, run snapshots, and append-only events. |
| **Planned** | A globally installed Go control plane; keyboard-first setup; bounded capabilities; Chronicle operational storage; VGXNESS-owned SQLite/FTS5 semantic memory; approvals; validation; recovery; safe delivery; and optional runtime, structural-intelligence, design, MCP, and compatibility adapters. |
| **Deferred** | Additional runtime adapters, optional local MCP exposure, broader external MCP clients, richer semantic retrieval, and graphical product surfaces. |
| **Non-goal** | Copied third-party artifacts, silent destructive autonomy, hidden operational truth, runtime or tool lock-in, unbounded agent loops, automatic prototype-to-production promotion, or a UI that owns business policy. |

**Current:** No Go source, binary, installer, bundled skill catalog, runtime adapter, MCP server/client, persistence implementation, Git automation, or product configuration mutation is delivered by this repository.

**Current limitation:** The schemas under [`schemas/`](schemas/README.md) are current contracts for future implementations. Their `$schema` declarations and compatibility with Draft 2020-12 tooling do not prove complete semantic correctness or runtime enforcement.

## 3. Product principles

1. **The human leads.** The user owns goals, scope, and approval of consequential actions.
2. **Teach, do not obscure.** Explain important facts, recommendations, tradeoffs, and uncertainty.
3. **Verify before agreeing.** Claims about code, tools, state, and completion require evidence.
4. **Coordinate through boundaries.** Orchestration, execution, review, storage, skills, permissions, and adapters remain separable.
5. **Keep state inspectable.** Chronicle records operational truth; semantic memory preserves durable meaning.
6. **Use the smallest capable path.** Simple work stays direct; complexity and risk receive proportionate planning and validation.
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
| **Current authorship rule** | Product prose and schemas in this repository are VGXNESS-owned contracts and remain independently authored. |
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
| Adapters | Replaceable translations between the control plane and external runtimes, tools, protocols, or stores. | OpenCode, CodeGraph, OpenPencil, Engram, MCP, and future adapters. |

**Planned:** Capability names describe stable product responsibilities, not one process per name and not provider-specific prompt files. `sdd-design` is a Blueprint operating mode, `sdd-apply` is a Forge operating mode, and `sdd-verify` is a Sentinel operating mode.

**Current:** Existing schema terms remain normative for machine-readable records. This blueprint owns the human-facing taxonomy, not schema field definitions.

## 6. System model

**Planned:** VGXNESS will be a globally installed, local-first Go control plane with explicit package and dependency boundaries. The Go architecture is detailed in [`go-implementation.md`](go-implementation.md).

```text
keyboard-first TUI / CLI / optional local MCP
                    |
             application services
                    |
 Navigator + bounded capabilities and operating modes
                    |
       Registry + Chronicle + Gatekeeper
                    |
 owned MemoryStore + optional external adapters
```

| Boundary | Status and responsibility |
| --- | --- |
| Go control plane | **Planned:** Own application policy, orchestration, installation services, validation, and composition. |
| Keyboard-first TUI | **Planned:** Provide setup and focused interaction without owning installation or orchestration policy. |
| CLI | **Planned:** Provide inspection, recovery, and automation; it is convenience, not lock-in. |
| OpenCode adapter | **Planned:** First preferred runtime adapter, selected only when capability and policy checks pass. |
| CodeGraph adapter | **Planned, optional:** Preferred structural-intelligence path when healthy and approved; filesystem analysis remains available. |
| OpenPencil adapter | **Planned, optional:** Design and prototyping path; artifacts remain proposals until separately implemented and verified. |
| Owned MemoryStore | **Planned from the initial foundation:** SQLite/FTS5-first semantic authority under VGXNESS control. |
| Chronicle files | **Planned:** Readable snapshots, JSONL events, receipts, artifacts, and recovery evidence. |
| Other runtime/MCP adapters | **Deferred:** Additional integrations may be added without changing core contracts. |

**Non-goal:** No adapter may bypass Gatekeeper, redefine taxonomy, become operational truth, or embed policy that belongs in the control plane.

## 7. User experience

### Setup wizard

**Planned:** A keyboard-first wizard will detect prerequisites and paths, explain optional integrations, show proposed changes, request required approval, back up existing configuration, perform approved installation through application services, read results back, and offer retry, repair, or rollback guidance.

The wizard may detect OpenCode, CodeGraph, and OpenPencil. It may offer to install an absent optional adapter only after disclosing source, version, command, destination, network use, configuration changes, and rollback. Declining an optional adapter preserves a supported fallback. Detection never authorizes installation or initialization.

**Non-goal:** Setup will not silently install packages, initialize repositories, mutate configuration, overwrite files, or claim success without readback evidence.

### Navigator interaction

**Planned:** Navigator matches the user's language, distinguishes facts from recommendations, asks one blocking question at a time, explains consequential decisions, keeps the happy path concise, and selects a bounded capability instead of performing every role itself.

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

**Schema migration prerequisite:** The current SDD preflight schema accepts `engram`, `openspec`, `hybrid`, and `none`. Its `engram` token is transitional and cannot cleanly represent the planned owned-memory default. Before the first Go implementation, the schema must add a provider-neutral/configured `memory` backend token; required SDD with the owned `MemoryStore` cannot ship until then. Engram remains only an optional compatibility/import adapter.

| User-facing store | Planned backend after migration | Behavior |
| --- | --- | --- |
| `memory` | `memory` → configured `MemoryStore` | Persist SDD artifacts through immutable memory references. VGXNESS-owned memory is the planned default; this row becomes contract-valid only after the prerequisite migration, not by relabeling owned memory as `engram`. |
| `openspec` | `openspec` | Persist artifacts in the repository's OpenSpec structure. |
| `both` | `hybrid` | Keep memory and filesystem artifacts synchronized. |
| disabled | `none` | Perform no SDD artifact access when policy permits skipping it. |

Phase order is evidence-driven rather than ceremonial. A phase may be skipped only when its required artifact or decision exists and remains valid. Apply follows approved requirements and design; verify proves the result; archive closes and synchronizes final state.

## 9. Context and persistence

### Thin context

**Planned:** Each bounded task receives the goal, scope, allowed paths and tools, immutable artifact references, exact skill references, acceptance criteria, approval state, and return contract. A continuity capsule preserves decisions, state references, provenance, completed and next actions, blockers, and recovery guidance without copying the transcript.

### Semantic authority and operational truth

| Concern | Status and owner |
| --- | --- |
| Chronicle | **Planned:** Operational authority for events, execution state, snapshots, receipts, approvals, artifact references, checkpoints, results, cancellations, and replay. |
| VGXNESS MemoryStore | **Planned from the initial Go foundation:** Semantic authority for durable decisions, preferences, conventions, discoveries, bug causes, constraints, approvals and their rationale, lessons, summaries, continuity capsules, and artifact references. |
| SQLite/FTS5 | **Planned from the initial Go foundation:** Owned local persistence and lexical retrieval, introduced incrementally behind `MemoryStore`; richer semantic indexing may follow. |
| Engram adapter | **Planned, optional:** Compatibility, import, and reference bridge. It may preserve external IDs and provenance but does not own VGXNESS semantics. |
| Project/user roots | **Planned:** Explicit project-local `.vgxness/` or user-global `~/.vgxness/projects/<project-id>/` storage policy. |

Memory entries carry stable IDs, type, topic, content, provenance, timestamps, scope, lifecycle state, and references. Search starts with deterministic filters and FTS5; summaries and embeddings may supplement retrieval later without replacing source records.

Chronicle and semantic memory may cross-reference each other but never substitute for each other. If semantic context conflicts with an event, receipt, or execution state, Chronicle controls the operational decision and the inconsistency is surfaced. If Engram is absent, declined, or unavailable, owned memory remains fully usable and authoritative.

### Semantic-memory lifecycle

| Stage | Owned-store behavior |
| --- | --- |
| Capture | Accept a typed durable observation with source, scope, and evidence; reject raw operational noise. |
| Normalize | Assign stable identity, topic, timestamps, lifecycle metadata, and immutable source references. |
| Retrieve | Apply scope/type/lifecycle filters before FTS5 ranking and return provenance with every result. |
| Compare | Preserve compatible, related, scoped, conflicting, or superseding relationships without deleting history silently. |
| Review | Surface stale or review-due knowledge before it is trusted as current fact. |
| Summarize | Create derived summaries that reference source entries rather than replacing them. |
| Import | Translate optional Engram records with source IDs and import provenance; never overwrite authority silently. |

Retention, redaction, export, backup, and migration remain explicit application services. Memory writes respect project/user scope and secret policy. Deleting or rewriting durable knowledge is consequential and cannot be smuggled through an ordinary edit lease.

### Recovery and authority conflicts

Recovery first reconstructs operational state from Chronicle snapshots, events, receipts, artifacts, and cancellations. It then retrieves semantic context to explain intent, constraints, prior lessons, and expected next actions. If the sources disagree, VGXNESS records a conflict reference, keeps the last valid operational state, and asks for correction or approval when evidence cannot resolve it. A semantic summary never repairs or advances a run by itself.

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
| Registry | Resolve exact agent, skill, adapter, version, source, provenance, capability, permission, and scope. | Rejects unresolved or out-of-scope references. |
| Chronicle | Record correlated operational facts and expose consistent state for inspection and recovery. | Never invents missing state or becomes semantic memory. |
| Gatekeeper | Enforce eligibility, schemas, permissions, leases, approvals, roots/tools, loop budgets, and transitions. | Fails closed and never asks an LLM to improvise policy. |

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

**Planned:** Independently authored first-party skills are versioned behavior contracts. Registry resolves exact identity, version, source, provenance, trigger, and allowed scope before dispatch. Navigator passes a resolved reference or frozen payload; it does not paraphrase memory as authority.

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

| Horizon | Status | Outcome |
| --- | --- | --- |
| Contract foundation | **Current** | Maintain independently authored bilingual product documentation and Draft 2020-12 schemas as reviewable contracts, without claiming full release validation. |
| Local product foundation | **Planned** | Build Go composition, storage resolution, Chronicle, Gatekeeper, owned SQLite/FTS5 `MemoryStore`, CLI inspection, and keyboard-first setup with backup/readback recovery. |
| Bounded orchestration | **Planned** | Add Navigator routing, thin packets, operating modes, Registry resolution, approvals, continuity, and optional Engram compatibility. |
| Structural and design adapters | **Planned** | Add optional CodeGraph and OpenPencil detection, wizard installation, provenance, safe fallbacks, and focused Sentinel validation. |
| Safe delivery | **Planned** | Add skill lifecycle, worktree/work-unit support, review budgets, Git safeguards, supervised background work, bounded reviews, and recovery. |
| Ecosystem expansion | **Deferred** | Add eligible runtimes beyond OpenCode, optional local MCP, broader clients, richer semantic retrieval, and graphical surfaces when contracts are stable. |

### Explicit non-goals

- Copying another system's code, prompts, schemas, skills, names, layouts, or exact workflows.
- Silent destructive actions, installs, commits, pushes, releases, external side effects, or configuration mutation.
- Treating a capability profile or lease as unbounded standing permission.
- Requiring CodeGraph, OpenPencil, Engram, or one runtime for core VGXNESS operation.
- Treating prototypes as production or semantic memory as operational truth.
- Multi-user synchronization or distributed scheduling without a future scope decision.
- Making a dashboard, wizard, or TUI the owner of orchestration, installation, memory, or permissions.

### Vision traceability

This is a review map, not a substitute for the definitions above.

| Agreed area | Blueprint authority | Classification |
| --- | --- | --- |
| Product outcome, status, canonicality, and bilingual contract | Sections 1-2 | **Current** documentation; **Planned** product. |
| Human control, pedagogy, critical guidance, and language | Sections 3 and 7 | **Planned**. |
| Clean-room parity, provenance, and prohibited copying | Section 4 | **Current** rule; copying is a **Non-goal**. |
| Capabilities, services, operating modes, and adapters | Sections 5, 6, and 10 | **Planned** taxonomy. |
| Go control plane, local-first state, TUI, CLI, and OpenCode | Sections 6-7 | **Planned**. |
| Safe, Balanced, Autonomous, Custom, and capability leases | Sections 7 and 11 | **Planned**; hard gates remain. |
| `direct`, `explore`, `plan`, `sdd`, `recovery`; `plan` versus `tasks` | Section 8 | **Planned** and explicitly distinct. |
| Automatic/interactive preflight and artifact backends | Section 8 | **Planned**. |
| Thin packets and continuity capsules | Section 9 | **Planned**. |
| Chronicle operational truth and readable JSON/JSONL | Sections 6 and 9 | **Planned**. |
| Owned SQLite/FTS5 semantic authority and durable knowledge scope | Section 9 | **Planned from initial foundation**. |
| Optional Engram compatibility/import/reference adapter | Sections 6 and 9 | **Planned, optional**. |
| Optional CodeGraph structural intelligence and wizard install | Sections 7 and 10 | **Planned, optional**, with fallback. |
| Optional OpenPencil design/prototyping and wizard install | Sections 7, 10, and 11 | **Planned, optional**; no automatic promotion. |
| Skills, exact resolution, approvals, reviews, and delivery | Section 11 | **Planned**. |
| Failure, cancellation, recovery, and background supervision | Sections 3, 9, and 11 | **Planned**. |
| Draft 2020-12 schemas and validation limitation | Sections 1-2 and [`schemas/README.md`](schemas/README.md) | **Current** contracts; full release validation is not claimed. |
| Documentation-only blueprint delivery | Sections 1-2 and this map | **Current**; no runtime capability is delivered. |

## Supporting documents

- [`../README.md`](../README.md) — repository status and bilingual documentation entry point.
- [`go-implementation.md`](go-implementation.md) — planned Go packages, interfaces, storage, and testing boundaries.
- [`orchestration-flow.md`](orchestration-flow.md) — planned request lifecycle, gates, operating modes, memory authority, and recovery flow.
- [`schemas/README.md`](schemas/README.md) — current machine-readable contract index and available validation guidance.
