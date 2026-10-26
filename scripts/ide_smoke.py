#!/usr/bin/env python3
"""Exercise the project workflow in an isolated tmux server."""
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
with tempfile.TemporaryDirectory(prefix='shack-ide-test-') as folder:
    root = Path(folder) / 'project'
    root.mkdir()
    for name in ('one.txt', 'two.txt'):
        (root / name).write_text('alpha\nsecond line\n')
    base = [tmux, '-S', str(Path(folder) / 'socket'), '-f', '/dev/null']
    def run(*args):
        return subprocess.run(base + list(args), check=True, capture_output=True)
    def keys(*args):
        run('send-keys', '-t', 'shack', *args)
    def type_text(value):
        run('send-keys', '-l', '-t', 'shack', value)
    def pane():
        return run('capture-pane', '-p', '-t', 'shack').stdout.decode()
    def wait(needle):
        for _ in range(150):
            text = pane()
            if needle in text:
                return text
            time.sleep(.04)
        raise AssertionError(f'missing {needle!r}:\n{text}')
    def command(value):
        keys('F2')
        type_text(value)
        keys('Enter')
    try:
        run('new-session', '-d', '-x', '110', '-y', '30', '-s', 'shack', binary, '--no-config', '--no-lsp', '--no-session', '--no-recovery', str(root), str(root / 'one.txt'))
        wait('Ctrl+E commands')
        command('Split vertically')
        wait('2 · one.txt')
        command('Toggle soft wrapping')
        wait('Soft wrapping: true')
        command('Preview replace in project')
        wait('Replace in project: find')
        type_text('alpha')
        keys('Enter')
        type_text('beta')
        keys('Enter')
        wait('Space toggle')
        keys('Enter')
        wait('Changed 2 buffers')
        assert (root / 'one.txt').read_text().startswith('alpha')
        command('Undo last workspace edit')
        wait('Workspace undo/redo applied')
        command('Redo last workspace edit')
        command('Save all')
        wait('Saved all buffers')
        for name in ('one.txt', 'two.txt'):
            assert (root / name).read_text() == 'beta\nsecond line\n'
        command('Run shell command')
        wait('Run shell command in project')
        type_text("printf 'one.txt:2:1: task result\\n'")
        keys('Enter')
        wait('[completed successfully]')
        command('Task output locations')
        wait('Task output locations')
        keys('Enter')
        wait('Shack  2:1')
        keys('C-q')
        print('tmux splits, wrapping, project preview, group undo/redo, save-all and task navigation: PASS')
    finally:
        subprocess.run(base + ['kill-server'], capture_output=True)
