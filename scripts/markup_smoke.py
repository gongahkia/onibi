#!/usr/bin/env python3
"""Exercise bundled Markdown/Org views and live source updates through tmux."""
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
with tempfile.TemporaryDirectory(prefix='shack-markup-test-') as folder:
    root = Path(folder)
    source = root / 'note.md'
    original = '# Hello **世界**\n\n- [x] First task\n\n[website](https://example.com)\n'
    source.write_text(original)
    org = root / 'note.org'
    org.write_text('#+TITLE: Org notebook\n* TODO Heading\nSome *bold* and /italic/ text.\n\n#+BEGIN_SRC python\nprint("inert code")\n#+END_SRC\n')
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
        keys('C-e')
        type_text(name)
        keys('Enter')
    try:
        run('new-session', '-d', '-x', '120', '-y', '30', '-s', 'shack', binary,
            '--no-lsp', '--config', str(config), '--no-session', '--no-recovery', str(source))
        wait('Ctrl+E commands')
        keys('F4')
        wait('Hello 世界')
        wait('☑ First task')
        assert '**世界**' not in pane()
        type_text('SHOULD NOT INSERT')
        wait('Read-only mode')
        assert source.read_text() == original
        keys('F4')
        wait('# Hello **世界**')
        palette('Document preview beside source')
        wait('2 · Preview')
        wait('Hello 世界')
        keys('F4', 'C-End', 'Enter')
        type_text('## Live update')
        # The heading must appear as rendered text in the right pane too.
        until(lambda: any('Live update' in row and '##' not in row for row in pane().splitlines()), 'preview did not update')
        keys('C-s')
        until(lambda: '## Live update' in source.read_text(), 'source save differs')
        keys('F4')
        palette('Document preview headings')
        wait('Document headings')
        wait('Live update')
        keys('Escape')
        until(lambda: 'Document headings' not in pane(), 'outline did not close')
        palette('Document preview links')
        wait('Document links · Enter copies URL')
        wait('https://example.com')
        keys('Enter')
        wait('Link copied internally')
        keys('C-o')
        type_text(str(org))
        keys('Enter', 'F4')
        wait('Org notebook')
        wait('Some bold and italic text.')
        wait('print("inert code")')
        keys('C-q')
        print('tmux Markdown/Org rendering, source toggle, split/live refresh, read-only controls, headings, links and save: PASS')
    finally:
        subprocess.run(base + ['kill-server'], capture_output=True)
