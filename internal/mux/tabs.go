// SPDX-License-Identifier: GPL-3.0-only
package mux

import (
	"strings"

	"shellstudio/internal/store"
	"shellstudio/internal/theme"
)

// TopBarRows is the height of the top bar: the status line of the views
// server, above every view window.
const TopBarRows = 1

// Status-line ranges of the top bar. tmux keeps at most 15 characters of a
// user range (docs/redesign-spike.md), so a tab carries "t" and the first
// 12 hex characters of its view ID.
const (
	RangeViews   = "vistas"
	RangeNew     = "new"
	RangeArrange = "arrange"
	RangeMore    = "more"
)

// TabRange is the user range of a view's tab.
func TabRange(viewID string) string {
	if len(viewID) > 12 {
		viewID = viewID[:12]
	}
	return "t" + viewID
}

// TabView extracts the view ID prefix of a tab range ("" if it is not one).
func TabView(r string) string {
	if len(r) > 1 && r[0] == 't' {
		return r[1:]
	}
	return ""
}

// FormatText escapes text for a tmux format: # starts a format sequence.
func FormatText(s string) string { return strings.ReplaceAll(s, "#", "##") }

// TabsFormat draws one clickable tab per view; the tab of the session being
// drawn has the accent background. maxName shortens names (0 keeps them).
func TabsFormat(views []store.View, maxName int) string {
	var b strings.Builder
	for _, v := range views {
		name := v.Name
		if r := []rune(name); maxName > 0 && len(r) > maxName {
			name = string(r[:max(1, maxName-1)]) + "…"
		}
		b.WriteString("#[range=user|" + TabRange(v.ID) + "]")
		b.WriteString("#{?#{==:#{session_name}," + ViewSession(v.ID) + "},")
		b.WriteString("#[fg=" + theme.Colour(theme.White) + "#,bg=" + theme.Colour(theme.Accent) + "#,bold],")
		b.WriteString("#[fg=" + theme.Colour(theme.HeaderFg) + "#,bg=" + theme.Colour(theme.HeaderBg) + "]}")
		b.WriteString(" " + FormatText(name) + " ")
		b.WriteString("#[default]#[norange] ")
	}
	return b.String()
}
