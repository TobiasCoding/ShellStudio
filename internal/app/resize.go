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

	"shellstudio/internal/platform"
)

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

// Detached runs this executable with arguments, detached from the caller.
func (a *App) Detached(args ...string) error {
	return startDetached(append([]string{a.Binary}, args...))
}

type resizeState struct {
	Pane     string `json:"pane"`
	Geometry []int  `json:"geometry"`
	Window   string `json:"window"`
	Started  int64  `json:"started"`
	Axis     string `json:"axis"`
}

func (a *App) geometry(p string) (string, []int, bool) {
	o, e := a.Mux.Run(true, "display-message", "-p", "-t", p, "#{window_id}\t#{pane_left}\t#{pane_top}\t#{pane_width}\t#{pane_height}")
	if e != nil {
		return "", nil, false
	}
	f := strings.Split(strings.TrimSpace(o), "\t")
	if len(f) != 5 {
		return "", nil, false
	}
	nums := make([]int, 4)
	for i := range nums {
		nums[i], _ = strconv.Atoi(f[i+1])
	}
	return f[0], nums, true
}

// Resize applies a border drag on release: one process on press, another on
// the first movement and another on release; the rest stays inside tmux, so
// consoles keep their geometry during the drag. The state file belongs to one
// client, so two connections never overwrite each other.
func (a *App) Resize(action, client, clientID, p, x, y, left, top string) {
	if client == "" {
		return
	}
	path := filepath.Join(a.Paths.Runtime, "resize-"+token(client)+".json")
	option := ""
	if _, e := strconv.Atoi(clientID); e == nil {
		option = "@ss_resize_point_" + clientID
	}
	switch action {
	case "cancel":
		os.Remove(path)
		if option != "" {
			a.Mux.Run(true, "set-option", "-gu", option)
		}
		return
	case "start":
		if option != "" {
			a.Mux.Run(true, "set-option", "-gu", option)
		}
		w, g, ok := a.geometry(p)
		if !ok {
			return
		}
		writeJSON(path, resizeState{Pane: p, Geometry: g, Window: w, Started: time.Now().UnixNano()})
		return
	}
	defer func() {
		if action == "end" {
			os.Remove(path)
			if option != "" {
				a.Mux.Run(true, "set-option", "-gu", option)
			}
		}
	}()
	b, e := os.ReadFile(path)
	if e != nil {
		return
	}
	var st resizeState
	if json.Unmarshal(b, &st) != nil || len(st.Geometry) != 4 || time.Since(time.Unix(0, st.Started)) > 30*time.Second {
		return
	}
	src := st.Geometry
	abs := func(x, y, left, top string) (int, int, bool) {
		xi, e1 := strconv.Atoi(x)
		yi, e2 := strconv.Atoi(y)
		li, e3 := strconv.Atoi(left)
		ti, e4 := strconv.Atoi(top)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || x == "" || y == "" {
			return 0, 0, false
		}
		return li + xi, ti + yi, true
	}
	axisOf := func(ax, ay int) string {
		right, bottom := src[0]+src[2], src[1]+src[3]
		if absInt(ax-right) <= absInt(ay-bottom) {
			return "x"
		}
		return "y"
	}
	if action == "orient" {
		// The first movement stays near the original border. MouseDown1Border
		// does not expose mouse_x/mouse_y in tmux 3.4.
		ax, ay, ok := abs(x, y, left, top)
		if !ok {
			return
		}
		st.Axis = axisOf(ax, ay)
		writeJSON(path, st)
		return
	}
	if action != "end" {
		return
	}
	ax, ay, ok := abs(x, y, left, top)
	if !ok && option != "" {
		o, _ := a.Mux.Run(true, "show-option", "-gqv", option)
		if pt := strings.Split(strings.TrimSpace(o), ","); len(pt) == 4 {
			ax, ay, ok = abs(pt[2], pt[3], pt[0], pt[1])
		}
	}
	if !ok {
		return
	}
	// A re-layout or external resize during the gesture invalidates the origin.
	w, cur, ok := a.geometry(st.Pane)
	if !ok || w != st.Window || cur[0] != src[0] || cur[1] != src[1] || cur[2] != src[2] || cur[3] != src[3] {
		return
	}
	axis := st.Axis
	if axis != "x" && axis != "y" {
		// Crossing a border, tmux can omit both coordinates in the first
		// MouseDrag1Border. The release over a pane does deliver them.
		axis = axisOf(ax, ay)
	}
	wanted, actual := ax-src[0], src[2]
	if axis == "y" {
		wanted, actual = ay-src[1], src[3]
	}
	if wanted != actual && wanted > 0 {
		a.Mux.Run(true, "resize-pane", "-t", st.Pane, "-"+axis, strconv.Itoa(wanted)) // tmux applies its minimums.
	}
}
func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
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
			if _, e := a.Mux.Run(true, "resize-window", "-t", "="+row[1]+":", "-x", row[2], "-y", row[3]); e == nil {
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
		_, e = a.Mux.Run(true, "resize-window", "-t", "="+st.Session+":", "-x", strconv.Itoa(st.Width), "-y", strconv.Itoa(st.Height))
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
	out, e := a.Mux.Run(true, "list-panes", "-t", "="+session+":", "-F", "#{@ss_kind}\t#{pane_id}\t#{pane_width}")
	if e != nil {
		return
	}
	present := false
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 3 || f[0] != "explorer" {
			continue
		}
		present = true
		paneWidth, _ := strconv.Atoi(f[2])
		// tmux can shrink the leftmost pane to two columns when a detached
		// window is attached to a smaller client. Keep its controls readable.
		if v.Explorer && width >= 70 && paneWidth < min(20, width/4) {
			a.Mux.Run(true, "resize-pane", "-t", f[1], "-x", strconv.Itoa(min(32, width/4)))
		}
	}
	if present != (v.Explorer && width >= 70) {
		if e := a.Sync(view); e != nil {
			a.RecordError(e, "Resize explorer")
		}
	}
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
