// SPDX-License-Identifier: GPL-3.0-only
package mux

import (
	"fmt"
	"strings"

	"shellstudio/internal/i18n"
	"shellstudio/internal/theme"
)

// The header of each console is the status line of the programs server, at the
// top. It lives INSIDE the view pane, so a click there arrives with row and
// column. A tmux 3.4 border does not report the mouse position and is also the
// divider: it could not distinguish opening the menu, moving or resizing.
// Focus arrives through focus events, enabled on both servers.
const focused = "#{m:*focused*,#{client_flags}}"

// header is the status line of every nested client: "Kind · Name", the
// finished badge and, at the right, the tag and the buttons ─ □ ▼ in fixed
// slots (HeaderButtons). The focused console takes the accent background.
// It is built when the controls are applied, in the current language.
func headerFormat() string {
	cond := func(a, b string) string { return "#{?" + focused + "," + a + "," + b + "}" }
	bg := cond(theme.Colour(theme.Accent), theme.Colour(theme.HeaderBg))
	fg := cond(theme.Colour(theme.White), theme.Colour(theme.HeaderFg))
	base := "#[fg=" + fg + "]#[bg=" + bg + "]#[nobold]"
	label := "#{?#{@ss_label},#{@ss_label},#{?#{@ss_name},#{@ss_name},#{session_name}}}"
	g := func(u, a string) string { return theme.TmuxGlyph(u, a) }
	return "#[fill=" + bg + "]" + base + cond("#[bold]", "") + " " + label + " " + base +
		"#{?pane_dead,#[fg=black#,bg=yellow#,bold] " + i18n.T("tmux.header.finished") + " ,}" + base +
		"#[align=right]#{?#{@ss_tag},#[fg=#{@ss_tag_fg}#,bg=#{@ss_tag_color}] #{@ss_tag} ,}" + base +
		" " + g(theme.Glyphs.Minimize, theme.ASCII.Minimize) + "  " +
		// ❐ while the view pane is zoomed (@ss_zoomed, see ZoomHook).
		"#{?#{@ss_zoomed}," + g(theme.Glyphs.Unzoom, theme.ASCII.Unzoom) + "," + g(theme.Glyphs.Maximize, theme.ASCII.Maximize) + "}" +
		"  " + g(theme.Glyphs.Menu, theme.ASCII.Menu) + " "
}

// HeaderButtons are the slots of the header buttons, counted in columns from
// the right edge of the pane (0 is the last column): ▼ menu, □ maximize and
// ─ minimize. Clicks there act at once and never start a drag.
var HeaderButtons = []struct {
	Action string
	Lo, Hi int
}{{"menu", 0, 2}, {"maximize", 3, 5}, {"minimize", 6, 8}}

func deadFormat() string {
	return "#[fg=black,bg=yellow,bold] " + i18n.T("tmux.dead.finished") + " " +
		"#{?pane_dead_signal,(" + i18n.T("tmux.dead.signal") + " #{pane_dead_signal}),#{?pane_dead_status,(" + i18n.T("tmux.dead.exit") + " #{pane_dead_status}),}} #[default] " +
		i18n.T("tmux.dead.options")
}

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

// LayoutLabel is the translated name of a layout, for display. Unknown
// layouts show as the grid.
func LayoutLabel(l string) string {
	if !ValidLayout(l) {
		l = "tiled"
	}
	return i18n.T("layout." + l)
}
func ValidLayout(v string) bool {
	for _, k := range LayoutKeys {
		if k.Layout == v {
			return true
		}
	}
	return false
}

// MoveKeys move the focused console within its view. Alt+[ and Alt+] were the
// old keys: Alt+[ is ESC [, the introducer of every CSI terminal report and
// mouse event, and Alt+] is ESC ], the introducer of OSC reports. A report
// that tmux cannot classify (split by SSH, or unsolicited) becomes M-[ plus
// literal text, so consoles moved by themselves while digits were typed.
var MoveKeys = []struct{ Key, Action string }{{"M-<", "move-before"}, {"M->", "move-after"}}

// Standard keys: F1 opens the view menu (help and every action), F2 renames,
// as on desktops, and Shift+F10 opens the context menu of the focused panel.
const (
	MenuKey        = "F1"
	RenameKey      = "F2"
	ContextMenuKey = "S-F10"
)

// RetiredKeys were bound by earlier versions. A running views server keeps its
// bindings across upgrades, so they are removed explicitly.
var RetiredKeys = []string{"M-[", "M-]"}

// ViewsEscapeTime is how long the views server waits for the rest of an
// escape sequence. Its input comes from the user's terminal through SSH or
// Mosh, where one mouse event (Claude and Codex track every movement) can
// arrive in two packets. tmux 3.4 turns an expired partial sequence into
// Alt+key plus literal text such as "<35;54;34M" typed into the console.
// It is also the window in which Escape followed by a key becomes Alt+key
// (Alt+1–5 and Alt+</> are bound), so it stays well below typing speed.
const ViewsEscapeTime = "50"

// terminalInput keeps the user's terminal from writing anything to tmux that
// the user did not type. tmux 3.4 asks every VT100-like terminal for device
// attributes, its version and colours on attach, and again for colours after
// a resize. Replies that arrive split, late or twice (ConPTY and Windows
// Terminal) are not recognised and are typed into the focused console as
// "?61;4;6;7;14;21;22;..." and the like. tmux decides "VT100-like" from the
// XT flag or a clear capability that starts with CSI; the shift-in prefix is a
// no-op for every terminal. Both servers answer colour queries from
// window-style, and the features VT100 detection would add are listed
// explicitly: Windows Terminal keeps exactly the same features. Terminals that
// tmux recognises from their reply (iTerm2, WezTerm, kitty, foot, mintty or an
// enclosing tmux) lose auto-detected extras such as RGB and synchronized
// output; terminfo and terminal-features still apply. The nested clients keep
// querying: the views server answers them locally, in one piece.
// Fixed slots after tmux's defaults, so each sync replaces them.
func terminalInput() [][]string {
	return [][]string{
		{"set-option", "-sg", "escape-time", ViewsEscapeTime},
		{"set-option", "-s", "terminal-overrides[9]", `*:XT@:clear=\017\E[H\E[2J`},
		{"set-option", "-s", "terminal-features[9]", "*:bpaste:focus:title"},
	}
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
		// Mosh 1.4 drops OSC 52 with an empty selection. Default it to the
		// clipboard (c), preserving explicit selections such as primary (p).
		// These private servers reapply controls on every sync: use a fixed
		// slot after tmux's defaults instead of appending duplicate overrides.
		{"set-option", "-s", "terminal-overrides[1]", `*:Ms=\E]52;%?%p1%l%t%p1%s%ec%;;%p2%s\007`},
		setg("window-style", "fg=white,"+black), setg("window-active-style", "fg=white,"+black),
		setg("status-style", "fg=white,"+black), setg("pane-border-style", "fg="+theme.Colour(theme.Border)+","+black),
		setg("pane-active-border-style", "fg="+theme.Colour(theme.Accent)+","+black+",bold"), setg("message-style", "fg=white,"+black),
		setg("message-command-style", "fg=cyan,"+black), setg("mode-style", "fg=cyan,"+black+",bold"),
		setg("status-left-style", "fg=cyan,"+black+",bold"), setg("status-right-style", "fg=white,"+black),
		setg("window-status-style", "fg=white,"+black), setg("window-status-current-style", "fg=cyan,"+black+",bold"),
		setg("popup-style", "fg=white,"+black), setg("popup-border-style", "fg="+theme.Colour(theme.Accent)+","+black),
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

// Options of a finished console, in its own pane (programs server).
func (m *Mux) deadMenu(position string) []string {
	dialog := func(action string) string {
		return "run-shell -b " + Quote(m.Self("_dialog", action)+` --tty "#{client_tty}"`)
	}
	return []string{"display-menu", "-O", "-T", "#[align=centre] #{@ss_name} " + i18n.T("tmux.dead.title") + " ",
		"-x", position, "-y", position,
		i18n.T("tmux.restart"), "r", "respawn-pane -k",
		i18n.T("tmux.rename"), "n", dialog("rename"),
		"",
		i18n.T("tmux.close_listed"), "q", m.RunAction("remove --tty #{client_tty}"),
		i18n.T("tmux.kill"), "m", dialog("kill")}
}

// ProgramsControls belong to the programs server: header and finished consoles.
func (m *Mux) ProgramsControls() [][]string {
	cmds := commonOptions()
	cmds = append(cmds,
		// Its terminal is a view pane: the views server writes each key or
		// mouse event in one piece, so Escape needs no network margin here.
		[]string{"set-option", "-sg", "escape-time", "10"},
		setg("prefix", "None"), setg("status", "on"), setg("status-position", "top"),
		setg("status-format[0]", headerFormat()))
	// The hook runs without a client: without -t, tmux would redraw the last one used.
	for _, hook := range []string{"client-focus-in", "client-focus-out"} {
		cmds = append(cmds, []string{"set-hook", "-g", hook, Script([]string{"run-shell", "-C", `refresh-client -S -t "#{hook_client}"`})})
	}
	// tmux does not redraw the status line when a pane dies: the header would
	// say the console is alive until the next focus change.
	cmds = append(cmds, []string{"set-hook", "-g", "pane-died", Script([]string{"run-shell", "-C", "#{L:refresh-client -S -t #{client_name} ; }"})},
		setg("remain-on-exit-format", deadFormat()),
		// They only change something in a finished pane; otherwise the key is unchanged.
		[]string{"bind-key", "-n", "Enter", "if-shell", "-F", "#{pane_dead}", Join(m.deadMenu("C")...), "send-keys Enter"},
		// A left click on a finished console only focuses it: its options
		// open with Enter, a right click or the header's ▼ menu.
		[]string{"bind-key", "-n", "MouseDown1Pane", "if-shell", "-F", "#{pane_dead}",
			Script([]string{"select-pane", "-t", "="}),
			Script([]string{"select-pane", "-t", "="}, []string{"send-keys", "-M"})},
		[]string{"bind-key", "-n", "MouseDown3Pane", "if-shell", "-F", "#{pane_dead}",
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
	// TopMenus are the entries of the top bar menus: new, arrange and more.
	TopMenus map[string][]MenuEntry
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
	popup := Join(m.ModalPopup("#{client_name}", width, height, command)...)
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
	return Join(m.ModalPopup(client, itoa(width), itoa(height), m.Self(args...))...)
}

// ModalEnv tells a dialog the size of its box and its client (see
// tui.Screen): "WIDTH,HEIGHT,CLIENT".
const ModalEnv = "SHELLSTUDIO_MODAL"

// ModalPopup is the command line of a dialog's popup. tmux 3.4 ignores
// clicks outside a popup, so the popup is borderless and covers the whole
// client; the dialog draws the workspace dimmed behind its own box of
// width×height (with its border; a number or a percentage) and closes on a
// click outside it. Sizes are given to the dialog, not to tmux: no format
// needs a client context to expand.
func (m *Mux) ModalPopup(client, width, height, command string) []string {
	return []string{m.Tmux, "-u", "-S", m.Socket(true), "display-popup", "-c", client, "-E", "-B",
		"-x", "0", "-y", "0", "-w", "100%", "-h", "100%", "-e", ModalEnv + "=" + width + "," + height + "," + client, command}
}

// ViewsControls belong to the views server: it contains clients, not programs.
func (m *Mux) ViewsControls(s Sizes) [][]string {
	cmds := commonOptions()
	cmds = append(cmds, terminalInput()...)
	// View windows keep their size during bursts of SSH client resizes; only the
	// stable geometry is applied.
	cmds = append(cmds,
		[]string{"set-hook", "-g", "client-resized", "run-shell -b " + Quote(m.Self("_resize_window", "--client", "#{hook_client}"))},
		[]string{"set-hook", "-g", "client-attached", "run-shell " + Quote(m.Self("_resize_window", "--client", "#{hook_client}", "--immediate"))},
		[]string{"set-hook", "-g", "client-session-changed", "run-shell " + Quote(m.Self("_resize_window", "--client", "#{hook_client}", "--immediate"))},
		[]string{"set-hook", "-gu", "client-detached"},
		m.zoomHook(),
		// The top bar: views as tabs and the New console, Arrange and More
		// menus. Window sizes leave its row (TopBarRows).
		setg("status", "on"), setg("status-position", "top"), setg("status-interval", "0"),
		setg("status-style", "fg="+theme.Colour(theme.HeaderFg)+",bg="+theme.Colour(theme.Panel)),
		setg("status-format[0]", topBar()), setg("prefix", "C-b"),
		// The header is inside each pane: the border is only a divider.
		setg("pane-border-status", "off"), []string{"set-option", "-gu", "pane-border-format"},
		setg("pane-active-border-style", "fg="+theme.Colour(theme.Accent)+",bg=#000000,bold"), setg("pane-border-lines", "heavy"),
		setg("remain-on-exit-format", "#[fg=black,bg=yellow,bold] "+i18n.T("tmux.panel_ended")+" #[default] "+i18n.T("tmux.panel_options")),
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
		// A finished program's Enter menu runs in the nested client. Remember
		// which outer client requested it so its dialogs return to that user.
		[]string{"bind-key", "-n", "Enter", "if-shell", "-F", "#{pane_dead}", m.deadPanelMenu("C"),
			Script([]string{"set-option", "-pF", "@ss_client", "#{client_name}"}, []string{"send-keys", "Enter"})},
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
	for _, b := range []struct{ key, action, region string }{{MenuKey, "menu", "menu"}, {"F3", "new", "new"},
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
	popups["rename"] = "run-shell " + Quote(m.PopupShell("rename", Fit("64", w), Fit("12", h)))
	// F2 renames the focused console; the explorer and the Minimized panel
	// receive it and rename their selection.
	cmds = append(cmds, []string{"bind-key", "-n", RenameKey, "if-shell", "-F", isConsole, popups["rename"], "send-keys " + RenameKey},
		// Shift+F10 is the context menu of the focused panel, as on desktops.
		[]string{"bind-key", "-n", ContextMenuKey, "run-shell", "-C", headerMenuKeyboard})
	// Ports are counted when opening: the program measures their popup.
	popups["ports"] = "run-shell " + Quote(m.Self("_dialog", "ports", "--client")+` "#{client_name}" --pane "#{pane_id}"`)
	popups["tags"] = "run-shell -b " + Quote(m.Self("_dialog", "tags", "--client")+` "#{client_name}" --pane "#{pane_id}"`)
	cmds = append(cmds, setg("@ss_views", itoa(s.Views)))
	cmds = append(cmds, m.topBarBindings(popups, s.TopMenus)...)
	cmds = append(cmds, m.headerMenus(popups)...)
	cmds = append(cmds, m.dragBindings()...)
	for _, l := range LayoutKeys {
		command := m.Self("_view", "layout", "--layout", l.Layout) + ` --client "#{client_name}" --pane "#{pane_id}"`
		cmds = append(cmds, []string{"bind-key", "-n", "M-" + l.Key, "run-shell", command}, []string{"bind-key", l.Key, "run-shell", command})
	}
	for _, key := range RetiredKeys {
		cmds = append(cmds, []string{"unbind-key", "-q", "-n", key})
	}
	for _, mv := range MoveKeys {
		cmds = append(cmds, []string{"bind-key", "-n", mv.Key, "run-shell", m.Self("_view", mv.Action) + ` --client "#{client_name}" --pane "#{pane_id}"`})
	}
	for _, d := range []struct{ key, dir string }{{"Left", "L"}, {"Right", "R"}, {"Up", "U"}, {"Down", "D"}} {
		cmds = append(cmds, []string{"bind-key", "-r", d.key, "resize-pane", "-" + d.dir, "5"})
	}
	for _, sw := range []struct {
		keys   []string
		action string
	}{{[]string{"C-S-Right", "F8", "C-NPage"}, "next"}, {[]string{"C-S-Left", "S-F8", "C-PPage"}, "previous"}} {
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
	var out [][]string
	// The mouse variant opens at the pointer; the keyboard one (Shift+F10)
	// at the lower left of the focused pane.
	for _, variant := range []struct {
		prefix string
		head   []string
	}{
		{menuOption, []string{"display-menu", "-O", "-T", "#[align=centre] #{@ss_name} ", "-t", "=", "-x", "M", "-y", "M"}},
		{menuOption + "kbd_", []string{"display-menu", "-O", "-T", "#[align=centre] #{@ss_name} ", "-x", "P", "-y", "P"}},
	} {
		out = append(out, m.paneMenus(variant.prefix, variant.head, popups, viewAction)...)
	}
	return out
}

func (m *Mux) paneMenus(prefix string, head []string, popups map[string]string, viewAction func(string) string) [][]string {
	console := append(append([]string{}, head...),
		i18n.T("tmux.rename"), "n", popups["rename"],
		i18n.T("console.description"), "d", m.RunAction(`describe --client "#{client_name}" --pane "#{pane_id}"`),
		i18n.T("console.tag")+" ▸", "t", m.RunAction(`tagmenu --client "#{client_name}" --pane "#{pane_id}"`),
		"",
		i18n.T("console.minimize"), "-", m.RunAction("minimize --pane #{pane_id}"),
		"#{?window_zoomed_flag,"+i18n.T("console.unmaximize")+","+i18n.T("console.maximize")+"} (F4)", "z", "resize-pane -Z",
		i18n.T("tmux.move_before"), "[", viewAction("move-before"),
		i18n.T("tmux.move_after"), "]", viewAction("move-after"),
		"",
		i18n.T("tmux.new_console"), "c", popups["new"],
		i18n.T("tmux.replace"), "r", popups["replace"],
		i18n.T("tmux.ports"), "p", popups["ports"],
		i18n.T("tmux.close_running"), "q", m.RunAction("remove --pane #{pane_id}"),
		i18n.T("tmux.kill_f9"), "m", popups["kill"],
		"",
		i18n.T("tmux.view_menu"), "v", popups["menu"])
	tree := append(append([]string{}, head...),
		i18n.T("tmux.search"), "/", "send-keys /",
		i18n.T("tmux.edit_nano"), "e", "send-keys E",
		i18n.T("tmux.rename_sel"), "n", "send-keys N",
		i18n.T("tmux.reload"), "r", "send-keys R",
		i18n.T("tmux.ports"), "p", popups["ports"],
		"",
		i18n.T("tmux.hide_explorer"), "o", m.RunAction("explorer --pane #{pane_id}"),
		"",
		i18n.T("tmux.view_menu"), "v", popups["menu"])
	empty := append(append([]string{}, head...),
		i18n.T("tmux.new_console"), "c", popups["new"],
		i18n.T("tmux.ports"), "p", popups["ports"],
		i18n.T("tmux.view_menu"), "v", popups["menu"])
	minimized := append(append([]string{}, head...),
		i18n.T("menu.tags"), "t", popups["tags"],
		"",
		i18n.T("menu.toggle_minimized"), "o", "run-shell -b "+Quote(m.Self("_view", "toggle-minimized")+` --client "#{client_name}" --pane "#{pane_id}"`),
		"",
		i18n.T("tmux.view_menu"), "v", popups["menu"])
	return [][]string{setg(prefix+"console", Join(console...)), setg(prefix+"tree", Join(tree...)), setg(prefix+"empty", Join(empty...)),
		setg(prefix+"minimized", Join(minimized...))}
}

// run-shell -C expands the format (chooses the menu for the pane) and keeps the
// mouse event: the menu appears where the click happened.
var headerMenu = Join("run-shell", "-C", paneMenuChoice(menuOption))

// headerMenuKeyboard is the format choosing the keyboard variant (Shift+F10).
var headerMenuKeyboard = paneMenuChoice(menuOption + "kbd_")

func paneMenuChoice(prefix string) string {
	return "#{?" + isConsole + ",#{" + prefix + "console}," +
		"#{?#{==:#{@ss_kind},explorer},#{" + prefix + "tree}," +
		"#{?#{==:#{@ss_kind},minimized},#{" + prefix + "minimized},#{" + prefix + "empty}}}}"
}

// deadPanelMenu are the options of a views pane whose program ended, at the
// pointer (M) or centered (C, from the keyboard).
func (m *Mux) deadPanelMenu(position string) string {
	args := []string{"display-menu", "-O", "-T", "#[align=centre] #{@ss_name} "}
	if position == "M" {
		args = append(args, "-t", "=")
	}
	return Join(append(args, "-x", position, "-y", position,
		i18n.T("tmux.redo_panel"), "r", "respawn-pane -k",
		i18n.T("tmux.close_panel"), "q", m.RunAction("remove --pane #{pane_id}"))...)
}

// zoomHook tells the console headers which pane of a view is zoomed: the
// header lives in the programs server, which does not see the view's zoom.
// A process runs only when the zoom flag of the window changes.
func (m *Mux) zoomHook() []string {
	return []string{"set-hook", "-g", "window-layout-changed", Script([]string{"if-shell", "-F",
		// Unset means not zoomed: a window's first layout change spawns nothing.
		"#{!=:#{window_zoomed_flag},#{?@ss_zoom_seen,#{@ss_zoom_seen},0}}",
		Script([]string{"set-option", "-wF", "@ss_zoom_seen", "#{window_zoomed_flag}"},
			[]string{"run-shell", "-b", m.Self("_action", "zoomed") + ` --pane "#{pane_id}"`})})}
}

// fromRight is the column of the pointer counted from the pane's right edge.
const fromRight = "#{e|-:#{e|-:#{pane_width},1},#{mouse_x}}"

// onButton is true when the pointer is on a header button (HeaderButtons).
func onButton(action string) string {
	for _, b := range HeaderButtons {
		if b.Action == action {
			return "#{&&:#{e|>=:" + fromRight + "," + itoa(b.Lo) + "},#{e|<=:" + fromRight + "," + itoa(b.Hi) + "}}"
		}
	}
	return "0"
}

// ownPanel is a pane drawn by ShellStudio itself, which reads the mouse.
var ownPanel = "#{||:#{==:#{@ss_kind},explorer},#{==:#{@ss_kind},minimized}}"

var onButtons = "#{e|<=:" + fromRight + "," + itoa(HeaderButtons[len(HeaderButtons)-1].Hi) + "}"

func (m *Mux) dragBindings() [][]string {
	drop := m.Self("_view", "drop") + ` --origin "#{` + dragPane + `}" --target "#{pane_id}" --client "#{client_name}"`
	move := m.Self("_action", "move") + ` --pane "#{` + dragPane + `}" --target "#{pane_id}" --client "#{client_name}"`
	ontoOtherConsole := "#{&&:" + dragIs("console") + ",#{&&:" + isConsole + ",#{!=:#{pane_id},#{" + dragPane + "}}}}"
	// Capture both IDs before any movement. The action stores the order and
	// swaps native panes under the render lock, preserving hand-adjusted sizes.
	swap := Script([]string{"run-shell", "-b", move})
	// tmux owns the original mouse position, divider and drag lifecycle.
	// Border formats omit mouse_x/mouse_y; inferring the axis from a later
	// pane event chooses the wrong edge at junctions and on short rows.
	var resizeBindings [][]string
	for _, key := range []string{"MouseDown1Border", "SecondClick1Border", "TripleClick1Border"} {
		resizeBindings = append(resizeBindings, []string{"bind-key", "-n", key, Script(dragReset)})
	}
	resizeBindings = append(resizeBindings, []string{"bind-key", "-n", "MouseDrag1Border", "resize-pane", "-M"})
	selectPane := []string{"select-pane", "-t", "="}
	out := [][]string{
		// A left click on an ended panel only focuses it; its options open
		// with Enter or a right click.
		{"bind-key", "-n", "MouseDown1Pane", "if-shell", "-F", "#{pane_dead}",
			Script(selectPane, dragReset),
			Script([]string{"if-shell", "-F", onHeader,
				// The header is not forwarded: it belongs to the view, not the
				// program. Its buttons act at once; elsewhere a press arms the
				// console drag (or the menu, on release without moving).
				Script([]string{"if-shell", "-F", "#{&&:" + isConsole + "," + onButtons + "}",
					Script(selectPane, dragReset, []string{"if-shell", "-F", onButton("menu"), headerMenu,
						Script([]string{"if-shell", "-F", onButton("maximize"), "resize-pane -Z -t =",
							m.RunAction("minimize --pane #{pane_id}")})}),
					Script(selectPane,
						[]string{"set-option", "-gF", dragClient, "#{client_name}"},
						[]string{"set-option", "-gF", dragPane, "#{pane_id}"},
						[]string{"set-option", "-gF", dragKind, "#{?" + isConsole + ",console,header}"})}),
				// tmux default plus the origin: only the explorer arms a path drag,
				// and not on its scrollbar (the last column but one).
				Script(selectPane, []string{"send-keys", "-M"},
					[]string{"set-option", "-pF", "@ss_client", "#{client_name}"},
					[]string{"set-option", "-gF", dragClient, "#{?#{&&:#{==:#{@ss_kind},explorer},#{!=:#{mouse_x},#{e|-:#{pane_width},2}}},#{client_name},}"},
					[]string{"set-option", "-gF", dragPane, "#{pane_id}"},
					[]string{"set-option", "-g", dragKind, "path"})})},
		// Pressing and releasing the header without moving: its menu.
		{"bind-key", "-n", "MouseUp1Pane", "if-shell", "-F", "#{&&:" + dragging + ",#{!=:#{" + dragKind + "},path}}",
			Script(dragReset, []string{"if-shell", "-F", "#{&&:" + onHeader + ",#{==:#{pane_id},#{" + dragPane + "}}}", headerMenu}),
			Script(dragReset, []string{"send-keys", "-M"})},
		{"bind-key", "-n", "MouseDown3Pane", "if-shell", "-F", "#{pane_dead}",
			Script(selectPane, dragReset) + " ; " + m.deadPanelMenu("M"),
			Script([]string{"if-shell", "-F", onHeader,
				Script(selectPane, dragReset) + " ; " + headerMenu,
				Script(selectPane, []string{"send-keys", "-M"})})},
		{"bind-key", "-n", "MouseDrag1Pane", "if-shell", "-F", dragging,
			Script([]string{"if-shell", "-F", dragIs("path"),
				Script([]string{"display-message", "-d", "1500", i18n.T("tmux.drop_path")}),
				Script([]string{"if-shell", "-F", dragIs("console"),
					Script([]string{"display-message", "-d", "1500", i18n.T("tmux.drop_swap")})})}),
			// The view selects the text: forwarding the drag to the nested tmux
			// depends on the program's momentary mouse mode and can end up typing
			// SGR sequences on its input line. Our own panels (explorer,
			// Minimized) take the drag: their scrollbars follow it.
			"if-shell -F " + Quote(ownPanel) + " 'send-keys -M' 'copy-mode -M'"},
		{"bind-key", "-n", "MouseDragEnd1Pane", "if-shell", "-F", dragging,
			Script([]string{"if-shell", "-F", dragIs("path"), Script([]string{"run-shell", drop}),
				Script([]string{"if-shell", "-F", ontoOtherConsole, swap})}, dragReset),
			"send-keys -M"},
	}
	// Dropping on a divider or the status line types nothing.
	// Ending a divider drag saves the sizes of the left column.
	saveSizes := m.RunAction(`panel-sizes --pane "#{pane_id}"`)
	for _, where := range []string{"Border", "Status"} {
		out = append(out, []string{"bind-key", "-n", "MouseUp1" + where, Script(dragReset)})
		notDragging := Script(dragReset)
		if where == "Border" {
			notDragging = Script(dragReset) + " ; " + saveSizes
		}
		out = append(out, []string{"bind-key", "-n", "MouseDragEnd1" + where, "if-shell", "-F", dragging,
			Script([]string{"if-shell", "-F", dragIs("path"), Script([]string{"run-shell", m.Self("_view", "drop") + ` --origin "#{` + dragPane + `}"`})}, dragReset),
			notDragging})
	}
	// tmux double and triple click (select a word or line and copy it), except in
	// the explorer: there a double click opens the file, and the default
	// select-pane would steal the focus from the new console.
	for _, k := range []struct{ key, selection string }{{"DoubleClick1Pane", "select-word"}, {"TripleClick1Pane", "select-line"}} {
		out = append(out, []string{"bind-key", "-n", k.key, "if-shell", "-F", onHeader,
			Script(dragReset), Script(dragReset, []string{"if-shell", "-F", "#{!=:#{@ss_kind},explorer}",
				Script(selectPane, []string{"if-shell", "-F", "#{||:#{pane_in_mode},#{mouse_any_flag}}", "send-keys -M",
					Script([]string{"copy-mode", "-H"}, []string{"send-keys", "-X", k.selection}, []string{"run-shell", "-d", "0.3"},
						[]string{"send-keys", "-X", "copy-pipe-and-cancel"})})})})
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
