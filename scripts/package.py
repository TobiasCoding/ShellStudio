#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
"""Package only audited tracked files, including licenses and vendored source."""
import argparse
import gzip
import hashlib
import io
import json
import re
import shutil
import tarfile
from publication_check import ROOT, audit


def package(root, version, epoch=0):
    if not re.fullmatch(r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)', version):
        raise ValueError('Packaging requires a stable numeric version')
    if (root / 'VERSION').read_text().strip() != version:
        raise ValueError('VERSION file does not match the package version')
    names = audit(root)
    dist = root / 'dist'
    dist.mkdir(exist_ok=True)
    assets = ['shellstudio-linux-amd64', 'shellstudio-linux-arm64']
    for name, machine in zip(assets, (62, 183)):
        data = (dist / name).read_bytes()
        if len(data) < 64 or data[:6] != b'\x7fELF\x02\x01' or int.from_bytes(data[18:20], 'little') != machine:
            raise ValueError(f'{name}: wrong or empty ELF binary')
    archive = f'shellstudio-{version}-source.tar.gz'
    with (dist / archive).open('wb') as output:
        with gzip.GzipFile(filename='', mode='wb', fileobj=output, mtime=epoch) as compressed:
            with tarfile.open(fileobj=compressed, mode='w') as tar:
                for name in names:
                    path = root / name
                    data = path.read_bytes()
                    info = tarfile.TarInfo(name)
                    info.size, info.mtime = len(data), epoch
                    info.mode = 0o755 if path.stat().st_mode & 0o111 else 0o644
                    tar.addfile(info, io.BytesIO(data))
                data = json.dumps(names).encode()
                info = tarfile.TarInfo('.source-files.json')
                info.size, info.mtime = len(data), epoch
                tar.addfile(info, io.BytesIO(data))
    assets.append(archive)
    for name in ('LICENSE', 'THIRD_PARTY_NOTICES.md'):
        shutil.copyfile(root / name, dist / name)
        assets.append(name)
    (dist / 'SHA256SUMS').write_text(''.join(
        f'{hashlib.sha256((dist / name).read_bytes()).hexdigest()}  {name}\n' for name in assets))
    print(f'Packaged {len(names)} audited source files and {len(assets)} assets')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    parser.add_argument('--epoch', type=int, default=0)
    args = parser.parse_args()
    package(ROOT, args.version, args.epoch)
