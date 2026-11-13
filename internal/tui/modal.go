// SPDX-License-Identifier: GPL-3.0-only
package tui

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/mattn/go-runewidth"
	"shellstudio/internal/mux"
	"shellstudio/internal/theme"
)

// ModalEnv describes a dialog drawn as a modal: "WIDTH,HEIGHT,CLIENT". tmux
// 3.4 ignores clicks outside a popup, so dialogs open in a borderless popup
// that covers the whole client. The Screen draws the workspace behind it
// (dimmed) and the dialog's box; a click outside the box is Escape, which
// closes every dialog. WIDTH and HEIGHT are the box with its border, as the
// popup size used to be, or a percentage ("80%") of the client.
const ModalEnv = mux.ModalEnv

// Backdrop returns the rows a client shows (plain text), drawn dimmed behind
// a modal. The program sets it; nil draws an empty background.
var Backdrop func(client string, width, height int) []string

type modal struct {
	reqW, reqH string
	client     string
	// fullW and fullH are the popup (the whole client); ox and oy the
	// position of the dialog's first cell, inside the border.
	fullW, fullH int
	ox, oy       int
	backdrop     []string
}

func parseModal(v string) *modal {
	f := strings.Split(v, ",")
	if len(f) < 2 {
		return nil
	}
	m := &modal{reqW: strings.TrimSpace(f[0]), reqH: strings.TrimSpace(f[1])}
	if len(f) > 2 {
		m.client = strings.Join(f[2:], ",")
	}
	return m
}

// dimension is a box size: a number or a percentage of full, clamped.
func dimension(v string, full int) int {
	n := 0
	if p, ok := strings.CutSuffix(v, "%"); ok {
		pc, _ := strconv.Atoi(p)
		n = full * pc / 100
	} else {
		n, _ = strconv.Atoi(v)
	}
	if n <= 0 {
		n = full
	}
	return max(3, min(n, full))
}

// layout centers the box in a popup of w×h cells and returns the size of
// the dialog inside its border.
func (m *modal) layout(w, h int, fetch bool) (int, int) {
	m.fullW, m.fullH = w, h
	bw, bh := dimension(m.reqW, w), dimension(m.reqH, h)
	m.ox, m.oy = (w-bw)/2+1, (h-bh)/2+1
	if fetch && Backdrop != nil && m.client != "" {
		m.backdrop = Backdrop(m.client, w, h)
	}
	return bw - 2, bh - 2
}

// inside reports whether a popup cell belongs to the box (border included).
func (m *modal) inside(x, y, w, h int) bool {
	return x >= m.ox-1 && x <= m.ox+w && y >= m.oy-1 && y <= m.oy+h
}

var backdropStyle = Style{Fg: theme.Border}
var modalBorder = Style{Fg: theme.Accent}

// compose is the whole popup: the backdrop, the box border and the frame.
func (m *modal) compose(frame [][]cell, w, h int) [][]cell {
	out := make([][]cell, m.fullH)
	for y := range out {
		row := make([]cell, m.fullW)
		for x := range row {
			row[x] = cell{' ', Normal}
		}
		if y < len(m.backdrop) {
			x := 0
			for _, r := range clean(m.backdrop[y]) {
				rw := runewidth.RuneWidth(r)
				if rw == 0 {
					continue
				}
				if x+rw > m.fullW {
					break
				}
				row[x] = cell{r, backdropStyle}
				if rw == 2 {
					row[x+1] = cell{0, backdropStyle}
				}
				x += rw
			}
		}
		out[y] = row
	}
	put := func(y, x int, r rune, st Style) {
		if y >= 0 && y < m.fullH && x >= 0 && x < m.fullW {
			// A wide backdrop character cut by the box becomes a space.
			if out[y][x].r == 0 && x > 0 {
				out[y][x-1] = cell{' ', backdropStyle}
			}
			if x+1 < m.fullW && out[y][x+1].r == 0 {
				out[y][x+1] = cell{' ', backdropStyle}
			}
			out[y][x] = cell{r, st}
		}
	}
	left, top, right, bottom := m.ox-1, m.oy-1, m.ox+w, m.oy+h
	for x := left; x <= right; x++ {
		put(top, x, '─', modalBorder)
		put(bottom, x, '─', modalBorder)
	}
	for y := top; y <= bottom; y++ {
		put(y, left, '│', modalBorder)
		put(y, right, '│', modalBorder)
	}
	put(top, left, '┌', modalBorder)
	put(top, right, '┐', modalBorder)
	put(bottom, left, '└', modalBorder)
	put(bottom, right, '┘', modalBorder)
	for y := 0; y < h && y < len(frame); y++ {
		for x := 0; x < w && x < len(frame[y]); x++ {
			put(m.oy+y, m.ox+x, ' ', Normal)
		}
		for x := 0; x < w && x < len(frame[y]); x++ {
			if yy, xx := m.oy+y, m.ox+x; yy < m.fullH && xx < m.fullW {
				out[yy][xx] = frame[y][x]
			}
		}
	}
	return out
}

var (
	modalOnce  sync.Once
	modalValue string
)

// modalFromEnv reads ModalEnv once and removes it from the environment:
// programs started from a dialog (a programs server after a reboot, other
// dialogs) must not believe they are modals of this client.
func modalFromEnv() *modal {
	modalOnce.Do(func() {
		modalValue = os.Getenv(ModalEnv)
		os.Unsetenv(ModalEnv)
	})
	if modalValue != "" {
		return parseModal(modalValue)
	}
	return nil
}
