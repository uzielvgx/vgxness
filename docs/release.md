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
| `darwin/amd64` | Preview / compile-only | Cross-built archive; native CI smoke covers the CLI and focused filesystem/runtime packages, but no native release-archive smoke. |
| `darwin/arm64` | Preview / compile-only | Cross-built archive; native CI smoke covers the CLI and focused filesystem/runtime packages, but no native release-archive smoke. |

The complete repository test suite runs on Linux and Windows in CI. Windows also runs the native self-install lifecycle; macOS runs a native CLI smoke and focused filesystem/runtime package tests.

## Release process

1. Update `CHANGELOG.md` and confirm the intended tag is strict `v`-prefixed SemVer.
2. Run `make verify`; native Windows installation evidence remains CI-only.
3. Create and push the tag. Branch pushes do not create releases.
4. The tag workflow calls the complete standard validation workflow at the exact tagged SHA while independently deriving the exact 40-character commit and its committer RFC3339 date and running `go run ./cmd/vgxness-release` with those values.
5. Publication remains blocked on standard validation, complete asset construction, checksum verification, and native Linux amd64 and Windows amd64 install paths before publishing the six archives and `SHA256SUMS` with `gh release create --verify-tag`.

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

### Homebrew on macOS or Linux

Install the release pinned by the official tap:

```sh
brew install uzielvgx/tap/vgxness
"$(brew --prefix vgxness)/bin/vgxness" version
```

Homebrew selects the matching macOS or Linux ARM64 or amd64 archive and verifies its formula SHA-256 before installation. The formula follows the support levels above; availability through Homebrew does not promote a preview target to alpha-supported.

Homebrew owns its executable and does not modify OpenCode. Preview and explicitly install the optional managed OpenCode integration with the brewed version:

```sh
"$(brew --prefix vgxness)/bin/vgxness" setup opencode --preview
"$(brew --prefix vgxness)/bin/vgxness" setup opencode --yes
```

The setup wizard creates a separate immutable installation and launcher under `~/.local`. After `brew upgrade vgxness`, rerun the preview and approved setup commands to activate the upgraded managed version. Removing the formula does not remove that separate managed installation. The formula source and complete ownership guidance live in the [official Homebrew tap](https://github.com/uzielvgx/homebrew-tap).

### Scoop on Windows

With [Scoop](https://scoop.sh) installed, register the official bucket and install VGXNESS:

```powershell
scoop bucket add vgxness https://github.com/uzielvgx/scoop-bucket
scoop install vgxness/vgxness
& "$(scoop prefix vgxness)\vgxness.exe" version
```

Scoop selects the matching Windows amd64 or ARM64 ZIP and verifies the downloaded archive against the SHA-256 pinned in the manifest. Windows amd64 is alpha-supported; Windows ARM64 remains preview and compile-only.

Scoop owns its app directory and `vgxness` shim and does not modify OpenCode. Preview and explicitly install the optional managed OpenCode integration with the Scoop-owned version:

```powershell
& "$(scoop prefix vgxness)\vgxness.exe" setup opencode --preview
& "$(scoop prefix vgxness)\vgxness.exe" setup opencode --yes
```

The setup wizard creates a separate managed installation and launcher. After `scoop update vgxness`, rerun the preview and approved setup commands to activate the upgraded managed version. Removing the Scoop app does not remove that separate managed installation. The manifest source and complete ownership guidance live in the [official Scoop bucket](https://github.com/uzielvgx/scoop-bucket).

### Direct archive

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
