#!/usr/bin/env python3
"""Exercise real LSP transport and snippet controls in an isolated tmux session."""
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
with tempfile.TemporaryDirectory(prefix='shack-snippet-test-') as folder:
    root = Path(folder)
    source = root / 'sample.txt'
    source.write_text('sn')
    server = root / 'server.py'
    server.write_text(r'''
import json, sys
from pathlib import Path
while True:
    length = None
    while True:
        line = sys.stdin.buffer.readline()
        if not line:
            raise SystemExit(0)
        if line in (b'\r\n', b'\n'):
            break
        if line.lower().startswith(b'content-length:'):
            length = int(line.split(b':', 1)[1])
    request = json.loads(sys.stdin.buffer.read(length))
    method = request.get('method')
    if method == 'textDocument/didOpen':
        Path(sys.argv[1]).write_text('ready')
    if 'id' not in request:
        continue
    result = None
    if method == 'initialize':
        result = {'capabilities': {'textDocumentSync': 1, 'completionProvider': {}}}
    elif method == 'textDocument/completion':
        result = [{'label': 'Rich snippet', 'insertTextFormat': 2, 'insertText': '${1:hello ${2|world,friend|}} / $1 / ${2/(.*)/${1:/upcase}/} / $TM_FILENAME_BASE$0'}]
    data = json.dumps({'jsonrpc': '2.0', 'id': request['id'], 'result': result}).encode()
    sys.stdout.buffer.write(f'Content-Length: {len(data)}\r\n\r\n'.encode() + data)
    sys.stdout.buffer.flush()
''')
    config = root / 'config.json'
    config.write_text(json.dumps({'file_tree': 'off', 'auto_complete': False, 'servers': {
        '.txt': {'command': [sys.executable, str(server), str(root / 'ready')], 'language': 'text'}}}))
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
            '--config', str(config), '--no-session', '--no-recovery', str(root), str(source))
        wait('Ctrl+Shift+P commands')
        until(lambda: (root / 'ready').exists(), 'LSP document did not synchronize')
        keys('End', 'C-Space')
        wait('Rich snippet')
        keys('Enter')
        wait('hello world / hello world / WORLD / sample')
        keys('Tab')
        wait('world')
        # Choices render inline using the same documented keyboard controls.
        wait('friend')
        keys('Down', 'Enter')
        wait('hello friend / hello friend / WORLD / sample')
        keys('Tab')
        wait('hello friend / hello friend / FRIEND / sample')
        keys('C-s')
        until(lambda: source.read_text() == 'hello friend / hello friend / FRIEND / sample', 'snippet save differs')
        keys('C-z')
        wait('hello world / hello world / WORLD / sample')
        keys('C-y', 'C-s', 'C-q')
        print('tmux LSP completion, nested linked placeholders, choice picker, file variable, transform and grouped undo/redo: PASS')
    finally:
        subprocess.run(base + ['kill-server'], capture_output=True)
