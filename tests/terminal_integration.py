#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
"""Real PTY/tmux regression tests. All state and sockets live below .work."""
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import signal
import sqlite3
import struct
import subprocess
import tempfile
import termios
import time
import unittest

ROOT = Path(__file__).resolve().parents[1]
BINARY = ROOT / "bin/shellstudio"
WORK = ROOT / ".work"


class Terminal:
    def __init__(self, env, *args, cwd=ROOT):
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.chdir(cwd)
            os.execve(str(BINARY), [str(BINARY), *args], env)
        self.output = b""
        self.resize(100, 30)
        self.pump(0.4)

    def resize(self, width, height):
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))

    def pump(self, seconds=0.15):
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            ready, _, _ = select.select([self.fd], [], [], min(0.05, max(0, end-time.monotonic())))
            if ready:
                try:
                    data = os.read(self.fd, 65536)
                except OSError:
                    break
                if not data:
                    break
                if b"\x1b[6n" in data:
                    os.write(self.fd, b"\x1b[1;1R")
                if b"\x1b]11;?" in data:
                    os.write(self.fd, b"\x1b]11;rgb:0000/0000/0000\x1b\\")
                self.output = (self.output + data)[-2_000_000:]
        return self.output

    def send(self, data, wait=0.15):
        os.write(self.fd, data)
        return self.pump(wait)

    def expect(self, text, timeout=10):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if text.encode() in self.output:
                return
            self.pump(0.1)
        raise AssertionError(f"terminal did not show {text!r}; tail={self.output[-2200:]!r}")

    def close(self, abrupt=False):
        try:
            os.kill(self.pid, signal.SIGKILL if abrupt else signal.SIGTERM)
            self.pump(0.3)
            deadline = time.monotonic() + 3
            while time.monotonic() < deadline:
                done, _ = os.waitpid(self.pid, os.WNOHANG)
                if done:
                    break
                time.sleep(0.05)
            else:
                os.kill(self.pid, signal.SIGKILL)
                os.waitpid(self.pid, 0)
        except (ProcessLookupError, ChildProcessError):
            pass
        os.close(self.fd)


class Integration(unittest.TestCase):
    def setUp(self):
        WORK.mkdir(exist_ok=True)
        self.temp = tempfile.TemporaryDirectory(prefix="it-", dir=WORK)
        self.root = Path(self.temp.name)
        self.env = {**os.environ, "TERM": "xterm-256color", "SHELL": "/bin/sh",
                    "SHELLSTUDIO_NO_UPDATE_CHECK": "1"}
        self.env.pop("TMUX", None)
        for k, sub in [("XDG_CONFIG_HOME", "c"), ("XDG_DATA_HOME", "d"),
                       ("XDG_STATE_HOME", "s"), ("XDG_RUNTIME_DIR", "r")]:
            self.env[k] = str(self.root / sub)
        self.terms = []

    def tearDown(self):
        for t in self.terms:
            t.close()
        for name in ["views", "programs"]:
            subprocess.run(["tmux", "-S", str(self.socket(name)), "kill-server"],
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
        self.temp.cleanup()

    def socket(self, name):
        return self.root / "r/shellstudio" / f"{name}.sock"

    def cli(self, *args):
        p = subprocess.run([str(BINARY), *args], env=self.env, cwd=ROOT,
                           capture_output=True, text=True, timeout=20)
        self.assertEqual(p.returncode, 0, p.stderr)
        return p.stdout

    def tmux(self, name, *args):
        p = subprocess.run(["tmux", "-S", str(self.socket(name)), *args],
                           env=self.env, capture_output=True, text=True, timeout=5)
        self.assertEqual(p.returncode, 0, p.stderr)
        return p.stdout.strip()

    def terminal(self, *args):
        if not args:
            args = ("--menu",)
        t = Terminal(self.env, *args)
        self.terms.append(t)
        return t

    def test_current_and_explicit_directory_workspace(self):
        self.assert_directory_workspace(self.root / "project with spaces")

    def test_directory_workspace_with_path_wider_than_terminal(self):
        # Keep sockets under the short test root while making the project path
        # wider than the 100-column PTY, regardless of the checkout location.
        folder = self.root / ("long-parent-" * 12) / "project with spaces"
        self.assertGreater(len(str(folder)), 100)
        self.assert_directory_workspace(folder)

    def assert_directory_workspace(self, folder):
        folder.mkdir(parents=True)
        for args, cwd in [((), folder), ((".",), folder), ((str(folder),), ROOT)]:
            with self.subTest(args=args, cwd=str(cwd)):
                terminal = Terminal(self.env, *args, cwd=cwd)
                self.terms.append(terminal)
                # Terminal rendering clips long titles. Check the screen using
                # its short view name, and the full path through persisted data.
                terminal.expect(f"{folder.name} · tiled · ")
                terminal.expect("t Terminal")
                views = json.loads(self.cli("views"))
                self.assertEqual(len(views), 1)
                self.assertEqual(views[0]["Folder"], str(folder))
                terminal.close()
                self.terms.remove(terminal)
        self.assertIn(self.cli("consoles").strip(), ("null", "[]"))

    def test_two_clients_resize_reconnect_and_process_survival(self):
        folder = self.root / "folder with spaces"
        folder.mkdir()
        view = json.loads(self.cli("new-view", "--folder", str(folder), "Main"))
        c1 = json.loads(self.cli("launch", "--view", view["ID"], "--name", "One"))
        c2 = json.loads(self.cli("launch", "--view", view["ID"], "--name", "Two"))
        time.sleep(0.3)
        original_pids = self.tmux("programs", "list-panes", "-a", "-F", "#{pane_pid}")
        cwd = self.tmux("programs", "display-message", "-p", "-t", "=c-"+c1["ID"]+":",
                        "#{pane_current_path}")
        self.assertEqual(cwd, str(folder))
        one = self.terminal()
        one.expect("ShellStudio")
        one.send(b"\r")
        one.expect("One")
        one.send(b"\r", 0.8)
        two = self.terminal()
        two.expect("ShellStudio")
        two.send(b"\r")
        two.send(b"\r", 0.8)
        sessions = self.tmux("views", "list-sessions", "-F", "#{session_name}").splitlines()
        self.assertEqual(len(sessions), 2)
        panes_before = self.tmux("views", "list-panes", "-a", "-F",
                                 "#{session_name}:#{pane_id}:#{pane_pid}")
        first = self.tmux("views", "list-panes", "-t", "="+sessions[0], "-F", "#{pane_id}").splitlines()
        second_active = self.tmux("views", "display-message", "-p", "-t", "="+sessions[1]+":", "#{pane_id}")
        self.tmux("views", "select-pane", "-t", first[-1])
        self.assertEqual(second_active, self.tmux("views", "display-message", "-p",
                                                 "-t", "="+sessions[1]+":", "#{pane_id}"))
        for width, height in [(80, 24), (150, 45), (60, 20), (100, 30)]:
            one.resize(width, height)
            one.pump(0.15)
        self.assertEqual(panes_before, self.tmux("views", "list-panes", "-a", "-F",
                                                "#{session_name}:#{pane_id}:#{pane_pid}"))
        one.send(b"\x1b[21~", 0.5)  # F10 detaches, returning to Bubble Tea.
        one.send(b"\r", 0.5)       # Reattach without rebuilding panes.
        self.assertEqual(panes_before, self.tmux("views", "list-panes", "-a", "-F",
                                                "#{session_name}:#{pane_id}:#{pane_pid}"))
        self.assertEqual(original_pids, self.tmux("programs", "list-panes", "-a", "-F", "#{pane_pid}"))
        # A replaced presentation pane does not replace the underlying program.
        self.tmux("views", "kill-pane", "-t", first[-1])
        one.send(b"\x1b[21~", 0.4)
        one.send(b"\r", 0.6)
        self.assertEqual(original_pids, self.tmux("programs", "list-panes", "-a", "-F", "#{pane_pid}"))
        one.close(abrupt=True)
        self.terms.remove(one)
        self.assertEqual(original_pids, self.tmux("programs", "list-panes", "-a", "-F", "#{pane_pid}"))
        self.cli("stop", c1["ID"])
        consoles = json.loads(self.cli("consoles"))
        self.assertEqual(len(consoles), 2)
        self.cli("restart", c1["ID"])
        self.assertIn("c-"+c1["ID"], self.tmux("programs", "list-sessions", "-F", "#{session_name}"))
        # Simulate loss of volatile process state at reboot, without rebooting the host.
        self.tmux("programs", "kill-server")
        self.cli("doctor")
        self.assertEqual(len(json.loads(self.cli("consoles"))), 2)
        self.assertEqual(len(json.loads(self.cli("views"))), 1)
        probe = subprocess.run(["tmux", "-S", str(self.socket("programs")), "list-sessions"],
                               capture_output=True, timeout=5)
        self.assertNotEqual(probe.returncode, 0, "opening metadata automatically restarted programs")

    def test_notes_continuous_autosave_and_abrupt_recovery(self):
        t = self.terminal("notes")
        t.expect("NOTES")
        t.send(b"n")
        t.send(b"\x15Recovery note\r")
        t.expect("Autosave")
        # Dispatch deadlines are checked deterministically in the Go model tests.
        # This checks durable progress while typing, allowing real fsync latency.
        for _ in range(80):
            t.send(b"x", 0.05)
        db = self.root / "d/shellstudio/shellstudio.db"
        with sqlite3.connect(db) as cx:
            body, revision = cx.execute("select body,revision from notes").fetchone()
        self.assertGreater(len(body), 0, "continuous typing never made durable progress")
        self.assertGreater(revision, 1)
        t.output = b""
        t.send(b" committed text", 0.1)
        t.expect("Saved")
        t.close(abrupt=True)
        self.terms.remove(t)
        self.cli("doctor")
        with sqlite3.connect(db) as cx:
            body, = cx.execute("select body from notes").fetchone()
            self.assertIn("committed text", body)
            self.assertEqual(cx.execute("pragma integrity_check").fetchone()[0], "ok")
        t = self.terminal("notes")
        t.expect("Recovery note")
        t.send(b"\r")
        t.expect("committed text")

    def test_no_agents_and_isolated_second_user_paths(self):
        self.cli("doctor")
        config = self.root / "c/shellstudio/extensions"
        config.mkdir()
        (config / "broken.json").write_text('{"schema_version":99}')
        t = self.terminal()
        t.expect("ShellStudio")
        t.send(b"\t")
        t.send(b"\t")
        t.expect("Invalid")
        view = json.loads(self.cli("new-view", "Safe"))
        self.cli("launch", "--view", view["ID"])
        other = {**self.env, "XDG_DATA_HOME": str(self.root / "other-data"),
                 "XDG_RUNTIME_DIR": str(self.root / "other-run")}
        p = subprocess.run([str(BINARY), "consoles"], env=other, capture_output=True, text=True)
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertIn(p.stdout.strip(), ("null", "[]"))
        for path in [self.root/"c/shellstudio", self.root/"d/shellstudio",
                     self.root/"r/shellstudio"]:
            self.assertEqual(path.stat().st_mode & 0o777, 0o700)


if __name__ == "__main__":
    unittest.main(verbosity=2)
