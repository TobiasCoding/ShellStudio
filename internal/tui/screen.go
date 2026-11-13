// SPDX-License-Identifier: GPL-3.0-only

// Package tui draws the dialogs and the file explorer that live inside tmux
// popups and panes. It is a small curses-like layer: a frame of cells, a key
// and mouse reader, and nothing that queries the terminal (no colour or
// device-attribute probes whose answers could leak into a console).
package tui

import (
	"bytes"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-runewidth"
	"shellstudio/internal/theme"
)

// Attr is a set of text attributes.
type Attr uint8

const (
	AttrBold Attr = 1 << iota
	AttrDim
	AttrReverse
	AttrUnderline
)

// Style is a cell's colors and attributes. Fg and Bg are indexes of the
// 256-color palette (no truecolor: Mosh renders these the same); 0 means the
// default, light grey text on RGB black.
type Style struct {
	Fg, Bg uint8
	Attr   Attr
}

var (
	Normal  = Style{}
	Bold    = Style{Attr: AttrBold}
	Dim     = Style{Attr: AttrDim}
	Reverse = Style{Attr: AttrReverse}
	// Cyan is the accent: titles, keys and focus.
	Cyan = Style{Fg: theme.Accent}
	// HeaderBar is the header of an inactive console.
	HeaderBar = Style{Fg: theme.HeaderFg, Bg: theme.HeaderBg}
	// ActiveBar is the header of the focused console.
	ActiveBar = Style{Fg: theme.White, Bg: theme.Accent, Attr: AttrBold}
	// Highlight is the selected row: white on dark blue, bold.
	Highlight = Style{Fg: theme.White, Bg: theme.Selection, Attr: AttrBold}
	// Muted is secondary text: descriptions, dates and hints.
	Muted = Style{Fg: theme.Muted}
	// FieldStyle is the background of text fields and the table header.
	FieldStyle = Style{Fg: theme.Text, Bg: theme.Panel}
)

// Color returns a style with the given foreground and background.
func Color(fg, bg int) Style { return Style{Fg: uint8(fg), Bg: uint8(bg)} }

// With adds attributes.
func (s Style) With(a Attr) Style { s.Attr |= a; return s }

// Has reports an attribute.
func (s Style) Has(a Attr) bool { return s.Attr&a != 0 }

// fills reports whether Draw extends the style to the end of the row.
func (s Style) fills() bool { return s.Has(AttrReverse) || s.Bg != 0 }

func sgr(s Style) string {
	codes := []string{"0"}
	if s.Has(AttrBold) {
		codes = append(codes, "1")
	}
	if s.Has(AttrDim) {
		codes = append(codes, "2")
	}
	if s.Has(AttrUnderline) {
		codes = append(codes, "4")
	}
	if s.Has(AttrReverse) {
		codes = append(codes, "7")
	}
	if s.Fg != 0 {
		codes = append(codes, "38;5;"+strconv.Itoa(int(s.Fg)))
	} else {
		codes = append(codes, "37")
	}
	bg := theme.Black
	if s.Bg != 0 {
		bg = int(s.Bg)
	}
	codes = append(codes, "48;5;"+strconv.Itoa(bg))
	return "\x1b[" + strings.Join(codes, ";") + "m"
}

type cell struct {
	r     rune
	style Style
}

type EventKind int

const (
	KeyEvent EventKind = iota
	MouseEvent
	PasteEvent
	TimeoutEvent
	ResizeEvent
)

// Event is one key, mouse event or paste. Mouse rows and columns are 0-based.
type Event struct {
	Kind                   EventKind
	Key                    string
	X, Y, Button           int
	Press, Release, Motion bool
	Text                   string
	// Shift is set for keys reported with the Shift modifier (CSI 1;2A).
	// Key stays the plain name, so lists keep moving with Shift held.
	Shift bool
	// At is when the event's bytes arrived: double clicks are measured by
	// arrival, not by when a busy program got to them.
	At time.Time
}

func (e Event) Is(keys ...string) bool {
	if e.Kind != KeyEvent {
		return false
	}
	for _, k := range keys {
		if e.Key == k {
			return true
		}
	}
	return false
}

// Left reports a left button press or release.
func (e Event) Left() (press, release bool) {
	if e.Kind != MouseEvent || e.Motion || e.Button != 0 {
		return false, false
	}
	return e.Press, e.Release
}

type Screen struct {
	in, out *os.File
	restore *term.State
	W, H    int
	frame   [][]cell
	bytes   chan []byte
	winch   chan os.Signal
	buf     []byte
	pending []Event
	paste   bool
	arrived time.Time
}

// Open takes over the terminal of this pane or popup.
func Open(paste bool) (*Screen, error) {
	in, out := os.Stdin, os.Stdout
	st, e := term.MakeRaw(in.Fd())
	if e != nil {
		return nil, e
	}
	s := &Screen{in: in, out: out, restore: st, bytes: make(chan []byte, 64), winch: make(chan os.Signal, 4), paste: paste}
	signal.Notify(s.winch, syscall.SIGWINCH)
	s.size()
	init := "\x1b[?1049h\x1b[?25l\x1b[?7l\x1b[?1000h\x1b[?1006h"
	if paste {
		init += "\x1b[?2004h"
	}
	out.WriteString(init)
	go func() {
		for {
			b := make([]byte, 4096)
			n, e := in.Read(b)
			if n > 0 {
				s.bytes <- b[:n]
			}
			if e != nil {
				close(s.bytes)
				return
			}
		}
	}()
	return s, nil
}

func (s *Screen) Close() {
	signal.Stop(s.winch)
	end := "\x1b[?1000l\x1b[?1006l\x1b[?7h\x1b[?25h\x1b[0m\x1b[?1049l"
	if s.paste {
		end = "\x1b[?2004l" + end
	}
	s.out.WriteString(end)
	term.Restore(s.in.Fd(), s.restore)
}

func (s *Screen) size() {
	w, h, e := term.GetSize(s.out.Fd())
	if e != nil || w <= 0 || h <= 0 {
		w, h = 80, 24
	}
	s.W, s.H = w, h
}

// Erase starts a new frame: every cell is a black space.
func (s *Screen) Erase() {
	s.frame = make([][]cell, s.H)
	for y := range s.frame {
		row := make([]cell, s.W)
		for x := range row {
			row[x] = cell{' ', Normal}
		}
		s.frame[y] = row
	}
}

func clean(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if r < 32 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return ' '
		}
		return r
	}, text)
}

// DrawAt writes text from (y, x) up to the last column but one.
func (s *Screen) DrawAt(y, x int, text string, style Style) {
	if y < 0 || y >= s.H || x < 0 || x >= s.W-1 || s.frame == nil {
		return
	}
	row := s.frame[y]
	for _, r := range clean(text) {
		w := runewidth.RuneWidth(r)
		if w == 0 {
			continue
		}
		if x+w > s.W-1 {
			break
		}
		row[x] = cell{r, style}
		if w == 2 {
			row[x+1] = cell{0, style}
		}
		x += w
	}
}

// Draw writes a row from column 1; a reversed style fills the whole row.
func (s *Screen) Draw(y int, text string, style Style) {
	text = clean(text)
	if style.fills() {
		if pad := s.W - 2 - runewidth.StringWidth(text); pad > 0 {
			text += strings.Repeat(" ", pad)
		}
	}
	s.DrawAt(y, 1, text, style)
}

// Header is the first row of panes that are not consoles, styled like the
// header of an inactive console.
func (s *Screen) Header(text string) {
	s.DrawAt(0, 0, text, HeaderBar)
}

// TrackDrag turns button-motion reports on or off, for panes that drag a
// scrollbar thumb. It is off by default: motion reports flood the pane.
func (s *Screen) TrackDrag(on bool) {
	if s.out == nil {
		return
	}
	if on {
		s.out.WriteString("\x1b[?1002h")
	} else {
		s.out.WriteString("\x1b[?1002l")
	}
}

// Frame returns the rows of the current frame as text, for tests and
// diagnostics.
func (s *Screen) Frame() []string {
	out := make([]string, len(s.frame))
	for y, row := range s.frame {
		var b strings.Builder
		for _, c := range row {
			if c.r != 0 {
				b.WriteRune(c.r)
			}
		}
		out[y] = b.String()
	}
	return out
}

// StyleAt is the style of a cell of the current frame.
func (s *Screen) StyleAt(y, x int) Style {
	if y < 0 || y >= len(s.frame) || x < 0 || x >= len(s.frame[y]) {
		return Normal
	}
	return s.frame[y][x].style
}

func (s *Screen) Refresh() {
	if s.out == nil {
		return // A test screen keeps only the frame.
	}
	var b strings.Builder
	b.WriteString("\x1b[H")
	cur := Style{Fg: 1, Bg: 1, Attr: 0xff}
	for y, row := range s.frame {
		b.WriteString("\x1b[" + strconv.Itoa(y+1) + ";1H")
		for _, c := range row {
			if c.r == 0 {
				continue
			}
			if c.style != cur {
				b.WriteString(sgr(c.style))
				cur = c.style
			}
			b.WriteRune(c.r)
		}
	}
	b.WriteString("\x1b[0m")
	s.out.WriteString(b.String())
}

// errTestEndValue ends a program reading a test screen; see testscreen_test.go.
var errTestEndValue any

// Unget returns an event to be read again.
func (s *Screen) Unget(e Event) { s.pending = append([]Event{e}, s.pending...) }

// Read waits for one event; a negative timeout waits forever.
func (s *Screen) Read(timeout time.Duration) Event {
	if len(s.pending) > 0 {
		e := s.pending[0]
		s.pending = s.pending[1:]
		if e.Kind == 1000 && s.in == nil {
			panic(errTestEndValue)
		}
		return e
	}
	var deadline <-chan time.Time
	if timeout >= 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		deadline = t.C
	}
	for {
		if ev, rest, ok := parse(s.buf, false); ok {
			s.buf = rest
			ev.At = s.arrived
			return ev
		}
		var escape <-chan time.Time
		var esc *time.Timer
		if len(s.buf) > 0 {
			// An incomplete sequence: a lone Escape after a short wait.
			esc = time.NewTimer(30 * time.Millisecond)
			escape = esc.C
		}
		stop := func() {
			if esc != nil {
				esc.Stop()
			}
		}
		select {
		case b, ok := <-s.bytes:
			if !ok {
				os.Exit(0) // The pane or popup closed.
			}
			stop()
			if len(s.buf) == 0 {
				s.arrived = time.Now()
			}
			s.buf = append(s.buf, b...)
		case <-escape:
			if ev, rest, ok := parse(s.buf, true); ok {
				s.buf = rest
				ev.At = s.arrived
				return ev
			}
		case <-s.winch:
			stop()
			s.size()
			return Event{Kind: ResizeEvent}
		case <-deadline:
			stop()
			return Event{Kind: TimeoutEvent}
		}
	}
}

var csiKeys = map[string]string{
	"A": "up", "B": "down", "C": "right", "D": "left", "H": "home", "F": "end", "Z": "backtab",
	"1~": "home", "7~": "home", "4~": "end", "8~": "end", "2~": "insert", "3~": "delete", "5~": "pgup", "6~": "pgdown",
	"11~": "f1", "12~": "f2", "13~": "f3", "14~": "f4", "15~": "f5", "17~": "f6", "18~": "f7", "19~": "f8",
	"20~": "f9", "21~": "f10", "23~": "f11", "24~": "f12", "P": "f1", "Q": "f2", "R": "f3", "S": "f4",
}

// parse extracts one event from the buffer. With flush, an incomplete escape
// sequence is returned as Escape (or Alt+key) instead of waiting for more.
func parse(b []byte, flush bool) (Event, []byte, bool) {
	if len(b) == 0 {
		return Event{}, b, false
	}
	key := func(k string, n int) (Event, []byte, bool) { return Event{Kind: KeyEvent, Key: k}, b[n:], true }
	c := b[0]
	if c != 0x1b {
		switch {
		case c == '\r' || c == '\n':
			return key("enter", 1)
		case c == '\t':
			return key("tab", 1)
		case c == 0x7f || c == 0x08:
			return key("backspace", 1)
		case c == 0:
			return key("ctrl+space", 1)
		case c < 32:
			return key("ctrl+"+string(rune('a'+c-1)), 1)
		}
		if !utf8.FullRune(b) && !flush {
			return Event{}, b, false
		}
		r, n := utf8.DecodeRune(b)
		return key(string(r), n)
	}
	if len(b) == 1 {
		if flush {
			return key("esc", 1)
		}
		return Event{}, b, false
	}
	switch b[1] {
	case '[':
		if bytes.HasPrefix(b, []byte("\x1b[200~")) {
			end := bytes.Index(b, []byte("\x1b[201~"))
			if end < 0 {
				if len(b) > 1<<16 {
					return Event{Kind: PasteEvent, Text: string(b[6:])}, nil, true
				}
				return Event{}, b, false
			}
			return Event{Kind: PasteEvent, Text: string(b[6:end])}, b[end+6:], true
		}
		if len(b) >= 3 && b[2] == 'M' {
			if len(b) < 6 {
				if flush {
					return key("esc", 1)
				}
				return Event{}, b, false
			}
			cb := int(b[3]) - 32
			ev := Event{Kind: MouseEvent, X: int(b[4]) - 33, Y: int(b[5]) - 33, Button: cb & 3, Motion: cb&32 != 0}
			if cb&64 != 0 {
				ev.Button = 64 + cb&3
				ev.Press = true
			} else if cb&3 == 3 {
				ev.Button, ev.Release = 0, true
			} else {
				ev.Press = true
			}
			return ev, b[6:], true
		}
		// CSI: parameters 0x30–0x3f, intermediates 0x20–0x2f, final 0x40–0x7e.
		i := 2
		for i < len(b) && b[i] >= 0x20 && b[i] <= 0x3f {
			i++
		}
		if i >= len(b) {
			if flush {
				return key("esc", 1)
			}
			return Event{}, b, false
		}
		params, final := string(b[2:i]), b[i]
		rest := b[i+1:]
		if strings.HasPrefix(params, "<") && (final == 'M' || final == 'm') {
			f := strings.Split(params[1:], ";")
			if len(f) == 3 {
				cb, _ := strconv.Atoi(f[0])
				x, _ := strconv.Atoi(f[1])
				y, _ := strconv.Atoi(f[2])
				ev := Event{Kind: MouseEvent, X: x - 1, Y: y - 1, Button: cb & 3, Motion: cb&32 != 0}
				if cb&64 != 0 {
					ev.Button = 64 + cb&3
				}
				ev.Press, ev.Release = final == 'M', final == 'm'
				return ev, rest, true
			}
		}
		if final == 'I' || final == 'O' {
			return parse(rest, flush) // Focus reports: nothing to do.
		}
		name, shift := params, false
		if j := strings.Index(name, ";"); j >= 0 {
			// xterm modifiers are 1 plus a bit mask: 1 Shift, 2 Alt, 4 Ctrl.
			if m, e := strconv.Atoi(name[j+1:]); e == nil && m > 1 {
				shift = (m-1)&1 != 0
			}
			name = name[:j]
		}
		if name == "1" && final != '~' {
			name = ""
		}
		if k, ok := csiKeys[name+string(final)]; ok {
			return Event{Kind: KeyEvent, Key: k, Shift: shift}, rest, true
		}
		return Event{Kind: KeyEvent, Key: "unknown"}, rest, true
	case 'O':
		if len(b) < 3 {
			if flush {
				return key("alt+O", 2)
			}
			return Event{}, b, false
		}
		if k, ok := csiKeys[string(b[2])]; ok {
			return Event{Kind: KeyEvent, Key: k}, b[3:], true
		}
		return Event{Kind: KeyEvent, Key: "unknown"}, b[3:], true
	case 0x1b:
		return key("esc", 1)
	}
	r, n := utf8.DecodeRune(b[1:])
	return Event{Kind: KeyEvent, Key: "alt+" + string(r)}, b[1+n:], true
}
