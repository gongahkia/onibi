#!/usr/bin/env python3
"""Exercise a real embedded PTY, Vim, resize, interrupts and child cleanup in tmux."""
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import tempfile
import time

tmux = os.environ.get('TMUX_BINARY') or shutil.which('tmux')
vim = shutil.which('vim')
if not tmux or not vim:
    raise SystemExit('tmux and vim are needed for this development check')
binary = str(Path(os.environ.get('SHACK_BINARY', 'build/shack')).resolve())
with tempfile.TemporaryDirectory(prefix='shack-terminal-test-') as folder:
    root = Path(folder)
    config = root / 'config.json'
    config.write_text(json.dumps({'terminal_shell': ['/bin/sh', '-i'], 'file_tree': 'off'}))
    fixture = root / 'fixture.txt'
    fixture.write_text('VIM SCREEN READY\n')
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
        for _ in range(160):
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
    def shell(value):
        type_text(value)
        keys('Enter')
    def gone(pid):
        try:
            os.kill(pid, 0)
            return False
        except ProcessLookupError:
            return True
    pids = []
    try:
        run('new-session', '-d', '-x', '110', '-y', '30', '-s', 'shack', binary,
            '--config', str(config), '--no-lsp', '--no-session', '--no-recovery', str(root))
        wait('Ctrl+E commands')
        editor_pid = run('display-message', '-p', '-t', 'shack', '#{pane_pid}').stdout.decode().strip()
        def rss():
            return int(subprocess.check_output(['ps', '-o', 'rss=', '-p', editor_pid]).strip()) / 1024
        idle_rss = rss()
        command('New terminal')
        wait('Terminal input')
        # Give shell startup its own output marker; typed input alone cannot satisfy the file check.
        shell("printf '%s' \"$$\" > shell.pid; printf '\033[31mPTY READY\033[0m\\n'; stty size > before.size")
        until(lambda: (root / 'shell.pid').exists() and (root / 'before.size').exists(), 'shell did not execute')
        pids.append(int((root / 'shell.pid').read_text()))
        wait('PTY READY')
        terminal_rss = rss()
        shell(shlex.quote(vim) + ' -Nu NONE -n fixture.txt')
        wait('VIM SCREEN READY')
        keys('Home', 'i')
        type_text('你好 edited inside Shack: ')
        keys('Escape')
        type_text(':wq')
        keys('Enter')
        until(lambda: fixture.exists() and fixture.read_text() == '你好 edited inside Shack: VIM SCREEN READY\n', 'embedded Vim could not edit/save')
        run('resize-window', '-t', 'shack', '-x', '100', '-y', '35')
        time.sleep(.2)
        shell('stty size > after.size')
        until(lambda: (root / 'after.size').exists(), 'shell resize check did not run')
        assert (root / 'before.size').read_text() != (root / 'after.size').read_text()
        shell('sleep 60')
        time.sleep(.2)
        keys('C-c')
        shell('printf interrupted > interrupt.txt')
        until(lambda: (root / 'interrupt.txt').exists(), 'Ctrl+C did not reach foreground program')
        # This child ignores hangup, so cleanup must terminate its foreground group.
        shell("/bin/sh -c 'trap \"\" HUP; echo $$ > child.pid; while :; do sleep 1; done'")
        until(lambda: (root / 'child.pid').exists(), 'foreground child did not start')
        pids.append(int((root / 'child.pid').read_text()))
        keys('C-]')
        wait('Editor controls')
        command('Split horizontally')
        wait('2 · Terminal')
        keys('C-q')
        for pid in pids:
            until(lambda: gone(pid), f'terminal child {pid} survived editor shutdown')
        print(f'embedded PTY shell, ANSI, Vim Unicode edit/save, resize, interrupt, escape, split and process cleanup: PASS; editor RSS before terminal {idle_rss:.1f} MiB, with idle terminal {terminal_rss:.1f} MiB (child excluded)')
    finally:
        subprocess.run(base + ['kill-server'], capture_output=True)
        for pid in pids:
            if not gone(pid):
                try:
                    os.killpg(pid, 9)
                except ProcessLookupError:
                    pass
