// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"database/sql"
	"errors"
	"strings"

	"shellstudio/internal/i18n"
	"shellstudio/internal/mux"
)

// KeyLabel shows a tmux key name the way people write it: M-t is Alt+T and
// C-S-F5 is Ctrl+Shift+F5.
func KeyLabel(key string) string {
	if key == "" {
		return ""
	}
	var parts []string
	for {
		switch {
		case strings.HasPrefix(key, "C-"):
			parts = append(parts, "Ctrl")
		case strings.HasPrefix(key, "M-"):
			parts = append(parts, "Alt")
		case strings.HasPrefix(key, "S-"):
			parts = append(parts, "Shift")
		default:
			if len([]rune(key)) == 1 {
				if r := []rune(key)[0]; r >= 'A' && r <= 'Z' {
					parts = append(parts, "Shift")
				}
				key = strings.ToUpper(key)
			}
			return strings.Join(append(parts, key), "+")
		}
		key = key[2:]
	}
}

// SetTagShortcut gives a tag a key (empty removes it) and rebinds the keys
// of every tag.
func (a *App) SetTagShortcut(id, key string) error {
	if key != "" {
		if !mux.ValidTagKey(key) {
			return errors.New(i18n.T("tags.key_invalid"))
		}
		if a.Mux.ReservedKeys()[key] {
			return errors.New(i18n.T("tags.key_reserved", KeyLabel(key)))
		}
		tags, e := a.Store.Tags()
		if e != nil {
			return e
		}
		for _, t := range tags {
			if t.ID != id && t.Shortcut == key {
				return errors.New(i18n.T("tags.key_taken", KeyLabel(key), t.Name))
			}
		}
	}
	if e := a.Store.SetTagShortcut(id, key); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return errors.New(i18n.T("err.tag_gone"))
		}
		return e
	}
	return a.BindTagKeys()
}

// BindTagKeys binds the shortcut of every tag in the views server and
// unbinds the keys no tag uses any more.
func (a *App) BindTagKeys() error {
	tags, e := a.Store.Tags()
	if e != nil {
		return e
	}
	keys := map[string]string{}
	for _, t := range tags {
		if t.Shortcut != "" {
			keys[t.Shortcut] = t.ID
		}
	}
	o, e := a.Mux.Run(true, "show-option", "-gqv", mux.TagKeysOption)
	if mux.NoServer(e) {
		return nil
	}
	_, e = a.Mux.Batch(true, a.Mux.TagKeyCommands(strings.Fields(o), keys)...)
	if mux.NoServer(e) {
		return nil
	}
	return e
}

// ToggleTag is a tag's shortcut: it tags the console, or untags it when it
// already has that tag.
func (a *App) ToggleTag(console, tag string) error {
	c, e := a.Store.Console(console)
	if e != nil {
		return errors.New(i18n.T("err.console_gone"))
	}
	if c.TagID == tag {
		tag = ""
	}
	return a.SetTag(console, tag)
}
