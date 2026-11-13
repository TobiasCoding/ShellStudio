// SPDX-License-Identifier: GPL-3.0-only

// Package theme is the palette shared by the cell renderer (internal/tui) and
// the tmux formats (internal/mux). Colors are indexes of the 256-color
// palette: no truecolor, so Mosh and older terminals render them the same.
package theme

import "strconv"

const (
	Black = 16 // RGB black; ANSI colour 0 can be grey in some terminals.
	White = 231

	Accent     = 33  // Active console header, current view tab, focus.
	AccentDark = 25  // Buttons and pressed states.
	Selection  = 24  // Selected row in lists and tables.
	HeaderFg   = 250 // Inactive console header.
	HeaderBg   = 235
	Text       = 252
	Muted      = 244 // Secondary text: descriptions, hints, dates.
	Border     = 240 // Inactive pane borders.
	Panel      = 234 // Fields and table header background.
	Green      = 71
	Amber      = 179
	Red        = 167
)

// TagPalette is the fixed set of colors a tag can take.
var TagPalette = [8]int{179, 33, 245, 71, 167, 135, 37, 209}

// TagColor returns a palette color; any other value falls back to grey.
func TagColor(c int) int {
	for _, p := range TagPalette {
		if p == c {
			return c
		}
	}
	return 245
}

// TagText is the text color that reads on a tag's background.
func TagText(bg int) int {
	switch bg {
	case 33, 167, 135, 37:
		return White
	}
	return Black
}

// Colour names a palette entry for tmux styles: colour33.
func Colour(n int) string { return "colour" + strconv.Itoa(n) }

// Glyph set for buttons and markers. Each glyph is one column wide.
type GlyphSet struct{ Minimize, Maximize, Unzoom, Menu, Dot, More, SortDown, SortUp, Grip, Open, Closed string }

var (
	// Glyphs are drawn by every client ShellStudio attaches (always UTF-8).
	Glyphs = GlyphSet{"─", "□", "❐", "▼", "●", "⋯", "↓", "↑", "::::", "▼", "▶"}
	// ASCII replaces them for a client attached without UTF-8 mode.
	ASCII = GlyphSet{"_", "O", "o", "v", "*", "...", "v", "^", "::::", "v", ">"}
)

// TmuxGlyph chooses the glyph by the client's UTF-8 mode inside a tmux format.
func TmuxGlyph(g, ascii string) string { return "#{?client_utf8," + g + "," + ascii + "}" }
