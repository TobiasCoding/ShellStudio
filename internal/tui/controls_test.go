// SPDX-License-Identifier: GPL-3.0-only
package tui

import (
	"strings"
	"testing"
)

func TestFieldEditing(t *testing.T) {
	f := &Field{Focused: true, Limit: 20}
	for _, k := range []string{"a", "b", "c", "left", "left", "X", "end", "backspace", "home", "delete"} {
		f.Handle(key(k))
	}
	if f.Text() != "Xb" || f.Cursor != 0 {
		t.Fatalf("%q %d", f.Text(), f.Cursor)
	}
	if c, ch := f.Handle(Event{Kind: PasteEvent, Text: "one two\nthree"}); !c || !ch || f.Text() != "one two threeXb" {
		t.Fatalf("%q", f.Text())
	}
	f.Handle(key("ctrl+w"))
	if f.Text() != "one two Xb" {
		t.Fatalf("%q", f.Text())
	}
	if c, _ := f.Handle(key("enter")); c {
		t.Fatal("enter consumed")
	}
	f.Focused = false
	if c, _ := f.Handle(key("z")); c {
		t.Fatal("unfocused field consumed a key")
	}
}

func TestFieldDrawsPlaceholderAndCursor(t *testing.T) {
	s := newTestScreen(30, 2)
	s.Erase()
	f := &Field{Placeholder: "Buscar archivos", Focused: true}
	f.Draw(s, 0, 0, 20)
	if !strings.Contains(s.Frame()[0], "Buscar archivos") || !s.StyleAt(0, 1).Has(AttrReverse) {
		t.Fatalf("%q", s.Frame()[0])
	}
	f.Set(strings.Repeat("x", 40))
	f.Draw(s, 1, 0, 20)
	if !s.StyleAt(1, 19).Has(AttrReverse) && !s.StyleAt(1, 18).Has(AttrReverse) {
		t.Fatalf("cursor not visible: %q", s.Frame()[1])
	}
}

func TestHitMapButtonsBadgeScrollbar(t *testing.T) {
	s := newTestScreen(40, 6)
	s.Erase()
	var h HitMap
	end := Button(s, &h, 0, 2, "Restaurar", "restore", Cyan)
	if end != 2+ButtonWidth("Restaurar") || h.At(0, 3) != "restore" || h.At(0, 1) != "" || h.At(1, 3) != "" {
		t.Fatal(end, h.At(0, 3))
	}
	h.Add(0, 0, 40, 1, "row")
	if h.At(0, 3) != "row" {
		t.Fatal("last zone wins")
	}
	if z, ok := h.Zone("restore"); !ok || z.X != 2 {
		t.Fatal(z)
	}
	h.Reset()
	if h.At(0, 3) != "" {
		t.Fatal("reset")
	}
	if w := Badge(s, 2, 0, "En curso", 33, 20); w != 10 || !strings.HasPrefix(s.Frame()[2], " En curso ") || s.StyleAt(2, 1).Bg != 33 {
		t.Fatal(w, s.Frame()[2])
	}
	Scrollbar(s, 1, 39-1, 4, 100, 10, 90)
	if !strings.HasSuffix(strings.TrimRight(s.Frame()[4], " "), "█") {
		t.Fatalf("%q", s.Frame()[4])
	}
	if ScrollOffset(3, 4, 100, 10) != 90 || ScrollOffset(0, 4, 100, 10) != 0 || ScrollOffset(5, 4, 5, 10) != 0 {
		t.Fatal("offsets")
	}
}
