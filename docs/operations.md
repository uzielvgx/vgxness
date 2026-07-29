# Operational inspection and retention

> **Status: Compatibility-only.** These commands inspect and maintain the legacy bridge/control-plane, ticket, lease, isolated-edit, and Delivery Authority state. They remain supported CLI/maintainer surfaces but are not the active installed OpenCode scheduler.

VGXNESS separates compatibility inventory from destructive maintenance:

```sh
vgxness doctor --deep --workspace /absolute/project/path
vgxness orchestrate list --workspace /absolute/project/path
vgxness maintenance prune --workspace /absolute/project/path --older-than 720h
vgxness maintenance prune --workspace /absolute/project/path --older-than 720h --apply
vgxness edit inspect --workspace /absolute/project/path --ticket ticket-...
vgxness edit review --workspace /absolute/project/path --ticket ticket-... --review-manifest review-manifest.json
vgxness edit approve --workspace /absolute/project/path --ticket ticket-... --manifest sha256-... --receipt RECEIPT_ID --actor NAME
vgxness edit integrate --workspace /absolute/project/path --ticket ticket-... --manifest sha256-... --actor NAME
vgxness edit retire --workspace /absolute/project/path --ticket ticket-... --manifest sha256-... --actor NAME
vgxness edit discard --workspace /absolute/project/path --ticket ticket-... --manifest sha256-... --actor NAME
```

`doctor --deep` verifies the current Chronicle pointer plus every recognized orchestration, native ticket, and native lease. It returns `doctor=degraded` and a nonzero exit when it finds blocked/stale work, expired or reclaimable execution state, malformed documents, or unsafe storage entries.

`orchestrate list` emits the verified orchestration inventory as JSON.

`maintenance prune` is a dry run by default. Retention must be between 24 hours and 10 years. Only terminal orchestration and ticket documents strictly older than the cutoff are candidates. Tickets referenced by retained orchestrations or active leases are protected. Applying a prune acquires each document's normal control-plane lock, re-reads and revalidates the candidate, removes only its JSON document, and preserves lock files, Chronicle evidence, memory, authority state, logs, and non-terminal work. Any corrupt inventory blocks pruning.

Within the compatibility isolated-edit subsystem, the `edit` lifecycle is the supported path from a completed `write-files` artifact into the canonical checkout. `inspect` reports the durable artifact and state. `review` issues a Delivery Authority receipt over the exact isolated artifact. `approve` requires that active receipt and binds its candidate tree and review digest to the operator, manifest, and base commit. `integrate` revalidates the receipt before mutation, requires a clean checkout at the same base, applies only the approved files, and leaves them unstaged. `retire` removes the isolated worktree only after verifying the integrated content; `discard` removes an artifact that has not been integrated. These commands do not describe the active native SDD apply path, where the read-only apply agent composes a hash-bound patch and the manager alone writes and validates the workspace.

The default `~/.vgxness/memory.db` is shared by isolated semantic-memory and structured SDD domains. After a binary upgrade from schema v4, read-only status and inspection commands cannot perform the v5 migration and may fail until one write-capable memory or SDD operation opens the database. Never delete the database; run that operation, then rerun status. See [Native memory](memory.md#upgrade-migration-caveat).
