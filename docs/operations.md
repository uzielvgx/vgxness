# Operational inspection and retention

VGXNESS exposes read-only inventory separately from destructive maintenance:

```sh
vgxness doctor --deep --workspace /absolute/project/path
vgxness orchestrate list --workspace /absolute/project/path
vgxness maintenance prune --workspace /absolute/project/path --older-than 720h
vgxness maintenance prune --workspace /absolute/project/path --older-than 720h --apply
vgxness edit inspect --workspace /absolute/project/path --ticket ticket-...
vgxness edit approve --workspace /absolute/project/path --ticket ticket-... --manifest sha256-... --actor NAME
vgxness edit integrate --workspace /absolute/project/path --ticket ticket-... --manifest sha256-... --actor NAME
vgxness edit retire --workspace /absolute/project/path --ticket ticket-... --manifest sha256-... --actor NAME
vgxness edit discard --workspace /absolute/project/path --ticket ticket-... --manifest sha256-... --actor NAME
```

`doctor --deep` verifies the current Chronicle pointer plus every recognized orchestration, native ticket, and native lease. It returns `doctor=degraded` and a nonzero exit when it finds blocked/stale work, expired or reclaimable execution state, malformed documents, or unsafe storage entries.

`orchestrate list` emits the verified orchestration inventory as JSON.

`maintenance prune` is a dry run by default. Retention must be between 24 hours and 10 years. Only terminal orchestration and ticket documents strictly older than the cutoff are candidates. Tickets referenced by retained orchestrations or active leases are protected. Applying a prune acquires each document's normal control-plane lock, re-reads and revalidates the candidate, removes only its JSON document, and preserves lock files, Chronicle evidence, memory, authority state, logs, and non-terminal work. Any corrupt inventory blocks pruning.

The `edit` lifecycle is the only supported path from a completed native `write-files` artifact into the canonical checkout. `inspect` reports the durable artifact and state. `approve` binds an operator to the exact manifest and base commit. `integrate` accepts only that approval, requires a clean checkout at the same base, applies only the approved files, and leaves them unstaged. `retire` removes the isolated worktree only after verifying the integrated content; `discard` removes an artifact that has not been integrated. All mutation actions require the manifest and actor explicitly and are safely replayable.
