// SPDX-License-Identifier: GPL-3.0-only
package mux

import (
	"regexp"
	"strings"
)

var unsafeShell = regexp.MustCompile(`[^\w@%+=:,./-]`)

// Quote follows POSIX shell single-quote rules. tmux's command parser accepts
// the same quoting, so one function serves shell commands and tmux scripts.
func Quote(s string) string {
	if s == "" {
		return "''"
	}
	if !unsafeShell.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// Join quotes each argument; arguments never become shell or tmux syntax.
func Join(args ...string) string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = Quote(a)
	}
	return strings.Join(out, " ")
}

// Script turns tmux commands into one command-list string for bind-key/if-shell.
func Script(commands ...[]string) string {
	out := make([]string, 0, len(commands))
	for _, c := range commands {
		out = append(out, Join(c...))
	}
	return strings.Join(out, " ; ")
}
