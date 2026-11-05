// SPDX-License-Identifier: GPL-3.0-only
package tui

import (
	"strings"

	"shellstudio/internal/app"
	"shellstudio/internal/i18n"
	"shellstudio/internal/store"
)

// RunTopBar opens the dialogs of a top bar tab: rename-view and close-view.
// target is the tab's range (mux.TabRange).
func RunTopBar(a *app.App, action, client, target string) error {
	v, e := a.ViewByTab(target)
	if e != nil {
		return e
	}
	s, e := Open(false)
	if e != nil {
		return e
	}
	err := topBarDialog(s, a, action, v)
	if err != nil {
		a.RecordError(err, "View tab")
		Message(s, "ACTION COULD NOT BE COMPLETED", err.Error())
	}
	s.Close()
	return nil
}

func topBarDialog(s *Screen, a *app.App, action string, v store.View) error {
	switch action {
	case "rename-view":
		for {
			name, ok := Prompt(s, i18n.T("menu.rename_view"), v.Name, v.Name, 80, v.Name)
			if !ok {
				return nil
			}
			e := a.RenameView(v.ID, name)
			if e == nil {
				return nil
			}
			Message(s, "ACTION COULD NOT BE COMPLETED", e.Error())
		}
	case "close-view":
		if !Confirm(s, i18n.T("menu.close_view")+": "+v.Name, "Its consoles keep running and stay in other views.") {
			return nil
		}
		return a.DeleteView(v.ID)
	}
	return nil
}

// isMenuAction is an F2 menu action that a top bar menu can open directly.
func isMenuAction(action string) bool {
	return strings.HasPrefix(action, "kind:") || action == "new-view"
}
