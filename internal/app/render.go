// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
	"shellstudio/internal/diagnostics"
	"shellstudio/internal/i18n"
	"shellstudio/internal/mux"
	"shellstudio/internal/platform"
	"shellstudio/internal/store"
)

// ConfigurePrograms applies the header and finished-console controls to a
// running programs server, and names its sessions after their consoles.
func (a *App) ConfigurePrograms(force bool) error {
	cur, e := a.Mux.Run(false, "show-option", "-gqv", "@ss_config")
	if mux.NoServer(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if strings.TrimSpace(cur) == a.Mux.Version && !force {
		return nil
	}
	cmds := a.Mux.ProgramsControls()
	programs, _ := a.Mux.Programs()
	cs, e := a.Store.Consoles("")
	if e != nil {
		return e
	}
	for _, c := range cs {
		if _, ok := programs[c.ID]; ok {
			cmds = append(cmds, a.Mux.HeaderCommands(c)...)
		}
	}
	_, e = a.Mux.Batch(false, cmds...)
	return e
}

func (a *App) ConfigureViews() error {
	vs, e := a.Store.Views()
	if e != nil {
		return e
	}
	s := a.DialogSizes()
	s.Views = len(vs)
	cmds := a.Mux.ViewsControls(s)
	// The language of this workspace: see adoptServerLanguage.
	cmds = append(cmds, []string{"set-option", "-g", "@ss_lang", i18n.Lang()},
		[]string{"set-option", "-g", "@ss_tabs", mux.TabsFormat(sortViews(vs), 0)},
		[]string{"set-option", "-g", "@ss_tabs_short", mux.TabsFormat(sortViews(vs), 10)})
	if _, e = a.Mux.Batch(true, cmds...); e != nil {
		return e
	}
	a.fitAttachedWindows()
	return nil
}

// fitAttachedWindows sizes the windows of attached views to their clients.
// View windows are manually sized: a server configured by an older release,
// without the top bar, keeps windows one row too tall after an upgrade, and
// the bar would hide their last row until the terminal was resized.
func (a *App) fitAttachedWindows() {
	o, e := a.Mux.Run(true, "list-clients", "-F", "#{client_session}\t#{client_width}\t#{client_height}\t#{window_height}")
	if e != nil {
		return
	}
	var cmds [][]string
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(o), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 4 || !strings.HasPrefix(f[0], "v-") || seen[f[0]] {
			continue
		}
		seen[f[0]] = true
		h, _ := strconv.Atoi(f[2])
		if want := strconv.Itoa(WindowHeight(h)); f[3] != want && h > 0 {
			cmds = append(cmds, []string{"resize-window", "-t", "=" + f[0] + ":", "-x", f[1], "-y", want})
		}
	}
	if len(cmds) > 0 {
		a.Mux.Batch(true, cmds...)
	}
}

// Sync renders a view, creating its session if needed. Renders queue behind
// each other: two shortcuts at once never build the same panes twice.
func (a *App) Sync(view string) (result error) {
	started := time.Now()
	defer func() { diagnostics.Record(diagnostics.Entry{Event: "view-sync", Reference: view}, result, started) }()
	l, e := platform.LockWait(filepath.Join(a.Paths.Runtime, "render.lock"), 10*time.Second)
	if e != nil {
		return e
	}
	defer platform.Unlock(l)
	return a.render(view)
}

// RefreshOpen re-renders only the views that have a session in the views server.
func (a *App) RefreshOpen(views ...string) error {
	var first error
	seen := map[string]bool{}
	for _, v := range views {
		if seen[v] {
			continue
		}
		seen[v] = true
		if _, e := a.Mux.Run(true, "has-session", "-t", "="+mux.ViewSession(v)); e != nil {
			continue
		}
		if e := a.Sync(v); e != nil && first == nil {
			first = e
		}
	}
	return first
}

type pane struct{ id, dead, state string }

// TerminalSize of the calling process, for windows created before attaching.
func TerminalSize() (int, int) {
	for _, f := range []*os.File{os.Stdout, os.Stdin, os.Stderr} {
		if w, h, e := term.GetSize(f.Fd()); e == nil && w > 0 && h > 0 {
			return w, h
		}
	}
	return 160, 48
}

// render moves the explorer aside while consoles are rebuilt, then joins it
// again at full height on the left.
func (a *App) render(view string) error {
	v, e := a.Store.View(view)
	if e != nil {
		return fmt.Errorf("%s: %w", i18n.T("err.view_gone"), e)
	}
	session := mux.ViewSession(view)
	rows, e := a.Mux.Run(true, "list-panes", "-s", "-t", "="+session, "-F",
		// A disconnected console counts as dead: it is reconnected.
		"#{pane_id}\t#{@ss_kind}\t#{@ss_console}\t#{?#{"+mux.Disconnected+"},1,#{pane_dead}}\t"+
			"#{pane_active}\t#{window_active}\t#{window_id}\t#{@ss_config}\t#{@ss_name}\t#{@ss_state}")
	// list-panes reports a missing session as either a session or window
	// target error, depending on the tmux target resolution context.
	missing := e != nil && (strings.HasPrefix(e.Error(), "can't find session:") || strings.HasPrefix(e.Error(), "can't find window:"))
	if e != nil && !mux.NoServer(e) && !missing {
		return fmt.Errorf("%s: %w", i18n.T("err.read_panels"), e)
	}
	exists := e == nil
	explorer, focused, version := "", "", ""
	explorerDead := false
	minimized, minimizedDead := "", false
	var duplicates []string
	var order []string
	panes := map[string]pane{}
	labels := map[string]string{}
	stale := map[string]bool{}
	for _, line := range strings.Split(strings.TrimRight(rows, "\n"), "\n") {
		f := strings.Split(line, "\t")
		if !exists {
			continue
		}
		if len(f) != 10 {
			return errors.New(i18n.T("err.panel_metadata"))
		}
		version = f[7]
		if f[5] != "1" {
			// A view is a single window. Another one can only be the remains of
			// an interrupted render: hidden nested clients that still count.
			stale[f[6]] = true
		} else if f[1] == "explorer" {
			if explorer != "" {
				if f[4] != "1" {
					duplicates = append(duplicates, f[0])
					continue
				}
				duplicates = append(duplicates, explorer)
			}
			explorer, explorerDead = f[0], f[3] == "1"
		} else if f[1] == "minimized" {
			// The Minimized panel, like the explorer, is kept by kind: without
			// this branch it would count as an empty pane and be killed.
			if minimized != "" {
				if f[4] != "1" {
					duplicates = append(duplicates, f[0])
					continue
				}
				duplicates = append(duplicates, minimized)
			}
			minimized, minimizedDead = f[0], f[3] == "1"
		} else {
			key := f[2]
			if f[1] != "console" {
				key = "empty"
			}
			if previous, dup := panes[key]; dup {
				// Keep the focused presentation when recovering an older or
				// interrupted render. Only its redundant client is removed;
				// the program lives on the separate programs server.
				if f[4] != "1" {
					duplicates = append(duplicates, f[0])
					continue
				}
				duplicates = append(duplicates, previous.id)
			} else {
				order = append(order, key)
			}
			panes[key] = pane{f[0], f[3], f[9]}
			labels[key] = f[8]
		}
		if f[4] == "1" && f[5] == "1" {
			focused = f[0]
		}
	}
	if len(duplicates) > 0 {
		var kill [][]string
		for _, p := range duplicates {
			kill = append(kill, []string{"kill-pane", "-t", p})
		}
		if _, e := a.Mux.Batch(true, kill...); e != nil {
			return e
		}
	}
	if len(stale) > 0 {
		var kill [][]string
		for w := range stale {
			kill = append(kill, []string{"kill-window", "-t", w})
		}
		a.Mux.Batch(true, kill...)
	}
	members, e := a.Store.Consoles(view)
	if e != nil {
		return e
	}
	programs, e := a.Mux.Programs()
	if e != nil {
		return e
	}
	state := func(id string) string {
		if _, ok := programs[id]; ok {
			return "live"
		}
		return "stopped"
	}
	if exists && len(stale) == 0 && len(duplicates) == 0 && version == a.Mux.Version {
		wanted := []string{"empty"}
		if len(members) > 0 {
			wanted = nil
			for _, c := range members {
				wanted = append(wanted, c.ID)
			}
		}
		info, e := a.Mux.Run(true, "display-message", "-p", "-t", "="+session+":", "#{window_width}\t#{@ss_layout}")
		if e == nil {
			cols, applied, _ := strings.Cut(strings.TrimRight(info, "\n"), "\t")
			width, _ := strconv.Atoi(cols)
			same := strings.Join(order, " ") == strings.Join(wanted, " ")
			var rename [][]string
			for _, c := range members {
				p := panes[c.ID]
				same = same && p.dead != "1" && p.state == state(c.ID)
				if labels[c.ID] != c.Name {
					rename = append(rename, []string{"set-option", "-p", "-t", p.id, "@ss_name", c.Name},
						[]string{"select-pane", "-t", p.id, "-T", c.Name})
				}
			}
			wantExplorer, wantMinimized := leftColumn(v, width)
			if same && (explorer != "") == wantExplorer && !explorerDead && (minimized != "") == wantMinimized && !minimizedDead && applied == v.Layout {
				// Labels do not change geometry. Rebuilding here discarded manual
				// divider sizes whenever a console was renamed in any view.
				if len(rename) > 0 {
					_, e = a.Mux.Batch(true, rename...)
					return e
				}
				return nil
			}
		}
	}
	if explorer != "" {
		if _, e = a.Mux.Run(true, "break-pane", "-d", "-s", explorer, "-n", "_files"); e != nil {
			return e
		}
	}
	if minimized != "" {
		if _, e = a.Mux.Run(true, "break-pane", "-d", "-s", minimized, "-n", "_minimized"); e != nil {
			return e
		}
	}
	re := a.renderConsoles(v, exists, order, panes, members, state, focused, version)
	// Even if an update fails, never leave an orphan auxiliary window.
	info, e := a.Mux.Run(true, "display-message", "-p", "-t", "="+session+":", "#{pane_id}\t#{window_width}\t#{window_height}")
	if e != nil {
		if re != nil {
			return re
		}
		return e
	}
	f := strings.Split(strings.TrimSpace(info), "\t")
	if len(f) != 3 {
		return errors.New(i18n.T("err.window_metadata"))
	}
	target := f[0]
	width, _ := strconv.Atoi(f[1])
	height, _ := strconv.Atoi(f[2])
	wantExplorer, wantMinimized := leftColumn(v, width)
	size := strconv.Itoa(LeftWidth(v, width))
	var cmds [][]string
	// The left column: the explorer on top, the Minimized panel under it, or
	// either one alone at full height.
	top := ""
	if wantExplorer {
		if explorer != "" {
			cmds = append(cmds, []string{"join-pane", "-d", "-f", "-h", "-b", "-l", size, "-s", explorer, "-t", target})
			// A new executable reloads the tree, which is a long-running process.
			if explorerDead || version != a.Mux.Version {
				cmds = append(cmds, []string{"respawn-pane", "-k", "-t", explorer, "exec " + a.Mux.Self("_explorer", view)})
			}
		} else {
			o, e := a.Mux.Run(true, "split-window", "-d", "-f", "-h", "-b", "-l", size, "-t", target, "-P", "-F", "#{pane_id}",
				"exec "+a.Mux.Self("_explorer", view))
			if e != nil {
				return e
			}
			explorer = strings.TrimSpace(o)
			cmds = append(cmds, []string{"set-option", "-p", "-t", explorer, "@ss_kind", "explorer"},
				[]string{"set-option", "-p", "-t", explorer, "@ss_console", "explorer"},
				[]string{"set-option", "-p", "-t", explorer, "@ss_name", "Files"})
		}
		top = explorer
	} else if explorer != "" {
		a.Mux.Run(true, "kill-pane", "-t", explorer)
		explorer = ""
	}
	if wantMinimized {
		// Under the explorer (vertical split of it) or alone on the left.
		join := []string{"-d", "-v", "-l", strconv.Itoa(MinimizedHeight(v, height)), "-t", top}
		if top == "" {
			join = []string{"-d", "-f", "-h", "-b", "-l", size, "-t", target}
		}
		if len(cmds) > 0 && top != "" {
			// The explorer must be in place before splitting it.
			if _, e = a.Mux.Batch(true, cmds...); e != nil {
				return e
			}
			cmds = nil
		}
		if minimized != "" {
			cmds = append(cmds, append(append([]string{"join-pane"}, join...), "-s", minimized))
			if minimizedDead || version != a.Mux.Version {
				cmds = append(cmds, []string{"respawn-pane", "-k", "-t", minimized, "exec " + a.Mux.Self("_minimized", view)})
			}
		} else {
			args := append(append([]string{"split-window"}, join...), "-P", "-F", "#{pane_id}", "exec "+a.Mux.Self("_minimized", view))
			o, e := a.Mux.Run(true, args...)
			if e != nil {
				return e
			}
			minimized = strings.TrimSpace(o)
			cmds = append(cmds, []string{"set-option", "-p", "-t", minimized, "@ss_kind", "minimized"},
				[]string{"set-option", "-p", "-t", minimized, "@ss_console", "minimized"},
				[]string{"set-option", "-p", "-t", minimized, "@ss_name", "Minimized"})
		}
	} else if minimized != "" {
		a.Mux.Run(true, "kill-pane", "-t", minimized)
		minimized = ""
	}
	if focused != "" && (focused == explorer || focused == minimized) {
		cmds = append(cmds, []string{"select-pane", "-t", focused})
	}
	if len(cmds) > 0 {
		if _, e = a.Mux.Batch(true, cmds...); e != nil {
			return e
		}
	}
	return re
}

// leftColumn says which panels the left column of a view shows at a window
// width: none below 70 columns.
func leftColumn(v store.View, width int) (explorer, minimized bool) {
	if width < 70 {
		return false, false
	}
	return v.Explorer, v.MinimizedPanel
}

// LeftWidth is the width of the left column: the saved one, kept readable
// and below half the window, or by default a quarter of the window up to 32
// columns; a third up to 48 when it holds the Minimized table.
func LeftWidth(v store.View, width int) int {
	if v.ExplorerWidth > 0 {
		return max(20, min(v.ExplorerWidth, width/2))
	}
	if v.MinimizedPanel && width >= 70 {
		return max(min(32, width/4), min(48, width/3))
	}
	return min(32, width/4)
}

// MinimizedHeight is the height of the Minimized panel under the explorer:
// the saved one, or 45% of the window, keeping at least 8 rows for it and 6
// for the explorer.
func MinimizedHeight(v store.View, height int) int {
	h := height * 45 / 100
	if v.MinimizedHeight > 0 {
		h = v.MinimizedHeight
	}
	return max(min(8, height/2), min(h, height-7))
}

// renderConsoles rebuilds the console panes with few batched invocations.
// Panes are reused by console ID: neither nested clients nor programs restart.
func (a *App) renderConsoles(v store.View, exists bool, order []string, panes map[string]pane, members []store.Console,
	state func(string) string, focusedPane, version string) error {
	session := mux.ViewSession(v.ID)
	if len(programsOf(members, state)) > 0 {
		if e := a.ConfigurePrograms(false); e != nil {
			return e
		}
	}
	cols, lines := TerminalSize()
	type item struct{ key, name, state string }
	items := []item{{"empty", i18n.T("ui.empty.name"), ""}}
	if len(members) > 0 {
		items = nil
		for _, c := range members {
			items = append(items, item{c.ID, c.Name, state(c.ID)})
		}
	}
	command := func(it item) string {
		switch {
		case it.key == "empty":
			return "exec " + a.Mux.Self("_empty")
		case it.state == "live":
			return a.Mux.AttachCommand(it.key)
		}
		return "exec " + a.Mux.Self("_disconnected")
	}
	remaining := append([]string{}, order...)
	take := func(key string) {
		for i, k := range remaining {
			if k == key {
				remaining = append(remaining[:i], remaining[i+1:]...)
				return
			}
		}
	}
	var cmds [][]string
	type placed struct {
		item
		pane string
	}
	var place []placed
	for _, it := range items {
		cur, ok := panes[it.key]
		if ok {
			take(it.key)
		}
		// Reusing a pane avoids needing room to split before removing it.
		if !ok && len(members) == 0 && len(remaining) > 0 {
			old := remaining[0]
			take(old)
			cur, ok = panes[old], true
			cmds = append(cmds, mux.Respawn(cur.id, command(it))...)
			cur.dead, cur.state = "0", it.state
		} else if !ok && contains(remaining, "empty") {
			take("empty")
			cur, ok = panes["empty"], true
			cmds = append(cmds, mux.Respawn(cur.id, command(it))...)
			cur.dead, cur.state = "0", it.state
		}
		p := ""
		switch {
		case ok && cur.dead != "1" && cur.state == it.state:
			p = cur.id
		case ok:
			p = cur.id
			cmds = append(cmds, mux.Respawn(p, command(it))...)
		case !exists:
			// The new pane identifier arrives in the output, in order.
			cmds = append(cmds, []string{"new-session", "-d", "-P", "-F", "#{pane_id}", "-s", session,
				"-x", strconv.Itoa(cols), "-y", strconv.Itoa(WindowHeight(lines)), "-n", "view", command(it)})
			exists = true
		default:
			// Widening the canvas before splitting lets detached views be prepared.
			cmds = append(cmds, []string{"select-layout", "-t", "=" + session + ":", "tiled"},
				[]string{"split-window", "-d", "-P", "-F", "#{pane_id}", "-t", "=" + session + ":", command(it)})
		}
		place = append(place, placed{it, p})
	}
	var created []string
	if len(cmds) > 0 {
		o, e := a.Mux.Batch(true, cmds...)
		if e != nil {
			return e
		}
		created = strings.Fields(o)
	}
	cmds = nil
	for i := range place {
		if place[i].pane == "" {
			if len(created) == 0 {
				return errors.New(i18n.T("err.no_new_pane"))
			}
			place[i].pane, created = created[0], created[1:]
		}
		p := place[i]
		kind := "console"
		if p.key == "empty" {
			kind = "empty"
		}
		cmds = append(cmds, []string{"set-option", "-p", "-t", p.pane, "@ss_kind", kind},
			[]string{"set-option", "-p", "-t", p.pane, "@ss_console", p.key},
			[]string{"set-option", "-p", "-t", p.pane, "@ss_name", p.name},
			[]string{"set-option", "-p", "-t", p.pane, "@ss_state", p.state},
			[]string{"select-pane", "-t", p.pane, "-T", p.name})
	}
	for _, key := range remaining {
		cmds = append(cmds, []string{"kill-pane", "-t", panes[key].id})
	}
	cmds = append(cmds, []string{"list-panes", "-t", "=" + session + ":", "-F", "#{pane_id}"})
	o, e := a.Mux.Batch(true, cmds...)
	if e != nil {
		return e
	}
	// Respect the saved order without restarting clients or programs.
	actual := strings.Fields(o)
	cmds = nil
	for i, p := range place {
		at := indexOf(actual, p.pane)
		if at >= 0 && at != i && i < len(actual) {
			cmds = append(cmds, []string{"swap-pane", "-d", "-s", p.pane, "-t", actual[i]})
			actual[i], actual[at] = actual[at], actual[i]
		}
	}
	if indexOf(actual, focusedPane) >= 0 {
		cmds = append(cmds, []string{"select-pane", "-t", focusedPane})
	}
	var consolePanes []string
	for _, p := range place {
		if p.key != "empty" {
			consolePanes = append(consolePanes, p.pane)
		}
	}
	cmds = append(cmds, mux.BlurInactive(consolePanes)...)
	layout := v.Layout
	if !mux.ValidLayout(layout) {
		layout = "tiled"
	}
	apply := []string{"select-layout", "-t", "=" + session + ":", layout}
	if version == a.Mux.Version {
		if _, e = a.Mux.Batch(true, append(cmds, apply)...); e != nil {
			return e
		}
	} else {
		if _, e = a.Mux.Batch(true, cmds...); e != nil {
			return e
		}
		// Borders change the usable height of each pane: lay out afterwards.
		if e = a.ConfigureViews(); e != nil {
			return e
		}
		if _, e = a.Mux.Run(true, apply...); e != nil {
			return e
		}
	}
	_, e = a.Mux.Batch(true, []string{"set-option", "-w", "-t", "=" + session + ":", "@ss_layout", v.Layout},
		[]string{"set-option", "-w", "-t", "=" + session + ":", "window-size", "manual"})
	return e
}

func programsOf(cs []store.Console, state func(string) string) []string {
	var out []string
	for _, c := range cs {
		if state(c.ID) == "live" {
			out = append(out, c.ID)
		}
	}
	return out
}
func contains(list []string, s string) bool { return indexOf(list, s) >= 0 }
func indexOf(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return -1
}

// InsideViews reports whether this process runs inside a view (TMUX of views.sock).
func (a *App) InsideViews() bool {
	t := os.Getenv("TMUX")
	if i := strings.Index(t, ","); i >= 0 {
		t = t[:i]
	}
	return t != "" && t == a.Mux.Socket(true)
}

// ConsoleClient finds the view client showing this console, when ShellStudio
// is started from a terminal console: that client switches instead of nesting.
func (a *App) ConsoleClient(id string) string {
	o, e := a.Mux.Run(true, "list-clients", "-F", "#{client_name}\t#{pane_id}")
	if e != nil {
		return ""
	}
	p, e := a.Mux.Run(true, "list-panes", "-a", "-F", "#{pane_id}\t#{@ss_console}")
	if e != nil {
		return ""
	}
	owner := map[string]string{}
	for _, l := range strings.Split(strings.TrimSpace(p), "\n") {
		pid, c, _ := strings.Cut(l, "\t")
		owner[pid] = c
	}
	for _, l := range strings.Split(strings.TrimSpace(o), "\n") {
		client, pid, _ := strings.Cut(l, "\t")
		if owner[pid] == id {
			return client
		}
	}
	return ""
}

// Open shows a view: switching an existing client, or replacing this process
// with a tmux client of the views server. No UI of ours ever runs on the outer
// terminal, so nothing can probe it and leak the answers into a console.
func (a *App) Open(view, client string) error {
	if e := a.Sync(view); e != nil {
		return e
	}
	a.RememberView(view)
	session := mux.ViewSession(view)
	if client == "" && os.Getenv("SHELLSTUDIO_CONSOLE") != "" {
		client = a.ConsoleClient(os.Getenv("SHELLSTUDIO_CONSOLE"))
		if client == "" {
			return errors.New(i18n.T("err.already_open"))
		}
	}
	if client != "" || a.InsideViews() {
		args := []string{"switch-client", "-t", "=" + session}
		if client != "" {
			args = []string{"switch-client", "-c", client, "-t", "=" + session}
		}
		_, e := a.Mux.Run(true, args...)
		return e
	}
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		return errors.New(i18n.T("err.needs_terminal"))
	}
	a.Close()
	env := []string{}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TMUX=") && !strings.HasPrefix(kv, "TMUX_PANE=") {
			env = append(env, kv)
		}
	}
	diagnostics.Record(diagnostics.Entry{Event: "view-attach", Reference: view}, nil, time.Time{})
	return syscall.Exec(a.Mux.Tmux, []string{"tmux", "-u", "-S", a.Mux.Socket(true), "attach-session", "-t", "=" + session}, env)
}

// Focus resolves a client's pane. A navigable popup can lose its pane ID while
// the view redraws; only non-destructive actions may fall back to the active one.
type Focus struct{ View, Pane, Kind, Console, Name string }

func (a *App) Focus(client, paneID string, staleFallback bool) (Focus, error) {
	session := ""
	if client != "" {
		o, e := a.Mux.Run(true, "list-clients", "-F", "#{client_name}\t#{session_name}")
		if e != nil {
			return Focus{}, e
		}
		for _, l := range strings.Split(strings.TrimSpace(o), "\n") {
			c, s, _ := strings.Cut(l, "\t")
			if c == client {
				session = s
			}
		}
		if session == "" {
			return Focus{}, errors.New(i18n.T("err.client_gone"))
		}
	} else {
		v, e := a.LastView()
		if e != nil {
			return Focus{}, e
		}
		session = mux.ViewSession(v.ID)
	}
	o, e := a.Mux.Run(true, "list-panes", "-t", "="+session+":", "-F", "#{pane_id}\t#{pane_active}\t#{@ss_kind}\t#{@ss_console}\t#{@ss_name}")
	if e != nil {
		return Focus{}, e
	}
	view := strings.TrimPrefix(session, "v-")
	var active *Focus
	for _, l := range strings.Split(strings.TrimSpace(o), "\n") {
		f := strings.Split(l, "\t")
		if len(f) != 5 {
			continue
		}
		cur := Focus{view, f[0], f[2], f[3], f[4]}
		if f[2] != "console" {
			cur.Console = ""
		}
		if f[1] == "1" {
			x := cur
			active = &x
		}
		if (paneID != "" && f[0] == paneID) || (paneID == "" && f[1] == "1") {
			return cur, nil
		}
	}
	if staleFallback && client != "" && active != nil {
		return *active, nil
	}
	return Focus{}, errors.New(i18n.T("err.panel_not_member"))
}

// FocusConsole moves the focus of a view to one of its consoles.
func (a *App) FocusConsole(view, id string) {
	o, e := a.Mux.Run(true, "list-panes", "-t", "="+mux.ViewSession(view)+":", "-F", "#{pane_id}\t#{@ss_console}")
	if e != nil {
		return
	}
	for _, l := range strings.Split(strings.TrimSpace(o), "\n") {
		p, c, _ := strings.Cut(l, "\t")
		if c == id {
			a.Mux.Run(true, "select-pane", "-t", p)
			if c, e := a.Store.Console(id); e == nil && (c.Extension == "claude" || c.Extension == "codex") {
				a.RememberKind(c.Extension)
			}
			return
		}
	}
}

// PaneOwner returns the view, kind, console and name of a views-server pane.
func (a *App) PaneOwner(p string) (Focus, error) {
	if !strings.HasPrefix(p, "%") {
		return Focus{}, errors.New(i18n.T("err.invalid_panel"))
	}
	o, e := a.Mux.Run(true, "display-message", "-p", "-t", p, "#{session_name}\t#{@ss_kind}\t#{@ss_console}\t#{@ss_name}")
	f := strings.Split(strings.TrimRight(o, "\n"), "\t")
	if e != nil || len(f) != 4 || !strings.HasPrefix(f[0], "v-") {
		return Focus{}, errors.New(i18n.T("err.panel_orphan"))
	}
	out := Focus{strings.TrimPrefix(f[0], "v-"), p, f[1], f[2], f[3]}
	if f[1] != "console" {
		out.Console = ""
	}
	return out, nil
}
func (a *App) PaneByTTY(tty string) (string, error) {
	o, _ := a.Mux.Run(true, "list-panes", "-a", "-F", "#{pane_id}\t#{pane_tty}")
	for _, l := range strings.Split(strings.TrimSpace(o), "\n") {
		p, t, _ := strings.Cut(l, "\t")
		if t == tty {
			return p, nil
		}
	}
	return "", errors.New(i18n.T("err.not_in_view"))
}

// ClientForPane routes dialogs from a nested console to its enclosing view.
func (a *App) ClientForPane(pane string) (string, error) {
	f, e := a.PaneOwner(pane)
	if e != nil {
		return "", e
	}
	o, e := a.Mux.Run(true, "list-clients", "-t", "="+mux.ViewSession(f.View), "-F", "#{client_name}")
	if e != nil {
		return "", e
	}
	clients := strings.Fields(o)
	if len(clients) == 0 {
		return "", errors.New(i18n.T("err.no_client"))
	}
	// Mouse interaction records the originating outer client on the pane.
	last, _ := a.Mux.Run(true, "show-option", "-pqv", "-t", pane, "@ss_client")
	if contains(clients, strings.TrimSpace(last)) {
		return strings.TrimSpace(last), nil
	}
	return clients[0], nil
}

// SwitchView walks the views by name, circularly.
func (a *App) SwitchView(client string, delta int) error {
	o, e := a.Mux.Run(true, "list-clients", "-F", "#{client_name}\t#{session_name}")
	if e != nil {
		return e
	}
	current := ""
	for _, l := range strings.Split(strings.TrimSpace(o), "\n") {
		c, s, _ := strings.Cut(l, "\t")
		if c == client {
			current = strings.TrimPrefix(s, "v-")
		}
	}
	vs, e := a.SortedViews()
	if e != nil || len(vs) == 0 {
		return e
	}
	index := 0
	for i, v := range vs {
		if v.ID == current {
			index = i
		}
	}
	target := vs[((index+delta)%len(vs)+len(vs))%len(vs)]
	if target.ID != current {
		if e = a.Sync(target.ID); e != nil {
			return e
		}
		if _, e = a.Mux.Run(true, "switch-client", "-c", client, "-t", "="+mux.ViewSession(target.ID)); e != nil {
			return e
		}
	}
	a.RememberView(target.ID)
	return nil
}
