#!/usr/bin/env python3
"""Exercise fold/source semantics with the actual terminal application."""
import json
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
with tempfile.TemporaryDirectory(prefix='shack-fold-test-') as folder:
    root = Path(folder)
    source = root / 'sample.go'
    original = 'func example() {\n  if true {\n    println("hidden 世界")\n  }\n}\nvar after = 1\n'
    source.write_text(original)
    config = root / 'config.json'
    config.write_text(json.dumps({'file_tree': 'off', 'auto_complete': False}))
    base = [tmux, '-S', str(root / 'socket'), '-f', '/dev/null']
    def run(*args):
        return subprocess.run(base + list(args), check=True, capture_output=True)
    def keys(*args):
        run('send-keys', '-t', 'shack', *args)
    def type_text(value):
        run('send-keys', '-l', '-t', 'shack', value)
    def pane():
        return run('capture-pane', '-p', '-t', 'shack').stdout.decode()
    def until(predicate, message):
        for _ in range(180):
            if predicate():
                return
            time.sleep(.04)
        raise AssertionError(message + '\n' + pane())
    def wait(text):
        until(lambda: text in pane(), 'missing ' + repr(text))
    def palette(name):
        keys('F2')
        type_text(name)
        keys('Enter')
    try:
        run('new-session', '-d', '-x', '110', '-y', '28', '-s', 'shack', binary,
            '--no-lsp', '--config', str(config), '--no-session', '--no-recovery', str(source))
        wait('Ctrl+E commands')
        keys('F6')
        wait('1 collapsed regions')
        assert 'hidden 世界' not in pane()
        wait('var after = 1')
        keys('Down')
        wait('Shack  6:1')
        keys('C-g')
        type_text('3')
        keys('Enter')
        wait('hidden 世界')
        palette('Fold all')
        wait('2 collapsed regions')
        assert 'hidden 世界' not in pane()
        palette('Split vertically')
        wait('2 · sample.go')
        palette('Unfold all')
        wait('All folds expanded')
        view = pane()
        # The child in the right pane is visible; the left stays folded.
        assert sum('hidden 世界' in row for row in view.splitlines()) == 1
        keys('C-s')
        wait('Saved sample.go')
        assert source.read_text() == original, 'folding changed the file'
        keys('C-q')
        print('tmux nested folding, visible-row movement, hidden-line jump, independent splits and unchanged save: PASS')
    finally:
        subprocess.run(base + ['kill-server'], capture_output=True)
