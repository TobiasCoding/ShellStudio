# Architecture

The entry point execs tmux directly; no outer TUI probes the terminal before
attaching. Each logical view has one presentation session on views.sock. Its
panes attach as clients to
programs.sock, which owns the persistent processes. Removing presentation panes
does not end programs. Clients showing the same view share focus and layout.
The explorer and popup dialogs use a small terminal cell renderer; Bubble Tea is
used for Notes, extension management and read-only viewers only.

The executable embeds its catalog, JSON schema and MIT Python packages. Metadata
uses versioned transactional migrations, WAL, foreign keys and a busy timeout
on every connection. Note writes commit with synchronous=FULL: Saved is shown
only after the WAL is on disk. View and console metadata commit with
synchronous=NORMAL: the database remains consistent after a power cut, but
recent metadata commits may be lost. Note writers serialize changes to the
connection's synchronization setting. Preferences (last view, last console kind,
recent folders) are replaced atomically without fsync; tree state is metadata.
Schema 3 is current; newer schemas
are rejected. Existing data is never silently reset after an integrity failure.

The program server invokes the private _exec entry point with a console ID. It
loads saved argv/environment, changes directory and calls execve. Manifest fields
never become shell source. Relays and explorer UIs are presentation processes.
Stable console IDs reconcile membership. Focus, attach and resize reuse panes.
Stale IDs can fall back only for navigation; destructive actions reject them.
F10 detaches the client and preserves its view session. Reopening reconciles
panes by console ID without restarting running programs. Explorer state is saved
per view. Migration from schema 2 enables the explorer on existing views.

tmux handles resize and terminal output natively. Commands are batched and have
a five-second deadline. Subprocess output is limited to 64 KiB, downloads to
256 MiB, extracted executables to 512 MiB, manifests to 256 KiB, previews to
64 KiB and directory lists to 2,000 entries. Notes lists return 200 summaries;
bodies are limited to 4 MiB. Search has a two-second deadline. Read-only viewers
load at most 200 rows once every two seconds. Timed-out installers have their
process groups terminated.

Each Notes editor permits one save in flight. Revision compare-and-swap and a
conflict-copy insert happen inside one transaction. The committed revision is
returned only after Commit succeeds. New edits arriving during a commit remain
pending. Failures retain text and display the actual error.

Extensions are manifests and external processes, not loaded Go plugins. Invalid
entries do not block Terminal. Private staging and verification precede the active
receipt. Partial installs never activate. HTTPS artifacts require a pinned SHA-256.
MCP packages use separate venvs to isolate their upstream module names.

MCP preview/apply checks the original file hash and rejects name conflicts.
Claude JSON retains unknown entries. Codex TOML retains original text and appends
one table. A private backup must succeed before atomic replacement.

Configuration writes use nonblocking advisory locks, atomic replacement and file
plus directory fsync. Events retain 1,000 entries with details capped at 4 KiB.
Program transcripts are not logged by ShellStudio.
