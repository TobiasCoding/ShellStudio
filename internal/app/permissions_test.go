// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"path/filepath"
	"reflect"
	"testing"

	"shellstudio/internal/extensions"
	"shellstudio/internal/platform"
)

func permissionApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	p := platform.Paths{Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data")}
	platform.PrivateDir(p.Config)
	e := platform.UpdateConfig(p.Config, func(c *platform.Config) error {
		c.Extensions["claude"] = platform.ExtensionConfig{Enabled: true, Default: "default"}
		c.Extensions["codex"] = platform.ExtensionConfig{Enabled: true, Default: "default"}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return &App{Paths: p, Extensions: extensions.Manager{Paths: p}}
}

func TestPermissionPresetsReachNewAgentConsoles(t *testing.T) {
	a := permissionApp(t)
	for _, agent := range PermissionAgents {
		p, e := a.profile(agent, "")
		if e != nil || len(p.Args) != 0 {
			t.Fatalf("%s default added flags: %v %v", agent, p.Args, e)
		}
	}
	cases := map[string][2]string{
		"claude": {"bypassPermissions", "--permission-mode bypassPermissions"},
		"codex":  {"workspace-write", "--sandbox workspace-write --ask-for-approval on-request"},
	}
	for agent, c := range cases {
		if e := a.SetPermission(agent, c[0]); e != nil {
			t.Fatal(e)
		}
		p, e := a.profile(agent, "")
		want, _ := FindPermission(agent, c[0])
		if e != nil || !reflect.DeepEqual(p.Args, want.Args) {
			t.Fatalf("%s: got %v %v, want %s", agent, p.Args, e, c[1])
		}
	}
	// Terminal and other kinds never receive agent flags.
	if p, e := a.profile("terminal", ""); e != nil || len(p.Args) != 1 {
		t.Fatal(p, e)
	}
	// Back to default removes the key instead of storing "default".
	if e := a.SetPermission("codex", "default"); e != nil {
		t.Fatal(e)
	}
	c, _ := platform.LoadConfig(a.Paths.Config)
	if _, ok := c.AgentPermissions["codex"]; ok || c.AgentPermissions["claude"] != "bypassPermissions" {
		t.Fatal(c.AgentPermissions)
	}
}

func TestPermissionSurvivesOtherConfigWrites(t *testing.T) {
	a := permissionApp(t)
	if e := a.SetPermission("claude", "acceptEdits"); e != nil {
		t.Fatal(e)
	}
	// Launch remembers folders and enabling rewrites the extension entry.
	if e := platform.Remember(a.Paths.Config, t.TempDir()); e != nil {
		t.Fatal(e)
	}
	if e := platform.UpdateConfig(a.Paths.Config, func(c *platform.Config) error { return nil }); e != nil {
		t.Fatal(e)
	}
	if p, e := a.Permission("claude"); e != nil || p.ID != "acceptEdits" {
		t.Fatal(p, e)
	}
}

func TestPermissionProfileFlagsWinAndInvalidValuesFail(t *testing.T) {
	a := permissionApp(t)
	if e := a.SetPermission("codex", "full-access"); e != nil {
		t.Fatal(e)
	}
	e := platform.UpdateConfig(a.Paths.Config, func(c *platform.Config) error {
		x := c.Extensions["codex"]
		x.Profiles = map[string]platform.Profile{"mine": {Executable: "codex", Args: []string{"-s", "read-only"}}}
		x.Default = "mine"
		c.Extensions["codex"] = x
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if p, e := a.profile("codex", ""); e != nil || !reflect.DeepEqual(p.Args, []string{"-s", "read-only"}) {
		t.Fatal("profile permission flags were overridden:", p.Args, e)
	}
	if e := a.SetPermission("claude", "yolo"); e == nil {
		t.Fatal("unknown mode accepted")
	}
	// A hand-edited unknown value blocks the launch instead of guessing.
	c, _ := platform.LoadConfig(a.Paths.Config)
	c.AgentPermissions["claude"] = "yolo"
	if e := platform.WriteJSON(filepath.Join(a.Paths.Config, "config.json"), c); e != nil {
		t.Fatal(e)
	}
	if _, e := a.profile("claude", ""); e == nil {
		t.Fatal("invalid stored mode launched")
	}
}
