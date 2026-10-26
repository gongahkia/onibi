#!/usr/bin/env python3
"""Real tmux workflow for document links, media and fenced code."""
import os
from pathlib import Path
import shutil
import struct
import subprocess
import tempfile
import time
import zlib

tmux = os.environ.get('TMUX_BINARY') or shutil.which('tmux')
if not tmux:
    raise SystemExit('tmux is needed for this development check')
binary = str(Path(os.environ.get('SHACK_BINARY', 'build/shack')).resolve())
with tempfile.TemporaryDirectory(prefix='shack-reading-') as directory:
    root = Path(directory)
    source = root / 'index.md'
    original = '''# First

[Jump](#destination)

[Other](other%20note.md#target)

Footnote[^one].

![pixels](pixels.png)

```go
package main
var message = "hello"
```

```mermaid
flowchart LR
A[Start] --> B[Done]
```

# Destination

Arrived at destination.

[^one]: Footnote body.
'''
    source.write_text(original)
    other = root / 'other note.md'
    other.write_text('# Target\n\nOther document body.\n')
    def chunk(kind, data):
        return struct.pack('>I', len(data)) + kind + data + struct.pack('>I', zlib.crc32(kind + data))
    png = (b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('>IIBBBBB', 2, 2, 8, 2, 0, 0, 0))
           + chunk(b'IDAT', zlib.compress(b'\0\xff\0\0\0\xff\0\0\0\0\xff\xff\xff\xff')) + chunk(b'IEND', b''))
    (root / 'pixels.png').write_bytes(png)
    base = [tmux, '-S', str(root / 'socket'), '-f', '/dev/null']
    def run(*args):
        return subprocess.run(base + list(args), check=True, capture_output=True)
    def keys(*args):
        run('send-keys', '-t', 'shack', *args)
    def type_text(value):
        run('send-keys', '-l', '-t', 'shack', value)
    def pane():
        return run('capture-pane', '-p', '-t', 'shack').stdout.decode()
    def until(predicate, label):
        for _ in range(200):
            if predicate():
                return
            time.sleep(.04)
        raise AssertionError(label + '\n' + pane())
    def wait(value):
        until(lambda: value in pane(), 'missing ' + value)
    def command(value):
        keys('F2')
        type_text(value)
        keys('Enter')
    def follow(value):
        command('Document preview links')
        wait('Document links')
        type_text(value)
        keys('Enter')
        until(lambda: 'Document links' not in pane(), 'link picker did not close')
    try:
        run('new-session', '-d', '-x', '140', '-y', '48', '-s', 'shack', binary,
            '--no-config', '--no-lsp', '--no-session', '--no-recovery', str(source))
        wait('Ctrl+E commands')
        keys('F4')
        wait('▀')
        wait('Start')
        wait('Done')
        wait('package main')
        assert 'A[Start]' not in pane(), 'Mermaid source was not rendered'
        follow('#destination')
        wait('Document link · destination')
        command('Jump back')
        follow('Footnote 1')
        wait('Document link · Footnote 1')
        follow('Back to footnote 1 reference 1')
        wait('Document link · Back to footnote 1 reference 1')
        follow('other%20note.md#target')
        wait('Other document body.')
        wait('Document link · target')
        command('Jump back')
        wait('Preview · index.md')
        command('Toggle preview media')
        until(lambda: 'A[Start]' in pane() and '▀' not in pane(), 'media did not turn off')
        command('Toggle preview media')
        wait('▀')
        follow('pixels.png')
        wait('Image · pixels.png')
        wait('▀')
        command('Jump back')
        wait('Preview · index.md')
        keys('F4')
        wait('index.md')
        assert source.read_text() == original
        assert other.read_text() == '# Target\n\nOther document body.\n'
        assert (root / 'pixels.png').read_bytes() == png
        keys('C-q')
        print('tmux heading/footnote/backlink/file/image navigation, media toggles, Mermaid, code and unchanged sources: PASS')
    finally:
        subprocess.run(base + ['kill-server'], capture_output=True)
