#!/usr/bin/env python3
"""Exercise first publication and update against a disposable local Git remote."""
from pathlib import Path
import json
import shutil
import subprocess
import sys
import tempfile
import unittest
import zipfile

SCRIPT = Path(__file__).with_name('publish-marketplace.py')


def git(root, *args):
    return subprocess.run(['git', *args], cwd=root, capture_output=True, text=True, check=True).stdout.strip()


class PublishTest(unittest.TestCase):
    def test_new_branch_and_update_include_binaries_without_source(self):
        with tempfile.TemporaryDirectory(prefix='tincan-publish-test-') as tmp:
            root = Path(tmp)
            remote = root / 'remote.git'; remote.mkdir(); git(remote, 'init', '--bare')
            source = root / 'source'; source.mkdir(); git(source, 'init', '-b', 'main')
            git(source, 'config', 'user.name', 'Test'); git(source, 'config', 'user.email', 'test@example.com')
            git(source, 'remote', 'add', 'origin', str(remote))
            (source / 'scripts').mkdir(); shutil.copy2(SCRIPT, source / 'scripts' / SCRIPT.name)
            for path in ('.agents/plugins/marketplace.json', '.claude-plugin/marketplace.json'):
                file = source / 'marketplace' / path; file.parent.mkdir(parents=True, exist_ok=True)
                manifest = {'plugins': [{'name': 'tincan', 'source': {'source': 'local', 'path': './plugins/tincan'}}]} if path.startswith('.agents') else {'plugins': [{'name': 'tincan', 'source': './plugins/tincan'}]}
                file.write_text(json.dumps(manifest))
            (source / 'plugins/tincan').mkdir(parents=True)
            (source / 'plugins/tincan/README.md').write_text('Source plugin')
            (source / '.gitignore').write_text('plugins/tincan/bin/\n')
            (source / 'source-only.go').write_text('package source\n')
            git(source, 'add', '.'); git(source, 'commit', '-m', 'Source'); git(source, 'push', '-u', 'origin', 'main')
            artifact = root / 'plugin.zip'
            codex_artifact = root / 'codex.zip'
            for payload in ('first', 'second'):
                with zipfile.ZipFile(artifact, 'w') as bundle:
                    bundle.writestr('tincan/bin/tincan', '#!/bin/sh\n')
                    bundle.writestr('tincan/bin/linux-amd64/tincan', payload)
                    bundle.writestr('tincan/scripts/launch.sh', '#!/bin/sh\n')
                with zipfile.ZipFile(codex_artifact, 'w') as bundle:
                    bundle.writestr('tincan/bin/tincan', '#!/bin/sh\n')
                    bundle.writestr('tincan/bin/linux-amd64/tincan', 'codex-' + payload)
                    bundle.writestr('tincan/scripts/launch.sh', '#!/bin/sh\n')
                    bundle.writestr('tincan/.codex-plugin/mcp.json', '{"mcpServers":{"tincan":{"tool_timeout_sec":3660}}}')
                result = subprocess.run([sys.executable, str(source / 'scripts' / SCRIPT.name), str(artifact), '--codex-archive', str(codex_artifact)],
                                        cwd=source, capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                files = git(remote, 'ls-tree', '-r', '--name-only', 'plugin-release').splitlines()
                self.assertIn('plugins/tincan/bin/linux-amd64/tincan', files)
                self.assertIn('.agents/plugins/marketplace.json', files)
                self.assertNotIn('source-only.go', files)
                self.assertNotIn('.gitignore', files)
                self.assertEqual(git(remote, 'show', 'plugin-release:plugins/tincan/bin/linux-amd64/tincan'), payload)
                self.assertTrue(git(remote, 'ls-tree', 'plugin-release', 'plugins/tincan/bin/tincan').startswith('100755'))
                self.assertEqual(git(remote, 'show', 'plugin-release:plugins/tincan-codex/bin/linux-amd64/tincan'), 'codex-' + payload)
                self.assertTrue(git(remote, 'ls-tree', 'plugin-release', 'plugins/tincan-codex/bin/tincan').startswith('100755'))
                codex_market = json.loads(git(remote, 'show', 'plugin-release:.agents/plugins/marketplace.json'))
                self.assertEqual(codex_market['plugins'][0]['source']['path'], './plugins/tincan-codex')
                claude_market = json.loads(git(remote, 'show', 'plugin-release:.claude-plugin/marketplace.json'))
                self.assertEqual(claude_market['plugins'][0]['source'], './plugins/tincan')
            checkout = root / 'harness-checkout'
            git(root, 'clone', '-b', 'main', str(remote), str(checkout))
            # Codex performs this plain checkout after cloning the source branch.
            # A branch named plugins is ambiguous with the plugins/ directory.
            git(checkout, 'checkout', 'plugin-release')
            self.assertTrue((checkout / 'plugins/tincan/bin/tincan').exists())
            self.assertEqual(git(source, 'status', '--porcelain'), '')


if __name__ == '__main__': unittest.main()
