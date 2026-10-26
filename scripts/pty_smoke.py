#!/usr/bin/env python3
"""Exercise the shipped executable through a real Unix pseudo-terminal."""
import errno
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import signal
import struct
import subprocess
import tempfile
import termios
import time

binary = str(Path(os.environ.get('SHACK_BINARY', 'build/shack')).resolve())
with tempfile.TemporaryDirectory(prefix='shack-pty-') as directory:
    target = Path(directory) / 'sample.txt'
    target.write_text('original\n')
    started = time.monotonic()
    pid, master = pty.fork()
    if pid == 0:
        env = dict(os.environ, TERM='xterm-256color')
        command = json.loads(os.environ['SHACK_COMMAND']) if 'SHACK_COMMAND' in os.environ else [binary, '--no-config', '--no-lsp', '{file}']
        command = [arg.replace('{file}', str(target)).replace('{directory}', directory) for arg in command]
        os.execvpe(command[0], command, env)
    fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack('HHHH', 24, 90, 0, 0))
    captured = bytearray()
    def receive_until(needle, timeout=10):
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            if needle in captured:
                return
            if select.select([master], [], [], 0.1)[0]:
                data = os.read(master, 65536)
                if not data:
                    break
                captured.extend(data)
        raise AssertionError(f'terminal never displayed {needle!r}: {bytes(captured[-1500:])!r}')
    try:
        receive_until(b'Ctrl+Shift+P commands')
        ready_ms = (time.monotonic() - started) * 1000
        rss_kib = int(subprocess.check_output(["ps", "-o", "rss=", "-p", str(pid)]).strip())
        def settle():
            deadline = time.monotonic() + 2
            while time.monotonic() < deadline and select.select([master], [], [], .08)[0]:
                os.read(master, 65536)
            captured.clear()
        # Actual terminal decoding: CSI-u and xterm modifyOtherKeys, including
        # uppercase encodings. Legacy control bytes retain their original meaning.
        for sequence, title in (
            (b'\x1b[112;6u', b'Commands'),
            (b'\x1b[80;6u', b'Commands'),
            (b'\x1b[27;6;80~', b'Commands'),
            (b'\x1b[104;5u', b'Help'),
            (b'\x1b[27;5;104~', b'Help'),
            (b'\x1bOQ', b'Commands'),
            (b'\x1bOP', b'Help'),
            (b'\x10', b'Project files'),
        ):
            settle()
            os.write(master, sequence)
            receive_until(title)
            os.write(master, b'\x1b')
            time.sleep(.1)
        settle()
        os.write(master, b'x\x7fy\x08\x13')
        receive_until(b'Saved sample.txt')
        assert target.read_text() == 'original\n', 'shortcut or Backspace changed source unexpectedly'
        settle()
        os.write(master, b'\x01\x1b[200~first line\nsecond line\x1b[201~')
        time.sleep(0.1)
        os.write(master, b'\x1a\x19')  # undo entire paste, then redo
        os.write(master, b'\x12line\rrow\r')
        receive_until(b'Space toggle')
        os.write(master, b' \r\x13')  # exclude first match, apply, save
        receive_until(b'Saved sample.txt')
        os.write(master, b'\x11')
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            done, status = os.waitpid(pid, os.WNOHANG)
            if done:
                assert os.waitstatus_to_exitcode(status) == 0
                pid = 0
                break
            if select.select([master], [], [], 0.05)[0]:
                try:
                    os.read(master, 65536)
                except OSError as exc:
                    if exc.errno != errno.EIO:
                        raise
        assert pid == 0, 'editor failed to exit'
        assert target.read_text() == 'first line\nsecond row', target.read_text()
        memory = '' if 'SHACK_COMMAND' in os.environ else f'; RSS {rss_kib / 1024:.1f} MiB'
        print(f'PTY palette/help protocols, Backspace, editing, paste undo/redo, selective preview, save and quit: PASS; first frame {ready_ms:.1f} ms{memory}')
    finally:
        if pid:
            os.kill(pid, signal.SIGKILL)
            os.waitpid(pid, 0)
        os.close(master)
