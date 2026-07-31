---
name: software-architecture-docs
description: Creates, reviews, and maintains evidence-bound system context and architecture views, technical design documents, ADRs, quality attributes, deployment/runtime/data-flow descriptions, and trade-off records; use for explaining or deciding software structure and its consequences. Do not use for product requirements, code review, implementation execution, API reference, operations runbooks, or unsupported security or readiness claims.
license: MIT
compatibility: Agent Skills hosts with repository and configured-tool inspection
metadata:
  version: "1"
  provenance: "VGXNESS portable global skill"
---

# Software architecture docs

Create or assess durable, evidence-bound explanations and decisions about a software system's structure, behavior, and trade-offs.

Read [Architecture view worksheet](references/architecture-view-worksheet.md) when selecting views, recording an ADR, or assessing a technical design.

## Inputs and preconditions

Establish the system boundary and version; stakeholders and decisions; existing code, diagrams, interfaces, and runtime evidence; quality attributes; constraints; deployment and operational context; data classifications and flows where known; alternatives; decision owner; and unknowns. Treat absent runtime or security evidence as unknown.

## Hard rules

- Select views for the stakeholder decision they support; do not complete a template or diagram set by default.
- Separate observed facts, proposals, assumptions, decisions, and unknowns, with evidence and version context.
- Record material alternatives, trade-offs, consequences, owners, and revisiting triggers for an architecture decision.
- Describe interfaces and data flows at the needed boundary, but do not substitute an API reference or operational procedure.
- Do not claim production readiness, security, compliance, performance, resilience, or runtime enforcement without relevant observed evidence.

## Workflow

1. **Frame the architecture question.** Identify the system boundary, stakeholders, decision, version, constraints, quality attributes, and available evidence.
2. **Select and build views.** Choose only useful context, container/component, runtime, deployment, and data-flow views. State purpose, scope, notation, facts, and unknowns for each.
3. **Record decisions.** For each ADR or technical design decision, capture context, options, decision, rationale, consequences, owner, status, and re-evaluation trigger.
4. **Assess trade-offs.** Connect choices to quality attributes, constraints, risks, dependencies, and unresolved evidence without overstating results.
5. **Maintain the record.** Define source links, version, review owner, and events that invalidate a view or decision.

## Boundaries

- Route product outcomes, scope, non-goals, and acceptance criteria to `product-requirements`.
- Route code-level review and implementation execution to the accountable engineering workflow.
- Route API schemas and reference material to the relevant interface owner, and operations runbooks to the operations/SRE owner.
- Route formal security, privacy, compliance, accessibility, and readiness assessment to qualified owners; this skill records architecture evidence and gaps, not certification.

## Decision gates

- If system boundary, stakeholder decision, version, or evidence is missing, produce a bounded discovery plan rather than a definitive architecture document.
- If a proposed product outcome determines the decision, obtain product requirements before selecting an architecture.
- If a view would imply unverified runtime behavior or security/readiness status, mark it unknown and name the evidence or owner needed.

## Verification

Check that each view has a purpose, scope, version, evidence, and owner; each decision records alternatives and consequences; data and deployment claims are traceable; quality attributes are tied to trade-offs; and unverified security or readiness claims are absent.

## Output contract

Provide the architecture question and boundary; stakeholder decisions; evidence and unknowns; selected views; ADR or design record; quality attributes and trade-offs; interfaces/data/deployment context; consequences and risks; owners; and review triggers. Clearly distinguish current facts from proposals.

## Failure and escalation

Do not invent a system model, runtime behavior, or assurance claim. Escalate absent boundaries, conflicting evidence, unowned decisions, unsupported quality claims, or a required specialist assessment; report the gap and needed evidence.
