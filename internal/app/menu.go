// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"strings"

	"github.com/mattn/go-runewidth"
	"shellstudio/internal/i18n"
	"shellstudio/internal/mux"
	"shellstudio/internal/store"
)

type MenuItem struct{ Key, Label, Action string }
type MenuColumn struct {
	Title string
	Items []MenuItem
}

// Shortcut lines of each list; they also give the exact size of its popup.
// They are functions so that they follow the current language. The key of
// every line stays first and untranslated: HintKey reads it.
func ArrangeHelp() []string {
	out := []string{i18n.T("ui.help.mark"), i18n.T("ui.help.mark_all"), i18n.T("ui.help.unmark_all"), i18n.T("ui.help.mark_active")}
	for _, l := range mux.LayoutKeys {
		out = append(out, l.Key+"  "+mux.LayoutLabel(l.Layout))
	}
	return append(out, i18n.T("ui.help.back"), i18n.T("ui.help.partial"))
}
func BrowseHelp() []string {
	return []string{i18n.T("ui.help.open"), i18n.T("ui.help.view_cons"), i18n.T("ui.help.new_view"), i18n.T("ui.help.delete_view"), i18n.T("ui.help.back")}
}
func NewHelp() []string { return []string{i18n.T("ui.help.create"), i18n.T("ui.help.cancel")} }
func AddHelp() []string { return []string{i18n.T("ui.help.add"), i18n.T("ui.help.back")} }

// PermissionsHelp: settings that apply to the whole application, not to this view.
func PermissionsHelp() []string {
	return []string{i18n.T("ui.help.change"), i18n.T("ui.help.back"), i18n.T("ui.help.applies")}
}
func PermissionModeHelp() []string {
	return []string{i18n.T("ui.help.use_mode"), i18n.T("ui.help.back")}
}
func ChooseKindHelp() []string {
	return []string{i18n.T("ui.help.choose_kind"), i18n.T("ui.help.cancel")}
}

// StatusText is the status code of Status as shown in a list item.
func StatusText(status string) string {
	switch status {
	case "running", "stopped", "finished":
		return i18n.T("status.word." + status)
	}
	return status
}

// widestLayout is the longest layout name, to size lists that show one.
func widestLayout() string {
	w := ""
	for _, l := range mux.LayoutKeys {
		if t := mux.LayoutLabel(l.Layout); Width(t) > Width(w) {
			w = t
		}
	}
	return w
}

// Rows of the view menu above and below the categories.
const MenuTop, MenuBottom = 3, 2

func (a *App) ConsolesHelp() []string {
	var out []string
	for _, k := range a.Kinds() {
		if k.Key != "" {
			out = append(out, i18n.T("ui.help.create_kind", k.Key, k.Label))
		}
	}
	return append(out, i18n.T("ui.help.add_existing"), i18n.T("ui.help.remove"), i18n.T("ui.help.rename"),
		i18n.T("ui.help.replace"), i18n.T("ui.help.kill"), i18n.T("ui.help.go_to"), i18n.T("ui.help.back"))
}

func (a *App) MenuColumns(v store.View) []MenuColumn {
	explorer := i18n.T("ui.menu.no")
	if v.Explorer {
		explorer = i18n.T("ui.menu.yes")
	}
	var consoles []MenuItem
	for _, k := range a.Kinds() {
		key := k.Key
		if key == "" {
			key = " "
		}
		consoles = append(consoles, MenuItem{key, i18n.T("ui.menu.new_kind", k.Label), "kind:" + k.ID})
	}
	consoles = append(consoles, MenuItem{"A", i18n.T("menu.add_existing"), "add"}, MenuItem{"S", i18n.T("ui.menu.in_view"), "consoles"})
	var layouts []MenuItem
	for _, l := range mux.LayoutKeys {
		layouts = append(layouts, MenuItem{l.Key, mux.LayoutLabel(l.Layout), "layout:" + l.Layout})
	}
	layouts = append(layouts, MenuItem{"L", i18n.T("menu.arrange_marked"), "arrange"})
	return []MenuColumn{
		{i18n.T("ui.menu.consoles"), consoles},
		{i18n.T("ui.menu.layout"), layouts},
		{i18n.T("ui.menu.views"), []MenuItem{{"V", i18n.T("ui.menu.new_view"), "new-view"}, {"B", i18n.T("ui.menu.other_views"), "views"}}},
		{i18n.T("ui.menu.more"), []MenuItem{{"E", i18n.T("ui.menu.explorer", explorer), "explorer"}, {"P", i18n.T("ui.menu.ports"), "ports"},
			{"W", i18n.T("ui.menu.permissions"), "permissions"},
			{"D", i18n.T("ui.menu.leave"), "leave"}, {"Q", i18n.T("ui.menu.back"), "back"}}},
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
	// 999/999 is as wide as the widest counter Choose draws.
	lines := append([]string{title, i18n.T("ui.choose.count", 999, 999)}, help...)
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

// PermissionItems shows the current choice of every agent.
func (a *App) PermissionItems() []string {
	var out []string
	for _, id := range PermissionAgents {
		label := AgentLabel(id) + " | "
		if p, e := a.Permission(id); e != nil {
			label += i18n.T("ui.perm.invalid", e.Error())
		} else {
			text, _ := PermissionText(id, p)
			label += text
		}
		out = append(out, label)
	}
	return out
}
func PermissionModeItems(agent string) []string {
	var out []string
	for _, p := range Permissions(agent) {
		label, detail := PermissionText(agent, p)
		out = append(out, label+" — "+detail)
	}
	return out
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
	x24 := strings.Repeat("x", 24)
	// Two-digit counters (10) are as wide as the 00 they stand for.
	s.NewW, s.NewH = ChooseSize(i18n.T("ui.dialog.new_in", x24), a.kindLabels(), NewHelp())
	s.ArrangeW, s.ArrangeRows = ChooseSize(i18n.T("ui.dialog.arrange", x24, 10, 10), []string{"[x] " + strings.Repeat("x", 30)}, ArrangeHelp())
	s.BrowseW, s.BrowseRows = ChooseSize(i18n.T("ui.dialog.saved_views"), []string{i18n.T("ui.dialog.view_item", x24, 10, widestLayout())}, BrowseHelp())
	s.TopMenus = a.TopMenuItems()
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
		return ChooseSize(i18n.T("ui.dialog.new_in", v.Name), a.kindLabels(), NewHelp())
	case "kill":
		return 64, 12
	case "consoles":
		programs, _ := a.Mux.Programs()
		var items []string
		for _, c := range members {
			items = append(items, c.Name+" | "+StatusText(Status(programs, c.ID)))
		}
		return ChooseSize(i18n.T("ui.dialog.consoles_of", v.Name), items, a.ConsolesHelp())
	case "add":
		items, _ := a.Addable(view)
		var labels []string
		for _, c := range items {
			labels = append(labels, c.Name+" | "+StatusText("running"))
		}
		return ChooseSize(i18n.T("ui.dialog.add"), labels, AddHelp())
	case "arrange":
		var items []string
		for _, c := range members {
			items = append(items, "[x] "+c.Name)
		}
		return ChooseSize(i18n.T("ui.dialog.arrange", v.Name, 10, 10), items, ArrangeHelp())
	case "views":
		vs, _ := a.Store.Views()
		var items []string
		for _, x := range vs {
			items = append(items, i18n.T("ui.dialog.view_item", x.Name, 10, widestLayout()))
		}
		return ChooseSize(i18n.T("ui.dialog.saved_views"), items, BrowseHelp())
	case "ports":
		return PortsSize(len(ListeningPorts()))
	case "tags":
		tags, _ := a.Store.Tags()
		return 76, max(14, len(tags)+11)
	case "tagchoose":
		tags, _ := a.Store.Tags()
		return 60, len(tags) + 9
	case "language":
		return 48, 10
	case "permissions":
		w, h := ChooseSize(i18n.T("ui.perm.title"), a.PermissionItems(), PermissionsHelp())
		for _, id := range PermissionAgents {
			mw, mh := ChooseSize(i18n.T("ui.perm.agent", strings.ToUpper(AgentLabel(id))), PermissionModeItems(id), PermissionModeHelp())
			w, h = max(w, mw), max(h, mh)
		}
		return w, h
	}
	return 64, 12
}

// Addable lists the consoles that are not in the view.
func (a *App) Addable(view string) ([]store.Console, error) {
	all, e := a.Store.Consoles("")
	if e != nil {
		return nil, e
	}
	// Minimized members belong to the view too: AddToView would only restore them.
	in, e := a.Store.Members(view)
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
