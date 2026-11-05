// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"

	"shellstudio/internal/i18n"
	"shellstudio/internal/mux"
	"shellstudio/internal/theme"
)

// NativeItem is one entry of a native tmux menu (display-menu). An empty
// Label is a separator. MenuItem is the entry of the F2 popup menu.
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

// TagNativeItems lists the tags of a console's menu: each tag in its color (the
// current one marked), No tag and Manage…. pane identifies the view; console
// is the console to tag.
func (a *App) TagNativeItems(client, pane, console string) ([]NativeItem, error) {
	c, e := a.Store.Console(console)
	if e != nil {
		return nil, e
	}
	tags, e := a.Store.Tags()
	if e != nil {
		return nil, e
	}
	set := func(tag string) string {
		return a.Mux.RunAction(mux.Join("tag", "--pane", pane, "--session", "c-"+console, "--target", tag))
	}
	var items []NativeItem
	for i, t := range tags {
		mark := "  "
		if t.ID == c.TagID {
			mark = "✓ "
		}
		key := ""
		if i < 9 {
			key = string(rune('1' + i))
		}
		items = append(items, NativeItem{Label: mark + "#[fg=" + theme.Colour(theme.TagColor(t.Color)) + "]" + theme.Glyphs.Dot + "#[default] " + mux.FormatText(t.Name), Key: key, Command: set(t.ID)})
	}
	items = append(items, NativeItem{},
		NativeItem{Label: i18n.T("console.no_tag"), Key: "0", Command: set("none")},
		NativeItem{Label: i18n.T("console.manage_tags"), Key: "m", Command: "run-shell -b " + mux.Quote(a.Mux.Self("_dialog", "tags", "--client", client, "--pane", pane))})
	return items, nil
}

// TagMenu shows the tag menu of a console beside the pane that asked.
func (a *App) TagMenu(client, pane, console string) error {
	items, e := a.TagNativeItems(client, pane, console)
	if e != nil {
		return e
	}
	return a.ShowMenu(client, pane, "#{popup_pane_right}", "#{popup_pane_top}", i18n.T("console.tag"), items)
}

// OpenDialogFor opens a dialog about a given target (a console listed in the
// Minimized panel, for example) in a popup of a client, and waits for it.
func (a *App) OpenDialogFor(action, client, pane, target string) error {
	view := ""
	if f, e := a.Focus(client, pane, true); e == nil {
		view = f.View
	}
	w, h := a.DialogSize(action, view)
	args := []string{"_view", action, "--client", client, "--target", target}
	if pane != "" {
		args = append(args, "--pane", pane)
	}
	return exec.Command(a.Mux.Tmux, "-u", "-S", a.Mux.Socket(true), "display-popup", "-c", client, "-E",
		"-x", "C", "-y", "C", "-w", mux.Fit(itoa(w), "#{client_width}"), "-h", mux.Fit(itoa(h), "#{client_height}"), a.Mux.Self(args...)).Run()
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
