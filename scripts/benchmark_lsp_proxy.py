#!/usr/bin/env python3
"""Transparent development-only LSP recorder; logs metadata, never source text."""
import json
import os
import subprocess
import sys
import threading
import time

server = subprocess.Popen([os.environ['SHACK_BENCH_GOPLS'], *sys.argv[1:]], stdin=subprocess.PIPE, stdout=subprocess.PIPE)
lock = threading.Lock()
pending = {}

def log(value):
    with lock:
        with open(os.environ['SHACK_BENCH_TRACE'], 'a') as output:
            output.write(json.dumps(dict(t=time.monotonic(), **value)) + '\n')

log(dict(kind='start', pid=os.getpid(), ppid=os.getppid(), server_pid=server.pid))

def forward(source, target, direction):
    try:
        while True:
            headers = bytearray()
            length = None
            while True:
                line = source.readline()
                if not line:
                    return
                headers.extend(line)
                if line.lower().startswith(b'content-length:'):
                    length = int(line.split(b':', 1)[1])
                if line in (b'\r\n', b'\n'):
                    break
            if length is None or length > 16 << 20:
                raise ValueError('invalid LSP frame')
            body = source.read(length)
            if len(body) != length:
                return
            message = json.loads(body)
            method = message.get('method')
            if direction == 'client' and method and 'id' in message:
                pending[message['id']] = method
            record = dict(kind=direction, method=method)
            if direction == 'server' and 'id' in message and method is None:
                record.update(request=pending.pop(message['id'], None), error=message.get('error'))
                result = message.get('result')
                if isinstance(result, list):
                    record['items'] = len(result)
                elif isinstance(result, dict) and 'items' in result:
                    record['items'] = len(result['items'])
            if method == 'textDocument/publishDiagnostics':
                record['diagnostics'] = len(message.get('params', {}).get('diagnostics', []))
            log(record)
            target.write(headers)
            target.write(body)
            target.flush()
    except (BrokenPipeError, OSError):
        pass
    finally:
        if direction == 'client':
            target.close()

thread = threading.Thread(target=forward, args=(sys.stdin.buffer, server.stdin, 'client'), daemon=True)
thread.start()
forward(server.stdout, sys.stdout.buffer, 'server')
server.wait()
