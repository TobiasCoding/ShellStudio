# ShellStudio

![ShellStudio](logo.png)

A standalone Linux terminal workspace for local terminals, WSL and SSH.
Persistent consoles live in a private tmux server. Bubble Tea provides views,
the file explorer, global Notes, extension management and forms.

License: GPL-3.0-only. Bundled agent-chat and agent-gantt packages retain MIT.
External programs have their own licenses. No agent installation is required.

## Build and install

Requirements: Linux/WSL2, Go 1.23+, tmux 3.4+. Optional MCP installation needs
Python 3.9+ with venv/ensurepip.

    sudo apt-get install tmux golang-go python3-venv
    make build
    install -Dm755 bin/shellstudio "$HOME/.local/bin/shellstudio"
    shellstudio doctor
    shellstudio

Go downloads the pinned toolchain if the distribution's Go is older. The binary
embeds the catalog, schema and MCP packages and does not need its source checkout.
For prebuilt releases, verify SHA256SUMS before installing the matching binary.

## Workspace

Press n in Views to create a view with a base folder. Enter opens its console
list; t launches Terminal, n selects an enabled extension. A blank launch folder
uses the view's folder, then ShellStudio's launch directory. Ctrl+F opens the
folder picker; Ctrl+R shows recent folders. Existing consoles keep their folders.

Enter opens a tmux view. F6 changes panel, F4 zooms, F10 returns to the menu.
Mouse clicks focus panels; drag borders to resize. Clients have independent focus.
Closing the UI or SSH connection preserves programs. After a machine restart,
saved consoles are stopped; r explicitly relaunches one.

In the console list: f edits the view; b toggles its explorer; l selects layout;
a links an existing console; d unlinks it; [ / ] moves it; x stops its program.
The explorer supports filtering, preview, e for an external editor, r to rename,
and c to copy a path using OSC 52.

## Notes

Tab to Notes; n creates a note. Search with /, trash with d, toggle trash with t,
rename/restore with r, and export Markdown with e. The library is global.

Saving starts after 300 ms idle or at least once per second while typing.
Saved means SQLite has committed and synced the transaction. Slow storage can
extend Saving. Escape flushes before leaving. Write failures keep the editor
and its text, show the actual error, and allow retry with Ctrl+S or another edit.
Concurrent revisions create a conflict copy. Abrupt kills may lose pending edits.

## Extensions

Terminal is the only built-in program launcher. Notes is bundled and enabled.
Claude, Codex, agent-chat and agent-gantt are optional. Their catalog is always
available, even if the programs are absent.

- Enter: origin, version, dependencies and installation details.
- i: preview/install; e: enable detected program; d: disable.
- c: configure a default named profile with executable, JSON arguments and env.
- a: import a manifest from a file or HTTPS URL; n: add an existing command.
- u: uninstall and retain data; uppercase X: also request data deletion.
- m: preview MCP registration for Claude/Codex, with backup and conflict checks.
  Missing clients produce pending registration; return to m after installation.

Installation is per-user, version pinned, integrity checked and verified before
activation. No automatic updates or permission-bypass flags. Disabling does not
stop programs. Uninstall retains immutable packages for active consoles; see
[storage](docs/storage.md) for reclamation. The MCPs install offline into separate
Python venvs and include read-only viewers. No private sessions or data migrate.

## Diagnostics and development

    ssh HOST -t shellstudio
    shellstudio doctor
    shellstudio backup /absolute/path/backup.db
    shellstudio validate examples/htop.json
    make check
    make release

Press ? for SSH, scp, forwarding, clipboard and WSL help. OSC 52 needs support
and permission in the local terminal. Ctrl+B then [ enters tmux copy mode.

See [architecture](docs/architecture.md), [extensions](docs/extensions.md),
[storage/recovery](docs/storage.md), [security](SECURITY.md),
[contributing](CONTRIBUTING.md) and [notices](THIRD_PARTY_NOTICES.md).
This is a local release candidate. Hosted CI and publication require a repository.
