# Storage and recovery

| Purpose | Default |
| --- | --- |
| Configuration and manifests | ~/.config/shellstudio |
| Metadata, notes, packages and extension data | ~/.local/share/shellstudio |
| Runtime fallback | ~/.local/state/shellstudio/run |
| Preferred sockets | $XDG_RUNTIME_DIR/shellstudio |

XDG_CONFIG_HOME, XDG_DATA_HOME and XDG_STATE_HOME override parent directories.
Relative values are rejected. ShellStudio directories must belong to the current
UID and are mode 0700. Sensitive files are mode 0600. Direct symlinks are rejected
for sensitive write destinations. Unix sockets require a short runtime path.

The data directory contains shellstudio.db. Do not copy it alone while writing:
its WAL may contain the latest commits. Create a verified backup with:

    shellstudio backup /absolute/private/path/backup.db

SQLite VACUUM INTO refuses an existing destination; ShellStudio opens and checks
the result. Opening corrupt or future-schema databases fails visibly.

Recovery:

1. Exit all UIs, Notes consoles and other writers. Preserve the damaged database
   with its -wal and -shm sidecars for investigation.
2. Copy the backup into a separate XDG_DATA_HOME at shellstudio/shellstudio.db.
   Run doctor there to verify it, using separate configuration and runtime paths.
3. Move the old database and sidecars out of the live directory. Put the verified
   backup at the live database path with mode 0600.
4. Run doctor; inspect views and notes before explicitly restarting programs.

Machine restart never relaunches saved commands automatically. No private data
or sessions are automatically imported from another application.

Uninstall removes an extension's receipt and disables new launches. Immutable
packages remain because active programs may still load their files. After ending
all consoles and MCP processes using it, reclaim extensions/ID in the data
directory. Its user data lives separately under extension-data/ID and is retained
unless explicitly purged. Imported manifests live at config/extensions/ID.json.

Interrupted installs may leave .install-* directories, which are never active.
Remove those only when no installation is running. New installs use fresh
immutable directories. Old programs never see a package-directory replacement.

To remove ShellStudio, stop programs explicitly, remove the executable, then
remove its own XDG subdirectories only if you also intend to delete notes and
metadata. Killing the program server ends all programs it owns.
