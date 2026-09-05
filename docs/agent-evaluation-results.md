# Evaluation results

Date: 2026-09-05. These are bounded development evaluations, not a reliability score or a production-readiness certification.

## Provider trials

Twelve independently reviewed provider trials covered scoped memory actions, exact retrieval, skill loading, a greeting, restored SQLite retrieval, and delegated marker reading.

| Provider | Result |
| --- | --- |
| Codex | 5 PASS, 1 INCONCLUSIVE |
| OpenCode | 6 PASS |

Codex delegation is inconclusive because the retained protocol lacks the required spawn, child identity, and child-return evidence. This is an evidence gap, not proof of a product failure. OpenCode retained a native task event and returned marker. Codex used version 0.153.3 with `gpt-6-astra` at high effort; OpenCode used version 1.18.27 with `openai/gpt-5.6-sol` and `--pure`, which excluded its ordinary lifecycle plugins and hooks.

Each provider case ran once as a development trial. Provider versions and model configurations differed, so these results do not support a controlled provider comparison, a holdout result, or a broader reliability claim. The local runner catalogue remains development-only and does not automatically execute these provider integrations.

## PostgreSQL and HTTP integration

The Go suite ran on Windows against PostgreSQL 17.11 in Linux Docker and an actual TCP HTTP listener with the configured backend. The full package run recorded 109 top-level passing tests, zero failures, and four skips because a supported Linux execution host was not used.

Observed coverage included authenticated capabilities, push and pull of a written mutation, revocation denial, listener shutdown, conflicting observations, replay, pull watermarks, and resolution against PostgreSQL. The skipped administrative permission and listener checks remain unvalidated on a supported Linux execution host.

## Two-client journey

Two separate Linux CLI processes used separate stores, workspaces, configuration, and device credentials. Their path was CLI through locally verified HTTPS to the actual sync service and PostgreSQL. The development journey was reproduced once by an independent native verifier against the retained package.

The retained journey showed bidirectional exact record retrieval, rejection of an untrusted lab CA before proxy access, process-scoped trust of the lab CA, offline local save/read, and recovery without duplicates after explicit foreground retries. An initial successful `synced` status did not immediately make the queued record visible to the other client; the later retry did.

Concurrent topic updates produced a reported conflict. Resolution and convergence without losing competing content are **INCONCLUSIVE**, so the journey does not establish conflict convergence, automatic background delivery, cross-host behavior, Windows keyring enrollment, or production-network safety.

## Evidence and assurance

The provider evidence package digest is `61353388b329aa80d317cbe7ab9382275aee1154c9255768965ef50e1969033d`. The later two-client evidence package digest is `0fe26423d0aced64463c8d2f95238f584c08bf9aa0554a443ef06c7e57ee190c`.

For the two-client package, local offline rebuilds matched the retained Linux executable bytes. The primary CARE reviewer and specialist accepted the scoped provenance, TLS, redaction, isolation, and cleanup evidence. That assurance concerns the stated evidence package; it does not convert the unresolved functional convergence outcome into a pass.

All retained raw traces, credentials, CA material, temporary harnesses, and local paths remain outside this repository. This document contains only sanitized findings, package identities, and stated limitations.

## Runner availability

`tools/agent_eval/runner.py` provides opt-in local development trace transport and offline self-test coverage. Its pending catalogue rows mean that automated adapters are not available in this increment. They do not erase or replace the separately executed manual integration artifacts summarized here.

Future work should capture complete Codex delegation traces, exercise OpenCode lifecycle hooks, run the Linux-only administrative checks on a supported host, make pending delivery visible, and establish a public conflict-resolution journey that proves convergence and preservation of competing values.

Reusable principle: verify the destination record and its exact content; a successful synchronization status alone does not prove completed delivery.
