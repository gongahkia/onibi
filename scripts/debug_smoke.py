#!/usr/bin/env python3
"""Run Shack's debugger controls through tmux and a real optional debugpy adapter."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time

tmux = os.environ.get('TMUX_BINARY') or shutil.which('tmux')
python = os.environ.get('SHACK_DEBUGPY_PYTHON')
if not tmux or not python:
    raise SystemExit('requires tmux and SHACK_DEBUGPY_PYTHON pointing to a Python with debugpy')
# Preserve virtual-environment symlinks: resolving bin/python selects the base
# interpreter and loses the environment's installed debugpy package.
python = os.path.abspath(python)
subprocess.run([python, '-c', 'import debugpy'], check=True, timeout=15)
binary = str(Path(os.environ.get('SHACK_BINARY', 'build/shack')).resolve())
with tempfile.TemporaryDirectory(prefix='shack-debug-test-') as folder:
    root = Path(folder)
    source = root / 'sample.py'
    source.write_text("import os\nfrom pathlib import Path\nPath('debug.pid').write_text(str(os.getpid()))\ndef add(a, b):\n    value = a + b\n    return value\nanswer = add(2, 3)\nwhile True:\n    pass\n")
    config = root / 'config.json'
    config.write_text(json.dumps({'file_tree': 'off', 'debuggers': {'Smoke Python': {
        'command': [python, '-m', 'debugpy.adapter'], 'request': 'launch', 'arguments': {
            'program': '${file}', 'cwd': '${workspaceFolder}', 'python': [python],
            'console': 'internalConsole', 'redirectOutput': True, 'subProcess': False}}}}))
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
        for _ in range(250):
            if predicate():
                return
            time.sleep(.04)
        raise AssertionError(message + '\n' + pane())
    def wait(text):
        until(lambda: text in pane(), 'missing ' + repr(text))
    def command(value):
        keys('F2')
        type_text(value)
        keys('Enter')
    def gone(pid):
        try:
            os.kill(pid, 0)
            return False
        except ProcessLookupError:
            return True
    target = None
    try:
        run('new-session', '-d', '-x', '120', '-y', '32', '-s', 'shack', binary,
            '--config', str(config), '--no-lsp', '--no-session', '--no-recovery', str(root), str(source))
        wait('Ctrl+E commands')
        keys('C-g')
        type_text('5')
        keys('Enter', 'F9')
        wait('Breakpoint at sample.py:5')
        keys('F5')
        wait('Debug profile')
        type_text('Smoke Python')
        keys('Enter')
        wait('Debugger stopped: breakpoint')
        until(lambda: (root / 'debug.pid').exists(), 'debug target did not start')
        target = int((root / 'debug.pid').read_text())
        command('Debug scopes and variables')
        wait('Debug scopes')
        type_text('Locals')
        keys('Enter')
        wait('a = 2')
        keys('Escape')
        until(lambda: 'Debug variables' not in pane(), 'variables picker did not close')
        keys('F10')
        wait('add:6')
        command('Debug evaluate expression')
        wait('Evaluate expression')
        type_text('value')
        keys('Enter')
        wait('Debug evaluation')
        assert any(line.strip().endswith('5') for line in pane().splitlines())
        keys('C-w', 'S-F11')
        wait('<module>')
        keys('F5')
        wait('Debug running')
        command('Debug pause')
        wait('stopped: pause')
        keys('S-F5')
        wait('Debugger stopped')
        until(lambda: gone(target), 'launched target survived debugger stop')
        keys('C-q')
        print('tmux DAP launch, gutter breakpoint, locals, evaluation, step, pause and target cleanup: PASS')
    except Exception:
        # Preserve adapter stderr in failures, not just the editor's EOF status.
        command('Debug output')
        for _ in range(30):
            if 'Debug output' in pane():
                break
            time.sleep(.04)
        print('Adapter output on failure:\n' + pane(), flush=True)
        if target is None and (root / 'debug.pid').exists():
            target = int((root / 'debug.pid').read_text())
        raise
    finally:
        subprocess.run(base + ['kill-server'], capture_output=True)
        if target and not gone(target):
            try:
                os.kill(target, 9)
            except ProcessLookupError:
                pass
