# Forward-Testing Agent Skills

## Contents

- Purpose
- Independence requirements
- Test matrix
- Execution protocol
- Contamination controls
- Review protocol
- Release decisions

## Purpose

Use forward-testing to determine whether a skill generalizes in realistic agent work. Do not use it merely to obtain another opinion about the prose.

Forward-testing answers:

- Does the host discover the skill?
- Does the agent navigate its resources correctly?
- Does the workflow survive incomplete or unexpected inputs?
- Does the output satisfy observable requirements?
- Does the skill coexist with adjacent skills?
- Does behavior remain reliable without leaked expectations?

## Independence requirements

Use a fresh agent context for every independent trial when the host permits it.

Give the evaluator:

- The candidate skill or installed skill path.
- A natural user request.
- Raw task artifacts.
- The normal tools and permissions for the task.

Do not give the evaluator:

- The expected answer.
- The suspected defect.
- The proposed fix.
- Previous evaluation output.
- A hidden summary of the intended workflow.
- Artifacts left by earlier trials.

Phrase the task like a real user request. Prefer:

```text
Use $candidate-skill to complete <realistic task>.
```

Avoid:

```text
Review this skill and prove that it follows requirement X.
```

## Test matrix

Run at least:

- One direct positive request.
- One indirect positive request.
- One incomplete request.
- One near-negative request.
- One edge or tool-failure request.
- One coexistence request for each likely overlapping skill.

For high-risk workflows, add:

- Permission denial.
- Network or dependency failure.
- Malformed input.
- Sensitive-data handling.
- Attempted instruction override.
- Destructive-action boundary.

Test every host and model family intended for production. Record unavailable combinations as untested.

## Execution protocol

1. Freeze the skill candidate and evaluation cases.
2. Create a clean workspace or temporary task directory.
3. Install or expose only the skill inventory intended for the trial.
4. Start a fresh agent context.
5. Submit the natural request and raw artifacts.
6. Capture the final output, tool trace, generated artifacts, errors, and timing.
7. Run deterministic validators on produced artifacts.
8. Grade the trial against the predeclared case criteria.
9. Remove generated artifacts before the next trial.
10. Repeat with a fresh context.

Use the case schema linked directly from `SKILL.md`. Keep expected answers outside the packaged skill.

## Contamination controls

Prevent:

- Reusing a conversation containing expected answers.
- Leaving previous outputs where the next agent can discover them.
- Sharing the author’s diagnosis with the evaluator.
- Modifying the skill between cases without recording a new candidate version.
- Grading against criteria invented after seeing the result.
- Training and validating against the same complete case set.

Keep development and holdout cases separate for mature skills. Add production failures to the development set only after recording their original holdout outcome.

## Review protocol

Classify every failure:

- Activation.
- Boundary.
- Workflow.
- Tool selection.
- Resource navigation.
- Verification.
- Output contract.
- Safety.
- Coexistence.
- Environment or dependency.

Inspect raw traces before changing instructions. A failed output may originate from unavailable tools, incorrect installation, missing permissions, or contaminated fixtures rather than the skill text.

Change the smallest responsible layer:

- Metadata for discovery failures.
- Decision gates for routing failures.
- References for missing conditional knowledge.
- Scripts for repeated deterministic errors.
- Verification for invalid artifacts.
- Host configuration for unavailable capabilities.

## Release decisions

Release when:

- Critical cases pass.
- No high-risk security finding remains.
- Coexistence does not materially regress.
- Holdout performance meets the declared acceptance criteria.
- The supported host and model matrix is documented.

Release with limitations when non-critical host combinations remain unavailable and a safe fallback exists.

Block release when:

- Independent trials only pass after leaking the expected answer.
- The skill triggers on unsafe or unrelated requests.
- Critical validation is skipped.
- A script, permission, or dependency remains unverified.
- High-risk behavior lacks an approval gate.

When independent contexts are unavailable, complete deterministic testing and mark forward-testing as pending. Do not represent self-review as an independent trial.
