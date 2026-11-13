// SPDX-License-Identifier: GPL-3.0-only
package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"shellstudio/internal/app"
	"shellstudio/internal/i18n"
	"shellstudio/internal/platform"
	"shellstudio/internal/store"
	"shellstudio/internal/theme"
)

// testCatalog builds an index like fileIndex: a trailing slash is a folder.
func testCatalog(paths ...string) []indexed {
	var out []indexed
	for _, p := range paths {
		dir := strings.HasSuffix(p, "/")
		p = strings.TrimSuffix(p, "/")
		out = append(out, indexed{p, strings.ToLower(p), dir})
	}
	return out
}

func rels(rows []row) []string {
	var out []string
	for _, r := range rows {
		out = append(out, fmt.Sprintf("%d:%s", r.depth, r.rel))
	}
	return out
}

var sampleCatalog = testCatalog("cmd/", "cmd/shellstudio/", "cmd/shellstudio/main.go",
	"docs/", "docs/explorer.md", "internal/", "internal/tui/", "internal/tui/explorer.go",
	"internal/tui/screen.go", "README.md", "main.go")

func TestFilterTreeShowsMatchesWithAncestors(t *testing.T) {
	f := filterTree(sampleCatalog, "explorer")
	want := []string{"0:.", "1:docs", "2:docs/explorer.md", "1:internal", "2:internal/tui", "3:internal/tui/explorer.go"}
	if got := rels(f.rows); !reflect.DeepEqual(got, want) {
		t.Fatalf("rows %v", got)
	}
	if !reflect.DeepEqual(f.matched, map[string]bool{"docs/explorer.md": true, "internal/tui/explorer.go": true}) {
		t.Fatalf("matched %v", f.matched)
	}
	if f.best != "docs/explorer.md" || f.truncated {
		t.Fatalf("best %q truncated %v", f.best, f.truncated)
	}
	for _, r := range f.rows {
		if f.matched[r.rel] == false && !r.dir {
			t.Fatalf("ancestor %q is not a folder", r.rel)
		}
	}
}

func TestFilterTreeOrdersFoldersFirstAndMatchesThePath(t *testing.T) {
	// Every word must be in the path: "tui" only appears in the folder.
	f := filterTree(sampleCatalog, "tui go")
	want := []string{"0:.", "1:internal", "2:internal/tui", "3:internal/tui/explorer.go", "3:internal/tui/screen.go"}
	if got := rels(f.rows); !reflect.DeepEqual(got, want) {
		t.Fatalf("rows %v", got)
	}
	if f.matched["internal/tui"] || !f.matched["internal/tui/screen.go"] {
		t.Fatalf("matched %v", f.matched)
	}
	f = filterTree(sampleCatalog, "MAIN")
	want = []string{"0:.", "1:cmd", "2:cmd/shellstudio", "3:cmd/shellstudio/main.go", "1:main.go"}
	if got := rels(f.rows); !reflect.DeepEqual(got, want) || f.best != "main.go" {
		t.Fatalf("rows %v best %q", got, f.best)
	}
	if f := filterTree(sampleCatalog, "nothing-like-this"); len(f.rows) != 0 || len(f.terms) == 0 {
		t.Fatalf("no match %+v", f)
	}
}

func TestFilterTreeCapsResults(t *testing.T) {
	var paths []string
	for i := 0; i < searchLimit+100; i++ {
		paths = append(paths, fmt.Sprintf("logs/f%04d.txt", i))
	}
	f := filterTree(testCatalog(paths...), "txt")
	if !f.truncated || len(f.matched) != searchLimit || len(f.rows) != searchLimit+2 {
		t.Fatalf("truncated %v matched %d rows %d", f.truncated, len(f.matched), len(f.rows))
	}
	f = filterTree(testCatalog(paths[:searchLimit]...), "txt")
	if f.truncated || len(f.matched) != searchLimit {
		t.Fatalf("exactly the limit: truncated %v matched %d", f.truncated, len(f.matched))
	}
}

func TestFilterTreeEmptyQueryIsNoFilter(t *testing.T) {
	for _, q := range []string{"", "   "} {
		if f := filterTree(sampleCatalog, q); f.rows != nil || f.terms != nil || f.truncated {
			t.Fatalf("%q: %+v", q, f)
		}
	}
}

func TestMatchSpans(t *testing.T) {
	mask := func(m []bool) string {
		var b strings.Builder
		for _, x := range m {
			if x {
				b.WriteByte('^')
			} else {
				b.WriteByte('.')
			}
		}
		return b.String()
	}
	if got := mask(matchSpans("ExplorerTest.go", []string{"test", "exp"})); got != "^^^.....^^^^..." {
		t.Fatal(got)
	}
	if got := mask(matchSpans("Ñandú.txt", []string{"ñ", "ú"})); got != "^...^...." {
		t.Fatal(got)
	}
}

// explorerFixture makes a view on a new folder holding files. Opening a store
// takes seconds, so the frame tests share one.
type explorerFixture struct {
	t *testing.T
	a *app.App
}

func newExplorerFixture(t *testing.T) *explorerFixture {
	t.Helper()
	t.Setenv("TMUX_PANE", "")
	dir := t.TempDir()
	st, e := store.Open(filepath.Join(dir, "db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.Close() })
	return &explorerFixture{t, &app.App{Store: st, CWD: dir, Paths: platform.Paths{State: dir}}}
}

func (x *explorerFixture) view(t *testing.T, files ...string) (*app.App, string, string) {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v, e := x.a.Store.NewView("Main", root)
	if e != nil {
		t.Fatal(e)
	}
	return x.a, v.ID, root
}

func runExplorer(t *testing.T, a *app.App, view, root string, s *Screen) {
	t.Helper()
	v, e := a.Store.View(view)
	if e != nil {
		t.Fatal(e)
	}
	runTest(t, s, func() { explore(a, view, root, v.Tree, s, nil) })
}

func rowOf(t *testing.T, s *Screen, text string) (int, int) {
	t.Helper()
	for y, l := range s.Frame() {
		if i := strings.Index(l, text); i >= 0 {
			return y, len([]rune(l[:i]))
		}
	}
	t.Fatalf("%q not in frame:\n%s", text, frameText(s))
	return 0, 0
}

var explorerFiles = []string{"internal/tui/explorer.go", "internal/app/app.go", "docs/guide.md", "README.md"}

func TestExplorerFrames(t *testing.T) {
	x := newExplorerFixture(t)
	t.Run("FiltersTree", func(t *testing.T) { testExplorerFiltersTree(t, x) })
	t.Run("NoMatchEscAndEnter", func(t *testing.T) { testExplorerNoMatchEscAndEnter(t, x) })
	t.Run("Scrollbar", func(t *testing.T) { testExplorerScrollbar(t, x) })
}

func testExplorerFiltersTree(t *testing.T, fx *explorerFixture) {
	a, view, root := fx.view(t, explorerFiles...)
	s := newTestScreen(40, 24, key("/"), key("e"), key("x"), key("p"))
	runExplorer(t, a, view, root, s)
	f := s.Frame()
	if !strings.HasPrefix(f[0], " "+i18n.T("explorer.title")) || !strings.HasSuffix(strings.TrimRight(f[0], " "), theme.Glyphs.Menu) || s.StyleAt(0, 30) != HeaderBar {
		t.Fatalf("header %q", f[0])
	}
	if !strings.Contains(f[1], "exp") || s.StyleAt(1, 1) != FieldStyle {
		t.Fatalf("field %q", f[1])
	}
	text := frameText(s)
	for _, gone := range []string{"README.md", "docs", "app.go"} {
		if strings.Contains(text, gone) {
			t.Fatalf("%q is not a match:\n%s", gone, text)
		}
	}
	y, x := rowOf(t, s, "▼ internal")
	if y != 3 {
		t.Fatalf("internal at row %d:\n%s", y, text)
	}
	if y, _ = rowOf(t, s, "▼ tui"); y != 4 {
		t.Fatalf("tui at row %d:\n%s", y, text)
	}
	y, x = rowOf(t, s, "explorer.go")
	if y != 5 {
		t.Fatalf("explorer.go at row %d:\n%s", y, text)
	}
	// The match is the selected row: its letters stand out on the blue bar.
	if s.StyleAt(y, x) != matchSelectedStyle || s.StyleAt(y, x+3) != Highlight || s.StyleAt(y, 1) != Highlight {
		t.Fatalf("styles %v %v %v", s.StyleAt(y, x), s.StyleAt(y, x+3), s.StyleAt(y, 1))
	}
	if s.StyleAt(2, 1) != Normal {
		t.Fatalf("the root is not selected: %v", s.StyleAt(2, 1))
	}
}

func testExplorerNoMatchEscAndEnter(t *testing.T, x *explorerFixture) {
	a, view, root := x.view(t, explorerFiles...)
	s := newTestScreen(40, 24, key("/"), key("z"), key("q"))
	runExplorer(t, a, view, root, s)
	if y, _ := rowOf(t, s, i18n.T("explorer.no_match")); y != 2 {
		t.Fatalf("no match at %d", y)
	}

	// Esc gives back the tree as it was, with its selection.
	s = newTestScreen(40, 24, key("down"), key("/"), key("g"), key("u"), key("esc"))
	runExplorer(t, a, view, root, s)
	text := frameText(s)
	if !strings.Contains(text, "README.md") || strings.Contains(text, "guide.md") {
		t.Fatalf("tree not restored:\n%s", text)
	}
	if y, _ := rowOf(t, s, "▶ docs"); s.StyleAt(y, 1) != Highlight {
		t.Fatalf("selection not restored:\n%s", text)
	}
	if !strings.Contains(s.Frame()[1], i18n.T("explorer.search")) {
		t.Fatalf("field not cleared %q", s.Frame()[1])
	}

	// Enter reveals the result in the tree and saves it.
	s = newTestScreen(40, 24, key("/"), key("e"), key("x"), key("p"), key("enter"))
	runExplorer(t, a, view, root, s)
	text = frameText(s)
	y, _ := rowOf(t, s, "explorer.go")
	if s.StyleAt(y, 1) != Highlight || !strings.Contains(text, "▶ app") || !strings.Contains(text, "README.md") {
		t.Fatalf("not revealed:\n%s", text)
	}
	v, _ := a.Store.View(view)
	var st treeState
	json.Unmarshal([]byte(v.Tree), &st)
	if st.Selected != "internal/tui/explorer.go" || !reflect.DeepEqual(st.Expanded, []string{".", "internal", "internal/tui"}) {
		t.Fatalf("saved %s", v.Tree)
	}
}

func testExplorerScrollbar(t *testing.T, x *explorerFixture) {
	var files []string
	for i := 0; i < 40; i++ {
		files = append(files, fmt.Sprintf("f%02d.txt", i))
	}
	a, view, root := x.view(t, files...)
	const w, h = 30, 20
	count := h - 2
	barX := w - 2
	// A click at the bottom of the bar shows the end; the release does not
	// touch the tree and the selection (the root) stays where it was.
	s := newTestScreen(w, h, click(2+count-1, barX), release(2+count-1, barX))
	runExplorer(t, a, view, root, s)
	f := s.Frame()
	if !strings.Contains(f[2+count-1], "f39.txt") || strings.Contains(strings.Join(f[2:], "\n"), "▼ ") {
		t.Fatalf("not scrolled to the end:\n%s", frameText(s))
	}
	if r := []rune(f[2+count-1]); string(r[barX]) != "█" || string([]rune(f[2])[barX]) != "│" {
		t.Fatalf("bar %q", f[2+count-1])
	}
	// Dragging the thumb back to the top.
	drag := Event{Kind: MouseEvent, Y: 2, X: barX, Press: true, Motion: true}
	s = newTestScreen(w, h, click(2+count-1, barX), Event{Kind: TimeoutEvent}, drag, release(2, barX))
	runExplorer(t, a, view, root, s)
	if !strings.Contains(s.Frame()[2], "▼ ") || s.StyleAt(2, 1) != Highlight {
		t.Fatalf("not dragged to the top:\n%s", frameText(s))
	}
	// Without overflow there is no bar.
	a, view, root = x.view(t, "one.txt")
	s = newTestScreen(w, h)
	runExplorer(t, a, view, root, s)
	if strings.ContainsAny(frameText(s), "█│") {
		t.Fatalf("bar without overflow:\n%s", frameText(s))
	}
}
