// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"shellstudio/internal/diagnostics"
	"shellstudio/internal/mux"
	"shellstudio/internal/platform"
	"shellstudio/internal/store"
)

// AppError is the last failure, kept for the explorer even after the dialog
// where it happened has closed.
type AppError struct {
	ID      string `json:"id"`
	Context string `json:"context"`
	Message string `json:"message"`
	Detail  string `json:"detail"`
}

func (a *App) errorFile() string { return filepath.Join(a.Paths.State, "last-error.json") }

func (a *App) RecordError(err error, context string) {
	if err == nil {
		return
	}
	diagnostics.Record(diagnostics.Entry{Event: "app-error", Operation: context}, err, time.Time{})
	l, e := platform.LockWait(filepath.Join(a.Paths.State, "errors.lock"), 2*time.Second)
	if e != nil {
		return
	}
	defer platform.Unlock(l)
	msg := strings.TrimSpace(err.Error())
	item := AppError{store.ID(), context, msg, fmt.Sprintf("%s: %s\n%s", context, msg, strings.Join(os.Args, " "))}
	b, _ := json.Marshal(item)
	platform.ReplaceFile(a.errorFile(), b)
}
func (a *App) CurrentError() *AppError {
	b, e := os.ReadFile(a.errorFile())
	if e != nil {
		return nil
	}
	var item AppError
	if json.Unmarshal(b, &item) != nil || item.ID == "" {
		return nil
	}
	return &item
}
func (a *App) DismissError(id string) {
	l, e := platform.LockWait(filepath.Join(a.Paths.State, "errors.lock"), 2*time.Second)
	if e != nil {
		return
	}
	defer platform.Unlock(l)
	if cur := a.CurrentError(); cur != nil && cur.ID == id {
		os.Remove(a.errorFile())
	}
}

// ErrorAgent prefers the agent with the most recent activity, then history.
func (a *App) ErrorAgent() (string, error) {
	var available []string
	for _, id := range []string{"claude", "codex"} {
		if man, e := a.Manifest(id); e == nil {
			if _, e = a.Extensions.Detect(man); e == nil {
				available = append(available, id)
			}
		}
	}
	if len(available) == 0 {
		return "", errors.New("neither Claude nor Codex is installed in PATH")
	}
	cs, _ := a.Store.Consoles("")
	activity := a.Mux.Activity()
	best, at := "", int64(-1)
	for _, c := range cs {
		for _, k := range available {
			if c.Extension == k && activity[c.ID] > at {
				best, at = k, activity[c.ID]
			}
		}
	}
	if best != "" {
		return best, nil
	}
	for _, k := range a.Kinds() {
		for _, x := range available {
			if k.ID == x {
				return x, nil
			}
		}
	}
	return available[0], nil
}

// SendErrorToAgent creates an agent console that receives the error as its
// initial prompt.
func (a *App) SendErrorToAgent(item AppError, view string) (store.Console, error) {
	if cur := a.CurrentError(); cur == nil || cur.ID != item.ID {
		return store.Console{}, errors.New("the visible error changed; check the panel again")
	}
	agent, e := a.ErrorAgent()
	if e != nil {
		return store.Console{}, e
	}
	detail := item.Detail
	if len(detail) > 2500 {
		detail = detail[len(detail)-2500:]
	}
	prompt := "fix this. ShellStudio failed in " + item.Context + ": " + item.Message + "\n\nTrace:\n" + detail +
		"\n\nFind the cause, fix it and verify the affected flow."
	return a.Create(view, agent, "", []string{prompt})
}

// Resolve keeps explorer paths inside the view folder.
func Resolve(root, relative string) (string, error) {
	p := root
	if relative != "." && relative != "" {
		p = filepath.Join(root, relative)
	}
	real, e := filepath.EvalSymlinks(p)
	if e != nil {
		real = p
	}
	rootReal, e := filepath.EvalSymlinks(root)
	if e != nil {
		rootReal = root
	}
	if real != rootReal && !strings.HasPrefix(real, rootReal+string(filepath.Separator)) {
		return "", errors.New("the path is outside the view folder")
	}
	return p, nil
}

// InsertPath pastes the absolute path, quoted when needed and followed by a
// space, into the console. It goes straight to the program session as
// bracketed paste when requested: never as Enter, whatever the name contains.
func (a *App) InsertPath(viewPane, consoleID, path string, focus bool) error {
	if consoleID == "" {
		return nil
	}
	if _, e := a.Mux.Batch(false, []string{"set-buffer", "-b", "ss-path", "--", mux.Quote(path) + " "},
		[]string{"paste-buffer", "-p", "-d", "-b", "ss-path", "-t", "=c-" + consoleID + ":"}); e != nil {
		return e
	}
	if focus {
		_, e := a.Mux.Run(true, "select-pane", "-t", viewPane)
		return e
	}
	return nil
}

// Drop writes what was dragged from the explorer into the pane under the pointer.
func (a *App) Drop(origin, target string) error {
	src, e := a.PaneOwner(origin)
	if e != nil || src.Kind != "explorer" || target == "" || target == origin {
		return nil
	}
	relative, _ := a.Mux.Run(true, "show-options", "-pqv", "-t", origin, "@ss_path")
	relative = strings.TrimSpace(relative)
	dst, e := a.PaneOwner(target)
	if e != nil || dst.Console == "" || relative == "" || dst.View != src.View {
		return nil
	}
	v, e := a.Store.View(src.View)
	if e != nil {
		return e
	}
	path, e := Resolve(v.Folder, relative)
	if e != nil {
		return e
	}
	return a.InsertPath(target, dst.Console, path, true)
}

// InsertIntoLast is the explorer's I key: the path goes to the last used
// console and the focus stays in the tree, to type several paths in a row.
func (a *App) InsertIntoLast(explorerPane, view, relative string) (bool, error) {
	v, e := a.Store.View(view)
	if e != nil {
		return false, e
	}
	path, e := Resolve(v.Folder, relative)
	if e != nil {
		return false, e
	}
	o, e := a.Mux.Run(true, "list-panes", "-t", explorerPane, "-F", "#{pane_id}\t#{@ss_kind}\t#{@ss_console}\t#{pane_last}\t#{@ss_state}")
	if e != nil {
		return false, e
	}
	var chosen []string
	for _, l := range strings.Split(strings.TrimSpace(o), "\n") {
		f := strings.Split(l, "\t")
		if len(f) == 5 && f[1] == "console" && f[4] == "live" && (chosen == nil || f[3] == "1") {
			chosen = f
		}
	}
	if chosen == nil {
		return false, nil
	}
	return true, a.InsertPath(chosen[0], chosen[2], path, false)
}

// ReadableText: no NUL bytes and UTF-8 or almost all printable.
func ReadableText(path string) bool {
	f, e := os.Open(path)
	if e != nil {
		return false
	}
	defer f.Close()
	b := make([]byte, 8192)
	n, _ := f.Read(b)
	b = b[:n]
	if strings.ContainsRune(string(b), 0) {
		return false
	}
	s := string(b)
	if strings.ToValidUTF8(s, "�") == s {
		return true
	}
	printable := 0
	for _, c := range b {
		if (c >= 32 && c < 127) || c == 9 || c == 10 || c == 13 {
			printable++
		}
	}
	return printable*100 >= 95*len(b)
}

var unsafeStem = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// OpenEditor opens a new console with nano on the file, in this view and with
// the focus. Leaving nano closes the console, like an editor tab.
func (a *App) OpenEditor(view, relative string) (store.Console, error) {
	v, e := a.Store.View(view)
	if e != nil {
		return store.Console{}, e
	}
	path, e := Resolve(v.Folder, relative)
	if e != nil {
		return store.Console{}, e
	}
	fi, e := os.Stat(path)
	if e != nil || !fi.Mode().IsRegular() {
		return store.Console{}, errors.New("not a file")
	}
	if fi.Size() > 50<<20 {
		return store.Console{}, errors.New("too large for nano (more than 50 MiB)")
	}
	if !ReadableText(path) {
		return store.Console{}, errors.New("not a text file")
	}
	nano, e := exec.LookPath("nano")
	if e != nil {
		return store.Console{}, errors.New("nano is missing")
	}
	stem := strings.Trim(unsafeStem.ReplaceAllString(filepath.Base(path), "-"), "-")
	if len(stem) > 40 {
		stem = stem[:40]
	}
	if stem == "" {
		stem = "file"
	}
	name := "nano-" + stem
	cs, _ := a.Store.Consoles("")
	for _, c := range cs {
		if c.Name == name {
			name += "-" + store.ID()[:4]
			break
		}
	}
	id := store.ID()
	// --mouse: a click places the cursor and the wheel scrolls the file.
	c := store.Console{ID: id, Name: name, Extension: "nano", Folder: filepath.Dir(path),
		Argv: []string{"/bin/sh", "-c", `"$1" --mouse -- "$2"; exec "$3" _close "$4"`, "sh", nano, path, a.Binary, id}}
	if e = a.Store.AddConsole(view, c); e != nil {
		return c, e
	}
	if e = a.Mux.Start(c); e != nil {
		a.Store.DeleteConsole(id)
		return c, e
	}
	if e = a.RefreshOpen(view); e != nil {
		return c, e
	}
	a.FocusConsole(view, id)
	return c, nil
}

// RenamePath renames inside the same folder and never replaces anything.
func RenamePath(root, relative, name string) (string, error) {
	if relative == "." || relative == "" {
		return "", errors.New("the root folder is not renamed from here")
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") {
		return "", errors.New("invalid name")
	}
	src, e := Resolve(root, relative)
	if e != nil {
		return "", e
	}
	dst := filepath.Join(filepath.Dir(src), name)
	if _, e := os.Lstat(dst); e == nil {
		return "", fmt.Errorf("%s already exists", name)
	}
	if e := os.Rename(src, dst); e != nil {
		return "", e
	}
	return filepath.ToSlash(filepath.Join(filepath.Dir(relative), name)), nil
}
