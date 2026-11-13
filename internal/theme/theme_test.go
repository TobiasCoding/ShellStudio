// SPDX-License-Identifier: GPL-3.0-only
package theme

import (
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestGlyphsAreOneColumn(t *testing.T) {
	for _, g := range []string{Glyphs.Minimize, Glyphs.Maximize, Glyphs.Unzoom, Glyphs.Menu, Glyphs.Dot, Glyphs.More, Glyphs.SortDown, Glyphs.SortUp, Glyphs.Open, Glyphs.Closed} {
		if w := runewidth.StringWidth(g); w != 1 {
			t.Errorf("%q is %d columns", g, w)
		}
	}
}

func TestTagColorsStayInPalette(t *testing.T) {
	if TagColor(TagPalette[3]) != TagPalette[3] || TagColor(-4) != 245 {
		t.Fatal("tag color fallback")
	}
	if TmuxGlyph("▼", "v") != "#{?client_utf8,▼,v}" {
		t.Fatal(TmuxGlyph("▼", "v"))
	}
}

func TestHeaderButtonFallbacksAreOneColumn(t *testing.T) {
	for _, g := range []string{ASCII.Minimize, ASCII.Maximize, ASCII.Unzoom, ASCII.Menu} {
		if runewidth.StringWidth(g) != 1 {
			t.Errorf("%q breaks the fixed header slots", g)
		}
	}
}
