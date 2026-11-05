// SPDX-License-Identifier: GPL-3.0-only
package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

// legacyDB creates a database at an old schema version, as earlier releases
// left it, so migrations are tested from real starting points.
func legacyDB(t *testing.T, path string, version int) *sql.DB {
	t.Helper()
	db, e := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(ON)")
	if e != nil {
		t.Fatal(e)
	}
	steps := []string{
		`CREATE TABLE views (id TEXT PRIMARY KEY,name TEXT NOT NULL,folder TEXT NOT NULL,layout TEXT NOT NULL DEFAULT 'tiled');
CREATE TABLE consoles (id TEXT PRIMARY KEY,name TEXT NOT NULL,extension TEXT NOT NULL,profile TEXT NOT NULL,folder TEXT NOT NULL,argv TEXT NOT NULL,env TEXT NOT NULL,created TEXT NOT NULL);
CREATE TABLE members (view_id TEXT REFERENCES views(id) ON DELETE CASCADE,console_id TEXT REFERENCES consoles(id) ON DELETE CASCADE,position INTEGER NOT NULL,PRIMARY KEY(view_id,console_id));
CREATE TABLE notes (id TEXT PRIMARY KEY,title TEXT NOT NULL,body TEXT NOT NULL,revision INTEGER NOT NULL DEFAULT 1,trashed INTEGER NOT NULL DEFAULT 0,updated TEXT NOT NULL);
CREATE TABLE events (id INTEGER PRIMARY KEY,at TEXT NOT NULL,kind TEXT NOT NULL,detail TEXT NOT NULL);
PRAGMA user_version=1;`,
		"ALTER TABLE views ADD COLUMN explorer INTEGER NOT NULL DEFAULT 0; PRAGMA user_version=2;",
		"ALTER TABLE views ADD COLUMN tree TEXT NOT NULL DEFAULT '{}'; UPDATE views SET explorer=1; PRAGMA user_version=3;",
	}
	for _, q := range steps[:version] {
		if _, e = db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	return db
}

func TestSchemaFourMigratesConsolesAndSeedsTags(t *testing.T) {
	p := filepath.Join(t.TempDir(), "db")
	db := legacyDB(t, p, 3)
	_, e := db.Exec(`INSERT INTO views(id,name,folder,layout,explorer,tree) VALUES('v','View','/','tiled',1,'{}');
INSERT INTO consoles VALUES('a','one','terminal','','/','["/bin/sh"]','{}','2026-01-02T03:04:05Z');
INSERT INTO consoles VALUES('b','two','terminal','','/','["/bin/sh"]','{}','2026-01-02T03:04:06Z');
INSERT INTO members VALUES('v','a',0),('v','b',1);`)
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	s, e := Open(p)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	cs, e := s.Consoles("v")
	if e != nil || len(cs) != 2 || cs[0].ID != "a" || cs[0].Created.IsZero() || cs[0].TagID != "" || cs[0].Description != "" {
		t.Fatal(e, cs)
	}
	v, e := s.View("v")
	if e != nil || !v.MinimizedPanel || v.ExplorerWidth != 0 || v.MinimizedHeight != 0 {
		t.Fatal(e, v)
	}
	tags, e := s.Tags()
	if e != nil || len(tags) != len(SeedTags) || tags[0].Name != SeedTags[0].Name || tags[3].Color != SeedTags[3].Color {
		t.Fatal(e, tags)
	}
}

func TestTagLifecycleLeavesConsolesUntagged(t *testing.T) {
	s := testStore(t)
	v, _ := s.NewView("View", "/")
	c := Console{ID: ID(), Name: "c", Extension: "terminal", Folder: "/", Argv: []string{"/bin/sh"}, Env: map[string]string{}}
	if e := s.AddConsole(v.ID, c); e != nil {
		t.Fatal(e)
	}
	tag, e := s.AddTag("Urgent", 167)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.AddTag("Urgent", 33); e == nil {
		t.Fatal("duplicate tag name accepted")
	}
	if e = s.SetConsoleTag(c.ID, tag.ID); e != nil {
		t.Fatal(e)
	}
	if n, _ := s.TagUses(tag.ID); n != 1 {
		t.Fatal(n)
	}
	tag.Name, tag.Color = "Later", 71
	if e = s.UpdateTag(tag); e != nil {
		t.Fatal(e)
	}
	if e = s.ReorderTags([]string{tag.ID}); e != nil {
		t.Fatal(e)
	}
	tags, _ := s.Tags()
	if tags[0].ID != tag.ID || tags[0].Name != "Later" || tags[0].Color != 71 {
		t.Fatal(tags)
	}
	if e = s.DeleteTag(tag.ID); e != nil {
		t.Fatal(e)
	}
	got, _ := s.Console(c.ID)
	if got.TagID != "" {
		t.Fatal("deleted tag still set", got.TagID)
	}
	if e = s.SetConsoleTag("missing", ""); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal(e)
	}
}

func ids(cs []Console) (out []string) {
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

func TestMinimizeKeepsPositionAcrossReorders(t *testing.T) {
	s := testStore(t)
	v, _ := s.NewView("View", "/")
	w, _ := s.NewView("Other", "/")
	for _, id := range []string{"a", "b", "c", "d"} {
		if e := s.AddConsole(v.ID, Console{ID: id, Name: id, Extension: "terminal", Folder: "/", Argv: []string{"/bin/sh"}, Env: map[string]string{}}); e != nil {
			t.Fatal(e)
		}
	}
	s.Link(w.ID, "b")
	if e := s.SetMinimized(v.ID, "b", true); e != nil {
		t.Fatal(e)
	}
	if got := ids(must(s.Consoles(v.ID))); len(got) != 3 || got[0] != "a" || got[1] != "c" {
		t.Fatal(got)
	}
	m := must(s.Minimized(v.ID))
	if len(m) != 1 || m[0].ID != "b" || !m[0].Minimized || m[0].MinimizedAt.IsZero() {
		t.Fatal(m)
	}
	// Minimized in one view, still visible in another.
	if got := ids(must(s.Consoles(w.ID))); len(got) != 1 || got[0] != "b" {
		t.Fatal(got)
	}
	// Reordering the visible members keeps b's place between a and c.
	if e := s.SetOrder(v.ID, []string{"d"}); e != nil {
		t.Fatal(e)
	}
	if got := ids(must(s.Consoles(v.ID))); got[0] != "d" || got[1] != "a" || got[2] != "c" {
		t.Fatal(got)
	}
	if e := s.Move(v.ID, "c", -1); e != nil {
		t.Fatal(e)
	}
	if e := s.SetMinimized(v.ID, "b", false); e != nil {
		t.Fatal(e)
	}
	if got := ids(must(s.Consoles(v.ID))); len(got) != 4 || got[0] != "d" || got[1] != "b" || got[2] != "c" || got[3] != "a" {
		t.Fatal(got)
	}
	if got := must(s.Members(v.ID)); len(got) != 4 {
		t.Fatal(got)
	}
	if e := s.SetMinimized(v.ID, "missing", true); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal(e)
	}
}

func TestReplaceKeepsTagDescriptionAndMinimized(t *testing.T) {
	s := testStore(t)
	v, _ := s.NewView("View", "/")
	old := Console{ID: "old", Name: "old", Extension: "terminal", Folder: "/", Argv: []string{"/bin/sh"}, Env: map[string]string{}}
	s.AddConsole(v.ID, old)
	tag, _ := s.AddTag("T", 33)
	s.SetConsoleTag("old", tag.ID)
	s.SetDescription("old", "builds the thing")
	s.SetMinimized(v.ID, "old", true)
	if e := s.Replace("old", Console{ID: "new", Name: "new", Extension: "terminal", Folder: "/", Argv: []string{"/bin/sh"}, Env: map[string]string{}}); e != nil {
		t.Fatal(e)
	}
	m := must(s.Minimized(v.ID))
	if len(m) != 1 || m[0].ID != "new" || m[0].TagID != tag.ID || m[0].Description != "builds the thing" {
		t.Fatal(m)
	}
}

func TestPanelSizesAndVisibility(t *testing.T) {
	s := testStore(t)
	v, _ := s.NewView("View", "/")
	if e := s.SetPanelSizes(v.ID, 40, 12); e != nil {
		t.Fatal(e)
	}
	if e := s.SetPanelSizes(v.ID, -1, 9); e != nil {
		t.Fatal(e)
	}
	v.MinimizedPanel = false
	s.UpdateView(v)
	got, _ := s.View(v.ID)
	if got.ExplorerWidth != 40 || got.MinimizedHeight != 9 || got.MinimizedPanel {
		t.Fatal(got)
	}
}

func must[T any](v T, e error) T {
	if e != nil {
		panic(e)
	}
	return v
}
