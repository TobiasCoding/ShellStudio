// SPDX-License-Identifier: GPL-3.0-only
package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"shellstudio/internal/app"
	"shellstudio/internal/i18n"
	"shellstudio/internal/mux"
	"shellstudio/internal/store"
	"shellstudio/internal/theme"
)

// editableHint follows the current language.
func editableHint() string { return i18n.T("ui.dialog.editable") }

// RunView opens one dialog in its popup. The result is the view to show
// afterwards; Esc in a dialog opened from the view menu goes back to it.
func RunView(a *app.App, action, client, pane string, back bool) error {
	s, e := Open(false)
	if e != nil {
		return e
	}
	result, err := runView(s, a, action, client, pane)
	if err != nil {
		a.RecordError(err, "Dialog")
		Message(s, i18n.T("ui.dialog.failed"), err.Error())
	}
	s.Close()
	if err != nil {
		return nil
	}
	// A click outside the dialog closes the whole chain: Escape goes back.
	if result == "" && back && client != "" && !s.ClosedOutside() {
		return a.Chain(client, "menu", pane, false)
	}
	if result == "" {
		return nil
	}
	if client != "" && clientView(a, client) == result {
		a.RememberView(result)
		return nil
	}
	return a.Open(result, client)
}

func clientView(a *app.App, client string) string {
	o, e := a.Mux.Run(true, "list-clients", "-F", "#{client_name}\t#{session_name}")
	if e != nil {
		return ""
	}
	for _, l := range strings.Split(strings.TrimSpace(o), "\n") {
		c, s, _ := strings.Cut(l, "\t")
		if c == client {
			return strings.TrimPrefix(s, "v-")
		}
	}
	return ""
}

func runView(s *Screen, a *app.App, action, client, pane string) (string, error) {
	focus := func() (app.Focus, error) {
		if client == "" {
			v, e := a.LastView()
			return app.Focus{View: v.ID}, e
		}
		return a.Focus(client, pane, true)
	}
	switch action {
	case "menu":
		f, e := focus()
		if e != nil {
			return "", e
		}
		return ViewMenu(s, a, f.View, f.Console, client, f.Pane), nil
	case "consoles", "add":
		f, e := focus()
		if e != nil {
			return "", e
		}
		if action == "add" {
			return AddDialog(s, a, f.View), nil
		}
		return EditView(s, a, f.View, f.Console), nil
	case "kill":
		return KillFocused(s, a, client, pane)
	case "rename":
		return RenameFocused(s, a, client, pane)
	case "replace":
		return ReplaceFocused(s, a, client, pane)
	case "arrange":
		f, e := focus()
		if e != nil {
			return "", e
		}
		return Arrange(s, a, f.View, f.Console), nil
	case "ports":
		Ports(s, a, client)
		return "", nil
	case "permissions":
		AgentPermissions(s, a)
		return "", nil
	case "new":
		f, e := focus()
		if e != nil {
			return "", e
		}
		return NewConsole(s, a, f.View), nil
	case "views":
		return Browse(s, a), nil
	case "tags":
		TagsDialog(s, a)
		return "", nil
	case "language":
		return "", LanguageDialog(s, a)
	}
	if isMenuAction(action) {
		// Opened from a top bar menu: the same step as in the F1 menu.
		f, e := focus()
		if e != nil {
			return "", e
		}
		v, e := a.Store.View(f.View)
		if e != nil {
			return "", e
		}
		r, _, e := menuAction(s, a, v, f.Console, client, f.Pane, action)
		return r, e
	}
	return "", errors.New(i18n.T("ui.dialog.unknown", action))
}

func kindLabels(kinds []app.Kind) []string {
	out := make([]string, len(kinds))
	for i, k := range kinds {
		out[i] = k.Label
	}
	return out
}

// moveInGrid: arrows between columns and rows of the menu go to the nearest
// option in that direction.
func moveInGrid(actions []app.Cell, selected, key string) string {
	y, x := -1, -1
	for _, c := range actions {
		if c.Action == selected {
			y, x = c.Y, c.X
		}
	}
	if y < 0 {
		return actions[0].Action
	}
	best, dist := selected, 1<<30
	pick := func(c app.Cell) {
		d := c.Y - y
		if d < 0 {
			d = -d
		}
		if d < dist {
			best, dist = c.Action, d
		}
	}
	switch key {
	case "up", "down":
		for _, c := range actions {
			if c.X == x && ((key == "down" && c.Y > y) || (key == "up" && c.Y < y)) {
				pick(c)
			}
		}
	default:
		target := -1
		for _, c := range actions {
			if key == "right" && c.X > x && (target < 0 || c.X < target) {
				target = c.X
			}
			if key == "left" && c.X < x && c.X > target {
				target = c.X
			}
		}
		if target < 0 {
			return selected
		}
		for _, c := range actions {
			if c.X == target {
				pick(c)
			}
		}
	}
	return best
}

// ViewMenu is F1: everything about the view, by category in columns; a
// click, its key, or the arrows and Enter choose an option.
func ViewMenu(s *Screen, a *app.App, view, focused, client, pane string) string {
	message, selected, pressed := "", "consoles", ""
	offset := 0
	for {
		v, e := a.Store.View(view)
		if e != nil {
			return ""
		}
		columns := a.MenuColumns(v)
		cells, _, _ := app.MenuLayout(columns, s.W)
		var actions []app.Cell
		for _, c := range cells {
			if c.Action != "" {
				actions = append(actions, c)
			}
		}
		// Stacked categories can exceed a small terminal. Keep the focused
		// action visible so every option remains reachable by keys or mouse.
		capacity := max(1, s.H-app.MenuTop-1)
		for _, c := range actions {
			if c.Action == selected {
				offset = max(0, min(offset, c.Y))
				if c.Y >= offset+capacity {
					offset = c.Y - capacity + 1
				}
			}
		}
		s.Erase()
		s.Draw(0, i18n.T("ui.menu.title", v.Name, mux.LayoutLabel(v.Layout)), Bold)
		s.Draw(1, i18n.T("ui.menu.hint"), Dim)
		for _, c := range cells {
			if c.Y < offset || c.Y >= offset+capacity {
				continue
			}
			style := Normal
			if c.Action == "" {
				style = Bold
			} else if c.Action == selected {
				style = Highlight
			}
			s.DrawAt(c.Y-offset+app.MenuTop, c.X, c.Text, style)
		}
		s.Draw(s.H-1, message, Normal)
		s.Refresh()
		ev := s.Read(-1)
		message = ""
		action := ""
		switch ev.Kind {
		case MouseEvent:
			if ev.Button == 64 || ev.Button == 65 {
				key := "down"
				if ev.Button == 64 {
					key = "up"
				}
				selected = moveInGrid(actions, selected, key)
				continue
			}
			down, up := ev.Left()
			hit := ""
			for _, c := range actions {
				if c.Y >= offset && c.Y < offset+capacity && ev.Y == c.Y-offset+app.MenuTop && ev.X >= c.X && ev.X < c.X+app.Width(c.Text) {
					hit = c.Action
				}
			}
			if down && hit != "" {
				selected, pressed = hit, hit
			}
			if up && hit != "" && hit == pressed {
				action = hit
			}
			if up {
				pressed = ""
			}
			if action == "" {
				continue
			}
		case KeyEvent:
			switch ev.Key {
			case "esc", "q", "Q":
				return ""
			case "f10":
				action = "leave"
			case "enter":
				action = selected
			case "up", "down", "left", "right":
				selected = moveInGrid(actions, selected, ev.Key)
				continue
			case "+":
				action = "add"
			default:
				for _, col := range columns {
					for _, it := range col.Items {
						if it.Key != " " && strings.EqualFold(it.Key, ev.Key) {
							action = it.Action
						}
					}
				}
				if action == "" {
					continue
				}
			}
		default:
			continue
		}
		selected = action
		result, stay, err := menuAction(s, a, v, focused, client, pane, action)
		if err != nil {
			a.RecordError(err, "View menu")
			message = err.Error()
			continue
		}
		if !stay {
			return result
		}
	}
}

func menuAction(s *Screen, a *app.App, v store.View, focused, client, pane, action string) (string, bool, error) {
	switch {
	case strings.HasPrefix(action, "kind:"):
		kind := strings.TrimPrefix(action, "kind:")
		label, ok := Prompt(s, i18n.T("ui.menu.new_kind_name", kind), editableHint(), "", 60, app.SuggestedName())
		if !ok {
			return "", true, nil
		}
		c, e := a.Create(v.ID, kind, label, nil)
		if e != nil {
			return "", true, e
		}
		a.FocusConsole(v.ID, c.ID)
		return v.ID, false, nil
	case strings.HasPrefix(action, "layout:"):
		return v.ID, false, a.ApplyLayout(v.ID, strings.TrimPrefix(action, "layout:"), focused)
	case action == "explorer":
		return v.ID, false, a.ToggleExplorer(v.ID)
	case action == "new-view":
		nv, ok, e := newViewPrompts(s, a, v.Folder)
		if !ok || e != nil {
			return "", true, e
		}
		return nv, false, nil
	case action == "leave":
		args := []string{"detach-client"}
		if client != "" {
			args = append(args, "-t", client)
		}
		_, e := a.Mux.Run(true, args...)
		return "", false, e
	case action == "back":
		return "", false, nil
	}
	// Lists open in their own popup, sized to their content.
	if client != "" {
		return "", false, a.Chain(client, action, pane, true)
	}
	var r string
	switch action {
	case "consoles":
		r = EditView(s, a, v.ID, focused)
	case "add":
		r = AddDialog(s, a, v.ID)
	case "arrange":
		r = Arrange(s, a, v.ID, focused)
	case "views":
		r = Browse(s, a)
	case "ports":
		Ports(s, a, "")
	case "permissions":
		AgentPermissions(s, a)
	}
	return r, r == "", nil
}

// AgentPermissions chooses the permissions that new Claude and Codex consoles
// start with. Modes without approvals need a second confirmation.
func AgentPermissions(s *Screen, a *app.App) {
	sel, message := 0, ""
	for {
		key, i := Choose(s, i18n.T("ui.perm.title"), a.PermissionItems(), sel, message, app.PermissionsHelp())
		if key != "enter" || i >= len(app.PermissionAgents) {
			return
		}
		sel, message = i, ""
		agent := app.PermissionAgents[i]
		label := app.AgentLabel(agent)
		modes := app.Permissions(agent)
		at := 0
		if cur, e := a.Permission(agent); e == nil {
			for j, p := range modes {
				if p.ID == cur.ID {
					at = j
				}
			}
		}
		k, j := Choose(s, i18n.T("ui.perm.agent", strings.ToUpper(label)), app.PermissionModeItems(agent), at, "", app.PermissionModeHelp())
		if k != "enter" || j >= len(modes) {
			continue
		}
		p := modes[j]
		modeLabel, modeDetail := app.PermissionText(agent, p)
		if p.Dangerous && !Confirm(s, i18n.T("ui.perm.confirm", label, modeLabel),
			i18n.T("ui.perm.danger", modeDetail)) {
			continue
		}
		if e := a.SetPermission(agent, p.ID); e != nil {
			a.RecordError(e, "Agent permissions")
			message = e.Error()
			continue
		}
		message = i18n.T("ui.perm.set", label, modeLabel)
	}
}

func newViewPrompts(s *Screen, a *app.App, folder string) (string, bool, error) {
	name, ok := Prompt(s, i18n.T("ui.dialog.new_view"), "", "", 60, "")
	if !ok || name == "" {
		return "", false, nil
	}
	dir, ok := Prompt(s, i18n.T("ui.dialog.view_folder"), i18n.T("ui.dialog.folder_hint"), folder, 4096, folder)
	if !ok {
		return "", false, nil
	}
	v, e := a.NewView(name, dir, nil, "")
	if e != nil {
		return "", false, e
	}
	return v.ID, true, nil
}

// NewConsole is F3. The last kind created comes first: Enter repeats it.
func NewConsole(s *Screen, a *app.App, view string) string {
	v, e := a.Store.View(view)
	if e != nil {
		return ""
	}
	message := ""
	for {
		kinds := a.Kinds()
		key, sel := Choose(s, i18n.T("ui.dialog.new_in", v.Name), kindLabels(kinds), 0, message, app.NewHelp())
		if key != "enter" || len(kinds) == 0 {
			return ""
		}
		kind := kinds[sel]
		label, ok := Prompt(s, i18n.T("ui.dialog.name_of", kind.ID), editableHint(), "", 60, app.SuggestedName())
		if !ok {
			return ""
		}
		c, e := a.Create(view, kind.ID, label, nil)
		if e != nil {
			a.RecordError(e, "New console")
			message = e.Error()
			continue
		}
		// Focus on the new console, without changing to another view.
		a.FocusConsole(view, c.ID)
		return view
	}
}

func chooseKind(s *Screen, a *app.App, title string) (app.Kind, bool) {
	kinds := a.Kinds()
	key, sel := Choose(s, title, kindLabels(kinds), 0, "", app.ChooseKindHelp())
	if key != "enter" || len(kinds) == 0 {
		return app.Kind{}, false
	}
	return kinds[sel], true
}

// ReplaceFocused chooses the replacement of the console that opened the dialog.
func ReplaceFocused(s *Screen, a *app.App, client, pane string) (string, error) {
	f, e := a.Focus(client, pane, false)
	if e != nil {
		return "", e
	}
	if f.Console == "" {
		return "", errors.New(i18n.T("ui.dialog.pick_replace"))
	}
	kind, ok := chooseKind(s, a, i18n.T("ui.dialog.replace", f.Name))
	if !ok {
		return "", nil
	}
	label, ok := Prompt(s, i18n.T("ui.dialog.name_new", kind.ID), editableHint(), "", 60, app.SuggestedName())
	if !ok {
		return "", nil
	}
	if !Confirm(s, i18n.T("ui.dialog.replace_c"), i18n.T("ui.dialog.replace_d", f.Name)) {
		return "", nil
	}
	// The dialog can stay open while another action rebuilds the view. Never
	// use a console captured from a pane that changed its origin.
	cur, e := a.Focus(client, pane, false)
	if e != nil || cur.View != f.View || cur.Console != f.Console || (pane != "" && cur.Pane != pane) {
		return "", errors.New(i18n.T("ui.dialog.changed"))
	}
	c, e := a.Replace(f.View, f.Console, kind.ID, label)
	if e != nil {
		return "", e
	}
	a.FocusConsole(f.View, c.ID)
	return f.View, nil
}

// RenameFocused keeps the console identity even if another client changes focus.
func RenameFocused(s *Screen, a *app.App, client, pane string) (string, error) {
	f, e := a.Focus(client, pane, false)
	if e != nil || f.Console == "" {
		return "", e
	}
	detail := i18n.T("ui.dialog.keep_name")
	for {
		name, ok := Prompt(s, i18n.T("ui.dialog.rename_c"), detail, "", 80, f.Name)
		if !ok {
			return "", nil
		}
		if e := app.ValidName(name); e != nil {
			detail = e.Error()
			continue
		}
		return "", a.Rename(f.Console, name)
	}
}

// KillFocused is F9 / Ctrl+Shift+Del. The identity is frozen before
// confirming: a focus or name change cannot make it end another program.
func KillFocused(s *Screen, a *app.App, client, pane string) (string, error) {
	f, e := a.Focus(client, pane, false)
	if e != nil {
		return "", e
	}
	if f.Console == "" {
		return "", nil
	}
	if Confirm(s, i18n.T("ui.dialog.kill_c"), i18n.T("ui.dialog.kill_d", f.Name)) {
		return "", a.Kill(f.Console)
	}
	return "", nil
}

// Arrange is F5: mark consoles and choose a layout. All marked rearranges the
// view; some marked creates and opens a partial view with them.
func Arrange(s *Screen, a *app.App, view, focused string) string {
	v, e := a.Store.View(view)
	if e != nil {
		return ""
	}
	members, _ := a.Store.Consoles(view)
	marked := map[string]bool{}
	selected := 0
	for i, c := range members {
		marked[c.ID] = true
		if c.ID == focused {
			selected = i
		}
	}
	message := ""
	for {
		items := make([]string, len(members))
		for i, c := range members {
			box := "[ ] "
			if marked[c.ID] {
				box = "[x] "
			}
			items[i] = box + c.Name
		}
		var key string
		key, selected = Choose(s, i18n.T("ui.dialog.arrange", v.Name, len(marked), len(members)), items, selected, message, app.ArrangeHelp())
		message = ""
		switch {
		case key == "q" || key == "Q" || key == "esc":
			return ""
		case key == " " && len(members) > 0:
			id := members[selected].ID
			if marked[id] {
				delete(marked, id)
			} else {
				marked[id] = true
			}
		case key == "a" || key == "A":
			for _, c := range members {
				marked[c.ID] = true
			}
		case key == "0":
			marked = map[string]bool{}
		case key == "s" || key == "S":
			marked = map[string]bool{}
			for _, c := range members {
				if c.ID == focused {
					marked[c.ID] = true
				}
			}
		case len(key) == 1 && key >= "1" && key <= "5":
			if len(marked) == 0 {
				message = i18n.T("ui.dialog.mark_one")
				continue
			}
			layout := mux.LayoutKeys[key[0]-'1'].Layout
			var chosen []string
			for _, c := range members {
				if marked[c.ID] {
					chosen = append(chosen, c.ID)
				}
			}
			if (layout == "main-vertical" || layout == "main-horizontal") && marked[focused] {
				rest := []string{focused}
				for _, id := range chosen {
					if id != focused {
						rest = append(rest, id)
					}
				}
				chosen = rest
			}
			if len(chosen) == len(members) {
				if e := a.ApplyLayout(view, layout, focused); e != nil {
					message = e.Error()
					continue
				}
				return view
			}
			label, ok := Prompt(s, i18n.T("ui.dialog.partial"), "", "", 60, "")
			if !ok {
				continue
			}
			if label == "" {
				name := []rune(v.Name)
				if len(name) > 45 {
					name = name[:45]
				}
				label = string(name) + "-partial-" + store.ID()[:6]
			}
			nv, e := a.NewView(label, v.Folder, chosen, layout)
			if e != nil {
				message = e.Error()
				continue
			}
			return nv.ID
		}
	}
}

// Browse is F7: the saved views.
func Browse(s *Screen, a *app.App) string {
	selected, message := 0, ""
	for {
		views, e := a.SortedViews()
		if e != nil {
			return ""
		}
		items := make([]string, len(views))
		for i, v := range views {
			cs, _ := a.Store.Consoles(v.ID)
			items[i] = i18n.T("ui.dialog.view_item", v.Name, len(cs), mux.LayoutLabel(v.Layout))
		}
		var key string
		key, selected = Choose(s, i18n.T("ui.dialog.saved_views"), items, selected, message, app.BrowseHelp())
		message = ""
		var err error
		switch {
		case key == "q" || key == "Q" || key == "esc":
			return ""
		case key == "enter" && len(views) > 0:
			return views[selected].ID
		case (key == "e" || key == "E") && len(views) > 0:
			if r := EditView(s, a, views[selected].ID, ""); r != "" {
				return r
			}
		case key == "n" || key == "N":
			folder := a.CWD
			if len(views) > 0 {
				folder = views[selected].Folder
			}
			var id string
			var ok bool
			id, ok, err = newViewPrompts(s, a, folder)
			if ok && err == nil {
				return id
			}
		case key == "delete" && len(views) > 0:
			if Confirm(s, i18n.T("ui.dialog.delete_view", views[selected].Name), i18n.T("ui.dialog.keep_running")) {
				err = a.DeleteView(views[selected].ID)
			}
		}
		if err != nil {
			a.RecordError(err, "Saved views")
			message = err.Error()
		}
	}
}

// AddDialog adds an existing console to the view.
func AddDialog(s *Screen, a *app.App, view string) string {
	options, _ := a.Addable(view)
	programs, _ := a.Mux.Programs()
	labels := make([]string, len(options))
	for i, c := range options {
		labels[i] = c.Name + " | " + app.StatusText(app.Status(programs, c.ID))
	}
	key, i := Choose(s, i18n.T("ui.dialog.add"), labels, 0, "", app.AddHelp())
	if key == "enter" && len(options) > 0 {
		if e := a.AddToView(view, options[i].ID); e != nil {
			a.RecordError(e, "Add console")
			return ""
		}
		a.FocusConsole(view, options[i].ID)
		return view
	}
	return ""
}

// EditView lists the consoles of a view and what can be done with each.
func EditView(s *Screen, a *app.App, view, focused string) string {
	members, _ := a.Store.Consoles(view)
	selected := 0
	for i, c := range members {
		if c.ID == focused {
			selected = i
		}
	}
	message := ""
	for {
		v, e := a.Store.View(view)
		if e != nil {
			return ""
		}
		members, _ = a.Store.Consoles(view)
		programs, _ := a.Mux.Programs()
		items := make([]string, len(members))
		for i, c := range members {
			items[i] = c.Name + " | " + app.StatusText(app.Status(programs, c.ID))
		}
		var key string
		key, selected = Choose(s, i18n.T("ui.dialog.consoles_of", v.Name), items, selected, message, a.ConsolesHelp())
		message = ""
		has := len(members) > 0 && selected < len(members)
		var err error
		switch {
		case key == "q" || key == "Q" || key == "esc":
			return ""
		case key == "enter":
			if has {
				a.FocusConsole(view, members[selected].ID)
			}
			return view
		case key == "a" || key == "A" || key == "+":
			options, _ := a.Addable(view)
			labels := make([]string, len(options))
			for i, c := range options {
				labels[i] = c.Name + " | " + app.StatusText(app.Status(programs, c.ID))
			}
			if k, i := Choose(s, i18n.T("ui.dialog.add"), labels, 0, "", app.AddHelp()); k == "enter" && len(options) > 0 {
				err = a.AddToView(view, options[i].ID)
			}
		case key == "-" && has:
			if err = a.RemoveFromView(view, members[selected].ID); err == nil {
				message = i18n.T("ui.dialog.removed")
			}
		case (key == "f2" || key == "n" || key == "N") && has:
			if name, ok := Prompt(s, i18n.T("ui.dialog.full_name"), members[selected].Name, members[selected].Name, 80, ""); ok && name != "" {
				err = a.Rename(members[selected].ID, name)
			}
		case (key == "r" || key == "R") && has:
			old := members[selected]
			kind, ok := chooseKind(s, a, i18n.T("ui.dialog.replace", old.Name))
			if !ok {
				continue
			}
			label, ok := Prompt(s, i18n.T("ui.dialog.name_new", kind.ID), editableHint(), "", 60, app.SuggestedName())
			if ok && Confirm(s, i18n.T("ui.dialog.replace_c"), i18n.T("ui.dialog.replace_d", old.Name)) {
				var c store.Console
				if c, err = a.Replace(view, old.ID, kind.ID, label); err == nil {
					message = i18n.T("ui.dialog.replaced", old.Name, c.Name)
				}
			}
		case (key == "m" || key == "M" || key == "delete") && has:
			old := members[selected]
			if Confirm(s, i18n.T("ui.dialog.kill_c"), i18n.T("ui.dialog.kill_short", old.Name)) {
				if err = a.Kill(old.ID); err == nil {
					message = i18n.T("ui.dialog.ended", old.Name)
				}
			}
		default:
			for _, k := range a.Kinds() {
				if k.Key != "" && strings.EqualFold(k.Key, key) {
					label, ok := Prompt(s, i18n.T("ui.menu.new_kind_name", k.ID), editableHint(), "", 60, app.SuggestedName())
					if !ok {
						break
					}
					c, e := a.Create(view, k.ID, label, nil)
					if e != nil {
						err = e
						break
					}
					a.FocusConsole(view, c.ID)
					return view
				}
			}
		}
		if err != nil {
			a.RecordError(err, "Consoles of the view")
			message = err.Error()
		}
	}
}

// Ports forwards ports through the same SSH connection used to log in. The
// server cannot open a port on the user's PC: the ssh client asks for it
// (-L). This screen finds the sites, builds the command and copies it.
func Ports(s *Screen, a *app.App, client string) {
	message, selected, copied := "", 0, "\x00"
	copyCommand := func(command string) string {
		if command == "" {
			command = i18n.T("ui.ports.none_cmd")
		}
		if e := a.CopyToClient(client, command); e != nil {
			return i18n.T("ui.ports.no_copy", e.Error())
		}
		return i18n.T("ui.ports.copied")
	}
	scan := func() ([]app.Port, map[int]bool) {
		s.Erase()
		s.Draw(0, i18n.T("ui.ports.title"), Bold)
		s.Draw(2, i18n.T("ui.ports.scanning"), Normal)
		s.Refresh()
		ports := app.ListeningPorts()
		return ports, app.AnswersHTTP(ports)
	}
	ports, web := scan()
	marked := map[int]bool{}
	target := a.SSHTarget(client)
	button := i18n.T("ui.ports.button")
	for {
		var chosen []int
		for _, p := range ports {
			if marked[p.Number] {
				chosen = append(chosen, p.Number)
			}
		}
		var forwards, urls, locals []string
		for i, p := range chosen {
			forwards = append(forwards, fmt.Sprintf("-L %d:127.0.0.1:%d", p, p))
			locals = append(locals, fmt.Sprintf("LocalForward %d 127.0.0.1:%d", p, p))
			if i < 4 {
				urls = append(urls, fmt.Sprintf("http://localhost:%d", p))
			}
		}
		command := ""
		if len(chosen) > 0 {
			command = "ssh " + strings.Join(forwards, " ") + " " + target
		}
		if command != copied {
			message = copyCommand(command)
			copied = command
		}
		orDots := func(s string) string {
			if s == "" {
				return "…"
			}
			return s
		}
		escape := "-L …"
		if len(chosen) > 0 {
			escape = fmt.Sprintf("-L %d:127.0.0.1:%d", chosen[0], chosen[0])
		}
		shown := command
		if shown == "" {
			shown = i18n.T("ui.ports.mark_one")
		}
		guide := []string{
			// The button is drawn over the leading spaces, at column 2.
			strings.Repeat(" ", app.Width(button)+6) + i18n.T("ui.ports.keys"),
			"",
			i18n.T("ui.ports.login"),
			"  " + shown,
			i18n.T("ui.ports.open", orDots(strings.Join(urls, ", "))),
			"",
			i18n.T("ui.ports.permanent"),
			"  " + orDots(strings.Join(locals, "  ")),
			i18n.T("ui.ports.escape", escape),
		}
		s.Erase()
		s.Draw(0, i18n.T("ui.ports.marked", len(chosen), len(ports)), Bold)
		s.Draw(1, i18n.T("ui.ports.intro"), Normal)
		capacity := max(1, s.H-len(guide)-5)
		selected = max(0, min(selected, len(ports)-1))
		top := (selected / capacity) * capacity
		for i := top; i < len(ports) && i < top+capacity; i++ {
			p := ports[i]
			box := "[ ]"
			if marked[p.Number] {
				box = "[x]"
			}
			tag := "    "
			if web[p.Number] {
				tag = "web "
			}
			program := p.Program
			if program == "" {
				program = "—"
			}
			line := fmt.Sprintf("%s %5d  %s %s", box, p.Number, tag, program)
			if i == selected {
				s.Draw(3+i-top, "> "+line, Highlight)
			} else {
				s.Draw(3+i-top, "  "+line, Normal)
			}
		}
		if len(ports) == 0 {
			s.Draw(3, i18n.T("ui.ports.empty"), Normal)
		}
		base := s.H - len(guide) - 1
		s.Draw(base, message, Bold)
		for i, l := range guide {
			s.Draw(base+1+i, l, Normal)
		}
		s.DrawAt(base+1, 2, button, Highlight)
		s.Refresh()
		ev := s.Read(-1)
		message = ""
		key := ev.Key
		switch ev.Kind {
		case MouseEvent:
			_, up := ev.Left()
			if !up {
				continue
			}
			if ev.Y == base+1 && ev.X >= 2 && ev.X < 2+app.Width(button) {
				key = "c"
			} else if ev.Y >= 3 && ev.Y < 3+min(capacity, len(ports)-top) {
				selected = top + ev.Y - 3
				key = " "
			} else {
				continue
			}
		case KeyEvent:
		default:
			continue
		}
		switch {
		case key == "q" || key == "Q" || key == "esc":
			return
		case (key == "down" || key == "j") && len(ports) > 0:
			selected = (selected + 1) % len(ports)
		case (key == "up" || key == "k") && len(ports) > 0:
			selected = (selected - 1 + len(ports)) % len(ports)
		case key == " " && len(ports) > 0:
			n := ports[selected].Number
			marked[n] = !marked[n]
			if !marked[n] {
				delete(marked, n)
			}
		case key == "a" || key == "A":
			if len(marked) == len(ports) {
				marked = map[int]bool{}
			} else {
				for _, p := range ports {
					marked[p.Number] = true
				}
			}
		case key == "r" || key == "R":
			ports, web = scan()
			still := map[int]bool{}
			for _, p := range ports {
				if marked[p.Number] {
					still[p.Number] = true
				}
			}
			marked = still
		case key == "c" || key == "C" || key == "enter":
			message = copyCommand(command)
		}
	}
}

// Upload shows the scp command, copied to the connected PC through OSC 52.
func Upload(a *app.App, source, view, target, client string) error {
	v, e := a.Store.View(view)
	if e != nil {
		return e
	}
	command, e := a.SCPCommand(source, v.Folder, target, client)
	if e != nil {
		return e
	}
	dest, _ := app.Resolve(v.Folder, target)
	s, e := Open(false)
	if e != nil {
		return e
	}
	defer s.Close()
	message := ""
	button := i18n.T("ui.ports.button")
	for {
		s.Erase()
		s.Draw(0, i18n.T("ui.upload.title"), Bold)
		s.Draw(2, i18n.T("ui.upload.source", source), Normal)
		s.Draw(3, i18n.T("ui.upload.dest", dest), Normal)
		s.Draw(4, i18n.T("ui.upload.folders"), Normal)
		s.Draw(6, i18n.T("ui.upload.run"), Normal)
		for i, chunk := range Wrap(command, max(20, s.W-4)) {
			if i >= max(0, s.H-10) {
				break
			}
			s.Draw(7+i, chunk, Normal)
		}
		by, bx := s.H-3, max(2, (s.W-app.Width(button))/2)
		s.DrawAt(by, bx, button, Highlight)
		hint := message
		if hint == "" {
			hint = i18n.T("ui.upload.keys")
		}
		s.Draw(s.H-2, hint, Normal)
		s.Refresh()
		ev := s.Read(-1)
		message = ""
		key := ev.Key
		if ev.Kind == MouseEvent {
			_, up := ev.Left()
			if !(up && ev.Y == by && ev.X >= bx && ev.X < bx+app.Width(button)) {
				continue
			}
			key = "c"
		} else if ev.Kind != KeyEvent {
			continue
		}
		switch key {
		case "esc", "q", "Q":
			return nil
		case "c", "C", "enter":
			if e := a.CopyToClient(client, command); e != nil {
				message = i18n.T("ui.ports.no_copy", e.Error())
			} else {
				message = i18n.T("ui.upload.copied")
			}
		}
	}
}

// Disconnected is shown in a console pane whose nested client ended (the
// program was killed elsewhere or the programs server restarted) or whose
// console is stopped, for example after a reboot. Options stay in the pane.
func Disconnected(a *app.App) error {
	s, e := Open(false)
	if e != nil {
		return e
	}
	defer s.Close()
	pane := os.Getenv("TMUX_PANE")
	message := ""
	for {
		f, err := a.PaneOwner(pane)
		programs, _ := a.Mux.Programs()
		_, running := programs[f.Console]
		c, gone := a.Store.Console(f.Console)
		exists := gone == nil
		// An agent console stopped by a reboot resumes its conversation.
		resumable := false
		if exists && !running {
			resumable = a.Resumable(c)
		}
		if err != nil && message == "" {
			message = err.Error()
		}
		s.Erase()
		s.Header(" " + f.Name + " " + theme.Glyphs.Menu + " ")
		if running {
			s.Draw(2, i18n.T("ui.disc.disconnected", f.Name), Bold)
			s.Draw(3, i18n.T("ui.disc.running"), Normal)
		} else if exists {
			s.Draw(2, i18n.T("ui.disc.stopped", f.Name), Bold)
			s.Draw(3, i18n.T("ui.disc.stopped_d"), Normal)
			if resumable {
				s.Draw(4, i18n.T("ui.disc.resumable"), Normal)
			}
		} else {
			s.Draw(2, i18n.T("ui.disc.gone", f.Name), Bold)
		}
		var options []string
		if running {
			options = append(options, i18n.T("ui.disc.reconnect"))
		} else if resumable {
			options = append(options, i18n.T("ui.disc.resume"), i18n.T("ui.disc.fresh"))
		} else if exists {
			options = append(options, i18n.T("ui.disc.start"))
		}
		options = append(options, i18n.T("ui.disc.close"))
		if exists {
			options = append(options, i18n.T("ui.disc.kill"))
		}
		options = append(options, "", i18n.T("ui.disc.new"), i18n.T("ui.disc.menu"))
		top := 5
		if resumable {
			top = 6
		}
		for i, l := range options {
			s.Draw(top+i, l, Normal)
		}
		s.Draw(top+len(options)+1, message, Normal)
		s.Refresh()
		ev := s.Read(2 * time.Second)
		key := ev.Key
		if ev.Kind == MouseEvent {
			if _, up := ev.Left(); up && ev.Y >= top && ev.Y < top+len(options) {
				key = HintKey(options[ev.Y-top])
			} else {
				continue
			}
		} else if ev.Kind != KeyEvent {
			continue
		}
		message = ""
		if err != nil {
			continue
		}
		switch {
		case key == "enter" && running:
			a.Detached("_sync", f.View)
			message = i18n.T("ui.disc.connecting")
		case key == "enter" && exists:
			if e := a.Detached("restart", f.Console); e != nil {
				message = e.Error()
			} else {
				message = i18n.T("ui.disc.starting")
			}
		case (key == "n" || key == "N") && resumable:
			if e := a.Detached("restart", "--new-conversation", f.Console); e != nil {
				message = e.Error()
			} else {
				message = i18n.T("ui.disc.starting")
			}
		case key == "q" || key == "Q" || key == "-":
			a.Detached("_action", "remove", "--pane", pane)
			message = i18n.T("ui.disc.closing")
		case (key == "m" || key == "M" || key == "delete") && exists:
			if e := a.Detached("_dialog", "kill", "--pane", pane); e != nil {
				message = e.Error()
			}
		}
	}
}

// Empty is the pane of a view without consoles.
func Empty() error {
	s, e := Open(false)
	if e != nil {
		return e
	}
	defer s.Close()
	for {
		s.Erase()
		s.Header(" " + i18n.T("ui.empty.name") + " " + theme.Glyphs.Menu + " ")
		s.Draw(2, i18n.T("ui.empty.title"), Bold)
		s.Draw(4, i18n.T("ui.empty.create"), Normal)
		s.Draw(6, i18n.T("ui.empty.menu"), Normal)
		s.Draw(8, i18n.T("ui.empty.switch"), Normal)
		s.Draw(10, i18n.T("ui.empty.leave"), Normal)
		s.Refresh()
		s.Read(-1)
	}
}
