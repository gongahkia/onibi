#!/usr/bin/env python3
"""Verify current release archives, packaged documentation and executable bytes."""
import hashlib
from pathlib import Path
import tarfile

root = Path(__file__).resolve().parent.parent
version = (root / 'VERSION').read_text().strip()
required = {'shack', 'THIRD_PARTY_LICENSES.txt'} | {
    path.name for path in (root / 'docs').glob('*.md')}
if (root / 'LICENSE').is_file():
    required.add('LICENSE')
checksums = {}
for line in (root / 'dist/SHA256SUMS').read_text().splitlines():
    digest, name = line.split(maxsplit=1)
    checksums[name.removeprefix('./')] = digest
for target in ('linux-amd64', 'linux-arm64', 'linux-arm', 'darwin-amd64', 'darwin-arm64'):
    binary = root / 'build/release' / target / 'shack'
    archive = root / 'dist' / f'shack-{version}-{target}.tar.gz'
    assert hashlib.sha256(archive.read_bytes()).hexdigest() == checksums[archive.name], archive
    with tarfile.open(archive) as package:
        names = package.getnames()
        assert set(names) == required and len(names) == len(required), (archive, names)
        for member in package.getmembers():
            assert member.isfile(), (archive, member.name)
        assert package.getmember('shack').mode & 0o111, 'executable bit missing'
        assert package.extractfile('shack').read() == binary.read_bytes(), archive
        for name in required - {'shack'}:
            source = root / 'docs' / name
            if name == 'LICENSE':
                source = root / name
            elif name == 'THIRD_PARTY_LICENSES.txt':
                source = root / 'build' / name
            assert package.extractfile(name).read() == source.read_bytes(), (archive, name)
    print(f'{target}: binary {binary.stat().st_size / (1 << 20):.3f} MiB, '
          f'archive {archive.stat().st_size / (1 << 20):.3f} MiB; contents/checksum PASS')
