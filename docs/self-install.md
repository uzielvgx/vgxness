# Versioned self-installation

VGXNESS can install a permanent launcher while keeping every application version immutable and addressable by its SHA-256 digest. Installation is explicit and does not download software or mutate shell configuration.

Release acquisition, artifact names, and checksum verification are documented in [Alpha releases](release.md). Self-installation only manages the already extracted executable: it does not download releases or edit `PATH`.

## Layout

The default managed paths are:

- `~/.local/bin/vgxness` — permanent launcher path.
- `~/.local/bin/vgxness.launcher.json` — atomic active-version pointer and one rollback reference.
- `~/.local/share/vgxness/versions/<sha256>/vgxness` — immutable application version.

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
- The launcher validates the exact manifest, active path, and active hash before replacing its process. It never invokes a shell.
- The managed launcher path remains constant across updates and rollback, so persistent integrations do not embed an ephemeral build or version path.
- The installer does not edit `PATH`, download a release, remove versions, or overwrite foreign files.

If `~/.local/bin` is not already on `PATH`, invoke the permanent launcher by its absolute path or update the shell environment separately.
