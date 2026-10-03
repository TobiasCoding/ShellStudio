# Sharing a diagnostic report

ShellStudio records operational events automatically from this version onward.
After updating, leave the view with F10 and reopen it so its helper processes
use the new binary. Existing programs keep running. Reproduce the problem and,
while the affected workspace is still open, run this from a second SSH session:

```sh
shellstudio report
```

The command prints the absolute path of a JSON file. Send that file together
with the approximate time of the problem and what you expected to happen.
There is no automatic upload. A screenshot is useful for visual problems;
terminal contents are deliberately absent from reports.

Reports default to `$XDG_STATE_HOME/shellstudio/reports/` or
`~/.local/state/shellstudio/reports/`. They have permissions `0600` and are
never overwritten. You can choose a new destination or stream JSON:

```sh
shellstudio report ./shellstudio-report.json
shellstudio report --stdout
```

`--stdout` also works when storage is unwritable. Reports still collect partial
results if configuration or the database is broken. Collection does not repair
the database, apply migrations, start tmux servers, restart programs or change
the workspace layout. Each database collection has a five-second deadline;
tmux probes have individual one-second deadlines. It is a live snapshot, so
concurrent actions may change state between sections.

## What is included

- ShellStudio version/build, Go runtime, OS/architecture, tmux version, terminal
  dimensions/capabilities and whether SSH or tmux is in use.
- Storage permissions, configuration validity and SQLite integrity/schema.
- Internal view/console IDs and membership, layouts, explorer visibility,
  pane sizes and positions, process IDs, exit statuses and connected clients.
- The latest 1,000 diagnostic events with UTC timestamps, binary version,
  process ID, operation, duration, error category and source call sites.

Reports exclude terminal output, keystrokes, conversations, notes, view/console
names, folder paths, command arguments, SSH addresses and environment/config
contents. Raw error messages are replaced with categories because they can
contain private arguments or credentials. Custom extension names are omitted.
The older `last-error.json` and database event details are not attached.

Automatic logs are JSON lines in the state directory: `diagnostics.jsonl`,
`diagnostics.jsonl.1` and `diagnostics.jsonl.2`, at most 2 MiB each. Rotation works
across helper processes. Successful fast tmux queries are omitted. Writes are
best-effort: lock contention, full disks or permission failures can lose events
without blocking the workspace. Reports themselves are kept until you delete
them. If startup says logging is unavailable, the report's storage sections can
help explain why. A successful process replacement (`console-exec` or
`view-attach`) has no subsequent `command-end` event.

For a quick human-readable dependency check, `shellstudio doctor` remains
available. Use the JSON report when sharing a reproducible problem.
