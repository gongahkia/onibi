#!/usr/bin/env python3
"""Exercise real LSP transport and reviewed server-command actions in an isolated tmux session."""
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
with tempfile.TemporaryDirectory(prefix='shack-resources-test-') as folder:
    root = Path(folder)
    source = root / 'sample.txt'
    source.write_text('one')
    renamed = root / 'renamed.txt'
    created = root / 'generated' / 'note.txt'
    removed = root / 'remove.txt'
    removed.write_text('remove me')
    server = root / 'server.py'
    server.write_text(r'''
import json, sys
from pathlib import Path
from urllib.parse import urlparse, unquote
pending = None
def send(value):
    data = json.dumps(dict(jsonrpc='2.0', **value)).encode()
    sys.stdout.buffer.write(f'Content-Length: {len(data)}\r\n\r\n'.encode() + data)
    sys.stdout.buffer.flush()
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
        uri = request['params']['textDocument']['uri']
        Path(sys.argv[1]).write_text('ready')
    if 'id' not in request:
        continue
    if method == 'initialize':
        send(dict(id=request['id'], result={'capabilities': {'textDocumentSync': 1, 'codeActionProvider': {'resolveProvider': True}}}))
    elif method == 'textDocument/codeAction':
        send(dict(id=request['id'], result=[{'title': 'Reviewed action', 'kind': 'quickfix', 'data': {'uri': uri}}]))
    elif method == 'codeAction/resolve':
        action = request['params']
        action['command'] = {'command': 'test.apply', 'arguments': [uri]}
        send(dict(id=request['id'], result=action))
    elif method == 'workspace/executeCommand':
        pending = request['id']
        root = Path(unquote(urlparse(uri).path)).parent
        renamed = (root / 'renamed.txt').as_uri()
        created = (root / 'generated' / 'note.txt').as_uri()
        edit = {'documentChanges': [
            {'kind': 'rename', 'oldUri': uri, 'newUri': renamed},
            {'textDocument': {'uri': renamed, 'version': None}, 'edits': [{'range': {'start': {'line': 0, 'character': 0}, 'end': {'line': 0, 'character': 3}}, 'newText': 'two'}]},
            {'kind': 'create', 'uri': created},
            {'textDocument': {'uri': created, 'version': None}, 'edits': [{'range': {'start': {'line': 0, 'character': 0}, 'end': {'line': 0, 'character': 0}}, 'newText': 'created 🦦\n'}]},
            {'kind': 'delete', 'uri': (root / 'remove.txt').as_uri()}
        ]}
        send(dict(id='review', method='workspace/applyEdit', params={'label': 'Reviewed server edit', 'edit': edit}))
    elif request.get('id') == 'review':
        Path(sys.argv[2]).write_text(json.dumps(request['result']))
        send(dict(id=pending, result=None))
    else:
        send(dict(id=request['id'], result=None))
''')
    config = root / 'config.json'
    config.write_text(json.dumps({'file_tree': 'off', 'auto_complete': False, 'servers': {
        '.txt': {'command': [sys.executable, str(server), str(root / 'ready'), str(root / 'result.json')], 'language': 'text'}}}))
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
        def palette(name):
            keys('F2')
            type_text(name)
            keys('Enter')
        palette('Code actions')
        wait('Reviewed action')
        keys('Enter')
        wait('Reviewed server edit')
        assert not (root / 'result.json').exists(), 'server replied before review'
        assert source.read_text() == 'one'
        keys('Escape')
        until(lambda: (root / 'result.json').exists(), 'no rejection reply')
        assert json.loads((root / 'result.json').read_text())['applied'] is False
        wait('Completed Reviewed action')
        (root / 'result.json').unlink()
        palette('Code actions')
        wait('Reviewed action')
        keys('Enter')
        wait('Reviewed server edit')
        keys('Enter')
        until(lambda: (root / 'result.json').exists(), 'no acceptance reply')
        assert json.loads((root / 'result.json').read_text())['applied'] is True
        assert not source.exists() and renamed.read_text() == 'one'
        assert created.read_text() == '' and not removed.exists()
        palette('Save all')
        until(lambda: renamed.read_text() == 'two' and created.read_text() == 'created 🦦\n', 'save differs')
        palette('Undo last workspace edit')
        wait('Workspace undo restored file operations and buffers')
        palette('Save all')
        until(lambda: source.exists() and source.read_text() == 'one' and removed.exists() and not renamed.exists() and not created.exists(), 'undo differs')
        palette('Redo last workspace edit')
        until(lambda: renamed.exists() and created.exists(), 'redo missing')
        wait('Workspace redo restored file operations and buffers')
        palette('Save all')
        until(lambda: renamed.read_text() == 'two' and created.read_text() == 'created 🦦\n', 'redo save differs')
        keys('C-q')
        print('tmux reviewed LSP rename/create/delete, Unicode text edits, cancellation, Save All, filesystem/buffer undo and redo: PASS')
    finally:
        subprocess.run(base + ['kill-server'], capture_output=True)
