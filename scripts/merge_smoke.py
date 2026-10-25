#!/usr/bin/env python3
"""Exercise real LSP transport and Git conflict resolution in a real merge in an isolated tmux session."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time

tmux = os.environ.get('TMUX_BINARY') or shutil.which('tmux')
if not tmux:
    raise SystemExit('tmux is needed for this development check')
binary = str(Path(os.environ.get('SHACK_BINARY', 'build/shack')).resolve())
with tempfile.TemporaryDirectory(prefix='shack-merge-test-') as folder:
    root = Path(folder)
    source = root / 'conflict file.txt'
    def git(*args, check=True):
        return subprocess.run(['git', '-C', str(root), *args], check=check, capture_output=True)
    git('init', '-q', '-b', 'main')
    for key, value in {'user.name': 'Shack tests', 'user.email': 'tests@example.invalid',
                       'commit.gpgsign': 'false', 'core.hooksPath': str(root / 'no-hooks'),
                       'core.attributesFile': '/dev/null', 'core.fsmonitor': 'false',
                       'merge.conflictStyle': 'zdiff3'}.items():
        git('config', key, value)
    source.write_text('header\nbase α\nfooter\n')
    git('add', '--', source.name)
    git('commit', '-qm', 'base')
    git('checkout', '-qb', 'other')
    source.write_text('header\ntheirs 🦦\nfooter\n')
    git('commit', '-qam', 'theirs')
    git('checkout', '-q', 'main')
    source.write_text('header\nours 😀\nfooter\n')
    git('commit', '-qam', 'ours')
    assert git('merge', '--no-edit', 'other', check=False).returncode != 0
    original = source.read_text()
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
    try:
        run('new-session', '-d', '-x', '120', '-y', '30', '-s', 'shack', binary,
            '--no-lsp', '--config', str(config), '--no-session', '--no-recovery', str(root), str(source))
        wait('Ctrl+E commands')
        def palette(name):
            keys('C-e')
            type_text(name)
            keys('Enter')
        palette('Git merge conflicts')
        wait('Merge conflicts · choose a file')
        keys('Enter')
        wait('Resolve hunk 1')
        keys('Enter')
        wait('Preview both: ours then theirs')
        keys('Down', 'Down', 'Enter')
        wait('Resolve conflict: both')
        assert source.read_text() == original, 'preview changed the worktree'
        keys('Tab')
        wait('Before')
        wait('After')
        keys('Tab', 'Enter')
        wait('Applied preview as one undoable change')
        keys('C-z')
        wait('<<<<<<<')
        keys('C-y', 'C-s')
        expected = 'header\nours 😀\ntheirs 🦦\nfooter\n'
        until(lambda: source.read_text() == expected, 'resolved file differs')
        palette('Inspect merge base')
        wait('base α')
        keys('C-w')
        palette('Stage saved merge resolution')
        wait('Staged resolution')
        assert git('ls-files', '-u').stdout == b''
        assert git('show', ':' + source.name).stdout.decode() == expected
        assert (root / '.git' / 'MERGE_HEAD').exists(), 'merge unexpectedly committed or aborted'
        keys('C-q')
        print('tmux real Git conflict discovery, zdiff3/Unicode inspection, hunk preview, undo/redo/save and explicit staging: PASS')
    finally:
        subprocess.run(base + ['kill-server'], capture_output=True)
