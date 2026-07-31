# Documentation taxonomy and planning worksheet

Use this reference after framing the product. It is a decision aid, not a required document list.

## Classification

| Class | Candidate artifacts | Select when | Do not infer |
| --- | --- | --- | --- |
| Baseline | product overview, audience/task map, ownership and change record | A product needs a shared explanation, accountable maintenance, or a way to revisit decisions | That every artifact must be separate or public |
| Audience conditional | onboarding, tutorials, how-to guidance, explanations, release notes | A named audience must learn, accomplish, understand, or track a change | That all Diátaxis-style categories apply to every product |
| Architecture conditional | context/container/component views, decision records, dependency or data-flow explanations | System structure, technical decisions, integration boundaries, or change impact need durable shared understanding | That C4 or arc42 mandates every view or template section |
| Operations conditional | deployment guide, runbook, service ownership, SLO/error-budget material, incident or recovery procedure | A service is operated, supported, monitored, recovered, or handed over | That a non-operated product needs an operations manual |
| Contract conditional | API/interface reference, schemas, compatibility/versioning policy, integration guide | Others depend on a stable interface, protocol, SDK, export, or behavioral contract | That internal-only or transient interfaces need public reference material |
| Legal conditional | terms, notices, privacy material, accessibility or regulatory documentation | A qualified owner identifies an applicable obligation or commitment | Legal applicability, compliance, certification, or conformance |
| Not applicable | any candidate above | No current audience, task, risk, interface, operation, or obligation supports it | Permanent exclusion; name a re-evaluation trigger |

## Planning worksheet

| Candidate | Classification | Audience and decision/task | Evidence and trigger | Owner/source of truth | Lifecycle and review event | Gap/next action |
| --- | --- | --- | --- | --- | --- | --- |
|  |  |  |  |  |  |  |

Use observable product changes as review events: audience changes, releases, interface changes, architectural decisions, operational ownership changes, incidents, support patterns, commitments, and retirement.

## Reference methods and boundaries

- [ISO/IEC/IEEE 12207](https://www.iso.org/standard/63712.html) describes software life-cycle processes; use it as contextual evidence, not as a universal named-document checklist.
- [Diátaxis](https://diataxis.fr/) distinguishes tutorials, how-to guides, reference, and explanation; select categories that match real audience needs.
- [C4 model](https://c4model.com/) and [arc42](https://arc42.org/) offer architecture communication structures; tailor views and sections to the decisions they support.
- [The Scrum Guide](https://scrumguides.org/scrum-guide.html) supports lightweight, evolving product knowledge; it does not prescribe a documentation portfolio.
- [Google SRE workbook](https://sre.google/workbook/table-of-contents/) informs service-operation material when a product is operated.
- [NIST](https://www.nist.gov/) and [WCAG](https://www.w3.org/TR/WCAG22/) may inform specialist review; do not use this taxonomy to make compliance or accessibility claims.

These sources support conditional classification only. Obtain qualified review before asserting legal, regulatory, contractual, security, or accessibility conclusions.
