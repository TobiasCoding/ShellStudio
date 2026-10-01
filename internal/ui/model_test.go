// SPDX-License-Identifier: GPL-3.0-only
package ui

import (
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"path/filepath"
	"shellstudio/internal/app"
	"shellstudio/internal/store"
	"testing"
	"time"
)

func noteModel(t *testing.T) *Model {
	t.Helper()
	dir := t.TempDir()
	s, e := store.Open(filepath.Join(dir, "db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	a := &app.App{Store: s, CWD: dir}
	m := New(a, "notes")
	n, e := s.NewNote("test")
	if e != nil {
		t.Fatal(e)
	}
	m.openNote(n)
	return m
}
func TestAutosaveDebounceContinuousAndExit(t *testing.T) {
	m := noteModel(t)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("first")})
	if !m.dirty || m.saveStatus != "Saving" {
		t.Fatal("pending edit shown as saved")
	}
	m.Update(tickMsg(time.Now().Add(100 * time.Millisecond)))
	if m.saving {
		t.Fatal("saved before idle threshold")
	}
	m.changed = time.Now()
	m.firstDirty = time.Now().Add(-1100 * time.Millisecond)
	m.Update(tickMsg(time.Now()))
	if !m.saving {
		t.Fatal("continuous typing did not trigger save")
	}
	m.saving = false
	cmd := m.startSave()
	m.Update(cmd())
	if m.dirty || m.saveStatus != "Saved" {
		t.Fatal(m.saveStatus)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" more")})
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("leaving did not flush")
	}
	m.Update(cmd())
	if m.Screen != "notes" {
		t.Fatal(m.Screen)
	}
}
func TestFailedSaveKeepsEditorAndRevision(t *testing.T) {
	m := noteModel(t)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pending")})
	rev := m.note.Revision
	m.leave = true
	m.Update(savedMsg{err: errors.New("database or disk is full")})
	if !m.dirty || m.Screen != "editor" || m.note.Revision != rev || m.saveStatus == "Saved" {
		t.Fatal("save failure was hidden")
	}
	m.Update(tickMsg(time.Now().Add(time.Minute)))
	if m.saving {
		t.Fatal("failed save retried in a tight loop")
	}
}
func TestTypingDuringCommitIsNotMarkedSaved(t *testing.T) {
	m := noteModel(t)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("old")})
	cmd := m.startSave()
	msg := cmd()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("new")})
	m.Update(msg)
	if !m.dirty || m.saveStatus == "Saved" {
		t.Fatal("unsaved newer text marked Saved")
	}
}
func TestNarrowViewDoesNotPanic(t *testing.T) {
	m := noteModel(t)
	m.Update(tea.WindowSizeMsg{Width: 20, Height: 8})
	if len(m.View()) == 0 {
		t.Fatal("empty view")
	}
	_ = os.Getpid()
}
