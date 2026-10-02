// SPDX-License-Identifier: GPL-3.0-only
package mux

import (
	"strings"
	"testing"
)

func TestQuoteMatchesPOSIXShell(t *testing.T) {
	for in, want := range map[string]string{
		"":               "''",
		"plain/path-1.x": "plain/path-1.x",
		"with space":     "'with space'",
		"it's":           `'it'"'"'s'`,
		"#{client_name}": "'#{client_name}'",
	} {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Script([]string{"a", "b c"}, []string{"d"}); got != "a 'b c' ; d" {
		t.Fatal(got)
	}
}

func TestBatchSplitsLongCommandLists(t *testing.T) {
	var calls [][]string
	m := &Mux{Run: func(_ bool, args ...string) (string, error) {
		calls = append(calls, args)
		return "x\n", nil
	}}
	var cmds [][]string
	for i := 0; i < 200; i++ {
		cmds = append(cmds, []string{"set-option", "-g", "@opt", strings.Repeat("v", 100)})
	}
	out, e := m.Batch(true, cmds...)
	if e != nil || len(calls) < 2 {
		t.Fatal(len(calls), e)
	}
	total := 0
	for _, c := range calls {
		size := 0
		for _, a := range c {
			size += len(a) + 1
		}
		if size > batchBytes+200 {
			t.Fatal("chunk too large:", size)
		}
		for _, a := range c {
			if a == "set-option" {
				total++
			}
		}
	}
	if total != 200 || strings.Count(out, "x") != len(calls) {
		t.Fatal(total, out)
	}
}

func TestViewControlsBindTheSesionesShortcuts(t *testing.T) {
	m := &Mux{Binary: "/bin/shellstudio", Tmux: "tmux", Version: "v"}
	m.Paths.Runtime = "/run/x"
	keys := map[string]bool{}
	for _, c := range m.ViewsControls(Sizes{MenuW: 90, MenuH: 14, MenuStacked: 30, NewW: 40, NewH: 12, ArrangeW: 50, ArrangeRows: 19, BrowseW: 50, BrowseRows: 13}) {
		if len(c) > 2 && c[0] == "bind-key" && c[1] == "-n" {
			keys[c[2]] = true
		}
	}
	for _, k := range []string{"F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "C-S-DC", "C-S-Left", "C-S-Right",
		"C-M-Left", "M-1", "M-5", "M-[", "M-]", "MouseDown1Pane", "MouseUp1Pane", "MouseDown3Pane", "MouseDrag1Pane",
		"MouseDragEnd1Pane", "DoubleClick1Pane", "MouseDown1Border", "C-BSpace"} {
		if !keys[k] {
			t.Error("missing binding", k)
		}
	}
}
