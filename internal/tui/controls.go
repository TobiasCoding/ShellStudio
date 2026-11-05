// SPDX-License-Identifier: GPL-3.0-only
package tui

import (
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
	"shellstudio/internal/theme"
)

// Field is a non-blocking single-line text input: the program owns the loop
// and passes each event to Handle. Editing follows Prompt: Backspace,
// Ctrl+U (clear) and Ctrl+W (previous word), plus a movable cursor.
type Field struct {
	Value       []rune
	Cursor      int
	Limit       int // 0 is 256 characters.
	Placeholder string
	Focused     bool
}

func (f *Field) Text() string { return string(f.Value) }

// Set replaces the value and puts the cursor at its end.
func (f *Field) Set(text string) {
	f.Value = []rune(text)
	f.Cursor = len(f.Value)
}

func (f *Field) limit() int {
	if f.Limit <= 0 {
		return 256
	}
	return f.Limit
}

func (f *Field) insert(r rune) bool {
	if !unicode.IsPrint(r) || len(f.Value) >= f.limit() {
		return false
	}
	f.Cursor = max(0, min(f.Cursor, len(f.Value)))
	f.Value = append(f.Value[:f.Cursor], append([]rune{r}, f.Value[f.Cursor:]...)...)
	f.Cursor++
	return true
}

// Handle applies a key or a paste to a focused field. It reports whether the
// event was consumed and whether the value changed. Enter, Escape, Tab and
// arrows up/down are left to the program.
func (f *Field) Handle(ev Event) (consumed, changed bool) {
	if !f.Focused {
		return false, false
	}
	f.Cursor = max(0, min(f.Cursor, len(f.Value)))
	if ev.Kind == PasteEvent {
		for _, r := range ev.Text {
			if r == '\n' || r == '\r' || r == '\t' {
				r = ' '
			}
			if f.insert(r) {
				changed = true
			}
		}
		return true, changed
	}
	if ev.Kind != KeyEvent {
		return false, false
	}
	before := string(f.Value)
	switch ev.Key {
	case "left":
		f.Cursor = max(0, f.Cursor-1)
	case "right":
		f.Cursor = min(len(f.Value), f.Cursor+1)
	case "home", "ctrl+a":
		f.Cursor = 0
	case "end", "ctrl+e":
		f.Cursor = len(f.Value)
	case "backspace":
		if f.Cursor > 0 {
			f.Value = append(f.Value[:f.Cursor-1], f.Value[f.Cursor:]...)
			f.Cursor--
		}
	case "delete", "ctrl+d":
		if f.Cursor < len(f.Value) {
			f.Value = append(f.Value[:f.Cursor], f.Value[f.Cursor+1:]...)
		}
	case "ctrl+u":
		f.Value, f.Cursor = nil, 0
	case "ctrl+w":
		i := f.Cursor
		for i > 0 && f.Value[i-1] == ' ' {
			i--
		}
		for i > 0 && f.Value[i-1] != ' ' {
			i--
		}
		f.Value = append(f.Value[:i], f.Value[f.Cursor:]...)
		f.Cursor = i
	default:
		r := []rune(ev.Key)
		if len(r) != 1 || !f.insert(r[0]) {
			return false, false
		}
	}
	return true, string(f.Value) != before
}

// Draw paints the field in w columns at (y, x): the value (or the greyed
// placeholder) and, when focused, the cursor as a reversed cell.
func (f *Field) Draw(s *Screen, y, x, w int) {
	if w <= 0 {
		return
	}
	s.DrawAt(y, x, strings.Repeat(" ", w), FieldStyle)
	if len(f.Value) == 0 {
		ph := runewidth.Truncate(f.Placeholder, w-2, "…")
		s.DrawAt(y, x+1, ph, Style{Fg: theme.Muted, Bg: theme.Panel})
		if f.Focused {
			// The cursor sits on the first letter of the placeholder.
			first := " "
			if r := []rune(ph); len(r) > 0 {
				first = string(r[0])
			}
			s.DrawAt(y, x+1, first, Style{Fg: theme.Muted, Bg: theme.Panel, Attr: AttrReverse})
		}
		return
	}
	// Scroll so the cursor stays visible.
	inner := max(1, w-2)
	start := 0
	for runewidth.StringWidth(string(f.Value[start:min(f.Cursor, len(f.Value))])) >= inner {
		start++
	}
	text := runewidth.Truncate(string(f.Value[start:]), inner, "")
	s.DrawAt(y, x+1, text, FieldStyle)
	if f.Focused {
		cx := x + 1 + runewidth.StringWidth(string(f.Value[start:f.Cursor]))
		r := " "
		if f.Cursor < len(f.Value) {
			r = string(f.Value[f.Cursor])
		}
		s.DrawAt(y, cx, r, FieldStyle.With(AttrReverse))
	}
}

// Hit is a clickable rectangle of a frame.
type Hit struct {
	Y, X, W, H int
	ID         string
}

// HitMap records the clickable zones drawn in a frame. Reset it with each
// Erase; the zone added last wins where zones overlap.
type HitMap struct{ hits []Hit }

func (h *HitMap) Reset() { h.hits = h.hits[:0] }

func (h *HitMap) Add(y, x, w, height int, id string) {
	if w > 0 && height > 0 {
		h.hits = append(h.hits, Hit{y, x, w, height, id})
	}
}

// At returns the ID of the zone under a cell, or "".
func (h *HitMap) At(y, x int) string {
	for i := len(h.hits) - 1; i >= 0; i-- {
		z := h.hits[i]
		if y >= z.Y && y < z.Y+z.H && x >= z.X && x < z.X+z.W {
			return z.ID
		}
	}
	return ""
}

// Zone returns the rectangle of an ID.
func (h *HitMap) Zone(id string) (Hit, bool) {
	for i := len(h.hits) - 1; i >= 0; i-- {
		if h.hits[i].ID == id {
			return h.hits[i], true
		}
	}
	return Hit{}, false
}

// Button draws "[ label ]" at (y, x), registers it under id and returns the
// column after it.
func Button(s *Screen, h *HitMap, y, x int, label, id string, style Style) int {
	text := "[ " + label + " ]"
	w := runewidth.StringWidth(text)
	s.DrawAt(y, x, text, style)
	if h != nil {
		h.Add(y, x, w, 1, id)
	}
	return x + w
}

// ButtonWidth is the width Button takes for a label.
func ButtonWidth(label string) int { return runewidth.StringWidth(label) + 4 }

// Scrollbar draws a vertical bar of height rows at column x from row y for
// a list of total rows showing visible rows from offset. Nothing is drawn when
// everything fits.
func Scrollbar(s *Screen, y, x, height, total, visible, offset int) {
	if height <= 0 || total <= visible || visible <= 0 {
		return
	}
	thumb := max(1, height*visible/total)
	maxOffset := total - visible
	top := 0
	if maxOffset > 0 {
		top = (height - thumb) * min(offset, maxOffset) / maxOffset
	}
	for i := 0; i < height; i++ {
		if i >= top && i < top+thumb {
			s.DrawAt(y+i, x, "█", Style{Fg: theme.Muted})
		} else {
			s.DrawAt(y+i, x, "│", Style{Fg: theme.Border})
		}
	}
}

// ScrollOffset converts a click or drag at row (relative to the bar's top)
// into the offset that centres the thumb there.
func ScrollOffset(row, height, total, visible int) int {
	if height <= 1 || total <= visible {
		return 0
	}
	maxOffset := total - visible
	o := row * maxOffset / (height - 1)
	return max(0, min(maxOffset, o))
}

// Badge draws a tag as " name " on its color, at most w columns, and returns
// the width it took.
func Badge(s *Screen, y, x int, name string, color, w int) int {
	if w < 3 {
		return 0
	}
	bg := theme.TagColor(color)
	text := " " + runewidth.Truncate(name, w-2, "…") + " "
	s.DrawAt(y, x, text, Style{Fg: uint8(theme.TagText(bg)), Bg: uint8(bg)})
	return runewidth.StringWidth(text)
}
