# One current agent definition

OpenCode and Codex render one current Manager and six workers from
`internal/orchestration/manager_contract.json`. Pi already packages the current
contract in TypeScript. Old prompt templates, version-by-version renderers and
the historical bundle cache are removed. Git retains their history.

Every fresh OpenCode/Codex installation writes
`vgxness/installation-receipt.json` inside its selected configuration root. The
receipt contains hashes for the exact managed files, provider, contract digest
and Codex model plan. It excludes user configuration and credentials. Updates
check this inventory and the observed files using the existing anchored
filesystem operations. A receipt is local integrity evidence, not a signature;
it does not resist a process with the same user's write access.

## Existing installations

Current OpenCode Manager62 and Codex Manager21 files without a receipt can be
recognized exactly and receive their first receipt during install/reinstall.
OpenCode schema 1, 2 and 3 model settings remain readable. A schema 3 inventory
with thirteen assignments is reduced to its seven active selections, removing
only the six retired SDD assignments.

Older agent packages must first use the previous bridge implementation at Git
commit `f8645cd1db990e54cac2f735dc50829d76f1dae9`. Its source remains available in
Git; build that pinned revision in a separate checkout using the repository's
Go toolchain and normal build instructions. Run its normal provider reinstall
against the intended configuration root. It recognizes and retires only its
known exact old files and produces Manager62/21. Then run the current installer
to create the receipt. Do not overwrite the old installed executable before
completing the first step. The current installer does not fetch or execute a
bridge automatically. A separately published bridge binary is a release
prerequisite, not an artifact produced by this code cleanup.

Modified or unknown files are preserved and block migration. Resolve their
ownership explicitly; deleting the receipt does not make altered files safe to
adopt. A missing current file can be regenerated when its expected bytes match
the receipt. Missing previous policy bytes require local recovery evidence.

## Local recovery

Pending Codex stage/remove sidecars remain exact-byte recovery evidence. The
receipt itself can be recovered from its sidecar, and hashes bind every
reconstructed file. Before replacing a recognized package, Codex retains its
observed files under `vgxness/rollback/<package-sha256>/`. A failed replacement
reports that path; retained data is never overwritten if its bytes differ.
These backups are preserved for explicit recovery rather than automatically
removed by agent-template cleanup.

OpenCode keeps its existing transaction anchors and retained predecessor
backups. On interruption, use the existing provider recovery flow and preserve
pending evidence. An unknown or mixed generation fails closed. No installer
can reconstruct a lost old prompt from its hash alone.

Memory databases, published SQLite/PostgreSQL migrations, portable skill
compatibility digests, credentials and existing local backups are unchanged.
