#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
"""Exercise the actual POSIX installer with isolated homes and release fixtures."""
import hashlib
import fcntl
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
INSTALLER = ROOT / "scripts/install.sh"
BINARY = ROOT / "bin/shellstudio"
ASSET = "shellstudio-linux-" + {"x86_64": "amd64", "aarch64": "arm64"}[platform.machine()]


class Installer(unittest.TestCase):
    def setUp(self):
        (ROOT / ".work").mkdir(exist_ok=True)
        self.temp = tempfile.TemporaryDirectory(prefix="installer-", dir=ROOT / ".work")
        self.root = Path(self.temp.name)
        self.home = self.root / "home with spaces"
        self.home.mkdir()
        self.release = self.root / "release with spaces"
        self.release.mkdir()
        shutil.copyfile(BINARY, self.release / ASSET)
        self.digest = hashlib.sha256(BINARY.read_bytes()).hexdigest()
        (self.release / "SHA256SUMS").write_text(f"{self.digest}  {ASSET}\n")
        self.bin = self.home / ".local/bin"
        self.env = {**os.environ, "HOME": str(self.home), "XDG_CONFIG_HOME": str(self.home / ".config"),
                    "PATH": "/usr/local/bin:/usr/bin:/bin", "SHELLSTUDIO_NO_UPDATE_CHECK": "1"}
        self.env.pop("SHELLSTUDIO_BIN_DIR", None)
        self.version = subprocess.check_output([str(BINARY), "--version"], text=True).strip()

    def tearDown(self):
        self.temp.cleanup()

    def install(self, *args, offline=True, success=True):
        command = ["sh", str(INSTALLER), "--no-deps"]
        if offline:
            command += ["--from", str(self.release)]
        result = subprocess.run(command + list(args), env=self.env, cwd=self.root,
                                capture_output=True, text=True, timeout=30)
        if success:
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        else:
            self.assertNotEqual(result.returncode, 0, result.stdout)
        return result

    def assert_preserved(self):
        self.assertEqual((self.bin / "shellstudio").read_bytes(), b"old executable")
        self.assertEqual([p for p in self.bin.glob(".shellstudio-install.*") if p.is_dir()], [])

    def seed_old(self):
        self.bin.mkdir(parents=True)
        (self.bin / "shellstudio").write_bytes(b"old executable")

    def test_install_path_idempotence_and_command_anywhere(self):
        (self.home / ".bash_profile").write_text("# existing login configuration\n")
        self.install()
        self.install()
        self.assertEqual(hashlib.sha256((self.bin / "shellstudio").read_bytes()).hexdigest(), self.digest)
        for name in [".profile", ".bashrc", ".zshrc", ".bash_profile"]:
            self.assertEqual((self.home / name).read_text().count("# ShellStudio PATH"), 1)
        self.assertTrue((self.home / ".config/fish/conf.d/shellstudio.fish").is_file())
        result = subprocess.run(["sh", "-c", '. "$HOME/.profile"; shellstudio --version'],
                                env=self.env, cwd=self.release, capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), self.version)
        self.assertEqual((self.bin / "shellstudio").stat().st_mode & 0o777, 0o755)

    def test_bad_or_duplicate_checksum_preserves_install(self):
        self.seed_old()
        for checksums in [f"{'0'*64}  {ASSET}\n", f"{self.digest}  {ASSET}\n" * 2, ""]:
            with self.subTest(checksums=checksums[:12]):
                (self.release / "SHA256SUMS").write_text(checksums)
                self.install(success=False)
                self.assert_preserved()

    def test_version_mismatch_preserves_install(self):
        self.seed_old()
        result = self.install("--version", "v987.654.321", success=False)
        self.assertIn("does not match", result.stderr)
        self.assert_preserved()

    def test_symlink_destination_is_not_followed(self):
        self.bin.mkdir(parents=True)
        target = self.root / "other program"
        target.write_bytes(b"unrelated")
        (self.bin / "shellstudio").symlink_to(target)
        self.install(success=False)
        self.assertEqual(target.read_bytes(), b"unrelated")

    def test_custom_directory_no_profile_changes(self):
        custom = self.root / "custom bin"
        self.install("--bin-dir", str(custom), "--no-path")
        self.assertTrue((custom / "shellstudio").is_file())
        self.assertFalse((self.home / ".profile").exists())

    def test_concurrent_installer_is_rejected(self):
        self.seed_old()
        with (self.bin / ".shellstudio-install.lock").open("w") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            result = self.install(success=False)
            self.assertIn("another installer", result.stderr)
            self.assert_preserved()

    def mock_network(self, fail=False):
        fakebin = self.root / "mock bin"
        fakebin.mkdir()
        curl = fakebin / "curl"
        curl.write_text('''#!/usr/bin/python3
import os, pathlib, shutil, sys
args = sys.argv[1:]
url = next(x for x in args if x.startswith("https://"))
out = args[args.index("--output")+1]
assert "--proto" in args and "--proto-redir" in args
with open(os.environ["CURL_LOG"], "a") as f: f.write(url+"\\n")
if url.endswith("/latest"):
    print("https://github.com/TobiasCoding/ShellStudio/releases/tag/"+os.environ["FIXTURE_TAG"], end="")
elif os.environ.get("FAIL_DOWNLOAD") == "1" and "shellstudio-linux" in url:
    pathlib.Path(out).write_bytes(b"partial download")
    sys.exit(18)
else:
    shutil.copyfile(pathlib.Path(os.environ["FIXTURE_RELEASE"])/url.rsplit("/",1)[1], out)
''')
        curl.chmod(0o755)
        self.env.update(PATH=str(fakebin) + ":" + self.env["PATH"], CURL_LOG=str(self.root / "requests"),
                        FIXTURE_RELEASE=str(self.release), FIXTURE_TAG="v"+self.version.split()[1],
                        FAIL_DOWNLOAD="1" if fail else "0")

    def test_latest_resolves_once_and_pins_downloads(self):
        self.mock_network()
        self.install(offline=False)
        urls = (self.root / "requests").read_text().splitlines()
        self.assertEqual(len(urls), 3)
        self.assertTrue(urls[0].endswith("/latest"))
        self.assertTrue(all("/download/" + self.env["FIXTURE_TAG"] + "/" in u for u in urls[1:]))

    def test_interrupted_download_keeps_old_binary(self):
        self.seed_old()
        self.mock_network(fail=True)
        self.install(offline=False, success=False)
        self.assert_preserved()

    def test_command_name_directory_does_not_shadow_version(self):
        (self.root / "version").mkdir()
        result = subprocess.run([str(BINARY), "version"], cwd=self.root, env=self.env,
                                capture_output=True, text=True, timeout=5)
        self.assertEqual(result.stdout.strip(), self.version)


if __name__ == "__main__":
    unittest.main(verbosity=2)
