# Third-party notices

ShellStudio's original code is GPL-3.0-only; see LICENSE. Direct and transitive Go
dependency versions are pinned in go.mod and go.sum. The release includes copied
upstream notices in third_party/licenses and an inventory in
[third_party/modules.json](third_party/modules.json).

Principal components include Bubble Tea, Bubbles and Lip Gloss (MIT),
go-toml/v2 (MIT), modernc SQLite and its runtime dependencies (their upstream
licenses are preserved), and Go's x packages (BSD-style licenses). SQLite itself
is public domain. See individual notices rather than assuming one license for
all transitive components.

Bubble Tea v1.3.4 is included under third_party/bubbletea with its MIT license.
ShellStudio changes its initialization to select the fixed dark theme without
querying the terminal. The source and patch are described in
[third_party/bubbletea/SHELLSTUDIO.md](third_party/bubbletea/SHELLSTUDIO.md).

Bundled agent-chat and agent-gantt 2.0.0 are separate MIT-licensed Python packages.
Their unmodified server and console-entry source and exact MIT licenses live in
internal/extensions/bundles/agent-chat and agent-gantt. ShellStudio's offline wheel
builder and viewers are new GPL code. Source hashes are recorded in
[third_party/mcp-sources.json](third_party/mcp-sources.json). Each installed wheel
retains its upstream LICENSE in package metadata.

The extension catalog contains references to, not copies of, Claude Code
(Anthropic commercial terms) and Codex (Apache-2.0). Their executables are fetched
only after the user reviews an installation. Review the upstream licenses and
service terms. No external agent's code is relicensed by ShellStudio.

tmux and Python are external runtime prerequisites, not included in the binary.
The Go runtime and standard library use the Go project's BSD-style license.
The build's dependency inventory and notices accompany source distribution.
