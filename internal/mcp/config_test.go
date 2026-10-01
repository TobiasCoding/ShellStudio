// SPDX-License-Identifier: GPL-3.0-only
package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"shellstudio/internal/platform"
	"strings"
	"testing"
)

func TestPreserveClientsAndBackups(t *testing.T) {
	for _, client := range []string{"claude", "codex"} {
		t.Run(client, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config")
			old := `{"unknown":{"token":"opaque"},"mcpServers":{"foreign":{"command":"keep"}}}`
			if client == "codex" {
				old = "# keep comment\nmodel = 'example'\n[mcp_servers.foreign]\ncommand = 'keep'\n"
			}
			os.WriteFile(p, []byte(old), 0600)
			profile := platform.Profile{Executable: "/a path/python", Args: []string{"-m", "server", "chat"}, Env: map[string]string{"EXAMPLE": "value"}}
			plan, e := Prepare(client, p, "agent-chat", profile)
			if e != nil {
				t.Fatal(e)
			}
			backup, e := Apply(plan)
			if e != nil {
				t.Fatal(e)
			}
			b, _ := os.ReadFile(backup)
			if string(b) != old {
				t.Fatal("backup mismatch")
			}
			b, _ = os.ReadFile(p)
			if !strings.Contains(string(b), "foreign") {
				t.Fatal("foreign entry lost")
			}
			if client == "codex" && !strings.Contains(string(b), "# keep comment") {
				t.Fatal("comment lost")
			}
			if client == "claude" {
				var data map[string]any
				json.Unmarshal(b, &data)
				if data["unknown"] == nil {
					t.Fatal("unknown entry lost")
				}
			}
			if _, e = Prepare(client, p, "agent-chat", profile); e != nil {
				t.Fatal("not idempotent", e)
			}
			profile.Executable = "changed"
			if _, e = Prepare(client, p, "agent-chat", profile); e == nil {
				t.Fatal("conflict not rejected")
			}
		})
	}
}
func TestRejectStalePreview(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte("{}"), 0600)
	plan, e := Prepare("claude", p, "chat", platform.Profile{Executable: "python", Args: []string{}})
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(p, []byte(`{"foreign":true}`), 0600)
	if _, e = Apply(plan); e == nil {
		t.Fatal("overwrote concurrent writer")
	}
}
