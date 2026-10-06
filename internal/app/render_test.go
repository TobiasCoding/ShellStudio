// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"errors"
	"path/filepath"
	"testing"

	"shellstudio/internal/mux"
	"shellstudio/internal/store"
)

func TestRenderDoesNotRecreatePanesWhenInspectionFails(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v, err := s.NewView("Main", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, output string
		err          error
	}{
		{"timeout", "", errors.New("tmux did not answer in time")},
		{"invalid separators", "%0_console_id_0_1_1_@0_version_name_live\n", nil},
		{"empty response", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			a := &App{Store: s, Mux: &mux.Mux{Run: func(views bool, args ...string) (string, error) {
				calls++
				if calls != 1 || !views || args[0] != "list-panes" {
					t.Fatalf("unexpected command after failed inspection: %v", args)
				}
				return tc.output, tc.err
			}}}
			if err := a.render(v.ID); err == nil {
				t.Fatal("failed inspection was treated as an empty view")
			}
		})
	}
}
