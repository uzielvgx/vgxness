---
name: product-requirements
description: Defines, reviews, and maintains evidence-bound product briefs, PRDs, feature specifications, user outcomes, scope, non-goals, requirements, and acceptance criteria; use for deciding what a product or feature must achieve and how success is observed. Do not use for architecture choices, implementation plans, backlog administration, API reference, or fabricated research, metrics, or commitments.
license: MIT
compatibility: Agent Skills hosts with repository and configured-tool inspection
metadata:
  version: "1"
  provenance: "VGXNESS portable global skill"
---

# Product requirements

Create or assess a testable statement of product intent and boundaries without prescribing its technical solution.

Read [Requirements worksheet](references/requirements-worksheet.md) when creating a PRD or resolving scope, outcome, or acceptance-criterion ambiguity.

## Inputs and preconditions

Establish the product or feature boundary; affected users and their context; problem or opportunity; available evidence; intended outcomes; constraints; current behavior; stakeholders and decision owner; dependencies; risks; and change history. Label missing, disputed, or inferred information as unknown.

## Hard rules

- Describe user and business outcomes before requirements, and requirements before solution options.
- Keep scope, non-goals, assumptions, dependencies, risks, and open questions explicit.
- Write observable acceptance criteria that distinguish required behavior from examples or future ideas.
- Preserve evidence and uncertainty; do not invent user research, usage metrics, market facts, commitments, or approvals.
- Do not turn a product requirement into an implementation plan, architecture decision, task backlog, API contract, or delivery estimate.

## Workflow

1. **Frame the decision.** State the product boundary, decision owner, affected users, current problem, desired outcome, and evidence quality.
2. **Define intent and boundaries.** Record goals, in-scope outcomes, non-goals, constraints, assumptions, dependencies, risks, and unresolved questions.
3. **Specify behavior.** Express user journeys, functional and quality requirements, and observable acceptance criteria. Separate mandatory behavior from options, examples, and later work.
4. **Review for coherence.** Trace each requirement to an outcome, evidence, constraint, or explicitly labeled assumption; find contradictions, untestable language, and scope creep.
5. **Maintain the record.** Record the decision, source evidence, owner, revision trigger, and changes to scope or acceptance criteria.

## Boundaries

- Route technical structure, trade-offs, runtime views, and architecture decisions to `software-architecture-docs`.
- Route implementation sequencing, estimates, task decomposition, and backlog administration to the delivery or product-management owner.
- Route API or protocol reference to the relevant interface owner.
- Request qualified research, legal, accessibility, security, privacy, or compliance review when evidence or a claim requires it; this skill does not fabricate or certify it.

## Decision gates

- If the user, outcome, scope, or decision owner is unknown, produce a bounded discovery request rather than a definitive PRD.
- If a requirement implies a technical choice, retain the product need and route the choice to architecture.
- If acceptance cannot be observed from stated evidence, mark it provisional and name the evidence or decision needed.

## Verification

Check that every requirement supports a named outcome or constraint; every acceptance criterion is observable; scope and non-goals do not conflict; evidence, assumptions, and unknowns are distinguishable; and architecture, implementation, API, and backlog work are not presented as product requirements.

## Output contract

Provide the product boundary; users and problem; evidence and uncertainty; outcomes; scope and non-goals; requirements and acceptance criteria; constraints, dependencies, risks, and assumptions; open decisions; owner; and maintenance trigger. Clearly label proposed content versus verified facts.

## Failure and escalation

Do not invent certainty to complete a brief. Escalate missing ownership, conflicting evidence, unsupported commitments, untestable acceptance, or a specialist claim; state the blocker and the decision or evidence required.
