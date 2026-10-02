// SPDX-License-Identifier: GPL-3.0-only
package tui

import (
	"container/heap"
	"encoding/json"
	"io"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"shellstudio/internal/app"
)

type row struct {
	rel   string
	depth int
	dir   bool
}

type treeState struct {
	Expanded []string `json:"expanded"`
	Selected string   `json:"selected"`
	Offset   int      `json:"offset"`
}

// treeRows reads only the open folders and never follows links: a symlinked
// folder is listed as an entry, not expanded.
func treeRows(root string, expanded map[string]bool) []row {
	var rows []row
	type item struct {
		rel   string
		depth int
		dir   bool
	}
	pending := []item{{".", 0, true}}
	for len(pending) > 0 {
		it := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		rows = append(rows, row{it.rel, it.depth, it.dir})
		if !it.dir || !expanded[it.rel] {
			continue
		}
		dir := root
		if it.rel != "." {
			dir = filepath.Join(root, it.rel)
		}
		entries, e := os.ReadDir(dir)
		if e != nil {
			continue
		}
		type child struct {
			name string
			dir  bool
		}
		var children []child
		for _, en := range entries {
			children = append(children, child{en.Name(), en.IsDir()})
		}
		sort.SliceStable(children, func(i, j int) bool {
			if children[i].dir != children[j].dir {
				return children[i].dir
			}
			a, b := strings.ToLower(children[i].name), strings.ToLower(children[j].name)
			if a != b {
				return a < b
			}
			return children[i].name < children[j].name
		})
		prefix := ""
		if it.rel != "." {
			prefix = it.rel + "/"
		}
		for i := len(children) - 1; i >= 0; i-- {
			pending = append(pending, item{prefix + children[i].name, it.depth + 1, children[i].dir})
		}
	}
	return rows
}

// The search does not enter noisy or huge folders; it does list them.
var searchSkip = map[string]bool{".git": true, "node_modules": true, "__pycache__": true, ".venv": true,
	"venv": true, ".mypy_cache": true, ".pytest_cache": true, ".dev-sessions": true, ".work": true}

const searchLimit = 500

type indexed struct {
	rel, folded string
	dir         bool
}

func fileIndex(root string) []indexed {
	var found []indexed
	pending := []string{""}
	for len(pending) > 0 && len(found) < 400000 {
		rel := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		entries, e := os.ReadDir(filepath.Join(root, rel))
		if e != nil {
			continue
		}
		for _, en := range entries {
			child := en.Name()
			if rel != "" {
				child = rel + "/" + en.Name()
			}
			found = append(found, indexed{child, strings.ToLower(child), en.IsDir()})
			if en.IsDir() && !searchSkip[en.Name()] {
				pending = append(pending, child)
			}
		}
	}
	return found
}

type hit struct {
	notInName bool
	slashes   int
	length    int
	rel       string
	dir       bool
}
type hits []hit

func (h hits) Len() int { return len(h) }
func (h hits) Less(i, j int) bool {
	// A max-heap of the worst kept hit.
	return !lessHit(h[i], h[j])
}
func (h hits) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *hits) Push(x any)   { *h = append(*h, x.(hit)) }
func (h *hits) Pop() any     { o := *h; x := o[len(o)-1]; *h = o[:len(o)-1]; return x }
func lessHit(a, b hit) bool {
	if a.notInName != b.notInName {
		return !a.notInName
	}
	if a.slashes != b.slashes {
		return a.slashes < b.slashes
	}
	if a.length != b.length {
		return a.length < b.length
	}
	return a.rel < b.rel
}

// searchPaths: every word must appear in the path; those in the name first.
func searchPaths(catalog []indexed, query string) []row {
	terms := strings.Fields(strings.ToLower(query))
	h := &hits{}
	for _, it := range catalog {
		ok := true
		for _, t := range terms {
			if !strings.Contains(it.folded, t) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		name := it.folded[strings.LastIndex(it.folded, "/")+1:]
		inName := true
		for _, t := range terms {
			inName = inName && strings.Contains(name, t)
		}
		x := hit{!inName, strings.Count(it.folded, "/"), len(it.folded), it.rel, it.dir}
		if h.Len() < searchLimit {
			heap.Push(h, x)
		} else if lessHit(x, (*h)[0]) {
			(*h)[0] = x
			heap.Fix(h, 0)
		}
	}
	list := []hit(*h)
	sort.Slice(list, func(i, j int) bool { return lessHit(list[i], list[j]) })
	out := make([]row, len(list))
	for i, x := range list {
		out[i] = row{x.rel, 0, x.dir}
	}
	return out
}

// preview shows a regular file inside the view folder, up to 128 KiB.
func preview(s *Screen, root, rel string) {
	var lines []string
	p, e := app.Resolve(root, rel)
	if e == nil {
		var f *os.File
		f, e = os.OpenFile(p, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
		if e == nil {
			fi, _ := f.Stat()
			if fi == nil || !fi.Mode().IsRegular() {
				lines = []string{"Not a regular file."}
			} else {
				raw, _ := io.ReadAll(io.LimitReader(f, 128*1024+1))
				if strings.ContainsRune(string(raw), 0) {
					lines = []string{"Binary file."}
				} else {
					text := string(raw[:min(len(raw), 128*1024)])
					lines = strings.Split(strings.ReplaceAll(strings.ToValidUTF8(text, "�"), "\r\n", "\n"), "\n")
					if len(raw) > 128*1024 {
						lines = append(lines, "[Preview limited to 128 KiB]")
					}
				}
			}
			f.Close()
		}
	}
	if e != nil {
		lines = []string{e.Error()}
	}
	top := 0
	for {
		count := max(1, s.H-4)
		s.Erase()
		s.Draw(0, path.Base(rel), Bold)
		for i := top; i < len(lines) && i < top+count; i++ {
			s.Draw(2+i-top, strings.ReplaceAll(lines[i], "\t", "    "), Normal)
		}
		s.Draw(s.H-1, "Esc: tree | ↑↓ read", Normal)
		s.Refresh()
		ev := s.Read(-1)
		if ev.Kind == MouseEvent {
			switch ev.Button {
			case 64:
				ev = Event{Kind: KeyEvent, Key: "up"}
			case 65:
				ev = Event{Kind: KeyEvent, Key: "down"}
			}
		}
		switch ev.Key {
		case "esc", "q", "enter":
			return
		case "down":
			top = min(max(0, len(lines)-count), top+1)
		case "pgdown":
			top = min(max(0, len(lines)-count), top+count)
		case "up":
			top = max(0, top-1)
		case "pgup":
			top = max(0, top-count)
		}
	}
}

var treeShortcuts = []struct{ label, action string }{{"F2 Menu", "menu"}, {"F3 New", "new"},
	{"F5 Arrange", "arrange"}, {"F9 Kill", "kill"}, {"F7 Views", "views"}, {"F10 Leave", "leave"}}

// Explorer is the file tree on the left of every view.
func Explorer(a *app.App, view string) error {
	v, e := a.Store.View(view)
	if e != nil {
		return e
	}
	root := v.Folder
	s, e := Open(true)
	if e != nil {
		return e
	}
	var st treeState
	json.Unmarshal([]byte(v.Tree), &st)
	expanded := map[string]bool{".": true}
	if st.Expanded != nil {
		expanded = map[string]bool{}
		for _, x := range st.Expanded {
			expanded[x] = true
		}
	}
	selected := st.Selected
	if selected == "" {
		selected = "."
	}
	offset := max(0, st.Offset)
	rows := treeRows(root, expanded)
	scanned := time.Now()
	pane := os.Getenv("TMUX_PANE")
	message := ""
	// Search: nil is the tree; a string (even empty) is the active search.
	var query *string
	var catalog []indexed
	var indexedAt time.Time
	treeSelected := selected
	// Double click is detected here: tmux passes both clicks to the program.
	lastPress, lastAt := "", time.Time{}
	swallowRelease := false
	sortedExpanded := func() []string {
		var out []string
		for k := range expanded {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	saved := ""
	var saving, failed atomic.Bool
	var lastSave chan struct{}
	persist := func() {
		// Saving when idle, not on every key: walking the tree with the arrows
		// does not rewrite the configuration dozens of times per second.
		sel := selected
		if query != nil {
			sel = treeSelected
		}
		b, _ := json.Marshal(treeState{sortedExpanded(), sel, offset})
		if string(b) == saved || !saving.CompareAndSwap(false, true) {
			return
		}
		saved = string(b)
		// A committed transaction can take a second on slow disks: the tree
		// keeps answering while it is written.
		done := make(chan struct{})
		go func() {
			if a.Store.SetTree(view, string(b)) != nil {
				failed.Store(true)
			}
			saving.Store(false)
			close(done)
		}()
		lastSave = done
	}
	flush := func() {
		if lastSave != nil {
			<-lastSave
		}
		if failed.Swap(false) {
			saved = ""
		}
		persist()
		if lastSave != nil {
			<-lastSave
		}
	}
	saved = func() string { b, _ := json.Marshal(treeState{sortedExpanded(), selected, offset}); return string(b) }()
	// Closing the pane (kill-pane) sends SIGHUP: save before leaving.
	stop := make(chan os.Signal, 2)
	signal.Notify(stop, syscall.SIGHUP, syscall.SIGTERM)
	go func() {
		<-stop
		flush()
		s.Close()
		os.Exit(0)
	}()
	defer func() { flush(); s.Close() }()
	startSearch := func() {
		treeSelected = selected
		if catalog == nil || time.Since(indexedAt) > 30*time.Second {
			s.Draw(1, "Indexing…", Highlight)
			s.Refresh()
			catalog, indexedAt = fileIndex(root), time.Now()
		}
		q := ""
		query = &q
	}
	reveal := func(target string) {
		// Leaving the search shows the result in the tree.
		parts := strings.Split(target, "/")
		expanded["."] = true
		for i := 1; i < len(parts); i++ {
			expanded[strings.Join(parts[:i], "/")] = true
		}
		query, selected = nil, target
		rows, scanned = treeRows(root, expanded), time.Now()
	}
	edit := func(rel string, dir bool) string {
		if dir {
			return "It is a folder."
		}
		if _, e := a.OpenEditor(view, rel); e != nil {
			a.RecordError(e, "Explorer: edit file")
			return e.Error()
		}
		return "In nano; leaving it closes the console."
	}
	shortcut := func(action string) error {
		client := ""
		if pane != "" {
			client = a.ExplorerClient(pane, view)
		}
		if client == "" {
			return errorf("the view client could not be identified")
		}
		if action == "leave" {
			_, e := a.Mux.Run(true, "detach-client", "-t", client)
			return e
		}
		o, e := a.Mux.Run(true, "list-panes", "-t", pane, "-F", "#{pane_id}\t#{@ss_kind}\t#{pane_last}")
		if e != nil {
			return e
		}
		target := pane
		for _, l := range strings.Split(strings.TrimSpace(o), "\n") {
			f := strings.Split(l, "\t")
			if len(f) == 3 && f[1] == "console" && (target == pane || f[2] == "1") {
				target = f[0]
			}
		}
		return a.OpenDialog(action, client, target)
	}
	for {
		item := a.CurrentError()
		var errorLines []string
		if item != nil {
			errorLines = Wrap(item.Context+": "+item.Message, max(8, s.W-3))
			if len(errorLines) == 0 {
				errorLines = []string{"Error"}
			}
			if len(errorLines) > 5 {
				errorLines = errorLines[:5]
				r := []rune(errorLines[4])
				errorLines[4] = string(r[:max(0, len(r)-1)]) + "…"
			}
		}
		shortcutsTop := s.H - len(treeShortcuts)
		statusRows, errorRows := 0, 0
		if message != "" {
			statusRows = 1
		}
		if item != nil {
			errorRows = len(errorLines) + 4
		}
		errorTop := shortcutsTop - statusRows - errorRows
		errorButtonY := errorTop + 2 + len(errorLines)
		count := max(1, errorTop-2)
		index := 0
		for i, r := range rows {
			if r.rel == selected {
				index = i
			}
		}
		if len(rows) > 0 {
			selected = rows[index].rel
		}
		offset = min(offset, max(0, len(rows)-count))
		offset = max(0, min(offset, index))
		if index >= offset+count {
			offset = index - count + 1
		}
		s.Erase()
		s.Header(" FILES ▾ ")
		if query == nil {
			s.Draw(1, "/ Search", Dim)
		} else {
			s.Draw(1, "/ "+*query+"_", Highlight)
		}
		for i := offset; i < len(rows) && i < offset+count; i++ {
			r := rows[i]
			var label string
			if query != nil && *query != "" {
				parent, leaf := path.Split(r.rel)
				label = leaf
				if r.dir {
					label += "/"
				}
				if parent != "" {
					label += "  " + strings.TrimSuffix(parent, "/")
				}
			} else {
				marker := "  "
				if r.dir {
					marker = "▸ "
					if expanded[r.rel] {
						marker = "▾ "
					}
				}
				leaf := path.Base(r.rel)
				if r.rel == "." {
					leaf = filepath.Base(root)
				}
				label = strings.Repeat("  ", r.depth) + marker + leaf
			}
			style := Normal
			if i == index {
				style = Highlight
			}
			s.Draw(i-offset+2, label, style)
		}
		if query != nil && *query != "" && len(rows) == 0 {
			s.Draw(2, "No results.", Normal)
		}
		if item != nil {
			sep := strings.Repeat("─", max(0, s.W-2))
			s.Draw(errorTop, sep, Dim)
			s.Draw(errorTop+1, "APP ERRORS", Bold)
			for i, l := range errorLines {
				s.Draw(errorTop+2+i, l, Normal)
			}
			s.Draw(errorButtonY, "[X] Close  [F] Fix this", Highlight)
			s.Draw(errorButtonY+1, sep, Dim)
		}
		if message != "" {
			s.Draw(shortcutsTop-1, message, Normal)
		}
		for i, sc := range treeShortcuts {
			s.Draw(shortcutsTop+i, sc.label, Dim)
		}
		s.Refresh()
		ev := s.Read(time.Second)
		// A notice lasts until the next key: the release of the click that
		// caused it does not erase it.
		if ev.Kind == KeyEvent || ev.Kind == PasteEvent {
			message = ""
		}
		if ev.Kind == TimeoutEvent || ev.Kind == ResizeEvent {
			persist()
			// New files appear by themselves, without reading the disk on every key.
			if query == nil && time.Since(scanned) >= 5*time.Second {
				rows, scanned = treeRows(root, expanded), time.Now()
			}
			continue
		}
		press, release := ev.Left()
		if (press || release) && ev.X >= 1 && ev.X < s.W-1 && ev.Y >= shortcutsTop && ev.Y < s.H {
			if release {
				if e := shortcut(treeShortcuts[ev.Y-shortcutsTop].action); e != nil {
					a.RecordError(e, "Explorer shortcut")
					message = e.Error()
				}
			}
			continue
		}
		if item != nil && query == nil {
			button := ""
			switch {
			case ev.Is("x", "X"):
				button = "close"
			case ev.Is("f", "F"):
				button = "agent"
			case release && ev.Y == errorButtonY:
				if ev.X >= 1 && ev.X < 12 {
					button = "close"
				} else if ev.X >= 13 && ev.X < s.W-1 {
					button = "agent"
				}
			}
			if button != "" {
				if button == "close" {
					a.DismissError(item.ID)
				} else if c, e := a.SendErrorToAgent(*item, view); e != nil {
					a.RecordError(e, "Error panel")
					message = e.Error()
				} else {
					a.FocusConsole(view, c.ID)
					message = "Sent to " + c.Name + "."
				}
				continue
			}
		}
		if query == nil {
			if source := windowsDrop(s, ev); source != "" {
				var chosen *row
				for i := range rows {
					if rows[i].rel == selected {
						chosen = &rows[i]
					}
				}
				switch {
				case chosen == nil || !chosen.dir:
					message = "First click the destination folder."
				case pane == "":
					message = "The tree panel could not be identified."
				default:
					client := a.ExplorerClient(pane, view)
					if client == "" {
						message = "Your SSH connection could not be identified."
						break
					}
					persist()
					if e := a.OpenUploadDialog(source, view, selected, client); e != nil {
						a.RecordError(e, "Explorer: upload file")
						message = e.Error()
					} else {
						message = "SCP command prepared for that folder."
					}
				}
				continue
			}
		}
		// With the search open, letters are the search.
		if query != nil && ev.Kind == KeyEvent {
			k := ev.Key
			r := []rune(k)
			if k == "backspace" || (len(r) == 1 && r[0] >= ' ') {
				q := *query
				if k == "backspace" {
					if qr := []rune(q); len(qr) > 0 {
						q = string(qr[:len(qr)-1])
					}
				} else {
					q += k
				}
				query = &q
				if q != "" {
					rows = searchPaths(catalog, q)
					selected, offset = "", 0
					if len(rows) > 0 {
						selected = rows[0].rel
					}
				} else {
					rows, selected = treeRows(root, expanded), treeSelected
				}
				continue
			}
		}
		if query != nil && ev.Kind == PasteEvent {
			q := *query + strings.ReplaceAll(ev.Text, "\n", " ")
			query = &q
			rows = searchPaths(catalog, q)
			continue
		}
		key := ev.Key
		if ev.Kind == KeyEvent && key == "esc" && query != nil {
			query, selected = nil, treeSelected
			rows, scanned = treeRows(root, expanded), time.Now()
			continue
		}
		if ev.Kind == MouseEvent {
			rowAt := -1
			if ev.Y >= 2 && ev.Y < 2+min(count, len(rows)-offset) {
				rowAt = offset + ev.Y - 2
			}
			switch {
			case ev.Button == 64 && ev.Press:
				key = "up"
			case ev.Button == 65 && ev.Press:
				key = "down"
			case ev.Y == 1 && press && query == nil:
				startSearch()
				continue
			case press && rowAt >= 0:
				// Pressing only selects and leaves the path ready to drag; a
				// folder opens on release over it, so it can be dragged too.
				index, selected = rowAt, rows[rowAt].rel
				if pane != "" {
					a.Mux.Run(true, "set-option", "-p", "-t", pane, "@ss_path", selected)
				}
				// In the tree every click on a folder must open or fold it, even
				// right after the previous one. Double click only edits files or
				// reveals results.
				double := (query != nil || !rows[rowAt].dir) && lastPress == selected && time.Since(lastAt) < 400*time.Millisecond
				if double {
					lastPress, lastAt = "", time.Time{}
					// The release of this second click does not fold the folder again.
					swallowRelease = true
					if query != nil && rows[rowAt].dir {
						reveal(selected)
					} else {
						message = edit(selected, rows[rowAt].dir)
					}
				} else {
					lastPress, lastAt = selected, time.Now()
				}
				continue
			case release && rowAt >= 0:
				if swallowRelease {
					swallowRelease = false
					continue
				}
				if rows[rowAt].rel != selected {
					continue
				}
				index, selected = rowAt, rows[rowAt].rel
				if rows[index].dir && query == nil {
					key = " "
				} else {
					continue
				}
			default:
				continue
			}
		} else if ev.Kind != KeyEvent {
			continue
		}
		if key == "/" && query == nil {
			startSearch()
			continue
		}
		if len(rows) == 0 {
			continue
		}
		index = max(0, min(index, len(rows)-1))
		r := rows[index]
		before := len(expanded)
		rescan := (key == "r" || key == "R") && query == nil
		switch key {
		case "down", "up", "pgdown", "pgup", "home", "end":
			delta := map[string]int{"down": 1, "up": -1, "pgdown": count, "pgup": -count, "home": -len(rows), "end": len(rows)}[key]
			selected = rows[max(0, min(len(rows)-1, index+delta))].rel
		default:
			if query != nil {
				if key == "enter" {
					reveal(r.rel)
				}
				continue
			}
			switch key {
			case "left":
				if r.dir && expanded[r.rel] && r.rel != "." {
					delete(expanded, r.rel)
				} else if r.rel != "." {
					selected = path.Dir(r.rel)
				}
			case "right":
				if r.dir {
					if expanded[r.rel] && index+1 < len(rows) && rows[index+1].depth > r.depth {
						selected = rows[index+1].rel
					} else {
						expanded[r.rel] = true
					}
				}
			case "enter", " ":
				if r.dir {
					if expanded[r.rel] {
						delete(expanded, r.rel)
					} else {
						expanded[r.rel] = true
					}
				} else if key == "enter" {
					preview(s, root, r.rel)
					rescan = true
				}
			case "e", "E":
				message = edit(r.rel, r.dir)
			case "n", "N":
				leaf := path.Base(r.rel)
				name, ok := Prompt(s, "Rename", r.rel, leaf, 255, "")
				if ok && name != "" && name != leaf {
					renamed, e := app.RenamePath(root, r.rel, name)
					if e != nil {
						a.RecordError(e, "Explorer: rename")
						message = e.Error()
						break
					}
					// Open folders inside the renamed one stay open.
					moved := map[string]bool{}
					for x := range expanded {
						if x == r.rel || strings.HasPrefix(x, r.rel+"/") {
							moved[renamed+x[len(r.rel):]] = true
						} else {
							moved[x] = true
						}
					}
					expanded, selected, rescan, catalog = moved, renamed, true, nil
					message = "Renamed."
				}
			case "i", "I", "insert":
				if pane != "" {
					done, e := a.InsertIntoLast(pane, view, r.rel)
					switch {
					case e != nil:
						a.RecordError(e, "Explorer: insert path")
						message = e.Error()
					case done:
						message = "Path written."
					default:
						message = "No console in the view."
					}
				}
			}
		}
		// Moving does not read the disk; opening or closing a folder does.
		if rescan || len(expanded) != before {
			rows, scanned = treeRows(root, expanded), time.Now()
		}
		index = 0
		for i, x := range rows {
			if x.rel == selected {
				index = i
			}
		}
		if len(rows) > 0 {
			selected = rows[index].rel
		}
		offset = max(0, min(offset, index))
		if index >= offset+count {
			offset = index - count + 1
		}
	}
}

type simpleError string

func (e simpleError) Error() string { return string(e) }
func errorf(s string) error         { return simpleError(s) }

// windowsDrop reads the bracketed paste that Windows Terminal sends when a
// file is dropped, or a path typed by an old terminal. The paste does not
// carry the drop position: only the folder chosen before dragging is used.
func windowsDrop(s *Screen, ev Event) string {
	if ev.Kind == PasteEvent {
		return app.WindowsDropPath(ev.Text)
	}
	if ev.Kind != KeyEvent {
		return ""
	}
	first := []rune(ev.Key)
	if len(first) != 1 || !(first[0] == '"' || first[0] == '\\' || (first[0] < 128 && (first[0]|0x20) >= 'a' && (first[0]|0x20) <= 'z')) {
		return ""
	}
	second := s.Read(100 * time.Millisecond)
	sr := []rune(second.Key)
	if second.Kind != KeyEvent || len(sr) != 1 || (first[0] != '"' && first[0] != '\\' && sr[0] != ':') || (first[0] == '\\' && sr[0] != '\\') {
		if second.Kind != TimeoutEvent {
			s.Unget(second)
		}
		return ""
	}
	content := string(first) + string(sr)
	for len(content) < 4096 {
		next := s.Read(100 * time.Millisecond)
		r := []rune(next.Key)
		if next.Kind != KeyEvent || len(r) != 1 {
			if next.Kind != TimeoutEvent {
				s.Unget(next)
			}
			break
		}
		content += next.Key
	}
	if p := app.WindowsDropPath(content); p != "" {
		return p
	}
	// Not a path: replay what was typed as ordinary keys.
	for _, r := range []rune(content)[1:] {
		s.pending = append(s.pending, Event{Kind: KeyEvent, Key: string(r)})
	}
	return ""
}
