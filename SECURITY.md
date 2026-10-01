# Security

ShellStudio and extensions run with the user's ordinary permissions. They are
not sandboxed. Review origin, commands, environment and artifact digests before
confirming an installation.

Controls include private per-user XDG directories and sockets, direct argv,
bounded reads, HTTPS, digests, staging, transactional storage and MCP previews
with backups. ShellStudio adds no telemetry or agent permission bypasses.

Secrets entered in profiles and MCP environment fields remain in private config
files and backups. Prefer the clients' own credential mechanisms. Processes with
the same UID and administrators can access user-owned files and sockets.

OSC 52 requires local terminal permission. Previews strip control characters.
SSH helpers display commands and never automatically forward agents or transfer
files. External editors and applications remain responsible for their content.

Advisory locks coordinate ShellStudio instances. Other programs may ignore them;
avoid editing MCP configuration concurrently with applying a preview. A hash
check rejects changes observed before replacement.

Report vulnerabilities privately to the repository maintainer using the hosting
platform's private security reporting when enabled. Do not post credentials or
private conversations in issues.
