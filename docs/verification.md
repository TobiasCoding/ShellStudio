# Verification record

The current workspace restoration is documented in
[ui-restoration-20261002.md](ui-restoration-20261002.md), including keyboard and
mouse workflows, shared view focus and startup-byte checks. The measurements
below are the historical pre-restoration verification, not new performance
measurements of the restored interface.

Local verification on Linux amd64, 2026-10-01. No live agent configurations,
conversations or operational databases were used. The runtime tests use synthetic
data, private XDG directories and isolated tmux sockets under the ignored .work
directory. Go 1.23.0, tmux 3.4 and Python 3.12 were available for this run.

## Coverage

- Go unit tests: note compare-and-swap, concurrent connections, trash conflicts,
  full-disk rollback, backups, schema checks, membership retention and log bounds.
- Configuration: atomic replacement, symlink rejection, lock contention, XDG
  validation, directory precedence and paths with spaces.
- Extensions: catalog without agents, invalid manifests, missing dependencies,
  incomplete staging, isolated offline Python installation and MCP initialization.
- MCP changes: preserve foreign JSON/TOML entries and comments, create exact
  backups, reject conflicting names and stale previews.
- UI: debounce/continuous-save dispatch, flush on exit, failed-save retention,
  edits arriving during commit and narrow terminal rendering.
- Real PTYs: independent clients, resize, replaced panes, detach/reattach,
  abrupt UI kill, retained program PIDs, explicit stop/restart, durable notes after
  abrupt kill, invalid extension isolation and separate XDG roots.
- Separate UID probe: a process running as nobody could not read a synthetic
  file inside a mode-0700 directory owned by the application user.
- Read-only viewers: missing files are not created, sources are not modified,
  terminal controls are stripped and timeline intervals are bounded.
- Subprocesses: deadline cancellation, literal argv and a verified 64 KiB output
  cap, including Go's optimized stream-copy paths. Python isolated mode prevents
  project-local modules from shadowing the installed MCP server.

Commands: make check (tests, race detector, offline MCP and PTY tests, go vet and
publication scan), make release (Linux amd64/arm64 plus SHA256SUMS).
Result: all 25 Go test functions (including the opt-in Python integration test),
the race-enabled suite, and all three real PTY scenarios passed. The process-loss
scenario additionally confirmed that reopening metadata does not restart programs.
Release files also include the corresponding source archive and license notices.
The amd64 build runs locally. The arm64 build is cross-compiled; it has not been
executed on arm64 hardware. Hosted CI is configured but has not run here.
External Claude/Codex artifacts were downloaded to measure their digests, not
installed into the user's agent environment. The source remains ready for review
and publication; no remote publication was performed.

## Local comparison with sesiones

The reproducible harness is scripts/benchmark.py. It accepts the reference's
source path explicitly and redirects all mutable reference state into .work.
Reference operational callbacks are disabled. Both applications have two detached
Terminal consoles and no explorer. Each warm reconciliation starts a fresh CLI
process. Resize measures two native tmux geometry changes. Idle includes the two
tmux servers and their descendants, excluding menu UIs.

| Measurement | ShellStudio | Reference |
| --- | ---: | ---: |
| Warm reconcile median, 12 samples | 18.520 ms | 75.706 ms |
| Warm reconcile maximum | 19.547 ms | 78.430 ms |
| Resize round-trip median, 12 samples | 2.858 ms | 2.901 ms |
| Resize round-trip maximum | 3.478 ms | 4.531 ms |
| Idle RSS, six processes each | 20.60 MiB | 34.41 MiB |
| Idle CPU over about three seconds | 0 measured ticks | 0 measured ticks |

These are host-specific smoke measurements, not statistically established speed
claims. Zero measured CPU ticks is below the sampling resolution, not proof of
zero CPU use. Interactive menu rendering and different agent workloads are not
covered by this benchmark. The reference was read locally and is not distributed.

## Practical limits

Autosave dispatch meets the idle and continuous-typing deadlines; durable commit
latency depends on storage. Saving remains visible until confirmation. Permanent
I/O failure cannot guarantee a commit. An abrupt kill may lose pending text.

Uninstallation retains package files required by running programs. Manual cleanup
after those programs finish is described in storage.md. Abruptly killed UIs can
leave unused presentation sessions until cleaned; source processes remain intact.
Clipboard and SSH behavior depend on the local terminal and SSH environment;
PTY regressions ran locally, not across a physical network or Windows console.
