---
name: installer-lifecycle
description: Designs, implements, debugs, tests, or reviews local software install, update, reinstall, rollback, uninstall, repair, activation, manifests, ownership, recovery, and durable artifact replacement; use when lifecycle transactions or partial failures own the behavior; do not use for package-manager usage, app-store release, ordinary application-data migration, generic deployment, or a pure cross-platform path bug without lifecycle ownership.
license: MIT
compatibility: Agent Skills hosts with repository inspection and explicitly authorized local artifact operations
metadata:
  version: "1"
  provenance: "VGXNESS portable global skill"
---

# Installer lifecycle

Protect local artifacts through explicit ownership, recoverable states, and no-destructive-default operations.

## Inputs and preconditions

Establish the requested operation, user authorization, artifact inventory and owners, exact managed predecessor identities, target locations, lifecycle states, concurrency model, and durability guarantees of the host filesystem.

## Hard rules

- Start with read-only preflight. Do not overwrite, remove, or restore unknown, modified, or unowned bytes.
- Define states for absent, staged, published, active, rollback-pending, recovery-pending, drifted, and uninstalled artifacts as applicable.
- Stage on safe filesystem boundaries; use no-overwrite publication and restoration; preserve exact backups only when their identity is verified.
- Make cancellation, concurrent replacement, partial failure, rollback, retry, and recovery reporting explicit. Never invent success.
- Uninstall only exact owned artifacts and leave foreign, modified, or ambiguous content intact for inspection.
- Do not introduce a speculative installer framework or bundled executable script. Destructive actions require current user authorization and a verified target.

## Workflow

1. **Inventory and preflight.** Read current state, hashes or equivalent identities, owners, permissions, free-space and boundary constraints. Reject drift, conflicts, ambiguous ownership, or unavailable prerequisites without mutation.
2. **Model the transition.** Define valid source and destination states, activation point, durable checkpoints, cancellation behavior, exact rollback inputs, and retry/recovery entry points.
3. **Stage and publish.** Create candidate artifacts in a safe location, verify content and metadata, then publish with no-overwrite semantics. Re-read durable state where the platform supports it.
4. **Recover deliberately.** On failure, restore only the verified predecessor without overwriting concurrent changes. Retain and report pending state when recovery cannot be proven.
5. **Uninstall or repair.** Re-run read-only ownership checks, remove only exact managed artifacts, and report retained foreign or modified files with next safe actions.

## Decision gates

- If exact ownership or predecessor bytes are unavailable, stop with drift or conflict; do not guess a rollback target.
- If replacement can race, verify identity immediately before publication and restoration; a changed target stops the transaction.
- If filesystem durability is not supported, disclose the limit and preserve recovery evidence rather than claiming crash safety.

## Verification

Test preflight, first install, update, idempotent reinstall, rollback, cancellation, partial failure, retry, concurrent replacement, permission failure, recovery-pending, and uninstall of exact versus modified artifacts. Verify durable readback where supported.

## Output contract

Provide inventory and ownership evidence, lifecycle states and transition result, authorized mutations, verification/readback results, rollback or recovery status, retained artifacts, and durability or platform limitations.

## Failure and escalation

Stop on missing authorization, unknown ownership, modified bytes, unsafe target boundaries, or unproven recovery. Route an OS-specific capability question to `cross-platform`; use both skills only when the installer lifecycle itself must behave across OSes.
