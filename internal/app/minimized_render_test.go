// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"path/filepath"
	"strings"
	"testing"

	"shellstudio/internal/mux"
	"shellstudio/internal/platform"
	"shellstudio/internal/store"
)

func TestLeftColumnSizes(t *testing.T) {
	v := store.View{Explorer: true, MinimizedPanel: true}
	if e, m := leftColumn(v, 69); e || m {
		t.Fatal("narrow windows hide the left column")
	}
	if e, m := leftColumn(store.View{MinimizedPanel: true}, 120); e || !m {
		t.Fatal("Minimized alone")
	}
	if LeftWidth(v, 160) != 48 || LeftWidth(v, 90) != 30 || LeftWidth(store.View{Explorer: true}, 160) != 32 || LeftWidth(store.View{ExplorerWidth: 50}, 160) != 50 ||
		LeftWidth(store.View{ExplorerWidth: 90}, 160) != 80 || LeftWidth(store.View{ExplorerWidth: 5}, 160) != 20 {
		t.Fatal("left width")
	}
	if h := MinimizedHeight(v, 44); h != 19 {
		t.Fatal(h)
	}
	if h := MinimizedHeight(store.View{MinimizedHeight: 40}, 44); h != 37 {
		t.Fatal("keeps rows for the explorer", h)
	}
	if h := MinimizedHeight(store.View{MinimizedHeight: 2}, 44); h != 8 {
		t.Fatal("at least 8 rows", h)
	}
}

// A view whose explorer, Minimized panel and consoles are in place is left
// alone: the panel is not taken for an empty pane and killed.
func TestRenderKeepsTheMinimizedPanel(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v, _ := s.NewView("Main", t.TempDir())
	c := store.Console{ID: "c1", Name: "one", Extension: "terminal", Folder: "/", Argv: []string{"/bin/sh"}, Env: map[string]string{}}
	s.AddConsole(v.ID, c)
	var calls []string
	m := &mux.Mux{Paths: platform.Paths{Runtime: t.TempDir()}, Version: "v1"}
	m.Run = func(views bool, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		switch {
		case args[0] == "list-panes" && views:
			return "%1\texplorer\texplorer\t0\t0\t1\t@1\tv1\tFiles\t\n" +
				"%2\tminimized\tminimized\t0\t0\t1\t@1\tv1\tMinimized\t\n" +
				"%3\tconsole\tc1\t0\t1\t1\t@1\tv1\tone\tstopped\n", nil
		case args[0] == "display-message" && strings.HasSuffix(args[len(args)-1], "#{window_height}"):
			return "%3\t160\t44\n", nil
		case args[0] == "display-message":
			return "160\ttiled\n", nil
		}
		return "", nil
	}
	a := &App{Store: s, Mux: m}
	if err := a.render(v.ID); err != nil {
		t.Fatal(err)
	}
	for _, c := range calls {
		if strings.HasPrefix(c, "kill-pane") || strings.HasPrefix(c, "break-pane") {
			t.Fatal("rebuilt a complete view:", calls)
		}
	}
	// Hiding the panel kills it; it is no console, so no program ends.
	v.MinimizedPanel = false
	s.UpdateView(v)
	calls = nil
	if err := a.render(v.ID); err != nil {
		t.Fatal(err)
	}
	killed := false
	for _, c := range calls {
		if c == "kill-pane -t %2" {
			killed = true
		}
		if c == "kill-pane -t %3" {
			t.Fatal("killed the console pane", calls)
		}
	}
	if !killed {
		t.Fatal("hidden panel kept:", calls)
	}
}
