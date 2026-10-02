// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"shellstudio/internal/extensions"
	"shellstudio/internal/mux"
	"shellstudio/internal/platform"
	"shellstudio/internal/store"
)

type App struct {
	Paths       platform.Paths
	Store       *store.Store
	Mux         *mux.Mux
	Extensions  extensions.Manager
	CWD, Binary string
}

func Open() (*App, error) {
	p, e := platform.Discover()
	if e != nil {
		return nil, e
	}
	s, e := store.Open(filepath.Join(p.Data, "shellstudio.db"))
	if e != nil {
		return nil, e
	}
	cwd, e := os.Getwd()
	if e != nil {
		s.Close()
		return nil, e
	}
	binary, e := os.Executable()
	if e != nil {
		s.Close()
		return nil, e
	}
	a := &App{Paths: p, Store: s, CWD: cwd, Binary: binary}
	a.Mux = mux.New(p, binary)
	a.Extensions = extensions.Manager{Paths: p, Binary: binary, Log: s.Event}
	a.Mux.Version = a.configVersion()
	return a, nil
}
func (a *App) Close() { a.Store.Close() }

// The controls change when the executable or the console kinds change; a
// running server is reconfigured only then.
func (a *App) configVersion() string {
	h := sha256.New()
	fmt.Fprint(h, a.Binary)
	if fi, e := os.Stat(a.Binary); e == nil {
		fmt.Fprint(h, fi.ModTime().UnixNano(), fi.Size())
	}
	for _, k := range a.Kinds() {
		fmt.Fprint(h, "|", k.ID, k.Key)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func (a *App) Manifest(id string) (extensions.Manifest, error) {
	for _, v := range extensions.Catalog(a.Paths.Config) {
		if v.Error == "" && v.Manifest.ID == id {
			return v.Manifest, nil
		}
	}
	return extensions.Manifest{}, fmt.Errorf("extension %s not found", id)
}

// Kind is a console type: the built-in Terminal or an available extension.
type Kind struct{ ID, Label, Key string }

var builtinKinds = map[string][2]string{
	"claude": {"Claude", "C"}, "codex": {"Codex", "X"}, "terminal": {"Terminal", "T"},
	"notes": {"Notes", "O"}, "agent-chat": {"Agent Chat", "H"}, "agent-gantt": {"Agent Gantt", "G"},
}

// Letters taken by the view menu and the console list.
const reservedKeys = "ASLVBEPDQNRMIFJKUWYZ"

// Kinds lists Terminal and every extension that is enabled or whose program is
// installed, the most recently created first: Enter repeats it.
func (a *App) Kinds() []Kind {
	all := []Kind{}
	used := map[string]bool{}
	add := func(id, label, key string) {
		if key != "" && used[key] {
			key = ""
		}
		if key != "" {
			used[key] = true
		}
		all = append(all, Kind{id, label, key})
	}
	present := map[string]extensions.Manifest{}
	var others []string
	for _, e := range extensions.Catalog(a.Paths.Config) {
		if e.Error != "" || e.Manifest.ID == "terminal" {
			continue
		}
		if a.Extensions.Enabled(e.Manifest) {
			present[e.Manifest.ID] = e.Manifest
		} else if _, err := a.Extensions.Detect(e.Manifest); err == nil {
			present[e.Manifest.ID] = e.Manifest
		} else {
			continue
		}
		if _, ok := builtinKinds[e.Manifest.ID]; !ok {
			others = append(others, e.Manifest.ID)
		}
	}
	for _, id := range []string{"claude", "codex", "terminal", "notes", "agent-chat", "agent-gantt"} {
		if _, ok := present[id]; ok || id == "terminal" {
			b := builtinKinds[id]
			add(id, b[0], b[1])
		}
	}
	sort.Strings(others)
	for _, id := range others {
		key := ""
		for _, r := range strings.ToUpper(id) {
			s := string(r)
			if r >= 'A' && r <= 'Z' && !used[s] && !strings.Contains(reservedKeys, s) {
				key = s
				break
			}
		}
		add(id, id, key)
	}
	recent := a.readList("last-kinds")
	order := map[string]int{}
	for i, id := range recent {
		if _, ok := order[id]; !ok {
			order[id] = i
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		oi, iok := order[all[i].ID]
		oj, jok := order[all[j].ID]
		if iok != jok {
			return iok
		}
		return iok && oi < oj
	})
	return all
}
func (a *App) Kind(id string) (Kind, bool) {
	for _, k := range a.Kinds() {
		if k.ID == id {
			return k, true
		}
	}
	return Kind{}, false
}
func (a *App) RememberKind(id string) {
	list := []string{id}
	for _, k := range a.readList("last-kinds") {
		if k != id && len(list) < 20 {
			list = append(list, k)
		}
	}
	platform.ReplaceFile(filepath.Join(a.Paths.Config, "last-kinds"), []byte(strings.Join(list, "\n")+"\n"))
}
func (a *App) readList(name string) []string {
	b, e := os.ReadFile(filepath.Join(a.Paths.Config, name))
	if e != nil {
		return nil
	}
	return strings.Fields(string(b))
}

func SuggestedName() string { return "session-" + store.ID()[:8] }

// ValidName keeps console names readable in headers and menus.
func ValidName(name string) error {
	if name == "" || len([]rune(name)) > 80 {
		return errors.New("use 1–80 characters")
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '#' {
			return errors.New("the name cannot contain control characters or #")
		}
	}
	return nil
}

// profile resolves the argv of a kind. A detected program that was never
// enabled is enabled here: choosing it in F3 is that explicit decision.
func (a *App) profile(kind, profile string) (platform.Profile, error) {
	if kind == "terminal" {
		return platform.Profile{Executable: LoginShell(), Args: []string{"-l"}}, nil
	}
	man, e := a.Manifest(kind)
	if e != nil {
		return platform.Profile{}, e
	}
	if !a.Extensions.Enabled(man) {
		if _, e = a.Extensions.Detect(man); e != nil {
			return platform.Profile{}, fmt.Errorf("%s was not found in PATH", man.Detection.Executable)
		}
		if e = a.Extensions.Enable(man, true); e != nil {
			return platform.Profile{}, e
		}
	}
	return a.Extensions.Profile(man, profile)
}

func LoginShell() string {
	for _, s := range []string{os.Getenv("SHELL"), "/bin/bash", "/bin/sh"} {
		if filepath.IsAbs(s) {
			if fi, e := os.Stat(s); e == nil && fi.Mode()&0111 != 0 {
				return s
			}
		}
	}
	return "/bin/sh"
}

// Launch creates a console in a view and starts its program. A program that
// cannot start leaves no metadata behind.
func (a *App) Launch(view, name, kind, profile, folder string, extra []string) (store.Console, error) {
	v, e := a.Store.View(view)
	if e != nil {
		return store.Console{}, fmt.Errorf("view not found: %w", e)
	}
	path, e := platform.Directory(folder, v.Folder, a.CWD)
	if e != nil {
		return store.Console{}, e
	}
	p, e := a.profile(kind, profile)
	if e != nil {
		return store.Console{}, e
	}
	if _, e = exec.LookPath(p.Executable); e != nil {
		return store.Console{}, fmt.Errorf("%s was not found in PATH", p.Executable)
	}
	if name == "" {
		name = kind
	}
	if e = ValidName(name); e != nil {
		return store.Console{}, e
	}
	c := store.Console{ID: store.ID(), Name: name, Extension: kind, Profile: profile, Folder: path,
		Argv: append(append([]string{p.Executable}, p.Args...), extra...), Env: p.Env}
	if e = a.Store.AddConsole(view, c); e != nil {
		return c, e
	}
	if e = a.Mux.Start(c); e != nil {
		a.Store.DeleteConsole(c.ID)
		return c, e
	}
	platform.Remember(a.Paths.Config, path)
	return c, nil
}

// Create is F3: kind-label in the view, which is redrawn if open.
func (a *App) Create(view, kind, label string, extra []string) (store.Console, error) {
	if label == "" {
		label = SuggestedName()
	}
	c, e := a.Launch(view, kind+"-"+label, kind, "", "", extra)
	if e != nil {
		return c, e
	}
	a.RememberKind(kind)
	return c, a.RefreshOpen(view)
}
func (a *App) Restart(id string) error {
	c, e := a.Store.Console(id)
	if e != nil {
		return e
	}
	a.Store.Event("console-restart", id)
	if e = a.Mux.Start(c); e != nil {
		return e
	}
	views, _ := a.Store.ConsoleViews(id)
	return a.RefreshOpen(views...)
}

// Stop ends the program and keeps the console, which can be restarted.
func (a *App) Stop(id string) error {
	a.Store.Event("console-stop", id)
	return a.Mux.Stop(id)
}

// Kill ends the program and removes the console from every view. Its panes go
// first: if the nested client died before, the pane would say "disconnected".
func (a *App) Kill(id string) error {
	if _, e := a.Store.Console(id); e != nil {
		return errors.New("the console no longer exists")
	}
	views, e := a.Store.ConsoleViews(id)
	if e != nil {
		return e
	}
	if e = a.Store.DeleteConsole(id); e != nil {
		return e
	}
	re := a.RefreshOpen(views...)
	if e = a.Mux.Stop(id); e != nil {
		return e
	}
	return re
}
func (a *App) Rename(id, name string) error {
	if e := ValidName(name); e != nil {
		return e
	}
	c, e := a.Store.Console(id)
	if e != nil {
		return errors.New("the console no longer exists")
	}
	if c.Name == name {
		return nil
	}
	if e = a.Store.RenameConsole(id, name); e != nil {
		return e
	}
	if e = a.Mux.SetName(id, name); e != nil {
		return e
	}
	views, _ := a.Store.ConsoleViews(id)
	return a.RefreshOpen(views...)
}

// Replace puts a new console in the place of the old one in every view. The
// old program ends last, so its place is never empty.
func (a *App) Replace(view, old, kind, label string) (store.Console, error) {
	members, e := a.Store.Consoles(view)
	if e != nil {
		return store.Console{}, e
	}
	var prev *store.Console
	for i := range members {
		if members[i].ID == old {
			prev = &members[i]
		}
	}
	if prev == nil {
		return store.Console{}, errors.New("the selected console no longer belongs to this view")
	}
	if label == "" {
		label = SuggestedName()
	}
	name := kind + "-" + label
	if e = ValidName(name); e != nil {
		return store.Console{}, e
	}
	p, e := a.profile(kind, "")
	if e != nil {
		return store.Console{}, e
	}
	if _, e = exec.LookPath(p.Executable); e != nil {
		return store.Console{}, fmt.Errorf("%s was not found in PATH", p.Executable)
	}
	c := store.Console{ID: store.ID(), Name: name, Extension: kind, Folder: prev.Folder,
		Argv: append([]string{p.Executable}, p.Args...), Env: p.Env}
	if e = a.Store.Replace(old, c); e != nil {
		return c, e
	}
	a.RememberKind(kind)
	start := a.Mux.Start(c)
	views, _ := a.Store.ConsoleViews(c.ID)
	re := a.RefreshOpen(views...)
	a.Mux.Stop(old)
	if start != nil {
		return c, start
	}
	return c, re
}

func (a *App) RemoveFromView(view, id string) error {
	if e := a.Store.Unlink(view, id); e != nil {
		return e
	}
	return a.RefreshOpen(view)
}
func (a *App) AddToView(view, id string) error {
	if _, e := a.Store.Console(id); e != nil {
		return errors.New("the console no longer exists")
	}
	if e := a.Store.Link(view, id); e != nil {
		return e
	}
	return a.RefreshOpen(view)
}

// Move shifts a console one place, circularly.
func (a *App) Move(view, id string, delta int) error {
	cs, e := a.Store.Consoles(view)
	if e != nil {
		return e
	}
	ids := make([]string, len(cs))
	index := -1
	for i, c := range cs {
		ids[i] = c.ID
		if c.ID == id {
			index = i
		}
	}
	if index < 0 || len(ids) < 2 {
		return nil
	}
	other := ((index+delta)%len(ids) + len(ids)) % len(ids)
	ids[index], ids[other] = ids[other], ids[index]
	if e = a.Store.SetOrder(view, ids); e != nil {
		return e
	}
	return a.RefreshOpen(view)
}
func (a *App) ApplyLayout(view, layout, focused string) error {
	if !mux.ValidLayout(layout) {
		return errors.New("invalid layout")
	}
	v, e := a.Store.View(view)
	if e != nil {
		return e
	}
	v.Layout = layout
	if e = a.Store.UpdateView(v); e != nil {
		return e
	}
	if (layout == "main-vertical" || layout == "main-horizontal") && focused != "" {
		if e = a.Store.SetOrder(view, []string{focused}); e != nil {
			return e
		}
	}
	return a.RefreshOpen(view)
}
func (a *App) ToggleExplorer(view string) error {
	v, e := a.Store.View(view)
	if e != nil {
		return e
	}
	v.Explorer = !v.Explorer
	if e = a.Store.UpdateView(v); e != nil {
		return e
	}
	return a.RefreshOpen(view)
}

func (a *App) NewView(name, folder string, members []string, layout string) (store.View, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return store.View{}, errors.New("use 1–80 characters for the view name")
	}
	views, e := a.Store.Views()
	if e != nil {
		return store.View{}, e
	}
	for _, v := range views {
		if v.Name == name {
			return store.View{}, fmt.Errorf("the view %s already exists", name)
		}
	}
	p, e := platform.Directory(folder, "", a.CWD)
	if e != nil {
		return store.View{}, e
	}
	v, e := a.Store.NewView(name, p)
	if e != nil {
		return v, e
	}
	for _, id := range members {
		if e = a.Store.Link(v.ID, id); e != nil {
			return v, e
		}
	}
	if layout != "" && layout != v.Layout {
		v.Layout = layout
		if e = a.Store.UpdateView(v); e != nil {
			return v, e
		}
	}
	a.publishViewCount()
	return v, nil
}

// DeleteView disconnects its clients; its consoles keep running.
func (a *App) DeleteView(id string) error {
	if e := a.Store.DeleteView(id); e != nil {
		return e
	}
	a.Mux.Run(true, "kill-session", "-t", "="+mux.ViewSession(id))
	a.publishViewCount()
	return nil
}
func (a *App) publishViewCount() {
	vs, e := a.Store.Views()
	if e == nil {
		a.Mux.Run(true, "set-option", "-g", "@ss_views", fmt.Sprint(len(vs)))
	}
}

// SortedViews orders views by name, as F7 and Ctrl+Shift+arrows walk them.
func (a *App) SortedViews() ([]store.View, error) {
	vs, e := a.Store.Views()
	sort.SliceStable(vs, func(i, j int) bool { return strings.ToLower(vs[i].Name) < strings.ToLower(vs[j].Name) })
	return vs, e
}
func (a *App) RememberView(id string) {
	platform.ReplaceFile(filepath.Join(a.Paths.Config, "last-view"), []byte(id+"\n"))
}

// LastView resumes the last visited view, or the first one.
func (a *App) LastView() (store.View, error) {
	if l := a.readList("last-view"); len(l) == 1 {
		if v, e := a.Store.View(l[0]); e == nil {
			return v, nil
		}
	}
	vs, e := a.SortedViews()
	if e != nil {
		return store.View{}, e
	}
	if len(vs) > 0 {
		return vs[0], nil
	}
	return a.Workspace("")
}

// Status of a console for lists: running, finished or stopped.
func Status(programs map[string]bool, id string) string {
	alive, ok := programs[id]
	if !ok {
		return "stopped"
	}
	if alive {
		return "running"
	}
	return "finished"
}
