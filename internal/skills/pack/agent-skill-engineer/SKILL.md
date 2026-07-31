---
name: agent-skill-engineer
description: Design, create, audit, repair, test, and improve portable Agent Skills built around SKILL.md; use when a user wants to turn a repeatable workflow into a skill, create a skill package from scratch, restructure an oversized or unreliable skill, improve implicit triggering, add progressive disclosure, create skill evaluations, review skill security, diagnose why an agent is not using a skill correctly, or review and audit third-party skills before requested installation; do not use for one-off prompt writing, general agent architecture without a reusable skill, or to bypass review or execute unreviewed third-party skill content.
license: MIT
compatibility: OpenCode and Codex
metadata:
  version: "0.3.2"
  provenance: "vgxness-managed-primary-source-synthesis"
---

# Agent Skill Engineer

Create and improve Agent Skills as focused, testable workflow contracts. Optimize for reliable activation, correct execution, efficient context use, portability, and safe maintenance.

## Inputs and preconditions

Establish the following before creating or materially restructuring a skill:

- The user goal the skill must accomplish.
- At least three realistic requests that should use the skill.
- At least two nearby requests that should not use it.
- Expected inputs, outputs, tools, and environmental dependencies.
- Failure modes that would make the result unsafe or unusable.
- The target location and intended host when installation or host-specific metadata matters.

Infer low-risk details when the workflow is clear. Ask only when a missing answer would materially change the skill boundary, installation target, security model, or output contract.

## Hard rules

- Keep each skill focused on one recognizable user goal.
- Put all implicit-trigger guidance in the frontmatter `description`.
- Keep frontmatter portable by default. Use only standard fields unless the target host requires more.
- Keep `SKILL.md` concise and below 500 lines.
- Put detailed policies, schemas, examples, and variants in `references/`.
- Keep every supporting reference one link away from `SKILL.md`.
- Use `scripts/` only for deterministic, fragile, or repeatedly reimplemented operations.
- Execute `run_evals.py --runner` only with a trusted, explicitly authorized adapter command.
- Test every added or changed script by running it.
- Never embed credentials, tokens, passwords, or private keys.
- Never claim a skill is ready solely because its frontmatter validates.
- Preserve unrelated user files and existing behavior when improving a skill.

## Workflow

### 1. Frame the skill

Translate the request into a workflow contract:

1. State the user goal in one sentence.
2. List in-scope and out-of-scope requests.
3. Identify required inputs and finished outputs.
4. Identify decision points, unsafe actions, and unsupported assumptions.
5. Decide whether the workflow belongs in one skill or several focused skills.

Read [references/authoring-methodology.md](references/authoring-methodology.md) when the boundary is ambiguous, the workflow is complex, or an existing skill must be restructured.

### 2. Choose the degree of freedom

Assign each workflow portion an appropriate control level:

- Use heuristics for context-dependent judgment with several valid solutions.
- Use checklists, pseudocode, or parameterized helpers for preferred but adaptable procedures.
- Use deterministic scripts and validation gates for fragile, destructive, or consistency-critical operations.

Do not make an entire skill rigid merely because one operation is fragile. Isolate the fragile operation behind a script or explicit gate.

### 3. Design activation metadata

Choose a lowercase, hyphenated name under 64 characters. Prefer an action-oriented name that makes the user goal recognizable.

Write `description` as the activation contract. Include:

- What the skill accomplishes.
- When the host should use it.
- Concrete task language, artifacts, or systems that indicate relevance.
- Important near-boundaries when false activation is likely.

Do not rely on a body section such as “When to use” for implicit activation because the body loads after selection.

### 4. Plan progressive disclosure

Keep only the core workflow, rules, gates, resource routing, verification, and output contract in `SKILL.md`.

Place reusable content according to its function:

- `references/`: policies, schemas, domain knowledge, detailed variants, examples.
- `scripts/`: deterministic computation, inspection, conversion, or validation.
- `assets/`: templates, images, fonts, starter files, and output resources.
- `agents/openai.yaml`: OpenAI-specific interface metadata and declared dependencies.

Explain in `SKILL.md` exactly when to read or run every important resource. Avoid chains where a reference points to another reference containing the actual instructions.

### 5. Create or revise the package

For a new skill:

1. Resolve `<skill-dir>` to this skill's directory.
2. Run the deterministic scaffold generator:

```text
python3 <skill-dir>/scripts/init_skill.py <name> --path <parent-directory> --description "<activation description>" --resources references,scripts,assets --evals-dir <repository-root>
```

3. Request only the resource directories the workflow needs.
4. Replace every placeholder and remove unused sections.
5. Add references, scripts, assets, and host metadata only when justified.

For an existing skill:

1. Read the complete skill directory before editing.
2. Preserve behavior that is intentional and still supported.
3. Separate activation failures from execution failures.
4. Remove duplicated, generic, stale, or unreachable instructions.
5. Move detail out of `SKILL.md` only after adding a clear routing instruction.

### 6. Build evaluations

Create evaluations before considering the skill complete. Cover:

- Direct positive activation.
- Indirect positive activation.
- Incomplete input requiring clarification.
- Near-negative requests that should not activate.
- Ambiguous requests.
- Edge and adversarial cases.
- Coexistence with overlapping skills.
- Output correctness and instruction adherence.

Read [references/evaluation-methodology.md](references/evaluation-methodology.md) and use [assets/eval-cases.template.json](assets/eval-cases.template.json) when creating a reusable evaluation suite.

Keep packaged skill files independent from test-only expected answers. Store evaluation fixtures adjacent to the skill in the owning repository unless the user requests a different layout.

Validate an evaluation suite before execution:

```text
python3 <skill-dir>/scripts/run_evals.py <cases.json>
```

Use `--runner <adapter-command>` to execute cases through a host adapter, or `--results <results.json>` to grade previously captured independent runs.

### 7. Review security

Audit all instructions, scripts, dependencies, paths, network access, and tool combinations before installing or distributing a skill.

Read [references/security-review.md](references/security-review.md) whenever a skill:

- Executes code or shell commands.
- Reads outside its own directory.
- Uses network access or external URLs.
- Calls MCP tools or modifies external systems.
- Handles confidential data.
- Comes from a third party.

Treat third-party skills like software dependencies. Inspect every bundled file before trusting them.

### 8. Validate and iterate

Resolve `<skill-dir>` to this skill's directory, then run:

```text
python3 <skill-dir>/scripts/validate_skill.py /path/to/skill
python3 <skill-dir>/scripts/generate_openai_yaml.py /path/to/skill --check
```

Resolve errors and review warnings. Then run the real workflow evaluations.

When a test fails, change the correct layer:

- Wrong activation: revise `name`, `description`, or overlap boundaries.
- Correct activation but wrong behavior: revise workflow instructions or decision gates.
- Correct behavior but inconsistent output: revise verification or output contract.
- Repeated fragile reasoning: add or improve a deterministic script.
- Missed supporting content: improve resource routing or move essential guidance into `SKILL.md`.
- Regression with other skills: narrow scope, consolidate overlaps, or route skill sets.

Change one conceptual area at a time and rerun the same evaluations so improvements are attributable.

### 9. Forward-test and release

Read [references/forward-testing.md](references/forward-testing.md) for complex, high-risk, shared, or production skills.

Use independent agent contexts when the host permits them. Give each evaluator the skill and a realistic user task, but do not reveal the intended answer, suspected defect, proposed fix, or previous conclusions.

When independent contexts are unavailable:

1. Complete all deterministic checks.
2. Validate the evaluation suite.
3. Record that independent behavioral testing is pending.
4. Do not represent the skill as production-proven.

Release only after structural validation, script tests, behavioral evaluations, coexistence checks, and the required security review pass.

## Decision gates

### Split or keep one skill

Split when candidate workflows have different activation language, inputs, success criteria, permissions, or lifecycle owners. Keep one skill when variants share the same user goal and differ only through a small, explicit decision gate.

### Instructions or script

Prefer instructions when existing tools can perform the operation reliably and context determines the approach. Add a script when exactness, repeatability, validation, or error handling materially improves reliability.

### Reference or main body

Keep content in `SKILL.md` when every successful run needs it. Move content to a reference when it applies only to a domain, variant, file type, provider, or advanced case.

### Ask or infer

Infer reversible, low-impact details. Ask before choosing an installation location, overwriting an existing skill, changing its public interface, granting new tool access, or selecting between materially different workflows.

### Portable or host-specific

Default to the open Agent Skills structure. Add host-specific metadata only when the target host is known, and keep host-specific behavior outside portable frontmatter when possible.

## Tools and resources

- Read [references/authoring-methodology.md](references/authoring-methodology.md) for the complete design lifecycle and quality rationale.
- Read [references/evaluation-methodology.md](references/evaluation-methodology.md) to design activation, behavior, and coexistence tests.
- Read [references/forward-testing.md](references/forward-testing.md) to run uncontaminated independent agent trials.
- Read [references/security-review.md](references/security-review.md) for threat modeling and approval gates.
- Copy [assets/SKILL.template.md](assets/SKILL.template.md) when starting a new skill.
- Copy [assets/eval-cases.template.json](assets/eval-cases.template.json) when creating an evaluation suite.
- Run `scripts/init_skill.py` for atomic scaffolding.
- Run `scripts/generate_openai_yaml.py` to generate or check OpenAI interface metadata.
- Run `scripts/validate_skill.py` for deterministic structural and hygiene checks.
- Run `scripts/run_evals.py` to validate, execute through an adapter, or grade evaluation cases.

## Verification

Before declaring completion:

1. Inspect the final file tree.
2. Read the complete final `SKILL.md`.
3. Run the structural validator.
4. Check that generated host metadata is current.
5. Run every bundled script with representative and invalid inputs.
6. Validate the evaluation suite and grade captured runs.
7. Execute positive, negative, ambiguous, and edge evaluations.
8. Test coexistence when overlapping skills may be active.
9. Confirm version, provenance, license status, and owner metadata.
10. Confirm the output meets the user's requested installation or delivery scope.
11. Report limitations and untested host behavior explicitly.

## Output contract

Provide:

- The skill name and location.
- A concise explanation of its activation boundary.
- The final file tree.
- Validation and evaluation results.
- Version, provenance, and license status.
- Security-relevant dependencies or permissions.
- Any remaining limitations.
- Clear installation or invocation instructions when requested.

Do not include internal reasoning, test-only expected answers, secrets, or claims unsupported by executed validation.

## Failure and escalation

- If the workflow boundary remains materially ambiguous, stop before creating files and ask one focused question.
- If the target directory already contains a skill with the same name, inspect it and ask before destructive replacement.
- If required reference material or assets are unavailable, create only the safe scaffold and identify the missing inputs.
- If a script cannot be executed in the available environment, label it unverified and do not claim readiness.
- If independent forward-testing is unavailable, mark it pending rather than substituting self-review.
- If security review finds unexplained network, credential, or broad filesystem behavior, do not install or distribute the skill until resolved.
