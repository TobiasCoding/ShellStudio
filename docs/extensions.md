# Extension development

Start with [examples/htop.json](../examples/htop.json). The public JSON schema is
[schema.json](../internal/extensions/schema.json), also printed by the CLI.

    shellstudio validate examples/htop.json

Identity fields: schema_version=1, id, version, description,
compatibility=shellstudio-v1, origin and license. IDs begin with a lowercase
letter and contain letters, digits and hyphens. Terminal is reserved. Unknown
fields, duplicate IDs and unsupported schemas are rejected.

Profiles contain executable, args (JSON array), and env (optional JSON object).
default_profile must exist. User profiles can override these and choose a new
default. The optional commands map describes other executable entry points;
expose launchable alternatives as profiles to select them in the launch form.

Arguments are literal, including whitespace, dollar signs and semicolons.
Declaring a shell explicitly is possible but becomes the extension's reviewed
behavior. ShellStudio never inserts a shell around a command.

Installation kinds:

- existing: resolve the detection executable; no download.
- artifact: choose linux/amd64 or linux/arm64, download its HTTPS url, verify
  sha256, optionally extract the exact regular-file member from tar.gz, and run
  detection.args. The executable goes into the extension's private bin directory.
- builtin and python-bundle: reserved for the embedded catalog.

Dependencies are executable names in PATH. Installation never runs a system
package manager. Imports are capped at 256 KiB. Origin is descriptive metadata,
not proof of trust; hashes identify artifacts but do not establish publisher trust.

Substitutions: {extension} is the verified package root, {data} the extension's
data directory, and {shellstudio} the current binary. Avoid personal absolute paths.

The optional mcp object contains name and a command profile for stdio. The UI
previews client, destination, command, args and environment. Absent clients yield
pending registration. Existing different entries are never replaced.

Bundled references: Claude Code 2.1.287 (upstream commercial terms), Codex 0.160.0
(Apache-2.0), Notes 1.0.0 (GPL-3.0-only), agent-chat and agent-gantt 2.0.0 (MIT).
Native agent artifacts come from upstream npm packages without lifecycle scripts.
MCP packages install from offline wheels into separate Python venvs.

Verification executes downloaded code with the user's ordinary permissions.
Extensions are not sandboxed. Upstream configuration references:
[Claude MCP](https://code.claude.com/docs/en/mcp) and
[Codex MCP](https://developers.openai.com/codex/mcp/).
