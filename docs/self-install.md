# Versioned self-installation

VGXNESS can install a permanent launcher while keeping every application version immutable and addressable by its SHA-256 digest. Installation is explicit and does not download software or mutate shell configuration.

Release acquisition, artifact names, and checksum verification are documented in [Alpha releases](release.md). Self-installation only manages the already extracted executable: it does not download releases or edit `PATH`.

## Layout

The default managed paths are:

- `~/.local/bin/vgxness` — permanent launcher path.
- `~/.local/bin/vgxness.launcher.json` — atomic active-version pointer and one rollback reference.
- `~/.local/share/vgxness/versions/<sha256>/vgxness` — immutable application version.
- `~/.local/share/vgxness/.manifest-recovery.json` — temporary, create-only recovery evidence retained only while manifest activation is incomplete.

On Windows, the corresponding explicit layout uses `.exe`:

- `<bin-dir>\vgxness.exe` — permanent launcher path.
- `<bin-dir>\vgxness.exe.launcher.json` — atomic active-version pointer and one rollback reference.
- `<data-dir>\versions\<sha256>\vgxness.exe` — immutable application version.

Use `--bin-dir` and `--data-dir` with absolute paths for controlled or test installations.

## Commands

Inspect a candidate without writing:

```sh
./vgxness self preview
```

Install that exact candidate, or atomically activate it when a managed installation already exists:

```sh
./vgxness self install
```

Inspect the managed installation through its permanent path:

```sh
~/.local/bin/vgxness self status
```

Return to the previous immutable version:

```sh
~/.local/bin/vgxness self rollback
```

Rollback is intentionally one level. A successful rollback clears the previous-version reference; version files remain content-addressed on disk.

### Version retention cleanup

Garbage collection is explicit and applies only to verified immutable application versions under the selected self-install data directory. It is never run by `self preview`, `self install`, `self status`, or `self rollback`.

```sh
# Read-only: validate the whole version inventory and print a bound plan digest.
~/.local/bin/vgxness self gc preview

# Delete exactly the versions named by that current plan.
~/.local/bin/vgxness self gc apply --plan-sha256 <lowercase-64-character-sha256>

# Explicitly resolve retained GC evidence after an interrupted collection.
~/.local/bin/vgxness self gc recover
```

`preview` and `apply` print `gc_plan_sha256`, candidate and retained counts, and digest records; `apply` also prints deleted records. `recover` prints recovered records. All commands end with `changed`. Copy the exact lowercase plan SHA-256 from a fresh preview into apply. A missing, malformed, stale, or changed plan is refused without deleting a version. If apply fails after one or more deletions, it prints the validated partial audit result to stdout before the classified error on stderr; retain that output and run `self gc recover` before making another plan. Invalid or empty error results print no GC audit output.

GC always retains the active version and the one rollback predecessor. It validates the entire `versions` inventory before acting: every entry must be a managed digest directory containing exactly its verified executable. Foreign names, links, special files, missing, extra, changed, empty, or oversized content fail closed. The inventory is limited to 1024 entries.

Each deletion is journaled privately through `prepared`, `moving`, `staged`, `deleting`, and `deleted`. A state transition first publishes a fixed private `next` evidence file create-only, then rechecks the canonical evidence identity and bytes before cleanup and republishes the next state create-only. This journal is observable recovery evidence, not a promise that every directory mutation is durable. Recovery recognizes canonical-only, next-only, and matching consecutive canonical-plus-next evidence; inconsistent, replaced, or tampered evidence is retained and fails closed. Recovery is preservation-first: it restores a verified staged version without replacement when appropriate and never unlinks an extant executable. After recovery, discard the old plan and run a fresh `self gc preview` before applying anything.

The same-UID filesystem race boundary still applies. Anchored roots, identity-and-byte rechecks immediately before named cleanup, and no-replace publication reduce detected path replacement risk, but cannot atomically eliminate the post-check/pre-unlink window against a same-UID process holding a writable descriptor; host enforcement is not claimed. A failed post-unlink sync or a failed root sync after stage removal retains recoverable GC evidence. A failed final root sync after journal removal can leave neither journal nor stage, but apply still reports the observed unlink in its error audit. On Windows, directory publication uses a handle-relative no-replace rename rather than a replace-capable pathname move. Directory durability still cannot be given the POSIX `fsync` guarantee; native Windows lifecycle coverage runs in CI.

If a manifest activation is interrupted after durable evidence is recorded,
`self status` reports `state=recovery_pending` and returns a recovery error.
Preserve the reported journal and predecessor bytes, then rerun `self install`
with the same absolute paths. Retry either finalizes the verified candidate or
restores the exact predecessor without overwriting concurrent content.

Windows examples should use absolute paths and the extracted `.exe` candidate:

```powershell
.\vgxness.exe self preview --bin-dir "$HOME\bin" --data-dir "$HOME\vgxness-data"
.\vgxness.exe self install --bin-dir "$HOME\bin" --data-dir "$HOME\vgxness-data"
& "$HOME\bin\vgxness.exe" self status --bin-dir "$HOME\bin" --data-dir "$HOME\vgxness-data"
& "$HOME\bin\vgxness.exe" self rollback --bin-dir "$HOME\bin" --data-dir "$HOME\vgxness-data"
```

## Safety properties

- Preview is non-mutating, and install/update is explicit.
- Existing unmanaged launchers, manifests, symlinks, or modified managed artifacts are refused as conflict or drift.
- Version directories are named by the verified binary SHA-256 and are never rewritten.
- Activation replaces only the small manifest after the new version is durable and verified.
- Manifest activation records the exact candidate and predecessor before changing the active pointer; incomplete publication remains explicitly recoverable.
- The launcher validates the exact manifest, active path, and active hash before replacing its process. It never invokes a shell.
- The managed launcher path remains constant across updates and rollback, so persistent integrations do not embed an ephemeral build or version path.
- The installer does not edit `PATH`, download a release, or overwrite foreign files; only explicit GC removes verified unprotected versions.
- Installation roots reject symlink ancestors before mutation and remain anchored for transaction writes; a replaced root fails closed or retains writes in the originally opened directory.

If `~/.local/bin` is not already on `PATH`, invoke the permanent launcher by its absolute path or update the shell environment separately.

## CARE lifecycle boundary

CARE recognizes exact predecessors only for lifecycle and upgrade handling; it does not make them current aliases or change self-install activation. See [CARE architecture](care.md) for markers and authority, and [CARE evaluation](care-evaluation.md) for static-check limits.
