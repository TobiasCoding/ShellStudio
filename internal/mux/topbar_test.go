// SPDX-License-Identifier: GPL-3.0-only
package mux

import (
	"regexp"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
	"shellstudio/internal/store"
	"shellstudio/internal/theme"
)

// tmux keeps 15 characters of a user range (docs/redesign-spike.md): longer
// ones would arrive cut and match nothing.
func TestStatusRangesFitTmux(t *testing.T) {
	format := topBar() + TabsFormat([]store.View{{ID: "0123456789abcdef01234567", Name: "Dev"}, {ID: "x", Name: "#odd,name"}}, 0)
	ranges := regexp.MustCompile(`range=user\|([^\]]*)\]`).FindAllStringSubmatch(format, -1)
	if len(ranges) < 6 {
		t.Fatal("ranges", ranges)
	}
	for _, r := range ranges {
		if len(r[1]) > 15 {
			t.Errorf("range %q is longer than tmux keeps", r[1])
		}
	}
	if TabView(TabRange("0123456789abcdef01234567")) != "0123456789ab" || !strings.Contains(format, "##odd,name") {
		t.Fatal("tab ranges and escaping")
	}
}

// The click slots of HeaderButtons must match the glyphs at the right of
// the header: " ─  □  ▼ ".
func TestHeaderButtonSlotsMatchTheHeader(t *testing.T) {
	tail := " " + theme.Glyphs.Minimize + "  " + theme.Glyphs.Maximize + "  " + theme.Glyphs.Menu + " "
	// As a UTF-8 client draws it.
	h := regexp.MustCompile(`#\{\?client_utf8,([^,]*),[^}]*\}`).ReplaceAllString(headerFormat(), "$1")
	if !strings.HasSuffix(h, tail) {
		t.Fatalf("header tail changed: %q", h[len(h)-40:])
	}
	cells := []rune(tail)
	if runewidth.StringWidth(tail) != len(cells) {
		t.Fatal("glyphs must be one column wide")
	}
	glyph := map[string]string{"menu": theme.Glyphs.Menu, "maximize": theme.Glyphs.Maximize, "minimize": theme.Glyphs.Minimize}
	for _, b := range HeaderButtons {
		found := false
		for rel := b.Lo; rel <= b.Hi; rel++ {
			if string(cells[len(cells)-1-rel]) == glyph[b.Action] {
				found = true
			}
		}
		if !found {
			t.Errorf("%s slot %d-%d misses its glyph", b.Action, b.Lo, b.Hi)
		}
	}
}
