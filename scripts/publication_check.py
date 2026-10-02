#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
"""Audit the exact tracked source set; ignored files are never packaging inputs."""
import json
from pathlib import Path, PurePosixPath
import re
import subprocess

ROOT = Path(__file__).resolve().parents[1]
DIRECTORIES = {'.github', 'cmd', 'internal', 'tests', 'scripts', 'docs', 'examples', 'third_party'}
FILES = {'.gitignore', 'VERSION', 'Makefile', 'go.mod', 'go.sum', 'LICENSE',
         'README.md', 'CONTRIBUTING.md', 'SECURITY.md', 'THIRD_PARTY_NOTICES.md', 'logo.svg'}
PRIVATE = re.compile(r'(^|/)(\.env(?:\..*)?|credentials?(?:\..*)?|\.work|\.git|bin|dist|personal|runtime|__pycache__)(/|$)|\.(db(?:-.*)?|sqlite3?(?:-.*)?|log|pem|key|p12|pfx|sock|pid|pyc|bak)$', re.I)
PATTERNS = {
    'absolute home path': r'/home/[a-zA-Z0-9_-]+/',
    'private key': r'-----BEGIN (?:RSA |OPENSSH |EC )?PRIVATE KEY-----',
    'API token': r'(?:sk-ant-|sk-proj-|ghp_|github_pat_)[A-Za-z0-9_-]{20,}',
    'source workspace leakage': r'/opt/(?:binance-trading|mcp)/',
}


def source_files(root=ROOT):
    if (root / '.git').exists():
        raw = subprocess.check_output(['git', 'ls-files', '-z'], cwd=root)
        return sorted(raw.decode().strip('\0').split('\0'))
    return json.loads((root / '.source-files.json').read_text())


def audit_entry(name, data, regular=True):
    p = PurePosixPath(name)
    if (p.is_absolute() or '..' in p.parts or not p.parts or
            (name not in FILES and p.parts[0] not in DIRECTORIES) or PRIVATE.search(name)):
        raise ValueError(f'{name}: unapproved or private source path')
    if not regular:
        raise ValueError(f'{name}: source must be a regular file')
    try:
        text = data.decode('utf-8')
    except UnicodeError:
        raise ValueError(f'{name}: unexpected binary') from None
    for label, pattern in PATTERNS.items():
        if re.search(pattern, text):
            raise ValueError(f'{name}: {label}')


def audit(root=ROOT):
    names = source_files(root)
    for name in names:
        path = root / name
        audit_entry(name, path.read_bytes(), path.is_file() and not path.is_symlink())
    return names


def audit_commit(root, revision):
    entries = subprocess.check_output(['git', 'ls-tree', '-rz', '--full-tree', revision], cwd=root).split(b'\0')
    files = []
    for entry in filter(None, entries):
        metadata, name = entry.split(b'\t', 1)
        mode, kind, oid = metadata.decode().split()
        if kind != 'blob' or mode not in ('100644', '100755'):
            raise ValueError(f'{name.decode()}: source must be a regular file')
        files.append((name.decode(), oid))
    # Read Git objects directly: export-ignore attributes cannot hide history.
    raw = subprocess.check_output(['git', 'cat-file', '--batch'], cwd=root,
                                  input=''.join(oid + '\n' for _, oid in files).encode())
    offset = 0
    for name, _ in files:
        end = raw.index(b'\n', offset)
        size = int(raw[offset:end].split()[2])
        audit_entry(name, raw[end + 1:end + 1 + size])
        offset = end + size + 2


if __name__ == '__main__':
    try:
        print(f'Publication scan: {len(audit())} tracked source files, no findings')
    except (ValueError, OSError) as error:
        raise SystemExit(str(error))
