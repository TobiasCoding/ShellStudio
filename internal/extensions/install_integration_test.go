// SPDX-License-Identifier: GPL-3.0-only
package extensions

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"shellstudio/internal/platform"
	"testing"
)

func TestOfflinePythonInstallAndMCPProtocol(t *testing.T) {
	if os.Getenv("SHELLSTUDIO_INTEGRATION") != "1" {
		t.Skip("set SHELLSTUDIO_INTEGRATION=1 to exercise isolated Python installs")
	}
	for _, id := range []string{"agent-chat", "agent-gantt"} {
		t.Run(id, func(t *testing.T) {
			root := t.TempDir()
			paths := platform.Paths{Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data")}
			platform.PrivateDir(paths.Config)
			platform.PrivateDir(paths.Data)
			m := Manager{Paths: paths, Binary: "/bin/true"}
			man, e := Parse(mustAsset(t, "catalog/"+id+".json"))
			if e != nil {
				t.Fatal(e)
			}
			if e = m.Install(man); e != nil {
				t.Fatal(e)
			}
			if !m.Enabled(man) {
				t.Fatal("not enabled")
			}
			p := m.Expand(man.ID, man.MCP.Command)
			cmd := exec.Command(p.Executable, p.Args...)
			// A project-local server.py must not shadow the installed package.
			shadow := filepath.Join(root, "project")
			if e = os.Mkdir(shadow, 0700); e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(filepath.Join(shadow, "server.py"), []byte("raise RuntimeError('shadowed')\n"), 0600); e != nil {
				t.Fatal(e)
			}
			cmd.Dir = shadow
			cmd.Env = os.Environ()
			for k, v := range p.Env {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
			cmd.Stdin = bytes.NewBufferString("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2024-11-05\",\"capabilities\":{},\"clientInfo\":{\"name\":\"test\",\"version\":\"1\"}}}\n")
			out, e := cmd.CombinedOutput()
			if e != nil {
				t.Fatalf("%v %s", e, out)
			}
			var response map[string]any
			if e = json.Unmarshal(out, &response); e != nil {
				t.Fatalf("%v %s", e, out)
			}
			if response["result"] == nil {
				t.Fatal(string(out))
			}
			data := filepath.Join(paths.Data, "extension-data", id)
			keep := filepath.Join(data, "keep")
			os.WriteFile(keep, []byte("data"), 0600)
			if e = m.Uninstall(man, false); e != nil {
				t.Fatal(e)
			}
			if _, e = os.Stat(keep); e != nil {
				t.Fatal("uninstall deleted data")
			}
			if m.Enabled(man) {
				t.Fatal("still enabled")
			}
		})
	}
}
