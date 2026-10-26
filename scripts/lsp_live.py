#!/usr/bin/env python3
"""Run optional real-server protocol and editor integration checks."""
import argparse
import os
from pathlib import Path
import shutil
import subprocess

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--race', action='store_true', help='enable Go race detection')
args = parser.parse_args()
root = Path(__file__).resolve().parent.parent
env = dict(os.environ, SHACK_LIVE_LSP='1')
cargo = Path.home() / '.cargo' / 'bin'
if cargo.is_dir():
    env['PATH'] = str(cargo) + os.pathsep + env.get('PATH', '')
servers = [
    ('SHACK_GOPLS', 'build/tools/gopls', 'gopls'),
    ('SHACK_RUST_ANALYZER', '', 'rust-analyzer'),
    ('SHACK_PYRIGHT', 'build/lsp-tools/node_modules/.bin/pyright-langserver', 'pyright-langserver'),
    ('SHACK_PYLSP', 'build/pylsp-venv/bin/pylsp', 'pylsp'),
    ('SHACK_TYPESCRIPT_LS', 'build/lsp-tools/node_modules/.bin/typescript-language-server', 'typescript-language-server'),
]
for key, local, program in servers:
    value = env.get(key)
    if not value and local and (root / local).is_file():
        value = str(root / local)
    value = value or shutil.which(program, path=env['PATH'])
    if not value:
        raise SystemExit(f'Missing optional development server {program}; see docs/LANGUAGE_SERVERS.md or set {key}')
    env[key] = value
    print(f'{key}={value}', flush=True)
command = ['go', 'test']
if args.race:
    command.append('-race')
command += ['./internal/lsp', './internal/editor', '-run', 'TestLiveLanguage', '-v', '-count=1', '-timeout=5m']
subprocess.run(command, cwd=root, env=env, check=True)
