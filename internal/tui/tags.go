// SPDX-License-Identifier: GPL-3.0-only
package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
	"shellstudio/internal/app"
	"shellstudio/internal/i18n"
	"shellstudio/internal/store"
	"shellstudio/internal/theme"
)

// hint is one clickable shortcut of a dialog's help line: its translated
// text and the key a click on it stands for ("" when it is not clickable).
type hint struct{ text, key string }

type placedHint struct {
	hint
	row, x int
}

// layoutHints places hints left to right in w columns from column 1,
// wrapping to another row when one does not fit.
func layoutHints(hints []hint, w int) ([]placedHint, int) {
	var out []placedHint
	row, x := 0, 1
	for _, h := range hints {
		hw := runewidth.StringWidth(h.text)
		if x > 1 && x+hw > w-1 {
			row, x = row+1, 1
		}
		out = append(out, placedHint{h, row, x})
		x += hw + 3
	}
	return out, row + 1
}

// drawHints draws the hints from row y and registers the clickable ones.
func drawHints(s *Screen, hm *HitMap, y int, placed []placedHint) {
	for _, p := range placed {
		s.DrawAt(y+p.row, p.x, p.text, Muted)
		if p.key != "" {
			hm.Add(y+p.row, p.x, runewidth.StringWidth(p.text), 1, "key:"+p.key)
		}
	}
}

func tagsHints() []hint {
	return []hint{
		{i18n.T("tags.hint_new"), "n"}, {i18n.T("tags.hint_rename"), "r"}, {i18n.T("tags.hint_color"), "c"},
		{i18n.T("tags.hint_up"), "["}, {i18n.T("tags.hint_down"), "]"},
		{i18n.T("tags.hint_delete"), "delete"}, {i18n.T("tags.hint_close"), "esc"},
	}
}

// tagRow is one row of a tag list: a badge (color >= 0) or plain text, and
// a note on the right.
type tagRow struct {
	name  string
	color int
	note  string
}

const badgeWidth = 26

// drawTagRows draws rows[top:] from row y, at most capacity of them, and
// registers each under "row:N".
func drawTagRows(s *Screen, hm *HitMap, y, capacity, top, selected int, rows []tagRow) {
	for i := top; i < len(rows) && i < top+capacity; i++ {
		r, yy := rows[i], y+i-top
		style, note := Normal, Muted
		prefix := "  "
		if i == selected {
			style, note, prefix = Highlight, Highlight, "> "
		}
		s.Draw(yy, prefix, style)
		x := 3
		if r.color >= 0 {
			x += Badge(s, yy, x, r.name, r.color, badgeWidth)
		} else {
			s.DrawAt(yy, x, runewidth.Truncate(r.name, badgeWidth, "…"), style)
			x += runewidth.StringWidth(runewidth.Truncate(r.name, badgeWidth, "…"))
		}
		if r.note != "" {
			s.DrawAt(yy, max(x+2, 3+badgeWidth+2), r.note, note)
		}
		hm.Add(yy, 0, s.W, 1, fmt.Sprintf("row:%d", i))
	}
	Scrollbar(s, y, s.W-2, capacity, len(rows), capacity, top)
}

// scrollTop keeps the selected row inside a window of capacity rows.
func scrollTop(top, selected, capacity, total int) int {
	if selected < top {
		top = selected
	}
	if selected >= top+capacity {
		top = selected - capacity + 1
	}
	return max(0, min(top, total-capacity))
}

// navigate applies a list movement key; ok is false for any other key.
func navigate(key string, selected, total, capacity int) (int, bool) {
	if total == 0 {
		switch key {
		case "up", "down", "k", "j", "home", "end", "pgup", "pgdown":
			return 0, true
		}
		return 0, false
	}
	switch key {
	case "down", "j":
		return (selected + 1) % total, true
	case "up", "k":
		return (selected - 1 + total) % total, true
	case "home":
		return 0, true
	case "end":
		return total - 1, true
	case "pgdown":
		return min(total-1, selected+capacity), true
	case "pgup":
		return max(0, selected-capacity), true
	}
	return selected, false
}

// listEvent turns a mouse event of a list dialog into a key: a click on a
// row selects it and another click on it is "enter"; a click on a hint is its
// key; the wheel moves the selection. ok is false when there is nothing to do.
func listEvent(ev Event, hm *HitMap, selected *int) (string, bool) {
	switch ev.Kind {
	case KeyEvent:
		return ev.Key, true
	case MouseEvent:
	default:
		return "", false
	}
	switch ev.Button {
	case 64:
		return "up", ev.Press
	case 65:
		return "down", ev.Press
	}
	if _, up := ev.Left(); !up {
		return "", false
	}
	id := hm.At(ev.Y, ev.X)
	if k, ok := strings.CutPrefix(id, "key:"); ok {
		return k, true
	}
	var row int
	if _, e := fmt.Sscanf(id, "row:%d", &row); e == nil {
		if row == *selected {
			return "enter", true
		}
		*selected = row
	}
	return "", false
}

func usesText(n int) string {
	if n == 1 {
		return i18n.T("tags.uses_one")
	}
	return i18n.T("tags.uses", n)
}

// TagsDialog manages the tags: create, rename, recolor, reorder and delete.
// Errors of App are shown in the message line.
func TagsDialog(s *Screen, a *app.App) {
	selected, top, message := 0, 0, ""
	var hm HitMap
	for {
		tags, e := a.Tags()
		if e != nil {
			message = e.Error()
		}
		rows := make([]tagRow, len(tags))
		for i, t := range tags {
			n, _ := a.TagUses(t.ID)
			rows[i] = tagRow{t.Name, t.Color, usesText(n)}
		}
		selected = max(0, min(selected, len(tags)-1))
		placed, hintRows := layoutHints(tagsHints(), s.W)
		capacity := s.H - hintRows - 4
		small := s.H < 7 || s.W < 30 || capacity < 1
		s.Erase()
		hm.Reset()
		s.Draw(0, i18n.T("tags.title"), Bold)
		if small {
			s.Draw(2, i18n.T("tags.small"), Normal)
		} else {
			top = scrollTop(top, selected, capacity, len(rows))
			drawTagRows(s, &hm, 2, capacity, top, selected, rows)
			if len(rows) == 0 {
				s.Draw(2, i18n.T("tags.empty"), Muted)
			}
			s.Draw(s.H-hintRows-2, message, Bold)
			drawHints(s, &hm, s.H-hintRows, placed)
		}
		s.Refresh()
		ev := s.Read(-1)
		if ev.Kind == ResizeEvent {
			continue
		}
		key, ok := listEvent(ev, &hm, &selected)
		if !ok {
			continue
		}
		message = ""
		if key == "esc" || key == "q" || key == "Q" {
			return
		}
		if small {
			continue
		}
		if n, ok := navigate(key, selected, len(tags), capacity); ok {
			selected = n
			continue
		}
		var current store.Tag
		if len(tags) > 0 {
			current = tags[selected]
		}
		switch key {
		case "n", "N":
			t, err := newTag(s, a, tags)
			if err != nil {
				message = err.Error()
			} else if t.ID != "" {
				selected = len(tags)
			}
		case "r", "R", "enter":
			if current.ID == "" {
				break
			}
			name, ok := Prompt(s, i18n.T("tags.rename"), i18n.T("tags.name_detail"), current.Name, 24, "")
			if ok && name != current.Name {
				current.Name = name
				if err := a.UpdateTag(current); err != nil {
					message = err.Error()
				}
			}
		case "c", "C":
			if current.ID == "" {
				break
			}
			if c, ok := pickColor(s, i18n.T("tags.color"), current.Name, current.Color); ok && c != current.Color {
				current.Color = c
				if err := a.UpdateTag(current); err != nil {
					message = err.Error()
				}
			}
		case "[", "]", "shift+up", "shift+down":
			if current.ID == "" {
				break
			}
			delta := 1
			if key == "[" || key == "shift+up" {
				delta = -1
			}
			if err := a.MoveTag(current.ID, delta); err != nil {
				message = err.Error()
			} else {
				selected = max(0, min(len(tags)-1, selected+delta))
			}
		case "delete":
			if current.ID == "" {
				break
			}
			n, err := a.TagUses(current.ID)
			if err != nil {
				message = err.Error()
				break
			}
			if Confirm(s, i18n.T("tags.title"), i18n.T("tags.delete_confirm", current.Name, n)) {
				if err := a.DeleteTag(current.ID); err != nil {
					message = err.Error()
				}
			}
		}
	}
}

// newTag asks for the name and then the color of a new tag. A cancelled
// step creates nothing and returns an empty tag.
func newTag(s *Screen, a *app.App, tags []store.Tag) (store.Tag, error) {
	name, ok := Prompt(s, i18n.T("tags.new"), i18n.T("tags.name_detail"), "", 24, "")
	if !ok {
		return store.Tag{}, nil
	}
	// Check the name before the color is chosen; AddTag checks it again.
	name = strings.TrimSpace(name)
	if e := app.ValidTagName(name); e != nil {
		return store.Tag{}, e
	}
	used := map[int]bool{}
	for _, t := range tags {
		if strings.EqualFold(t.Name, name) {
			return store.Tag{}, errors.New(i18n.T("tags.duplicate", name))
		}
		used[t.Color] = true
	}
	// Suggest the first color no tag uses yet.
	color := theme.TagPalette[len(tags)%len(theme.TagPalette)]
	for _, c := range theme.TagPalette {
		if !used[c] {
			color = c
			break
		}
	}
	color, ok = pickColor(s, i18n.T("tags.color"), name, color)
	if !ok {
		return store.Tag{}, nil
	}
	return a.AddTag(name, color)
}

// pickColor chooses one of the colors of theme.TagPalette, shown as
// numbered swatches, with arrows and Enter, digits or a click. name, when
// given, is previewed as a badge in the chosen color.
func pickColor(s *Screen, title, name string, current int) (int, bool) {
	palette := theme.TagPalette
	selected := 0
	for i, c := range palette {
		if c == current {
			selected = i
		}
	}
	const cell = 7 // "[" + 5-column swatch + "]"
	var hm HitMap
	hints := []hint{{i18n.T("tags.hint_pick"), ""}, {i18n.T("tags.hint_accept"), "enter"}, {i18n.T("tags.hint_cancel"), "esc"}}
	for {
		perRow := max(1, min(len(palette), (s.W-2)/cell))
		rows := (len(palette) + perRow - 1) / perRow
		placed, hintRows := layoutHints(hints, s.W)
		s.Erase()
		hm.Reset()
		s.Draw(0, title, Bold)
		for i, c := range palette {
			y, x := 2+(i/perRow)*2, 1+(i%perRow)*cell
			s.DrawAt(y, x+1, fmt.Sprintf("  %d  ", i+1), Color(theme.TagText(c), c))
			if i == selected {
				s.DrawAt(y, x, "[", Bold)
				s.DrawAt(y, x+cell-1, "]", Bold)
			}
			hm.Add(y, x, cell, 1, fmt.Sprintf("swatch:%d", i))
		}
		if name != "" {
			Badge(s, 2+rows*2, 2, name, palette[selected], badgeWidth)
		}
		drawHints(s, &hm, s.H-hintRows, placed)
		s.Refresh()
		ev := s.Read(-1)
		key := ev.Key
		switch ev.Kind {
		case KeyEvent:
		case MouseEvent:
			if _, up := ev.Left(); !up {
				continue
			}
			id := hm.At(ev.Y, ev.X)
			var i int
			if _, e := fmt.Sscanf(id, "swatch:%d", &i); e == nil {
				return palette[i], true
			}
			k, ok := strings.CutPrefix(id, "key:")
			if !ok {
				continue
			}
			key = k
		default:
			continue
		}
		switch key {
		case "esc", "q", "Q":
			return current, false
		case "enter", " ":
			return palette[selected], true
		case "right", "l", "tab":
			selected = (selected + 1) % len(palette)
		case "left", "h", "backtab":
			selected = (selected - 1 + len(palette)) % len(palette)
		case "down", "j":
			selected = min(len(palette)-1, selected+perRow)
		case "up", "k":
			selected = max(0, selected-perRow)
		default:
			if len(key) == 1 && key[0] >= '1' && key[0] < '1'+byte(len(palette)) {
				selected = int(key[0] - '1')
			}
		}
	}
}

// ChooseTag sets the tag of a console from a popup list: every tag, then
// "No tag" and "Manage…", which opens TagsDialog. It is the fallback of the
// native tmux tag menu.
func ChooseTag(s *Screen, a *app.App, console string) error {
	c, e := a.Store.Console(console)
	if e != nil {
		return e
	}
	selected, top, message, first := 0, 0, "", true
	var hm HitMap
	hints := []hint{{i18n.T("tags.hint_choose"), "enter"}, {i18n.T("tags.hint_cancel"), "esc"}}
	for {
		tags, e := a.Tags()
		if e != nil {
			return e
		}
		rows := make([]tagRow, 0, len(tags)+2)
		for i, t := range tags {
			note := ""
			if t.ID == c.TagID {
				note = "✓"
				if first {
					selected = i
				}
			}
			rows = append(rows, tagRow{t.Name, t.Color, note})
		}
		none, manage := len(tags), len(tags)+1
		noneNote := ""
		if c.TagID == "" {
			noneNote = "✓"
			if first {
				selected = none
			}
		}
		first = false
		rows = append(rows, tagRow{i18n.T("console.no_tag"), -1, noneNote}, tagRow{i18n.T("console.manage_tags"), -1, ""})
		selected = max(0, min(selected, len(rows)-1))
		placed, hintRows := layoutHints(hints, s.W)
		capacity := s.H - hintRows - 4
		small := s.H < 7 || s.W < 30 || capacity < 1
		s.Erase()
		hm.Reset()
		s.Draw(0, i18n.T("tags.choose_title", c.Name), Bold)
		if small {
			s.Draw(2, i18n.T("tags.small"), Normal)
		} else {
			top = scrollTop(top, selected, capacity, len(rows))
			drawTagRows(s, &hm, 2, capacity, top, selected, rows)
			s.Draw(s.H-hintRows-2, message, Bold)
			drawHints(s, &hm, s.H-hintRows, placed)
		}
		s.Refresh()
		ev := s.Read(-1)
		if ev.Kind == ResizeEvent {
			continue
		}
		key, ok := listEvent(ev, &hm, &selected)
		if !ok {
			continue
		}
		message = ""
		if key == "esc" || key == "q" || key == "Q" {
			return nil
		}
		if small {
			continue
		}
		if n, ok := navigate(key, selected, len(rows), capacity); ok {
			selected = n
			continue
		}
		if key != "enter" {
			continue
		}
		if selected == manage {
			TagsDialog(s, a)
			if c, e = a.Store.Console(console); e != nil {
				return e
			}
			// The list changed: select the console's tag again.
			first = true
			continue
		}
		tag := ""
		if selected != none {
			tag = tags[selected].ID
		}
		if e := a.SetTag(console, tag); e != nil {
			message = e.Error()
			continue
		}
		return nil
	}
}

// DescribeDialog edits the description of a console.
func DescribeDialog(s *Screen, a *app.App, console string) error {
	c, e := a.Store.Console(console)
	if e != nil {
		return e
	}
	text, ok := Prompt(s, i18n.T("describe.title")+" | "+c.Name, i18n.T("describe.detail"), c.Description, 120, "")
	if !ok {
		return nil
	}
	return a.SetDescription(console, text)
}

// LanguageDialog chooses the language of the interface.
func LanguageDialog(s *Screen, a *app.App) error {
	values := []string{"auto", "es", "en"}
	items := []string{i18n.T("lang.auto"), i18n.T("lang.es"), i18n.T("lang.en")}
	selected := 0
	for i, v := range values {
		if v == a.Language() {
			selected = i
		}
	}
	for {
		key, i := Choose(s, i18n.T("lang.title"), items, selected, "", nil)
		switch key {
		case "enter":
			return a.SetLanguage(values[i])
		case "esc", "q":
			return nil
		}
		selected = i
	}
}
