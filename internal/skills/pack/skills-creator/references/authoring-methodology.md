# Agent Skill Authoring Methodology

## Contents

- Selection criteria
- Phase 1: Discover
- Phase 2: Bound
- Phase 3: Architect
- Phase 4: Author
- Phase 5: Verify
- Phase 6: Operate
- Quality dimensions
- Common anti-patterns

## Selection criteria

Create a skill when successful work depends on a reusable procedure, non-obvious domain knowledge, consistent output requirements, repeated tool coordination, or deterministic helpers.

Do not create a skill merely to preserve:

- A one-off instruction.
- Generic knowledge the model already possesses.
- A broad persona without a concrete workflow.
- Repository-wide conventions better suited to `AGENTS.md`.
- Live data, authentication, or controlled actions better suited to MCP.
- Mechanical enforcement better suited to hooks, permissions, or policy.

## Phase 1: Discover

Collect concrete usage examples before choosing structure.

For each example, record:

- User request.
- Required inputs.
- Expected output.
- Tools and data sources.
- Decisions the agent must make.
- Facts it must not invent.
- Conditions requiring clarification.
- Conditions requiring refusal or approval.
- Verification evidence.

Include successful examples, near misses, incomplete requests, and unsafe variants.

## Phase 2: Bound

Write the boundary as four short lists:

1. In scope.
2. Out of scope.
3. Required preconditions.
4. Success criteria.

Split the skill when workflows differ in at least two of these dimensions:

- Trigger language.
- Input type.
- Tool or permission requirements.
- Primary execution sequence.
- Output artifact.
- Verification method.
- Owner or release cadence.

Do not split solely because a workflow supports several providers or file formats. Use a decision gate and provider-specific references when the user goal remains the same.

## Phase 3: Architect

### Activation plane

Use `name` and `description` to make selection reliable.

Write descriptions in this order:

1. Primary action and result.
2. Positive triggering contexts.
3. Important artifacts, systems, or user phrases.
4. Negative boundary when confusion is plausible.

Front-load the most distinctive terms. Avoid vague descriptions such as “Helps with documents” or “Useful for development.”

### Execution plane

Use `SKILL.md` for the instructions every activated run needs:

- Inputs and preconditions.
- Hard rules.
- Workflow.
- Decision gates.
- Tool and resource routing.
- Verification.
- Output contract.
- Failure and escalation.

### Knowledge plane

Use focused reference files for:

- Policies.
- Schemas.
- Provider-specific guidance.
- Detailed examples.
- Legacy or migration notes.
- Large tables or domain vocabularies.

Link every important reference directly from `SKILL.md`. State the condition that makes the reference relevant.

### Deterministic plane

Use scripts when an operation benefits from:

- Exact parsing or calculation.
- Repeatable file conversion.
- Machine-verifiable validation.
- Stable formatting.
- Helpful structured errors.
- Reduced token use.

Keep scripts self-contained when possible. Declare unavoidable dependencies and fail with actionable messages.

### Output plane

Use assets for materials that should be copied, transformed, or included in final outputs. Do not make the agent read binary or large static assets unless inspection is necessary.

## Phase 4: Author

Write in imperative form. Prefer one strong default over a menu of equivalent options.

Use requirement words deliberately:

- `MUST`: violating the rule makes the result unsafe, invalid, or incompatible.
- `SHOULD`: the default is strongly preferred but context may justify deviation.
- `MAY`: optional behavior with no expected default.

Avoid:

- Repeating the same rule in several sections.
- Explaining common knowledge.
- Encoding temporary dates as permanent logic.
- Hiding required behavior in examples only.
- Mixing test answers into production instructions.
- Requiring the agent to read every reference every time.

Use examples when they:

- Encode an exact product requirement.
- Distinguish similar triggers.
- Correct a measured failure.
- Demonstrate a non-obvious input or output shape.

Remove examples that merely restate the instructions.

## Phase 5: Verify

Perform four independent checks:

1. Structural validation: frontmatter, paths, links, line count, required files.
2. Script verification: representative success, invalid input, and edge cases.
3. Behavioral evaluation: activation and task completion.
4. Security review: code, network, filesystem, tools, secrets, and data movement.

Do not use the same expected answer as hidden context for the agent performing a behavioral evaluation. Evaluate generalization rather than reconstruction.

## Phase 6: Operate

Maintain an external registry for production skills containing:

- Name and purpose.
- Owner.
- Current version or commit.
- Supported hosts and models.
- Dependencies and permissions.
- Last security review.
- Last evaluation date and result.
- Known limitations.
- Deprecation status.

Keep lightweight package-local lifecycle metadata in `skill-manifest.json`:

- Skill name.
- Semantic version.
- Accountable owner.
- Provenance.
- License status.
- Creation and last-validation dates.
- Supported hosts.

Use the manifest for traceability, not activation. Keep `name` and `description` authoritative in `SKILL.md`, and regenerate host metadata when either changes.

Rerun evaluations after changes to:

- `description`.
- Workflow rules or output contract.
- Bundled scripts.
- Tools or MCP dependencies.
- Target model family.
- Overlapping skill inventory.
- Relevant policies or schemas.

Deprecate a skill when its workflow is retired, ownership disappears, persistent failures remain unresolved, or another evaluated skill replaces it.

## Quality dimensions

Assess a skill across:

- Activation precision.
- Activation recall.
- Boundary clarity.
- Instruction adherence.
- Task success.
- Output validity.
- Tool correctness.
- Context efficiency.
- Coexistence.
- Safety.
- Portability.
- Maintainability.

A long skill is not automatically complete. Completeness means every critical decision, dependency, verification path, and failure mode is covered at the correct disclosure level.

## Common anti-patterns

### The monolith

One skill covers unrelated workflows and loads extensive instructions for every task.

Correct by splitting distinct goals and moving conditional detail to references.

### The hidden trigger

The body explains when to use the skill, but `description` is vague.

Correct by moving all activation information into `description`.

### The encyclopedia

The skill repeats general documentation instead of providing workflow guidance.

Correct by retaining only non-obvious, task-relevant knowledge and linking focused references.

### The script reflex

The package adds code for work existing tools already perform reliably.

Correct by preferring instructions until determinism or repeated implementation justifies code.

### The happy-path skill

The workflow omits missing inputs, failures, ambiguity, permissions, and verification.

Correct by adding decision gates, escalation rules, and negative evaluations.

### The isolated success

The skill passes alone but steals triggers or degrades behavior when other skills are installed.

Correct by adding coexistence evaluations and narrowing overlapping descriptions.
