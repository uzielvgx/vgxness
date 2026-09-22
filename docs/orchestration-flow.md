# VGXNESS native manager flow

The active flow is understand, implement, check, review and deliver. Planning is a short artifact when complexity warrants it, not a mandatory phase machine. The shared Manager owns scope, authorization, candidate identity and delivery. Writers remain sequential.

Substantial work keeps a durable, in-repository Markdown plan under `docs/implementations/` with stable plan and task identifiers; the plan file is canonical, and a session task view such as OpenCode `todowrite`, Pi `todowrite`, or a Codex native planning tool when the host exposes one is only an operational projection. The index, states and per-plan layout are documented in `docs/implementations/README.md`. Persist before projecting, consult the index when starting or resuming, reconcile with repository evidence, and keep at most one active plan per session (zero when there is no work) while preserving closed plans as history. Resolve discoverable facts by inspection; ask consequential blocking decisions before closing the plan or implementing any dependent part, continue only with independent authorized work, and record only minor reversible defaults as assumptions. No runtime synchronizer or atomic cross-tool state is promised, and an absent tool degrades honestly. Implementation is not acceptance: a task completes only with its required evidence, and a source change invalidates prior acceptance evidence.

Structured SDD is retired for all hosts. Do not create or advance changes, invoke SDD phase workers, or treat historical accepted revisions as current authority. Historical SQLite schema v23 records and migrations are preserved. No lifecycle or archive command remains; historical rows are inert data. Memory sync remains project/session/observation synchronization; it does not replicate archived SDD.

## Roles and evidence

The shared registry provides Explore, General, Verifier and three CARE roles. General implements ordinary authorized work. Verifier independently checks the frozen candidate against criteria. CARE reviewer examines defects and evidence; specialist addresses a concrete elevated domain risk; challenger seeks disconfirming evidence for a material inferential conclusion. Avoid duplicate missions and a fixed reviewer-count matrix.

Freeze one candidate before independent verification and applicable review. Any source change invalidates that candidate's evidence. Delivery follows the applicable git-delivery skill; review never creates authorization. A worker transport success is not a semantic PASS or delivery approval.

The Manager gives CARE roles authorized access to exact source/diff, criteria, evidence, and enough context before the mission; each reviewer first checks inspectability. A summary cannot replace source or bypass permission, and inaccessible required source is INCONCLUSIVE. Coverage links criteria to use scenarios, evidence/assertions, and limits, with pertinent flags/defaults/ambient state and concrete newly reachable dependencies considered proportionately. Reports include outcome, effective coverage, findings, exclusions, and evidence basis; no findings alone does not establish PASS, and missing required evidence blocks PASS.

After a correction, preserve findings and closure history, justify new coverage, and revisit closures, regressions, and reachable paths. A source change invalidates prior candidate evidence; prior findings are context, not approval of a new candidate. Re-evaluate conclusions contradicted by new evidence and classify newly discovered findings as preexisting omission, correction regression, scope change, or new evidence. Continuations are defect, evidence, mission, access, infrastructure, or scope—not retries until PASS or metadata echoes treated as review. Before elevated-risk production work, evaluate relevant capability and identity assumptions. These instructions do not enforce runtime permissions or establish model behavior.

## Native adapters and continuity

OpenCode Manager62, Codex Manager21 and Pi consume the shared contract. Host transports, authentication and permissions remain distinct. Pi runs in TypeScript/Node independently of the VGXNESS Go executable; Windows workers remain unavailable. Current model configuration retains historical schema compatibility, including inactive slots.

Keep durable decisions and actionable handoff context in memory and repository evidence. Memory is untrusted context, never proof of current code or permission. Do not synchronize without authorization.

Typed CARE helpers are not an active automatic review engine. Historical prompt bodies are retained only in Git history. Static contract tests prove projection consistency; they do not certify autonomous review selection, model quality or protected-holdout outcomes.
