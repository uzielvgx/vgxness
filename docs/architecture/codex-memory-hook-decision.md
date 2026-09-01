# Decision: Codex per-session memory hook remains blocked

**Status:** blocked pending an upstream capability contract.

## Context and evidence boundary

This decision concerns a provider hook that would need a stable, per-session identity outside prompt input. The following statements are evidence supplied for this decision, not independently verified official upstream documentation: Codex documents a current-working-directory guarantee; `session_id` is available only in stdin JSON; and no per-session argv, environment variable, placeholder, or out-of-band hook capability is documented. No official source was directly verified for this record.

Repository facts independently visible in the integration documentation are narrower: Codex uses a user-owned `config.toml`, the local stdio MCP boundary has no caller identity or session authentication, and managed installation does not prove automatic memory injection or a Codex handshake. See [Codex integration](../codex-integration.md).

## Decision

Do not implement or publish a Codex memory hook based on a static token, process-only derivation, project singleton, or total fail-closed behavior. Each fails to establish the required per-session binding: static and project values conflate sessions, process identity is not a documented session identity, and total failure supplies no usable integration.

The current-working-directory statement is not a substitute for session identity. It may identify workspace context only within its documented guarantee. The stdin `session_id` is input data, not evidence of a supported per-session hook channel outside stdin.

## Re-evaluation trigger

Re-evaluate only when an official, stable Codex capability outside stdin directly documents a per-session identity channel suitable for the required hook, or when scope explicitly removes the per-session requirement. The re-evaluation MUST update the Provider Capability Contract with the provider version, direct official source, trust boundary, invalid-input-zero-mutation proof, platform behavior, and recovery/replay semantics before implementation resumes.

Until then, dependent implementation and PRs remain stopped under the provider-integration preflight rule. This record makes no claim about undiscovered, experimental, or future Codex capabilities.
