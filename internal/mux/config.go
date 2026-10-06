// SPDX-License-Identifier: GPL-3.0-only
package mux

import (
	"fmt"
	"strings"
)

// The header of each console is the status line of the programs server, at the
// top. It lives INSIDE the view pane, so a click there arrives with row and
// column. A tmux 3.4 border does not report the mouse position and is also the
// divider: it could not distinguish opening the menu, moving or resizing.
// Focus arrives through focus events, enabled on both servers.
const focused = "#{m:*focused*,#{client_flags}}"

var header = "#[fg=#{?" + focused + ",black,colour250},bg=#{?" + focused + ",cyan,colour235}]" +
	"#{?" + focused + ",#[bold] >> ACTIVE: ,    }#{?#{@ss_name},#{@ss_name},#{session_name}} ▾ " +
	"#{?" + focused + ",<< ,}" +
	"#{?pane_dead,#[fg=black#,bg=yellow#,bold] FINISHED · Enter: options ,}#[default]"

const dead = "#[fg=black,bg=yellow,bold] Finished " +
	"#{?pane_dead_signal,(signal #{pane_dead_signal}),#{?pane_dead_status,(exit #{pane_dead_status}),}} #[default] " +
	"Enter or click: restart · rename · close panel · kill console"

// Dragging from the explorer onto a console types the path, as in VS Code
// terminals; dragging a console header onto another console swaps them. tmux
// reports each drag event against the pane UNDER the pointer, so the origin and
// the kind of gesture are recorded on press.
const (
	dragClient   = "@ss_drag_client"
	dragPane     = "@ss_drag_pane"
	dragKind     = "@ss_drag_kind" // path | console | header
	Disconnected = "@ss_disconnected"
	menuOption   = "@ss_menu_"
	dragging     = "#{==:#{" + dragClient + "},#{client_name}}"
	isConsole    = "#{==:#{@ss_kind},console}"
	// The first row of every view pane is its header.
	onHeader = "#{&&:#{==:#{mouse_y},0},#{!=:#{@ss_kind},}}"
)

var dragReset = []string{"set-option", "-g", dragClient, ""}

func dragIs(kind string) string { return "#{==:#{" + dragKind + "}," + kind + "}" }

// Layout keys, as in the view menu: Alt+N or Ctrl+B then N.
var LayoutKeys = []struct{ Key, Layout, Label string }{
	{"1", "even-horizontal", "Columns"}, {"2", "even-vertical", "Rows"}, {"3", "tiled", "Grid"},
	{"4", "main-vertical", "Active on the left"}, {"5", "main-horizontal", "Active on top"},
}

func LayoutLabel(l string) string {
	for _, k := range LayoutKeys {
		if k.Layout == l {
			return k.Label
		}
	}
	return "Grid"
}
func ValidLayout(v string) bool {
	for _, k := range LayoutKeys {
		if k.Layout == v {
			return true
		}
	}
	return false
}

func setg(option, value string) []string { return []string{"set-option", "-g", option, value} }

// Both servers: black background everywhere, also in empty cells, and the
// clipboard through OSC 52 so that selecting with the mouse copies to the PC.
func commonOptions() [][]string {
	black := "bg=#000000"
	return [][]string{
		setg("default-terminal", "tmux-256color"), setg("default-shell", "/bin/sh"),
		setg("destroy-unattached", "off"), setg("exit-unattached", "off"),
		setg("remain-on-exit", "on"), setg("history-limit", "10000"), setg("mouse", "on"),
		{"unbind-key", "-q", "-n", "MouseDrag1Border"},
		setg("set-clipboard", "on"),
		setg("window-style", "fg=white,"+black), setg("window-active-style", "fg=white,"+black),
		setg("status-style", "fg=white,"+black), setg("pane-border-style", "fg=colour240,"+black),
		setg("pane-active-border-style", "fg=cyan,"+black), setg("message-style", "fg=white,"+black),
		setg("message-command-style", "fg=cyan,"+black), setg("mode-style", "fg=cyan,"+black+",bold"),
		setg("status-left-style", "fg=cyan,"+black+",bold"), setg("status-right-style", "fg=white,"+black),
		setg("window-status-style", "fg=white,"+black), setg("window-status-current-style", "fg=cyan,"+black+",bold"),
		setg("popup-style", "fg=white,"+black), setg("popup-border-style", "fg=cyan,"+black),
		// Escape answers without the default half-second wait. It is paid twice
		// (view and console are two nested tmux), so 10 ms.
		{"set-option", "-sg", "escape-time", "10"},
		setg("status", "off"),
		{"set-option", "-s", "extended-keys", "on"},
		{"set-option", "-s", "focus-events", "on"},
		// Ctrl+Backspace: classic terminals send C-h, others extended CSI.
		{"bind-key", "-n", "C-h", "send-keys", "C-w"},
		{"bind-key", "-n", "C-BSpace", "send-keys", "C-w"},
	}
}

// RunAction is a ShellStudio action run from tmux, without waiting for it.
func (m *Mux) RunAction(arguments string) string {
	return "run-shell -b " + Quote(m.Self("_action")+" "+arguments)
}
func (m *Mux) killPrompt(name, arguments string) string {
	return "confirm-before -y -p " + Quote("Kill "+name+"? Enter confirms, Esc cancels ") + " " + Quote(m.RunAction(arguments))
}

// Options of a finished console, in its own pane (programs server).
func (m *Mux) deadMenu(position string) []string {
	rename := `command-prompt -I "#{@ss_name}" -p "New name:" ` + Quote(`set-option @ss_rename "%%%" ; `+
		m.RunAction("rename --session #{session_name}"))
	return []string{"display-menu", "-O", "-T", "#[align=centre] #{@ss_name} finished ",
		"-x", position, "-y", position,
		"Restart", "r", "respawn-pane -k",
		"Rename", "n", rename,
		"",
		"Close panel (the console stays in the list)", "q", m.RunAction("remove --tty #{client_tty}"),
		"Kill console", "m", m.killPrompt("#{@ss_name}", "kill --session #{session_name}")}
}

// ProgramsControls belong to the programs server: header and finished consoles.
func (m *Mux) ProgramsControls() [][]string {
	cmds := commonOptions()
	cmds = append(cmds,
		setg("prefix", "None"), setg("status", "on"), setg("status-position", "top"),
		setg("status-format[0]", header))
	// The hook runs without a client: without -t, tmux would redraw the last one used.
	for _, hook := range []string{"client-focus-in", "client-focus-out"} {
		cmds = append(cmds, []string{"set-hook", "-g", hook, Script([]string{"run-shell", "-C", `refresh-client -S -t "#{hook_client}"`})})
	}
	// tmux does not redraw the status line when a pane dies: the header would
	// say the console is alive until the next focus change.
	cmds = append(cmds, []string{"set-hook", "-g", "pane-died", Script([]string{"run-shell", "-C", "#{L:refresh-client -S -t #{client_name} ; }"})},
		setg("remain-on-exit-format", dead),
		// They only change something in a finished pane; otherwise the key is unchanged.
		[]string{"bind-key", "-n", "Enter", "if-shell", "-F", "#{pane_dead}", Join(m.deadMenu("C")...), "send-keys Enter"},
		[]string{"bind-key", "-n", "MouseDown1Pane", "if-shell", "-F", "#{pane_dead}",
			Script([]string{"select-pane", "-t", "="}, m.deadMenu("M")),
			Script([]string{"select-pane", "-t", "="}, []string{"send-keys", "-M"})})
	// The status line is the header: no window switching with the wheel nor window menus.
	for _, key := range []string{"MouseDown1Status", "MouseDown3Status", "MouseDown3StatusLeft",
		"M-MouseDown3Status", "M-MouseDown3StatusLeft", "WheelUpStatus", "WheelDownStatus"} {
		cmds = append(cmds, []string{"unbind-key", "-q", "-n", key})
	}
	return append(cmds, setg("@ss_config", m.Version))
}

// Sizes are the content sizes of the dialogs, computed by the caller.
type Sizes struct {
	MenuW, MenuH, MenuStacked int
	NewW, NewH                int
	ArrangeW, ArrangeRows     int
	BrowseW, BrowseRows       int
	Views                     int
}

// Fit is min(size, limit) as a tmux format: -w and -h cannot exceed the terminal.
func Fit(size, limit string) string {
	return "#{?#{e|<:" + limit + "," + size + "}," + limit + "," + size + "}"
}
func itoa(n int) string { return fmt.Sprint(n) }

// PopupShell is the command of a shortcut that opens a dialog: a popup sized to
// its content. tmux 3.4 neither resizes an open popup nor stacks another on top,
// so a dialog that leads to another writes its popup command to a file and this
// loop opens it as soon as the first one closes.
func (m *Mux) PopupShell(action, width, height string) string {
	command := m.Self("_view", action, "--client") + ` "#{client_name}" --pane "#{pane_id}"`
	popup := Join(m.Tmux, "-u", "-S", m.Socket(true), "display-popup", "-c", "#{client_name}", "-E", "-w", width, "-h", height, command)
	marker := Quote(m.Paths.Runtime) + "/next-#{client_pid}"
	return "f=" + marker + `; rm -f "$f"; ` + popup + `; while [ -s "$f" ]; do c=$(cat "$f"); rm -f "$f"; eval "$c"; done`
}

// PopupLine opens one dialog for a known client with a fixed size.
func (m *Mux) PopupLine(client, action string, width, height int, pane string, back bool) string {
	args := []string{"_view", action, "--client", client}
	if pane != "" {
		args = append(args, "--pane", pane)
	}
	if back {
		args = append(args, "--back")
	}
	return Join(m.Tmux, "-u", "-S", m.Socket(true), "display-popup", "-c", client, "-E", "-w", itoa(width), "-h", itoa(height), m.Self(args...))
}

// ViewsControls belong to the views server: it contains clients, not programs.
func (m *Mux) ViewsControls(s Sizes) [][]string {
	cmds := commonOptions()
	// View windows keep their size during bursts of SSH client resizes; only the
	// stable geometry is applied.
	cmds = append(cmds,
		[]string{"set-hook", "-g", "client-resized", "run-shell -b " + Quote(m.Self("_resize_window", "--client", "#{hook_client}"))},
		[]string{"set-hook", "-g", "client-attached", "run-shell " + Quote(m.Self("_resize_window", "--client", "#{hook_client}", "--immediate"))},
		[]string{"set-hook", "-g", "client-session-changed", "run-shell " + Quote(m.Self("_resize_window", "--client", "#{hook_client}", "--immediate"))},
		[]string{"set-hook", "-gu", "client-detached"},
		setg("status", "off"), setg("prefix", "C-b"),
		// The header is inside each pane: the border is only a divider.
		setg("pane-border-status", "off"), []string{"set-option", "-gu", "pane-border-format"},
		setg("pane-active-border-style", "fg=cyan,bg=#000000,bold"), setg("pane-border-lines", "heavy"),
		setg("remain-on-exit-format", "#[fg=black,bg=yellow,bold] This panel ended #[default] Click: redo · close panel"),
		// A console whose nested client ended (the program was killed elsewhere
		// or the programs server restarted) shows its options in the same pane.
		// Only once: if that screen fails, the pane stays dead, without a loop.
		// respawn-pane can replace the process before its queued death hook
		// runs. Never disconnect the replacement if it is already alive.
		[]string{"set-hook", "-g", "pane-died", Script([]string{"if-shell", "-F", "#{&&:#{pane_dead},#{&&:" + isConsole + ",#{!=:#{" + Disconnected + "},1}}}",
			Script([]string{"set-option", "-p", Disconnected, "1"}, []string{"respawn-pane", "-k", "exec " + m.Self("_disconnected")})})})
	for _, d := range []struct{ key, dir string }{{"Left", "L"}, {"Right", "R"}, {"Up", "U"}, {"Down", "D"}} {
		// Ctrl+Alt: Windows Terminal keeps Ctrl+Shift+Up/Down to scroll its history.
		cmds = append(cmds, []string{"bind-key", "-n", "C-M-" + d.key, "select-pane", "-" + d.dir})
	}
	cmds = append(cmds, []string{"unbind-key", "-q", "-n", "C-S-Up"}, []string{"unbind-key", "-q", "-n", "C-S-Down"},
		[]string{"bind-key", "-n", "F6", "select-pane", "-t", ":.+"},
		[]string{"bind-key", "-n", "S-F6", "select-pane", "-t", ":.-"},
		[]string{"bind-key", "-n", "F4", "resize-pane", "-Z"},
		[]string{"bind-key", "-n", "F10", "detach-client"})
	w, h := "#{client_width}", "#{client_height}"
	narrow := "#{e|<:#{client_width}," + itoa(s.MenuW) + "}"
	menuHeight := "#{?" + narrow + "," + Fit(itoa(s.MenuStacked), h) + "," + Fit(itoa(s.MenuH), h) + "}"
	sizes := map[string][2]string{
		"menu": {Fit(itoa(s.MenuW), w), menuHeight},
		"new":  {Fit(itoa(max(s.NewW, 46)), w), Fit(itoa(max(s.NewH, 12)), h)},
		// One row per pane (the explorer included: at most one too many).
		"arrange": {Fit(itoa(s.ArrangeW), w), Fit("#{e|+:#{window_panes},"+itoa(s.ArrangeRows-1)+"}", h)},
		"views":   {Fit(itoa(s.BrowseW), w), Fit("#{e|+:#{@ss_views},"+itoa(s.BrowseRows-1)+"}", h)},
		"kill":    {Fit("64", w), Fit("12", h)},
	}
	popups := map[string]string{}
	for _, b := range []struct{ key, action, region string }{{"F2", "menu", "menu"}, {"F3", "new", "new"},
		{"F7", "views", "views"}, {"F5", "arrange", "arrange"}, {"F9", "kill", "kill"}} {
		size := sizes[b.action]
		popup := m.PopupShell(b.action, size[0], size[1])
		cmds = append(cmds, []string{"bind-key", "-n", b.key, "run-shell", popup})
		if b.action == "kill" {
			cmds = append(cmds, []string{"bind-key", "-n", "C-S-DC", "run-shell", popup})
		}
		popups[b.region] = "run-shell " + Quote(popup)
	}
	popups["replace"] = "run-shell " + Quote(m.PopupShell("replace", sizes["new"][0], sizes["new"][1]))
	// Ports are counted when opening: the program measures their popup.
	popups["ports"] = "run-shell " + Quote(m.Self("_dialog", "ports", "--client")+` "#{client_name}" --pane "#{pane_id}"`)
	cmds = append(cmds, setg("@ss_views", itoa(s.Views)), []string{"unbind-key", "-q", "-n", "MouseDown1Status"})
	cmds = append(cmds, m.headerMenus(popups)...)
	cmds = append(cmds, m.dragBindings()...)
	for _, l := range LayoutKeys {
		command := m.Self("_view", "layout", "--layout", l.Layout) + ` --client "#{client_name}" --pane "#{pane_id}"`
		cmds = append(cmds, []string{"bind-key", "-n", "M-" + l.Key, "run-shell", command}, []string{"bind-key", l.Key, "run-shell", command})
	}
	for _, mv := range []struct{ key, action string }{{"M-[", "move-before"}, {"M-]", "move-after"}} {
		cmds = append(cmds, []string{"bind-key", "-n", mv.key, "run-shell", m.Self("_view", mv.action) + ` --client "#{client_name}" --pane "#{pane_id}"`})
	}
	for _, d := range []struct{ key, dir string }{{"Left", "L"}, {"Right", "R"}, {"Up", "U"}, {"Down", "D"}} {
		cmds = append(cmds, []string{"bind-key", "-r", d.key, "resize-pane", "-" + d.dir, "5"})
	}
	for _, sw := range []struct {
		keys   []string
		action string
	}{{[]string{"C-S-Right", "F8"}, "next"}, {[]string{"C-S-Left", "S-F8"}, "previous"}} {
		for _, key := range sw.keys {
			cmds = append(cmds, []string{"bind-key", "-n", key, "run-shell", m.Self("_view", sw.action) + ` --client "#{client_name}"`})
		}
	}
	return append(cmds, setg("@ss_config", m.Version))
}

// headerMenus are native tmux menus: they appear at once, without a process.
// Each menu lives in an option and is invoked by name: nested in the mouse
// bindings, quote escaping grows per level and tmux rejects long commands.
func (m *Mux) headerMenus(popups map[string]string) [][]string {
	viewAction := func(action string) string {
		return "run-shell -b " + Quote(m.Self("_view", action)+` --client "#{client_name}" --pane "#{pane_id}"`)
	}
	rename := `command-prompt -I "#{@ss_name}" -p "New name:" ` + Quote(`set-option -p @ss_rename "%%%" ; `+
		m.RunAction(`rename --client "#{client_name}"`))
	head := []string{"display-menu", "-O", "-T", "#[align=centre] #{@ss_name} ", "-t", "=", "-x", "M", "-y", "M"}
	console := append(append([]string{}, head...),
		"Rename", "n", rename,
		"#{?window_zoomed_flag,Restore the layout,Zoom} (F4)", "z", "resize-pane -Z",
		"Move before (Alt+[)", "[", viewAction("move-before"),
		"Move after (Alt+])", "]", viewAction("move-after"),
		"",
		"New console (F3)", "c", popups["new"],
		"Kill and replace here", "r", popups["replace"],
		"Forward ports to your PC", "p", popups["ports"],
		"Close panel (the console keeps running)", "q", m.RunAction("remove --pane #{pane_id}"),
		"Kill console (F9)", "m", m.killPrompt("#{@ss_name}", "kill --pane #{pane_id}"),
		"",
		"View menu (F2)", "v", popups["menu"])
	tree := append(append([]string{}, head...),
		"Search (/)", "/", "send-keys /",
		"Edit the selection with nano (E)", "e", "send-keys E",
		"Rename the selection (N)", "n", "send-keys N",
		"Reload folders (R)", "r", "send-keys R",
		"Forward ports to your PC", "p", popups["ports"],
		"",
		"Hide explorer", "o", m.RunAction("explorer --pane #{pane_id}"),
		"",
		"View menu (F2)", "v", popups["menu"])
	empty := append(append([]string{}, head...),
		"New console (F3)", "c", popups["new"],
		"Forward ports to your PC", "p", popups["ports"],
		"View menu (F2)", "v", popups["menu"])
	return [][]string{setg(menuOption+"console", Join(console...)), setg(menuOption+"tree", Join(tree...)), setg(menuOption+"empty", Join(empty...))}
}

// run-shell -C expands the format (chooses the menu for the pane) and keeps the
// mouse event: the menu appears where the click happened.
var headerMenu = Join("run-shell", "-C", "#{?"+isConsole+",#{"+menuOption+"console},"+
	"#{?#{==:#{@ss_kind},explorer},#{"+menuOption+"tree},#{"+menuOption+"empty}}}")

func (m *Mux) deadPanelMenu() string {
	return Join("display-menu", "-O", "-T", "#[align=centre] #{@ss_name} ", "-t", "=", "-x", "M", "-y", "M",
		"Redo the panel", "r", "respawn-pane -k",
		"Close panel", "q", m.RunAction("remove --pane #{pane_id}"))
}

func (m *Mux) dragBindings() [][]string {
	drop := m.Self("_view", "drop") + ` --origin "#{` + dragPane + `}" --target "#{pane_id}"`
	move := m.Self("_action", "move") + ` --pane "#{` + dragPane + `}" --target "#{pane_id}"`
	ontoOtherConsole := "#{&&:" + dragIs("console") + ",#{&&:" + isConsole + ",#{!=:#{pane_id},#{" + dragPane + "}}}}"
	// Capture both IDs before any movement. The action stores the order and
	// swaps native panes under the render lock, preserving hand-adjusted sizes.
	swap := Script([]string{"run-shell", "-b", move})
	resize := m.Self("_resize")
	resizeClient := ` --client "#{client_name}" --id "#{client_pid}"`
	resizePoint := ` --pane "#{pane_id}" --x "#{mouse_x}" --y "#{mouse_y}" --left "#{pane_left}" --top "#{pane_top}"`
	begin := resize + " start" + resizeClient + ` --pane "#{pane_id}"`
	first := resize + " orient" + resizeClient + resizePoint
	end := resize + " end" + resizeClient + resizePoint
	cancel := resize + " cancel" + resizeClient
	lastPoint := `set-option -g @ss_resize_point_#{client_pid} "#{pane_left},#{pane_top},#{mouse_x},#{mouse_y}"`
	var resizeBindings [][]string
	for _, key := range []string{"MouseDown1Border", "SecondClick1Border", "TripleClick1Border"} {
		parts := [][]string{{"run-shell", begin}, {"switch-client", "-T", "ss-resize-start"}}
		if key == "MouseDown1Border" {
			parts = append([][]string{dragReset}, parts...)
		}
		resizeBindings = append(resizeBindings, []string{"bind-key", "-n", key, Script(parts...)})
	}
	for _, where := range []string{"Border", "Pane", "Status"} {
		var remember [][]string
		if where == "Pane" {
			remember = [][]string{{"run-shell", "-C", lastPoint}}
		}
		resizeBindings = append(resizeBindings,
			[]string{"bind-key", "-T", "ss-resize-start", "MouseDrag1" + where,
				Script(append(append([][]string{}, remember...), []string{"run-shell", first}, []string{"switch-client", "-T", "ss-resize-active"})...)},
			[]string{"bind-key", "-T", "ss-resize-active", "MouseDrag1" + where,
				Script(append(append([][]string{}, remember...), []string{"display-message", "-d", "500", "Release to apply the size"},
					[]string{"switch-client", "-T", "ss-resize-active"})...)},
			[]string{"bind-key", "-T", "ss-resize-start", "MouseDragEnd1" + where, "run-shell", cancel},
			[]string{"bind-key", "-T", "ss-resize-active", "MouseDragEnd1" + where, "run-shell", end},
			[]string{"bind-key", "-T", "ss-resize-start", "MouseUp1" + where, "run-shell", cancel})
	}
	for _, table := range []string{"ss-resize-start", "ss-resize-active"} {
		resizeBindings = append(resizeBindings, []string{"bind-key", "-T", table, "Escape", "run-shell", cancel})
	}
	selectPane := []string{"select-pane", "-t", "="}
	out := [][]string{
		{"bind-key", "-n", "MouseDown1Pane", "if-shell", "-F", "#{pane_dead}",
			Script(selectPane, dragReset) + " ; " + m.deadPanelMenu(),
			Script([]string{"if-shell", "-F", onHeader,
				// The header is not forwarded: it belongs to the view, not the program.
				Script(selectPane,
					[]string{"set-option", "-gF", dragClient, "#{client_name}"},
					[]string{"set-option", "-gF", dragPane, "#{pane_id}"},
					[]string{"set-option", "-gF", dragKind, "#{?" + isConsole + ",console,header}"}),
				// tmux default plus the origin: only the explorer arms a path drag.
				Script(selectPane, []string{"send-keys", "-M"},
					[]string{"set-option", "-pF", "@ss_client", "#{client_name}"},
					[]string{"set-option", "-gF", dragClient, "#{?#{==:#{@ss_kind},explorer},#{client_name},}"},
					[]string{"set-option", "-gF", dragPane, "#{pane_id}"},
					[]string{"set-option", "-g", dragKind, "path"})})},
		// Pressing and releasing the header without moving: its menu.
		{"bind-key", "-n", "MouseUp1Pane", "if-shell", "-F", "#{&&:" + dragging + ",#{!=:#{" + dragKind + "},path}}",
			Script(dragReset, []string{"if-shell", "-F", "#{&&:" + onHeader + ",#{==:#{pane_id},#{" + dragPane + "}}}", headerMenu}),
			"send-keys -M"},
		{"bind-key", "-n", "MouseDown3Pane", "if-shell", "-F", onHeader,
			Script(selectPane, dragReset) + " ; " + headerMenu,
			Script(selectPane, []string{"send-keys", "-M"})},
		{"bind-key", "-n", "MouseDrag1Pane", "if-shell", "-F", dragging,
			Script([]string{"if-shell", "-F", dragIs("path"),
				Script([]string{"display-message", "-d", "1500", "Drop on a console to type the path"}),
				Script([]string{"if-shell", "-F", dragIs("console"),
					Script([]string{"display-message", "-d", "1500", "Drop on another console to swap them"})})}),
			// The view selects the text: forwarding the drag to the nested tmux
			// depends on the program's momentary mouse mode and can end up typing
			// SGR sequences on its input line.
			"copy-mode -M"},
		{"bind-key", "-n", "MouseDragEnd1Pane", "if-shell", "-F", dragging,
			Script([]string{"if-shell", "-F", dragIs("path"), Script([]string{"run-shell", drop}),
				Script([]string{"if-shell", "-F", ontoOtherConsole, swap})}, dragReset),
			"send-keys -M"},
	}
	// Dropping on a divider or the status line types nothing.
	for _, where := range []string{"Border", "Status"} {
		out = append(out, []string{"bind-key", "-n", "MouseDragEnd1" + where, "if-shell", "-F", dragging,
			Script([]string{"if-shell", "-F", dragIs("path"), Script([]string{"run-shell", m.Self("_view", "drop") + ` --origin "#{` + dragPane + `}"`})}, dragReset)})
	}
	// tmux double and triple click (select a word or line and copy it), except in
	// the explorer: there a double click opens the file, and the default
	// select-pane would steal the focus from the new console.
	for _, k := range []struct{ key, selection string }{{"DoubleClick1Pane", "select-word"}, {"TripleClick1Pane", "select-line"}} {
		out = append(out, []string{"bind-key", "-n", k.key, "if-shell", "-F", "#{!=:#{@ss_kind},explorer}",
			Script(selectPane, []string{"if-shell", "-F", "#{||:#{pane_in_mode},#{mouse_any_flag}}", "send-keys -M",
				Script([]string{"copy-mode", "-H"}, []string{"send-keys", "-X", k.selection}, []string{"run-shell", "-d", "0.3"},
					[]string{"send-keys", "-X", "copy-pipe-and-cancel"})})})
	}
	return append(out, resizeBindings...)
}

// BlurInactive: a new nested client starts believing it has the focus and tmux
// only reports focus CHANGES; inactive consoles are told once, after starting,
// so their header does not say ACTIVE.
func BlurInactive(panes []string) [][]string {
	var blur [][]string
	for _, p := range panes {
		blur = append(blur, []string{"if-shell", "-F", "-t", p, "##{?pane_active,,1}", Join("send-keys", "-t", p, "-l", "\x1b[O")})
	}
	if len(blur) == 0 {
		return nil
	}
	// run-shell -C expands formats before parsing: ## postpones them.
	return [][]string{{"run-shell", "-b", "-d", "0.5", "-C", Script(blur...)}}
}

// Respawn replaces a view pane's process and clears its disconnected mark.
func Respawn(pane, command string) [][]string {
	return [][]string{{"respawn-pane", "-k", "-t", pane, command}, {"set-option", "-p", "-qu", "-t", pane, Disconnected}}
}

func trimLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
