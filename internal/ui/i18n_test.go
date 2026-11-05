// SPDX-License-Identifier: GPL-3.0-only
package ui

import (
	"errors"
	"strings"
	"testing"

	"shellstudio/internal/i18n"
)

func withLang(t *testing.T, l string) {
	t.Helper()
	prev := i18n.Lang()
	i18n.SetLang(l)
	t.Cleanup(func() { i18n.SetLang(prev) })
}

// The save state stays in English (it is compared); only View translates it.
func TestNotesInSpanish(t *testing.T) {
	withLang(t, "es")
	m := noteModel(t)
	v := m.View()
	if m.saveStatus != "Saved" || !strings.Contains(v, "test · Guardada") || !strings.Contains(v, "Autoguardado:") {
		t.Fatalf("%q\n%s", m.saveStatus, v)
	}
	m.Update(savedMsg{err: errors.New("disco lleno")})
	if !strings.HasPrefix(m.saveStatus, "Error:") || !strings.Contains(m.View(), "test · Error: disco lleno") {
		t.Fatalf("%q\n%s", m.saveStatus, m.View())
	}
	m.goTo("notes")
	if v := m.View(); !strings.Contains(v, "NOTAS") || !strings.Contains(v, "n nueva · Enter editar") {
		t.Fatal(v)
	}
	m.trash = true
	if !strings.Contains(m.View(), "NOTAS · Papelera") {
		t.Fatal(m.View())
	}
}

// The integration tests run with LANG=C and match these strings.
func TestNotesInEnglish(t *testing.T) {
	withLang(t, "en")
	m := noteModel(t)
	if v := m.View(); !strings.Contains(v, "test · Saved") || !strings.Contains(v, "Autosave: 300 ms idle / 1 s continuous") {
		t.Fatal(v)
	}
	m.goTo("notes")
	if v := m.View(); !strings.Contains(v, "NOTES") || !strings.Contains(v, "n new · Enter edit · / search") {
		t.Fatal(v)
	}
}
