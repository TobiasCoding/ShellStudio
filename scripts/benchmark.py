#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
"""Compare two-console presentation work with an explicitly selected reference.

Only synthetic state and isolated sockets are used. Reference helper callbacks
are disabled; no operational scripts are invoked. Results are local observations,
not claims of equivalence between the applications' entire feature sets.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import statistics
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]
BINARY = ROOT / "bin/shellstudio"


def command(argv, env=None):
    return subprocess.run(list(map(str, argv)), env=env, cwd=ROOT, check=True,
                          capture_output=True, text=True, timeout=20).stdout.strip()


def reference(source, root):
    sys.dont_write_bytecode = True
    spec = importlib.util.spec_from_file_location("benchmark_reference", source)
    mod = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = mod
    spec.loader.exec_module(mod)
    mod.ROOT = root
    mod.RUNTIME = root / "old"
    mod.RUNTIME.mkdir(exist_ok=True, mode=0o700)
    mod.SOCKET = mod.RUNTIME / "programs.sock"
    mod.VIEW_SOCKET = mod.RUNTIME / "views.sock"
    for attr, name in [("VIEWS_FILE", "views.json"), ("LAST_VIEW", "last-view"),
                       ("LAST_KIND", "last-kind"), ("ERROR_FILE", "error.json")]:
        setattr(mod, attr, mod.RUNTIME / name)
    mod.SCRIPT = Path("/bin/true")  # Never invoke the reference's operational launcher.
    mod._SETUP = None
    return mod


def timings(work, n=12):
    values = []
    for _ in range(n):
        start = time.perf_counter()
        work()
        values.append((time.perf_counter()-start)*1000)
    return {"median_ms": round(statistics.median(values), 3),
            "max_ms": round(max(values), 3), "samples": len(values)}


def processes(server_pids):
    table = {}
    for item in Path("/proc").iterdir():
        if not item.name.isdigit():
            continue
        try:
            fields = (item/"stat").read_text().rsplit(")", 1)[1].split()
            table[int(item.name)] = (int(fields[1]), int(fields[11])+int(fields[12]),
                                     int(fields[21])*os.sysconf("SC_PAGE_SIZE"))
        except (OSError, ValueError, IndexError):
            continue
    owned = set(server_pids)
    while True:
        more = {pid for pid, (parent, _, _) in table.items() if parent in owned}
        if more <= owned:
            break
        owned |= more
    return {pid: table[pid] for pid in owned if pid in table}


def idle(sockets):
    pids = [int(command(["tmux", "-S", p, "display-message", "-p", "#{pid}"]))
            for p in sockets]
    before = processes(pids)
    start = time.monotonic()
    time.sleep(3)
    after = processes(pids)
    elapsed = time.monotonic()-start
    ticks = sum(max(0, values[1]-before.get(pid, values)[1]) for pid, values in after.items())
    return {"rss_mib": round(sum(v[2] for v in after.values())/2**20, 2),
            "cpu_percent_one_core": round(ticks/os.sysconf("SC_CLK_TCK")/elapsed*100, 3),
            "processes": len(after), "interval_seconds": round(elapsed, 2)}


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--reference", type=Path, required=True)
    p.add_argument("--render-reference", action="store_true")
    p.add_argument("--state", type=Path)
    args = p.parse_args()
    if args.render_reference:
        reference(args.reference, args.state).render_view("bench")
        return
    work = ROOT / ".work"
    work.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="bench-", dir=work) as directory:
        root = Path(directory)
        env = {**os.environ, "SHELL": "/bin/sh", "TERM": "xterm-256color"}
        for key, part in [("XDG_CONFIG_HOME", "c"), ("XDG_DATA_HOME", "d"),
                          ("XDG_STATE_HOME", "s"), ("XDG_RUNTIME_DIR", "r")]:
            env[key] = str(root/part)
        old = reference(args.reference, root)
        sockets = [old.SOCKET, old.VIEW_SOCKET,
                   root/"r/shellstudio/programs.sock", root/"r/shellstudio/views.sock"]
        try:
            old.save_view("bench", [])
            with old.view_store(write=True) as data:
                data["bench"]["explorer"] = False
            for i in range(2):
                old.create("terminal", f"benchmark{i}", True, [], announce=False,
                           view_name="bench")
            old.render_view("bench")
            view = json.loads(command([BINARY, "new-view", "Benchmark"], env))
            for i in range(2):
                command([BINARY, "launch", "--view", view["ID"], "--name", f"console{i}"], env)
            client = "1234567890abcdef12345678"
            render = [BINARY, "_render", client, view["ID"]]
            session = command(render, env)
            old_command = [sys.executable, __file__, "--reference", args.reference,
                           "--render-reference", "--state", root]
            results = {
                "method": "Two detached consoles, explorer disabled; fresh process per warm reconcile. Idle includes both tmux servers and descendants, excludes menu UI.",
                "shellstudio": {"warm_reconcile": timings(lambda: command(render, env))},
                "reference": {"warm_reconcile": timings(lambda: command(old_command, env))},
            }
            for label, socket, target in [("shellstudio", sockets[3], "="+session+":"),
                                          ("reference", sockets[1], "=bench:")]:
                def resize():
                    command(["tmux", "-S", socket, "resize-window", "-t", target,
                             "-x", "80", "-y", "24", ";", "resize-window", "-t", target,
                             "-x", "140", "-y", "40"])
                results[label]["resize_roundtrip"] = timings(resize)
            results["shellstudio"]["idle"] = idle(sockets[2:])
            results["reference"]["idle"] = idle(sockets[:2])
            print(json.dumps(results, indent=2))
        finally:
            for socket in sockets:
                subprocess.run(["tmux", "-S", str(socket), "kill-server"],
                               capture_output=True, timeout=5)


if __name__ == "__main__":
    main()
