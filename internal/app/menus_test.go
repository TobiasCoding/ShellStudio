// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"strings"
	"testing"
)

// Dialogs opened from Go (Description and Restore in another view from the
// Minimized panel) once passed #{client_width} formats that tmux cannot
// expand outside a key binding: the popup failed with "height invalid".
func TestDialogForPopupHasNoUnexpandedFormats(t *testing.T) {
	a, _ := offlineApp(t)
	a.Mux.Tmux, a.Mux.Binary = "tmux", "/bin/shellstudio"
	argv := a.dialogForArgv("describe", "/dev/pts/3", "%4", "console1")
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, "#{") {
		t.Fatalf("unexpanded format: %q", argv)
	}
	if !strings.Contains(joined, "display-popup -c /dev/pts/3 -E -B -x 0 -y 0 -w 100% -h 100% -e SHELLSTUDIO_MODAL=64,12,/dev/pts/3") ||
		!strings.Contains(joined, "_view describe --client /dev/pts/3 --target console1 --pane %4") {
		t.Fatalf("argv %q", argv)
	}
}
