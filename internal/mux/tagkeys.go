// SPDX-License-Identifier: GPL-3.0-only
package mux

import (
	"regexp"
	"sort"
	"strings"
)

// TagKeysOption lists the keys bound to tags in the views server, so a
// reconfiguration unbinds the ones that no tag uses any more.
const TagKeysOption = "@ss_tag_keys"

var (
	altKey = regexp.MustCompile(`^M-[A-Za-z0-9]$`)
	fnKey  = regexp.MustCompile(`^((C-)?(M-)?(S-)?)F([1-9]|1[0-2])$`)
)

// ValidTagKey reports whether a tmux key name may be a tag shortcut: Alt plus
// a letter or digit, or F1–F12 with any of Ctrl, Alt and Shift (in that
// order). Plain characters, Enter, Tab, Escape and Ctrl+letter stay with the
// programs.
func ValidTagKey(key string) bool { return altKey.MatchString(key) || fnKey.MatchString(key) }

// ReservedKeys are the keys ShellStudio itself binds in the views server.
func (m *Mux) ReservedKeys() map[string]bool {
	out := map[string]bool{}
	for _, c := range m.ViewsControls(Sizes{}) {
		if len(c) > 2 && c[0] == "bind-key" && c[1] == "-n" {
			out[c[2]] = true
		}
	}
	for _, k := range RetiredKeys {
		out[k] = true
	}
	return out
}

// TagKeyCommands unbind the previous tag keys and bind each key of keys
// (key → tag ID) to tag the focused console, skipping reserved keys.
func (m *Mux) TagKeyCommands(previous []string, keys map[string]string) [][]string {
	var cmds [][]string
	reserved := m.ReservedKeys()
	for _, k := range previous {
		if k != "" && !reserved[k] {
			cmds = append(cmds, []string{"unbind-key", "-q", "-n", k})
		}
	}
	var bound []string
	for k := range keys {
		if ValidTagKey(k) && !reserved[k] {
			bound = append(bound, k)
		}
	}
	sort.Strings(bound)
	for _, k := range bound {
		cmds = append(cmds, []string{"bind-key", "-n", k, "run-shell", "-b",
			m.Self("_action", "tagkey", "--target", keys[k]) + ` --client "#{client_name}" --pane "#{pane_id}"`})
	}
	return append(cmds, setg(TagKeysOption, strings.Join(bound, " ")))
}
