# Contributing

Use English for code, comments, documentation and UI. Keep process lifetime
separate from presentation. Do not add hidden installers, permission bypasses,
automatic updates or telemetry.

    make check
    make release

Tests use private state below .work, isolated tmux sockets and synthetic data.
Never test against live MCP databases, agent configurations or tmux servers.
The Python install test works offline. Downloaded agents are not run by tests.

Cover meaningful state transitions: revision conflicts, interrupted installs,
stale pane identity, storage failures and configuration preservation. Schema
changes need transactional migrations and recovery coverage. Keep dependencies
pinned in go.mod/go.sum. Run gofmt on Go source.

Before publication, run scripts/publication_check.py and inspect the diff.
Exclude runtime databases, logs, conversations, credentials and personal paths.
CI builds Linux amd64/arm64 binaries and checksums; owners decide publication.

Contributions use GPL-3.0-only. Preserve notices on bundled MIT code and identify
modifications to it.
