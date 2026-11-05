// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"testing"

	"shellstudio/internal/i18n"
)

// Permission presets and status words are data in English; only their
// display is translated, and in English it is the data itself.
func TestDisplayedDataTranslates(t *testing.T) {
	prev := i18n.Lang()
	t.Cleanup(func() { i18n.SetLang(prev) })
	for _, l := range []string{"en", "es"} {
		i18n.SetLang(l)
		for _, agent := range PermissionAgents {
			for _, p := range Permissions(agent) {
				if !i18n.Has("perm." + agent + "." + p.ID) {
					t.Fatalf("no translation for %s %s", agent, p.ID)
				}
				label, detail := PermissionText(agent, p)
				if l == "en" && (label != p.Label || detail != p.Detail) {
					t.Errorf("%s %s: %q / %q", agent, p.ID, label, detail)
				}
				if l == "es" && detail == p.Detail {
					t.Errorf("%s %s detail untranslated", agent, p.ID)
				}
			}
		}
		for _, s := range []string{"running", "stopped", "finished"} {
			if got := StatusText(s); (l == "en") != (got == s) {
				t.Errorf("%s: StatusText(%s) = %q", l, s, got)
			}
		}
	}
}
