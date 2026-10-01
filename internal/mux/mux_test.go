// SPDX-License-Identifier: GPL-3.0-only
package mux

import (
	"fmt"
	"testing"
)

func focusMux() *Mux {
	return &Mux{Run: func(_ bool, args ...string) (string, error) {
		switch args[0] {
		case "list-clients":
			return "/dev/pts/9\tview-one\n/dev/pts/10\tview-two\n", nil
		case "list-panes":
			if args[2] == "=view-one:" {
				return "%31\tconsole-a\t0\t1\n%51\tconsole-b\t0\t0\n", nil
			}
			return "%99\tother\t0\t1\n", nil
		}
		return "", fmt.Errorf("unexpected command %v", args)
	}}
}
func TestClientFocusReplacement(t *testing.T) {
	m := focusMux()
	s, p, e := m.Focus("/dev/pts/9", "%7", true)
	if e != nil || s != "view-one" || p.ID != "%31" {
		t.Fatal(s, p, e)
	}
	if _, _, e = m.Focus("/dev/pts/9", "%7", false); e == nil {
		t.Fatal("destructive stale pane accepted")
	}
	if _, _, e = m.Focus("/dev/pts/11", "%31", true); e == nil {
		t.Fatal("disconnected client used another focus")
	}
	_, p, e = m.Focus("/dev/pts/10", "", false)
	if e != nil || p.ID != "%99" {
		t.Fatal(p, e)
	}
}
