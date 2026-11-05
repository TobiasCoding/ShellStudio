// SPDX-License-Identifier: GPL-3.0-only
package tui

import (
	"strings"
	"testing"
	"time"

	"shellstudio/internal/app"
	"shellstudio/internal/platform"
	"shellstudio/internal/store"
)

func TestMinimizedRowsFilterAndSort(t *testing.T) {
	t0 := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	review := store.Tag{ID: "r", Name: "Por revisar", Position: 0}
	done := store.Tag{ID: "d", Name: "Listo", Position: 1}
	rows := []minRow{
		{Console: store.Console{ID: "a", Created: t0, Description: "Revisión de webhooks", TagID: "r", Folder: "/srv/api"}, Label: "Claude · Backend", Tag: review},
		{Console: store.Console{ID: "b", Created: t0.Add(time.Hour), TagID: "d"}, Label: "Codex · Docs", Tag: done},
		{Console: store.Console{ID: "c", Created: t0.Add(2 * time.Hour)}, Label: "Terminal · Build"},
	}
	ids := func(rs []minRow) (out string) {
		for _, r := range rs {
			out += r.ID
		}
		return out
	}
	if got := ids(minimizedRows(rows, "", "", "created", true)); got != "cba" {
		t.Fatal(got)
	}
	if got := ids(minimizedRows(rows, "", "", "console", false)); got != "abc" {
		t.Fatal(got)
	}
	if got := ids(minimizedRows(rows, "", "", "tag", false)); got != "abc" {
		t.Fatal("tagged first, by tag position:", got)
	}
	if got := ids(minimizedRows(rows, "webhooks claude", "", "created", true)); got != "a" {
		t.Fatal(got)
	}
	if got := ids(minimizedRows(rows, "api", "", "created", true)); got != "a" {
		t.Fatal("folder search", got)
	}
	if got := ids(minimizedRows(rows, "", "d", "created", true)); got != "b" {
		t.Fatal(got)
	}
	if got := ids(minimizedRows(rows, "listo", "", "created", true)); got != "b" {
		t.Fatal("tag name search", got)
	}
}

func TestMinimizedPrefsPersist(t *testing.T) {
	dir := t.TempDir()
	p := loadMinPrefs(dir)
	if strings.Join(p.Columns, ",") != "created,console,tag" || p.Sort != "created" || !p.Desc {
		t.Fatal(p)
	}
	for _, c := range []string{"col:status", "col:tag", "col:console", "sort:console", "sort:console", "filter:x"} {
		if e := UpdateMinPrefs(dir, c); e != nil {
			t.Fatal(e)
		}
	}
	p = loadMinPrefs(dir)
	if strings.Join(p.Columns, ",") != "created,console,status" || p.Sort != "console" || !p.Desc || p.Filter != "x" {
		t.Fatal(p)
	}
	c, _ := platform.LoadConfig(dir)
	if c.MinimizedSort != "console:desc" {
		t.Fatal(c.MinimizedSort)
	}
}

func minimizedApp(t *testing.T) (*app.App, string) {
	a := tagsApp(t)
	vs, _ := a.Store.Views()
	view := vs[0].ID
	for _, c := range []store.Console{
		{ID: "c2", Name: "Backend", Extension: "claude", Folder: "/", Argv: []string{"/bin/sh"}, Env: map[string]string{}},
		{ID: "c3", Name: "Docs", Extension: "codex", Folder: "/", Argv: []string{"/bin/sh"}, Env: map[string]string{}},
	} {
		if e := a.Store.AddConsole(view, c); e != nil {
			t.Fatal(e)
		}
	}
	tags, _ := a.Tags()
	a.Store.SetConsoleTag("c2", tags[0].ID)
	a.Store.SetDescription("c2", "Revisión de webhooks")
	for _, id := range []string{"c2", "c3"} {
		if e := a.Minimize(view, id); e != nil {
			t.Fatal(e)
		}
	}
	return a, view
}

func TestMinimizedPanelFrameSearchAndRestore(t *testing.T) {
	a, view := minimizedApp(t)
	s := newTestScreen(60, 20)
	runTest(t, s, func() { runMinimized(s, a, view, "%9") })
	f := frameText(s)
	for _, want := range []string{"MINIMIZED  2", "Columns ▼", "Search consoles", "Created ↓", "Console", "Tag",
		"Claude · Backend", "Revisión de webhooks", "Codex · Docs", tagNamesOf(t, a)[0], "[ Restore ]", "[ Rename ]", "Stopped"} {
		if !strings.Contains(f, want) {
			t.Fatalf("%q missing:\n%s", want, f)
		}
	}
	// The newest console comes first and is selected.
	y, _ := find(t, s, "Codex · Docs")
	if s.StyleAt(y, 0) != Highlight {
		t.Fatal("selection is not highlighted")
	}
	// Search, then restore the only match with Enter.
	s = newTestScreen(60, 20, events("/", typed("webhooks"), "enter")...)
	runTest(t, s, func() { runMinimized(s, a, view, "%9") })
	if strings.Contains(frameText(s), "Claude · Backend") {
		t.Fatalf("restored console still listed:\n%s", frameText(s))
	}
	vis, _ := a.Store.Consoles(view)
	if len(vis) != 2 || vis[1].ID != "c2" {
		t.Fatal("restored to its original place", vis)
	}
	// Double click restores; the panel is then empty.
	s = newTestScreen(60, 20)
	runTest(t, s, func() { runMinimized(s, a, view, "%9") })
	y, x := find(t, s, "Codex · Docs")
	s = newTestScreen(60, 20, events(clickAt(y, x), clickAt(y, x))...)
	runTest(t, s, func() { runMinimized(s, a, view, "%9") })
	if !strings.Contains(frameText(s), "No minimized consoles") || !strings.Contains(frameText(s), "MINIMIZED  0") {
		t.Fatalf("\n%s", frameText(s))
	}
}

func TestMinimizedPanelSortsByClickingHeaders(t *testing.T) {
	a, view := minimizedApp(t)
	s := newTestScreen(60, 20)
	runTest(t, s, func() { runMinimized(s, a, view, "%9") })
	y, x := find(t, s, "Console")
	s = newTestScreen(60, 20, events(clickAt(y, x))...)
	runTest(t, s, func() { runMinimized(s, a, view, "%9") })
	f := frameText(s)
	if !strings.Contains(f, "Console ↑") || strings.Index(f, "Claude · Backend") > strings.Index(f, "Codex · Docs") {
		t.Fatalf("\n%s", f)
	}
}

func tagNamesOf(t *testing.T, a *app.App) []string {
	tags, _ := a.Tags()
	var out []string
	for _, x := range tags {
		out = append(out, x.Name)
	}
	return out
}

func TestMinimizedPanelNarrowKeepsConsoleAndTag(t *testing.T) {
	a, view := minimizedApp(t)
	s := newTestScreen(26, 14)
	runTest(t, s, func() { runMinimized(s, a, view, "%9") })
	f := frameText(s)
	if strings.Contains(f, "Created") || !strings.Contains(f, "Console") || !strings.Contains(f, "Tag") {
		t.Fatalf("\n%s", f)
	}
	for _, row := range s.Frame() {
		if w := len([]rune(row)); w > 26 {
			t.Fatalf("row wider than the pane: %q", row)
		}
	}
}
