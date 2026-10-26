#!/usr/bin/env python3
"""Kill a real terminal session, recover its unsaved text, then save it."""
import errno
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import signal
import struct
import tempfile
import termios
import time

binary = str(Path(os.environ.get('SHACK_BINARY', 'build/shack')).resolve())
with tempfile.TemporaryDirectory(prefix='shack-recovery-test-') as folder:
    root = Path(folder) / 'project'
    state = Path(folder) / 'state'
    root.mkdir()
    target = root / 'sample.txt'
    target.write_text('original')
    pid = 0
    master = None
    output = bytearray()

    def start(*paths):
        global pid, master, output
        output = bytearray()
        pid, master = pty.fork()
        if pid == 0:
            env = dict(os.environ, TERM='xterm-256color', XDG_STATE_HOME=str(state))
            os.execve(binary, [binary, '--no-config', '--no-lsp', '--no-session', *map(str, paths)], env)
        fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack('HHHH', 24, 90, 0, 0))

    def wait(predicate, timeout=8):
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            if predicate():
                return
            if select.select([master], [], [], .05)[0]:
                try:
                    output.extend(os.read(master, 65536))
                except OSError as exc:
                    if exc.errno != errno.EIO:
                        raise
        raise AssertionError(bytes(output[-1200:]))

    def stop(sig):
        global pid, master
        if pid:
            os.kill(pid, sig)
            os.waitpid(pid, 0)
            pid = 0
        if master is not None:
            os.close(master)
            master = None

    try:
        start(root, target)
        wait(lambda: b'Ctrl+Shift+P commands' in output)
        os.write(master, b'\x01\x1b[200~recovered after SIGKILL\x1b[201~')
        def persisted():
            for path in (state / 'shack' / 'recovery').glob('*.json'):
                if any(b['Contents'] == 'recovered after SIGKILL' for b in json.loads(path.read_text())['Buffers']):
                    assert path.stat().st_mode & 0o777 == 0o600
                    return True
            return False
        wait(persisted)
        assert target.read_text() == 'original'
        stop(signal.SIGKILL)
        start(root)
        wait(lambda: b'Recovery snapshots' in output)
        os.write(master, b'\r')
        wait(lambda: b'Restore buffers' in output)
        os.write(master, b'\r')
        wait(lambda: b'Recovered unsaved text' in output)
        assert target.read_text() == 'original'
        os.write(master, b'\x13')
        wait(lambda: target.read_text() == 'recovered after SIGKILL')
        os.write(master, b'\x11')
        def exited():
            global pid
            done, status = os.waitpid(pid, os.WNOHANG)
            if done:
                assert os.waitstatus_to_exitcode(status) == 0
                pid = 0
                return True
            return False
        wait(exited)
        assert not list((state / 'shack' / 'recovery').glob('*.json'))
        print('PTY SIGKILL recovery, private snapshot, explicit save and cleanup: PASS')
    finally:
        stop(signal.SIGKILL)
