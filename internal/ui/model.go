// SPDX-License-Identifier: GPL-3.0-only
package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"shellstudio/internal/app"
	"shellstudio/internal/explorer"
	"shellstudio/internal/extensions"
	"shellstudio/internal/mcp"
	"shellstudio/internal/mux"
	"shellstudio/internal/platform"
	"shellstudio/internal/runner"
	"shellstudio/internal/store"
	"shellstudio/internal/viewer"
)

type tickMsg time.Time
type autosaveMsg struct {
	ID       string
	Sequence uint64
	Deadline bool
}
type resultMsg struct {
	value any
	err   error
}
type savedMsg struct {
	note        store.Note
	body, title string
	conflict    bool
	err         error
}
type ExitMsg struct{}
type Row struct{ ID, Label string }
type Field struct {
	Label string
	Input textinput.Model
}
type Model struct {
	App                                  *app.App
	Client                               string
	Screen, Status                       string
	Width, Height, Index                 int
	Rows                                 []Row
	ViewID                               string
	views                                []store.View
	consoles                             []store.Console
	catalog                              []extensions.Entry
	selected                             extensions.Manifest
	busy                                 bool
	after                                func(any)
	formTitle, formBack                  string
	fields                               []Field
	field                                int
	submit                               func([]string) tea.Cmd
	confirmText, confirmBack             string
	confirm                              func() tea.Cmd
	viewport                             viewport.Model
	notes                                []store.Note
	trash                                bool
	search                               string
	note                                 store.Note
	editor                               textarea.Model
	dirty, saving                        bool
	changed, firstDirty                  time.Time
	editSequence                         uint64
	leave, quit                          bool
	saveStatus                           string
	folder, folderSearch, folderBack     string
	entries                              []explorer.Entry
	treePath                             string
	treeEntries                          []explorer.Entry
	treeIndex                            int
	treeFocus                            bool
	pick                                 func(string)
	choice                               func(string) tea.Cmd
	viewerKind, viewerPath, viewerFilter string
	textBack                             string
	viewerLast                           time.Time
}

func New(a *app.App, screen string) *Model {
	e := textarea.New()
	e.CharLimit = 4 << 20
	e.ShowLineNumbers = true
	e.Placeholder = "Write a note…"
	e.Prompt = "│ "
	e.Focus()
	m := &Model{App: a, Client: store.ID(), Screen: screen, Width: 100, Height: 30, editor: e, viewport: viewport.New(90, 20), saveStatus: "Saved"}
	if m.Screen == "" {
		m.Screen = "views"
	}
	m.refresh()
	return m
}
func (m *Model) Init() tea.Cmd {
	return tea.Batch(tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) }), textinput.Blink)
}
func (m *Model) OpenWorkspace(v store.View) {
	m.ViewID = v.ID
	m.folder = v.Folder
	m.treePath = v.Folder
	if m.treePath == "" {
		m.treePath = m.App.CWD
	}
	m.goTo("consoles")
}
func (m *Model) fail(e error) {
	if e != nil {
		m.Status = "Error: " + e.Error()
	}
}
func (m *Model) refresh() {
	m.Rows = nil
	switch m.Screen {
	case "views":
		v, e := m.App.Store.Views()
		m.fail(e)
		m.views = v
		for _, x := range v {
			m.Rows = append(m.Rows, Row{x.ID, x.Name + "  ·  " + x.Folder})
		}
	case "consoles":
		if v, e := m.App.Store.View(m.ViewID); e == nil {
			if m.treePath == "" {
				m.treePath = v.Folder
				if m.treePath == "" {
					m.treePath = m.App.CWD
				}
			}
			if entries, err := explorer.List(m.treePath, ""); err == nil {
				m.treeEntries = entries
			} else {
				m.treeEntries = nil
				m.fail(err)
			}
		}
		cs, e := m.App.Store.Consoles(m.ViewID)
		m.fail(e)
		m.consoles = cs
		p, e := m.App.Mux.Programs()
		m.fail(e)
		for _, c := range cs {
			state := "stopped"
			if p[c.ID] {
				state = "running"
			}
			m.Rows = append(m.Rows, Row{c.ID, fmt.Sprintf("%-10s %-22s %s", state, c.Name, c.Folder)})
		}
	case "notes":
		ns, e := m.App.Store.Notes(m.search, m.trash)
		m.fail(e)
		m.notes = ns
		for _, n := range ns {
			m.Rows = append(m.Rows, Row{n.ID, n.Title + "  ·  " + n.Updated})
		}
	case "extensions":
		m.catalog = extensions.Catalog(m.App.Paths.Config)
		for i, c := range m.catalog {
			label := c.Manifest.ID + "  " + c.Manifest.Version
			if c.Error != "" {
				label = "Invalid: " + c.Source + " — " + c.Error
			} else if m.App.Extensions.Enabled(c.Manifest) {
				label += "  [enabled]"
			} else {
				label += "  [disabled]"
			}
			m.Rows = append(m.Rows, Row{fmt.Sprint(i), label})
		}
	case "files":
		es, e := explorer.List(m.folder, m.folderSearch)
		m.fail(e)
		m.entries = es
		for _, x := range es {
			label := x.Name
			if x.Dir {
				label += "/"
			}
			m.Rows = append(m.Rows, Row{x.Path, label})
		}
	}
	m.Index = max(0, min(m.Index, len(m.Rows)-1))
}
func (m *Model) goTo(screen string) { m.Screen = screen; m.Index = 0; m.Status = ""; m.refresh() }
func (m *Model) op(work func() (any, error), after func(any)) tea.Cmd {
	m.busy = true
	m.after = after
	m.Status = "Working…"
	return func() tea.Msg { v, e := work(); return resultMsg{v, e} }
}
func (m *Model) form(title, back string, labels, values []string, submit func([]string) tea.Cmd) {
	m.formTitle = title
	m.formBack = back
	m.fields = nil
	m.field = 0
	for i, l := range labels {
		v := textinput.New()
		v.CharLimit = 16384
		v.Width = max(20, m.Width-8)
		if i < len(values) {
			v.SetValue(values[i])
		}
		if i == 0 {
			v.Focus()
		}
		m.fields = append(m.fields, Field{l, v})
	}
	m.submit = submit
	m.Screen = "form"
}
func (m *Model) ask(text, back string, f func() tea.Cmd) {
	m.confirmText = text
	m.confirmBack = back
	m.confirm = f
	m.viewport.SetContent(explorer.Clean(text))
	m.viewport.GotoTop()
	m.Screen = "confirm"
}
func (m *Model) choose(title, back string, rows []Row, f func(string) tea.Cmd) {
	m.formTitle = title
	m.formBack = back
	m.Rows = rows
	m.choice = f
	m.Index = 0
	m.Screen = "choice"
}
func (m *Model) selectedID() string {
	if m.Index >= 0 && m.Index < len(m.Rows) {
		return m.Rows[m.Index].ID
	}
	return ""
}
func (m *Model) startSave() tea.Cmd {
	if !m.dirty || m.saving {
		return nil
	}
	m.saving = true
	m.saveStatus = "Saving"
	n := m.note
	n.Body = m.editor.Value()
	s := m.App.Store
	return func() tea.Msg {
		saved, conflict, e := s.SaveNote(n)
		return savedMsg{saved, n.Body, n.Title, conflict, e}
	}
}
func (m *Model) openNote(n store.Note) {
	m.note = n
	m.editor.SetValue(n.Body)
	m.dirty = false
	m.saving = false
	m.leave = false
	m.saveStatus = "Saved"
	m.Screen = "editor"
	m.editor.Focus()
}
func (m *Model) attach() tea.Cmd {
	v, e := m.App.Store.View(m.ViewID)
	if e != nil {
		m.fail(e)
		return nil
	}
	cs, e := m.App.Store.Consoles(m.ViewID)
	if e != nil {
		m.fail(e)
		return nil
	}
	session, e := m.App.Mux.Sync(m.Client, v, cs, m.Width, m.Height)
	if e != nil {
		m.fail(e)
		return nil
	}
	m.after = func(any) {
		layout, e := m.App.Mux.Run(true, "display-message", "-p", "-t", "="+session+":", "#{window_layout}")
		if e != nil {
			m.fail(e)
			return
		}
		layout = strings.TrimSpace(layout)
		if mux.ValidLayout(layout) {
			v, e := m.App.Store.View(m.ViewID)
			if e == nil {
				v.Layout = layout
				e = m.App.Store.UpdateView(v)
			}
			m.fail(e)
		}
	}
	return tea.ExecProcess(m.App.Mux.Attach(session), func(e error) tea.Msg { return resultMsg{nil, e} })
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case autosaveMsg:
		if m.Screen == "editor" && m.note.ID == v.ID && m.dirty && !m.saving && !strings.HasPrefix(m.saveStatus, "Error:") && (v.Deadline || v.Sequence == m.editSequence) {
			return m, m.startSave()
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.Width = max(24, v.Width)
		m.Height = max(8, v.Height)
		m.editor.SetWidth(max(10, m.Width-4))
		m.editor.SetHeight(max(3, m.Height-8))
		m.viewport.Width = max(10, m.Width-4)
		m.viewport.Height = max(3, m.Height-8)
		for i := range m.fields {
			m.fields[i].Input.Width = max(10, m.Width-8)
		}
		return m, nil
	case ExitMsg:
		m.quit = true
		if m.Screen == "editor" && (m.dirty || m.saving) {
			return m, m.startSave()
		}
		return m, tea.Quit
	case resultMsg:
		m.busy = false
		f := m.after
		m.after = nil
		if v.err != nil {
			m.fail(v.err)
		} else {
			m.Status = "Done"
			if f != nil {
				f(v.value)
			}
		}
		m.refresh()
		return m, nil
	case savedMsg:
		m.saving = false
		if v.err != nil {
			m.saveStatus = "Error: " + v.err.Error()
			m.Status = m.saveStatus
			m.leave = false
			m.quit = false
			return m, nil
		}
		m.note.ID = v.note.ID
		m.note.Revision = v.note.Revision
		m.note.Title = v.note.Title
		m.note.Updated = v.note.Updated
		m.dirty = m.editor.Value() != v.body
		if v.conflict {
			m.Status = "Concurrent edit: both versions kept; editing the conflict copy"
		}
		if m.dirty {
			m.firstDirty = time.Now()
			m.saveStatus = "Saving"
		} else {
			m.note.Body = v.body
			m.saveStatus = "Saved"
			if m.quit {
				return m, tea.Quit
			}
			if m.leave {
				m.goTo("notes")
			}
		}
		return m, nil
	case tickMsg:
		cmds := []tea.Cmd{tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })}
		now := time.Time(v)
		if m.Screen == "editor" && m.dirty && !m.saving && !strings.HasPrefix(m.saveStatus, "Error:") && (now.Sub(m.changed) >= 300*time.Millisecond || now.Sub(m.firstDirty) >= time.Second) {
			cmds = append(cmds, m.startSave())
		}
		if m.Screen == "viewer" && !m.busy && now.Sub(m.viewerLast) >= 2*time.Second {
			m.viewerLast = now
			kind, path, filter := m.viewerKind, m.viewerPath, m.viewerFilter
			cmds = append(cmds, m.op(func() (any, error) { return viewer.Read(kind, path, filter) }, func(v any) { m.viewport.SetContent(v.(string)) }))
		}
		return m, tea.Batch(cmds...)
	case tea.MouseMsg:
		if !m.busy && (m.Screen == "views" || m.Screen == "consoles" || m.Screen == "notes" || m.Screen == "extensions" || m.Screen == "files") && v.Button == tea.MouseButtonLeft && v.Action == tea.MouseActionPress && v.Y == 0 {
			for _, tab := range []struct {
				name       string
				start, end int
			}{{"views", 14, 19}, {"notes", 22, 27}, {"extensions", 30, 40}, {"files", 43, 48}} {
				if v.X >= tab.start && v.X < tab.end {
					if tab.name == "files" {
						m.folder = m.App.CWD
						m.folderBack = ""
						m.pick = nil
					}
					m.goTo(tab.name)
					return m, nil
				}
			}
		}
		if !m.busy && m.Screen == "form" && m.Width >= 70 && v.Button == tea.MouseButtonLeft && v.Action == tea.MouseActionPress {
			contentHeight := max(1, m.Height-9)
			cardHeight := 7 + 5*len(m.fields)
			cardTop := 4 + max(0, (contentHeight-cardHeight)/2)
			firstField := cardTop + 4
			fieldRow := v.Y - firstField
			if fieldRow >= 0 && fieldRow < 5*len(m.fields) {
				idx := fieldRow / 5
				m.fields[m.field].Input.Blur()
				m.field = idx
				return m, m.fields[m.field].Input.Focus()
			}
			buttonRow := firstField + 5*len(m.fields)
			if v.Y == buttonRow {
				if v.X >= m.Width/2 {
					m.goTo(m.formBack)
					return m, nil
				}
				values := make([]string, 0, len(m.fields))
				for _, field := range m.fields {
					values = append(values, field.Input.Value())
				}
				return m, m.submit(values)
			}
		}
		if m.Screen != "editor" && m.Screen != "form" && m.Screen != "confirm" && m.Screen != "viewer" && m.Screen != "text" {
			if m.Screen == "consoles" && m.Width >= 70 && v.Button == tea.MouseButtonLeft && v.Action == tea.MouseActionPress {
				sidebarWidth := max(22, min(34, m.Width/3))
				if v.X < sidebarWidth {
					idx := v.Y - 6
					if idx >= 0 && idx < len(m.treeEntries) {
						m.treeIndex, m.treeFocus = idx, true
						return m, m.activateTree()
					}
					return m, nil
				}
			}
			if v.Button == tea.MouseButtonWheelDown || v.Button == tea.MouseButtonWheelUp {
				delta := 1
				if v.Button == tea.MouseButtonWheelUp {
					delta = -1
				}
				if m.Screen == "consoles" && m.Width >= 70 && v.X < max(22, min(34, m.Width/3)) {
					m.treeIndex = max(0, min(max(0, len(m.treeEntries)-1), m.treeIndex+delta))
					m.treeFocus = true
				} else {
					m.Index = max(0, min(max(0, len(m.Rows)-1), m.Index+delta))
					if m.Screen == "consoles" {
						m.treeFocus = false
					}
				}
			}
			if m.Screen == "choice" && v.Button == tea.MouseButtonLeft && v.Action == tea.MouseActionPress {
				cardHeight := len(m.Rows) + 6
				firstRow := 4 + max(0, (m.Height-9-cardHeight)/2) + 4
				idx := v.Y - firstRow
				if idx >= 0 && idx < len(m.Rows) {
					m.Index = idx
					return m, m.activate()
				}
			}
			if m.Screen != "choice" && v.Button == tea.MouseButtonLeft && v.Action == tea.MouseActionPress {
				idx := v.Y - 4 + m.offset()
				if idx >= 0 && idx < len(m.Rows) {
					m.Index = idx
					m.treeFocus = false
					return m, m.activate()
				}
			}
		}
	case tea.KeyMsg:
		k := v.String()
		if m.busy {
			if k == "ctrl+c" {
				m.Status = "Wait for the active operation to finish before closing"
			}
			return m, nil
		}
		if m.Screen == "editor" {
			if k == "esc" || k == "ctrl+c" {
				m.leave = true
				m.quit = k == "ctrl+c"
				if !m.dirty && !m.saving {
					if m.quit {
						return m, tea.Quit
					}
					m.goTo("notes")
					return m, nil
				}
				return m, m.startSave()
			}
			if k == "ctrl+s" {
				return m, m.startSave()
			}
			return m, m.edit(msg)
		}
		if m.Screen == "form" {
			if k == "esc" {
				m.goTo(m.formBack)
				return m, nil
			}
			if k == "ctrl+f" && (strings.Contains(strings.ToLower(m.fields[m.field].Label), "folder") || strings.Contains(strings.ToLower(m.fields[m.field].Label), "carpeta")) {
				m.folderBack = "form"
				m.folder = m.App.CWD
				if p, e := platform.Directory(m.fields[m.field].Input.Value(), "", m.App.CWD); e == nil {
					m.folder = p
				}
				field := m.field
				m.pick = func(p string) { m.fields[field].Input.SetValue(p); m.Screen = "form" }
				m.goTo("files")
				return m, nil
			}
			if k == "ctrl+r" && (strings.Contains(strings.ToLower(m.fields[m.field].Label), "folder") || strings.Contains(strings.ToLower(m.fields[m.field].Label), "carpeta")) {
				c, e := platform.LoadConfig(m.App.Paths.Config)
				m.fail(e)
				rows := []Row{}
				for _, p := range c.Recent {
					rows = append(rows, Row{p, p})
				}
				idx := m.field
				m.choose("Recent folders", "form", rows, func(p string) tea.Cmd { m.fields[idx].Input.SetValue(p); m.Screen = "form"; return nil })
				return m, nil
			}
			if k == "tab" || k == "shift+tab" || k == "down" || k == "up" {
				m.fields[m.field].Input.Blur()
				d := 1
				if k == "shift+tab" || k == "up" {
					d = -1
				}
				m.field = (m.field + d + len(m.fields)) % len(m.fields)
				return m, m.fields[m.field].Input.Focus()
			}
			if k == "enter" {
				vals := []string{}
				for _, f := range m.fields {
					vals = append(vals, f.Input.Value())
				}
				return m, m.submit(vals)
			}
			var cmd tea.Cmd
			m.fields[m.field].Input, cmd = m.fields[m.field].Input.Update(msg)
			return m, cmd
		}
		if m.Screen == "confirm" {
			if k == "y" {
				return m, m.confirm()
			}
			if k == "n" || k == "esc" {
				m.goTo(m.confirmBack)
				return m, nil
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
		if k == "ctrl+c" || k == "q" {
			return m, tea.Quit
		}
		if k == "?" {
			m.viewport.SetContent(explorer.SSHHelp(m.App.CWD))
			m.Screen = "text"
			return m, nil
		}
		if m.Screen == "viewer" || m.Screen == "text" || m.Screen == "extension" {
			return m, m.detailKey(k, msg)
		}
		if k == "esc" {
			if m.Screen == "consoles" {
				m.goTo("views")
			} else if m.Screen == "choice" {
				m.goTo(m.formBack)
			} else if m.Screen == "files" && m.folderBack != "" {
				m.pick = nil
				m.goTo(m.folderBack)
			} else {
				m.goTo("views")
			}
			return m, nil
		}
		if k == "tab" {
			tabs := []string{"views", "notes", "extensions", "files"}
			i := 0
			for j, s := range tabs {
				if s == m.Screen {
					i = j + 1
				}
			}
			if tabs[i%4] == "files" {
				m.folder = m.App.CWD
				m.folderBack = ""
				m.pick = nil
			}
			m.goTo(tabs[i%4])
			return m, nil
		}
		if m.Screen == "consoles" && (k == "f6" || k == "ctrl+e") {
			m.treeFocus = !m.treeFocus
			return m, nil
		}
		if m.Screen == "consoles" && m.treeFocus {
			switch k {
			case "up", "k":
				m.treeIndex = max(0, m.treeIndex-1)
				return m, nil
			case "down", "j":
				m.treeIndex = min(max(0, len(m.treeEntries)-1), m.treeIndex+1)
				return m, nil
			case "enter":
				return m, m.activateTree()
			}
		}
		if k == "up" || k == "k" {
			m.Index = max(0, m.Index-1)
			return m, nil
		}
		if k == "down" || k == "j" {
			m.Index = min(len(m.Rows)-1, m.Index+1)
			return m, nil
		}
		if k == "pgdown" {
			m.Index = min(len(m.Rows)-1, m.Index+max(1, m.Height-9))
			return m, nil
		}
		if k == "pgup" {
			m.Index = max(0, m.Index-max(1, m.Height-9))
			return m, nil
		}
		if k == "enter" {
			return m, m.activate()
		}
		return m, m.key(k)
	}
	if m.Screen == "editor" {
		return m, m.edit(msg)
	}
	return m, nil
}

// All textarea messages, including asynchronous clipboard pastes, track pending edits.
func (m *Model) edit(msg tea.Msg) tea.Cmd {
	old := m.editor.Value()
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	if m.editor.Value() == old {
		return cmd
	}
	now := time.Now()
	id := m.note.ID
	m.editSequence++
	sequence := m.editSequence
	cmds := []tea.Cmd{cmd, tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg { return autosaveMsg{ID: id, Sequence: sequence} })}
	if !m.dirty {
		m.firstDirty = now
		cmds = append(cmds, tea.Tick(time.Second, func(time.Time) tea.Msg { return autosaveMsg{ID: id, Deadline: true} }))
	}
	m.changed = now
	m.dirty = true
	m.saveStatus = "Saving"
	return tea.Batch(cmds...)
}

func (m *Model) activate() tea.Cmd {
	id := m.selectedID()
	if id == "" {
		return nil
	}
	switch m.Screen {
	case "views":
		m.ViewID = id
		if v, err := m.App.Store.View(id); err == nil {
			m.treePath = v.Folder
			m.treeIndex = 0
		}
		m.goTo("consoles")
	case "consoles":
		return m.attach()
	case "notes":
		if m.trash {
			m.Status = "Press r to restore this note"
			return nil
		}
		n, e := m.App.Store.Note(id)
		if e != nil {
			m.fail(e)
		} else {
			m.openNote(n)
		}
	case "extensions":
		if m.Index < len(m.catalog) {
			e := m.catalog[m.Index]
			if e.Error != "" {
				m.Status = e.Error
				return nil
			}
			m.selected = e.Manifest
			m.viewport.SetContent(m.App.Extensions.Preview(m.selected) + "\nSource: " + e.Source + "\n\n" + m.selected.Description)
			m.viewport.GotoTop()
			m.Screen = "extension"
		}
	case "files":
		if m.Index < len(m.entries) {
			e := m.entries[m.Index]
			if e.Dir {
				m.folder = e.Path
				m.Index = 0
				m.refresh()
			} else {
				s, err := explorer.Preview(e.Path)
				m.fail(err)
				if err == nil {
					m.viewport.SetContent(s)
					m.textBack = "files"
					m.Screen = "text"
				}
			}
		}
	case "choice":
		return m.choice(id)
	}
	return nil
}

func (m *Model) activateTree() tea.Cmd {
	if m.treeIndex < 0 || m.treeIndex >= len(m.treeEntries) {
		return nil
	}
	e := m.treeEntries[m.treeIndex]
	if e.Dir {
		m.treePath = e.Path
		m.treeIndex = 0
		m.refresh()
		return nil
	}
	preview, err := explorer.Preview(e.Path)
	m.fail(err)
	if err == nil {
		m.viewport.SetContent(preview)
		m.textBack = "consoles"
		m.Screen = "text"
	}
	return nil
}
func (m *Model) key(k string) tea.Cmd {
	id := m.selectedID()
	switch m.Screen {
	case "views":
		if k == "n" {
			m.form("New view", "views", []string{"Name", "Base folder (blank uses launch directory)"}, []string{"Main", m.App.CWD}, func(v []string) tea.Cmd {
				p, e := platform.Directory(v[1], "", m.App.CWD)
				if e != nil {
					m.fail(e)
					return nil
				}
				_, e = m.App.Store.NewView(v[0], p)
				m.fail(e)
				if e == nil {
					m.goTo("views")
				}
				return nil
			})
		}
		if k == "d" && id != "" {
			m.ask("Delete this view?\nIts consoles keep running and remain available through Add existing console.", "views", func() tea.Cmd { e := m.App.Store.DeleteView(id); m.goTo("views"); m.fail(e); return nil })
		}
	case "consoles":
		switch k {
		case "n", "t":
			m.launchForm(k == "t")
		case "o":
			return m.attach()
		case "r":
			if id != "" {
				m.ask("Restart this stopped console explicitly?\nIts saved executable, arguments and folder will be reused.", "consoles", func() tea.Cmd {
					return m.op(func() (any, error) { return nil, m.App.Restart(id) }, func(any) { m.goTo("consoles") })
				})
			}
		case "x":
			if id != "" {
				m.ask("Stop this program?\nThis ends the process in every view. Unsaved program data may be lost.", "consoles", func() tea.Cmd {
					return m.op(func() (any, error) { return nil, m.App.Stop(id) }, func(any) { m.goTo("consoles") })
				})
			}
		case "d":
			if id != "" {
				m.fail(m.App.Store.Unlink(m.ViewID, id))
				m.refresh()
			}
		case "[", "]":
			d := -1
			if k == "]" {
				d = 1
			}
			m.fail(m.App.Store.Move(m.ViewID, id, d))
			m.refresh()
		case "a":
			cs, e := m.App.Store.Consoles("")
			m.fail(e)
			rows := []Row{}
			for _, c := range cs {
				rows = append(rows, Row{c.ID, c.Name + " · " + c.Folder})
			}
			m.choose("Add existing console", "consoles", rows, func(id string) tea.Cmd { m.fail(m.App.Store.Link(m.ViewID, id)); m.goTo("consoles"); return nil })
		case "l":
			rows := []Row{}
			for _, l := range []string{"tiled", "even-horizontal", "even-vertical", "main-horizontal", "main-vertical"} {
				rows = append(rows, Row{l, l})
			}
			m.choose("Layout", "consoles", rows, func(l string) tea.Cmd {
				v, e := m.App.Store.View(m.ViewID)
				if e == nil {
					v.Layout = l
					e = m.App.Store.UpdateView(v)
				}
				m.goTo("consoles")
				m.fail(e)
				return nil
			})
		case "f":
			v, e := m.App.Store.View(m.ViewID)
			m.fail(e)
			m.form("Edit view", "consoles", []string{"Name", "Base folder"}, []string{v.Name, v.Folder}, func(vals []string) tea.Cmd {
				p, e := platform.Directory(vals[1], "", m.App.CWD)
				if e != nil {
					m.fail(e)
					return nil
				}
				v.Name = vals[0]
				v.Folder = p
				m.treePath = p
				e = m.App.Store.UpdateView(v)
				m.fail(e)
				if e == nil {
					m.goTo("consoles")
				}
				return nil
			})
		case "b":
			v, e := m.App.Store.View(m.ViewID)
			if e == nil {
				v.Explorer = !v.Explorer
				e = m.App.Store.UpdateView(v)
			}
			m.fail(e)
			m.refresh()
		}
	case "notes":
		switch k {
		case "n":
			m.form("New note", "notes", []string{"Title"}, []string{"Untitled"}, func(v []string) tea.Cmd {
				n, e := m.App.Store.NewNote(v[0])
				m.fail(e)
				if e == nil {
					m.openNote(n)
				}
				return nil
			})
		case "/":
			m.form("Search notes", "notes", []string{"Text"}, []string{m.search}, func(v []string) tea.Cmd { m.search = v[0]; m.goTo("notes"); return nil })
		case "t":
			m.trash = !m.trash
			m.refresh()
		case "d":
			if id != "" {
				m.fail(m.App.Store.Trash(id, true))
				m.refresh()
			}
		case "r":
			if id != "" {
				if m.trash {
					m.fail(m.App.Store.Trash(id, false))
					m.refresh()
				} else {
					n, e := m.App.Store.Note(id)
					if e != nil {
						m.fail(e)
						break
					}
					m.form("Rename note", "notes", []string{"Title"}, []string{n.Title}, func(v []string) tea.Cmd {
						n.Title = v[0]
						_, _, e := m.App.Store.SaveNote(n)
						if e != nil {
							m.fail(e)
						} else {
							m.goTo("notes")
						}
						return nil
					})
				}
			}
		case "e":
			if id != "" {
				n, e := m.App.Store.Note(id)
				m.fail(e)
				m.form("Export Markdown", "notes", []string{"Destination (existing file is replaced)"}, []string{filepath.Join(m.App.CWD, "note-"+n.ID+".md")}, func(v []string) tea.Cmd {
					path := v[0]
					m.ask("Export Markdown to "+path+"?\nAn existing file is replaced.", "notes", func() tea.Cmd {
						e := platform.AtomicWrite(path, []byte("# "+n.Title+"\n\n"+n.Body+"\n"))
						m.goTo("notes")
						m.fail(e)
						return nil
					})
					return nil
				})
			}
		}
	case "extensions":
		if k == "a" {
			m.form("Import extension", "extensions", []string{"Manifest file or HTTPS URL"}, nil, func(v []string) tea.Cmd {
				return m.op(func() (any, error) { return extensions.ImportPreview(v[0]) }, func(x any) {
					man := x.(extensions.Manifest)
					m.ask(m.App.Extensions.Preview(man)+"\nImport this manifest? (No commands run yet.)", "extensions", func() tea.Cmd {
						e := extensions.Import(m.App.Paths.Config, man)
						m.goTo("extensions")
						m.fail(e)
						return nil
					})
				})
			})
		}
		if k == "n" {
			m.commandForm()
		}
	case "files":
		switch k {
		case "s":
			if m.pick != nil {
				f := m.pick
				m.pick = nil
				f(m.folder)
			} else {
				m.Status = "Folder: " + m.folder
			}
		case "p":
			m.form("Open folder", "files", []string{"Folder"}, []string{m.folder}, func(v []string) tea.Cmd {
				p, e := platform.Directory(v[0], "", m.App.CWD)
				if e != nil {
					m.fail(e)
					return nil
				}
				m.folder = p
				m.goTo("files")
				return nil
			})
		case "/":
			m.form("Filter this folder (up to 2,000 entries)", "files", []string{"Name contains"}, []string{m.folderSearch}, func(v []string) tea.Cmd { m.folderSearch = v[0]; m.goTo("files"); return nil })
		case "c":
			if id != "" {
				return func() tea.Msg { _, e := os.Stdout.WriteString(explorer.OSC52(id)); return resultMsg{nil, e} }
			}
		case "h":
			m.viewport.SetContent(explorer.SSHHelp(id))
			m.Screen = "text"
		case "e":
			if id != "" {
				editor := os.Getenv("EDITOR")
				if editor == "" {
					editor = "vi"
				}
				c := exec.Command(editor, id)
				c.Env = runner.Env(nil)
				return tea.ExecProcess(c, func(e error) tea.Msg { return resultMsg{nil, e} })
			}
		case "r":
			if id != "" {
				m.form("Rename file or folder", "files", []string{"New name (same directory)"}, []string{filepath.Base(id)}, func(v []string) tea.Cmd {
					if filepath.Base(v[0]) != v[0] || v[0] == "." || v[0] == ".." {
						m.Status = "Invalid name"
						return nil
					}
					dest := filepath.Join(filepath.Dir(id), v[0])
					if _, e := os.Lstat(dest); !os.IsNotExist(e) {
						m.Status = "Destination already exists"
						return nil
					}
					e := os.Rename(id, dest)
					m.goTo("files")
					m.fail(e)
					return nil
				})
			}
		}
	}
	return nil
}
func (m *Model) launchForm(terminal bool) {
	open := func(id string) tea.Cmd {
		profiles := "default"
		if id != "terminal" {
			man, e := m.App.Manifest(id)
			if e != nil {
				m.fail(e)
				return nil
			}
			keys := []string{}
			for k := range man.Profiles {
				keys = append(keys, k)
			}
			cfg, e := platform.LoadConfig(m.App.Paths.Config)
			if e != nil {
				m.fail(e)
				return nil
			}
			for k := range cfg.Extensions[id].Profiles {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			profiles = strings.Join(keys, ", ")
		}
		title := "Nueva consola"
		if id == "terminal" {
			title = "Nueva terminal"
		}
		profileLabel := "Perfil (opciones: " + profiles + "; vacío usa el predeterminado)"
		m.form(title, "consoles", []string{"Nombre", profileLabel, "Carpeta de trabajo (vacía usa la carpeta de la vista)"}, []string{id, "", ""}, func(v []string) tea.Cmd {
			view := m.ViewID
			return m.op(func() (any, error) { return m.App.Launch(view, v[0], id, v[1], v[2]) }, func(any) { m.goTo("consoles") })
		})
		return nil
	}
	if terminal {
		open("terminal")
		return
	}
	rows := []Row{{"terminal", "Terminal"}}
	for _, e := range extensions.Catalog(m.App.Paths.Config) {
		if e.Error == "" && m.App.Extensions.Enabled(e.Manifest) {
			rows = append(rows, Row{e.Manifest.ID, e.Manifest.ID})
		}
	}
	m.choose("Launch a console", "consoles", rows, open)
}

func (m *Model) detailKey(k string, msg tea.Msg) tea.Cmd {
	if k == "esc" {
		if m.Screen == "extension" {
			m.goTo("extensions")
		} else if m.Screen == "text" && m.textBack != "" {
			back := m.textBack
			m.textBack = ""
			m.goTo(back)
		} else {
			m.goTo("views")
		}
		return nil
	}
	if m.Screen == "viewer" {
		if k == "/" {
			m.form("Filter viewer", "viewer", []string{"Text"}, []string{m.viewerFilter}, func(v []string) tea.Cmd {
				m.viewerFilter = v[0]
				m.viewerLast = time.Time{}
				m.Screen = "viewer"
				return nil
			})
		}
		if k == "r" {
			m.viewerLast = time.Time{}
		}
	}
	if m.Screen == "extension" {
		man := m.selected
		switch k {
		case "i":
			m.ask(m.App.Extensions.Preview(man)+"\nInstall and enable?", "extensions", func() tea.Cmd {
				return m.op(func() (any, error) { return nil, m.App.Extensions.Install(man) }, func(any) { m.goTo("extensions") })
			})
			return nil
		case "e", "d":
			enable := k == "e"
			m.fail(m.App.Extensions.Enable(man, enable))
			return nil
		case "u", "X":
			purge := k == "X"
			text := "Uninstall " + man.ID + "?\nRunning consoles continue. Data is retained."
			if purge {
				text += "\nDELETE this extension's stored data as well."
			}
			m.ask(text, "extensions", func() tea.Cmd {
				e := m.App.Extensions.Uninstall(man, purge)
				m.goTo("extensions")
				m.fail(e)
				return nil
			})
			return nil
		case "c":
			cfg, e := platform.LoadConfig(m.App.Paths.Config)
			if e != nil {
				m.fail(e)
				return nil
			}
			name := man.Default
			if cfg.Extensions[man.ID].Default != "" {
				name = cfg.Extensions[man.ID].Default
			}
			p := man.Profiles[name]
			if x, ok := cfg.Extensions[man.ID].Profiles[name]; ok {
				p = x
			}
			a, _ := json.Marshal(p.Args)
			env, _ := json.Marshal(p.Env)
			m.form("Save default profile · "+man.ID, "extensions", []string{"Profile name", "Executable", "Arguments (JSON array)", "Environment (JSON object)"}, []string{name, p.Executable, string(a), string(env)}, func(v []string) tea.Cmd {
				var p platform.Profile
				p.Executable = v[1]
				if e := json.Unmarshal([]byte(v[2]), &p.Args); e != nil {
					m.fail(e)
					return nil
				}
				if e := json.Unmarshal([]byte(v[3]), &p.Env); e != nil {
					m.fail(e)
					return nil
				}
				if e := extensions.ValidateProfile(p); e != nil {
					m.fail(e)
					return nil
				}
				e := platform.UpdateConfig(m.App.Paths.Config, func(c *platform.Config) error {
					x := c.Extensions[man.ID]
					if x.Profiles == nil {
						x.Profiles = map[string]platform.Profile{}
					}
					x.Profiles[v[0]] = p
					x.Default = v[0]
					c.Extensions[man.ID] = x
					return nil
				})
				if e != nil {
					m.fail(e)
				} else {
					m.goTo("extensions")
				}
				return nil
			})
			return nil
		case "m":
			if man.MCP == nil {
				m.Status = "This extension has no MCP server"
				return nil
			}
			if !m.App.Extensions.Enabled(man) {
				m.Status = "Install and enable the MCP extension first"
				return nil
			}
			m.choose("Register MCP client", "extensions", []Row{{"claude", "Claude"}, {"codex", "Codex"}}, func(client string) tea.Cmd {
				path, e := mcp.ClientPath(client)
				if e != nil {
					m.fail(e)
					return nil
				}
				p := m.App.Extensions.Expand(man.ID, man.MCP.Command)
				plan, e := mcp.Prepare(client, path, man.MCP.Name, p)
				if e != nil {
					m.fail(e)
					return nil
				}
				clientMan, e := m.App.Manifest(client)
				detected := false
				if e == nil {
					_, e = m.App.Extensions.Detect(clientMan)
					detected = e == nil
				}
				text := plan.Preview
				if !detected {
					text += "\nClient absent: save a pending registration only. Apply again after installing the client."
				}
				m.ask(text, "extensions", func() tea.Cmd {
					if !detected {
						e = platform.WriteJSON(filepath.Join(m.App.Paths.Config, "pending-"+client+"-"+man.ID+".json"), p)
					} else {
						_, e = mcp.Apply(plan)
						if e == nil {
							os.Remove(filepath.Join(m.App.Paths.Config, "pending-"+client+"-"+man.ID+".json"))
						}
					}
					m.goTo("extensions")
					m.fail(e)
					return nil
				})
				return nil
			})
			return nil
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return cmd
}
func (m *Model) commandForm() {
	m.form("Existing command extension", "extensions", []string{"ID (lowercase letters, digits, hyphens)", "Description", "Executable", "Arguments (JSON array)", "Environment (JSON object)"}, []string{"my-tool", "My command", "", "[]", "{}"}, func(v []string) tea.Cmd {
		p := platform.Profile{Executable: v[2]}
		if e := json.Unmarshal([]byte(v[3]), &p.Args); e != nil {
			m.fail(e)
			return nil
		}
		if e := json.Unmarshal([]byte(v[4]), &p.Env); e != nil {
			m.fail(e)
			return nil
		}
		man := extensions.Manifest{Schema: 1, ID: v[0], Version: "1.0.0", Description: v[1], Compatibility: "shellstudio-v1", Origin: "local:user", License: "user-specified", Dependencies: []string{}, Detection: extensions.Detection{Executable: v[2], Args: []string{}}, Install: extensions.Install{Kind: "existing"}, Commands: map[string]platform.Profile{}, Profiles: map[string]platform.Profile{"default": p}, Default: "default"}
		b, _ := json.Marshal(man)
		if _, e := extensions.Parse(b); e != nil {
			m.fail(e)
			return nil
		}
		m.ask(m.App.Extensions.Preview(man)+"\nAdd this command?", "extensions", func() tea.Cmd {
			e := extensions.Import(m.App.Paths.Config, man)
			if e == nil {
				e = m.App.Extensions.Enable(man, true)
			}
			m.goTo("extensions")
			m.fail(e)
			return nil
		})
		return nil
	})
}
func (m *Model) offset() int { return max(0, m.Index-max(1, m.Height-11)+1) }
func (m *Model) View() string {
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	selected := lipgloss.NewStyle().Foreground(lipgloss.Color("235")).Background(lipgloss.Color("86"))
	header := accent.Render(" ShellStudio ") + "  Views · Notes · Extensions · Files"
	title := strings.ToUpper(m.Screen)
	if m.Screen == "consoles" {
		if v, e := m.App.Store.View(m.ViewID); e == nil {
			layout := map[string]string{"tiled": "Mosaico", "even-horizontal": "Columnas", "even-vertical": "Filas", "main-vertical": "Panel principal a la izquierda", "main-horizontal": "Panel principal arriba"}[v.Layout]
			if layout == "" {
				layout = "Personalizado"
			}
			title = v.Name + " · " + layout + " · " + v.Folder
		}
	}
	if m.Screen == "files" {
		title = m.folder
	}
	if m.Screen == "notes" && m.trash {
		title = "NOTES · Trash"
	}
	var body, help string
	switch m.Screen {
	case "form":
		title = m.formTitle
		labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true)
		cardWidth := max(18, min(72, m.Width-6))
		inputStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1).Width(max(10, cardWidth-14))
		for _, f := range m.fields {
			body += labelStyle.Render(explorer.Clean(f.Label)) + "\n" + inputStyle.Render(f.Input.View()) + "\n\n"
		}
		button := "[ Guardar ]"
		if strings.HasPrefix(strings.ToLower(m.formTitle), "nueva terminal") {
			button = "[ Crear terminal ]"
		} else if strings.HasPrefix(strings.ToLower(m.formTitle), "nueva consola") {
			button = "[ Crear consola ]"
		}
		body += lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true).Render(button) + "     " + lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("[ Cancelar ]")
		help = "Tab cambiar campo · Enter confirmar · Esc cancelar · Ctrl+F elegir carpeta · Ctrl+R recientes"
	case "choice":
		title = m.formTitle
		help = "↑↓ choose · Enter select · Esc back"
	case "confirm":
		title = "Review action"
		body = m.viewport.View()
		help = "y confirm · n / Esc cancel · ↑↓ scroll"
	case "editor":
		title = m.note.Title + " · " + m.saveStatus
		body = m.editor.View()
		help = "Autosave: 300 ms idle / 1 s continuous · Ctrl+S save · Esc save and leave"
	case "extension":
		title = m.selected.ID
		body = m.viewport.View()
		help = "i install · e enable · d disable · c profile · m MCP · u uninstall · X purge · Esc"
	case "text", "viewer":
		body = m.viewport.View()
		help = "↑↓ / PgUp/PgDn scroll · / filter viewer · r refresh · Esc menu · q quit"
	case "views":
		help = "n new view · d delete view · Enter consoles · Tab section · ? SSH help · q quit"
	case "consoles":
		help = "F6 lista de archivos · t nueva terminal · n otra consola · Enter abrir · f carpeta · l distribución · a vincular · d desvincular · r reiniciar · x detener"
	case "notes":
		help = "n new · Enter edit · / search · r rename/restore · d trash · t trash view · e export · Tab section"
	case "extensions":
		help = "Enter details · a import file/HTTPS · n existing command · Tab section"
	case "files":
		help = "Enter open · s select · p path · / filter · c copy · e editor · r rename · h SSH · Tab section"
	}
	if body == "" {
		start := m.offset()
		end := min(len(m.Rows), start+max(1, m.Height-11))
		for i := start; i < end; i++ {
			line := "  " + explorer.Clean(m.Rows[i].Label)
			if i == m.Index {
				line = selected.Render("› " + explorer.Clean(m.Rows[i].Label))
			}
			body += line + "\n"
		}
		if len(m.Rows) == 0 {
			body = "  Nothing here yet. Use the actions below to get started.\n"
		}
	}
	header = accent.Render("ShellStudio") + "   " + navTab("Views", m.Screen == "views") + "   " + navTab("Notes", m.Screen == "notes") + "   " + navTab("Extensions", m.Screen == "extensions") + "   " + navTab("Files", m.Screen == "files")
	contentHeight := max(1, m.Height-9)
	content := lipgloss.NewStyle().MaxWidth(m.Width).Height(contentHeight).MaxHeight(contentHeight).Render(body)
	if m.Screen == "consoles" && m.Width >= 70 {
		content = m.consoleWorkspace(contentHeight, accent, muted, selected)
	}
	if m.Screen == "form" || m.Screen == "choice" || m.Screen == "confirm" {
		cardWidth := max(18, min(72, m.Width-6))
		card := lipgloss.NewStyle().Width(cardWidth).Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("86")).Background(lipgloss.Color("235")).Render(accent.Render(title) + "\n\n" + body)
		content = lipgloss.Place(m.Width, contentHeight, lipgloss.Center, lipgloss.Center, card)
	}
	pageTitle := accent.Render(explorer.Clean(title))
	if m.Screen == "form" || m.Screen == "choice" || m.Screen == "confirm" {
		pageTitle = ""
	}
	return header + "\n" + muted.Render(strings.Repeat("─", max(1, m.Width-1))) + "\n" + pageTitle + "\n\n" + content + "\n" + muted.Render(explorer.Clean(help)) + "\n" + lipgloss.NewStyle().MaxWidth(m.Width).Render(explorer.Clean(m.Status)) + "\n"
}

func navTab(label string, active bool) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	if active {
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true).Underline(true)
	}
	return style.Render(label)
}

func (m *Model) consoleWorkspace(height int, accent, muted, selected lipgloss.Style) string {
	sidebarWidth := max(22, min(34, m.Width/3))
	mainWidth := max(20, m.Width-sidebarWidth-1)
	rowsHeight := max(1, height-2)
	var tree strings.Builder
	tree.WriteString(accent.Render("Files") + "\n")
	tree.WriteString(muted.Render(explorer.Clean(m.treePath)) + "\n")
	start := max(0, m.treeIndex-rowsHeight+1)
	for i := start; i < len(m.treeEntries) && i < start+rowsHeight; i++ {
		entry := m.treeEntries[i]
		label := entry.Name
		if entry.Dir {
			label = "▸ " + label + "/"
		} else {
			label = "   " + label
		}
		label = lipgloss.NewStyle().MaxWidth(sidebarWidth - 3).Render(explorer.Clean(label))
		if m.treeFocus && i == m.treeIndex {
			label = selected.Render(label)
		}
		tree.WriteString(label + "\n")
	}
	if len(m.treeEntries) == 0 {
		tree.WriteString(muted.Render("No se pudo leer la carpeta") + "\n")
	}
	main := ""
	if len(m.Rows) == 0 {
		main = "  No hay consolas en esta vista.\n\n  Pulsa t para crear una terminal.\n"
	} else {
		start := m.offset()
		end := min(len(m.Rows), start+rowsHeight)
		for i := start; i < end; i++ {
			line := lipgloss.NewStyle().MaxWidth(mainWidth - 2).Render(explorer.Clean(m.Rows[i].Label))
			if i == m.Index && !m.treeFocus {
				line = selected.Render(line)
			}
			main += "  " + line + "\n"
		}
	}
	left := lipgloss.NewStyle().Width(sidebarWidth).Height(height).MaxHeight(height).Render(tree.String())
	right := lipgloss.NewStyle().Width(mainWidth).Height(height).MaxHeight(height).Render(main)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, muted.Render("│"), right)
}
func (m *Model) SetViewer(kind, path string) {
	m.Screen = "viewer"
	m.viewerKind = kind
	m.viewerPath = path
	m.viewport.SetContent("Loading…")
}
func (m *Model) SetExplorer(path string) { m.folder = path; m.goTo("files") }
