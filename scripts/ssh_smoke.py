#!/usr/bin/env python3
"""Exercise Shack over isolated loopback SSH, terminal profiles and remote tmux.

Uses temporary host/user keys and a loopback-only unprivileged sshd. No user SSH
configuration, authorized_keys, known_hosts or system clipboard is modified.
"""
import base64
import errno
import fcntl
import json
import os
from pathlib import Path
import pty
import pwd
import select
import shlex
import shutil
import signal
import socket
import statistics
import struct
import subprocess
import tempfile
import termios
import time

binary = str(Path(os.environ.get('SHACK_BINARY', 'build/shack')).resolve())
sshd = os.environ.get('SSHD_BINARY') or shutil.which('sshd') or '/usr/sbin/sshd'
tmux = os.environ.get('TMUX_BINARY') or shutil.which('tmux')
if not Path(sshd).is_file() or not tmux:
    raise SystemExit('OpenSSH sshd/client and tmux are needed for this development check')

class Session:
    def __init__(self, argv, profile):
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.execvpe(argv[0], argv, dict(os.environ, TERM=profile, LANG='en_US.UTF-8'))
        self.output = bytearray()
        self.resize(100, 30)
    def resize(self, cols, rows):
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack('HHHH', rows, cols, 0, 0))
    def read(self, timeout=.05):
        if select.select([self.fd], [], [], timeout)[0]:
            try:
                data = os.read(self.fd, 65536)
            except OSError as exc:
                if exc.errno == errno.EIO:
                    return False
                raise
            self.output.extend(data)
            if len(self.output) > 2 << 20:
                del self.output[:len(self.output) - (2 << 20)]
            return bool(data)
        return True
    def wait(self, needle, timeout=12):
        if isinstance(needle, str):
            needle = needle.encode()
        end = time.monotonic() + timeout
        while needle not in self.output and time.monotonic() < end:
            if not self.read():
                break
        assert needle in self.output, f'missing {needle!r}: {bytes(self.output[-3000:])!r}'
    def until(self, predicate, message, timeout=12):
        end = time.monotonic() + timeout
        while not predicate() and time.monotonic() < end:
            if not self.read():
                break
        assert predicate(), message + ': ' + repr(bytes(self.output[-2000:]))
    def send(self, data, clear=True):
        if clear:
            self.output.clear()
        os.write(self.fd, data.encode() if isinstance(data, str) else data)
    def command(self, name):
        self.send('\x1bOQ' + name + '\r')
    def save(self, path, expected):
        self.send('\x13')
        self.until(lambda: path.read_text() == expected, 'saved text differs')
    def finish(self):
        self.send('\x11')
        end = time.monotonic() + 8
        while time.monotonic() < end:
            done, status = os.waitpid(self.pid, os.WNOHANG)
            if done:
                self.pid = 0
                assert os.waitstatus_to_exitcode(status) == 0, status
                return
            self.read()
        raise AssertionError('SSH/editor did not exit')
    def benchmark(self):
        # Drain startup work before measuring real encrypted SSH round trips.
        while select.select([self.fd], [], [], .25)[0]:
            self.read(0)
        delays, sizes = [], []
        for _ in range(20):
            started = time.perf_counter_ns()
            self.send('x')
            assert select.select([self.fd], [], [], 3)[0], 'SSH edit produced no frame'
            delays.append((time.perf_counter_ns() - started) / 1e6)
            self.read(0)
            while select.select([self.fd], [], [], .03)[0]:
                self.read(0)
            assert b'\x1b[2J' not in self.output, 'ordinary SSH edit cleared the screen'
            sizes.append(len(self.output))
        print(f'Loopback SSH, 20 edits: first-output median {statistics.median(delays):.3f} ms, '
              f'max {max(delays):.3f} ms; terminal bytes/edit median {statistics.median(sizes):.0f}, '
              f'max {max(sizes)}; no full-screen clears', flush=True)
    def close(self):
        if self.pid:
            os.kill(self.pid, signal.SIGKILL)
            os.waitpid(self.pid, 0)
            self.pid = 0
        os.close(self.fd)

def contents(path):
    try:
        return path.read_text()
    except (FileNotFoundError, UnicodeDecodeError):
        return None

def gone(pid):
    try:
        os.kill(pid, 0)
        return False
    except ProcessLookupError:
        return True

with tempfile.TemporaryDirectory(prefix='shack-ssh-') as folder:
    root = Path(folder)
    for name in ('host', 'client'):
        subprocess.run(['ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-f', str(root / name)], check=True)
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        port = sock.getsockname()[1]
    user = pwd.getpwuid(os.getuid()).pw_name
    conf = root / 'sshd_config'
    conf.write_text(f'Port {port}\nListenAddress 127.0.0.1\nHostKey {root}/host\nPidFile {root}/pid\n'
                    f'AuthorizedKeysFile {root}/client.pub\nPasswordAuthentication no\nKbdInteractiveAuthentication no\n'
                    f'UsePAM no\nStrictModes no\nAllowUsers {user}\nAllowTcpForwarding no\nX11Forwarding no\n')
    known = root / 'known_hosts'
    known.write_text('shack-test ' + (root / 'host.pub').read_text())
    log = open(root / 'sshd.log', 'wb')
    daemon = subprocess.Popen([sshd, '-D', '-e', '-f', str(conf)], stdout=log, stderr=log)
    ssh = ['ssh', '-F', '/dev/null', '-tt', '-p', str(port), '-i', str(root / 'client'),
           '-o', 'IdentitiesOnly=yes', '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=yes',
           '-o', 'HostKeyAlias=shack-test', '-o', f'UserKnownHostsFile={known}',
           '-o', 'ConnectTimeout=5', f'{user}@127.0.0.1']
    config = root / 'config.json'
    config.write_text(json.dumps({'file_tree': 'off', 'terminal_shell': ['/bin/sh', '-i']}))
    tmux_config = root / 'tmux.conf'
    tmux_config.write_text('set -g default-terminal "tmux-256color"\nset -g mouse on\nset -s set-clipboard on\n')
    sockets = []
    try:
        for _ in range(60):
            if daemon.poll() is not None:
                raise AssertionError((root / 'sshd.log').read_text())
            try:
                with socket.create_connection(('127.0.0.1', port), timeout=.1):
                    break
            except OSError:
                time.sleep(.05)
        profiles = ['xterm-256color', 'xterm-kitty', 'xterm-ghostty', 'alacritty',
                    'screen-256color', 'tmux-256color', 'linux', 'vt100']
        selected = os.environ.get('SHACK_TERMINAL_PROFILES')
        if selected:
            profiles = selected.split(',')
        for index, profile in enumerate(profiles + ['remote-tmux']):
            nested = profile == 'remote-tmux'
            target = root / f'sample-{index}.txt'
            target.write_text('original\n')
            argv = [binary, '--config', str(config), '--plain', '--no-session', '--no-recovery', str(root), str(target)]
            remote = shlex.join(argv)
            if nested:
                sock = str(root / f'tmux-{index}.socket')
                sockets.append(sock)
                remote = shlex.join([tmux, '-S', sock, '-f', str(tmux_config), 'new-session', '-s', 'shack', remote])
            session = Session(ssh + [remote], 'xterm-256color' if nested else profile)
            try:
                session.wait('Ctrl+E commands')
                if index == 0:
                    session.benchmark()
                # All profiles get Unicode insertion, whole-grapheme deletion and undo.
                text = 'first 😀é👩‍💻'
                session.send('\x01' + text)
                session.send('\x7f', clear=False)
                session.save(target, 'first 😀é')
                session.send('\x1a')
                session.save(target, text)
                # Modern profiles additionally negotiate bracketed paste, OSC 52 and SGR mouse.
                modern = profile not in ('vt100', 'linux')
                if modern:
                    session.send('\x01\x1b[200~first line\nsecond line\x1b[201~')
                else:
                    session.send('\x01first line\rsecond line')
                session.save(target, 'first line\nsecond line')
                session.send('\x12line\rrow\r')
                session.wait('Space toggle')
                session.send(' \r')
                session.save(target, 'first line\nsecond row')
                if modern:
                    # Click column 3 of line 1. A four-cell gutter precedes text.
                    session.send('\x1b[<0;7;2M\x1b[<0;7;2mX')
                    session.save(target, 'fiXrst line\nsecond row')
                    session.send('\x1b[<2;7;2M\x1b[<2;7;2m')
                    session.wait('Actions')
                    session.send('\r')
                    session.wait('Copied')
                    session.send('\x01\x03')
                    session.wait('Copied')
                    # The test PTY captures clipboard writes; no OS clipboard is changed.
                    if profile != 'screen-256color':
                        session.wait(b'\x1b]52;')
                        session.wait(base64.b64encode(b'fiXrst line\nsecond row'))
                        if not nested:
                            session.send('\x16')
                            session.wait(b';?')
                            reply = base64.b64encode('remote clipboard 界'.encode())
                            session.send(b'\x1b]52;c;' + reply + b'\x07')
                            session.save(target, 'remote clipboard 界')
                    else:
                        # The screen terminfo profile has no OSC 52 capability.
                        session.send('temporary')
                        session.send('\x01')
                        session.command('Paste Shack internal clipboard')
                        session.save(target, 'fiXrst line\nsecond row')
                session.output.clear()
                session.resize(12, 4)
                session.wait('enla')
                session.output.clear()
                session.resize(100, 30)
                session.command('Help / keyboard shortcuts')
                session.wait('arrows')
                session.send('\r')  # The first help action is Save.
                session.wait('Saved')
                session.finish()
                print(f'SSH {profile}: Unicode/grapheme undo, replacement preview, save, resize, quit'
                      + ('; bracketed paste, left/right SGR mouse, clipboard ' + ('fallback' if profile == 'screen-256color' else 'wire protocol') if modern else '') + ': PASS', flush=True)
            finally:
                session.close()
        # Embedded shell + Vim inside Shack inside tmux over SSH.
        fixture = root / 'vim.txt'
        fixture.write_text('VIM REMOTE READY\n')
        editor_source = root / 'editor.txt'
        editor_source.write_text('SSH terminal test\n')
        argv = [binary, '--config', str(config), '--plain', '--no-session', '--no-recovery', str(root), str(editor_source)]
        sock = str(root / 'embedded.socket')
        sockets.append(sock)
        remote = shlex.join([tmux, '-S', sock, '-f', str(tmux_config), 'new-session', '-s', 'shack', shlex.join(argv)])
        session = Session(ssh + [remote], 'xterm-256color')
        children = []
        try:
            session.wait('Ctrl+E commands')
            session.command('New terminal')
            session.wait('Terminal input')
            session.send('echo $$ > shell.pid; stty size > before.size; printf "REMOTE_PTY_READY\\n"\r')
            session.until(lambda: (root / 'shell.pid').exists() and (root / 'before.size').exists(), 'remote shell not ready')
            children.append(int((root / 'shell.pid').read_text()))
            vim = shutil.which('vim')
            assert vim, 'vim is needed for the nested interactive-program check'
            session.send(shlex.quote(vim) + ' -Nu NONE -n vim.txt; printf ready > vim.done\r')
            session.wait('VIM REMOTE READY')
            session.send('i你好 remote: \x1b:wq\r')
            session.until(lambda: contents(fixture) == '你好 remote: VIM REMOTE READY\n', 'embedded Vim save differs')
            session.until(lambda: (root / 'vim.done').exists(), 'Vim did not return to the shell')
            session.resize(110, 36)
            session.send('i=0; while [ "$i" -lt 40 ]; do stty size > after.size; [ "$(cat after.size)" != "$(cat before.size)" ] && break; i=$((i+1)); sleep .05; done\r')
            session.until(lambda: (root / 'after.size').exists() and (root / 'after.size').read_text() != (root / 'before.size').read_text(), 'nested PTY resize not propagated')
            session.send("/bin/sh -c 'echo $$ > child.pid; exec sleep 60'\r")
            session.until(lambda: (root / 'child.pid').exists(), 'foreground child missing')
            children.append(int((root / 'child.pid').read_text()))
            session.send('\x1d')
            session.wait('Editor controls')
            session.finish()
            end = time.monotonic() + 5
            while any(not gone(pid) for pid in children) and time.monotonic() < end:
                time.sleep(.05)
            assert all(gone(pid) for pid in children), 'remote terminal child survived shutdown'
            print('SSH → tmux → Shack → shell/Vim: Unicode save, resize, escape and child cleanup: PASS', flush=True)
        finally:
            session.close()
            for pid in children:
                if not gone(pid):
                    os.kill(pid, signal.SIGKILL)
    finally:
        for sock in sockets:
            subprocess.run([tmux, '-S', sock, 'kill-server'], capture_output=True)
        daemon.terminate()
        try:
            daemon.wait(timeout=3)
        except subprocess.TimeoutExpired:
            daemon.kill()
            daemon.wait()
        log.close()
