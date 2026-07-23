# Operational inspection and retention

VGXNESS exposes read-only inventory separately from destructive maintenance:

```sh
vgxness doctor --deep --workspace /absolute/project/path
vgxness orchestrate list --workspace /absolute/project/path
vgxness maintenance prune --workspace /absolute/project/path --older-than 720h
vgxness maintenance prune --workspace /absolute/project/path --older-than 720h --apply
```

`doctor --deep` verifies the current Chronicle pointer plus every recognized orchestration, native ticket, and native lease. It returns `doctor=degraded` and a nonzero exit when it finds blocked/stale work, expired or reclaimable execution state, malformed documents, or unsafe storage entries.

`orchestrate list` emits the verified orchestration inventory as JSON.

`maintenance prune` is a dry run by default. Retention must be between 24 hours and 10 years. Only terminal orchestration and ticket documents strictly older than the cutoff are candidates. Tickets referenced by retained orchestrations or active leases are protected. Applying a prune acquires each document's normal control-plane lock, re-reads and revalidates the candidate, removes only its JSON document, and preserves lock files, Chronicle evidence, memory, authority state, logs, and non-terminal work. Any corrupt inventory blocks pruning.
