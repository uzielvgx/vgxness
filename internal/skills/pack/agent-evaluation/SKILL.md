---
name: agent-evaluation
description: Designs, runs, grades, or improves evaluations for agents, Agent Skills, prompts, tool workflows, traces, activation precision and recall, holdouts, regressions, rubric graders, or model comparisons; use when defining evaluation semantics and evidence for an existing agent, skill, or workflow; do not use to author or restructure a SKILL.md package, diagnose a failing CI job, or replace independent evidence with self-grading.
license: MIT
compatibility: Agent Skills hosts with a trusted evaluation adapter or captured run evidence
metadata:
  version: "1"
  provenance: "VGXNESS portable global skill"
---

# Agent evaluation

Build evaluation evidence that measures the intended behavior without teaching the system the answers.

## Inputs and preconditions

Establish the evaluated target and version, intended users and decisions, success and harm criteria, available traces or outputs, evaluation environment, and whether execution or grading needs user authorization. Tool capability does not grant permission to run models, access data, or mutate an external host.

## Hard rules

- Define the target behavior and success criteria before selecting a score or dataset.
- Separate development cases used for tuning from protected holdouts used only for evaluation. Do not leak holdout prompts, answers, graders, or failure labels into tuning context.
- Include representative direct, indirect, negative, adversarial, and coexistence cases; test activation precision as well as recall when routing matters.
- Use deterministic assertions for observable facts and calibrated rubric grading only for judgment that cannot be asserted deterministically. Record rubric, uncertainty, and grader limitations.
- Require independent runs and preserve prompts, versions, configurations, outputs, traces, tool evidence, and grading decisions needed to reproduce conclusions.
- Do not treat self-grading, a single aggregate score, a rerun, or an anecdote as sufficient evidence.

## Workflow

1. **Frame the decision.** Name the target, intended behavior, risk, success threshold, baseline, and decision that the evaluation will support.
2. **Design the dataset.** Sample representative production-like requests plus positive, negative, ambiguous, adversarial, and overlapping-skill cases. Partition development and holdout data before tuning.
3. **Specify measurements.** Bind each case to deterministic assertions or a rubric with anchors, allowed evidence, and separate graders where practical. Measure routing precision/recall, behavior quality, safety, cost, and latency only when relevant.
4. **Run independently.** Freeze target and evaluator versions, execute independent trials, capture traces and tool evidence, and record failures without silently retrying them away.
5. **Analyze and report.** Compare against the baseline, classify failures by activation, instruction, tool, environment, grader, or target behavior; report uncertainty, slices, regressions, and holdout results separately.

## Boundaries

- Use `skills-creator` to author, restructure, package, or security-review an Agent Skill; use `agent-evaluation` to evaluate an existing skill, agent, prompt, or workflow.
- Use `ci-triage` to diagnose the failing CI run, job, configuration, infrastructure, or cascade even when the job executes evaluations. This skill may define evaluation semantics for that diagnosis.
- Use `cross-platform` or `installer-lifecycle` for their repairs, and `stacked-pr` only after the evaluation-backed change is ready for delivery.

## Decision gates

- If the decision, target, or success criterion is unspecified, stop before scoring and obtain it from repository or user evidence.
- If a case or grader could influence tuning, exclude it from the holdout and record the partition change.
- If runs, traces, versions, or independent grading are unavailable, report the evaluation as incomplete rather than conclusive.

## Verification

Validate dataset partitions, case schema, target and baseline identity, assertion determinism, rubric calibration, trace retention, independent-run provenance, and regression comparison. Report unexecuted host-adapter behavior as pending rather than proven.

## Output contract

Provide the target and decision, dataset and holdout policy, case coverage, assertions and rubrics, run and trace provenance, baseline comparison, failure classification, uncertainty, regressions, and the exact evidence supporting each conclusion.

## Failure and escalation

Stop before claiming a result when the target, baseline, protected holdout, grading evidence, or authorization is missing. Escalate sensitive data access, external execution, or host mutations for explicit user authorization.
