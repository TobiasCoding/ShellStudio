#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
"""Exercise release gates with local Git repositories; never contact GitHub."""
import hashlib
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import release
import package
import publication_check as publication


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        (ROOT / '.work').mkdir(exist_ok=True)
        self.temp = tempfile.TemporaryDirectory(prefix='rel-', dir=ROOT / '.work')
        self.base = Path(self.temp.name)
        self.root = self.base / 'repo'
        self.root.mkdir()
        self.git('init', '-b', 'main')
        self.git('config', 'user.name', 'Release Test')
        self.git('config', 'user.email', 'test@example.invalid')
        (self.root / 'VERSION').write_text('1.2.3\n')
        (self.root / '.gitignore').write_text('/.work/\n/dist/\n*.db\n.env\n')
        for name in ('README.md', 'LICENSE', 'THIRD_PARTY_NOTICES.md'):
            (self.root / name).write_text('Public fixture\n')
        self.git('add', '.')
        self.git('commit', '-qm', 'Initial public source')

    def tearDown(self):
        self.temp.cleanup()

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.root, text=True, stderr=subprocess.DEVNULL).strip()

    def remote(self):
        remote = self.base / 'remote.git'
        subprocess.run(['git', 'init', '--bare', str(remote)], check=True, capture_output=True)
        self.git('remote', 'add', 'origin', str(remote))
        self.git('push', '-u', 'origin', 'main')

    def test_numeric_version_bumps(self):
        versions = ['v1.2.9', '1.2.10', 'v9.0.0-rc.1', 'v01.2.0', 'garbage']
        self.assertEqual(release.next_version(versions), '1.2.11')
        self.assertEqual(release.next_version(versions, 'minor'), '1.3.0')
        self.assertEqual(release.next_version(versions, 'major'), '2.0.0')

    def test_preview_isolates_all_xdg_paths(self):
        with patch.dict(os.environ, {'TMUX': 'live', 'TMUX_PANE': '%1', 'SHELLSTUDIO_CONSOLE': 'live'}):
            env = release.preview_env(self.root)
        for key in ('XDG_CONFIG_HOME', 'XDG_DATA_HOME', 'XDG_STATE_HOME', 'XDG_RUNTIME_DIR'):
            self.assertTrue(Path(env[key]).is_relative_to(self.root / '.work' / 'preview'))
        self.assertEqual(env['SHELLSTUDIO_NO_UPDATE_CHECK'], '1')
        self.assertNotIn('TMUX', env)
        self.assertNotIn('SHELLSTUDIO_CONSOLE', env)

    def test_scan_excludes_untracked_private_files(self):
        (self.root / 'notes.db').write_bytes(b'personal')
        (self.root / '.env').write_text('private configuration')
        self.assertNotIn('.env', publication.audit(self.root))
        release.clean(self.root)

    def test_force_added_private_file_is_rejected(self):
        (self.root / 'notes.db').write_bytes(b'personal')
        self.git('add', '-f', 'notes.db')
        with self.assertRaisesRegex(ValueError, 'private source path'):
            publication.audit(self.root)

    def test_symlinks_and_unapproved_roots_are_rejected(self):
        (self.root / 'docs').mkdir()
        (self.root / 'docs' / 'link.md').symlink_to('../README.md')
        self.git('add', 'docs/link.md')
        with self.assertRaisesRegex(ValueError, 'regular file'):
            publication.audit(self.root)
        with self.assertRaises(ValueError):
            publication.audit_entry('personal.txt', b'private')

    def test_recognized_secrets_are_rejected(self):
        for data in [('ghp_' + 'a' * 30).encode(), ('-----BEGIN ' + 'PRIVATE KEY-----').encode()]:
            with self.assertRaises(ValueError):
                publication.audit_entry('docs/sample.md', data)

    def test_deleted_secret_in_outgoing_history_is_rejected(self):
        self.remote()
        (self.root / 'docs').mkdir()
        (self.root / 'docs' / 'secret.md').write_text('ghp_' + 'a' * 30)
        self.git('add', '.')
        self.git('commit', '-qm', 'Accidental secret fixture')
        self.git('rm', 'docs/secret.md')
        self.git('commit', '-qm', 'Delete fixture')
        publication.audit(self.root)
        with self.assertRaisesRegex(ValueError, 'API token'):
            release.publish(self.root, 'patch')
        self.assertEqual((self.root / 'VERSION').read_text(), '1.2.3\n')

    def test_dirty_source_cannot_publish(self):
        (self.root / 'README.md').write_text('unreviewed')
        with self.assertRaisesRegex(ValueError, 'clean'):
            release.publish(self.root, 'patch')

    def test_failed_tests_leave_version_commit_and_tags_unchanged(self):
        self.remote()
        before = self.git('rev-parse', 'HEAD')
        actual = release.run
        def fail_checks(*args, **kwargs):
            if args[0] == 'make':
                raise subprocess.CalledProcessError(1, args)
            return actual(*args, **kwargs)
        with patch.object(release, 'run', side_effect=fail_checks):
            with self.assertRaises(subprocess.CalledProcessError):
                release.publish(self.root, 'patch')
        self.assertEqual(self.git('rev-parse', 'HEAD'), before)
        self.assertEqual(self.git('tag', '--list'), '')
        self.assertEqual((self.root / 'VERSION').read_text(), '1.2.3\n')

    def test_verified_candidate_pushes_matching_version_and_tag(self):
        self.remote()
        actual = release.run
        checks = []
        def fake_build(*args, **kwargs):
            if args[0] == 'make':
                candidate = kwargs['cwd']
                self.assertEqual((candidate / 'VERSION').read_text(), '1.2.4\n')
                self.assertNotEqual(candidate, self.root)
                checks.append(args[1])
                return None
            return actual(*args, **kwargs)
        with patch.object(release, 'run', side_effect=fake_build):
            release.publish(self.root, 'patch')
        self.assertEqual(checks, ['check', 'release'])
        self.assertEqual(self.git('rev-parse', 'v1.2.4^{}'), self.git('rev-parse', 'origin/main'))
        self.assertEqual(self.git('show', 'v1.2.4:VERSION'), '1.2.4')

    def test_candidate_mutation_is_rejected(self):
        self.remote()
        actual = release.run
        def mutate(*args, **kwargs):
            if args[0] == 'make':
                (kwargs['cwd'] / 'README.md').write_text('modified while testing')
                return None
            return actual(*args, **kwargs)
        with patch.object(release, 'run', side_effect=mutate):
            with self.assertRaisesRegex(ValueError, 'candidate changed'):
                release.publish(self.root, 'patch')
        self.assertEqual(self.git('tag', '--list'), '')

    def binaries(self):
        dist = self.root / 'dist'
        dist.mkdir()
        for arch, machine in [('amd64', 62), ('arm64', 183)]:
            data = bytearray(64)
            data[:6] = b'\x7fELF\x02\x01'
            data[18:20] = machine.to_bytes(2, 'little')
            (dist / f'shellstudio-linux-{arch}').write_bytes(data)
        return dist

    def test_archive_is_reproducible_and_omits_untracked_data(self):
        dist = self.binaries()
        (self.root / 'docs').mkdir()
        (self.root / 'docs' / 'private.md').write_text('personal untracked data')
        package.package(self.root, '1.2.3', 123)
        archive = dist / 'shellstudio-1.2.3-source.tar.gz'
        before = archive.read_bytes()
        with tarfile.open(archive) as source:
            self.assertNotIn('docs/private.md', source.getnames())
            self.assertIn('LICENSE', source.getnames())
            self.assertIn('.source-files.json', source.getnames())
        package.package(self.root, '1.2.3', 123)
        self.assertEqual(archive.read_bytes(), before)
        for line in (dist / 'SHA256SUMS').read_text().splitlines():
            digest, name = line.split()
            self.assertEqual(hashlib.sha256((dist / name).read_bytes()).hexdigest(), digest)

    def test_empty_binary_and_version_mismatch_block_packaging(self):
        dist = self.binaries()
        with self.assertRaisesRegex(ValueError, 'match'):
            package.package(self.root, '1.2.4')
        (dist / 'shellstudio-linux-arm64').write_bytes(b'')
        with self.assertRaisesRegex(ValueError, 'ELF'):
            package.package(self.root, '1.2.3')

    def test_exported_source_manifest_can_be_audited_without_git(self):
        dist = self.binaries()
        package.package(self.root, '1.2.3')
        exported = self.base / 'exported'
        exported.mkdir()
        with tarfile.open(dist / 'shellstudio-1.2.3-source.tar.gz') as source:
            for member in source:
                self.assertTrue(member.isfile())
                self.assertFalse(Path(member.name).is_absolute())
                self.assertNotIn('..', Path(member.name).parts)
                target = exported / member.name
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(source.extractfile(member).read())
        self.assertEqual(publication.audit(exported), publication.audit(self.root))


if __name__ == '__main__':
    unittest.main(verbosity=2)
