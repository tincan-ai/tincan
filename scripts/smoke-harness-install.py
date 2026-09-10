#!/usr/bin/env python3
"""Install into a disposable harness profile and probe the actual installed plugin."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('smoke', ROOT / 'scripts/smoke-plugin.py')
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('archive', type=Path)
    p.add_argument('--host', choices=['codex', 'claude'], required=True)
    args = p.parse_args()
    with tempfile.TemporaryDirectory(prefix='tincan isolated harness ') as tmp:
        root = Path(tmp)
        market = root / 'marketplace'
        plugin = smoke.extract(args.archive, market / 'plugins')
        for source in (ROOT / 'marketplace').rglob('marketplace.json'):
            target = market / source.relative_to(ROOT / 'marketplace')
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, target)
        profile = root / 'profile'; profile.mkdir()
        env = dict(os.environ)
        env['CODEX_HOME' if args.host == 'codex' else 'CLAUDE_CONFIG_DIR'] = str(profile)
        if args.host == 'codex':
            commands = [['codex', 'plugin', 'marketplace', 'add', str(market), '--json'],
                        ['codex', 'plugin', 'add', 'tincan@tincan', '--json']]
        else:
            commands = [['claude', 'plugin', 'marketplace', 'add', str(market)],
                        ['claude', 'plugin', 'install', 'tincan@tincan']]
        for command in commands:
            result = subprocess.run(command, env=env, cwd=root, capture_output=True, text=True, timeout=45)
            assert result.returncode == 0, result.stdout + result.stderr
        if args.host == 'codex':
            installed = Path(json.loads(result.stdout)['installedPath'])
        else:
            candidates = list((profile / 'plugins/cache').rglob('.mcp.json'))
            assert len(candidates) == 1, candidates
            installed = candidates[0].parent
        assert (installed / 'release.json').exists(), 'harness installed source rather than release'
        runtime_env = smoke.clean_env(root / 'runtime state')
        manifest = '.codex-plugin/plugin.json' if args.host == 'codex' else '.mcp.json'
        smoke.probe(smoke.command_for(installed, manifest), runtime_env, root)
        print(f'{args.host}: installed through plugin manager and started installed MCP server successfully; no model calls or user profile changes.')


if __name__ == '__main__': main()
