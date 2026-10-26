#!/usr/bin/env python3
"""Matched code/LSP comparison with explicitly recorded editor configurations.

Requires prepared tools under build/compare-tools, gopls, and a private snapshot
of the user's Neovim config/data. Does not modify the user's live configuration.
"""
import argparse
import errno
import fcntl
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import pty
import select
import shutil
import signal
import statistics
import struct
import subprocess
import sys
import tempfile
import termios
import time

ROOT = Path(__file__).resolve().parent.parent
TOOLS = ROOT / 'build/compare-tools'
SNAPSHOT = TOOLS / 'nvim-snapshot'
HELIX = TOOLS / 'helix-25.07.1-x86_64-macos'
PROFILES = ['Shack', 'Micro+LSP', 'Neovim-current', 'Neovim+gopls', 'Helix', 'Kakoune+LSP']
SOURCE = '// COMPARE_READY\npackage bench\n\nvar Diagnosis = unresolvedName\n\n' + ''.join(
    f'// Transform{i} processes a value.\nfunc Transform{i}(value int) int {{\n\treturn value + {i}\n}}\n\n' for i in range(200))

SOURCE = SOURCE.rstrip() + "\n"

class Session:
    def __init__(self, profile, folder):
        self.profile, self.folder = profile, folder
        self.trace = folder / 'lsp.jsonl'
        self.target = folder / 'main.go'
        self.target.write_text(SOURCE)
        (folder / 'go.mod').write_text('module example.com/bench\n\ngo 1.24.0\n')
        (folder / 'other.go').write_text('package bench\n\nfunc Helper(value int) int { return value * 2 }\n')
        binpath = folder / 'bin'
        binpath.mkdir()
        wrapper = binpath / 'gopls'
        wrapper.symlink_to(ROOT / 'scripts/benchmark_lsp_proxy.py')
        env = dict(os.environ, TERM='xterm-256color', LANG='en_US.UTF-8',
                   PATH=str(binpath) + ':' + str(TOOLS) + ':' + os.environ['PATH'],
                   SHACK_BENCH_STATE=str(folder / 'nvim-health.json'),
                   SHACK_BENCH_GOPLS=str(ROOT / 'build/tools/gopls'), SHACK_BENCH_TRACE=str(self.trace),
                   GOTOOLCHAIN='local', GOPROXY='off', GOSUMDB='off')
        self.control = 'nonmodal'
        if profile == 'Shack':
            argv = [str(ROOT / 'build/shack'), '--no-config', '--no-session', '--no-recovery', str(self.target)]
        elif profile == 'Micro+LSP':
            cfg = folder / 'micro'
            (cfg / 'plug').mkdir(parents=True)
            (cfg / 'plug/lsp').symlink_to(TOOLS / 'micro-lsp')
            (cfg / 'settings.json').write_text(json.dumps({'lsp.server': 'go=gopls', 'lsp.formatOnSave': False,
                 'clipboard': 'internal', 'backup': False, 'savehistory': False, 'saveundo': False}))
            argv = [shutil.which('micro'), '-config-dir', str(cfg), str(self.target)]
        elif profile.startswith('Neovim'):
            self.control = 'vim'
            env.update(XDG_CONFIG_HOME=str(SNAPSHOT / 'config'), XDG_DATA_HOME=str(SNAPSHOT / 'data'),
                       XDG_STATE_HOME=str(folder / 'nvim-state'), XDG_CACHE_HOME=str(SNAPSHOT / 'cache'))
            argv = [shutil.which('nvim'), '-i', 'NONE', '-n']
            if profile == 'Neovim+gopls':
                argv += ['-c', "lua vim.lsp.start({name='gopls',cmd={'gopls'},root_dir=vim.fn.getcwd(),capabilities=require('cmp_nvim_lsp').default_capabilities()})"]
            argv += [str(self.target)]
        elif profile == 'Helix':
            self.control = 'helix'
            cfg = folder / 'helix.toml'
            cfg.write_text('[editor]\ntrue-color = true\n')
            env.update(HELIX_RUNTIME=str(HELIX / 'runtime'), XDG_CONFIG_HOME=str(folder / 'config'), XDG_CACHE_HOME=str(folder / 'cache'))
            argv = [str(HELIX / 'hx'), '-c', str(cfg), '--log', str(folder / 'helix.log'), str(self.target)]
        else:
            self.control = 'kak'
            cfg = folder / 'kak'
            cfg.mkdir()
            (cfg / 'kakrc').write_text('eval %sh{kak-lsp}\nlsp-enable\n')
            env['KAKOUNE_CONFIG_DIR'] = str(cfg)
            self.kak_session = 'sb' + str(os.getpid())
            argv = [shutil.which('kak'), '-s', self.kak_session, str(self.target)]
        self.argv = argv
        self.started = time.monotonic()
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.chdir(folder)
            os.execvpe(argv[0], argv, env)
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack('HHHH', 30, 100, 0, 0))
        self.output = bytearray()
        self.ack_offset = 0
        self.startup_prompts = 0

    def read(self, timeout=.02):
        if select.select([self.fd], [], [], timeout)[0]:
            try:
                data = os.read(self.fd, 65536)
            except OSError as exc:
                if exc.errno == errno.EIO:
                    return b''
                raise
            self.output.extend(data)
            return data
        return b''

    def drain(self, quiet=.05):
        out = bytearray()
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            data = self.read(quiet)
            if not data:
                break
            out.extend(data)
        return bytes(out)

    def records(self):
        if not self.trace.exists():
            return []
        return [json.loads(line) for line in self.trace.read_text().splitlines() if line.endswith('}')]

    def until(self, predicate, label, timeout=30):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if b'Press ENTER or type command to continue' in self.output[self.ack_offset:]:
                self.send(b'\r')
                self.ack_offset = len(self.output)
                self.startup_prompts += 1
            if predicate():
                return
            self.read(.05)
        raise AssertionError((self.profile, label, bytes(self.output[-2000:]), self.records()[-10:]))

    def send(self, value):
        os.write(self.fd, value)

    def memory(self):
        rows = [tuple(map(int, row.split())) for row in subprocess.check_output(['ps', '-axo', 'pid=,ppid=,rss='], text=True).splitlines()]
        roots = [self.pid]
        starts = [r for r in self.records() if r['kind'] == 'start']
        for r in starts:
            roots += [r['pid'], r['server_pid'], r['ppid']]
        def descendants(roots):
            seen = set()
            while roots:
                pid = roots.pop()
                if pid in seen:
                    continue
                seen.add(pid)
                roots += [p for p, parent, _ in rows if parent == pid]
            return seen
        seen = descendants(roots)
        services = descendants([pid for r in starts for pid in (r['pid'], r['server_pid'])])
        return dict(rss_mib=sum(rss for p, _, rss in rows if p in seen)/1024,
                    editor_and_bridge_mib=sum(rss for p, _, rss in rows if p in seen-services)/1024,
                    server_and_recorder_mib=sum(rss for p, _, rss in rows if p in seen & services)/1024,
                    processes=sum(1 for p, _, _ in rows if p in seen))

    def run(self):
        self.until(lambda: b'COMPARE_READY' in self.output, 'first file display')
        startup = (time.monotonic() - self.started)*1000
        if self.profile != 'Neovim-current':
            self.until(lambda: any(r.get('diagnostics', 0)>0 for r in self.records()), 'live diagnostic publication', 45)
        else:
            self.drain(.8)
        ready = (time.monotonic() - self.started)*1000
        self.drain(.4)
        initial = self.memory()
        if self.control == 'vim':
            self.send(b'gg$')
        elif self.control == 'helix':
            self.send(b'gg')
        elif self.control == 'kak':
            self.send(b'gg')
        else:
            self.send(b'\x1b[F')
        if self.control in ('vim','helix','kak'):
            self.send(b'a' if self.control == 'vim' else b'A')
        self.drain(.4)
        delays, sizes, settled = [], [], []
        for _ in range(20):
            self.drain(.03)
            start = time.monotonic()
            self.send(b'x')
            first = self.read(10)
            assert first, (self.profile, 'no response')
            delays.append((time.monotonic()-start)*1000)
            sizes.append(len(first)+len(self.drain(.03)))
            # Include delayed syntax/LSP redraws in a separate output window.
            settled.append(sizes[-1]+len(self.drain(.25)))
        active = self.memory()
        if self.control != 'nonmodal':
            self.send(b'\x1b')
            self.drain(.15)
            self.send(b':w\r')
        else:
            self.send(b'\x13')
        expected = SOURCE.replace('// COMPARE_READY', '// COMPARE_READY'+'x'*20, 1)
        self.until(lambda:self.target.read_text()==expected, 'verified save')
        rec = self.records()
        changes = sum(r.get('method')=='textDocument/didChange' and r['kind']=='client' for r in rec)
        if self.profile != 'Neovim-current':
            assert changes>0, (self.profile, 'server did not receive edits')
        health = None
        if self.control == 'vim':
            self.send(b":lua vim.fn.writefile({vim.json.encode({treesitter=vim.treesitter.highlighter.active[vim.api.nvim_get_current_buf()]~=nil,clients=vim.tbl_map(function(c) return c.name end,vim.lsp.get_clients({bufnr=0}))})},vim.env.SHACK_BENCH_STATE)\r")
            health_path = self.folder / 'nvim-health.json'
            self.until(health_path.exists, 'Neovim runtime state')
            health = json.loads(health_path.read_text())
            assert health['treesitter'], 'Neovim syntax highlighter is inactive'
            assert ('gopls' in health['clients']) == (self.profile == 'Neovim+gopls'), health
        self.send(b':q!\r' if self.control != 'nonmodal' else b'\x11')
        self.until(lambda:self.exited(), 'clean exit', 10)
        return dict(first_file_ms=startup, lsp_ready_ms=ready if self.profile!='Neovim-current' else None,
                    initial=initial, editing=active, first_output_ms=delays, immediate_bytes=sizes,
                    settled_bytes=settled, did_change_count=changes, diagnostic_publications=sum('diagnostics' in r for r in rec),
                    startup_prompts=self.startup_prompts, nvim_health=health, gopls_started=any(r['kind']=='start' for r in rec), server_records=rec)

    def exited(self, check=True):
        if not self.pid:
            return True
        done, status = os.waitpid(self.pid, os.WNOHANG)
        if done:
            self.pid = 0
            if check: assert os.waitstatus_to_exitcode(status)==0
            return True
        return False

    def close(self):
        if self.pid:
            os.kill(self.pid, signal.SIGTERM)
            deadline=time.monotonic()+3
            while time.monotonic()<deadline and not self.exited(check=False):
                self.read(.05)
            if self.pid:
                os.kill(self.pid, signal.SIGKILL)
                os.waitpid(self.pid,0)
                self.pid=0
        for record in self.records():
            if record['kind']=='start':
                for pid in (record['server_pid'],record['pid']):
                    try:os.kill(pid,signal.SIGTERM)
                    except ProcessLookupError:pass
        os.close(self.fd)

def provenance():
    binaries = {'Shack': (ROOT / 'build/shack', '--version'),
                'Micro': (Path(shutil.which('micro')), '-version'),
                'Neovim': (Path(shutil.which('nvim')), '--version'),
                'Helix': (HELIX / 'hx', '--version'),
                'Kakoune': (Path(shutil.which('kak')), '-version'),
                'kak-lsp': (TOOLS / 'kak-lsp', '--version'),
                'gopls': (ROOT / 'build/tools/gopls', 'version')}
    info = dict(system=platform.platform(), machine=platform.machine(),
                cpu=subprocess.check_output(['sysctl', '-n', 'machdep.cpu.brand_string'], text=True).strip(),
                terminal='xterm-256color', dimensions=[100,30], keys_per_run=20,
                source_bytes=len(SOURCE.encode()), source_lines=len(SOURCE.splitlines()),
                source_sha256=hashlib.sha256(SOURCE.encode()).hexdigest(),
                repository_commit=subprocess.check_output(['git','rev-parse','HEAD'], cwd=ROOT, text=True).strip(),
                versions={}, binary_sha256={}, config_sha256={}, script_sha256={})
    for name,(binary,flag) in binaries.items():
        info['versions'][name]=subprocess.check_output([str(binary),flag], text=True, stderr=subprocess.STDOUT).splitlines()[:3]
        info['binary_sha256'][name]=hashlib.sha256(binary.read_bytes()).hexdigest()
    for name in ('init.lua','lazy-lock.json'):
        info['config_sha256'][name]=hashlib.sha256((SNAPSHOT / 'config/nvim' / name).read_bytes()).hexdigest()
    info['nvim_plugin_locks']=json.loads((SNAPSHOT / 'config/nvim/lazy-lock.json').read_text())
    info['micro_lsp_commit']=subprocess.check_output(['git','rev-parse','HEAD'],cwd=TOOLS/'micro-lsp',text=True).strip()
    for name in ('ide_compare.py','benchmark_lsp_proxy.py'):
        info['script_sha256'][name]=hashlib.sha256((ROOT/'scripts'/name).read_bytes()).hexdigest()
    return info


def summarize(results):
    summary={}
    for name,runs in results.items():
        if not runs: continue
        latencies=sorted(x for r in runs for x in r['first_output_ms'])
        summary[name]=dict(first_output_ms_median=statistics.median(latencies),
            first_output_ms_p95=latencies[math.ceil(.95*len(latencies))-1],
            first_output_ms_max=max(latencies),
            rss_mib=statistics.median(r['editing']['rss_mib'] for r in runs),
            editor_and_bridge_mib=statistics.median(r['editing']['editor_and_bridge_mib'] for r in runs),
            immediate_bytes=statistics.median(x for r in runs for x in r['immediate_bytes']),
            settled_bytes=statistics.median(x for r in runs for x in r['settled_bytes']))
    return summary


if __name__ == '__main__':
    parser=argparse.ArgumentParser()
    parser.add_argument('--profiles',default=','.join(PROFILES))
    parser.add_argument('--runs',type=int,default=3)
    parser.add_argument('--output',default='build/ide-comparison.json')
    args=parser.parse_args()
    profiles=args.profiles.split(',')
    if set(profiles)-set(PROFILES): parser.error('unknown profile')
    if args.runs<0: parser.error('runs must be nonnegative')
    metadata=provenance()
    metadata.update(measured_runs=args.runs, discarded_warmups=1)
    results={name:[] for name in profiles}
    for run in range(args.runs+1):
        for name in profiles[run%len(profiles):]+profiles[:run%len(profiles)]:
            print(f'{name}: '+('warmup' if run==0 else f'run {run}'),file=sys.stderr,flush=True)
            with tempfile.TemporaryDirectory(prefix='shack-ide-compare-') as directory:
                session=Session(name,Path(directory))
                try:
                    value=session.run()
                    if run:results[name].append(value)
                finally:session.close()
            Path(args.output).write_text(json.dumps(dict(metadata=metadata, summary=summarize(results), runs=results),indent=2))
    for name,values in summarize(results).items():
        print(name,json.dumps(values))
