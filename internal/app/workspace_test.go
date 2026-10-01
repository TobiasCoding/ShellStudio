// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"os"
	"path/filepath"
	"shellstudio/internal/platform"
	"shellstudio/internal/store"
	"testing"
)

func TestWorkspaceUsesFolderWithoutLaunchingOrDuplicating(t *testing.T) {
	root := t.TempDir()
	s, e := store.Open(filepath.Join(root, "data", "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	config := filepath.Join(root, "config")
	platform.PrivateDir(config)
	a := &App{CWD: root, Store: s, Paths: platform.Paths{Config: config}}
	if _, e = s.NewView("Unassigned", ""); e != nil {
		t.Fatal(e)
	}
	v, e := a.Workspace("")
	if e != nil || v.Folder != root {
		t.Fatal(v, e)
	}
	os.Symlink(root, filepath.Join(root, "alias"))
	again, e := a.Workspace("alias")
	if e != nil || again.ID != v.ID {
		t.Fatal(again, e)
	}
	other := filepath.Join(root, "folder with spaces")
	os.Mkdir(other, 0700)
	v2, e := a.Workspace(other)
	if e != nil || v2.ID == v.ID || a.CWD != other {
		t.Fatal(v2, e)
	}
	cs, e := s.Consoles("")
	if e != nil || len(cs) != 0 {
		t.Fatal("workspace launch started a program", e)
	}
	if _, e = a.Workspace(filepath.Join(root, "missing")); e == nil {
		t.Fatal("invalid folder accepted")
	}
}
