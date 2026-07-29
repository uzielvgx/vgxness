# Alpha releases

VGXNESS alpha releases are built from pushed, annotated or lightweight tags that match strict SemVer in the form `vMAJOR.MINOR.PATCH`, optionally followed by prerelease and build metadata. Release archives use the same version without the leading `v`.

## Artifacts

For version `<version>`, a release contains exactly these assets:

- `vgxness_<version>_linux_amd64.tar.gz`
- `vgxness_<version>_linux_arm64.tar.gz`
- `vgxness_<version>_darwin_amd64.tar.gz`
- `vgxness_<version>_darwin_arm64.tar.gz`
- `vgxness_<version>_windows_amd64.zip`
- `vgxness_<version>_windows_arm64.zip`
- `SHA256SUMS`

Each archive expands to one directory named after the archive stem. That directory contains `vgxness` on Linux and macOS or `vgxness.exe` on Windows, plus `LICENSE` and `README.md`.

## Support matrix

| Platform | Alpha support level | Release evidence |
| --- | --- | --- |
| `linux/amd64` | Alpha-supported | Native artifact version and self-install/status smoke in the release workflow. |
| `windows/amd64` | Alpha-supported | Native artifact version and preview/install/status smoke in the release workflow. |
| `linux/arm64` | Preview / compile-only | Cross-built archive; no native release smoke. |
| `windows/arm64` | Preview / compile-only | Cross-built archive; no native release smoke. |
| `darwin/amd64` | Preview / compile-only | Cross-built archive; manually smoke-tested only when it matches the maintainer host. |
| `darwin/arm64` | Preview / compile-only | Cross-built archive; manually smoke-tested only when it matches the maintainer host. |

The complete repository test suite runs on Linux in CI. Windows CI intentionally covers installer and launcher contracts plus the native Windows self-install lifecycle; it does not claim that every repository package test runs natively on Windows.

## Release process

1. Update `CHANGELOG.md` and confirm the intended tag is strict `v`-prefixed SemVer.
2. Run the repository quality gates and tagged E2E suite.
3. Create and push the tag. Branch pushes do not create releases.
4. The tag workflow derives the exact 40-character commit and its committer RFC3339 date, then runs `go run ./cmd/vgxness-release` with those values.
5. The workflow verifies checksums and native Linux amd64 and Windows amd64 install paths before publishing the six archives and `SHA256SUMS` with `gh release create --verify-tag`.

For a local rehearsal from the repository root:

```sh
commit="$(git rev-parse HEAD)"
date="$(git show -s --format=%cI "$commit")"
go run ./cmd/vgxness-release \
  --version v0.1.0-alpha.1 \
  --commit "$commit" \
  --date "$date" \
  --output /absolute/empty-or-new/dist
```

The output directory must be absent or empty. The packager refuses traversal, malformed metadata, symlinks, files, and nonempty output directories. It stages all builds before reserving the output directory, then creates every archive and checksum there exclusively without replacing existing entries. If publication fails or the output changes concurrently, the command fails closed and may leave a partial, nonempty output directory for inspection; remove it or choose a new output path before retrying.

## Verify acquisition

Download the archive for the target platform and `SHA256SUMS` from the same GitHub release. On Unix, from their download directory:

```sh
sha256sum -c SHA256SUMS --ignore-missing
```

On macOS, where `sha256sum` is not normally installed:

```sh
expected="$(awk '$2 == "vgxness_0.1.0-alpha.1_darwin_arm64.tar.gz" { print $1 }' SHA256SUMS)"
actual="$(shasum -a 256 vgxness_0.1.0-alpha.1_darwin_arm64.tar.gz | awk '{ print $1 }')"
test -n "$expected" && test "$actual" = "$expected"
```

In PowerShell:

```powershell
$archive = "vgxness_0.1.0-alpha.1_windows_amd64.zip"
$expected = ((Get-Content SHA256SUMS | Where-Object { $_ -match "  $([regex]::Escape($archive))$" }) -split "\s+")[0]
$actual = (Get-FileHash -Algorithm SHA256 $archive).Hash.ToLowerInvariant()
if (-not $expected -or $actual -ne $expected) { throw "SHA-256 verification failed" }
```

Release binaries are not code-signed or notarized yet. `SHA256SUMS` and acquisition over GitHub HTTPS are the current integrity layer; verify both the repository/release URL and the checksum before executing a binary.

## Install and operate

Extract the selected archive, enter its top-level directory, and inspect its metadata:

```sh
./vgxness version
./vgxness self preview
./vgxness self install
~/.local/bin/vgxness self status
```

Windows uses the `.exe` name and absolute managed paths:

```powershell
.\vgxness.exe version
.\vgxness.exe self preview --bin-dir "$HOME\bin" --data-dir "$HOME\vgxness-data"
.\vgxness.exe self install --bin-dir "$HOME\bin" --data-dir "$HOME\vgxness-data"
& "$HOME\bin\vgxness.exe" self status --bin-dir "$HOME\bin" --data-dir "$HOME\vgxness-data"
```

Use `vgxness self rollback` through the installed launcher to return to its one recorded previous version. See [Versioned self-installation](self-install.md) for layout, drift handling, and rollback details.

## Release rollback boundaries

Application rollback is local and one level deep; it changes the active immutable binary and does not alter a Git tag or GitHub release. If a published release is unsafe, maintainers may mark it as a prerelease, delete the GitHub release, and separately decide whether to delete the remote tag. Deleting a release does not remove installations, archives already downloaded by users, or the Git object. Tags must never be silently moved or reused; publish a new patch or prerelease tag for corrected code.
