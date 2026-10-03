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

    def expect(self, text, timeout=10):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if text.encode() in self.output:
                return
            self.pump(0.1)
        raise AssertionError(f"terminal did not show {text!r}; tail={self.output[-2200:]!r}")

    def mouse(self, button, x, y, release=False, wait=0.15):
        return self.send(f"\x1b[<{button};{x+1};{y+1}{'m' if release else 'M'}".encode(), wait)

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
        self.env.pop("TMUX_PANE", None)
        self.env.pop("SHELLSTUDIO_CONSOLE", None)
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
        self.assertIn("F2 Menu", self.tmux("views", "capture-pane", "-p", "-t", tree[0]["id"]))
        return tree[0]

    def test_current_and_explicit_directory_workspace(self):
        self.assert_directory_workspace(self.root / "project with spaces")

    def test_cli_startup_does_not_probe_terminal(self):
        t = self.terminal("version")
        t.expect("ShellStudio")
        self.assertEqual(t.output, self.cli("version").strip().encode()+b"\r\n")
        (WORK / "ui-startup.pty").write_bytes(t.capture)

    def test_diagnostic_report_preserves_running_workspace_and_privacy(self):
        secret = "PRIVATE-DIAGNOSTIC-FIXTURE"
        folder = self.root / secret
        folder.mkdir()
        terminal = self.terminal(str(folder))
        terminal.expect("F2 Menu")
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
                terminal.expect("F3 New")
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
                tree = self.assert_explorer()
                y = int(tree["h"])-5  # Click F3 in the tree's shortcut area.
                t.mouse(0, 5, y)
                t.mouse(0, 5, y, release=True, wait=0.4)
            else:
                t.send(b"\x1bOR", 0.4)  # F3
            t.expect("NEW CONSOLE IN Main")
            t.send(b"\r")
            t.expect("Name of terminal")
            t.send(name.encode() + b"\r")
            self.wait_for(lambda: any(c["Name"] == "terminal-"+name
                                     for c in json.loads(self.cli("consoles")) or []))
            t.expect("ACTIVE:")
            # Creation records metadata before reconciling the panes. Wait for
            # the dialog process to exit before sending another shortcut.
            self.wait_dialog_closed()
            self.assert_explorer()
        consoles = json.loads(self.cli("consoles"))
        ids = {c["Name"]: c["ID"] for c in consoles}
        pids = self.tmux("programs", "list-panes", "-a", "-F", "#{pane_pid}")
        for key, title in ((b"\x1bOQ", "VIEW Main"), (b"\x1b[15~", "ARRANGE Main"),
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
        # Follow a dialog from F2 and return with Escape, then toggle the tree.
        t.pump(0.5)
        t.output = b""
        t.send(b"\x1bOQ", 0.4)
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
        t.send(b"\x1bOQ", 0.5)
        t.expect("VIEW Main")
        t.send(b"e", 0.5)
        self.wait_for(lambda: any(p["kind"] == "explorer" for p in self.panes()))
        self.wait_dialog_closed()
        self.assert_explorer()
        # A compact F2 scrolls through stacked categories to the last action.
        t.resize(70, 24)
        t.pump(0.6)
        t.output = b""
        t.send(b"\x1bOQ", 0.5)
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
        self.assertEqual(int(self.assert_explorer()["w"]), border)
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


if __name__ == "__main__":
    unittest.main(verbosity=2)
