// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"errors"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"

	"shellstudio/internal/i18n"
	"shellstudio/internal/mux"
)

// NativeItem is one entry of a native tmux menu (display-menu). An empty
// Label is a separator. MenuItem is the entry of the F1 popup menu.
type NativeItem = mux.MenuEntry

// ShowMenu opens a native menu in a client, at client coordinates x and y
// (formats such as #{popup_pane_right} are accepted; empty is the pane's
// lower left). A menu waits for the user, so it runs without the five-second
// deadline of background tmux commands; callers run from run-shell -b.
func (a *App) ShowMenu(client, pane, x, y, title string, items []NativeItem) error {
	if x == "" {
		x = "P"
	}
	if y == "" {
		y = "P"
	}
	args := []string{"-u", "-S", a.Mux.Socket(true), "display-menu", "-O", "-T", "#[align=centre] " + mux.FormatText(title) + " ", "-x", x, "-y", y}
	if client != "" {
		args = append(args, "-c", client)
	}
	if pane != "" {
		args = append(args, "-t", pane)
	}
	for _, it := range items {
		if it.Label == "" {
			args = append(args, "")
			continue
		}
		args = append(args, it.Label, it.Key, it.Command)
	}
	return exec.Command(a.Mux.Tmux, args...).Run()
}

// TagMenu shows the tag menu of a console beside the pane that asked.
//
// It is a centered dialog, not a native menu: the Tag ▸ entry that opens it
// is chosen on the press, and the release that follows would land on a
// native menu opened under the pointer and close it (or pick a tag).
func (a *App) TagMenu(client, pane, console string) error {
	if _, e := a.Store.Console(console); e != nil {
		return errors.New(i18n.T("err.console_gone"))
	}
	return a.OpenDialogFor("tagchoose", client, pane, console)
}

// OpenDialogFor opens a dialog about a given target (a console listed in the
// Minimized panel, for example) in a popup of a client, and waits for it.
func (a *App) OpenDialogFor(action, client, pane, target string) error {
	argv := a.dialogForArgv(action, client, pane, target)
	return exec.Command(argv[0], argv[1:]...).Run()
}

// dialogForArgv is the popup of OpenDialogFor. Its sizes are plain numbers:
// a tmux command run from here has no client context in which a format
// such as #{client_width} could expand ("height invalid").
func (a *App) dialogForArgv(action, client, pane, target string) []string {
	view := ""
	if f, e := a.Focus(client, pane, true); e == nil {
		view = f.View
	}
	w, h := a.DialogSize(action, view)
	args := []string{"_view", action, "--client", client, "--target", target}
	if pane != "" {
		args = append(args, "--pane", pane)
	}
	return a.Mux.ModalPopup(client, itoa(w), itoa(h), a.Mux.Self(args...))
}

func itoa(n int) string { return strconv.Itoa(n) }

// SwitchToTab shows the view of a top bar tab in a client.
func (a *App) SwitchToTab(client, tab string) error {
	v, e := a.ViewByTab(tab)
	if e != nil {
		return e
	}
	return a.Open(v.ID, client)
}

// TopMenu opens a top bar menu (new, arrange or more) from the keyboard or
// a script; a click on the bar opens the same entries at the pointer.
func (a *App) TopMenu(client, pane, which string) error {
	items := a.TopMenuItems()[which]
	if len(items) == 0 {
		return nil
	}
	return a.ShowMenu(client, "", "C", "1", i18n.T("top."+which), items)
}

// TopMenuItems are the entries of the top bar menus. Commands expand
// #{client_name} when chosen; dialogs open in popups sized to their content.
func (a *App) TopMenuItems() map[string][]NativeItem {
	dialog := func(action string) string {
		return "run-shell -b " + mux.Quote(a.Mux.Self("_dialog", action, "--client")+` "#{client_name}"`)
	}
	view := func(action string, extra ...string) string {
		return "run-shell -b " + mux.Quote(a.Mux.Self(append([]string{"_view", action}, extra...)...)+` --client "#{client_name}"`)
	}
	var newMenu []NativeItem
	for _, k := range a.Kinds() {
		newMenu = append(newMenu, NativeItem{Label: mux.FormatText(k.Label), Key: k.Key, Command: dialog("kind:" + k.ID)})
	}
	newMenu = append(newMenu, NativeItem{}, NativeItem{Label: i18n.T("menu.add_existing"), Key: "A", Command: dialog("add")})
	var arrange []NativeItem
	for _, l := range mux.LayoutKeys {
		arrange = append(arrange, NativeItem{Label: l.Label, Key: l.Key, Command: view("layout", "--layout", l.Layout)})
	}
	arrange = append(arrange, NativeItem{}, NativeItem{Label: i18n.T("menu.arrange_marked") + " (F5)", Key: "L", Command: dialog("arrange")})
	more := []NativeItem{
		{Label: i18n.T("menu.new_view"), Key: "V", Command: dialog("new-view")},
		{Label: i18n.T("menu.other_views") + " (F7)", Key: "B", Command: dialog("views")},
		{},
		{Label: i18n.T("menu.toggle_explorer"), Key: "E", Command: view("toggle-explorer")},
		{Label: i18n.T("menu.toggle_minimized"), Key: "M", Command: view("toggle-minimized")},
		{Label: i18n.T("menu.tags"), Key: "T", Command: dialog("tags")},
		{},
		{Label: i18n.T("menu.ports"), Key: "P", Command: dialog("ports")},
		{Label: i18n.T("menu.permissions"), Key: "W", Command: dialog("permissions")},
		{Label: i18n.T("menu.language"), Key: "I", Command: dialog("language")},
		{},
		{Label: i18n.T("menu.quit") + " (F10)", Key: "Q", Command: "detach-client"},
	}
	return map[string][]NativeItem{mux.RangeNew: newMenu, mux.RangeArrange: arrange, mux.RangeMore: more}
}

// RenameView renames a view and updates the tabs.
func (a *App) RenameView(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return errors.New(i18n.T("err.view_name"))
	}
	v, e := a.Store.View(id)
	if e != nil {
		return errors.New(i18n.T("err.view_gone"))
	}
	vs, e := a.Store.Views()
	if e != nil {
		return e
	}
	for _, o := range vs {
		if o.ID != id && o.Name == name {
			return errors.New(i18n.T("err.view_exists", name))
		}
	}
	v.Name = name
	if e = a.Store.UpdateView(v); e != nil {
		return e
	}
	a.PublishTabs()
	return nil
}

// ToggleMinimizedPanel shows or hides the Minimized panel of a view.
func (a *App) ToggleMinimizedPanel(view string) error {
	v, e := a.Store.View(view)
	if e != nil {
		return e
	}
	v.MinimizedPanel = !v.MinimizedPanel
	if e = a.Store.UpdateView(v); e != nil {
		return e
	}
	return a.RefreshOpen(view)
}

var styleDirective = regexp.MustCompile(`#\[[^\]]*\]`)

// statusText turns an expanded status line into plain text of width
// columns: style directives go, and the part after #[align=right] is
// right-aligned.
func statusText(line string, width int) string {
	right := ""
	if i := strings.Index(line, "align=right"); i >= 0 {
		if j := strings.LastIndex(line[:i], "#["); j >= 0 {
			line, right = line[:j], line[j:]
		}
	}
	plain := func(s string) string {
		return strings.ReplaceAll(styleDirective.ReplaceAllString(s, ""), "##", "#")
	}
	l, r := plain(line), plain(right)
	pad := width - runewidth.StringWidth(l) - runewidth.StringWidth(r)
	if pad < 1 {
		return runewidth.Truncate(l, width, "")
	}
	return l + strings.Repeat(" ", pad) + r
}

// Backdrop is what a client shows, as plain text rows of width×height: the
// top bar and the visible panes with their dividers. Dialogs draw it dimmed
// behind their box (tui.Backdrop).
func (a *App) Backdrop(client string, width, height int) []string {
	grid := make([][]rune, height)
	for y := range grid {
		grid[y] = []rune(strings.Repeat(" ", width))
	}
	put := func(y, x int, text string) {
		if y < 0 || y >= height {
			return
		}
		for _, r := range text {
			w := runewidth.RuneWidth(r)
			if w == 0 {
				continue
			}
			if x < 0 || x+w > width {
				break
			}
			grid[y][x] = r
			if w == 2 {
				grid[y][x+1] = 0
			}
			x += w
		}
	}
	o, e := a.Mux.Run(true, "display-message", "-p", "-c", client, "#{window_id}\t#{status}\t#{status-position}\t#{window_zoomed_flag}")
	f := strings.Split(strings.TrimSpace(o), "\t")
	if e != nil || len(f) != 4 {
		return nil
	}
	offset := 0
	if f[1] == "on" {
		row := 0
		if f[2] == "top" {
			offset = 1
		} else {
			row = height - 1
		}
		if s, e := a.Mux.Run(true, "display-message", "-p", "-c", client, "#{E:status-format[0]}"); e == nil {
			put(row, 0, statusText(strings.TrimRight(s, "\n"), width))
		}
	}
	panes, _ := a.Mux.Run(true, "list-panes", "-t", f[0], "-F", "#{pane_id}\t#{pane_left}\t#{pane_top}\t#{pane_width}\t#{pane_height}\t#{pane_active}")
	for _, l := range strings.Split(strings.TrimSpace(panes), "\n") {
		p := strings.Split(l, "\t")
		if len(p) != 6 || (f[3] == "1" && p[5] != "1") {
			continue
		}
		x, _ := strconv.Atoi(p[1])
		y, _ := strconv.Atoi(p[2])
		w, _ := strconv.Atoi(p[3])
		h, _ := strconv.Atoi(p[4])
		y += offset
		text, e := a.Mux.Run(true, "capture-pane", "-p", "-t", p[0])
		if e == nil {
			for i, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
				if i >= h {
					break
				}
				put(y+i, x, runewidth.Truncate(line, w, ""))
			}
		}
		if f[3] != "1" {
			// Dividers on the right and below, as tmux draws them.
			for i := -1; i <= h; i++ {
				put(y+i, x+w, "│")
			}
			put(y+h, x, strings.Repeat("─", max(0, w)))
		}
	}
	out := make([]string, height)
	for y, row := range grid {
		var b strings.Builder
		for _, r := range row {
			if r != 0 {
				b.WriteRune(r)
			}
		}
		out[y] = b.String()
	}
	return out
}
