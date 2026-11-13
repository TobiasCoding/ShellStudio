// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"strings"
	"testing"

	"shellstudio/internal/i18n"
)

func TestKeyLabel(t *testing.T) {
	i18n.SetLang("en")
	for key, want := range map[string]string{"M-t": "Alt+T", "M-T": "Alt+Shift+T", "M-7": "Alt+7", "F12": "F12",
		"S-F5": "Shift+F5", "C-M-S-F3": "Ctrl+Alt+Shift+F3", "": ""} {
		if got := KeyLabel(key); got != want {
			t.Errorf("KeyLabel(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestTagShortcutsAreValidatedAndBound(t *testing.T) {
	i18n.SetLang("en")
	a, _ := offlineApp(t)
	a.Mux.Binary = "/bin/shellstudio"
	var log [][]string
	a.Mux.Run = func(views bool, args ...string) (string, error) {
		log = append(log, args)
		return "", nil
	}
	tags, _ := a.Store.Tags()
	if len(tags) < 2 {
		t.Fatal(tags)
	}
	for _, bad := range []string{"a", "Enter", "C-a", "M-<", "F1", "F2", "M-1", "S-F8"} {
		if e := a.SetTagShortcut(tags[0].ID, bad); e == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if e := a.SetTagShortcut(tags[0].ID, "M-t"); e != nil {
		t.Fatal(e)
	}
	if e := a.SetTagShortcut(tags[1].ID, "M-t"); e == nil || !strings.Contains(e.Error(), tags[0].Name) {
		t.Fatalf("duplicate: %v", e)
	}
	got, _ := a.Store.Tag(tags[0].ID)
	if got.Shortcut != "M-t" {
		t.Fatal(got)
	}
	bound := false
	for _, c := range log {
		if len(c) > 5 && c[0] == "bind-key" && c[2] == "M-t" && strings.Contains(strings.Join(c, " "), "tagkey --target "+tags[0].ID) {
			bound = true
		}
	}
	if !bound {
		t.Fatalf("not bound: %q", log)
	}
	cmds := a.Mux.TagKeyCommands([]string{"M-t", "F1"}, map[string]string{"F12": "x"})
	text := ""
	for _, c := range cmds {
		text += strings.Join(c, " ") + "\n"
	}
	// The old key is unbound, a reserved one never is, and the new one is bound.
	if !strings.Contains(text, "unbind-key -q -n M-t") || strings.Contains(text, "-n F1 ") ||
		!strings.Contains(text, "bind-key -n F12") || !strings.Contains(text, "@ss_tag_keys F12") {
		t.Fatal(text)
	}
}

func TestToggleTag(t *testing.T) {
	a, _ := offlineApp(t)
	v, _ := a.Store.NewView("Main", "/")
	addConsoles(t, a, v.ID, "a")
	tags, _ := a.Store.Tags()
	if e := a.ToggleTag("a", tags[0].ID); e != nil {
		t.Fatal(e)
	}
	if c, _ := a.Store.Console("a"); c.TagID != tags[0].ID {
		t.Fatal(c)
	}
	a.ToggleTag("a", tags[0].ID)
	if c, _ := a.Store.Console("a"); c.TagID != "" {
		t.Fatal(c)
	}
}
