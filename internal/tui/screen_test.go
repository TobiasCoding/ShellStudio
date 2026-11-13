// SPDX-License-Identifier: GPL-3.0-only
package tui

import "testing"

func TestFragmentedTerminalInput(t *testing.T) {
	for _, tc := range []struct {
		input string
		kind  EventKind
		key   string
	}{
		{"\x1b[<0;38;24M", MouseEvent, ""},
		{"\x1b[<0;38;24m", MouseEvent, ""},
		{"\x1b[<35;38;24M", MouseEvent, ""},
		{"\x1b[200~file name\x1b[201~", PasteEvent, ""},
		{"\x1bOR", KeyEvent, "f3"},
		{"\x1b[18~", KeyEvent, "f7"},
		{"é", KeyEvent, "é"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			for i := 1; i < len(tc.input); i++ {
				if _, _, ok := parse([]byte(tc.input[:i]), false); ok {
					t.Fatalf("consumed incomplete sequence at byte %d", i)
				}
			}
			e, rest, ok := parse([]byte(tc.input+"x"), false)
			if !ok || e.Kind != tc.kind || e.Key != tc.key || string(rest) != "x" {
				t.Fatalf("event=%+v rest=%q ok=%v", e, rest, ok)
			}
		})
	}
}

func TestMouseMotionIsNotAClick(t *testing.T) {
	e, _, _ := parse([]byte("\x1b[<35;177;24M"), false)
	if down, up := e.Left(); down || up || !e.Motion || e.X != 176 || e.Y != 23 {
		t.Fatalf("motion became a click: %+v", e)
	}
}

func TestShiftArrowsAreReported(t *testing.T) {
	for in, want := range map[string]Event{
		"\x1b[1;2A": {Kind: KeyEvent, Key: "up", Shift: true},
		"\x1b[1;2B": {Kind: KeyEvent, Key: "down", Shift: true},
		"\x1b[1;5A": {Kind: KeyEvent, Key: "up"},
		"\x1b[A":    {Kind: KeyEvent, Key: "up"},
	} {
		if e, _, ok := parse([]byte(in), false); !ok || e != want {
			t.Errorf("%q: %+v", in, e)
		}
	}
}
