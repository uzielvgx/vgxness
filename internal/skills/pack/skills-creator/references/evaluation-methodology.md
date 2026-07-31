# Agent Skill Evaluation Methodology

## Contents

- Evaluation layers
- Case design
- Metrics
- Grading
- Runner contract
- Coexistence testing
- Iteration protocol
- Release gate

## Evaluation layers

Evaluate activation and execution independently.

### Layer 1: Discovery

Measure whether the host selects the skill for the correct requests.

Include:

- Direct positives.
- Paraphrased positives.
- Implicit positives that mention the goal but not the skill name.
- Near negatives sharing vocabulary.
- Clearly unrelated negatives.
- Ambiguous and incomplete requests.

### Layer 2: Workflow

When the skill is activated, measure:

- Correct input discovery.
- Correct decision path.
- Required tool usage.
- Respect for hard rules.
- Handling of missing information.
- Verification before completion.
- Output contract compliance.

### Layer 3: Robustness

Test:

- Empty and malformed inputs.
- Large or unusual artifacts.
- Tool failures.
- Missing dependencies.
- Conflicting user constraints.
- Unsupported requests.
- Attempts to bypass safety or verification.

### Layer 4: Coexistence

Activate the normal inventory of adjacent skills and rerun discovery cases. Check whether the new skill:

- Steals requests from a more specific skill.
- Is shadowed by a broader skill.
- Causes multiple workflows to run unnecessarily.
- Changes tool selection or output quality in unrelated tasks.

### Layer 5: Efficiency

Track:

- Context loaded.
- References read.
- Tool calls.
- Repeated operations.
- Latency.
- Token or compute cost when available.

Efficiency is a guardrail, not a substitute for task success.

## Case design

Start with at least:

- 3 direct positives.
- 3 indirect positives.
- 3 near negatives.
- 2 incomplete or ambiguous cases.
- 2 edge or adversarial cases.
- 2 coexistence cases for each likely overlapping skill.

Expand the suite with every real failure discovered.

Keep a separate development and holdout set for mature or automatically optimized skills. Do not tune against every case and then report the same cases as evidence of generalization.

Each case should define:

- Stable case ID.
- Natural user prompt.
- Available context or fixture.
- Expected activation state.
- Required behaviors.
- Forbidden behaviors.
- Output checks.
- Risk level.
- Applicable hosts or models.

Avoid specifying one exact prose answer unless exact text is a product requirement. Prefer observable criteria.

## Metrics

### Activation precision

`true positive activations / all activations`

Low precision means the description is too broad or overlaps another skill.

### Activation recall

`true positive activations / all requests that should activate`

Low recall means the description is vague, missing user language, or truncated among too many skills.

### Workflow pass rate

`cases satisfying every critical behavior / activated workflow cases`

Track critical and non-critical failures separately.

### Output validity

Use deterministic validators for schemas, files, formatting, required fields, and numerical invariants.

### Safety violation rate

Count any unsupported action, invented fact, secret exposure, skipped approval, or forbidden tool call as a critical failure.

### Coexistence regression

Compare baseline performance of adjacent skills before and after introducing the candidate skill.

## Grading

Prefer deterministic checks when possible:

- File exists.
- Schema parses.
- Required section present.
- Command exits successfully.
- Value falls within a valid range.
- No forbidden path or network call appears.

Use rubric-based model grading for semantic qualities such as completeness, relevance, clarity, or justification. Calibrate model graders against human-reviewed examples and periodically audit disagreements.

Use binary pass/fail criteria for release-blocking requirements. Use ordinal scores only when intermediate quality is meaningful and graders can distinguish levels consistently.

Record raw outputs, tool traces, validator results, and final artifacts. Do not rely only on a summary score.

## Runner contract

Use `scripts/run_evals.py` with JSON cases. Keep cases outside the packaged skill when they contain test-only expectations.

An adapter receives one case object as JSON on standard input and returns one result object:

```json
{
  "id": "direct-positive-001",
  "activated": true,
  "satisfied_behaviors": [
    "Observable requirement copied exactly from the case"
  ],
  "violations": [],
  "validator_passed": true,
  "output": "Optional raw or summarized output"
}
```

Use exact behavior strings so deterministic grading can compare declared requirements with observed evidence. Let the adapter or an external grader decide which semantic behaviors were satisfied; keep the aggregation and release gate deterministic.

Run:

```text
python3 <skill-dir>/scripts/run_evals.py <cases.json> --runner <adapter-command>
```

To grade previously captured independent runs:

```text
python3 <skill-dir>/scripts/run_evals.py <cases.json> --results <results.json>
```

Treat adapter output as evidence, not ground truth. Audit raw artifacts and traces for high-risk cases.

## Coexistence testing

Build an overlap map:

| Candidate skill | Adjacent skill | Shared language | Correct routing rule |
|---|---|---|---|
| new skill | existing skill | keywords | distinguishing user goal |

For each overlap, create paired prompts differing by the smallest meaningful boundary. Example:

- “Diagnose the failing GitHub Actions check” should select a CI repair skill.
- “Review this PR for maintainability” should select a review skill.

If routing remains unreliable:

1. Narrow descriptions.
2. Give the more specific skill distinctive trigger language.
3. Consolidate only when the workflows share inputs and success criteria.
4. Route separate skill inventories by task domain when the host supports it.

## Iteration protocol

When improving a skill:

1. Freeze the current evaluation suite.
2. Record a baseline.
3. Classify failures by activation, workflow, output, tool, safety, or coexistence.
4. Change one conceptual layer.
5. Rerun the full suite.
6. Inspect regressions, not only aggregate improvement.
7. Add new real-world failures to the suite.
8. Validate on holdout cases before release.

Avoid leaking the intended fix, expected answer, or diagnosis into a fresh agent evaluation.

## Release gate

Block release for:

- Invalid structure.
- Broken bundled scripts.
- Any critical safety failure.
- Unsupported external action.
- Missing verification on critical outputs.
- Material coexistence regression.
- Unresolved high-risk security finding.

Document accepted limitations for:

- Host-specific behavior not available to test.
- Minor stylistic variance.
- Optional integrations.
- Known low-risk edge cases with an explicit fallback.
