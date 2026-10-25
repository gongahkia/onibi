#!/usr/bin/env python3
"""Run the Linux binary in a scratch image with no Go, libc, shell or network."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import uuid

root = Path(__file__).resolve().parent.parent
name = 'shack-check-' + uuid.uuid4().hex[:12]
with tempfile.TemporaryDirectory(prefix='shack-container-') as directory:
    context = Path(directory)
    shutil.copy2(root / 'build/release/linux-amd64/shack', context / 'shack')
    (context / 'Dockerfile').write_text('FROM scratch\nCOPY shack /shack\n')
    try:
        build = subprocess.run(['docker', 'build', '--platform', 'linux/amd64', '--network', 'none', '-t', name, str(context)], capture_output=True)
        if build.returncode:
            raise RuntimeError(build.stderr.decode())
        subprocess.run(['docker', 'run', '--rm', '--network', 'none', '--memory', '64m', '--cpus', '0.5', name, '/shack', '--version'], check=True)
        command = ['docker', 'run', '--rm', '--name', name, '--network', 'none', '--memory', '64m', '--cpus', '0.5', '-it', '-e', 'TERM=xterm-256color', '-e', 'LANG=C.UTF-8', '--mount', 'type=bind,source={directory},target=/work', '-w', '/work', name, '/shack', '--no-config', '--no-lsp', '/work/sample.txt']
        env = dict(os.environ, SHACK_COMMAND=json.dumps(command))
        subprocess.run([sys.executable, str(root / 'scripts/pty_smoke.py')], env=env, check=True)
        print('Linux scratch container, 64 MiB memory, 0.5 CPU, no network or installed runtime: PASS')
    finally:
        subprocess.run(['docker', 'rm', '-f', name], capture_output=True)
        subprocess.run(['docker', 'image', 'rm', name], capture_output=True)
