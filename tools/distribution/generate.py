"""Render checksum-pinned stable-release package manifests; never install or publish."""
import argparse
import json
from pathlib import Path
import re

TARGETS = [(f'{os}_{arch}', 'zip' if os == 'windows' else 'tar.gz')
           for os in ('darwin', 'linux', 'windows') for arch in ('amd64', 'arm64')]
BASE = 'https://github.com/uzielvgx/vgxness/releases/download'


def render(tag, checksums):
    if not re.fullmatch(r'v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)', tag):
        raise ValueError('a stable v-prefixed SemVer is required')
    version = tag[1:]
    hashes = {}
    for line in checksums.splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  ([A-Za-z0-9_.+-]+)', line)
        if not match or match[2] in hashes:
            raise ValueError('invalid or duplicate checksum entry')
        hashes[match[2]] = match[1]
    assets = {}
    for target, suffix in TARGETS:
        name = f'vgxness_{version}_{target}.{suffix}'
        if name not in hashes:
            raise ValueError('missing platform checksum')
        assets[target] = (f'{BASE}/{tag}/{name}', hashes[name])
    lines = ['class Vgxness < Formula',
             '  desc "Set up AI coding agents and shared project memory"',
             '  homepage "https://github.com/uzielvgx/vgxness"',
             f'  version "{version}"', '  license "MIT"', '']
    for os, block in [('darwin', 'on_macos'), ('linux', 'on_linux')]:
        lines.append(f'  {block} do')
        for arch, arch_block in [('amd64', 'on_intel'), ('arm64', 'on_arm')]:
            url, digest = assets[f'{os}_{arch}']
            lines.extend([f'    {arch_block} do', f'      url "{url}"', f'      sha256 "{digest}"', '    end'])
        lines.extend(['  end', ''])
    lines.extend(['  def install', '    bin.install "vgxness"', '  end', '',
                  '  test do', f'    assert_match "version={tag}", shell_output("#{{bin}}/vgxness version")',
                  '  end', 'end', ''])
    manifest = {'version': version, 'description': 'Set up AI coding agents and shared project memory',
                'homepage': 'https://github.com/uzielvgx/vgxness', 'license': 'MIT',
                'architecture': {}, 'bin': 'vgxness.exe'}
    for arch, key in [('amd64', '64bit'), ('arm64', 'arm64')]:
        url, digest = assets[f'windows_{arch}']
        manifest['architecture'][key] = {'url': url, 'hash': digest,
                                         'extract_dir': f'vgxness_{version}_windows_{arch}'}
    return '\n'.join(lines), manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    parser.add_argument('--checksums', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    formula, manifest = render(args.version, args.checksums.read_text(encoding='utf-8'))
    args.output.mkdir()  # New private staging destination; never replace existing files.
    (args.output / 'vgxness.rb').write_text(formula, encoding='utf-8')
    (args.output / 'vgxness.json').write_text(json.dumps(manifest, indent=2) + '\n', encoding='utf-8')


if __name__ == '__main__':
    main()
