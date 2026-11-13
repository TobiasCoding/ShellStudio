// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"shellstudio/internal/mux"
	"shellstudio/internal/platform"
)

// ShowError addresses only the client that initiated an action. The text goes
// through a private file, never through tmux formats or a shell command.
func (a *App) ShowError(client, title, detail string) error {
	if client == "" {
		return nil
	}
	f, e := os.CreateTemp(a.Paths.Runtime, "message-*.json")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	e = json.NewEncoder(f).Encode(struct{ Title, Detail string }{title, detail})
	f.Close()
	if e != nil {
		return e
	}
	// Popups wait for user input; the five-second command timeout is for
	// background tmux operations, not for reading a message.
	argv := a.Mux.ModalPopup(client, "80%", "60%", a.Mux.Self("_message", f.Name()))
	return exec.Command(argv[0], argv[1:]...).Run()
}

func token(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:24]
}

// startDetached runs a command in its own session: it survives the pane or
// popup that asked for it.
func startDetached(argv []string) error {
	c := exec.Command(argv[0], argv[1:]...)
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	c.Env = os.Environ()
	if e := c.Start(); e != nil {
		return e
	}
	go c.Wait()
	return nil
}

// WindowHeight is the height of a view window in a client of the given
// height: the top bar (the views server's status line) takes its first rows.
func WindowHeight(client int) int { return max(1, client-mux.TopBarRows) }

// Detached runs this executable with arguments, detached from the caller.
func (a *App) Detached(args ...string) error {
	return startDetached(append([]string{a.Binary}, args...))
}

type windowResize struct {
	Session string `json:"session"`
	Client  string `json:"client"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	At      int64  `json:"at"`
}

// ResizeWindow groups bursts of SSH terminal resizes per view window: the
// change is applied ~200 ms after the last one.
func (a *App) ResizeWindow(client, worker string, immediate bool) {
	if _, e := os.Stat(a.Mux.Socket(true)); e != nil {
		return
	}
	if worker == "" {
		o, e := a.Mux.Run(true, "list-clients", "-F", "#{client_name}\t#{client_session}\t#{client_width}\t#{client_height}")
		if e != nil {
			return
		}
		var row []string
		for _, l := range strings.Split(strings.TrimSpace(o), "\n") {
			if f := strings.Split(l, "\t"); len(f) == 4 && f[0] == client {
				row = f
			}
		}
		// On detach, keep the view of a client that still looks at it.
		if row == nil || row[1] == "" {
			return
		}
		if immediate {
			height, _ := strconv.Atoi(row[3])
			if _, e := a.Mux.Run(true, "resize-window", "-t", "="+row[1]+":", "-x", row[2], "-y", strconv.Itoa(WindowHeight(height))); e == nil {
				width, _ := strconv.Atoi(row[2])
				a.resizeExplorer(row[1], width)
			}
			return
		}
		width, _ := strconv.Atoi(row[2])
		height, _ := strconv.Atoi(row[3])
		t := token(row[1])
		l, e := platform.LockWait(filepath.Join(a.Paths.Runtime, "window-resize-"+t+".state.lock"), 2*time.Second)
		if e != nil {
			return
		}
		writeJSON(filepath.Join(a.Paths.Runtime, "window-resize-"+t+".json"), windowResize{row[1], client, width, height, time.Now().UnixNano()})
		platform.Unlock(l)
		a.Detached("_resize_window", "--worker", row[1])
		return
	}
	t := token(worker)
	path := filepath.Join(a.Paths.Runtime, "window-resize-"+t+".json")
	wl, e := os.OpenFile(filepath.Join(a.Paths.Runtime, "window-resize-"+t+".worker.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return
	}
	defer wl.Close()
	if syscall.Flock(int(wl.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return
	}
	read := func() (windowResize, bool) {
		var st windowResize
		b, e := os.ReadFile(path)
		if e != nil || json.Unmarshal(b, &st) != nil {
			return st, false
		}
		return st, true
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, e := os.Stat(a.Mux.Socket(true)); e != nil {
			return
		}
		st, ok := read()
		if !ok {
			return
		}
		if wait := 200*time.Millisecond - time.Since(time.Unix(0, st.At)); wait > 0 {
			time.Sleep(min(wait, 200*time.Millisecond))
			continue
		}
		l, e := platform.LockWait(filepath.Join(a.Paths.Runtime, "window-resize-"+t+".state.lock"), 2*time.Second)
		if e != nil {
			return
		}
		cur, ok := read()
		if !ok || cur.At != st.At {
			platform.Unlock(l)
			continue
		}
		o, _ := a.Mux.Run(true, "list-clients", "-F", "#{client_name}\t#{client_session}\t#{client_width}\t#{client_height}")
		var attached [][]string
		found := false
		for _, line := range strings.Split(strings.TrimSpace(o), "\n") {
			if f := strings.Split(line, "\t"); len(f) == 4 && f[1] == st.Session {
				attached = append(attached, f)
				found = found || f[0] == st.Client
			}
		}
		if len(attached) == 0 {
			os.Remove(path)
			platform.Unlock(l)
			return
		}
		if !found {
			// The client that started the change left; use one still looking.
			st.Width, _ = strconv.Atoi(attached[0][2])
			st.Height, _ = strconv.Atoi(attached[0][3])
		}
		_, e = a.Mux.Run(true, "resize-window", "-t", "="+st.Session+":", "-x", strconv.Itoa(st.Width), "-y", strconv.Itoa(WindowHeight(st.Height)))
		os.Remove(path)
		platform.Unlock(l)
		if e != nil {
			// On failure, tmux follows the client size again.
			a.Mux.Run(true, "set-option", "-w", "-t", "="+st.Session+":", "window-size", "latest")
		} else {
			a.resizeExplorer(st.Session, st.Width)
		}
		return
	}
	// A stuck worker never leaves a view frozen indefinitely.
	a.Mux.Run(true, "set-option", "-w", "-t", "="+worker+":", "window-size", "latest")
}

// A narrow terminal can temporarily hide the tree. Restore it when there is
// room again, without changing the user's saved preference or rebuilding
// console panes on ordinary resizes.
func (a *App) resizeExplorer(session string, width int) {
	view := strings.TrimPrefix(session, "v-")
	v, e := a.Store.View(view)
	if e != nil {
		return
	}
	out, e := a.Mux.Run(true, "list-panes", "-t", "="+session+":", "-F", "#{@ss_kind}\t#{pane_id}\t#{pane_width}\t#{pane_height}\t#{window_height}")
	if e != nil {
		return
	}
	explorer, minimized := false, false
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 5 || (f[0] != "explorer" && f[0] != "minimized") {
			continue
		}
		if f[0] == "explorer" {
			explorer = true
		} else {
			minimized = true
		}
		paneWidth, _ := strconv.Atoi(f[2])
		// tmux can shrink the leftmost pane to two columns when a detached
		// window is attached to a smaller client. Keep its controls readable.
		// A saved width (or height) is the user's: tmux scales panes when the
		// window grows from the size it was created at.
		if want := LeftWidth(v, width); width >= 70 && (paneWidth < min(20, width/4) || (v.ExplorerWidth > 0 && paneWidth != want)) {
			a.Mux.Run(true, "resize-pane", "-t", f[1], "-x", strconv.Itoa(want))
		}
		if f[0] == "minimized" && explorer && v.MinimizedHeight > 0 {
			h, _ := strconv.Atoi(f[3])
			wh, _ := strconv.Atoi(f[4])
			if want := MinimizedHeight(v, wh); h != want {
				a.Mux.Run(true, "resize-pane", "-t", f[1], "-y", strconv.Itoa(want))
			}
		}
	}
	wantExplorer, wantMinimized := leftColumn(v, width)
	if explorer != wantExplorer || minimized != wantMinimized {
		if e := a.Sync(view); e != nil {
			a.RecordError(e, "Resize explorer")
		}
	}
}

// SavePanelSizes stores the width of the left column and the height of the
// Minimized panel of the view that contains a pane, after the user drags a
// divider. Narrow windows, where the column is hidden, keep the saved sizes.
func (a *App) SavePanelSizes(pane string) error {
	o, e := a.Mux.Run(true, "display-message", "-p", "-t", pane, "#{session_name}\t#{window_width}")
	if e != nil {
		return e
	}
	session, cols, _ := strings.Cut(strings.TrimSpace(o), "\t")
	if w, _ := strconv.Atoi(cols); w < 70 || !strings.HasPrefix(session, "v-") {
		return nil
	}
	out, e := a.Mux.Run(true, "list-panes", "-t", "="+session+":", "-F", "#{@ss_kind}\t#{pane_width}\t#{pane_height}")
	if e != nil {
		return e
	}
	var ew, mw, mh = -1, -1, -1
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			continue
		}
		switch f[0] {
		case "explorer":
			ew, _ = strconv.Atoi(f[1])
		case "minimized":
			mw, _ = strconv.Atoi(f[1])
			mh, _ = strconv.Atoi(f[2])
		}
	}
	width, height := ew, mh
	if ew < 0 {
		// Alone in the column, the panel's height is the window's.
		width, height = mw, -1
	}
	if width < 0 && height < 0 {
		return nil
	}
	return a.Store.SetPanelSizes(strings.TrimPrefix(session, "v-"), width, height)
}

// Chain opens another dialog in its own popup, sized to its content, as soon
// as this one closes (see mux.PopupShell).
func (a *App) Chain(client, action, p string, back bool) error {
	o, e := a.Mux.Run(true, "display-message", "-p", "-c", client, "#{client_pid}\t#{client_width}\t#{client_height}\t#{session_name}")
	if e != nil {
		return e
	}
	f := strings.Split(strings.TrimSpace(o), "\t")
	if len(f) != 4 {
		return nil
	}
	cols, _ := strconv.Atoi(f[1])
	lines, _ := strconv.Atoi(f[2])
	w, h := a.DialogSize(action, strings.TrimPrefix(f[3], "v-"))
	if action == "menu" && cols < w {
		s := a.DialogSizes()
		w, h = cols, s.MenuStacked
	}
	line := a.Mux.PopupLine(client, action, min(w, cols), min(h, lines), p, back)
	return platform.ReplaceFile(filepath.Join(a.Paths.Runtime, "next-"+f[0]), []byte(line+"\n"))
}

// OpenDialog opens a variable-size dialog from a tmux menu or the explorer.
// It waits for the popup and for any dialog chained after it.
func (a *App) OpenDialog(action, client, p string) error {
	o, e := a.Mux.Run(true, "display-message", "-p", "-c", client, "#{client_pid}\t#{client_width}\t#{client_height}\t#{session_name}")
	if e != nil {
		return e
	}
	f := strings.Split(strings.TrimSpace(o), "\t")
	if len(f) != 4 {
		return nil
	}
	cols, _ := strconv.Atoi(f[1])
	lines, _ := strconv.Atoi(f[2])
	w, h := a.DialogSize(action, strings.TrimPrefix(f[3], "v-"))
	if action == "menu" && cols < w {
		w, h = cols, a.DialogSizes().MenuStacked
	}
	marker := filepath.Join(a.Paths.Runtime, "next-"+f[0])
	os.Remove(marker)
	line := a.Mux.PopupLine(client, action, min(w, cols), min(h, lines), p, false)
	for line != "" {
		c := exec.Command("/bin/sh", "-c", line)
		c.Env = os.Environ()
		c.Run()
		b, _ := os.ReadFile(marker)
		os.Remove(marker)
		line = strings.TrimSpace(string(b))
	}
	return nil
}

// writeJSON stores runtime state: never synced, it lives in the runtime directory.
func writeJSON(path string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return platform.ReplaceFile(path, b)
}
