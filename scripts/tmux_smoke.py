#!/usr/bin/env python3
"""Test an isolated tmux server; never touch the user's existing sessions."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time

tmux = os.environ.get('TMUX_BINARY') or shutil.which('tmux')
if not tmux:
    raise SystemExit('tmux is needed for this development check')
binary = str(Path(os.environ.get('SHACK_BINARY', 'build/shack')).resolve())
with tempfile.TemporaryDirectory(prefix='shack-tmux-') as folder:
    socket = str(Path(folder) / 'socket')
    path = Path(folder) / 'sample.txt'
    path.write_text('original\n')
    base = [tmux, '-S', socket, '-f', '/dev/null']
    def run(*args, **kwargs):
        return subprocess.run(base + list(args), check=True, capture_output=True, **kwargs)
    try:
        run('new-session', '-d', '-x', '90', '-y', '24', '-s', 'shack', binary, '--no-config', '--no-lsp', str(path))
        for _ in range(100):
            pane = run('capture-pane', '-p', '-t', 'shack').stdout
            if b'Ctrl+E commands' in pane:
                break
            time.sleep(.05)
        else:
            raise AssertionError('Shack did not start in tmux')
        run('send-keys', '-t', 'shack', 'C-a')
        run('load-buffer', '-b', 'shack-test', '-', input='你好\n  pasted through tmux\n'.encode())
        run('paste-buffer', '-p', '-b', 'shack-test', '-t', 'shack')
        run('send-keys', '-t', 'shack', 'C-z', 'C-y', 'C-s')
        for _ in range(100):
            if path.read_text() == '你好\n  pasted through tmux\n':
                break
            time.sleep(.05)
        else:
            raise AssertionError(path.read_text())
        run('send-keys', '-t', 'shack', 'C-q')
        print('tmux startup, UTF-8 bracketed paste, undo/redo and save: PASS')
    finally:
        subprocess.run(base + ['kill-server'], capture_output=True)
