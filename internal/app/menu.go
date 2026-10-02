// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
	"shellstudio/internal/mux"
	"shellstudio/internal/store"
)

type MenuItem struct{ Key, Label, Action string }
type MenuColumn struct {
	Title string
	Items []MenuItem
}

// Shortcut lines of each list; they also give the exact size of its popup.
var (
	ArrangeHelp = []string{"Space  Mark / unmark", "A  Mark all", "0  Unmark all", "S  Mark only the active one",
		"1  Columns", "2  Rows", "3  Grid", "4  Active on the left", "5  Active on top", "Q / Esc  Back",
		"Partial selection: creates another view."}
	BrowseHelp = []string{"Enter  Open", "E  Consoles of the view", "N  New view", "Del  Delete view", "Q / Esc  Back"}
	NewHelp    = []string{"Enter  Create", "Q / Esc  Cancel"}
	AddHelp    = []string{"Enter  Add", "Q / Esc  Back"}
)

// Rows of the view menu above and below the categories.
const MenuTop, MenuBottom = 3, 2

func (a *App) ConsolesHelp() []string {
	var out []string
	for _, k := range a.Kinds() {
		if k.Key != "" {
			out = append(out, k.Key+"  Create "+k.Label)
		}
	}
	return append(out, "A  Add existing console", "-  Remove from this view", "N  Rename",
		"R  Kill and replace here", "M/Del  Kill console", "Enter  Go to the selected console", "Q / Esc  Back")
}

func (a *App) MenuColumns(v store.View) []MenuColumn {
	explorer := "no"
	if v.Explorer {
		explorer = "yes"
	}
	var consoles []MenuItem
	for _, k := range a.Kinds() {
		key := k.Key
		if key == "" {
			key = " "
		}
		consoles = append(consoles, MenuItem{key, "New " + k.Label, "kind:" + k.ID})
	}
	consoles = append(consoles, MenuItem{"A", "Add existing…", "add"}, MenuItem{"S", "Consoles in this view…", "consoles"})
	var layouts []MenuItem
	for _, l := range mux.LayoutKeys {
		layouts = append(layouts, MenuItem{l.Key, l.Label, "layout:" + l.Layout})
	}
	layouts = append(layouts, MenuItem{"L", "Arrange marked…", "arrange"})
	return []MenuColumn{
		{"CONSOLES", consoles},
		{"LAYOUT", layouts},
		{"VIEWS", []MenuItem{{"V", "New view…", "new-view"}, {"B", "Other views…", "views"}}},
		{"MORE", []MenuItem{{"E", "Explorer: " + explorer, "explorer"}, {"P", "Forward ports…", "ports"},
			{"D", "Leave without stopping (F10)", "leave"}, {"Q", "Back (Esc)", "back"}}},
	}
}

type Cell struct {
	Y, X         int
	Text, Action string
}

func Width(s string) int { return runewidth.StringWidth(s) }

// MenuLayout puts the categories in columns when they fit the width, otherwise
// one below another. It returns the cells (an empty action is a title) and the
// size they take.
func MenuLayout(columns []MenuColumn, width int) ([]Cell, int, int) {
	widths := make([]int, len(columns))
	total := 0
	for i, c := range columns {
		widths[i] = Width(c.Title)
		for _, it := range c.Items {
			widths[i] = max(widths[i], Width(it.Key+"  "+it.Label))
		}
		total += widths[i]
	}
	var cells []Cell
	if total+3*(len(widths)-1)+2 <= width {
		x, tall := 1, 0
		for i, c := range columns {
			cells = append(cells, Cell{0, x, c.Title, ""})
			for row, it := range c.Items {
				cells = append(cells, Cell{row + 1, x, it.Key + "  " + it.Label, it.Action})
			}
			x += widths[i] + 3
			tall = max(tall, len(c.Items))
		}
		return cells, x - 2, 1 + tall
	}
	y, wide := 0, 0
	for i, c := range columns {
		cells = append(cells, Cell{y, 1, c.Title, ""})
		for row, it := range c.Items {
			cells = append(cells, Cell{y + 1 + row, 1, it.Key + "  " + it.Label, it.Action})
		}
		y += len(c.Items) + 2
		wide = max(wide, widths[i])
	}
	return cells, wide + 2, y - 1
}

// MenuSizes: popup of the view menu, width and height in columns, and the
// height when categories are stacked.
func (a *App) MenuSizes() (int, int, int) {
	cols := a.MenuColumns(store.View{Explorer: true})
	_, w, h := MenuLayout(cols, 1<<20)
	_, _, stacked := MenuLayout(cols, 0)
	extra := MenuTop + MenuBottom + 2
	return w + 2, h + extra, stacked + extra
}

// ChooseSize is the width and height (with border) of a list popup.
func ChooseSize(title string, items, help []string) (int, int) {
	lines := append([]string{title, "000/000 | ↑↓ choose"}, help...)
	for _, it := range items {
		lines = append(lines, "> "+it)
	}
	w := 0
	for _, l := range lines {
		w = max(w, Width(l))
	}
	// 40 columns at least: the notice of an action fits in one row.
	return max(40, w+2) + 2, max(1, len(items)) + len(help) + 7
}

func (a *App) kindLabels() []string {
	var out []string
	for _, k := range a.Kinds() {
		out = append(out, k.Label)
	}
	return out
}

// DialogSizes are the sizes compiled into the shortcuts of the views server.
func (a *App) DialogSizes() mux.Sizes {
	var s mux.Sizes
	s.MenuW, s.MenuH, s.MenuStacked = a.MenuSizes()
	s.NewW, s.NewH = ChooseSize("NEW CONSOLE IN "+strings.Repeat("x", 24), a.kindLabels(), NewHelp)
	s.ArrangeW, s.ArrangeRows = ChooseSize("ARRANGE "+strings.Repeat("x", 24)+" | 00/00 marked", []string{"[x] " + strings.Repeat("x", 30)}, ArrangeHelp)
	s.BrowseW, s.BrowseRows = ChooseSize("SAVED VIEWS", []string{strings.Repeat("x", 24) + " | 00 consoles | Active on the left"}, BrowseHelp)
	return s
}

func PortsSize(count int) (int, int) { return 112, count + 9 + 5 + 2 }

// DialogSize measures one dialog with its current content.
func (a *App) DialogSize(action, view string) (int, int) {
	v, _ := a.Store.View(view)
	members, _ := a.Store.Consoles(view)
	switch action {
	case "menu":
		w, h, _ := a.MenuSizes()
		return w, h
	case "new", "replace":
		return ChooseSize("NEW CONSOLE IN "+v.Name, a.kindLabels(), NewHelp)
	case "kill":
		return 64, 12
	case "consoles":
		programs, _ := a.Mux.Programs()
		var items []string
		for _, c := range members {
			items = append(items, c.Name+" | "+Status(programs, c.ID))
		}
		return ChooseSize("CONSOLES OF "+v.Name, items, a.ConsolesHelp())
	case "add":
		items, _ := a.Addable(view)
		var labels []string
		for _, c := range items {
			labels = append(labels, c.Name+" | running")
		}
		return ChooseSize("ADD CONSOLE", labels, AddHelp)
	case "arrange":
		var items []string
		for _, c := range members {
			items = append(items, "[x] "+c.Name)
		}
		return ChooseSize("ARRANGE "+v.Name+" | 00/00 marked", items, ArrangeHelp)
	case "views":
		vs, _ := a.Store.Views()
		var items []string
		for _, x := range vs {
			items = append(items, fmt.Sprintf("%s | 00 consoles | %s", x.Name, "Active on the left"))
		}
		return ChooseSize("SAVED VIEWS", items, BrowseHelp)
	case "ports":
		return PortsSize(len(ListeningPorts()))
	}
	return 64, 12
}

// Addable lists the consoles that are not in the view.
func (a *App) Addable(view string) ([]store.Console, error) {
	all, e := a.Store.Consoles("")
	if e != nil {
		return nil, e
	}
	in, e := a.Store.Consoles(view)
	if e != nil {
		return nil, e
	}
	member := map[string]bool{}
	for _, c := range in {
		member[c.ID] = true
	}
	var out []store.Console
	for _, c := range all {
		if !member[c.ID] {
			out = append(out, c)
		}
	}
	return out, nil
}
