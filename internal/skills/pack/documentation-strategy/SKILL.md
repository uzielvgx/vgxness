---
name: documentation-strategy
description: Decides what documentation a software product needs, audits documentation gaps, and defines audiences, ownership, lifecycle, and maintenance; use for documentation strategy, documentation audits, doc inventories, information architecture, document ownership, maintenance plans, or deciding whether product, architecture, operations, API, accessibility, or legal documentation is applicable. Do not use to author a specific document, API reference, tutorial, runbook, architecture diagram, legal notice, or accessibility conformance claim; route that artifact to its specialized skill or accountable domain owner.
license: MIT
compatibility: Agent Skills hosts with repository and configured-tool inspection
metadata:
  version: "1"
  provenance: "VGXNESS portable global skill"
---

# Documentation strategy

Establish an evidence-based, proportionate documentation portfolio for a software product; do not assume a universal document set.

Read [Documentation taxonomy and planning worksheet](references/documentation-taxonomy.md) when classifying artifacts or producing an inventory.

## Inputs and preconditions

Establish the product scope and maturity; intended audiences and tasks; delivery and support model; architecture and interfaces; operational risks; applicable commitments; existing evidence and documentation; owners; change cadence; and constraints. Label unavailable evidence as unknown rather than infer that an artifact is required or unnecessary.

## Hard rules

- Start from decisions, user tasks, risks, interfaces, and obligations—not a generic checklist.
- Classify each candidate artifact as **baseline**, **audience/architecture/operations/contract/legal conditional**, or **not applicable**, with evidence, owner, lifecycle, and review trigger.
- A standard, framework, or method can inform a decision but does not by itself impose a universal named document set.
- Keep claims proportionate to evidence. Do not make legal, regulatory, certification, accessibility-conformance, security, or operational-readiness claims.
- Preserve evidence links and decision rationale so later maintainers can revisit applicability.

## Workflow

1. **Frame the product.** Identify product boundaries, audiences, supported tasks, lifecycle stage, evidence sources, and accountable owners.
2. **Inventory evidence and gaps.** Compare what each audience must decide or do with current materials. Distinguish absent evidence, stale material, duplicate material, and an actual documentation gap.
3. **Classify the portfolio.** Use the reference taxonomy to mark each candidate baseline, conditional, or not applicable. For every conditional artifact, record the trigger and evidence; for every exclusion, record the rationale and re-evaluation trigger.
4. **Assign maintenance.** Define the owner, source of truth, publication location, review cadence, change events, approval needs, and retirement or archival path for every selected artifact.
5. **Prioritize and report.** Sequence work by audience impact, delivery risk, uncertainty, and cost. State assumptions, dependencies, unresolved ownership, and items that require specialist review.

## Boundaries

- Route actual Agent Skill package authoring to `skills-creator` and evaluation-suite design or grading to `agent-evaluation`.
- Route API or interface reference authoring to the relevant API or platform owner; route tutorials and task instructions to their documentation authoring owner.
- Route architecture diagrams and technical design decisions to the architecture owner; route runbooks, incident procedures, and reliability practices to the operations/SRE owner.
- Route contracts, notices, privacy material, regulatory interpretation, and accessibility conformance work to qualified legal, compliance, privacy, or accessibility owners. This skill identifies a possible need; it does not author or approve those artifacts.

## Decision gates

- If audience, product scope, ownership, or evidence is missing, produce a bounded discovery plan rather than a definitive portfolio.
- If no trigger supports a conditional artifact, mark it not applicable for the current scope and name the event that would reopen the decision.
- If an artifact crosses a specialist boundary, record the need and owner; do not substitute a generic template or claim completion.

## Verification

Check that every selected artifact has an audience, purpose, evidence, owner, lifecycle, and maintenance trigger; that each conditional decision names its trigger; and that exclusions are revisitable. Validate portfolio coverage against actual user tasks and product changes, not document count.

## Output contract

Provide scope and evidence; audience/task map; current-state gaps; a portfolio table with classification, rationale, owner, lifecycle, and trigger; prioritized next actions; specialist handoffs; assumptions; and unresolved decisions. Clearly distinguish recommendations from verified product facts.

## Failure and escalation

Do not manufacture a document backlog from a framework. Escalate missing ownership, unknown obligations, conflicting evidence, or a claim needing qualified review; report the uncertainty and the decision needed.
