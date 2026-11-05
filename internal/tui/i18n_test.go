// SPDX-License-Identifier: GPL-3.0-only
package tui

import (
	"strings"
	"testing"

	"shellstudio/internal/app"
	"shellstudio/internal/i18n"
	"shellstudio/internal/mux"
)

// helpLines are every shortcut list whose lines HintKey makes clickable, in
// the current language.
func helpLines(a *app.App) map[string][]string {
	keys := func(ks ...string) []string {
		out := make([]string, len(ks))
		for i, k := range ks {
			out[i] = i18n.T(k)
		}
		return out
	}
	var tree []string
	for _, sc := range treeShortcuts() {
		tree = append(tree, sc.label)
	}
	return map[string][]string{
		"arrange":     app.ArrangeHelp(),
		"browse":      app.BrowseHelp(),
		"new":         app.NewHelp(),
		"add":         app.AddHelp(),
		"permissions": app.PermissionsHelp(),
		"mode":        app.PermissionModeHelp(),
		"kind":        app.ChooseKindHelp(),
		"consoles":    a.ConsolesHelp(),
		"tree":        tree,
		"disconnected": keys("ui.disc.reconnect", "ui.disc.start", "ui.disc.close", "ui.disc.kill",
			"ui.disc.new", "ui.disc.menu"),
		"empty":   keys("ui.empty.create", "ui.empty.menu", "ui.empty.switch", "ui.empty.leave"),
		"widgets": keys("ui.confirm.ok", "ui.confirm.cancel", "ui.prompt.accept", "ui.message.keys", "ui.choose.more"),
	}
}

func setLang(t *testing.T, l string) {
	t.Helper()
	prev := i18n.Lang()
	i18n.SetLang(l)
	t.Cleanup(func() { i18n.SetLang(prev) })
}

// A translated shortcut line keeps its key first: a click on it must do
// what the English line does, and no explanatory line may start with a
// one-letter word that HintKey would take for a key.
func TestSpanishShortcutsKeepTheirKeys(t *testing.T) {
	a := tagsApp(t)
	en := helpLines(a)
	setLang(t, "es")
	es := helpLines(a)
	for name, lines := range en {
		if len(es[name]) != len(lines) {
			t.Fatalf("%s: %d lines in Spanish, %d in English", name, len(es[name]), len(lines))
		}
		translated := 0
		for i, l := range lines {
			if HintKey(es[name][i]) != HintKey(l) {
				t.Errorf("%s: %q gives key %q, %q gives %q", name, es[name][i], HintKey(es[name][i]), l, HintKey(l))
			}
			if es[name][i] != l {
				translated++
			}
		}
		if translated == 0 {
			t.Errorf("%s: nothing translated: %q", name, es[name])
		}
	}
	for _, c := range []struct{ line, key string }{{"N  Renombrar", "n"}, {"Space  Marcar / desmarcar", " "},
		{"Del  Borrar vista", "delete"}, {"Q / Esc  Volver", "q"}, {"M/Del  Matar consola", "m"},
		{"Enter  Ir a la consola seleccionada", "enter"}, {"Selección parcial: crea otra vista.", ""}} {
		if HintKey(c.line) != c.key {
			t.Errorf("HintKey(%q) = %q, want %q", c.line, HintKey(c.line), c.key)
		}
	}
}

func TestSpanishMenusAndDialogs(t *testing.T) {
	a := tagsApp(t)
	setLang(t, "es")
	vs, _ := a.Store.Views()
	v := vs[0]
	v.Explorer = true
	cols := a.MenuColumns(v)
	var titles, items []string
	for _, c := range cols {
		titles = append(titles, c.Title)
		for _, it := range c.Items {
			items = append(items, it.Key+"  "+it.Label)
		}
	}
	if strings.Join(titles, ",") != "CONSOLAS,DISTRIBUCIÓN,VISTAS,MÁS" {
		t.Fatalf("titles %q", titles)
	}
	all := strings.Join(items, "\n")
	for _, want := range []string{"A  Agregar existente…", "1  Columnas", "3  Cuadrícula", "4  Activa a la izquierda",
		"L  Distribuir marcadas…", "V  Nueva vista…", "E  Explorador: sí", "D  Salir sin detener (F10)", "Q  Volver (Esc)"} {
		if !strings.Contains(all, want) {
			t.Errorf("menu lacks %q:\n%s", want, all)
		}
	}
	// The layout names in mux.LayoutKeys stay English: they are data.
	if mux.LayoutKeys[0].Label != "Columns" || mux.LayoutLabel("even-horizontal") != "Columnas" || mux.LayoutLabel("bogus") != "Cuadrícula" {
		t.Fatal(mux.LayoutLabel("even-horizontal"))
	}

	s := newTestScreen(120, 30)
	runTest(t, s, func() { ViewMenu(s, a, v.ID, "c1", "", "") })
	if f := frameText(s); !strings.Contains(f, "VISTA Main | ") || !strings.Contains(f, "Clic o tecla · flechas y Enter") {
		t.Fatalf("view menu:\n%s", f)
	}

	// A click on a translated shortcut line still acts: N opens the new view prompt.
	s = newTestScreen(80, 24)
	runTest(t, s, func() { Browse(s, a) })
	y, x := rowOf(t, s, "N  Nueva vista")
	s = newTestScreen(80, 24, click(y, x), release(y, x))
	runTest(t, s, func() { Browse(s, a) })
	if f := frameText(s); !strings.Contains(f, "Nombre de la vista nueva") || !strings.Contains(f, "Enter  Aceptar") {
		t.Fatalf("click on N did not open the prompt:\n%s", f)
	}

	s = newTestScreen(100, 30)
	runTest(t, s, func() { EditView(s, a, v.ID, "c1") })
	if f := frameText(s); !strings.Contains(f, "CONSOLAS DE Main") || !strings.Contains(f, "api | detenida") || !strings.Contains(f, "1/1 | ↑↓ elegir") {
		t.Fatalf("consoles of the view:\n%s", f)
	}

	// The popup sizes come from the translated text.
	w, _ := a.DialogSize("views", v.ID)
	if want, _ := app.ChooseSize("VISTAS GUARDADAS", []string{"Main | 10 consolas | Activa a la izquierda"}, app.BrowseHelp()); w != want {
		t.Fatalf("views width %d, want %d", w, want)
	}

	m := &mux.Mux{Binary: "/bin/shellstudio", Tmux: "tmux", Version: "v"}
	var programs []string
	for _, c := range m.ProgramsControls() {
		programs = append(programs, strings.Join(c, " "))
	}
	p := strings.Join(programs, "\n")
	for _, want := range []string{"TERMINADA · Enter: opciones", "(señal #{pane_dead_signal})", "Reiniciar", "Matar consola"} {
		if !strings.Contains(p, want) {
			t.Errorf("programs controls lack %q", want)
		}
	}
}

// The integration tests run with LANG=C and match these English strings.
func TestEnglishTextMatchedByIntegrationTests(t *testing.T) {
	a := tagsApp(t)
	vs, _ := a.Store.Views()
	v := vs[0]
	var tree []string
	for _, sc := range treeShortcuts() {
		tree = append(tree, sc.label)
	}
	if strings.Join(tree, "|") != "F2 Menu|F3 New|F5 Arrange|F9 Kill|F7 Views|F10 Leave" {
		t.Fatalf("tree shortcuts %q", tree)
	}
	for key, want := range map[string]string{
		"ui.empty.title":  "This view is empty",
		"ui.empty.create": "F3  Create a console here",
		"ui.empty.name":   "Empty view",
		"ui.disc.close":   "Q      Close panel",
	} {
		if got := i18n.T(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if got := i18n.T("ui.disc.stopped", "api"); got != "api: the console is stopped." {
		t.Errorf("%q", got)
	}
	if e := a.ApplyLayout(v.ID, "bogus", ""); e == nil || e.Error() != "invalid layout" {
		t.Errorf("layout error %v", e)
	}
	if e := app.ValidName("bad#name"); e == nil || !strings.Contains(e.Error(), "cannot contain") {
		t.Errorf("name error %v", e)
	}
	if strings.Join(app.ArrangeHelp(), "|") != "Space  Mark / unmark|A  Mark all|0  Unmark all|S  Mark only the active one|"+
		"1  Columns|2  Rows|3  Grid|4  Active on the left|5  Active on top|Q / Esc  Back|Partial selection: creates another view." {
		t.Errorf("arrange help %q", app.ArrangeHelp())
	}
	frame := func(w, h int, fn func(s *Screen), evs ...Event) string {
		s := newTestScreen(w, h, evs...)
		runTest(t, s, func() { fn(s) })
		return frameText(s)
	}
	checks := []struct {
		frame string
		want  []string
	}{
		{frame(120, 30, func(s *Screen) { ViewMenu(s, a, v.ID, "c1", "", "") }),
			[]string{"VIEW Main | ", "CONSOLES", "LAYOUT", "VIEWS", "MORE", "A  Add existing…", "1  Columns", "Q  Back (Esc)", "E  Explorer: "}},
		{frame(80, 24, func(s *Screen) { Browse(s, a) }), []string{"SAVED VIEWS", "Main | 1 consoles | ", "Del  Delete view", "1/1 | ↑↓ choose"}},
		{frame(80, 24, func(s *Screen) { NewConsole(s, a, v.ID) }), []string{"NEW CONSOLE IN Main", "Enter  Create", "Q / Esc  Cancel"}},
		{frame(80, 24, func(s *Screen) { NewConsole(s, a, v.ID) }, key("enter")), []string{"Name of " + a.Kinds()[0].ID + " (editable)", "Enter  Accept", "Esc  Cancel"}},
		{frame(80, 30, func(s *Screen) { Arrange(s, a, v.ID, "c1") }), []string{"ARRANGE Main | 1/1 marked"}},
		{frame(100, 30, func(s *Screen) { EditView(s, a, v.ID, "c1") }), []string{"CONSOLES OF Main", "api | stopped", "R  Kill and replace here"}},
		{frame(100, 30, func(s *Screen) { EditView(s, a, v.ID, "c1") }, key("m")), []string{"KILL CONSOLE", "api — ends the process", "Enter  Confirm"}},
	}
	for _, c := range checks {
		for _, want := range c.want {
			if !strings.Contains(c.frame, want) {
				t.Errorf("%q not in frame:\n%s", want, c.frame)
			}
		}
	}
	m := &mux.Mux{Binary: "/bin/shellstudio", Tmux: "tmux", Version: "v"}
	var all []string
	for _, c := range append(m.ProgramsControls(), m.ViewsControls(a.DialogSizes())...) {
		all = append(all, strings.Join(c, " "))
	}
	controls := strings.Join(all, "\n")
	for _, want := range []string{"FINISHED · Enter: options", "Restart", "Kill and replace here", "View menu (F2)",
		"Close panel (the console keeps running)", "This panel ended #[default] Click: redo · close panel",
		"Drop on a console to type the path", "Enter or click: restart · rename · close panel · kill console"} {
		if !strings.Contains(controls, want) {
			t.Errorf("tmux controls lack %q", want)
		}
	}
}
