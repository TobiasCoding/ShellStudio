// SPDX-License-Identifier: GPL-3.0-only
package tui

import (
	"strings"
	"testing"
)

// errTestEnd stops a program under test once its scripted events run out.
type errTestEnd struct{}

func init() { errTestEndValue = errTestEnd{} }

// newTestScreen is a w×h screen without a terminal. Read returns the events
// given to it in order and then stops the program with errTestEnd, which
// runTest recovers.
func newTestScreen(w, h int, events ...Event) *Screen {
	s := &Screen{W: w, H: h}
	s.pending = append(s.pending, events...)
	s.pending = append(s.pending, Event{Kind: testEnd})
	return s
}

// testEnd is an event kind only test screens produce.
const testEnd EventKind = 1000

// runTest runs fn until the scripted events of s run out.
func runTest(t *testing.T, s *Screen, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(errTestEnd); !ok {
				panic(r)
			}
		}
	}()
	fn()
}

func key(k string) Event   { return Event{Kind: KeyEvent, Key: k} }
func click(y, x int) Event { return Event{Kind: MouseEvent, Y: y, X: x, Press: true} }
func release(y, x int) Event {
	return Event{Kind: MouseEvent, Y: y, X: x, Release: true}
}

func frameText(s *Screen) string { return strings.Join(s.Frame(), "\n") }

func TestTestScreenFrame(t *testing.T) {
	s := newTestScreen(20, 3)
	s.Erase()
	s.DrawAt(1, 2, "hola", Highlight)
	if got := s.Frame()[1]; strings.TrimRight(got, " ") != "  hola" {
		t.Fatalf("%q", got)
	}
	if s.StyleAt(1, 2) != Highlight || s.StyleAt(1, 0) != Normal {
		t.Fatal("styles")
	}
	if sgr(Highlight) != "\x1b[0;1;38;5;231;48;5;24m" || sgr(Normal) != "\x1b[0;37;48;5;16m" {
		t.Fatalf("%q %q", sgr(Highlight), sgr(Normal))
	}
	s.Refresh() // no terminal: must not write anywhere
}
