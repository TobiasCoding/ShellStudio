#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
"""Preview locally, verify without publishing, or promote a tested stable release."""
import argparse
import fcntl
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
from publication_check import ROOT, audit, audit_commit

STABLE = re.compile(r'v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)')


def run(*args, cwd=ROOT, capture=False, **kwargs):
    result = subprocess.run(args, cwd=cwd, check=True, text=True,
                            stdout=subprocess.PIPE if capture else None, **kwargs)
    return result.stdout.strip() if capture else None


def next_version(versions, bump='patch'):
    parsed = [tuple(map(int, match.groups())) for value in versions
              if (match := STABLE.fullmatch(value))]
    if not parsed:
        raise ValueError('No stable version found')
    parts = list(max(parsed))
    index = {'major': 0, 'minor': 1, 'patch': 2}[bump]
    parts[index] += 1
    parts[index + 1:] = [0] * (2 - index)
    return '.'.join(map(str, parts))


def clean(root):
    if run('git', 'status', '--porcelain', '--untracked-files=all', cwd=root, capture=True):
        raise ValueError('Commit reviewed source changes before publishing (working tree must be clean)')


def preview_env(root):
    base = root / '.work' / 'preview'
    env = os.environ.copy()
    for key, directory in [('XDG_CONFIG_HOME', 'config'), ('XDG_DATA_HOME', 'data'),
                           ('XDG_STATE_HOME', 'state'), ('XDG_RUNTIME_DIR', 'r')]:
        path = base / directory
        path.mkdir(parents=True, exist_ok=True, mode=0o700)
        env[key] = str(path)
    env['SHELLSTUDIO_NO_UPDATE_CHECK'] = '1'
    for key in ('TMUX', 'TMUX_PANE', 'SHELLSTUDIO_CONSOLE', 'SHELLSTUDIO_JUST_UPDATED'):
        env.pop(key, None)
    return env


def preview(root, build_only=False):
    revision = run('git', 'rev-parse', '--short', 'HEAD', cwd=root, capture=True)
    version = (root / 'VERSION').read_text().strip() + '-dev.' + revision
    run('make', 'build', f'VERSION={version}', cwd=root)
    target = root / '.work' / 'preview' / 'shellstudio'
    target.parent.mkdir(parents=True, exist_ok=True)
    pending = target.with_suffix('.new')
    shutil.copy2(root / 'bin' / 'shellstudio', pending)
    pending.replace(target)
    env = preview_env(root)
    print(f'Preview {version}: isolated data in {target.parent}', flush=True)
    if not build_only:
        run(str(target), cwd=root, env=env)


def publish(root, bump):
    clean(root)
    if run('git', 'branch', '--show-current', cwd=root, capture=True) != 'main':
        raise ValueError('Publish from the reviewed main branch')
    run('git', 'fetch', 'origin', '--tags', cwd=root)
    run('git', 'merge-base', '--is-ancestor', 'origin/main', 'HEAD', cwd=root)
    original = run('git', 'rev-parse', 'HEAD', cwd=root, capture=True)
    names = audit(root)
    # Audit every outgoing commit, including files deleted again before HEAD.
    outgoing = run('git', 'rev-list', 'origin/main..HEAD', cwd=root, capture=True).splitlines()
    for commit in outgoing:
        audit_commit(root, commit)
    versions = run('git', 'tag', '--list', cwd=root, capture=True).splitlines()
    versions.append((root / 'VERSION').read_text().strip())
    version = next_version(versions, bump)
    epoch = run('git', 'log', '-1', '--format=%ct', cwd=root, capture=True)
    (root / '.work').mkdir(exist_ok=True)
    # Freeze the candidate before testing; no version, commit, tag or push on failure.
    with tempfile.TemporaryDirectory(prefix='rc-', dir=root / '.work') as directory:
        candidate = Path(directory)
        for name in names:
            target = candidate / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(root / name, target)
        (candidate / '.source-files.json').write_text(json.dumps(names))
        (candidate / 'VERSION').write_text(version + '\n')
        print(f'Checking candidate {version} in isolation', flush=True)
        run('make', 'check', cwd=candidate)
        run('make', 'release', f'SOURCE_DATE_EPOCH={epoch}', cwd=candidate)
        clean(root)
        if run('git', 'rev-parse', 'HEAD', cwd=root, capture=True) != original:
            raise ValueError('Source changed during verification; nothing published')
        for name in names:
            if name != 'VERSION' and (root / name).read_bytes() != (candidate / name).read_bytes():
                raise ValueError(f'{name}: candidate changed during verification')
        (root / 'VERSION').write_text(version + '\n')
        run('git', 'add', '--', 'VERSION', cwd=root)
        run('git', 'commit', '-m', f'Release ShellStudio {version}', cwd=root)
        clean(root)
        changed = run('git', 'diff', '--name-only', original, 'HEAD', cwd=root, capture=True)
        if changed != 'VERSION' or run('git', 'show', 'HEAD:VERSION', cwd=root, capture=True) != version:
            raise ValueError('Release commit differs from the tested candidate; nothing pushed')
        audit_commit(root, 'HEAD')
        run('git', 'tag', '-a', f'v{version}', '-m', f'ShellStudio {version}', cwd=root)
        try:
            run('git', '-c', 'push.followTags=false', 'push', '--atomic', 'origin',
                'HEAD:refs/heads/main', f'refs/tags/v{version}', cwd=root)
        except subprocess.CalledProcessError:
            print(f'Push failed. Verified commit and tag v{version} remain locally. '
                  f'Retry: git -c push.followTags=false push --atomic origin HEAD:refs/heads/main refs/tags/v{version}', flush=True)
            raise
    print(f'Pushed v{version}. GitHub Actions will repeat checks and publish all assets together.\n'
          'Follow Publish release: https://github.com/TobiasCoding/ShellStudio/actions\n'
          f'Release (available after CI succeeds): https://github.com/TobiasCoding/ShellStudio/releases/tag/v{version}')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    preview_parser = sub.add_parser('preview', help='Build and launch with separate local data; never publish')
    preview_parser.add_argument('--build-only', action='store_true')
    sub.add_parser('check', help='Run all tests without bumping, packaging or publishing')
    publish_parser = sub.add_parser('publish', help='Test, bump, commit and atomically push main and its release tag')
    publish_parser.add_argument('--bump', choices=['patch', 'minor', 'major'], default='patch')
    args = parser.parse_args()
    (ROOT / '.work').mkdir(exist_ok=True)
    with (ROOT / '.work' / 'release.lock').open('w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if args.command == 'preview':
            preview(ROOT, args.build_only)
        elif args.command == 'check':
            run('make', 'check')
        else:
            publish(ROOT, args.bump)


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        raise SystemExit(f'Release stopped: {error}')
