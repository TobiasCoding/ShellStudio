// SPDX-License-Identifier: GPL-3.0-only
package tui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
)

// Wrap splits text into lines of at most width columns, breaking long words.
func Wrap(text string, width int) []string {
	width = max(1, width)
	var out []string
	for _, para := range strings.Split(text, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			for runewidth.StringWidth(word) > width {
				if line != "" {
					out = append(out, line)
					line = ""
				}
				cut := runewidth.Truncate(word, width, "")
				if cut == "" {
					cut = string([]rune(word)[:1])
				}
				out = append(out, cut)
				word = word[len(cut):]
			}
			switch {
			case word == "":
			case line == "":
				line = word
			case runewidth.StringWidth(line)+1+runewidth.StringWidth(word) <= width:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// HintKey is the key of a shortcut line ("N  Rename"): a click does the same.
func HintKey(line string) string {
	f := strings.Fields(line)
	if len(f) == 0 {
		return ""
	}
	named := map[string]string{"Enter": "enter", "Space": " ", "Tab": "tab", "Del": "delete", "Esc": "esc"}
	if k, ok := named[f[0]]; ok {
		return k
	}
	first := strings.Split(f[0], "/")[0]
	if r := []rune(first); len(r) == 1 {
		return string(unicode.ToLower(r[0]))
	}
	return ""
}

// Choose is the list shared by every dialog. It returns the key that ended it
// (Enter, Esc, q or any key it does not handle) and the selected index.
func Choose(s *Screen, title string, items []string, selected int, message string, help []string) (string, int) {
	page := 0
	var visible []string
	offset, capacity := 0, 1
	for {
		selected = max(0, min(selected, len(items)-1))
		s.Erase()
		s.Draw(0, title, Bold)
		if s.H < 8 || s.W < 24 {
			s.Draw(2, "Enlarge the terminal. Q: leave", Normal)
		} else {
			// List and shortcuts stay apart even in short terminals. The list
			// keeps the rows it needs (up to 3): in a popup sized to its content
			// every shortcut fits without pages.
			room := max(1, s.H-5-max(1, min(len(items), 3)))
			helpCap := room
			if len(help) > room {
				helpCap = max(1, room-1)
			}
			pages := max(1, (len(help)+helpCap-1)/helpCap)
			page %= pages
			visible = help[min(len(help), page*helpCap):min(len(help), (page+1)*helpCap)]
			if pages > 1 {
				visible = append(append([]string{}, visible...), fmt.Sprintf("Tab  More shortcuts (%d/%d)", page+1, pages))
			}
			capacity = max(1, s.H-len(visible)-5)
			offset = (selected / capacity) * capacity
			n := 0
			if len(items) > 0 {
				n = selected + 1
			}
			s.Draw(1, fmt.Sprintf("%d/%d | ↑↓ choose", n, len(items)), Normal)
			for i := offset; i < len(items) && i < offset+capacity; i++ {
				if i == selected {
					s.Draw(3+i-offset, "> "+items[i], Highlight)
				} else {
					s.Draw(3+i-offset, "  "+items[i], Normal)
				}
			}
			if len(items) == 0 {
				s.Draw(3, "No items.", Normal)
			}
			s.Draw(s.H-len(visible)-2, message, Normal)
			for i, line := range visible {
				s.Draw(s.H-len(visible)+i, line, Normal)
			}
		}
		s.Refresh()
		ev := s.Read(-1)
		key := ev.Key
		switch ev.Kind {
		case ResizeEvent, TimeoutEvent, PasteEvent:
			continue
		case MouseEvent:
			// A click on an item selects it; another click on it is Enter. A
			// click on a shortcut line does what its key does.
			if ev.Button == 64 || ev.Button == 65 {
				if len(items) > 0 {
					if ev.Button == 64 {
						selected = max(0, selected-1)
					} else {
						selected = min(len(items)-1, selected+1)
					}
				}
				continue
			}
			_, up := ev.Left()
			if !up {
				continue
			}
			firstHelp := s.H - len(visible)
			if ev.Y >= 3 && ev.Y < 3+capacity && offset+ev.Y-3 < len(items) {
				if offset+ev.Y-3 == selected {
					return "enter", selected
				}
				selected = offset + ev.Y - 3
				continue
			}
			if ev.Y >= firstHelp && ev.Y < s.H && ev.Y-firstHelp < len(visible) {
				key = HintKey(visible[ev.Y-firstHelp])
				if key == "" {
					continue
				}
			} else {
				continue
			}
		}
		if key == "q" || key == "Q" || key == "esc" {
			return key, selected
		}
		if s.H < 8 || s.W < 24 {
			continue
		}
		switch {
		case key == "tab":
			page++
		case (key == "down" || key == "j") && len(items) > 0:
			selected = (selected + 1) % len(items)
		case (key == "up" || key == "k") && len(items) > 0:
			selected = (selected - 1 + len(items)) % len(items)
		case key == "home" && len(items) > 0:
			selected = 0
		case key == "end" && len(items) > 0:
			selected = len(items) - 1
		case key == "pgdown" && len(items) > 0:
			selected = min(len(items)-1, selected+capacity)
		case key == "pgup" && len(items) > 0:
			selected = max(0, selected-capacity)
		default:
			return key, selected
		}
	}
}

// Confirm: one key confirms; Escape cancels without sending anything to a program.
func Confirm(s *Screen, title, detail string) bool {
	start := -1
	for {
		s.Erase()
		titles := Wrap(title, max(1, s.W-2))
		details := Wrap(detail, max(1, s.W-2))
		fits := s.W >= 30 && s.H >= len(titles)+len(details)+7
		if fits {
			for i, l := range titles {
				s.Draw(1+i, l, Bold)
			}
			start = len(titles) + 2
			for i, l := range details {
				s.Draw(start+i, l, Normal)
			}
			start += len(details) + 1
			s.Draw(start, "Enter  Confirm", Highlight)
			s.Draw(start+1, "Esc  Cancel", Normal)
		} else {
			s.Draw(1, "Enlarge the terminal to confirm.", Normal)
			s.Draw(3, "Esc  Cancel", Normal)
		}
		s.Refresh()
		ev := s.Read(-1)
		key := ev.Key
		if ev.Kind == MouseEvent && fits {
			_, up := ev.Left()
			key = ""
			if up && ev.Y == start {
				key = "enter"
			} else if up && ev.Y == start+1 {
				key = "esc"
			}
		} else if ev.Kind != KeyEvent {
			continue
		}
		if fits && key == "enter" {
			return true
		}
		switch key {
		case "esc", "q", "Q", "n", "N":
			return false
		}
	}
}

// Prompt is a short input inside the pane; Escape cancels and Enter accepts.
// An empty answer takes the default, shown greyed until something is typed.
func Prompt(s *Screen, title, detail, initial string, limit int, def string) (string, bool) {
	value := []rune(initial)
	for {
		s.Erase()
		s.Draw(2, title, Bold)
		s.Draw(3, detail, Normal)
		shown := string(value)
		if shown == "" {
			shown = def
		}
		visible := max(1, s.W-5)
		if r := []rune(shown); len(r) > visible {
			shown = string(r[len(r)-visible:])
		}
		s.Draw(4, "> "+shown+"_", Highlight)
		s.Draw(6, "Enter  Accept", Normal)
		s.Draw(7, "Esc  Cancel", Normal)
		s.Refresh()
		ev := s.Read(-1)
		switch ev.Kind {
		case MouseEvent:
			if _, up := ev.Left(); up && ev.Y == 6 {
				ev = Event{Kind: KeyEvent, Key: "enter"}
			} else if up && ev.Y == 7 {
				ev = Event{Kind: KeyEvent, Key: "esc"}
			} else {
				continue
			}
		case PasteEvent:
			for _, r := range ev.Text {
				if unicode.IsPrint(r) && len(value) < limit {
					value = append(value, r)
				}
			}
			continue
		case KeyEvent:
		default:
			continue
		}
		switch ev.Key {
		case "esc":
			return "", false
		case "enter":
			v := strings.TrimSpace(string(value))
			if v == "" {
				v = def
			}
			return v, true
		case "backspace":
			if len(value) > 0 {
				value = value[:len(value)-1]
			}
		case "ctrl+u":
			// Quickly replace the suggested name.
			value = nil
		case "ctrl+w":
			v := strings.TrimRight(string(value), " ")
			if i := strings.LastIndex(v, " "); i >= 0 {
				value = []rune(v[:i+1])
			} else {
				value = nil
			}
		default:
			if r := []rune(ev.Key); len(r) == 1 && unicode.IsPrint(r[0]) && len(value) < limit {
				value = append(value, r[0])
			}
		}
	}
}
