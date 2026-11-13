// SPDX-License-Identifier: GPL-3.0-only
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
	"shellstudio/internal/app"
	"shellstudio/internal/i18n"
	"shellstudio/internal/mux"
	"shellstudio/internal/platform"
	"shellstudio/internal/store"
	"shellstudio/internal/theme"
)

// Columns of the Minimized table. created, console and tag are shown by
// default; the others are chosen in Columns ▼.
var minColumns = []string{"created", "console", "tag", "folder", "status", "minimized_at"}

var minDefaultColumns = []string{"created", "console", "tag"}

// minPrefs are the panel preferences saved in config.json. The search is not.
type minPrefs struct {
	Columns []string
	Sort    string // column
	Desc    bool
	Filter  string // tag ID; "" is every tag
}

func loadMinPrefs(dir string) minPrefs {
	c, _ := platform.LoadConfig(dir)
	p := minPrefs{Columns: c.MinimizedColumns, Sort: "created", Desc: true, Filter: c.MinimizedFilter}
	if len(p.Columns) == 0 {
		p.Columns = minDefaultColumns
	}
	if col, dir, ok := strings.Cut(c.MinimizedSort, ":"); ok && validMinColumn(col) {
		p.Sort, p.Desc = col, dir == "desc"
	}
	return p
}

func validMinColumn(c string) bool {
	for _, x := range minColumns {
		if x == c {
			return true
		}
	}
	return false
}

// UpdateMinPrefs applies one change from the panel's menus: col:NAME
// toggles a column (console stays), sort:NAME sorts by it (again: reverses)
// and filter:TAG filters by a tag (filter: shows all).
func UpdateMinPrefs(dir, change string) error {
	kind, value, _ := strings.Cut(change, ":")
	return platform.UpdateConfig(dir, func(c *platform.Config) error {
		p := minPrefs{Columns: c.MinimizedColumns, Sort: "created", Desc: true}
		if len(p.Columns) == 0 {
			p.Columns = append([]string{}, minDefaultColumns...)
		}
		if col, d, ok := strings.Cut(c.MinimizedSort, ":"); ok && validMinColumn(col) {
			p.Sort, p.Desc = col, d == "desc"
		}
		switch kind {
		case "col":
			if !validMinColumn(value) || value == "console" {
				return nil
			}
			var out []string
			found := false
			for _, x := range p.Columns {
				if x == value {
					found = true
				} else {
					out = append(out, x)
				}
			}
			if !found {
				// Keep the canonical order.
				out = nil
				for _, x := range minColumns {
					if x == value || contains(p.Columns, x) {
						out = append(out, x)
					}
				}
			}
			c.MinimizedColumns = out
		case "sort":
			if !validMinColumn(value) {
				return nil
			}
			if p.Sort == value {
				p.Desc = !p.Desc
			} else {
				p.Sort, p.Desc = value, value == "created" || value == "minimized_at"
			}
			d := "asc"
			if p.Desc {
				d = "desc"
			}
			c.MinimizedSort = p.Sort + ":" + d
		case "filter":
			c.MinimizedFilter = value
		}
		return nil
	})
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// minRow is a minimized console with what the table shows.
type minRow struct {
	store.Console
	Label  string // Kind · Name
	Tag    store.Tag
	Status string
}

// minimizedRows filters by tag and search terms (every term must appear in
// the name, kind, description, tag or folder) and sorts.
func minimizedRows(rows []minRow, query, filter, by string, desc bool) []minRow {
	terms := strings.Fields(strings.ToLower(query))
	var out []minRow
	for _, r := range rows {
		if filter != "" && r.TagID != filter {
			continue
		}
		hay := strings.ToLower(strings.Join([]string{r.Label, r.Description, r.Tag.Name, r.Folder}, "\x00"))
		ok := true
		for _, t := range terms {
			if !strings.Contains(hay, t) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, r)
		}
	}
	less := func(a, b minRow) bool {
		switch by {
		case "console":
			return strings.ToLower(a.Label) < strings.ToLower(b.Label)
		case "tag":
			if a.Tag.ID == "" || b.Tag.ID == "" {
				return a.Tag.ID != "" // Tagged first.
			}
			return a.Tag.Position < b.Tag.Position
		case "folder":
			return a.Folder < b.Folder
		case "status":
			return a.Status < b.Status
		case "minimized_at":
			return a.MinimizedAt.Before(b.MinimizedAt)
		}
		return a.Created.Before(b.Created)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if desc {
			return less(out[j], out[i])
		}
		return less(out[i], out[j])
	})
	return out
}

// minState is the panel between frames.
type minState struct {
	view, pane, client string
	search             Field
	prefs              minPrefs
	rows               []minRow // shown, filtered and sorted
	total              int      // minimized consoles before filtering
	tags               []store.Tag
	selected           string // console ID
	offset             int    // first row shown
	message            string
	hits               HitMap
	lastClick          time.Time
	lastClickRow       string
	listRows           int // table rows that fit (two lines each)
}

func (st *minState) index() int {
	for i, r := range st.rows {
		if r.ID == st.selected {
			return i
		}
	}
	return -1
}

func (st *minState) move(delta int) {
	if len(st.rows) == 0 {
		return
	}
	i := max(0, min(len(st.rows)-1, st.index()+delta))
	st.selected = st.rows[i].ID
}

// load reads the minimized consoles, their tags and status.
func (st *minState) load(a *app.App) {
	cs, _ := a.Store.Minimized(st.view)
	tags, _ := a.Store.Tags()
	st.tags = tags
	byID := map[string]store.Tag{}
	for _, t := range tags {
		byID[t.ID] = t
	}
	labels := a.KindLabelsCached()
	programs, _ := a.Mux.Programs()
	rows := make([]minRow, len(cs))
	for i, c := range cs {
		rows[i] = minRow{Console: c, Label: app.ConsoleLabel(labels, c), Tag: byID[c.TagID], Status: app.Status(programs, c.ID)}
	}
	st.total = len(rows)
	if st.prefs.Filter != "" {
		if _, ok := byID[st.prefs.Filter]; !ok {
			st.prefs.Filter = "" // A deleted tag filters nothing.
		}
	}
	st.rows = minimizedRows(rows, st.search.Text(), st.prefs.Filter, st.prefs.Sort, st.prefs.Desc)
	if st.index() < 0 && len(st.rows) > 0 {
		st.selected = st.rows[0].ID
	}
}

func statusText(status string) (string, Style) {
	switch status {
	case "running":
		return i18n.T("status.running"), Style{Fg: theme.Green}
	case "finished":
		return i18n.T("status.finished"), Style{Fg: theme.Amber}
	}
	return i18n.T("status.stopped"), Style{Fg: theme.Muted}
}

func pad(s string, w int) string {
	s = runewidth.Truncate(s, max(0, w), "…")
	return s + strings.Repeat(" ", max(0, w-runewidth.StringWidth(s)))
}

// minLayout is the x and width of each visible column.
type minCol struct {
	name string
	x, w int
}

func (st *minState) layout(width int) []minCol {
	right := width - 2 // The last column but one holds the scrollbar.
	// Date columns fit "dd/mm" and their title with the sort arrow.
	dateW := func(c string) int { return max(5, runewidth.StringWidth(colTitle(c))+2) }
	fixed := map[string]int{"created": dateW("created"), "tag": st.tagWidth(width), "folder": 12, "status": 13, "minimized_at": dateW("minimized_at")}
	var cols []minCol
	used := 1
	for _, c := range minColumns {
		if !contains(st.prefs.Columns, c) && c != "console" {
			continue
		}
		// A narrow column keeps the console and its tag; dates go first.
		if width < 44 && (c == "created" || c == "minimized_at") {
			continue
		}
		w := fixed[c]
		cols = append(cols, minCol{name: c, w: w})
		used += w + 2
	}
	// The console column takes the rest; optional columns go first when
	// the pane is too narrow.
	for used > right-10 && len(cols) > 2 {
		drop := -1
		for i := len(cols) - 1; i >= 0; i-- {
			if cols[i].name != "console" && cols[i].name != "created" && cols[i].name != "tag" {
				drop = i
				break
			}
		}
		if drop < 0 {
			break
		}
		used -= cols[drop].w + 2
		cols = append(cols[:drop], cols[drop+1:]...)
	}
	x := 1
	for i := range cols {
		if cols[i].name == "console" {
			cols[i].w = max(6, right-used-0)
		}
		cols[i].x = x
		x += cols[i].w + 2
	}
	return cols
}

// tagWidth fits the longest tag name and its ▼, within limits.
func (st *minState) tagWidth(width int) int {
	w := runewidth.StringWidth(i18n.T("console.no_tag"))
	for _, t := range st.tags {
		w = max(w, runewidth.StringWidth(t.Name))
	}
	return max(7, min(w+4, 18, width/3))
}

func colTitle(c string) string { return i18n.T("min.col." + c) }

// draw paints one frame and records the clickable zones.
func (st *minState) draw(s *Screen) {
	s.Erase()
	st.hits.Reset()
	W, H := s.W, s.H
	// Header: MINIMIZADAS N and Columnas ▼.
	s.DrawAt(0, 0, strings.Repeat(" ", W), HeaderBar)
	title := fmt.Sprintf(" %s  %d", i18n.T("min.title"), st.total)
	s.DrawAt(0, 0, title, HeaderBar.With(AttrBold))
	cols := i18n.T("min.columns") + " " + theme.Glyphs.Menu + " "
	cx := W - 1 - runewidth.StringWidth(cols)
	if cx > runewidth.StringWidth(title)+1 {
		s.DrawAt(0, cx, cols, HeaderBar)
		st.hits.Add(0, cx, runewidth.StringWidth(cols), 1, "columns")
	}
	if H < 6 {
		return
	}
	// Search and tag filter.
	filter := i18n.T("min.filter_tag")
	filterStyle := FieldStyle
	for _, t := range st.tags {
		if t.ID == st.prefs.Filter {
			filter = t.Name
			filterStyle = Style{Fg: uint8(theme.TagText(theme.TagColor(t.Color))), Bg: uint8(theme.TagColor(t.Color))}
		}
	}
	filter = " " + runewidth.Truncate(filter, 14, "…") + " " + theme.Glyphs.Menu + " "
	fw := runewidth.StringWidth(filter)
	fieldW := max(8, W-2-fw-2)
	st.search.Placeholder = i18n.T("min.search")
	st.search.Draw(s, 1, 1, fieldW)
	st.hits.Add(1, 1, fieldW, 1, "search")
	s.DrawAt(1, 1+fieldW+1, filter, filterStyle)
	st.hits.Add(1, 1+fieldW+1, fw, 1, "filter")
	// Table header, a blank row below the search when there is room.
	headY := 2
	if H >= 14 {
		headY = 3
	}
	layout := st.layout(W)
	for _, c := range layout {
		t := colTitle(c.name)
		if c.name == st.prefs.Sort {
			if st.prefs.Desc {
				t += " " + theme.Glyphs.SortDown
			} else {
				t += " " + theme.Glyphs.SortUp
			}
		}
		s.DrawAt(headY, c.x, pad(t, c.w), Bold)
		st.hits.Add(headY, c.x, c.w, 1, "sort:"+c.name)
	}
	// Rows: two lines each, between the table header and the footer.
	top, bottom := headY+1, H-3
	st.listRows = max(1, (bottom-top)/2)
	if len(st.rows) == 0 {
		msg := i18n.T("min.empty")
		if st.total > 0 {
			msg = i18n.T("min.no_match")
		}
		s.DrawAt(top+1, 2, msg, Muted)
	}
	sel := st.index()
	if sel >= 0 {
		if sel < st.offset {
			st.offset = sel
		}
		if sel >= st.offset+st.listRows {
			st.offset = sel - st.listRows + 1
		}
	}
	st.offset = max(0, min(st.offset, len(st.rows)-st.listRows))
	for i := 0; i < st.listRows && st.offset+i < len(st.rows); i++ {
		r := st.rows[st.offset+i]
		y := top + 2*i
		base, dim := Normal, Muted
		if r.ID == st.selected {
			base, dim = Highlight, Style{Fg: theme.Text, Bg: theme.Selection}
			s.DrawAt(y, 0, strings.Repeat(" ", W-2), base)
			s.DrawAt(y+1, 0, strings.Repeat(" ", W-2), base)
		}
		st.hits.Add(y, 0, W-2, 2, "row:"+r.ID)
		for _, c := range layout {
			switch c.name {
			case "created", "minimized_at":
				t := r.Created
				if c.name == "minimized_at" {
					t = r.MinimizedAt
				}
				if !t.IsZero() {
					t = t.Local()
					s.DrawAt(y, c.x, t.Format("02/01"), base)
					s.DrawAt(y+1, c.x, t.Format("15:04"), dim)
				}
			case "console":
				s.DrawAt(y, c.x, pad(r.Label, c.w), base)
				s.DrawAt(y+1, c.x, pad(r.Description, c.w), dim)
			case "folder":
				s.DrawAt(y, c.x, pad(filepath.Base(r.Folder), c.w), dim)
			case "status":
				text, style := statusText(r.Status)
				style.Bg = base.Bg
				s.DrawAt(y, c.x, pad(theme.Glyphs.Dot+" "+text, c.w), style)
			case "tag":
				name := i18n.T("console.no_tag")
				color := theme.Border
				if r.Tag.ID != "" {
					name, color = r.Tag.Name, r.Tag.Color
				}
				bg := theme.TagColor(color)
				if r.Tag.ID == "" {
					bg = theme.HeaderBg
				}
				text := " " + pad(name, c.w-4) + " " + theme.Glyphs.Menu + " "
				st := Style{Fg: uint8(theme.TagText(bg)), Bg: uint8(bg)}
				if r.Tag.ID == "" {
					st.Fg = theme.Muted
				}
				s.DrawAt(y, c.x, text, st)
			}
		}
		if tagCol := findCol(layout, "tag"); tagCol != nil {
			st.hits.Add(y, tagCol.x, tagCol.w, 1, "tag:"+r.ID)
		}
	}
	Scrollbar(s, top, W-2, bottom-top, len(st.rows), st.listRows, st.offset)
	if len(st.rows) > st.listRows {
		st.hits.Add(top, W-2, 1, bottom-top, "scroll")
	}
	// Footer: the selected console, its status and the buttons.
	s.DrawAt(H-3, 0, strings.Repeat("─", W-1), Style{Fg: theme.Border})
	if sel >= 0 {
		r := st.rows[sel]
		text, style := statusText(r.Status)
		status := theme.Glyphs.Dot + " " + text
		sw := runewidth.StringWidth(status)
		s.DrawAt(H-2, 1, pad(r.Label, W-4-sw), Bold)
		s.DrawAt(H-2, W-2-sw, status, style)
		x := Button(s, &st.hits, H-1, 1, i18n.T("min.restore"), "restore", Cyan)
		// A narrow panel keeps Restore and ⋯; Rename is also in ⋯ and on R.
		if x+2+ButtonWidth(i18n.T("min.rename"))+2+ButtonWidth(theme.Glyphs.More) < W-1 {
			x = Button(s, &st.hits, H-1, x+2, i18n.T("min.rename"), "rename", Normal)
		}
		Button(s, &st.hits, H-1, x+2, theme.Glyphs.More, "more", Normal)
	} else if st.message == "" {
		s.DrawAt(H-2, 1, "", Normal)
	}
	if st.message != "" {
		s.DrawAt(H-2, 1, pad(st.message, W-3), Style{Fg: theme.Amber})
	}
}

func findCol(cols []minCol, name string) *minCol {
	for i := range cols {
		if cols[i].name == name {
			return &cols[i]
		}
	}
	return nil
}

// Minimized is the panel under the explorer that lists the minimized consoles
// of the view: search, tag filter, sortable columns and Restore.
func Minimized(a *app.App, view string) error {
	s, e := Open(true)
	if e != nil {
		return e
	}
	defer s.Close()
	pane := os.Getenv("TMUX_PANE")
	return runMinimized(s, a, view, pane)
}

func runMinimized(s *Screen, a *app.App, view, pane string) error {
	st := &minState{view: view, pane: pane}
	st.prefs = loadMinPrefs(a.Paths.Config)
	rev := ""
	st.load(a)
	dragging := false
	for {
		st.draw(s)
		s.Refresh()
		ev := s.Read(time.Second)
		if ev.Kind == TimeoutEvent || ev.Kind == ResizeEvent {
			// Menus change the preferences in config.json; Minimize and
			// Restore bump the revision.
			if r := a.MinimizedRev(); r != rev || ev.Kind == TimeoutEvent {
				rev = r
				st.prefs = loadMinPrefs(a.Paths.Config)
				st.load(a)
			}
			continue
		}
		st.message = ""
		if ev.Kind == MouseEvent {
			if dragging && (ev.Motion || ev.Release) {
				if z, ok := st.hits.Zone("scroll"); ok {
					st.offset = ScrollOffset(ev.Y-z.Y, z.H, len(st.rows), st.listRows)
					if st.index() < st.offset || st.index() >= st.offset+st.listRows {
						st.selected = st.rows[st.offset].ID
					}
				}
				if ev.Release {
					dragging = false
					s.TrackDrag(false)
				}
				continue
			}
			if ev.Button == 64 || ev.Button == 65 {
				// The wheel scrolls the selection.
				if ev.Button == 64 {
					st.move(-1)
				} else {
					st.move(1)
				}
				continue
			}
			press, _ := ev.Left()
			if !press {
				continue
			}
			st.click(s, a, ev)
			if st.hits.At(ev.Y, ev.X) == "scroll" {
				dragging = true
				s.TrackDrag(true)
			}
			continue
		}
		if c, changed := st.search.Handle(ev); c {
			if changed {
				st.load(a)
			}
			continue
		}
		switch ev.Key {
		case "up":
			st.move(-1)
		case "down":
			st.move(1)
		case "pgup":
			st.move(-st.listRows)
		case "pgdown":
			st.move(st.listRows)
		case "home":
			st.move(-len(st.rows))
		case "end":
			st.move(len(st.rows))
		case "/":
			st.search.Focused = true
		case "esc":
			if st.search.Focused && st.search.Text() != "" {
				st.search.Set("")
				st.load(a)
			}
			st.search.Focused = false
		case "tab":
			st.search.Focused = !st.search.Focused
		case "enter":
			st.act(s, a, "restore")
		case "f2", "r", "R":
			st.act(s, a, "rename")
		case "t", "T":
			st.act(s, a, "tag")
		case "d", "D":
			st.act(s, a, "describe")
		case "delete":
			st.act(s, a, "kill")
		}
	}
}

// click handles a left press on a zone of the frame.
func (st *minState) click(s *Screen, a *app.App, ev Event) {
	zone := st.hits.At(ev.Y, ev.X)
	st.search.Focused = zone == "search"
	switch {
	case zone == "columns":
		st.columnsMenu(a)
	case zone == "filter":
		st.filterMenu(a)
	case strings.HasPrefix(zone, "sort:"):
		UpdateMinPrefs(a.Paths.Config, zone)
		st.prefs = loadMinPrefs(a.Paths.Config)
		st.load(a)
	case strings.HasPrefix(zone, "tag:"):
		st.selected = strings.TrimPrefix(zone, "tag:")
		st.act(s, a, "tag")
	case strings.HasPrefix(zone, "row:"):
		id := strings.TrimPrefix(zone, "row:")
		at := ev.At
		if at.IsZero() {
			at = time.Now()
		}
		double := id == st.lastClickRow && at.Sub(st.lastClick) < 500*time.Millisecond
		st.selected, st.lastClickRow, st.lastClick = id, id, at
		if double {
			st.lastClickRow = ""
			st.act(s, a, "restore")
		}
	case zone == "scroll":
		if z, ok := st.hits.Zone("scroll"); ok {
			st.offset = ScrollOffset(ev.Y-z.Y, z.H, len(st.rows), st.listRows)
			if len(st.rows) > 0 {
				st.selected = st.rows[min(st.offset, len(st.rows)-1)].ID
			}
		}
	case zone == "restore", zone == "rename", zone == "more":
		st.act(s, a, zone)
	}
}

// act runs an action on the selected console.
func (st *minState) act(s *Screen, a *app.App, action string) {
	i := st.index()
	if i < 0 {
		return
	}
	r := st.rows[i]
	var err error
	switch action {
	case "restore":
		err = a.Restore(st.view, r.ID)
	case "rename":
		name, ok := Prompt(s, i18n.T("min.rename")+" — "+r.Label, editableHint(), r.Name, 80, r.Name)
		if ok {
			err = a.Rename(r.ID, name)
		}
	case "describe":
		var text string
		var ok bool
		text, ok = Prompt(s, i18n.T("describe.title")+" — "+r.Label, i18n.T("describe.detail"), r.Description, 120, "")
		if ok {
			err = a.SetDescription(r.ID, text)
		}
	case "tag":
		err = st.native(a, func(client string) error { return a.TagMenu(client, st.pane, r.ID) })
	case "kill":
		if Confirm(s, i18n.T("min.kill"), i18n.T("min.kill_confirm", r.Label)) {
			err = a.Kill(r.ID)
		}
	case "close":
		err = a.RemoveFromView(st.view, r.ID)
	case "restore-in":
		err = st.native(a, func(client string) error { return st.restoreInMenu(a, client, r) })
	case "more":
		err = st.native(a, func(client string) error { return st.moreMenu(a, client, r) })
	}
	if err != nil {
		st.message = err.Error()
	}
	st.load(a)
}

// native opens a native tmux menu for the client looking at this pane. It
// runs in the background: the menu waits for the user, the panel does not.
func (st *minState) native(a *app.App, open func(client string) error) error {
	client := st.client
	if client == "" {
		c, e := a.ClientForPane(st.pane)
		if e != nil {
			return e
		}
		client = c
	}
	go open(client)
	return nil
}

// keys sends keys to this panel: native menu entries reach it that way.
func (st *minState) keys(a *app.App, keys ...string) string {
	return mux.Join(append([]string{"send-keys", "-t", st.pane}, keys...)...)
}

func (st *minState) moreMenu(a *app.App, client string, r minRow) error {
	action := func(name string) string {
		return a.Mux.RunAction(mux.Join(name, "--pane", st.pane, "--session", "c-"+r.ID))
	}
	items := []app.NativeItem{
		{Label: i18n.T("min.restore"), Key: "r", Command: action("restore")},
		{Label: i18n.T("min.restore_in"), Key: "v", Command: "run-shell -b " + mux.Quote(a.Mux.Self("_view", "restore-in", "--client", client, "--pane", st.pane, "--target", r.ID))},
		{},
		{Label: i18n.T("console.tag") + " ▸", Key: "t", Command: st.keys(a, "t")},
		{Label: i18n.T("console.description"), Key: "d", Command: st.keys(a, "d")},
		{Label: i18n.T("min.rename"), Key: "n", Command: st.keys(a, "r")},
		{},
		{Label: i18n.T("min.close_panel"), Key: "q", Command: action("remove")},
		{Label: i18n.T("min.kill"), Key: "m", Command: st.keys(a, "DC")},
	}
	return a.ShowMenu(client, st.pane, "#{popup_pane_right}", "#{popup_pane_bottom}", r.Label, items)
}

func (st *minState) restoreInMenu(a *app.App, client string, r minRow) error {
	return a.OpenDialogFor("restore-in", client, st.pane, r.ID)
}

func (st *minState) columnsMenu(a *app.App) {
	st.native(a, func(client string) error {
		var items []app.NativeItem
		for i, c := range minColumns {
			if c == "console" {
				continue
			}
			mark := "  "
			if contains(st.prefs.Columns, c) {
				mark = "✓ "
			}
			items = append(items, app.NativeItem{Label: mark + colTitle(c), Key: string(rune('1' + i)),
				Command: a.Mux.RunAction(mux.Join("minprefs", "--target", "col:"+c))})
		}
		return a.ShowMenu(client, st.pane, "#{popup_pane_right}", "#{popup_pane_top}", i18n.T("min.columns"), items)
	})
}

func (st *minState) filterMenu(a *app.App) {
	st.native(a, func(client string) error {
		set := func(tag string) string { return a.Mux.RunAction(mux.Join("minprefs", "--target", "filter:"+tag)) }
		mark := func(on bool) string {
			if on {
				return "✓ "
			}
			return "  "
		}
		items := []app.NativeItem{{Label: mark(st.prefs.Filter == "") + i18n.T("min.all_tags"), Key: "0", Command: set("")}, {}}
		for i, t := range st.tags {
			key := ""
			if i < 9 {
				key = string(rune('1' + i))
			}
			items = append(items, app.NativeItem{Label: mark(st.prefs.Filter == t.ID) + "#[fg=" + theme.Colour(theme.TagColor(t.Color)) + "]" +
				theme.Glyphs.Dot + "#[default] " + mux.FormatText(t.Name), Key: key, Command: set(t.ID)})
		}
		return a.ShowMenu(client, st.pane, "#{popup_pane_right}", "#{popup_pane_top}", i18n.T("min.filter_tag"), items)
	})
}

// RestoreInDialog chooses another view to show a minimized console in.
func RestoreInDialog(s *Screen, a *app.App, client, console string) (string, error) {
	views, e := a.SortedViews()
	if e != nil {
		return "", e
	}
	current := clientView(a, client)
	var targets []store.View
	var items []string
	for _, v := range views {
		if v.ID != current {
			targets = append(targets, v)
			items = append(items, v.Name)
		}
	}
	if len(targets) == 0 {
		Message(s, i18n.T("min.restore_in"), "There are no other views.")
		return "", nil
	}
	key, i := Choose(s, i18n.T("min.restore_in"), items, 0, "", []string{"Enter  " + i18n.T("min.restore"), "Q / Esc  Back"})
	if key != "enter" {
		return "", nil
	}
	return targets[i].ID, a.RestoreIn(targets[i].ID, console)
}
