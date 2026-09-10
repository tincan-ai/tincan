#!/usr/bin/env python3
"""Publish a verified universal bundle on the dedicated plugin-release branch."""
import argparse
from pathlib import Path
import shutil
import subprocess
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def git(*args, cwd=ROOT):
    return subprocess.run(['git', *args], cwd=cwd, check=True, capture_output=True, text=True).stdout.strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('archive', type=Path)
    args = parser.parse_args()
    archive = args.archive.resolve()
    with tempfile.TemporaryDirectory(prefix='tincan-publish-') as tmp:
        tree = Path(tmp) / 'checkout'
        git('clone', '--no-checkout', '--shared', str(ROOT), str(tree))
        git('remote', 'set-url', 'origin', git('remote', 'get-url', 'origin'), cwd=tree)
        # The parent Actions checkout supplies the credential only to Git, never
        # to artifact files. Reuse its local HTTP authorization for the push.
        headers = subprocess.run(['git', 'config', '--get-regexp', r'http\..*\.extraheader'],
                                 cwd=ROOT, capture_output=True, text=True)
        for line in headers.stdout.splitlines():
            key, value = line.split(' ', 1); git('config', key, value, cwd=tree)
        if git('ls-remote', '--heads', 'origin', 'plugin-release', cwd=tree):
            git('fetch', 'origin', 'plugin-release', cwd=tree)
            git('checkout', '-B', 'plugin-release', 'FETCH_HEAD', cwd=tree)
        else:
            git('checkout', '--orphan', 'plugin-release', cwd=tree)
        # An orphan checkout initially inherits the source tree and index too.
        # Remove it before assembling this generated branch, including .gitignore.
        git('rm', '-rf', '--ignore-unmatch', '.', cwd=tree)
        with zipfile.ZipFile(archive) as bundle:
            bundle.extractall(tree / 'plugins')
            for entry in bundle.infolist():
                if not entry.is_dir():
                    mode = (entry.external_attr >> 16) & 0o777
                    if mode: (tree / 'plugins' / entry.filename).chmod(mode)
        for source in (ROOT / 'marketplace').rglob('marketplace.json'):
            target = tree / source.relative_to(ROOT / 'marketplace')
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, target)
        (tree / 'README.md').write_text('# Tincan plugin distribution\n\nInstall Tincan in your harness, then prompt “Connect me to Tincan”.\n\nThis generated branch contains the complete universal plugin. Source: https://github.com/tincan-ai/tincan-plugin\n')
        git('config', 'user.name', 'github-actions[bot]', cwd=tree)
        git('config', 'user.email', '41898282+github-actions[bot]@users.noreply.github.com', cwd=tree)
        git('add', '.', cwd=tree)
        # Git carries the executable bit even when the publisher runs on Windows.
        git('update-index', '--chmod=+x', 'plugins/tincan/bin/tincan', 'plugins/tincan/scripts/launch.sh', cwd=tree)
        git('commit', '-m', f'Publish plugin from {git("rev-parse", "HEAD")}', cwd=tree)
        git('push', 'origin', 'HEAD:refs/heads/plugin-release', cwd=tree)
        print('Published the verified universal plugin to the plugin-release branch.')


if __name__ == '__main__': main()
