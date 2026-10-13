// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"fmt"
	"strings"

	"shellstudio/internal/platform"
)

// Permission is a launch preset for an agent. Default adds no flag: the
// agent's own configuration decides, as before this setting existed.
type Permission struct {
	ID, Label, Detail string
	Args              []string
	Dangerous         bool
}

// PermissionAgents are the agents whose default permissions can be chosen.
var PermissionAgents = []string{"claude", "codex"}

// Only values advertised by the bundled CLIs.
var permissions = map[string][]Permission{
	"claude": {
		{"default", "Agent default", "No flag: Claude's own settings decide.", nil, false},
		{"plan", "Plan", "Read and plan; no edits or commands.", []string{"--permission-mode", "plan"}, false},
		{"acceptEdits", "Accept edits", "File edits are accepted; commands still ask.", []string{"--permission-mode", "acceptEdits"}, false},
		{"auto", "Auto", "Claude approves actions it judges safe.", []string{"--permission-mode", "auto"}, false},
		{"dontAsk", "Don't ask", "Anything not allowed in advance is denied.", []string{"--permission-mode", "dontAsk"}, false},
		{"bypassPermissions", "Bypass permissions", "Every action runs without asking.", []string{"--permission-mode", "bypassPermissions"}, true},
	},
	"codex": {
		{"default", "Agent default", "No flag: Codex's own settings decide.", nil, false},
		{"read-only", "Read only", "Read-only sandbox; asks before anything else.", []string{"--sandbox", "read-only", "--ask-for-approval", "on-request"}, false},
		{"workspace-write", "Workspace write", "Edits in the workspace; asks outside it.", []string{"--sandbox", "workspace-write", "--ask-for-approval", "on-request"}, false},
		{"workspace-never-ask", "Workspace, never ask", "Edits in the workspace; failures return to Codex.", []string{"--sandbox", "workspace-write", "--ask-for-approval", "never"}, false},
		{"full-access", "Full access", "No sandbox and no approvals.", []string{"--dangerously-bypass-approvals-and-sandbox"}, true},
	},
}

// Flags that a profile can already carry; the profile then wins.
var permissionFlags = map[string][]string{
	"claude": {"--permission-mode", "--dangerously-skip-permissions", "--allow-dangerously-skip-permissions"},
	"codex": {"--sandbox", "-s", "--ask-for-approval", "-a", "--full-auto", "--approve-for-me",
		"--dangerously-bypass-approvals-and-sandbox"},
}

func Permissions(agent string) []Permission { return permissions[agent] }
func AgentLabel(agent string) string        { return builtinKinds[agent][0] }

func FindPermission(agent, id string) (Permission, error) {
	if id == "" {
		id = "default"
	}
	for _, p := range permissions[agent] {
		if p.ID == id {
			return p, nil
		}
	}
	return Permission{}, fmt.Errorf("unknown %s permission mode %q", agent, id)
}

// Permission is the saved choice of an agent, or its default.
func (a *App) Permission(agent string) (Permission, error) {
	c, e := platform.LoadConfig(a.Paths.Config)
	if e != nil {
		return Permission{}, e
	}
	return FindPermission(agent, c.AgentPermissions[agent])
}

// SetPermission applies to consoles created afterwards; running ones and
// restarts keep the arguments they were created with.
func (a *App) SetPermission(agent, id string) error {
	p, e := FindPermission(agent, id)
	if e != nil {
		return e
	}
	return platform.UpdateConfig(a.Paths.Config, func(c *platform.Config) error {
		if c.AgentPermissions == nil {
			c.AgentPermissions = map[string]string{}
		}
		if p.ID == "default" {
			delete(c.AgentPermissions, agent)
		} else {
			c.AgentPermissions[agent] = p.ID
		}
		return nil
	})
}

// withPermission appends the agent's preset unless the profile already
// chooses its own permissions.
func (a *App) withPermission(kind string, p platform.Profile) (platform.Profile, error) {
	if _, ok := permissions[kind]; !ok {
		return p, nil
	}
	perm, e := a.Permission(kind)
	if e != nil || len(perm.Args) == 0 {
		return p, e
	}
	for _, arg := range p.Args {
		for _, f := range permissionFlags[kind] {
			if arg == f || strings.HasPrefix(arg, f+"=") {
				return p, nil
			}
		}
	}
	p.Args = append(append([]string{}, p.Args...), perm.Args...)
	return p, nil
}
