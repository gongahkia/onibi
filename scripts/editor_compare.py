#!/usr/bin/env python3
"""Local core-editor comparison, not an IDE/configuration/whole-frame benchmark.

Uses isolated config, 100x30 PTYs, syntax/services disabled, one discarded warmup
per editor/workload, then interleaved measured runs. Results go to stdout as JSON.
No user config, clipboard, installed files or system caches are modified.
"""
import errno
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import shutil
import signal
import statistics
import struct
import subprocess
import tempfile
import termios
import time

root = Path(__file__).resolve().parent.parent
editors = {'Shack': str(root / 'build/shack')}
for label, command in [('Micro', 'micro'), ('Vim', 'vim'), ('Neovim', 'nvim')]:
    if path := shutil.which(command):
        editors[label] = path
unit = '界 é 👩‍💻 🇸🇬\t'
fixtures = {
    'small': 'COMPARE_READY\n' + 'sample plain text\n' * 1000,
    'unicode_1m': 'COMPARE_READY ' + unit * ((1 << 20) // len(unit.encode())) + 'END',
}

def run(label, binary, kind, source):
    with tempfile.TemporaryDirectory(prefix='shack-compare-') as directory:
        folder = Path(directory)
        target = folder / 'sample.txt'
        target.write_text(source)
        if label == 'Shack':
            argv = [binary, '--no-config', '--plain', '--no-session', '--no-recovery', str(target)]
        elif label == 'Micro':
            argv = [binary, '-config-dir', str(folder / 'micro'), '-syntax', 'off',
                    '-clipboard', 'internal', '-backup', 'false', '-savehistory', 'false',
                    '-saveundo', 'false', '-eofnewline', 'false', str(target)]
        else:
            argv = [binary, '-u', 'NONE', '-i', 'NONE', '-n',
                    '-c', 'set nowrap nofixendofline', str(target)]
            if label == 'Vim':
                argv[1:1] = ['-N', '-U', 'NONE']
        started = time.perf_counter_ns()
        pid, master = pty.fork()
        if pid == 0:
            os.chdir(directory)
            os.execve(binary, argv, dict(os.environ, TERM='xterm-256color', LANG='en_US.UTF-8'))
        fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack('HHHH', 30, 100, 0, 0))

        def read(timeout):
            if not select.select([master], [], [], timeout)[0]:
                return b''
            try:
                return os.read(master, 65536)
            except OSError as exc:
                if exc.errno == errno.EIO:
                    return b''
                raise

        def drain(quiet=.02):
            out = bytearray()
            deadline = time.monotonic() + 10
            while time.monotonic() < deadline:
                data = read(quiet)
                if not data:
                    break
                out.extend(data)
            return bytes(out)

        def exchange(data):
            start = time.perf_counter_ns()
            os.write(master, data)
            output = read(10)
            delay = (time.perf_counter_ns() - start) / 1e6
            assert output, (label, kind, 'no response')
            output += drain()
            return delay, len(output)

        def rss():
            return int(subprocess.check_output(['ps', '-o', 'rss=', '-p', str(pid)]).strip()) / 1024

        try:
            output = bytearray()
            deadline = time.monotonic() + 15
            while b'COMPARE_READY' not in output and time.monotonic() < deadline:
                output.extend(read(.05))
            assert b'COMPARE_READY' in output, (label, kind, bytes(output[-2000:]))
            startup = (time.perf_counter_ns() - started) / 1e6
            drain(.3)
            initial_rss = rss()
            modal = label in ('Vim', 'Neovim')
            if kind == 'unicode_1m':
                seek_ms, _ = exchange(b'G$a' if modal else b'\x1b[1;5F')
            else:
                seek_ms = None
                if modal:
                    exchange(b'gg0i')
            drain(.2)
            delays, sizes = [], []
            for _ in range(20):
                delay, size = exchange(b'x')
                delays.append(delay)
                sizes.append(size)
            active_rss = rss()
            exchange(b'\x1b:w\r' if modal else b'\x13')
            expected = source + 'x' * 20 if kind == 'unicode_1m' else 'x' * 20 + source
            deadline = time.monotonic() + 5
            while target.read_text() != expected and time.monotonic() < deadline:
                drain(.05)
            assert target.read_text() == expected, (label, kind, 'saved contents differ', repr(target.read_text()[-100:]))
            os.write(master, b':q!\r' if modal else b'\x11')
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                done, status = os.waitpid(pid, os.WNOHANG)
                if done:
                    pid = 0
                    assert os.waitstatus_to_exitcode(status) == 0
                    break
                drain(.03)
            assert pid == 0, (label, 'failed to quit')
            return dict(startup_ms=startup, initial_rss_mib=initial_rss, editing_rss_mib=active_rss,
                        first_output_ms=delays, output_bytes=sizes, cold_end_first_output_ms=seek_ms)
        finally:
            if pid:
                os.kill(pid, signal.SIGKILL)
                os.waitpid(pid, 0)
            os.close(master)

results = {kind: {name: [] for name in editors} for kind in fixtures}
for kind, source in fixtures.items():
    for round in range(4):
        # Rotate order to reduce systematic machine-load/thermal ordering effects.
        names = list(editors)
        names = names[round:] + names[:round]
        for name in names:
            print(f'{kind}: {name}, {"warmup" if round == 0 else "run " + str(round)}', file=__import__('sys').stderr, flush=True)
            result = run(name, editors[name], kind, source)
            if round:
                results[kind][name].append(result)
summary = {}
for kind, rows in results.items():
    summary[kind] = {}
    for name, runs in rows.items():
        latencies = [v for run in runs for v in run['first_output_ms']]
        sizes = [v for run in runs for v in run['output_bytes']]
        summary[kind][name] = {
            'startup_ms_median': statistics.median(r['startup_ms'] for r in runs),
            'initial_rss_mib_median': statistics.median(r['initial_rss_mib'] for r in runs),
            'editing_rss_mib_median': statistics.median(r['editing_rss_mib'] for r in runs),
            'first_output_ms_median': statistics.median(latencies),
            'first_output_ms_max': max(latencies),
            'output_bytes_median': statistics.median(sizes),
        }
print(json.dumps(dict(summary=summary, runs=results), indent=2))
