// SPDX-License-Identifier: GPL-3.0-only
package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirectoryPriorityAndSpaces(t *testing.T) {
	root := t.TempDir()
	explicit := filepath.Join(root, "chosen space")
	view := filepath.Join(root, "view")
	os.Mkdir(explicit, 0700)
	os.Mkdir(view, 0700)
	for _, tc := range []struct{ e, v, w string }{{explicit, view, explicit}, {"", view, view}, {"", "", root}} {
		got, e := Directory(tc.e, tc.v, root)
		if e != nil || got != tc.w {
			t.Fatal(got, e)
		}
	}
	if _, e := Directory(filepath.Join(root, "missing"), view, root); e == nil {
		t.Fatal("silently fell back from invalid explicit path")
	}
}
func TestAtomicWriteAndLock(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if e := AtomicWrite(p, []byte("old")); e != nil {
		t.Fatal(e)
	}
	if e := AtomicWrite(p, []byte("new")); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(p)
	fi, _ := os.Stat(p)
	if string(b) != "new" || fi.Mode().Perm() != 0600 {
		t.Fatal("bad write")
	}
	os.Symlink(p, p+".link")
	if e := AtomicWrite(p+".link", []byte("bad")); e == nil {
		t.Fatal("symlink accepted")
	}
	if e := ReplaceFile(p+".link", []byte("bad")); e == nil {
		t.Fatal("preference writer accepted symlink")
	}
	if e := ReplaceFile(p, []byte("preference")); e != nil {
		t.Fatal(e)
	}
	if b, e := os.ReadFile(p); e != nil || string(b) != "preference" {
		t.Fatalf("preference replacement: %q, %v", b, e)
	}
	l, e := Lock(p + ".lock")
	if e != nil {
		t.Fatal(e)
	}
	defer Unlock(l)
	if _, e = Lock(p + ".lock"); e == nil {
		t.Fatal("second lock acquired")
	}
}
func TestXDGIsolatedAndPrivate(t *testing.T) {
	root, e := os.MkdirTemp("", "xdg-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(root)
	for i, k := range []string{"XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR"} {
		t.Setenv(k, filepath.Join(root, string(rune('a'+i))))
	}
	p, e := Discover()
	if e != nil {
		t.Fatal(e)
	}
	for _, d := range []string{p.Data, p.Config, p.State, p.Runtime} {
		fi, e := os.Stat(d)
		if e != nil || fi.Mode().Perm() != 0700 {
			t.Fatal(d, e)
		}
	}
	t.Setenv("XDG_DATA_HOME", "relative")
	if _, e = Discover(); e == nil {
		t.Fatal("relative XDG accepted")
	}
}
