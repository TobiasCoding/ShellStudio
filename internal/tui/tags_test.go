// SPDX-License-Identifier: GPL-3.0-only
package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
	"shellstudio/internal/app"
	"shellstudio/internal/i18n"
	"shellstudio/internal/mux"
	"shellstudio/internal/platform"
	"shellstudio/internal/store"
	"shellstudio/internal/theme"
)

// tagsApp has a real store and a tmux that is not running, like the
// offlineApp of the app tests. It has one console, "c1", in a view.
func tagsApp(t *testing.T) *app.App {
	t.Helper()
	prev := i18n.Lang()
	i18n.SetLang("en")
	t.Cleanup(func() { i18n.SetLang(prev) })
	s, e := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	p := platform.Paths{Config: t.TempDir(), Data: t.TempDir(), State: t.TempDir(), Runtime: t.TempDir()}
	a := &app.App{Store: s, Paths: p, CWD: t.TempDir()}
	a.Mux = &mux.Mux{Paths: p, Run: func(views bool, args ...string) (string, error) {
		return "", errors.New("no server running on /x")
	}}
	v, e := s.NewView("Main", "/")
	if e != nil {
		t.Fatal(e)
	}
	c := store.Console{ID: "c1", Name: "api", Extension: "terminal", Folder: "/", Argv: []string{"/bin/sh"}, Env: map[string]string{}}
	if e := s.AddConsole(v.ID, c); e != nil {
		t.Fatal(e)
	}
	return a
}

func typed(text string) []Event {
	var out []Event
	for _, r := range text {
		out = append(out, key(string(r)))
	}
	return out
}

func events(groups ...any) []Event {
	var out []Event
	for _, g := range groups {
		switch v := g.(type) {
		case Event:
			out = append(out, v)
		case []Event:
			out = append(out, v...)
		case string:
			out = append(out, key(v))
		}
	}
	return out
}

// clickAt is a full left click: press and release.
func clickAt(y, x int) []Event { return []Event{click(y, x), release(y, x)} }

// find returns the row and column of the first cell of text in the frame.
func find(t *testing.T, s *Screen, text string) (int, int) {
	t.Helper()
	for y, row := range s.Frame() {
		if i := strings.Index(row, text); i >= 0 {
			return y, runewidth.StringWidth(row[:i])
		}
	}
	t.Fatalf("%q not in frame:\n%s", text, frameText(s))
	return 0, 0
}

func tagNames(t *testing.T, a *app.App) []string {
	t.Helper()
	tags, e := a.Tags()
	if e != nil {
		t.Fatal(e)
	}
	var out []string
	for _, tg := range tags {
		out = append(out, tg.Name)
	}
	return out
}

func tagByName(t *testing.T, a *app.App, name string) store.Tag {
	t.Helper()
	tags, _ := a.Tags()
	for _, tg := range tags {
		if tg.Name == name {
			return tg
		}
	}
	t.Fatalf("no tag %q in %v", name, tagNames(t, a))
	return store.Tag{}
}

func TestTagsDialogLifecycle(t *testing.T) {
	a := tagsApp(t)
	// Create (6 picks the sixth color), rename, recolor and move up.
	s := newTestScreen(72, 16, events(
		"n", typed("Build"), "enter", "6", "enter",
		"r", "ctrl+u", typed("Deploy"), "enter",
		"c", "right", "enter",
		"[",
		"esc")...)
	runTest(t, s, func() { TagsDialog(s, a) })
	if got := strings.Join(tagNames(t, a), ","); got != "Por revisar,En curso,Pendiente,Deploy,Listo" {
		t.Fatal(got)
	}
	d := tagByName(t, a, "Deploy")
	if d.Color != theme.TagPalette[6] {
		t.Fatal(d.Color)
	}
	frame := frameText(s)
	for _, name := range []string{"TAGS", "Por revisar", "Deploy", "Listo", "0 consoles", "N new", "Del delete"} {
		if !strings.Contains(frame, name) {
			t.Fatalf("%q missing:\n%s", name, frame)
		}
	}
	// The selection followed the moved tag.
	y, x := find(t, s, "Deploy")
	if !strings.HasPrefix(strings.TrimSpace(s.Frame()[y]), ">") || s.StyleAt(y, 1) != Highlight {
		t.Fatalf("selection did not follow the tag:\n%s", frame)
	}
	// The name is drawn as a badge on its color.
	if st := s.StyleAt(y, x); st.Bg != uint8(theme.TagPalette[6]) || st.Fg != uint8(theme.TagText(theme.TagPalette[6])) {
		t.Fatalf("badge style %+v", st)
	}

	// Delete asks first, with the name and how many consoles use it.
	if e := a.SetTag("c1", d.ID); e != nil {
		t.Fatal(e)
	}
	s = newTestScreen(72, 16, events("down", "down", "down", "delete")...)
	runTest(t, s, func() { TagsDialog(s, a) })
	if !strings.Contains(frameText(s), "Delete the tag Deploy? 1 consoles") {
		t.Fatal(frameText(s))
	}
	s = newTestScreen(72, 16, events("down", "down", "down", "delete", "esc", "esc")...)
	runTest(t, s, func() { TagsDialog(s, a) })
	if len(tagNames(t, a)) != 5 {
		t.Fatal("cancelled delete removed the tag")
	}
	s = newTestScreen(72, 16, events("down", "down", "down", "delete", "enter", "esc")...)
	runTest(t, s, func() { TagsDialog(s, a) })
	if got := strings.Join(tagNames(t, a), ","); got != "Por revisar,En curso,Pendiente,Listo" {
		t.Fatal(got)
	}
	if c, _ := a.Store.Console("c1"); c.TagID != "" {
		t.Fatalf("console still tagged %q", c.TagID)
	}
	if strings.Contains(frameText(s), "Deploy") {
		t.Fatal(frameText(s))
	}
}

func TestTagsDialogShowsErrorsAndCancels(t *testing.T) {
	a := tagsApp(t)
	// A duplicate name is refused before the color is chosen.
	s := newTestScreen(72, 16, events("n", typed("listo"), "enter")...)
	runTest(t, s, func() { TagsDialog(s, a) })
	if !strings.Contains(frameText(s), "a tag listo already exists") {
		t.Fatal(frameText(s))
	}
	// Renaming to an empty name shows App's error and keeps the tag.
	s = newTestScreen(72, 16, events("r", "ctrl+u", "enter")...)
	runTest(t, s, func() { TagsDialog(s, a) })
	if !strings.Contains(frameText(s), "use 1–24 characters") || tagNames(t, a)[0] != "Por revisar" {
		t.Fatal(frameText(s))
	}
	// Cancelling the color creates nothing; moving the first tag up does nothing.
	s = newTestScreen(72, 16, events("n", typed("Nope"), "enter", "esc", "[", "c", "esc", "esc")...)
	runTest(t, s, func() { TagsDialog(s, a) })
	if got := strings.Join(tagNames(t, a), ","); got != "Por revisar,En curso,Pendiente,Listo" {
		t.Fatal(got)
	}
}

func TestTagsDialogEmptyAndSmall(t *testing.T) {
	a := tagsApp(t)
	tags, _ := a.Tags()
	for _, tg := range tags {
		if e := a.DeleteTag(tg.ID); e != nil {
			t.Fatal(e)
		}
	}
	s := newTestScreen(72, 16, events("r", "c", "delete", "[", "down")...)
	runTest(t, s, func() { TagsDialog(s, a) })
	if !strings.Contains(frameText(s), "No tags. N creates one.") {
		t.Fatal(frameText(s))
	}
	s = newTestScreen(20, 5)
	runTest(t, s, func() { TagsDialog(s, a) })
	if !strings.Contains(frameText(s), "Enlarge") {
		t.Fatal(frameText(s))
	}
}

func TestTagsDialogMouse(t *testing.T) {
	a := tagsApp(t)
	probe := newTestScreen(72, 16)
	runTest(t, probe, func() { TagsDialog(probe, a) })
	ry, rx := find(t, probe, "En curso")
	dy, dx := find(t, probe, "] down")
	cy, cx := find(t, probe, "C color")
	ey, ex := find(t, probe, "Esc close")
	// Click selects the row, a second click renames it; the hints are keys.
	// In the color picker, a click on a swatch chooses it (y=2, 7 columns each).
	s := newTestScreen(72, 16, events(
		clickAt(ry, rx), clickAt(ry, rx), "ctrl+u", typed("Doing"), "enter",
		clickAt(dy, dx+1),
		clickAt(cy, cx), clickAt(2, 1+7*7+3),
		Event{Kind: MouseEvent, Button: 64, Press: true},
		clickAt(ey, ex))...)
	runTest(t, s, func() { TagsDialog(s, a) })
	if got := strings.Join(tagNames(t, a), ","); got != "Por revisar,Pendiente,Doing,Listo" {
		t.Fatal(got)
	}
	if c := tagByName(t, a, "Doing").Color; c != theme.TagPalette[7] {
		t.Fatal(c)
	}
	// The wheel moved the selection up, to Pendiente.
	y, _ := find(t, s, "Pendiente")
	if s.StyleAt(y, 1) != Highlight {
		t.Fatal(frameText(s))
	}
}

func TestPickColor(t *testing.T) {
	prev := i18n.Lang()
	i18n.SetLang("en")
	defer i18n.SetLang(prev)
	var c int
	var ok bool
	s := newTestScreen(72, 12, events("left", "left", "enter")...)
	runTest(t, s, func() { c, ok = pickColor(s, "Tag color", "Build", theme.TagPalette[1]) })
	if !ok || c != theme.TagPalette[7] {
		t.Fatal(c, ok)
	}
	s = newTestScreen(72, 12, events("3")...)
	runTest(t, s, func() { pickColor(s, "Tag color", "Build", theme.TagPalette[0]) })
	if y, x := find(t, s, "Build"); s.StyleAt(y, x).Bg != uint8(theme.TagPalette[2]) {
		t.Fatal("preview does not follow the selection")
	}
	if !strings.Contains(frameText(s), "[  3  ]") {
		t.Fatal(frameText(s))
	}
	s = newTestScreen(72, 12, events("esc")...)
	runTest(t, s, func() { c, ok = pickColor(s, "Tag color", "", theme.TagPalette[4]) })
	if ok || c != theme.TagPalette[4] {
		t.Fatal(c, ok)
	}
	// Narrow: swatches wrap and down moves a row.
	s = newTestScreen(32, 12, events("down", "enter")...)
	runTest(t, s, func() { c, ok = pickColor(s, "Tag color", "", theme.TagPalette[0]) })
	if !ok || c != theme.TagPalette[4] {
		t.Fatal(c, ok)
	}
}

func TestChooseTag(t *testing.T) {
	a := tagsApp(t)
	run := func(evs ...any) (*Screen, error) {
		s := newTestScreen(72, 16, events(evs...)...)
		var err error
		runTest(t, s, func() { err = ChooseTag(s, a, "c1") })
		return s, err
	}
	tagOf := func() string {
		c, e := a.Store.Console("c1")
		if e != nil {
			t.Fatal(e)
		}
		return c.TagID
	}
	// Untagged: "No tag" is selected and checked.
	s, _ := run()
	if y, _ := find(t, s, "No tag"); s.StyleAt(y, 1) != Highlight || !strings.Contains(s.Frame()[y], "✓") {
		t.Fatal(frameText(s))
	}
	if !strings.Contains(frameText(s), "TAG OF api") || !strings.Contains(frameText(s), "Manage…") {
		t.Fatal(frameText(s))
	}
	if _, e := run("home", "down", "enter"); e != nil || tagOf() != tagByName(t, a, "En curso").ID {
		t.Fatal(e, tagOf())
	}
	// The current tag starts selected; "No tag" (end, up) removes it.
	if _, e := run("end", "up", "enter"); e != nil || tagOf() != "" {
		t.Fatal(e, tagOf())
	}
	// Manage opens the tag manager; its new tag is listed on return.
	s, e := run("end", "enter", "n", typed("Ops"), "enter", "enter", "esc")
	if e != nil || !strings.Contains(frameText(s), "Ops") || !strings.Contains(frameText(s), "TAG OF api") {
		t.Fatal(e, frameText(s))
	}
	// Back from Manage, the console's tag is selected again: Enter keeps it.
	listo := tagByName(t, a, "Listo").ID
	if e := a.SetTag("c1", listo); e != nil {
		t.Fatal(e)
	}
	if _, e := run("end", "enter", "n", typed("Dev"), "enter", "enter", "esc", "enter"); e != nil || tagOf() != listo {
		t.Fatal(e, tagOf())
	}
	// Mouse: a click selects a row and another click chooses it.
	probe, _ := run()
	y, x := find(t, probe, "Listo")
	if _, e := run(clickAt(y, x), clickAt(y, x)); e != nil || tagOf() != tagByName(t, a, "Listo").ID {
		t.Fatal(e, tagOf())
	}
	// Esc changes nothing.
	if _, e := run("home", "esc"); e != nil || tagOf() != tagByName(t, a, "Listo").ID {
		t.Fatal(e, tagOf())
	}
	if e := ChooseTag(newTestScreen(72, 16), a, "missing"); e == nil {
		t.Fatal("chose a tag for a missing console")
	}
}

func TestDescribeDialog(t *testing.T) {
	a := tagsApp(t)
	desc := func() string {
		c, _ := a.Store.Console("c1")
		return c.Description
	}
	var err error
	s := newTestScreen(72, 12, events(typed("runs the API"), "enter")...)
	runTest(t, s, func() { err = DescribeDialog(s, a, "c1") })
	if err != nil || desc() != "runs the API" {
		t.Fatal(err, desc())
	}
	s = newTestScreen(72, 12, events(typed(" more"))...)
	runTest(t, s, func() { DescribeDialog(s, a, "c1") })
	if f := frameText(s); !strings.Contains(f, "DESCRIPTION | api") || !strings.Contains(f, "runs the API more") {
		t.Fatal(f)
	}
	s = newTestScreen(72, 12, events("ctrl+u", typed("other"), "esc")...)
	runTest(t, s, func() { err = DescribeDialog(s, a, "c1") })
	if err != nil || desc() != "runs the API" {
		t.Fatal(err, desc())
	}
	// The prompt stops at 120 characters.
	s = newTestScreen(72, 12, events("ctrl+u", Event{Kind: PasteEvent, Text: strings.Repeat("x", 130)}, "enter")...)
	runTest(t, s, func() { err = DescribeDialog(s, a, "c1") })
	if err != nil || desc() != strings.Repeat("x", 120) {
		t.Fatal(err, len(desc()))
	}
	// An empty answer clears it.
	s = newTestScreen(72, 12, events("ctrl+u", "enter")...)
	runTest(t, s, func() { err = DescribeDialog(s, a, "c1") })
	if err != nil || desc() != "" {
		t.Fatal(err, desc())
	}
	if e := DescribeDialog(newTestScreen(72, 12), a, "missing"); e == nil {
		t.Fatal("described a missing console")
	}
}
