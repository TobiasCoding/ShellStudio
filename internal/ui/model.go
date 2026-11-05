// SPDX-License-Identifier: GPL-3.0-only
package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"shellstudio/internal/app"
	"shellstudio/internal/diagnostics"
	"shellstudio/internal/explorer"
	"shellstudio/internal/extensions"
	"shellstudio/internal/i18n"
	"shellstudio/internal/mcp"
	"shellstudio/internal/platform"
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
	Screen, Status, home                 string
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
	e.Placeholder = i18n.T("notes.placeholder")
	e.Prompt = "│ "
	e.Focus()
	m := &Model{App: a, Client: store.ID(), Screen: screen, Width: 100, Height: 30, editor: e, viewport: viewport.New(90, 20), saveStatus: "Saved"}
	if m.Screen == "" {
		m.Screen = "notes"
	}
	m.home = m.Screen
	m.refresh()
	return m
}
func (m *Model) Init() tea.Cmd {
	return tea.Batch(tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) }), textinput.Blink)
}
func (m *Model) fail(e error) {
	if e != nil {
		diagnostics.Record(diagnostics.Entry{Event: "ui-error", Operation: m.Screen}, e, time.Time{})
		m.Status = i18n.T("notes.error", e.Error())
	}
}
func (m *Model) refresh() {
	m.Rows = nil
	switch m.Screen {
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
				label = i18n.T("notes.ext_invalid", c.Source, c.Error)
			} else if m.App.Extensions.Enabled(c.Manifest) {
				label += "  " + i18n.T("notes.ext_enabled")
			} else {
				label += "  " + i18n.T("notes.ext_disabled")
			}
			m.Rows = append(m.Rows, Row{fmt.Sprint(i), label})
		}
	}
	m.Index = max(0, min(m.Index, len(m.Rows)-1))
}
func (m *Model) goTo(screen string) { m.Screen = screen; m.Index = 0; m.Status = ""; m.refresh() }
func (m *Model) op(work func() (any, error), after func(any)) tea.Cmd {
	m.busy = true
	m.after = after
	m.Status = i18n.T("notes.working")
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
		started := time.Now()
		diagnostics.Record(diagnostics.Entry{Event: "note-save-start", Reference: n.ID}, nil, time.Time{})
		saved, conflict, e := s.SaveNote(n)
		operation := "save"
		if conflict {
			operation = "conflict-copy"
		}
		diagnostics.Record(diagnostics.Entry{Event: "note-save-end", Operation: operation, Reference: n.ID}, e, started)
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
			m.Status = i18n.T("notes.done")
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
			m.Status = i18n.T("notes.error", v.err.Error())
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
			m.Status = i18n.T("notes.conflict")
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
			if v.Button == tea.MouseButtonWheelDown || v.Button == tea.MouseButtonWheelUp {
				delta := 1
				if v.Button == tea.MouseButtonWheelUp {
					delta = -1
				}
				m.Index = max(0, min(max(0, len(m.Rows)-1), m.Index+delta))
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
					return m, m.activate()
				}
			}
		}
	case tea.KeyMsg:
		k := v.String()
		if m.busy {
			if k == "ctrl+c" {
				m.Status = i18n.T("notes.busy")
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
			if m.Screen == "choice" {
				m.goTo(m.formBack)
			}
			return m, nil
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
	case "notes":
		if m.trash {
			m.Status = i18n.T("notes.restore_hint")
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
			m.viewport.SetContent(m.App.Extensions.Preview(m.selected) + "\n" + i18n.T("notes.ext_source", e.Source) + "\n\n" + m.selected.Description)
			m.viewport.GotoTop()
			m.Screen = "extension"
		}
	case "choice":
		return m.choice(id)
	}
	return nil
}

func (m *Model) key(k string) tea.Cmd {
	id := m.selectedID()
	switch m.Screen {
	case "notes":
		switch k {
		case "n":
			m.form(i18n.T("notes.new"), "notes", []string{i18n.T("notes.field_title")}, []string{i18n.T("notes.untitled")}, func(v []string) tea.Cmd {
				n, e := m.App.Store.NewNote(v[0])
				m.fail(e)
				if e == nil {
					m.openNote(n)
				}
				return nil
			})
		case "/":
			m.form(i18n.T("notes.search"), "notes", []string{i18n.T("notes.field_text")}, []string{m.search}, func(v []string) tea.Cmd { m.search = v[0]; m.goTo("notes"); return nil })
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
					m.form(i18n.T("notes.rename"), "notes", []string{i18n.T("notes.field_title")}, []string{n.Title}, func(v []string) tea.Cmd {
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
				m.form(i18n.T("notes.export"), "notes", []string{i18n.T("notes.export_dest")}, []string{filepath.Join(m.App.CWD, "note-"+n.ID+".md")}, func(v []string) tea.Cmd {
					path := v[0]
					m.ask(i18n.T("notes.export_ask", path), "notes", func() tea.Cmd {
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
			m.form(i18n.T("notes.ext_import"), "extensions", []string{i18n.T("notes.ext_manifest")}, nil, func(v []string) tea.Cmd {
				return m.op(func() (any, error) { return extensions.ImportPreview(v[0]) }, func(x any) {
					man := x.(extensions.Manifest)
					m.ask(m.App.Extensions.Preview(man)+"\n"+i18n.T("notes.ext_import_ok"), "extensions", func() tea.Cmd {
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
	}
	return nil
}
func (m *Model) detailKey(k string, msg tea.Msg) tea.Cmd {
	if k == "esc" {
		if m.Screen == "extension" {
			m.goTo("extensions")
		} else if m.Screen == "text" && m.textBack != "" {
			back := m.textBack
			m.textBack = ""
			m.goTo(back)
		} else if m.home != m.Screen {
			m.goTo(m.home)
		}
		return nil
	}
	if m.Screen == "viewer" {
		if k == "/" {
			m.form(i18n.T("notes.filter"), "viewer", []string{i18n.T("notes.field_text")}, []string{m.viewerFilter}, func(v []string) tea.Cmd {
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
			m.ask(m.App.Extensions.Preview(man)+"\n"+i18n.T("notes.ext_install"), "extensions", func() tea.Cmd {
				return m.op(func() (any, error) { return nil, m.App.Extensions.Install(man) }, func(any) { m.goTo("extensions") })
			})
			return nil
		case "e", "d":
			enable := k == "e"
			m.fail(m.App.Extensions.Enable(man, enable))
			return nil
		case "u", "X":
			purge := k == "X"
			text := i18n.T("notes.ext_uninstall", man.ID)
			if purge {
				text += "\n" + i18n.T("notes.ext_purge")
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
			m.form(i18n.T("notes.ext_profile", man.ID), "extensions", []string{i18n.T("notes.ext_prof_name"), i18n.T("notes.ext_exec"), i18n.T("notes.ext_args"), i18n.T("notes.ext_env")}, []string{name, p.Executable, string(a), string(env)}, func(v []string) tea.Cmd {
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
				m.Status = i18n.T("notes.ext_no_mcp")
				return nil
			}
			if !m.App.Extensions.Enabled(man) {
				m.Status = i18n.T("notes.ext_mcp_first")
				return nil
			}
			m.choose(i18n.T("notes.ext_register"), "extensions", []Row{{"claude", "Claude"}, {"codex", "Codex"}}, func(client string) tea.Cmd {
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
					text += "\n" + i18n.T("notes.ext_absent")
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
	m.form(i18n.T("notes.ext_command"), "extensions", []string{i18n.T("notes.ext_id"), i18n.T("notes.ext_desc"), i18n.T("notes.ext_exec"), i18n.T("notes.ext_args"), i18n.T("notes.ext_env")}, []string{"my-tool", i18n.T("notes.ext_my_cmd"), "", "[]", "{}"}, func(v []string) tea.Cmd {
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
		m.ask(m.App.Extensions.Preview(man)+"\n"+i18n.T("notes.ext_add_cmd"), "extensions", func() tea.Cmd {
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
	title := m.screenTitle()
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
		body += lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true).Render(i18n.T("notes.save")) + "     " + lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(i18n.T("notes.cancel"))
		help = i18n.T("notes.help_form")
	case "choice":
		title = m.formTitle
		help = i18n.T("notes.help_choice")
	case "confirm":
		title = i18n.T("notes.review")
		body = m.viewport.View()
		help = i18n.T("notes.help_confirm")
	case "editor":
		title = m.note.Title + " · " + m.saveLabel()
		body = m.editor.View()
		help = i18n.T("notes.help_editor")
	case "extension":
		title = m.selected.ID
		body = m.viewport.View()
		help = i18n.T("notes.help_ext")
	case "text", "viewer":
		body = m.viewport.View()
		help = i18n.T("notes.help_viewer")
	case "notes":
		help = i18n.T("notes.help_notes")
	case "extensions":
		help = i18n.T("notes.help_exts")
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
			body = "  " + i18n.T("notes.empty") + "\n"
		}
	}
	contentHeight := max(1, m.Height-9)
	content := lipgloss.NewStyle().MaxWidth(m.Width).Height(contentHeight).MaxHeight(contentHeight).Render(body)
	if m.Screen == "form" || m.Screen == "choice" || m.Screen == "confirm" {
		cardWidth := max(18, min(72, m.Width-6))
		card := lipgloss.NewStyle().Width(cardWidth).Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("86")).Background(lipgloss.Color("235")).Render(accent.Render(title) + "\n\n" + body)
		content = lipgloss.Place(m.Width, contentHeight, lipgloss.Center, lipgloss.Center, card)
	}
	// The console header above already names the program: no second bar.
	return accent.Render(explorer.Clean(title)) + "\n" + muted.Render(strings.Repeat("─", max(1, m.Width-1))) + "\n\n\n" + content + "\n" + muted.Render(explorer.Clean(help)) + "\n" + lipgloss.NewStyle().MaxWidth(m.Width).Render(explorer.Clean(m.Status)) + "\n"
}

// screenTitle is the title of the current screen; form, choice and the
// other detail screens replace it in View.
func (m *Model) screenTitle() string {
	switch {
	case m.Screen == "notes" && m.trash:
		return i18n.T("notes.trash_title")
	case m.Screen == "notes":
		return i18n.T("notes.title")
	case m.Screen == "extensions":
		return i18n.T("notes.ext_title")
	case m.Screen == "viewer":
		return i18n.T("notes.viewer_title")
	case m.Screen == "text":
		return i18n.T("notes.text_title")
	}
	return strings.ToUpper(m.Screen)
}

// saveLabel shows saveStatus, kept in English as state, in the current language.
func (m *Model) saveLabel() string {
	switch m.saveStatus {
	case "Saving":
		return i18n.T("notes.saving")
	case "Saved":
		return i18n.T("notes.saved")
	}
	if detail, ok := strings.CutPrefix(m.saveStatus, "Error: "); ok {
		return i18n.T("notes.error", detail)
	}
	return m.saveStatus
}

func (m *Model) SetViewer(kind, path string) {
	m.Screen = "viewer"
	m.viewerKind = kind
	m.viewerPath = path
	m.viewport.SetContent(i18n.T("notes.loading"))
}
