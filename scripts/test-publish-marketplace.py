#!/usr/bin/env python3
"""Exercise first publication and update against a disposable local Git remote."""
from pathlib import Path
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
                file = source / 'marketplace' / path; file.parent.mkdir(parents=True, exist_ok=True); file.write_text('{}')
            (source / '.gitignore').write_text('plugins/tincan/bin/\n')
            (source / 'source-only.go').write_text('package source\n')
            git(source, 'add', '.'); git(source, 'commit', '-m', 'Source'); git(source, 'push', '-u', 'origin', 'main')
            artifact = root / 'plugin.zip'
            for payload in ('first', 'second'):
                with zipfile.ZipFile(artifact, 'w') as bundle:
                    bundle.writestr('tincan/bin/tincan', '#!/bin/sh\n')
                    bundle.writestr('tincan/bin/linux-amd64/tincan', payload)
                    bundle.writestr('tincan/scripts/launch.sh', '#!/bin/sh\n')
                result = subprocess.run([sys.executable, str(source / 'scripts' / SCRIPT.name), str(artifact)],
                                        cwd=source, capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                files = git(remote, 'ls-tree', '-r', '--name-only', 'plugins').splitlines()
                self.assertIn('plugins/tincan/bin/linux-amd64/tincan', files)
                self.assertIn('.agents/plugins/marketplace.json', files)
                self.assertNotIn('source-only.go', files)
                self.assertNotIn('.gitignore', files)
                self.assertEqual(git(remote, 'show', 'plugins:plugins/tincan/bin/linux-amd64/tincan'), payload)
                self.assertTrue(git(remote, 'ls-tree', 'plugins', 'plugins/tincan/bin/tincan').startswith('100755'))
            self.assertEqual(git(source, 'status', '--porcelain'), '')


if __name__ == '__main__': unittest.main()
