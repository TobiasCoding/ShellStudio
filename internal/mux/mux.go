// SPDX-License-Identifier: GPL-3.0-only
package mux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"shellstudio/internal/diagnostics"
	"shellstudio/internal/platform"
	"shellstudio/internal/runner"
	"shellstudio/internal/store"
)

// Two private tmux servers: programs.sock owns the persistent processes and
// views.sock owns the windows that show them. Killing the views server never
// ends a program.
type Mux struct {
	Paths   platform.Paths
	Binary  string
	Tmux    string
	Version string
	Run     func(views bool, args ...string) (string, error)
}

// A tmux message admits ~16 KiB ("command too long"); batches are split by size.
const batchBytes = 8192

func New(p platform.Paths, binary string) *Mux {
	tm, e := exec.LookPath("tmux")
	if e != nil {
		tm = "tmux"
	}
	m := &Mux{Paths: p, Binary: binary, Tmux: tm}
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
func (m *Mux) run(views bool, args ...string) (output string, result error) {
	started := time.Now()
	defer func() {
		if len(args) == 0 {
			return
		}
		op := args[0]
		read := strings.HasPrefix(op, "list-") || strings.HasPrefix(op, "show-") || op == "display-message" || op == "has-session"
		if read && result == nil && time.Since(started) < time.Second {
			return
		}
		server := "programs"
		if views {
			server = "views"
		}
		count := 1
		for _, a := range args {
			if a == ";" {
				count++
			}
		}
		diagnostics.Record(diagnostics.Entry{Event: "tmux", Operation: op, Server: server, Commands: count}, result, started)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, m.Tmux, append([]string{"-S", m.Socket(views), "-f", "/dev/null"}, args...)...)
	c.Env = runner.Env(nil)
	var out, errOut bytes.Buffer
	c.Stdout, c.Stderr = &out, &errOut
	e := c.Run()
	if ctx.Err() != nil {
		return out.String(), errors.New("tmux did not answer in time")
	}
	if e != nil {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = e.Error()
		}
		return out.String(), errors.New(msg)
	}
	return out.String(), nil
}

// Batch runs several commands per tmux invocation. tmux stops at the first
// failing command, so later chunks are not sent either.
func (m *Mux) Batch(views bool, cmds ...[]string) (string, error) {
	var chunks [][]string
	var cur []string
	size := 0
	for _, c := range cmds {
		if len(c) == 0 {
			continue
		}
		n := 2
		for _, a := range c {
			n += len(a) + 1
		}
		if len(cur) > 0 && size+n > batchBytes {
			chunks = append(chunks, cur)
			cur, size = nil, 0
		}
		if len(cur) > 0 {
			cur = append(cur, ";")
		}
		cur = append(cur, c...)
		size += n
	}
	if len(cur) > 0 {
		chunks = append(chunks, cur)
	}
	var all strings.Builder
	for _, c := range chunks {
		o, e := m.Run(views, c...)
		all.WriteString(o)
		if e != nil {
			return all.String(), e
		}
	}
	return all.String(), nil
}

// NoServer reports errors that only mean the server is not running yet.
func NoServer(e error) bool {
	if e == nil {
		return false
	}
	s := e.Error()
	return strings.Contains(s, "no server running") || (strings.Contains(s, "error connecting to") &&
		(strings.Contains(s, "No such file or directory") || strings.Contains(s, "Connection refused")))
}

// Programs maps console IDs to true when alive and false when the process ended.
func (m *Mux) Programs() (map[string]bool, error) {
	result := map[string]bool{}
	if _, e := os.Stat(m.Socket(false)); os.IsNotExist(e) {
		return result, nil
	}
	o, e := m.Run(false, "list-panes", "-a", "-F", "#{session_name}\t#{pane_dead}")
	if e != nil {
		if NoServer(e) || e.Error() == "no current target" {
			return result, nil
		}
		return nil, e
	}
	for _, l := range strings.Split(strings.TrimSpace(o), "\n") {
		p := strings.Split(l, "\t")
		if len(p) == 2 && strings.HasPrefix(p[0], "c-") {
			id := strings.TrimPrefix(p[0], "c-")
			result[id] = result[id] || p[1] == "0"
		}
	}
	return result, nil
}

// Activity returns the last activity time per console, for choosing an agent.
func (m *Mux) Activity() map[string]int64 {
	out := map[string]int64{}
	o, e := m.Run(false, "list-sessions", "-F", "#{session_name}\t#{session_activity}")
	if e != nil {
		return out
	}
	for _, l := range strings.Split(strings.TrimSpace(o), "\n") {
		name, at, ok := strings.Cut(l, "\t")
		var n int64
		if ok && strings.HasPrefix(name, "c-") {
			fmt.Sscan(at, &n)
			out[strings.TrimPrefix(name, "c-")] = n
		}
	}
	return out
}

// Start creates the console session and, in the same invocation, the header and
// finished-console controls: a program that exits at once already shows them.
func (m *Mux) Start(c store.Console) error {
	l, e := platform.LockWait(filepath.Join(m.Paths.Runtime, "programs.lock"), 5*time.Second)
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
		return fmt.Errorf("%s was not found in PATH", c.Argv[0])
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
	cmds := [][]string{{"new-session", "-d", "-s", name, "-x", "120", "-y", "35", "-c", c.Folder, m.Binary, "_exec", c.ID},
		{"set-option", "-t", "=" + name + ":", "@ss_name", c.Name}}
	cmds = append(cmds, m.ProgramsControls()...)
	if _, e = m.Batch(false, cmds...); e != nil {
		// Never leave a program that no console describes.
		m.Run(false, "kill-session", "-t", "="+name)
	}
	return e
}
func (m *Mux) Stop(id string) error {
	_, e := m.Run(false, "kill-session", "-t", "=c-"+id)
	if NoServer(e) || (e != nil && strings.Contains(e.Error(), "can't find session")) {
		return nil
	}
	return e
}

// SetName updates the header of a running console and redraws its clients.
func (m *Mux) SetName(id, name string) error {
	_, e := m.Run(false, "set-option", "-t", "=c-"+id+":", "@ss_name", name)
	if NoServer(e) || (e != nil && strings.Contains(e.Error(), "can't find session")) {
		return nil
	}
	if e != nil {
		return e
	}
	// Redraw the headers of the nested clients showing it. A loop format would
	// also visit this very command client, which has no status line.
	o, _ := m.Run(false, "list-clients", "-t", "=c-"+id, "-F", "#{client_name}")
	var redraw [][]string
	for _, c := range strings.Fields(o) {
		redraw = append(redraw, []string{"refresh-client", "-S", "-t", c})
	}
	m.Batch(false, redraw...)
	return nil
}

// AttachCommand is the nested client that shows a program inside a view pane.
func (m *Mux) AttachCommand(id string) string {
	return "exec " + Join("env", "-u", "TMUX", "-u", "TMUX_PANE", m.Tmux, "-S", m.Socket(false), "attach-session", "-t", "=c-"+id)
}

// Self is a shell command running this executable with arguments.
func (m *Mux) Self(args ...string) string { return Join(append([]string{m.Binary}, args...)...) }

func ViewSession(viewID string) string { return "v-" + viewID }
