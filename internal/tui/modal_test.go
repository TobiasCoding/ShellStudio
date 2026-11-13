// SPDX-License-Identifier: GPL-3.0-only
package tui

import (
	"strings"
	"testing"
)

func TestModalCentersItsBoxOverTheBackdrop(t *testing.T) {
	prev := Backdrop
	t.Cleanup(func() { Backdrop = prev })
	Backdrop = func(client string, w, h int) []string {
		if client != "/dev/pts/7" {
			t.Errorf("client %q", client)
		}
		return []string{strings.Repeat("b", w), strings.Repeat("c", w)}
	}
	m := parseModal("20,6,/dev/pts/7")
	w, h := m.layout(40, 10, true)
	if w != 18 || h != 4 || m.ox != 11 || m.oy != 3 {
		t.Fatalf("w=%d h=%d ox=%d oy=%d", w, h, m.ox, m.oy)
	}
	s := &Screen{W: w, H: h, modal: m}
	s.Erase()
	s.Draw(0, "TITLE", Bold)
	frame := m.compose(s.frame, w, h)
	row := func(y int) string {
		var b strings.Builder
		for _, c := range frame[y] {
			if c.r != 0 {
				b.WriteRune(c.r)
			}
		}
		return b.String()
	}
	if row(0) != strings.Repeat("b", 40) || !strings.HasPrefix(row(2), "          ┌") || !strings.Contains(row(3), "│ TITLE") {
		t.Fatalf("frame:\n%s\n%s\n%s\n%s", row(0), row(1), row(2), row(3))
	}
	if frame[0][0].style != backdropStyle || frame[3][12].style != Bold {
		t.Fatal("styles", frame[0][0].style, frame[3][12].style)
	}
	// Percentages and oversized boxes fit the client.
	if w, h := parseModal("50%,999,x").layout(40, 10, false); w != 18 || h != 8 {
		t.Fatal(w, h)
	}
}

func TestModalClickOutsideIsEscape(t *testing.T) {
	m := parseModal("20,6,")
	w, h := m.layout(40, 10, false)
	s := &Screen{W: w, H: h, modal: m}
	in := Event{Kind: MouseEvent, X: 12, Y: 4, Press: true}
	if ev, ok := s.translate(in); !ok || ev.X != 1 || ev.Y != 1 || ev.Kind != MouseEvent {
		t.Fatal(ev, ok)
	}
	// The border belongs to the box: a click there does not close it.
	if ev, ok := s.translate(Event{Kind: MouseEvent, X: 10, Y: 2, Press: true}); !ok || ev.Kind != MouseEvent {
		t.Fatal(ev, ok)
	}
	if s.ClosedOutside() {
		t.Fatal("closed by a click inside")
	}
	if ev, ok := s.translate(Event{Kind: MouseEvent, X: 1, Y: 1, Press: true}); !ok || !ev.Is("esc") || !s.ClosedOutside() {
		t.Fatal(ev, ok)
	}
	// Releases, motion and the wheel outside do nothing.
	for _, out := range []Event{{Kind: MouseEvent, X: 1, Y: 1, Release: true}, {Kind: MouseEvent, X: 1, Y: 1, Press: true, Motion: true},
		{Kind: MouseEvent, X: 1, Y: 1, Press: true, Button: 64}} {
		if _, ok := s.translate(out); ok {
			t.Fatal("not dropped", out)
		}
	}
	// Keys pass unchanged; a screen that is not a modal changes nothing.
	if ev, _ := s.translate(key("x")); !ev.Is("x") {
		t.Fatal(ev)
	}
	plain := &Screen{W: 10, H: 5}
	if ev, ok := plain.translate(Event{Kind: MouseEvent, X: 1, Y: 1, Press: true}); !ok || ev.X != 1 {
		t.Fatal(ev)
	}
}

func TestStrayReleaseIsDropped(t *testing.T) {
	if (&Screen{}).strayRelease(Event{Kind: MouseEvent, Release: true}) {
		t.Fatal("pane dropped a release")
	}
	s := &Screen{modal: parseModal("20,6,")}
	if !s.strayRelease(Event{Kind: MouseEvent, Release: true}) {
		t.Fatal("release without press kept")
	}
	if s.strayRelease(Event{Kind: MouseEvent, Press: true}) || s.strayRelease(Event{Kind: MouseEvent, Release: true}) {
		t.Fatal("click dropped")
	}
}
