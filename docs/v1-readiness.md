# V1 release acceptance

**State: proposed v1.0.0.** The user authorized the first stable release and
command-line distribution. This file defines its accepted scope; it does not
assert that the tag or packages have already been published.

## Stable scope

VGXNESS provisions OpenCode, Codex and Pi, installs portable skills and provides
shared SQLite memory. Pi executes its services in TypeScript/Node without a
running VGXNESS process or Go executable. OpenCode and Pi expose one model for
all seven agents or per-agent choices; only Codex retains model plans.

Structured SDD is retired. The current workflow uses the Manager, native workers
and proportional CARE review. Historical database records remain data, not
active workflow authority. There is one current definition per agent role;
receipt-backed updates preserve unknown or modified files.

## Evidence and release gates

- Main `609cc0b47c3976685236d5feee2d0a6d0f4c850a` passed standard CI and was
  installed on the development host for all three integrations. This is baseline
  evidence, not a substitute for validation of the release commit.
- PR #394's final CI run [34677479703](https://github.com/uzielvgx/vgxness/actions/runs/34677479703)
  passed every lane, including native Linux installation, Windows installation,
  Darwin smoke, all six Pi Node/OS combinations, coverage and race checks.
- The exact v1 tag must pass standard validation and native Linux amd64,
  Windows amd64 and Darwin arm64 archive smokes before GitHub publication.
- Homebrew and Scoop manifests are generated from the release archive checksums.
  Publish their channels only after reading back the matching release assets;
  validate each channel on its native package manager.

## Support and accepted limits

| Target | v1 scope |
| --- | --- |
| Linux amd64, Windows amd64, macOS arm64 | Release-gated native archive installation |
| Linux arm64, macOS amd64, Windows arm64 | Distributed binaries; exact-tag native archive smoke is not part of the release gate |
| Pi tools and shared memory | Node 22.19+; compatible Pi host dependencies required |
| Pi workers | Linux/macOS; Windows process-tree ownership remains unsupported |

Trusted hosts and repositories are required. Agent instructions are not an OS
sandbox. Extended ACL and verification-to-execution identity limits remain;
package checksums establish integrity relative to the trusted publisher, not
independent signatures. Binaries are not code-signed or notarized.

Model-driven behavior is not deterministic. Historical Pi development cohorts
included failed skill-selection and evaluation-writing cases. The user chose
not to continue those account-based evaluations or add automatic model review;
v1 does not relabel those failures as passes. A full behavioral holdout and
large dirty-worktree recovery remain follow-up work, not v1 guarantees.

The intermittent historical Windows startup failure has no proven root cause.
Retain it as an unresolved report; do not promise it fixed because CI is green.
The consolidated current follow-up remains [#383](https://github.com/uzielvgx/vgxness/issues/383).

## Upgrade and recovery

See [release and installation](release.md) for Homebrew, Scoop and direct archive
commands, and [self-installation](self-install.md) for one-level binary rollback.
Existing receipt-backed integrations retain selections and foreign settings.
Older unreceipted installations require the documented bridge; v1 does not
force-overwrite drift. Application rollback is not a database downgrade.
No user data or historical memories are deleted by publishing this release.
