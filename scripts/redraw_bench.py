#!/usr/bin/env python3
"""Measure local key-to-first-output delay and emitted bytes; no SSH claims."""
import errno
import fcntl
import os
from pathlib import Path
import pty
import select
import signal
import statistics
import struct
import tempfile
import termios
import time

binary = str(Path(os.environ.get('SHACK_BINARY', 'build/shack')).resolve())
with tempfile.TemporaryDirectory(prefix='shack-redraw-') as directory:
    target = Path(directory) / 'file.txt'
    target.write_text('')
    pid, master = pty.fork()
    if pid == 0:
        os.execve(binary, [binary, '--no-config', '--plain', '--no-session', '--no-recovery', directory, str(target)], dict(os.environ, TERM='xterm-256color'))
    fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack('HHHH', 24, 80, 0, 0))
    def drain(quiet=.03):
        out = bytearray()
        while select.select([master], [], [], quiet)[0]:
            try:
                data = os.read(master, 65536)
            except OSError as exc:
                if exc.errno == errno.EIO:
                    break
                raise
            if not data:
                break
            out.extend(data)
        return bytes(out)
    try:
        captured = bytearray()
        until = time.monotonic() + 10
        while b'Ctrl+E commands' not in captured and time.monotonic() < until:
            captured.extend(drain(.1))
        assert b'Ctrl+E commands' in captured
        drain(.2)
        delays, sizes = [], []
        for _ in range(30):
            started = time.perf_counter_ns()
            os.write(master, b'x')
            assert select.select([master], [], [], 2)[0]
            delays.append((time.perf_counter_ns() - started) / 1e6)
            out = os.read(master, 65536) + drain()
            assert b'\x1b[2J' not in out, 'ordinary edit cleared the terminal'
            sizes.append(len(out))
        os.write(master, b'\x13')
        drain(.2)
        assert target.read_text() == 'x' * 30
        os.write(master, b'\x11')
        drain(.1)
        done, status = os.waitpid(pid, 0)
        pid = 0
        assert os.waitstatus_to_exitcode(status) == 0
        print(f'30 local PTY edits: first-output median {statistics.median(delays):.3f} ms, max {max(delays):.3f} ms; bytes/edit median {statistics.median(sizes):.0f}, max {max(sizes)}; no full-screen clears')
    finally:
        if pid:
            os.kill(pid, signal.SIGKILL)
            os.waitpid(pid, 0)
        os.close(master)
