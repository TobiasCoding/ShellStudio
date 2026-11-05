// SPDX-License-Identifier: GPL-3.0-only
package mux

import (
	"shellstudio/internal/i18n"
	"shellstudio/internal/theme"
)

// MenuEntry is one entry of a native tmux menu. An empty Label is a
// separator. Command is a tmux command; formats expand when it runs.
type MenuEntry struct{ Label, Key, Command string }

// TopMenuNames are the menus of the top bar, in their order at the right.
var TopMenuNames = []string{RangeNew, RangeArrange, RangeMore}

// topBar is the status line of the views server: VISTAS, one tab per view
// (@ss_tabs, short names below 100 columns) and the menus at the right.
// Every element is a user range: a click arrives as #{mouse_status_range}.
func topBar() string {
	menu := theme.TmuxGlyph(theme.Glyphs.Menu, theme.ASCII.Menu)
	button := func(r, label string) string {
		return "#[range=user|" + r + "] " + FormatText(label) + " " + menu + " #[norange]"
	}
	return "#[range=user|" + RangeViews + "]#[fg=" + theme.Colour(theme.White) + ",bold] " + FormatText(i18n.T("top.views")) + " #[norange]#[nobold]" +
		"#[fg=" + theme.Colour(theme.HeaderFg) + "] " +
		"#{?#{e|<:#{client_width},100},#{E:@ss_tabs_short},#{E:@ss_tabs}}" +
		"#[align=right]#[fg=" + theme.Colour(theme.Text) + "]" +
		button(RangeNew, i18n.T("top.new")) + button(RangeArrange, i18n.T("top.arrange")) + button(RangeMore, i18n.T("top.more"))
}

const tabTarget = "@ss_tab_target"

// topBarBindings: a click on a tab shows its view, on VISTAS the views
// dialog (F7) and on a menu its native menu under the pointer. A right click
// on a tab offers rename, close and new view.
func (m *Mux) topBarBindings(popups map[string]string, menus map[string][]MenuEntry) [][]string {
	var cmds [][]string
	for _, name := range TopMenuNames {
		args := []string{"display-menu", "-O", "-T", "#[align=centre] " + FormatText(i18n.T("top."+name)) + " ", "-x", "M", "-y", "S"}
		for _, e := range menus[name] {
			if e.Label == "" {
				args = append(args, "")
				continue
			}
			args = append(args, e.Label, e.Key, e.Command)
		}
		if len(menus[name]) == 0 {
			args = append(args, "-", "", "")
		}
		cmds = append(cmds, setg(menuOption+"top"+name, Join(args...)))
	}
	cmds = append(cmds, setg(menuOption+"top"+RangeViews, popups["views"]))
	rangeIs := func(r string) string { return "#{==:#{mouse_status_range}," + r + "}" }
	choose := "#{?" + rangeIs(RangeViews) + ",#{" + menuOption + "top" + RangeViews + "}," +
		"#{?" + rangeIs(RangeNew) + ",#{" + menuOption + "top" + RangeNew + "}," +
		"#{?" + rangeIs(RangeArrange) + ",#{" + menuOption + "top" + RangeArrange + "}," +
		"#{?" + rangeIs(RangeMore) + ",#{" + menuOption + "top" + RangeMore + "},}}}}"
	isTab := "#{m:t*,#{mouse_status_range}}"
	tab := "run-shell -b " + Quote(m.Self("_view", "tab")+` --client "#{client_name}" --target "#{mouse_status_range}"`)
	viewDialog := func(action string) string {
		return "run-shell -b " + Quote(m.Self("_view", action)+` --client "#{client_name}" --target "#{`+tabTarget+`}"`)
	}
	tabMenu := Join("display-menu", "-O", "-T", "#[align=centre] "+FormatText(i18n.T("top.views"))+" ", "-x", "M", "-y", "S",
		i18n.T("menu.rename_view"), "r", viewDialog("rename-view"),
		i18n.T("menu.close_view"), "x", viewDialog("close-view"),
		"",
		i18n.T("menu.new_view"), "n", "run-shell -b "+Quote(m.Self("_dialog", "new-view", "--client")+` "#{client_name}"`))
	cmds = append(cmds,
		[]string{"bind-key", "-n", "MouseDown1Status", "if-shell", "-F", isTab, tab,
			Script([]string{"if-shell", "-F", "#{mouse_status_range}", Join("run-shell", "-C", choose)})},
		[]string{"bind-key", "-n", "MouseDown3Status", "if-shell", "-F", isTab,
			Script([]string{"set-option", "-gF", tabTarget, "#{mouse_status_range}"}) + " ; " + tabMenu},
	)
	// No window switching with the wheel nor tmux's own window menus.
	for _, key := range []string{"MouseDown3StatusLeft", "MouseDown3StatusRight", "M-MouseDown3Status",
		"M-MouseDown3StatusLeft", "WheelUpStatus", "WheelDownStatus"} {
		cmds = append(cmds, []string{"unbind-key", "-q", "-n", key})
	}
	return cmds
}
