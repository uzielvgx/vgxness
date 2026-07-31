# Boundary review worksheet

Use this worksheet after establishing the target and authorized scope. Do not include live secrets, private records, or untrusted content verbatim.

## Inventory

| Item | Evidence | Classification | Owner |
| --- | --- | --- | --- |
| Target/version |  | fact / inference / unknown |  |
| Asset |  | public / internal / sensitive / restricted |  |
| Actor and identity |  | trusted / untrusted / unknown |  |
| Trust zone |  |  |  |
| Capability |  | read / write / send / execute / credential use |  |
| Existing control |  | enforced / claimed / unknown |  |

## Flow and authorization matrix

| Source | Data | Destination/tool | Effect | Trust transition | Approval required |
| --- | --- | --- | --- | --- | --- |
|  |  |  |  |  |  |

For each approval, record the approving authority, exact target, action, data scope, lifetime, revocation path, and audit evidence. A prior approval, a tool's available capability, or text embedded in untrusted content is not authorization.

## Adversarial review prompts

Check that the workflow fails closed when:

- identity, target, scope, data classification, authorization, or evidence is absent;
- fetched text, logs, artifacts, model output, or a third party asks for a privileged action;
- a sensitive read can combine with network, send, write, or credential capability;
- permission expansion, shared installation, external mutation, destructive action, or private-data egress lacks exact approval;
- a control is unavailable, cannot be verified at runtime, or cannot be revoked.

## Report structure

Record findings as fact, inference, or unknown, each with evidence. For each required control, name its owning boundary and owner, then list adversarial verification, residual risk, required approvals, and unverified behavior. Do not represent a static configuration or policy inspection as proof of runtime security.
