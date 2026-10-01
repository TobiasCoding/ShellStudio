// SPDX-License-Identifier: GPL-3.0-only
package mux

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"shellstudio/internal/platform"
	"shellstudio/internal/runner"
	"shellstudio/internal/store"
)

type Mux struct {
	Paths  platform.Paths
	Binary string
	Run    func(bool, ...string) (string, error)
}
type Pane struct {
	ID, Console  string
	Dead, Active bool
}

func New(p platform.Paths, binary string) *Mux {
	m := &Mux{Paths: p, Binary: binary}
	m.Run = m.run
	return m
}
func (m *Mux) Socket(views bool) string {
	n := "programs.sock"
	if views {
		n = "views.sock"
	}
	return filepath.Join(m.Paths.Runtime, n)
}
func (m *Mux) run(views bool, args ...string) (string, error) {
	base := []string{"-S", m.Socket(views), "-f", "/dev/null"}
	return runner.Run(5*time.Second, "", nil, "tmux", append(base, args...)...)
}
func (m *Mux) Batch(views bool, cmds ...[]string) (string, error) {
	var all []string
	for _, c := range cmds {
		if len(c) == 0 {
			continue
		}
		if len(all) > 0 {
			all = append(all, ";")
		}
		all = append(all, c...)
	}
	if len(all) == 0 {
		return "", nil
	}
	return m.Run(views, all...)
}
func (m *Mux) Programs() (map[string]bool, error) {
	result := map[string]bool{}
	if _, e := os.Stat(m.Socket(false)); os.IsNotExist(e) {
		return result, nil
	}
	o, e := m.Run(false, "list-panes", "-a", "-F", "#{session_name}\t#{pane_dead}")
	if e != nil {
		if strings.Contains(o, "no server running") || strings.Contains(o, "Connection refused") {
			return result, nil
		}
		return nil, e
	}
	for _, l := range strings.Split(strings.TrimSpace(o), "\n") {
		p := strings.Split(l, "\t")
		if len(p) == 2 {
			result[strings.TrimPrefix(p[0], "c-")] = p[1] == "0"
		}
	}
	return result, nil
}
func (m *Mux) Start(c store.Console) error {
	l, e := platform.Lock(filepath.Join(m.Paths.Runtime, "programs.lock"))
	if e != nil {
		return e
	}
	defer platform.Unlock(l)
	if _, e := platform.Directory(c.Folder, "", c.Folder); e != nil {
		return e
	}
	if len(c.Argv) == 0 {
		return errors.New("console has no executable")
	}
	if _, e = exec.LookPath(c.Argv[0]); e != nil {
		return e
	}
	p, e := m.Programs()
	if e != nil {
		return e
	}
	if p[c.ID] {
		return errors.New("console is already running")
	}
	name := "c-" + c.ID
	if _, exists := p[c.ID]; exists {
		if _, e = m.Run(false, "kill-session", "-t", "="+name); e != nil {
			return e
		}
	}
	_, e = m.Batch(false,
		[]string{"new-session", "-d", "-s", name, "-x", "120", "-y", "35", "-c", c.Folder, m.Binary, "_exec", c.ID},
		[]string{"set-option", "-g", "status", "off"}, []string{"set-option", "-g", "prefix", "None"}, []string{"set-option", "-g", "mouse", "off"},
		[]string{"set-option", "-g", "remain-on-exit", "on"}, []string{"set-option", "-g", "exit-empty", "off"},
		[]string{"set-option", "-s", "escape-time", "10"}, []string{"set-option", "-g", "focus-events", "on"}, []string{"set-option", "-g", "set-clipboard", "on"},
		[]string{"set-option", "-g", "history-limit", "10000"}, []string{"set-option", "-g", "window-size", "smallest"})
	return e
}
func (m *Mux) Stop(id string) error { _, e := m.Run(false, "kill-session", "-t", "=c-"+id); return e }
func (m *Mux) panes(session string) ([]Pane, error) {
	o, e := m.Run(true, "list-panes", "-t", "="+session+":", "-F", "#{pane_id}\t#{@console}\t#{pane_dead}\t#{pane_active}")
	if e != nil {
		return nil, e
	}
	var out []Pane
	for _, l := range strings.Split(strings.TrimSpace(o), "\n") {
		p := strings.Split(l, "\t")
		if len(p) == 4 {
			out = append(out, Pane{p[0], p[1], p[2] == "1", p[3] == "1"})
		}
	}
	return out, nil
}

var safeID = regexp.MustCompile(`^[a-f0-9]{24}$`)

func Session(client, view string) string { return "v-" + client + "-" + view }

// Sync reconciles membership only. Focus/resize do not rebuild panes or processes.
// Each UI has its own presentation session, so client focus is never shared.
func (m *Mux) Sync(client string, v store.View, cs []store.Console, width, height int) (string, error) {
	if !safeID.MatchString(client) || !safeID.MatchString(v.ID) {
		return "", errors.New("invalid view/client ID")
	}
	l, e := platform.Lock(filepath.Join(m.Paths.Runtime, "views.lock"))
	if e != nil {
		return "", e
	}
	defer platform.Unlock(l)
	session := Session(client, v.ID)
	panes, e := m.panes(session)
	exists := e == nil
	if !exists {
		_, e = m.Batch(true, []string{"new-session", "-d", "-s", session, "-x", strconv.Itoa(max(width, 80)), "-y", strconv.Itoa(max(height, 24)), m.Binary, "_empty"},
			[]string{"set-option", "-g", "mouse", "on"}, []string{"set-option", "-g", "remain-on-exit", "on"}, []string{"set-option", "-g", "set-clipboard", "on"},
			[]string{"set-option", "-g", "status-right", " F6 focus | F4 zoom | F10 menu "}, []string{"set-option", "-g", "status-left", " ShellStudio "},
			[]string{"set-option", "-g", "pane-border-status", "top"}, []string{"set-option", "-g", "pane-border-format", " #{pane_index}: #{pane_title} "},
			[]string{"set-option", "-g", "allow-rename", "off"}, []string{"set-option", "-g", "history-limit", "10000"},
			[]string{"bind-key", "-n", "F10", "detach-client"}, []string{"bind-key", "-n", "F6", "select-pane", "-t", ":.+"},
			[]string{"bind-key", "-n", "F4", "resize-pane", "-Z"}, []string{"bind-key", "-n", "C-M-Left", "select-pane", "-L"}, []string{"bind-key", "-n", "C-M-Right", "select-pane", "-R"},
			[]string{"bind-key", "-n", "C-M-Up", "select-pane", "-U"}, []string{"bind-key", "-n", "C-M-Down", "select-pane", "-D"},
			[]string{"set-option", "-g", "window-size", "latest"})
		if e != nil {
			return "", e
		}
		panes, e = m.panes(session)
		if e != nil {
			return "", e
		}
	}
	if !exists {
		if _, e = m.Batch(true, []string{"set-option", "-s", "escape-time", "10"}, []string{"set-option", "-g", "focus-events", "on"}); e != nil {
			return "", e
		}
	}
	by := map[string]Pane{}
	focused := ""
	for _, p := range panes {
		by[p.Console] = p
		if p.Active {
			focused = p.ID
		}
	}
	if len(cs) == 0 {
		cs = []store.Console{{ID: "empty", Name: "No consoles — F10 opens the menu"}}
	}
	if v.Explorer {
		cs = append(append([]store.Console{}, cs...), store.Console{ID: "explorer", Name: "Files"})
	}
	wanted := map[string]bool{}
	changed := false
	var order []string
	for _, c := range cs {
		wanted[c.ID] = true
		p, ok := by[c.ID]
		cmd := []string{m.Binary, "_relay", c.ID}
		if c.ID == "empty" {
			cmd = []string{m.Binary, "_empty"}
		}
		if c.ID == "explorer" {
			cmd = []string{m.Binary, "_explorer", v.Folder}
		}
		if !ok {
			if empty, yes := by[""]; yes {
				p = empty
				delete(by, "")
				_, e = m.Run(true, append([]string{"respawn-pane", "-k", "-t", p.ID}, cmd...)...)
			} else {
				// Keep a generous detached canvas while adding panes, then let attach resize it.
				_, e = m.Run(true, "resize-window", "-t", "="+session+":", "-x", strconv.Itoa(max(width, 160)), "-y", strconv.Itoa(max(height, len(cs)*4+8)))
				if e != nil {
					return "", e
				}
				o, err := m.Run(true, append([]string{"split-window", "-d", "-t", "=" + session + ":", "-P", "-F", "#{pane_id}"}, cmd...)...)
				e = err
				p.ID = strings.TrimSpace(o)
			}
			changed = true
		} else if p.Dead {
			_, e = m.Run(true, append([]string{"respawn-pane", "-k", "-t", p.ID}, cmd...)...)
			changed = true
		}
		if e != nil {
			return "", e
		}
		order = append(order, p.ID)
		if !ok || p.Dead {
			_, e = m.Batch(true, []string{"set-option", "-p", "-t", p.ID, "@console", c.ID}, []string{"select-pane", "-t", p.ID, "-T", c.Name})
			if e != nil {
				return "", e
			}
		}
		if changed {
			if _, e = m.Run(true, "select-layout", "-t", "="+session+":", "tiled"); e != nil {
				return "", e
			}
		}
	}
	var cmds [][]string
	for id, p := range by {
		if !wanted[id] {
			cmds = append(cmds, []string{"kill-pane", "-t", p.ID})
			changed = true
		}
	}
	if _, e = m.Batch(true, cmds...); e != nil {
		return "", e
	}
	actual, e := m.panes(session)
	if e != nil {
		return "", e
	}
	for i, id := range order {
		for j := i; j < len(actual); j++ {
			if actual[j].ID == id && j != i {
				if _, e = m.Run(true, "swap-pane", "-d", "-s", id, "-t", actual[i].ID); e != nil {
					return "", e
				}
				actual[i], actual[j] = actual[j], actual[i]
				changed = true
				break
			}
		}
	}
	last, e := m.Run(true, "show-option", "-wqv", "-t", "="+session+":", "@layout")
	if e != nil {
		return "", e
	}
	if changed || strings.TrimSpace(last) != v.Layout {
		layout := v.Layout
		if !ValidLayout(layout) {
			layout = "tiled"
		}
		_, e = m.Run(true, "select-layout", "-t", "="+session+":", layout)
		if e != nil && savedLayout.MatchString(layout) {
			layout = "tiled"
			_, e = m.Run(true, "select-layout", "-t", "="+session+":", layout)
		}
		if e != nil {
			return "", e
		}
		if _, e = m.Run(true, "set-option", "-w", "-t", "="+session+":", "@layout", layout); e != nil {
			return "", e
		}
	}
	for _, id := range order {
		if id == focused {
			if _, e = m.Run(true, "select-pane", "-t", id); e != nil {
				return "", e
			}
		}
	}
	_, e = m.Run(true, "set-option", "-w", "-t", "="+session+":", "window-size", "latest")
	return session, e
}

var savedLayout = regexp.MustCompile(`^[a-f0-9]{4},[0-9x,{}\[\]]+$`)

func ValidLayout(v string) bool {
	switch v {
	case "tiled", "even-horizontal", "even-vertical", "main-horizontal", "main-vertical":
		return true
	}
	return savedLayout.MatchString(v)
}
func (m *Mux) Attach(session string) *exec.Cmd {
	c := exec.Command("tmux", "-S", m.Socket(true), "attach-session", "-t", "="+session)
	c.Env = runner.Env(nil)
	return c
}
func (m *Mux) Cleanup(client string) {
	o, e := m.Run(true, "list-sessions", "-F", "#{session_name}")
	if e != nil {
		return
	}
	var cs [][]string
	for _, s := range strings.Fields(o) {
		if strings.HasPrefix(s, "v-"+client+"-") {
			cs = append(cs, []string{"kill-session", "-t", "=" + s})
		}
	}
	m.Batch(true, cs...)
}

// Focus resolves a live client first. Stale IDs are rejected for destructive actions.
func (m *Mux) Focus(client, pane string, allowFallback bool) (string, Pane, error) {
	o, e := m.Run(true, "list-clients", "-F", "#{client_name}\t#{session_name}")
	if e != nil {
		return "", Pane{}, e
	}
	session := ""
	for _, l := range strings.Split(strings.TrimSpace(o), "\n") {
		p := strings.Split(l, "\t")
		if len(p) == 2 && p[0] == client {
			session = p[1]
		}
	}
	if session == "" {
		return "", Pane{}, errors.New("client is no longer connected")
	}
	ps, e := m.panes(session)
	if e != nil {
		return "", Pane{}, e
	}
	var active Pane
	for _, p := range ps {
		if p.Active {
			active = p
		}
		if p.ID == pane || (pane == "" && p.Active) {
			return session, p, nil
		}
	}
	if allowFallback && active.ID != "" {
		return session, active, nil
	}
	return "", Pane{}, fmt.Errorf("pane %s no longer belongs to this client", pane)
}
