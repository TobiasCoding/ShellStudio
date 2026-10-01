// SPDX-License-Identifier: GPL-3.0-only
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, e := Open(filepath.Join(t.TempDir(), "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func TestNotesConflictAndTrash(t *testing.T) {
	s := testStore(t)
	n, e := s.NewNote("Shared")
	if e != nil {
		t.Fatal(e)
	}
	a, b := n, n
	a.Body = "first editor"
	b.Body = "second editor"
	a, c, e := s.SaveNote(a)
	if e != nil || c {
		t.Fatalf("first: %v %v", c, e)
	}
	b, c, e = s.SaveNote(b)
	if e != nil || !c || a.ID == b.ID {
		t.Fatalf("conflict: %v %v", c, e)
	}
	ns, e := s.Notes("editor", false)
	if e != nil || len(ns) != 2 {
		t.Fatalf("notes: %v %v", ns, e)
	}
	if e = s.Trash(a.ID, true); e != nil {
		t.Fatal(e)
	}
	a.Body = "after trash"
	n, c, e = s.SaveNote(a)
	if e != nil || !c || n.ID == a.ID {
		t.Fatal("trash overwrite", e)
	}
	trashed, e := s.Notes("", true)
	if e != nil || len(trashed) != 1 {
		t.Fatal(e, trashed)
	}
	if e = s.Trash(a.ID, false); e != nil {
		t.Fatal(e)
	}
}
func TestConcurrentConnectionsPreserveBoth(t *testing.T) {
	p := filepath.Join(t.TempDir(), "db")
	a, e := Open(p)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	b, e := Open(p)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	n, e := a.NewNote("Concurrent")
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i, s := range []*Store{a, b} {
		wg.Add(1)
		go func(i int, s *Store) {
			defer wg.Done()
			copy := n
			copy.Body = fmt.Sprint(i)
			_, _, e := s.SaveNote(copy)
			errs <- e
		}(i, s)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	ns, e := a.Notes("", false)
	if e != nil || len(ns) != 2 {
		t.Fatal(e, len(ns))
	}
}
func TestDiskFullDoesNotClaimSaved(t *testing.T) {
	s := testStore(t)
	n, e := s.NewNote("Disk full")
	if e != nil {
		t.Fatal(e)
	}
	var pages int
	if e = s.DB.QueryRow("PRAGMA page_count").Scan(&pages); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec(fmt.Sprintf("PRAGMA max_page_count=%d", pages)); e != nil {
		t.Fatal(e)
	}
	n.Body = strings.Repeat("x", 2<<20)
	_, _, e = s.SaveNote(n)
	if e == nil {
		t.Fatal("expected SQLITE_FULL")
	}
	old, e := s.Note(n.ID)
	if e != nil || old.Body != "" || old.Revision != 1 {
		t.Fatal("failed transaction changed note", e)
	}
	if e = s.Check(); e != nil {
		t.Fatal(e)
	}
}
func TestBackupRecoveryAndMigrations(t *testing.T) {
	s := testStore(t)
	n, e := s.NewNote("Persist")
	if e != nil {
		t.Fatal(e)
	}
	n.Body = "confirmed"
	n, _, e = s.SaveNote(n)
	if e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(t.TempDir(), "backup.db")
	if e = s.Backup(p); e != nil {
		t.Fatal(e)
	}
	if e = s.Backup(p); e == nil {
		t.Fatal("backup overwrote an existing file")
	}
	b, e := Open(p)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	got, e := b.Note(n.ID)
	if e != nil || got.Body != n.Body {
		t.Fatal(e, got)
	}
	var v int
	b.DB.QueryRow("PRAGMA user_version").Scan(&v)
	if v != 2 {
		t.Fatal(v)
	}
	b.DB.Exec("PRAGMA user_version=999")
	b.Close()
	if _, e = Open(p); e == nil {
		t.Fatal("opened future schema")
	}
}
func TestMembershipAndEventBound(t *testing.T) {
	s := testStore(t)
	v, e := s.NewView("View", "/a path")
	if e != nil {
		t.Fatal(e)
	}
	w, _ := s.NewView("Other", "/")
	c := Console{ID: ID(), Name: "test", Extension: "terminal", Folder: "/a path", Argv: []string{"/bin/sh", "literal; echo unsafe"}, Env: map[string]string{"X": "$(no)"}}
	if e = s.AddConsole(v.ID, c); e != nil {
		t.Fatal(e)
	}
	if e = s.Link(w.ID, c.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.Unlink(v.ID, c.ID); e != nil {
		t.Fatal(e)
	}
	cs, e := s.Consoles(w.ID)
	if e != nil || len(cs) != 1 || cs[0].Argv[1] != c.Argv[1] {
		t.Fatal(e, cs)
	}
	tx, e := s.DB.Begin()
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 1005; i++ {
		if _, e = tx.Exec("INSERT INTO events(at,kind,detail) VALUES('now','test','value')"); e != nil {
			t.Fatal(e)
		}
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if e = s.Event("test", "new"); e != nil {
		t.Fatal(e)
	}
	var count int
	s.DB.QueryRow("SELECT count(*) FROM events").Scan(&count)
	if count != 1000 {
		t.Fatal(count)
	}
}
func TestRejectSymlinkDatabase(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	os.WriteFile(target, []byte("keep"), 0600)
	os.Symlink(target, filepath.Join(dir, "db"))
	if _, e := Open(filepath.Join(dir, "db")); e == nil {
		t.Fatal("accepted symlink")
	}
	b, _ := os.ReadFile(target)
	if string(b) != "keep" {
		t.Fatal("modified target")
	}
}
