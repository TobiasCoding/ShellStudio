// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"shellstudio/internal/i18n"
	"shellstudio/internal/mux"
	"shellstudio/internal/platform"
	"shellstudio/internal/store"
)

// offlineApp has a store and a tmux that is not running; it records commands.
func offlineApp(t *testing.T) (*App, *[][]string) {
	t.Helper()
	s, e := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	var log [][]string
	p := platform.Paths{Config: t.TempDir(), Data: t.TempDir(), Runtime: t.TempDir()}
	a := &App{Store: s, Paths: p, CWD: t.TempDir()}
	a.Mux = &mux.Mux{Paths: p, Run: func(views bool, args ...string) (string, error) {
		log = append(log, args)
		return "", errors.New("no server running on /x")
	}}
	a.Mux.HeaderOptions = a.headerOptions
	return a, &log
}

func addConsoles(t *testing.T, a *App, view string, ids ...string) {
	t.Helper()
	for _, id := range ids {
		c := store.Console{ID: id, Name: id, Extension: "terminal", Folder: "/", Argv: []string{"/bin/sh"}, Env: map[string]string{}}
		if e := a.Store.AddConsole(view, c); e != nil {
			t.Fatal(e)
		}
	}
}

func TestMinimizeAndRestoreKeepOrderAndMembership(t *testing.T) {
	a, log := offlineApp(t)
	v, _ := a.Store.NewView("Main", "/")
	addConsoles(t, a, v.ID, "a", "b", "c")
	if e := a.Minimize(v.ID, "b"); e != nil {
		t.Fatal(e)
	}
	vis, _ := a.Store.Consoles(v.ID)
	min, _ := a.Store.Minimized(v.ID)
	if len(vis) != 2 || len(min) != 1 || min[0].ID != "b" {
		t.Fatal(vis, min)
	}
	bumped := false
	for _, c := range *log {
		if len(c) >= 3 && c[0] == "set-option" && c[2] == "@ss_minimized_rev" {
			bumped = true
		}
	}
	if !bumped {
		t.Fatal("minimize did not notify the panels")
	}
	// Adding the console to the same view restores it.
	if e := a.AddToView(v.ID, "b"); e != nil {
		t.Fatal(e)
	}
	vis, _ = a.Store.Consoles(v.ID)
	if len(vis) != 3 || vis[1].ID != "b" {
		t.Fatal(vis)
	}
	if e := a.Minimize(v.ID, "nope"); e == nil {
		t.Fatal("minimized a non-member")
	}
	// Closing a panel unlinks: it does not appear among the minimized.
	if e := a.RemoveFromView(v.ID, "c"); e != nil {
		t.Fatal(e)
	}
	if min, _ = a.Store.Minimized(v.ID); len(min) != 0 {
		t.Fatal(min)
	}
	// Kill deletes the console, minimized or not.
	a.Minimize(v.ID, "a")
	if e := a.Kill("a"); e != nil {
		t.Fatal(e)
	}
	if min, _ = a.Store.Minimized(v.ID); len(min) != 0 {
		t.Fatal(min)
	}
}

func TestTagsValidateAndFeedTheHeader(t *testing.T) {
	defer i18n.SetLang(i18n.Lang())
	i18n.SetLang("en")
	a, _ := offlineApp(t)
	v, _ := a.Store.NewView("Main", "/")
	addConsoles(t, a, v.ID, "a")
	if _, e := a.AddTag("bad#name", 33); e == nil {
		t.Fatal("accepted #")
	}
	if _, e := a.AddTag("Ok", 1); e == nil {
		t.Fatal("accepted a color outside the palette")
	}
	seeded, _ := a.Tags()
	if _, e := a.AddTag(strings.ToUpper(seeded[0].Name), 33); e == nil {
		t.Fatal("accepted a duplicate name")
	}
	tag, e := a.AddTag("Deploy", 167)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.SetTag("a", tag.ID); e != nil {
		t.Fatal(e)
	}
	c, _ := a.Store.Console("a")
	opts := a.headerOptions(c)
	if opts["@ss_label"] != "Terminal · a" || opts["@ss_tag"] != "Deploy" || opts["@ss_tag_color"] != "colour167" || opts["@ss_tag_fg"] != "colour231" {
		t.Fatal(opts)
	}
	if e = a.MoveTag(tag.ID, -1); e != nil {
		t.Fatal(e)
	}
	tags, _ := a.Tags()
	if tags[len(tags)-2].ID != tag.ID {
		t.Fatal("move up", tags)
	}
	if e = a.DeleteTag(tag.ID); e != nil {
		t.Fatal(e)
	}
	c, _ = a.Store.Console("a")
	if c.TagID != "" || a.headerOptions(c)["@ss_tag"] != "" {
		t.Fatal(c)
	}
	if e = a.SetTag("a", "gone"); e == nil {
		t.Fatal("set a missing tag")
	}
	if e = a.SetDescription("a", strings.Repeat("x", 121)); e == nil {
		t.Fatal("long description")
	}
	if e = a.SetDescription("a", " runs the API "); e != nil {
		t.Fatal(e)
	}
	c, _ = a.Store.Console("a")
	if c.Description != "runs the API" {
		t.Fatal(c.Description)
	}
	cmds := a.Mux.HeaderCommands(c)
	if cmds[0][3] != "@ss_name" || len(cmds) != 5 {
		t.Fatal(cmds)
	}
}

func TestKindLabelFallsBackToID(t *testing.T) {
	a, _ := offlineApp(t)
	if a.KindLabel("terminal") != "Terminal" || a.KindLabel("gone-ext") != "gone-ext" {
		t.Fatal(a.KindLabel("gone-ext"))
	}
}

func TestTabsAndLanguage(t *testing.T) {
	defer i18n.SetLang(i18n.Lang())
	t.Setenv("LC_ALL", "C")
	i18n.SetLang("en")
	a, _ := offlineApp(t)
	v, _ := a.Store.NewView("Zeta", "/")
	w, _ := a.Store.NewView("Alpha", "/")
	f := a.TabsFormat()
	if strings.Index(f, " Alpha ") > strings.Index(f, " Zeta ") || !strings.Contains(f, mux.TabRange(v.ID)) {
		t.Fatal(f)
	}
	got, e := a.ViewByTab(mux.TabRange(w.ID))
	if e != nil || got.ID != w.ID {
		t.Fatal(got, e)
	}
	if _, e = a.ViewByTab("vistas"); e == nil {
		t.Fatal("resolved a non-tab range")
	}
	before := a.configVersion()
	a.SetLanguage("es")
	if a.Language() != "es" || i18n.Lang() != "es" || a.configVersion() == before {
		t.Fatal(a.Language(), i18n.Lang())
	}
	a.SetLanguage("whatever")
	if a.Language() != "auto" {
		t.Fatal(a.Language())
	}
}
