#!/usr/bin/env python3
"""Real PTY long-line editing/resize test and first-output measurements."""
import errno
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import signal
import statistics
import struct
import subprocess
import tempfile
import termios
import time

binary = str(Path(os.environ.get('SHACK_BINARY', 'build/shack')).resolve())
with tempfile.TemporaryDirectory(prefix='shack-unicode-') as directory:
    target = Path(directory) / 'sample.txt'
    unit = '界 é 👩‍💻 🇸🇬\t'
    tail = 'é👩‍💻🇸🇬'
    original = unit * ((1 << 20) // len(unit.encode())) + tail
    target.write_text(original)
    pid, master = pty.fork()
    if pid == 0:
        command = json.loads(os.environ['SHACK_COMMAND']) if 'SHACK_COMMAND' in os.environ else [binary, '--no-config', '--plain', '--no-session', '--no-recovery', '{file}']
        command = [arg.replace('{file}', str(target)).replace('{directory}', directory) for arg in command]
        os.execvpe(command[0], command, dict(os.environ, TERM='xterm-256color', LANG='C.UTF-8'))

    def resize(cols, rows):
        fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack('HHHH', rows, cols, 0, 0))

    def drain(quiet=.03):
        output = bytearray()
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline and select.select([master], [], [], quiet)[0]:
            try:
                data = os.read(master, 65536)
            except OSError as exc:
                if exc.errno == errno.EIO:
                    break
                raise
            if not data:
                break
            output.extend(data)
        return bytes(output)

    def exchange(data):
        drain()
        start = time.perf_counter_ns()
        os.write(master, data)
        assert select.select([master], [], [], 10)[0], 'input produced no output'
        delay = (time.perf_counter_ns() - start) / 1e6
        return delay, os.read(master, 65536) + drain()

    def save(expected):
        exchange(b'\x13')
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            try:
                if target.read_text() == expected:
                    return
            except FileNotFoundError:
                pass
            drain(.05)
        raise AssertionError('saved Unicode text differs')

    try:
        resize(100, 30)
        output = bytearray()
        deadline = time.monotonic() + 10
        while b'Ctrl+E commands' not in output and time.monotonic() < deadline:
            output.extend(drain(.1))
        assert b'Ctrl+E commands' in output, 'startup did not complete'
        jump_ms, _ = exchange(b'\x1b[F')  # End: first distant Unicode index.
        exchange(b'\x05Toggle soft wrapping\r')
        exchange(b'\x7f\x7f\x7f')  # flag, ZWJ emoji, then combining-accent cluster
        save(original[:-len(tail)])
        exchange(b'\x1a\x1a\x1a')
        save(original)
        resize(70, 15)
        drain(.15)
        resize(120, 36)
        drain(.15)
        delays, sizes = [], []
        for _ in range(20):
            delay, output = exchange(b'x')
            assert b'\x1b[2J' not in output, 'ordinary edit cleared screen'
            delays.append(delay)
            sizes.append(len(output))
        save(original + 'x' * 20)
        rss = ''
        if 'SHACK_COMMAND' not in os.environ:
            rss_kib = int(subprocess.check_output(['ps', '-o', 'rss=', '-p', str(pid)]).strip())
            rss = f'; editor RSS {rss_kib / 1024:.1f} MiB'
        os.write(master, b'\x11')
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            done, status = os.waitpid(pid, os.WNOHANG)
            if done:
                pid = 0
                assert os.waitstatus_to_exitcode(status) == 0
                break
            drain(.05)
        assert pid == 0, 'editor failed to exit'
        print(f'1 MiB Unicode PTY: whole-grapheme delete/undo/save, wrap, resize and quit PASS; '
              f'cold End first output {jump_ms:.2f} ms; 20 wrapped edits first-output '
              f'median {statistics.median(delays):.3f} / max {max(delays):.3f} ms; '
              f'bytes median {statistics.median(sizes):.0f} / max {max(sizes)}{rss}')
    finally:
        if pid:
            os.kill(pid, signal.SIGKILL)
            os.waitpid(pid, 0)
        os.close(master)
