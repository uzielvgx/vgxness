# Contract documentation worksheet

Use after locating an authoritative interface source. OpenAPI concepts can structure HTTP contracts; they do not prove runtime behavior.

| Item | Record | Evidence or unknown |
| --- | --- | --- |
| Interface | Audience, owner, version, authoritative source | |
| Surface | Endpoint/event, inputs, outputs, schemas | |
| Auth | Supported method and safe placeholder | |
| Semantics | Errors, pagination, idempotency, rate limits, delivery/order only if evidenced | |
| Examples | Minimal request, response, event, SDK use; no secrets | |
| Evolution | Compatibility, deprecation, migration policy/status | |
| Maintenance | Reviewer, publication, release/discrepancy trigger | |

Do not use this worksheet to generate an API, SDK, or OpenAPI definition, or to infer unstated runtime behavior.
