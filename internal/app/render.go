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
			cmds = append(cmds, []string{"set-option", "-t", "=c-" + c.ID + ":", "@ss_name", c.Name})
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
	_, e = a.Mux.Batch(true, a.Mux.ViewsControls(s)...)
	return e
}

// Sync renders a view, creating its session if needed. Renders queue behind
// each other: two shortcuts at once never build the same panes twice.
func (a *App) Sync(view string) error {
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
		return fmt.Errorf("the view no longer exists: %w", e)
	}
	session := mux.ViewSession(view)
	rows, e := a.Mux.Run(true, "list-panes", "-s", "-t", "="+session, "-F",
		// A disconnected console counts as dead: it is reconnected.
		"#{pane_id}\t#{@ss_kind}\t#{@ss_console}\t#{?#{"+mux.Disconnected+"},1,#{pane_dead}}\t"+
			"#{pane_active}\t#{window_active}\t#{window_id}\t#{@ss_config}\t#{@ss_name}\t#{@ss_state}")
	exists := e == nil
	explorer, focused, version := "", "", ""
	explorerDead := false
	var order []string
	panes := map[string]pane{}
	labels := map[string]string{}
	stale := map[string]bool{}
	for _, line := range strings.Split(strings.TrimRight(rows, "\n"), "\n") {
		f := strings.Split(line, "\t")
		if !exists || len(f) != 10 {
			continue
		}
		version = f[7]
		if f[5] != "1" {
			// A view is a single window. Another one can only be the remains of
			// an interrupted render: hidden nested clients that still count.
			stale[f[6]] = true
		} else if f[1] == "explorer" {
			explorer, explorerDead = f[0], f[3] == "1"
		} else {
			key := f[2]
			if f[1] != "console" {
				key = "empty"
			}
			if _, dup := panes[key]; !dup {
				order = append(order, key)
			}
			panes[key] = pane{f[0], f[3], f[9]}
			labels[key] = f[8]
		}
		if f[4] == "1" && f[5] == "1" {
			focused = f[0]
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
	if exists && len(stale) == 0 && version == a.Mux.Version {
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
			for _, c := range members {
				p := panes[c.ID]
				same = same && p.dead != "1" && labels[c.ID] == c.Name && p.state == state(c.ID)
			}
			if same && (explorer != "") == (v.Explorer && width >= 70) && !explorerDead && applied == v.Layout {
				return nil
			}
		}
	}
	if explorer != "" {
		if _, e = a.Mux.Run(true, "break-pane", "-d", "-s", explorer, "-n", "_files"); e != nil {
			return e
		}
	}
	re := a.renderConsoles(v, exists, order, panes, members, state, focused, version)
	// Even if an update fails, never leave an orphan auxiliary window.
	info, e := a.Mux.Run(true, "display-message", "-p", "-t", "="+session+":", "#{pane_id}\t#{window_width}")
	if e != nil {
		if re != nil {
			return re
		}
		return e
	}
	target, cols, _ := strings.Cut(strings.TrimSpace(info), "\t")
	width, _ := strconv.Atoi(cols)
	if v.Explorer && width >= 70 {
		size := strconv.Itoa(min(32, width/4))
		var cmds [][]string
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
		if focused == explorer {
			cmds = append(cmds, []string{"select-pane", "-t", explorer})
		}
		if _, e = a.Mux.Batch(true, cmds...); e != nil {
			return e
		}
	} else if explorer != "" {
		a.Mux.Run(true, "kill-pane", "-t", explorer)
	}
	return re
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
	items := []item{{"empty", "Empty view", ""}}
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
				"-x", strconv.Itoa(cols), "-y", strconv.Itoa(lines), "-n", "view", command(it)})
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
				return errors.New("tmux did not report a new pane")
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
			return errors.New("ShellStudio is already open in this terminal; use F7 to change views")
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
		return errors.New("opening a view needs a terminal")
	}
	a.Close()
	env := []string{}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TMUX=") && !strings.HasPrefix(kv, "TMUX_PANE=") {
			env = append(env, kv)
		}
	}
	return syscall.Exec(a.Mux.Tmux, []string{"tmux", "-S", a.Mux.Socket(true), "attach-session", "-t", "=" + session}, env)
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
			return Focus{}, errors.New("the client is no longer connected")
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
	return Focus{}, errors.New("the selected panel no longer belongs to this view")
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
		return Focus{}, errors.New("invalid panel")
	}
	o, e := a.Mux.Run(true, "display-message", "-p", "-t", p, "#{session_name}\t#{@ss_kind}\t#{@ss_console}\t#{@ss_name}")
	f := strings.Split(strings.TrimRight(o, "\n"), "\t")
	if e != nil || len(f) != 4 || !strings.HasPrefix(f[0], "v-") {
		return Focus{}, errors.New("the panel no longer belongs to a view")
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
	return "", errors.New("this console is not inside a view")
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
