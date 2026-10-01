// SPDX-License-Identifier: GPL-3.0-only
package extensions

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"shellstudio/internal/platform"
	"strings"
	"testing"
)

func TestCatalogWithoutProgramsAndInvalidManifest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	cs := Catalog(dir)
	if len(cs) != 5 {
		t.Fatal(len(cs))
	}
	for _, c := range cs {
		if c.Error != "" {
			t.Fatal(c.Error)
		}
	}
	os.Mkdir(filepath.Join(dir, "extensions"), 0700)
	os.WriteFile(filepath.Join(dir, "extensions", "bad.json"), []byte(`{"id":"terminal"}`), 0600)
	cs = Catalog(dir)
	if len(cs) != 6 {
		t.Fatal(len(cs))
	}
	valid := 0
	for _, c := range cs {
		if c.Error == "" {
			valid++
		}
	}
	if valid != 5 {
		t.Fatal("bad extension blocked catalog")
	}
}
func TestStrictManifest(t *testing.T) {
	b, _ := Assets.ReadFile("catalog/notes.json")
	for _, change := range []func(map[string]any){func(m map[string]any) { m["surprise"] = true }, func(m map[string]any) { m["schema_version"] = 2 }, func(m map[string]any) { m["id"] = "../unsafe" }, func(m map[string]any) { m["default_profile"] = "absent" }} {
		var m map[string]any
		json.Unmarshal(b, &m)
		change(m)
		bad, _ := json.Marshal(m)
		if _, e := Parse(bad); e == nil {
			t.Fatal("invalid manifest accepted")
		}
	}
}
func TestFailedDependencyDoesNotEnable(t *testing.T) {
	root := t.TempDir()
	p := platform.Paths{Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data")}
	platform.PrivateDir(p.Config)
	platform.PrivateDir(p.Data)
	man, _ := Parse(mustAsset(t, "catalog/agent-chat.json"))
	man.Dependencies = []string{"shellstudio-nonexistent-command"}
	m := Manager{Paths: p}
	if e := m.Install(man); e == nil {
		t.Fatal("missing dependency accepted")
	}
	if m.Enabled(man) {
		t.Fatal("failed installation enabled")
	}
	stage := filepath.Join(p.Data, "extensions", man.ID, ".install-interrupted")
	os.MkdirAll(stage, 0700)
	if m.Enabled(man) {
		t.Fatal("partial installation activated")
	}
}
func TestBundleWheelIsIsolatedAndLicensed(t *testing.T) {
	for _, id := range []string{"agent-chat", "agent-gantt"} {
		b, e := bundleWheel(id)
		if e != nil {
			t.Fatal(e)
		}
		z, e := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if e != nil {
			t.Fatal(e)
		}
		names := []string{}
		for _, f := range z.File {
			names = append(names, f.Name)
		}
		joined := strings.Join(names, ",")
		for _, n := range []string{"server.py", "LICENSE", "METADATA", "WHEEL", "RECORD"} {
			if !strings.Contains(joined, n) {
				t.Fatal(n, joined)
			}
		}
	}
}
func mustAsset(t *testing.T, p string) []byte {
	t.Helper()
	b, e := Assets.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
