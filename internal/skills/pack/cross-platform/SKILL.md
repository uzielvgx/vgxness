---
name: cross-platform
description: Designs, implements, debugs, tests, or reviews software behavior across operating systems and architectures; use when a request involves Linux, macOS, Windows, portable paths, filesystems, processes, shells, permissions, build constraints, CI matrices, or platform capability claims; do not use for responsive UI, single-platform bugs without a portability boundary, release-store workflows, or installer lifecycle ownership.
license: MIT
compatibility: Agent Skills hosts with repository inspection and platform-appropriate build or test tools
metadata:
  version: "1"
  provenance: "VGXNESS portable global skill"
---

# Cross-platform

Make platform behavior explicit, portable where possible, and honestly verified.

## Inputs and preconditions

Establish the supported OS/architecture matrix from repository configuration, CI, release policy, or user evidence. Record the affected runtime, build targets, capability assumptions, and whether a reported failure is compile-time or runtime.

## Hard rules

- Do not infer support from the host, a build tag, or cross-compilation alone.
- Do not treat paths, case sensitivity, permissions, `chmod`, `fsync`, symlinks, rename atomicity, process signals, shells, encodings, or newline handling as uniform.
- Prefer portable standard-library abstractions. Isolate unavoidable platform-specific code behind a small, documented boundary.
- Do not take ownership of installer transactions. Route a one-OS install, rollback, or uninstall lifecycle primarily to `installer-lifecycle`; use both only when that lifecycle genuinely has cross-OS semantics.

## Workflow

1. **Map support.** Identify supported and unsupported OS/architecture pairs, the source of each claim, and CI or release coverage. Mark missing evidence as untested.
2. **Locate boundaries.** Inspect paths, filesystem operations, process launch and quoting, shell use, permissions, links, atomic replacement, durability, text encoding/newlines, build constraints, and environment discovery.
3. **Choose behavior.** Use portable APIs and normalized inputs where they preserve the contract. For platform-specific behavior, isolate it, define capability detection and fallback or a clear unsupported error, and avoid silently weakening safety.
4. **Test separately.** Compile or type-check each supported target where available, then run target-native tests for runtime semantics. Exercise OS-specific branches, filesystem/process edge cases, and CI matrix configuration independently.
5. **Report limits.** State the matrix exercised, evidence for each claim, fallback behavior, and every target or capability not tested. A successful Windows cross-build is not Windows runtime verification.

## Decision gates

- If the support matrix is absent, do not invent one; derive it from repository evidence or request the minimal decision.
- If a capability differs, preserve the invariant first, then use an explicit fallback or unsupported result rather than emulation by assumption.
- If a proposed fix depends on shell syntax or permissions, test that boundary on the target OS or label it unverified.

## Verification

Verify path and filesystem behavior with target-native tests where possible; verify build constraints independently from runtime behavior; inspect CI matrix coverage; and confirm error/fallback paths for unsupported capabilities.

## Output contract

Provide the supported matrix and its evidence, platform-sensitive boundaries, selected abstraction or isolated implementation, compile and runtime results separately, fallbacks, and untested platform limits.

## Failure and escalation

Stop before making an unsupported platform claim, broad compatibility promise, or destructive workaround. Escalate missing matrix policy, unavailable target access, or a lifecycle-owned installer change to the appropriate owner.
