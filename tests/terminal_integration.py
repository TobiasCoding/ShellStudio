#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
"""Real PTY/tmux regression tests. All state and sockets live below .work."""
import base64
import fcntl
import json
import os
from pathlib import Path
import pty
import re
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


# Rows of the views server status line (the top bar) above every window.
TOP_BAR_ROWS = 1


class Terminal:
    def __init__(self, env, *args, cwd=ROOT):
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.chdir(cwd)
            os.execve(str(BINARY), [str(BINARY), *args], env)
        self.output = b""
        self.capture = b""
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
                # Answer the probes tmux sends, as a real terminal would.
                if b"\x1b[c" in data or b"\x1b[0c" in data:
                    os.write(self.fd, b"\x1b[?61;4;6;7;14;21;22;23;24;28;32;42;52c")
                if b"\x1b[>c" in data or b"\x1b[>0c" in data:
                    os.write(self.fd, b"\x1b[>0;10;1c")
                if b"\x1b]10;?" in data:
                    os.write(self.fd, b"\x1b]10;rgb:cccc/cccc/cccc\x1b\\")
                self.output = (self.output + data)[-2_000_000:]
                self.capture = (self.capture + data)[-2_000_000:]
        return self.output

    def send(self, data, wait=0.15):
        os.write(self.fd, data)
        return self.pump(wait)

    def expect(self, text, timeout=20):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if text.encode() in self.output:
                return
            self.pump(0.1)
        raise AssertionError(f"terminal did not show {text!r}; tail={self.output[-2200:]!r}")

    def mouse(self, button, x, y, release=False, wait=0.15):
        """x and y are window coordinates: the top bar takes the first row."""
        return self.send(f"\x1b[<{button};{x+1};{y+1+TOP_BAR_ROWS}{'m' if release else 'M'}".encode(), wait)

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
        # English UI regardless of the developer's locale: scenarios match
        # English text. Spanish has its own scenario.
        self.env = {**os.environ, "LANG": "C", "LC_ALL": "C", "LC_MESSAGES": "C", "TERM": "xterm-256color", "SHELL": "/bin/sh",
                    "SHELLSTUDIO_NO_UPDATE_CHECK": "1"}
        self.env.pop("TMUX", None)
        self.env.pop("TMUX_PANE", None)
        self.env.pop("SHELLSTUDIO_CONSOLE", None)
        for k, sub in [("XDG_CONFIG_HOME", "c"), ("XDG_DATA_HOME", "d"),
                       ("XDG_STATE_HOME", "s"), ("XDG_RUNTIME_DIR", "r")]:
            self.env[k] = str(self.root / sub)
        self.terms = []

    def tearDown(self):
        result = self._outcome.result
        if any(test is self for test, _ in result.failures + result.errors):
            # Preserve the state before teardown stops the synthetic sessions.
            try:
                report = subprocess.run([str(BINARY), "report", "--stdout"], env=self.env,
                                        capture_output=True, text=True, timeout=20)
                if report.returncode == 0:
                    (WORK / (self._testMethodName + "-report.json")).write_text(report.stdout)
            except (OSError, subprocess.TimeoutExpired):
                pass  # Diagnostics must not prevent cleanup of test sessions.
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
        p = subprocess.run(["tmux", "-u", "-S", str(self.socket(name)), *args],
                           env=self.env, capture_output=True, text=True, timeout=5)
        self.assertEqual(p.returncode, 0, p.stderr)
        return p.stdout.strip()

    def terminal(self, *args):
        if not args:
            args = ("--menu",)
        t = Terminal(self.env, *args)
        self.terms.append(t)
        return t

    def wait_for(self, predicate, timeout=10):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            for terminal in self.terms:
                terminal.pump(0.05)
            if predicate():
                return
        self.fail("condition did not become true")

    def panes(self):
        rows = self.tmux("views", "list-panes", "-a", "-F",
                         "#{pane_id}|#{@ss_kind}|#{@ss_console}|#{pane_left}|#{pane_top}|#{pane_width}|#{pane_height}")
        return [dict(zip(("id", "kind", "console", "x", "y", "w", "h"), row.split("|")))
                for row in rows.splitlines()]

    def wait_dialog_closed(self):
        def closed():
            for entry in Path("/proc").glob("[0-9]*/cmdline"):
                try:
                    argv = entry.read_bytes().split(b"\0")
                    if len(argv) > 2 and argv[0] == str(BINARY).encode() and argv[1] in (b"_view", b"_dialog"):
                        env = (entry.parent / "environ").read_bytes().split(b"\0")
                        if ("XDG_DATA_HOME="+self.env["XDG_DATA_HOME"]).encode() in env:
                            return False
                except OSError:
                    pass
            return True
        self.wait_for(closed)
        for terminal in self.terms:
            terminal.pump(0.2)

    def assert_explorer(self):
        tree = [p for p in self.panes() if p["kind"] == "explorer"]
        self.assertEqual(len(tree), 1)
        self.assertEqual((tree[0]["x"], tree[0]["y"]), ("0", "0"))
        self.assertGreaterEqual(int(tree[0]["w"]), 17, "explorer collapsed to an unreadable sliver")
        # The Minimized panel splits the explorer after it first draws: wait
        # for the redraw at its final height.
        self.wait_for(lambda: "FILES" in self.tmux("views", "capture-pane", "-p", "-t", tree[0]["id"]))
        return tree[0]

    def test_current_and_explicit_directory_workspace(self):
        self.assert_directory_workspace(self.root / "project with spaces")

    def test_cli_startup_does_not_probe_terminal(self):
        t = self.terminal("version")
        t.expect("ShellStudio")
        self.assertEqual(t.output, self.cli("version").strip().encode()+b"\r\n")
        (WORK / "ui-startup.pty").write_bytes(t.capture)

    def test_clipboard_uses_mosh_compatible_selector_through_nested_tmux(self):
        terminal = self.terminal(str(self.root))
        terminal.expect("FILES")
        view = json.loads(self.cli("views"))[0]["ID"]
        self.cli("launch", "--view", view, "--name", "clipboard")
        self.wait_for(lambda: bool(self.tmux("programs", "list-clients")))

        # Exercise both the outer copy path and the nested program copy path.
        # Mosh 1.4 accepts OSC 52;c; but drops tmux's default OSC 52;;.
        for server in ("views", "programs"):
            with self.subTest(server=server):
                text = "clipboard from " + server + " — café\nsecond line"
                payload = base64.b64encode(text.encode())
                terminal.output = b""
                self.tmux(server, "set-buffer", "-w", text)
                self.wait_for(lambda: payload in terminal.output)
                sequences = re.findall(rb"\x1b\]52;[^\x07]*\x07", terminal.output)
                self.assertIn(b"\x1b]52;c;" + payload + b"\x07", sequences)
                self.assertFalse(any(seq.startswith(b"\x1b]52;;") for seq in sequences))

        # An application writing OSC 52 must also survive both tmux servers.
        payload = base64.b64encode(b"clipboard from application")
        terminal.output = b""
        self.tmux("programs", "send-keys", "-l",
                  "printf '\\033]52;c;" + payload.decode() + "\\007'")
        self.tmux("programs", "send-keys", "Enter")
        self.wait_for(lambda: b"\x1b]52;" in terminal.output)
        self.assertIn(b"\x1b]52;c;" + payload + b"\x07",
                      re.findall(rb"\x1b\]52;[^\x07]*\x07", terminal.output))

    def test_diagnostic_report_preserves_running_workspace_and_privacy(self):
        secret = "PRIVATE-DIAGNOSTIC-FIXTURE"
        folder = self.root / secret
        folder.mkdir()
        terminal = self.terminal(str(folder))
        terminal.expect("FILES")
        view = json.loads(self.cli("views"))[0]["ID"]
        console = json.loads(self.cli("launch", "--view", view, "--name", secret))
        before = self.tmux("programs", "list-panes", "-a", "-F", "#{pane_pid}")
        terminal.send(("printf '" + secret + "\\n'\n").encode())
        report_path = self.root / "report.json"
        self.cli("report", str(report_path))
        raw = report_path.read_text()
        report = json.loads(raw)
        self.assertNotIn(secret, raw)
        self.assertNotIn(str(self.root), raw)
        self.assertEqual(report["sections"]["database"]["status"], "ok")
        self.assertTrue(report["sections"]["views_panes"]["rows"])
        self.assertTrue(report["sections"]["programs_panes"]["rows"])
        self.assertTrue(any(entry["event"] == "view-sync" for entry in report["logs"]))
        self.assertEqual(report_path.stat().st_mode & 0o777, 0o600)
        self.assertEqual(before, self.tmux("programs", "list-panes", "-a", "-F", "#{pane_pid}"))
        self.assertEqual(console["ID"], report["sections"]["consoles"]["rows"][0]["id"])

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
                terminal.expect("This view is empty")
                terminal.expect("FILES")
                self.assert_explorer()
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
        one.expect("One")
        two = self.terminal()
        two.expect("Two")
        sessions = self.tmux("views", "list-sessions", "-F", "#{session_name}").splitlines()
        self.assertEqual(len(sessions), 1)
        def console_processes():
            rows = self.tmux("views", "list-panes", "-a", "-F",
                             "#{@ss_kind}:#{session_name}:#{pane_id}:#{pane_pid}")
            return [row for row in rows.splitlines() if row.startswith("console:")]
        panes_before = console_processes()
        first = self.tmux("views", "list-panes", "-t", "="+sessions[0], "-F", "#{pane_id}").splitlines()
        self.tmux("views", "select-pane", "-t", first[-1])
        self.assertEqual(first[-1], self.tmux("views", "display-message", "-p",
                                             "-t", "="+sessions[0]+":", "#{pane_id}"))
        for width, height in [(80, 24), (150, 45), (60, 20), (100, 30)]:
            one.resize(width, height)
            one.pump(0.4)
            self.wait_for(lambda: any(p["kind"] == "explorer" for p in self.panes()) == (width >= 70))
        self.assert_explorer()
        self.assertEqual(panes_before, console_processes())
        one.send(b"\x1b[21~", 0.5)  # F10 leaves directly; programs survive.
        one.close()
        self.terms.remove(one)
        one = self.terminal()
        one.expect("One")
        self.assertEqual(panes_before, console_processes())
        self.assertEqual(original_pids, self.tmux("programs", "list-panes", "-a", "-F", "#{pane_pid}"))
        # A previously saved narrow sidebar must recover when attaching.
        tree = self.assert_explorer()
        self.tmux("views", "resize-pane", "-t", tree["id"], "-x", "2")
        repair = self.terminal()
        repair.expect("One")
        self.wait_for(lambda: int(next(p for p in self.panes() if p["kind"] == "explorer")["w"]) >= 20)
        self.assert_explorer()
        repair.close()
        self.terms.remove(repair)
        # A replaced presentation pane does not replace the underlying program.
        self.tmux("views", "kill-pane", "-t", first[-1])
        one.close()
        self.terms.remove(one)
        one = self.terminal()
        one.expect("Two")
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

    def test_reconcile_removes_duplicate_panels_without_restarting_programs(self):
        view = json.loads(self.cli("new-view", "Main"))
        consoles = [json.loads(self.cli("launch", "--view", view["ID"], "--name", name))
                    for name in ("One", "Two", "Three")]
        t = self.terminal()
        t.resize(209, 51)
        t.expect("Three")
        self.wait_for(lambda: self.tmux("views", "display-message", "-p", "#{window_width}") == "209")
        session = "=v-" + view["ID"] + ":"
        original = {p["console"]: p["id"] for p in self.panes() if p["kind"] == "console"}
        pids = self.tmux("programs", "list-panes", "-a", "-F", "#{pane_id}:#{pane_pid}")
        # Reproduce the screenshot: several clients for each of three programs
        # in one view. Metadata remains unique; only presentation is damaged.
        duplicates = []
        for console in consoles * 2:
            source = original[console["ID"]]
            command = self.tmux("views", "display-message", "-p", "-t", source, "#{pane_start_command}")
            self.tmux("views", "select-layout", "-t", session, "tiled")
            duplicate = self.tmux("views", "split-window", "-d", "-P", "-F", "#{pane_id}",
                                  "-t", session, command)
            for option in ("@ss_kind", "@ss_console", "@ss_name", "@ss_state"):
                value = self.tmux("views", "show-option", "-pqv", "-t", source, option)
                self.tmux("views", "set-option", "-p", "-t", duplicate, option, value)
            duplicates.append(duplicate)
        self.tmux("views", "select-pane", "-t", duplicates[-1])
        self.cli("_view", "layout", "--pane", duplicates[-1], "--layout", "main-horizontal")
        self.cli("_sync", view["ID"])
        panels = [p for p in self.panes() if p["kind"] == "console"]
        self.assertCountEqual([p["console"] for p in panels], [c["ID"] for c in consoles])
        self.assertEqual(duplicates[-1], self.tmux("views", "display-message", "-p", "-t", session, "#{pane_id}"))
        self.assert_explorer()
        # The active program must occupy its pane, not a narrow strip with dots.
        focused = next(p for p in panels if p["id"] == duplicates[-1])
        self.wait_for(lambda: self.tmux("programs", "display-message", "-p", "-t",
                                       "=c-" + focused["console"] + ":", "#{pane_width}") == focused["w"])
        for _ in range(3):
            self.cli("_sync", view["ID"])
        self.assertEqual(panels, [p for p in self.panes() if p["kind"] == "console"])
        self.assertEqual(pids, self.tmux("programs", "list-panes", "-a", "-F", "#{pane_id}:#{pane_pid}"))

    def test_utf8_headers_with_ssh_c_locale(self):
        self.env.update(LANG="C", LC_ALL="C")
        view = json.loads(self.cli("new-view", "Main"))
        self.cli("launch", "--view", view["ID"], "--name", "café")
        t = self.terminal()
        t.expect("café")
        pids = self.tmux("programs", "list-panes", "-a", "-F", "#{pane_pid}")
        for _ in range(3):
            self.cli("_sync", view["ID"])
        self.assertEqual(len([p for p in self.panes() if p["kind"] == "console"]), 1)
        self.assertEqual(pids, self.tmux("programs", "list-panes", "-a", "-F", "#{pane_pid}"))
        report = json.loads(self.cli("report", "--stdout"))
        for section in ("views_panes", "programs_panes", "views_clients", "programs_clients"):
            self.assertEqual(report["sections"][section]["status"], "ok", section)
        for server in ("views", "programs"):
            flags = self.tmux(server, "list-clients", "-F", "#{client_flags}").splitlines()
            self.assertTrue(flags)
            self.assertTrue(all("UTF-8" in f for f in flags), (server, flags))
        t.expect("▼")

    def test_stale_pane_death_does_not_disconnect_live_console(self):
        view = json.loads(self.cli("new-view", "Main"))
        console = json.loads(self.cli("launch", "--view", view["ID"], "--name", "One"))
        t = self.terminal()
        t.expect("One")
        pane = next(p["id"] for p in self.panes() if p["kind"] == "console")
        pid = self.tmux("views", "display-message", "-p", "-t", pane, "#{pane_pid}")
        # A queued pane-died hook may run after respawn has already replaced
        # the old process. It must not kill the new, healthy nested client.
        self.tmux("views", "set-hook", "-R", "-t", pane, "pane-died")
        t.pump(0.5)
        self.assertEqual(pid, self.tmux("views", "display-message", "-p", "-t", pane, "#{pane_pid}"))
        self.assertNotEqual("1", self.tmux("views", "show-option", "-pqv", "-t", pane, "@ss_disconnected"))
        # Explicit stop must reconcile even if the asynchronous notification
        # is missed; only unexpected external exits depend on that hook.
        self.tmux("views", "set-hook", "-gu", "pane-died")
        self.cli("stop", console["ID"])
        t.expect("console is stopped")

    def test_switch_to_existing_view_fits_current_terminal(self):
        first = json.loads(self.cli("new-view", "Alpha"))
        self.cli("launch", "--view", first["ID"], "--name", "One")
        second = json.loads(self.cli("new-view", "Beta"))
        self.cli("launch", "--view", second["ID"], "--name", "Two")
        t = self.terminal()
        t.resize(140, 42)
        t.expect("One")
        self.wait_for(lambda: self.tmux("views", "display-message", "-p", "#{window_width}") == "140")
        client = self.tmux("views", "list-clients", "-F", "#{client_name}")
        session = "=v-" + second["ID"] + ":"
        # A view saved on another terminal must resize on switching, even
        # though SSH has not sent another SIGWINCH to the current client.
        self.cli("_sync", second["ID"])
        self.tmux("views", "resize-window", "-t", session, "-x", "80", "-y", "24")
        self.cli("_view", "previous", "--client", client)
        self.wait_for(lambda: self.tmux("views", "display-message", "-p", "-t", session,
                                       "#{window_width}x#{window_height}") == f"140x{42-TOP_BAR_ROWS}")

    def test_notes_continuous_autosave_and_abrupt_recovery(self):
        t = self.terminal("notes")
        t.expect("NOTES")
        t.send(b"n")
        t.send(b"\x15Recovery note\r")
        t.expect("Autosave")
        # Dispatch deadlines are checked deterministically in the Go model tests.
        # This checks durable progress while typing, allowing real fsync latency.
        db = self.root / "d/shellstudio/shellstudio.db"
        body, revision = "", 0
        deadline = time.monotonic() + 10
        # Keep typing until a committed revision is visible. A fixed four-second
        # sleep races with fsync on busy disks during isolated release builds.
        while time.monotonic() < deadline and not body:
            for _ in range(10):
                t.send(b"x", 0.05)
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

    def test_sesiones_dialogs_mouse_and_console_lifecycle(self):
        folder = self.root / "project"
        folder.mkdir()
        (folder / "example.txt").write_text("example\n")
        view = json.loads(self.cli("new-view", "--folder", str(folder), "Main"))
        # Never start installed agents in a UI regression test.
        (self.root / "c/shellstudio/last-kinds").write_text("terminal\n")
        t = self.terminal()
        t.resize(160, 45)
        t.expect("This view is empty")
        t.pump(0.5)
        for name in ("first", "second"):
            t.output = b""
            if name == "first":
                self.assert_explorer()
            t.send(b"\x1bOR", 0.4)  # F3
            t.expect("NEW CONSOLE IN Main")
            t.send(b"\r")
            t.expect("Name of terminal")
            t.send(name.encode() + b"\r")
            self.wait_for(lambda: any(c["Name"] == "terminal-"+name
                                     for c in json.loads(self.cli("consoles")) or []))
            t.expect("□")  # console headers are drawn
            # Creation records metadata before reconciling the panes. Wait for
            # the dialog process to exit before sending another shortcut.
            self.wait_dialog_closed()
            self.assert_explorer()
        consoles = json.loads(self.cli("consoles"))
        ids = {c["Name"]: c["ID"] for c in consoles}
        pids = self.tmux("programs", "list-panes", "-a", "-F", "#{pane_pid}")
        # Dialogs are modal: a click outside the box closes them, and the
        # release of that click reaches nothing behind.
        t.output = b""
        t.send(b"\x1bOR", 0.4)  # F3
        t.expect("NEW CONSOLE IN Main")
        t.mouse(0, 0, 3)
        t.mouse(0, 0, 3, release=True, wait=0.4)
        self.wait_dialog_closed()
        self.assertEqual(len(json.loads(self.cli("consoles"))), 2)
        for key, title in ((b"\x1bOP", "VIEW Main"), (b"\x1b[15~", "ARRANGE Main"),
                           (b"\x1b[18~", "SAVED VIEWS"), (b"\x1b[20~", "KILL CONSOLE")):
            t.output = b""
            t.send(key, 0.4)
            t.expect(title)
            t.send(b"\x1b", 0.4)
            self.wait_dialog_closed()
        # Header menus open with either button; cancellation leaves programs intact.
        console = next(p for p in self.panes() if p["kind"] == "console")
        x, y = int(console["x"])+5, int(console["y"])
        for button in (0, 2):
            t.output = b""
            t.mouse(button, x, y)
            t.mouse(button, x, y, release=True)
            t.expect("Kill and replace here")
            t.send(b"\x1b", 0.5)
        before = [p["console"] for p in self.panes() if p["kind"] == "console"]
        src, dest = [p for p in self.panes() if p["kind"] == "console"]
        t.mouse(0, int(src["x"])+5, int(src["y"]))
        t.mouse(32, int(src["x"])+8, int(src["y"]))
        t.mouse(32, int(dest["x"])+5, int(dest["y"])+2)
        t.mouse(0, int(dest["x"])+5, int(dest["y"])+2, release=True, wait=0.8)
        self.wait_for(lambda: [p["console"] for p in self.panes() if p["kind"] == "console"] == before[::-1])
        self.assert_explorer()
        self.assertEqual(pids, self.tmux("programs", "list-panes", "-a", "-F", "#{pane_pid}"))
        # Reconciliation keeps the drag order, including after reconnecting.
        self.cli("_sync", view["ID"])
        self.assertEqual([p["console"] for p in self.panes() if p["kind"] == "console"], before[::-1])
        # Dragging a tree file inserts its path, without executing it.
        tree = self.assert_explorer()
        dest = next(p for p in self.panes() if p["console"] == ids["terminal-first"])
        t.mouse(0, 8, 3)
        t.mouse(32, 10, 3)
        t.mouse(32, int(dest["x"])+5, int(dest["y"])+3)
        t.mouse(0, int(dest["x"])+5, int(dest["y"])+3, release=True, wait=0.6)
        captured = self.tmux("programs", "capture-pane", "-p", "-t", "=c-"+ids["terminal-first"]+":")
        self.assertIn("example.txt", captured)
        t.send(b"\x15")  # Clear the inserted path.
        # Unsolicited pointer motion must not become shell input.
        dest = next(p for p in self.panes() if p["console"] == ids["terminal-first"])
        self.tmux("views", "select-pane", "-t", dest["id"])
        for x in range(int(dest["x"])+2, int(dest["x"])+12):
            t.mouse(35, x, int(dest["y"])+3, wait=0.02)
        t.send(b"printf 'CLEAN_%s\\n' INPUT\r", 0.5)
        captured = self.tmux("programs", "capture-pane", "-p", "-t", "=c-"+ids["terminal-first"]+":")
        self.assertIn("CLEAN_INPUT", captured)
        self.assertNotRegex(captured, r"\?61;|>0;10;|rgb:|<35;|\^\[")
        # Finished program: Enter opens the native in-pane restart menu.
        t.output = b""
        t.send(b"exit\r", 0.5)
        self.wait_for(lambda: self.tmux("programs", "display-message", "-p", "-t",
                                       "=c-"+ids["terminal-first"]+":", "#{pane_dead}") == "1")
        t.send(b"\r")
        t.expect("Restart")
        t.send(b"r", 0.5)
        self.wait_for(lambda: self.tmux("programs", "display-message", "-p", "-t",
                                       "=c-"+ids["terminal-first"]+":", "#{pane_dead}") == "0")
        # Loss of the program session gets a reconnect/restart screen.
        t.output = b""
        self.cli("stop", ids["terminal-first"])
        t.expect("console is stopped")
        t.send(b"\r", 0.5)
        self.wait_for(lambda: "c-"+ids["terminal-first"] in self.tmux("programs", "list-sessions", "-F", "#{session_name}"))
        self.wait_for(lambda: self.tmux("views", "show-option", "-pqv", "-t", dest["id"], "@ss_disconnected") != "1")
        self.assert_explorer()
        # Follow a dialog from F1 and return with Escape, then toggle the tree.
        t.pump(0.5)
        t.output = b""
        t.send(b"\x1bOP", 0.4)
        t.expect("VIEW Main")
        t.send(b"s", 0.5)
        t.expect("CONSOLES OF Main")
        t.output = b""
        t.send(b"\x1b", 0.5)
        t.expect("VIEW Main")
        t.send(b"e", 0.5)
        self.wait_for(lambda: all(p["kind"] != "explorer" for p in self.panes()))
        self.wait_dialog_closed()
        t.output = b""
        t.send(b"\x1bOP", 0.5)
        t.expect("VIEW Main")
        t.send(b"e", 0.5)
        self.wait_for(lambda: any(p["kind"] == "explorer" for p in self.panes()))
        self.wait_dialog_closed()
        self.assert_explorer()
        # A compact F1 scrolls through stacked categories to the last action.
        t.resize(70, 24)
        t.pump(0.6)
        t.output = b""
        t.send(b"\x1bOP", 0.5)
        for _ in range(24):
            t.send(b"\x1b[B", 0.03)
        t.expect("Back (Esc)")
        t.send(b"\x1b", 0.4)
        self.wait_dialog_closed()
        t.resize(160, 45)
        t.pump(0.8)
        t.output = b""
        t.send(b"\x1b[15~", 0.4)
        t.expect("ARRANGE Main")
        t.send(b"1", 0.5)
        self.wait_dialog_closed()
        self.assertEqual(json.loads(self.cli("views"))[0]["Layout"], "even-horizontal")
        tree = self.assert_explorer()
        border = int(tree["w"])
        t.mouse(0, border, 10)
        t.mouse(32, border+1, 10)
        t.mouse(32, border+8, 10)
        self.assertEqual(int(self.assert_explorer()["w"]), border+8)
        t.mouse(0, border+8, 10, release=True, wait=0.5)
        self.wait_for(lambda: int(self.assert_explorer()["w"]) != border)
        selected = next(p for p in self.panes() if p["console"] == ids["terminal-second"])
        self.tmux("views", "select-pane", "-t", selected["id"])
        t.output = b""
        t.send(b"\x1b[20~", 0.4)
        t.expect("KILL CONSOLE")
        t.send(b"\r", 0.5)
        self.wait_dialog_closed()
        self.assertEqual([c["ID"] for c in json.loads(self.cli("consoles"))], [ids["terminal-first"]])
        (WORK / "ui-restoration.pty").write_bytes(t.capture)

    def test_resize_every_divider_preserves_console_identity_and_input(self):
        view = json.loads(self.cli("new-view", "Resize"))
        consoles = [json.loads(self.cli("launch", "--view", view["ID"])) for _ in range(4)]
        t = self.terminal()
        t.resize(160, 48)
        t.expect("□")  # console headers are drawn
        t.pump(0.8)
        pids = self.tmux("programs", "list-panes", "-a", "-F", "#{pane_pid}")
        client = self.tmux("views", "list-clients", "-F", "#{client_name}")
        for layout in ("even-horizontal", "even-vertical", "tiled", "main-vertical", "main-horizontal"):
            self.cli("_view", "layout", "--client", client, "--layout", layout)
            t.pump(0.4)
            original = self.panes()
            order = [p["console"] for p in original]
            # Exercise every right/bottom edge, including lower rows and
            # junctions with another divider. Both directions must work.
            edges = [(p["id"], axis) for p in original for axis in ("x", "y")
                     if (axis == "x" and int(p["x"])+int(p["w"]) < 159)
                     or (axis == "y" and int(p["y"])+int(p["h"]) < 47)]
            for pane, axis in edges:
                for delta in (3, -3):
                    with self.subTest(layout=layout, pane=pane, axis=axis, delta=delta):
                        src = next(p for p in self.panes() if p["id"] == pane)
                        x = int(src["x"])+(int(src["w"]) if axis == "x" else int(src["w"])//2)
                        y = int(src["y"])+(int(src["h"]) if axis == "y" else int(src["h"])//2)
                        dx, dy = (delta, 0) if axis == "x" else (0, delta)
                        t.mouse(0, x, y)
                        t.mouse(32, x+dx//abs(delta), y+dy//abs(delta))
                        t.mouse(32, x+dx, y+dy)
                        t.mouse(0, x+dx, y+dy, release=True, wait=0.3)
                        dst = next(p for p in self.panes() if p["id"] == pane)
                        size = "w" if axis == "x" else "h"
                        self.assertEqual(int(dst[size]), int(src[size])+delta)
                        self.assertEqual([p["console"] for p in self.panes()], order)
        self.assertEqual(pids, self.tmux("programs", "list-panes", "-a", "-F", "#{pane_pid}"))
        for c in consoles:
            pane = next(p for p in self.panes() if p["console"] == c["ID"])
            self.tmux("views", "select-pane", "-t", pane["id"])
            t.send(b"printf 'RESIZE_%s\\n' CLEAN\r", 0.2)
            captured = self.tmux("programs", "capture-pane", "-p", "-t", "=c-"+c["ID"]+":")
            self.assertIn("RESIZE_CLEAN", captured)
            self.assertNotRegex(captured, r"<32;|<0;|\^\[")

    def test_vertical_drag_near_divider_junction_keeps_width(self):
        view = json.loads(self.cli("new-view", "Junctions"))
        for _ in range(4):
            self.cli("launch", "--view", view["ID"])
        t = self.terminal()
        t.resize(160, 48)
        t.expect("□")  # console headers are drawn
        t.pump(0.8)
        src = next(p for p in self.panes() if p["kind"] == "console")
        before = self.panes()
        # A horizontal divider, one cell away from its junction with a
        # vertical divider: proximity at release must not choose the axis.
        x, y = int(src["x"])+int(src["w"])-1, int(src["y"])+int(src["h"])
        t.mouse(0, x, y)
        t.mouse(32, x, y+1)
        t.mouse(32, x, y+4)
        t.mouse(0, x, y+4, release=True, wait=0.4)
        after = self.panes()
        dst = next(p for p in after if p["id"] == src["id"])
        self.assertEqual(int(dst["h"]), int(src["h"])+4)
        self.assertEqual([p["w"] for p in after], [p["w"] for p in before])
        self.assertEqual([p["console"] for p in after], [p["console"] for p in before])

    def test_terminal_reports_never_move_consoles_or_type_digits(self):
        view = json.loads(self.cli("new-view", "Reports"))
        consoles = [json.loads(self.cli("launch", "--view", view["ID"])) for _ in range(3)]
        t = self.terminal()
        t.resize(160, 48)
        t.expect("□")  # console headers are drawn
        t.pump(0.8)
        # Replies to these probes come back through SSH/ConPTY and leaked into
        # the focused console; the user's terminal must not be asked at all.
        for probe in (b"\x1b[c", b"\x1b[>c", b"\x1b[>q", b"\x1b]10;?", b"\x1b]11;?"):
            self.assertNotIn(probe, t.capture)
        order = [p["console"] for p in self.panes()]
        # Mouse motion arriving in two SSH packets, and an unsolicited report.
        for i in range(6):
            event = f"\x1b[<35;{60+i};20M".encode()
            os.write(t.fd, event[:6])
            t.pump(0.05)
            os.write(t.fd, event[6:])
            t.pump(0.05)
        t.send(b"\x1b[54;64R", 1.0)
        self.assertEqual([p["console"] for p in self.panes()], order)
        t.send(b"\x1b<", 0.3)  # Alt+<: the focused console moves before its neighbor.
        self.wait_for(lambda: [p["console"] for p in self.panes()] != order)
        moved = [p["console"] for p in self.panes()]
        self.assertEqual(sorted(moved), sorted(order))
        self.assertEqual(self.tmux("views", "list-keys", "-T", "root").count("move-before"), 1)
        for c in consoles:
            captured = self.tmux("programs", "capture-pane", "-p", "-t", "=c-"+c["ID"]+":")
            self.assertNotRegex(captured, r"35;\d|<35")

    def test_header_dialogs_keep_target_and_manual_geometry(self):
        view = json.loads(self.cli("new-view", "Dialogs"))
        consoles = [json.loads(self.cli("launch", "--view", view["ID"])) for _ in range(2)]
        t = self.terminal()
        t.resize(160, 48)
        t.expect("□")  # console headers are drawn
        t.pump(0.8)
        src, other = [p for p in self.panes() if p["kind"] == "console"]
        self.tmux("views", "resize-pane", "-t", src["id"], "-x", "47")
        before = self.panes()
        pids = self.tmux("programs", "list-panes", "-a", "-F", "#{pane_pid}")

        def header_action(key, title):
            p = next(p for p in self.panes() if p["id"] == src["id"])
            t.output = b""
            t.mouse(2, int(p["x"])+5, int(p["y"]))
            t.mouse(2, int(p["x"])+5, int(p["y"]), release=True)
            t.expect("Kill and replace here")
            t.send(key, 0.4)
            t.expect(title)

        header_action(b"n", "RENAME CONSOLE")
        # Invalid input stays in the dialog; it never becomes terminal input.
        t.send(b"bad#name\r")
        t.expect("cannot contain")
        self.tmux("views", "select-pane", "-t", other["id"])
        t.send(b"renamed 'console'\r", 0.5)
        self.wait_dialog_closed()
        self.assertEqual(next(c["Name"] for c in json.loads(self.cli("consoles"))
                              if c["ID"] == src["console"]), "renamed 'console'")
        self.assertEqual(self.panes(), before)
        header_action(b"m", "KILL CONSOLE")
        t.send(b"\x1b", 0.4)
        self.wait_dialog_closed()
        self.assertEqual(len(json.loads(self.cli("consoles"))), 2)
        self.assertEqual(pids, self.tmux("programs", "list-panes", "-a", "-F", "#{pane_pid}"))
        # The finished-program menu also opens the full-screen centered modal.
        self.tmux("views", "select-pane", "-t", src["id"])
        t.send(b"exit\r", 0.5)
        t.output = b""
        t.send(b"\r")
        t.expect("Restart")
        t.send(b"n", 0.4)
        t.expect("RENAME CONSOLE")
        t.send(b"\x1b", 0.4)
        self.wait_dialog_closed()
        # Confirming while focus changes still ends the originally named console.
        header_action(b"m", "KILL CONSOLE")
        self.tmux("views", "select-pane", "-t", other["id"])
        t.send(b"\r", 0.5)
        self.wait_dialog_closed()
        self.assertEqual([c["ID"] for c in json.loads(self.cli("consoles"))], [other["console"]])

    def test_action_error_stays_in_modal_until_dismissed(self):
        view = json.loads(self.cli("new-view", "Errors"))
        console = json.loads(self.cli("launch", "--view", view["ID"]))
        t = self.terminal()
        t.resize(120, 36)
        t.expect("□")  # console headers are drawn
        client = self.tmux("views", "list-clients", "-F", "#{client_name}")
        t.output = b""
        p = subprocess.Popen([str(BINARY), "_view", "layout", "--client", client,
                              "--layout", "invalid"], env=self.env,
                             stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            t.expect("Layout")
            t.expect("invalid layout")
            t.expect("Close")
            t.pump(5.2)  # Longer than the timeout for background tmux commands.
            self.assertIsNone(p.poll())
            t.send(b"\x1b", 0.3)
            out, err = p.communicate(timeout=10)
            self.assertEqual(p.returncode, 0, err)
            self.assertEqual(out, b"")
        finally:
            if p.poll() is None:
                p.kill()
                p.communicate()
        self.assertEqual(list((self.root / "r/shellstudio").glob("message-*.json")), [])
        pane = next(p for p in self.panes() if p["console"] == console["ID"])
        self.tmux("views", "select-pane", "-t", pane["id"])
        t.send(b"printf 'MODAL_%s\\n' CLEAN\r", 0.3)
        captured = self.tmux("programs", "capture-pane", "-p", "-t", "=c-"+console["ID"]+":")
        self.assertIn("MODAL_CLEAN", captured)
        self.assertNotIn("invalid layout", captured)

    def test_no_agents_and_isolated_second_user_paths(self):
        self.cli("doctor")
        config = self.root / "c/shellstudio/extensions"
        config.mkdir()
        (config / "broken.json").write_text('{"schema_version":99}')
        t = self.terminal("extensions", "manage")
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

    def test_empty_program_server_keeps_stopped_console_metadata(self):
        view = json.loads(self.cli("new-view", "Main"))
        console = json.loads(self.cli("launch", "--view", view["ID"]))
        self.tmux("programs", "set-option", "-s", "exit-empty", "off")
        self.cli("stop", console["ID"])
        self.assertEqual(self.tmux("programs", "list-sessions"), "")
        self.cli("doctor")
        t = self.terminal()
        t.expect("console is stopped")
        self.assertEqual(len(json.loads(self.cli("consoles"))), 1)
        self.assertEqual(self.tmux("programs", "list-sessions"), "")

    # --- Redesign: top bar, header buttons, Minimized panel, tags, i18n ---

    def db(self):
        return sqlite3.connect(self.root / "d/shellstudio/shellstudio.db")

    def pane_of(self, kind, console=None, view=None):
        if view is None:
            # The view the terminal is looking at.
            sessions = self.tmux("views", "list-clients", "-F", "#{session_name}").split()
            view = sessions[0][2:] if sessions else None
        # The window on screen only: a render briefly moves panels to helper windows.
        rows = self.tmux("views", "list-panes", "-t", "=v-" + view + ":view", "-F",
                         "#{pane_id}|#{@ss_kind}|#{@ss_console}|#{pane_left}|#{pane_top}|#{pane_width}|#{pane_height}") if view else ""
        for row in rows.splitlines():
            p = dict(zip(("id", "kind", "console", "x", "y", "w", "h"), row.split("|")))
            if p["kind"] == kind and (console is None or p["console"] == console):
                return p
        return None

    def program_pid(self, console):
        return self.tmux("programs", "display-message", "-p", "-t", "=c-" + console + ":", "#{pane_pid}")

    def wait_text(self, pane, text, present=True, timeout=10):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            shown = self.tmux("views", "capture-pane", "-p", "-t", pane["id"])
            if (text in shown) == present:
                return shown
            time.sleep(0.2)
        self.fail(f"{text!r} {'not ' if present else ''}shown in {pane['kind']}:\n{shown}")

    def click_header_button(self, t, pane, slot):
        """slot counts columns from the pane's right edge: 1 menu, 4 maximize, 7 minimize."""
        x = int(pane["x"]) + int(pane["w"]) - 1 - slot
        t.mouse(0, x, int(pane["y"]))
        t.mouse(0, x, int(pane["y"]), release=True, wait=0.6)

    def test_top_bar_tabs_and_header_buttons(self):
        alpha = json.loads(self.cli("new-view", "Alpha"))
        beta = json.loads(self.cli("new-view", "Beta"))
        self.cli("launch", "--view", alpha["ID"], "--name", "One")
        consoles = [json.loads(self.cli("launch", "--view", beta["ID"], "--name", n)) for n in ("Two", "Three")]
        t = self.terminal()
        t.resize(160, 48)
        t.expect("VIEWS")
        t.expect("New console")
        client = self.tmux("views", "list-clients", "-F", "#{client_name}")
        def session():
            return self.tmux("views", "list-clients", "-F", "#{session_name}")
        # Tabs follow " VIEWS ": " Alpha " then " Beta ", one space apart.
        x_beta = len(" VIEWS ") + 1 + len(" Alpha ") + 1 + 1
        target = "v-" + (alpha["ID"] if session() == "v-" + beta["ID"] else beta["ID"])
        x = x_beta if target == "v-" + beta["ID"] else len(" VIEWS ") + 2
        t.mouse(0, x, -TOP_BAR_ROWS)
        t.mouse(0, x, -TOP_BAR_ROWS, release=True, wait=0.5)
        self.wait_for(lambda: session() == target)
        if target != "v-" + beta["ID"]:
            t.mouse(0, x_beta, -TOP_BAR_ROWS)
            t.mouse(0, x_beta, -TOP_BAR_ROWS, release=True, wait=0.5)
            self.wait_for(lambda: session() == "v-" + beta["ID"])
        t.expect("□")
        self.wait_for(lambda: self.pane_of("console", consoles[0]["ID"]) is not None)
        pid = self.program_pid(consoles[0]["ID"])
        # □ zooms the pane, ─ minimizes the console without ending it.
        self.click_header_button(t, self.pane_of("console", consoles[1]["ID"]), 4)
        self.wait_for(lambda: self.tmux("views", "display-message", "-p", "-t", "=v-" + beta["ID"] + ":", "#{window_zoomed_flag}") == "1")
        zoomed = lambda c: self.tmux("programs", "show-options", "-qv", "-t", "=c-" + c["ID"] + ":", "@ss_zoomed")
        self.wait_for(lambda: zoomed(consoles[1]) == "1")  # its header shows ❐
        self.tmux("views", "resize-pane", "-Z", "-t", self.pane_of("console", consoles[1]["ID"])["id"])
        self.wait_for(lambda: zoomed(consoles[1]) == "")
        self.click_header_button(t, self.pane_of("console", consoles[0]["ID"]), 7)
        self.wait_for(lambda: self.pane_of("console", consoles[0]["ID"]) is None)
        self.assertEqual(self.program_pid(consoles[0]["ID"]), pid)
        with self.db() as db:
            rows = db.execute("SELECT console_id, minimized FROM members WHERE view_id=? ORDER BY position", (beta["ID"],)).fetchall()
        self.assertEqual(rows, [(consoles[0]["ID"], 1), (consoles[1]["ID"], 0)])
        # The render that minimized it joins the panel back when it ends.
        self.wait_for(lambda: self.pane_of("minimized") is not None)
        panel = self.pane_of("minimized")
        self.wait_text(panel, "Terminal · Two")
        # A double click on its row restores it, in its place, same program.
        # The table row (not the footer) of the console, on a settled frame.
        def table_row():
            out = subprocess.run(["tmux", "-u", "-S", str(self.socket("views")), "capture-pane", "-p", "-t", panel["id"]],
                                 env=self.env, capture_output=True, text=True, timeout=5).stdout.split("\n")
            return next((i for i, r in enumerate(out) if "Terminal · Two" in r and "No tag" in r), None)
        self.wait_for(lambda: table_row() is not None)
        panel = self.pane_of("minimized")
        x, y = int(panel["x"]) + 14, int(panel["y"]) + table_row()
        # A terminal sends a double click as press/release twice in a row.
        click = f"\x1b[<0;{x+1};{y+1+TOP_BAR_ROWS}M\x1b[<0;{x+1};{y+1+TOP_BAR_ROWS}m"
        t.send((click * 2).encode(), 0.5)
        self.wait_text(panel, "No minimized consoles", timeout=20)
        self.wait_for(lambda: self.pane_of("console", consoles[0]["ID"]) is not None)
        ours = {c["ID"] for c in consoles}
        self.assertEqual([p["console"] for p in self.panes() if p["console"] in ours],
                         [consoles[0]["ID"], consoles[1]["ID"]])
        self.assertEqual(self.program_pid(consoles[0]["ID"]), pid)
        self.wait_text(panel, "No minimized consoles")

    def test_minimized_search_filter_and_tags(self):
        view = json.loads(self.cli("new-view", "Main"))
        consoles = [json.loads(self.cli("launch", "--view", view["ID"], "--name", n)) for n in ("Api", "Web", "Keep")]
        t = self.terminal()
        t.resize(160, 48)
        t.expect("□")
        self.wait_for(lambda: self.pane_of("minimized") is not None and self.pane_of("console", consoles[2]["ID"]) is not None)
        panel = self.pane_of("minimized")
        with self.db() as db:
            tags = db.execute("SELECT id, name FROM tags ORDER BY position").fetchall()
        self.cli("_action", "tag", "--pane", panel["id"], "--session", "c-" + consoles[0]["ID"], "--target", tags[1][0])
        self.assertEqual(self.tmux("programs", "show-options", "-qv", "-t", "=c-" + consoles[0]["ID"] + ":", "@ss_tag"), tags[1][1])
        for c in consoles[:2]:
            self.cli("_action", "minimize", "--pane", panel["id"], "--session", "c-" + c["ID"])
        # The panel keeps its pane ID when a render moves it aside and back.
        capture = lambda: self.tmux("views", "capture-pane", "-p", "-t", panel["id"])
        self.wait_for(lambda: "Terminal · Api" in capture() and "Terminal · Web" in capture())
        self.assertIn("MINIMIZED  2", capture())
        # Filter by tag through the stored preference, as its menu does.
        self.cli("_action", "minprefs", "--target", "filter:" + tags[1][0])
        self.wait_for(lambda: "Terminal · Web" not in capture())
        self.assertIn("Terminal · Api", capture())
        self.cli("_action", "minprefs", "--target", "filter:")
        self.wait_for(lambda: "Terminal · Web" in capture())
        # Search: click the field and type.
        self.wait_for(lambda: self.pane_of("minimized") is not None)
        panel = self.pane_of("minimized")
        t.mouse(0, int(panel["x"]) + 3, int(panel["y"]) + 1)
        t.mouse(0, int(panel["x"]) + 3, int(panel["y"]) + 1, release=True)
        t.send(b"web", 0.6)
        self.wait_for(lambda: "Terminal · Api" not in capture())
        self.assertIn("Terminal · Web", capture())
        # Deleting the tag in the tags dialog leaves the console untagged.
        client = self.tmux("views", "list-clients", "-F", "#{client_name}")
        dialog = subprocess.Popen([str(BINARY), "_dialog", "tags", "--client", client], env=self.env,
                                  stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        t.expect("TAGS")
        t.expect("consoles", timeout=10)  # the list is drawn
        t.pump(0.5)
        t.send(b"\x1b[B", 0.3)      # second tag
        t.send(b"\x1b[3~", 0.5)     # Del
        t.expect("left untagged")
        t.send(b"\r", 0.5)
        t.send(b"\x1b", 0.5)
        dialog.wait(timeout=10)
        with self.db() as db:
            left = db.execute("SELECT name FROM tags ORDER BY position").fetchall()
            self.assertIsNone(db.execute("SELECT tag_id FROM consoles WHERE id=?", (consoles[0]["ID"],)).fetchone()[0], (tags, left))
            self.assertEqual(db.execute("SELECT count(*) FROM tags WHERE id=?", (tags[1][0],)).fetchone()[0], 0)
        self.assertEqual(self.tmux("programs", "show-options", "-qv", "-t", "=c-" + consoles[0]["ID"] + ":", "@ss_tag"), "")

    def test_explorer_search_filters_the_tree(self):
        project = self.root / "project"
        for f in ("src/main.go", "src/util/strings.go", "docs/README.md", "Makefile"):
            (project / f).parent.mkdir(parents=True, exist_ok=True)
            (project / f).write_text("x\n")
        t = Terminal(self.env, str(project))
        self.terms.append(t)
        t.resize(140, 40)
        t.expect("Search files")
        self.wait_for(lambda: self.pane_of("explorer") is not None)
        tree = self.pane_of("explorer")
        t.mouse(0, 3, 1)
        t.mouse(0, 3, 1, release=True)
        t.send(b"strings", 1.0)
        capture = lambda: self.tmux("views", "capture-pane", "-p", "-t", tree["id"])
        self.wait_for(lambda: "strings.go" in capture() and "Makefile" not in capture())
        self.assertIn("util", capture())  # ancestors of the match stay
        t.send(b"\x1b", 0.6)
        self.wait_for(lambda: "Makefile" in capture())

    def test_panel_sizes_persist_after_leaving(self):
        project = self.root / "project"
        project.mkdir()
        t = Terminal(self.env, str(project))
        self.terms.append(t)
        t.resize(160, 48)
        t.expect("MINIMIZED")
        self.wait_for(lambda: self.pane_of("explorer") is not None)
        tree = self.pane_of("explorer")
        self.tmux("views", "resize-pane", "-t", tree["id"], "-x", "56")
        self.cli("_action", "panel-sizes", "--pane", tree["id"])
        view = json.loads(self.cli("views"))[0]["ID"]
        with self.db() as db:
            self.assertEqual(db.execute("SELECT explorer_width FROM views WHERE id=?", (view,)).fetchone()[0], 56)
        t.send(b"\x1b[21~", 1.0)  # F10
        self.tmux("views", "kill-server")
        t = self.terminal()
        t.resize(160, 48)
        t.expect("MINIMIZED")
        self.wait_for(lambda: self.pane_of("explorer") is not None and self.pane_of("explorer")["w"] == "56")

    def test_spanish_interface(self):
        self.env.update(LANG="es_AR.UTF-8", LC_ALL="es_AR.UTF-8", LC_MESSAGES="es_AR.UTF-8")
        project = self.root / "proyecto"
        project.mkdir()
        t = Terminal(self.env, str(project))
        self.terms.append(t)
        t.resize(160, 48)
        for text in ("VISTAS", "Nueva consola", "Buscar archivos", "MINIMIZADAS", "Buscar consolas"):
            t.expect(text)
        self.assertEqual(self.tmux("views", "show-options", "-gqv", "@ss_lang"), "es")


    def test_client_without_utf8_sees_ascii_buttons(self):
        """ShellStudio attaches with tmux -u; a foreign client without UTF-8
        mode gets one-column ASCII buttons instead of glyphs it cannot draw."""
        self.env.update(LANG="C", LC_ALL="C")
        view = json.loads(self.cli("new-view", "Main"))
        self.cli("launch", "--view", view["ID"], "--name", "One")
        t = self.terminal()
        t.resize(120, 30)
        t.expect("□")
        pid, fd = pty.fork()
        if pid == 0:
            os.execvpe("tmux", ["tmux", "-S", str(self.socket("views")), "attach-session", "-r", "-t", "=v-" + view["ID"]],
                       {**self.env, "TERM": "xterm-256color"})
        try:
            fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 120, 0, 0))
            out, deadline = b"", time.monotonic() + 20
            while time.monotonic() < deadline and b"New console v" not in out:
                ready, _, _ = select.select([fd], [], [], 0.2)
                if ready:
                    try:
                        out += os.read(fd, 65536)
                    except OSError:
                        break
            self.assertIn(b"New console v", out)
            self.assertNotIn("▼".encode(), out)
            clients = self.tmux("views", "list-clients", "-F", "#{client_utf8}").split()
            self.assertIn("0", clients)
        finally:
            os.kill(pid, signal.SIGKILL)
            os.waitpid(pid, 0)
            os.close(fd)


if __name__ == "__main__":
    unittest.main(verbosity=2)
