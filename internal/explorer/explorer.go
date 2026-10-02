// SPDX-License-Identifier: GPL-3.0-only
package explorer

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var ansiControl = regexp.MustCompile(`(\x1b|\^\[)(\[[0-?]*[ -/]*[@-~]|\][^\x07]*(\x07|\x1b\\)|[@-_])`)

type Entry struct {
	Name, Path string
	Dir        bool
}

func List(path, search string) ([]Entry, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	items, e := f.ReadDir(2000)
	if e != nil && e != io.EOF {
		return nil, e
	}
	out := []Entry{{"..", filepath.Dir(path), true}}
	for _, it := range items {
		if search != "" && !strings.Contains(strings.ToLower(it.Name()), strings.ToLower(search)) {
			continue
		}
		p := filepath.Join(path, it.Name())
		dir := it.IsDir()
		if it.Type()&os.ModeSymlink != 0 {
			if fi, e := os.Stat(p); e == nil {
				dir = fi.IsDir()
			}
		}
		out = append(out, Entry{it.Name(), p, dir})
	}
	sort.SliceStable(out[1:], func(i, j int) bool {
		a, b := out[i+1], out[j+1]
		if a.Dir != b.Dir {
			return a.Dir
		}
		return a.Name < b.Name
	})
	return out, nil
}
func Clean(s string) string {
	s = ansiControl.ReplaceAllString(s, "")
	return strings.Map(func(r rune) rune {
		if (unicode.IsControl(r) && r != '\n' && r != '\t') || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
func Preview(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	s, e := f.Stat()
	if e != nil {
		return "", e
	}
	if !s.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular file")
	}
	b, e := io.ReadAll(io.LimitReader(f, 65536))
	if e != nil {
		return "", e
	}
	if strings.ContainsRune(string(b), 0) {
		return "Binary file (preview disabled)", nil
	}
	return Clean(string(b)), nil
}
func OSC52(s string) string {
	if len(s) > 65536 {
		s = s[:65536]
	}
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(s)) + "\x07"
}
func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func SSHHelp(path string) string {
	return "SSH / WSL helpers\n\nReconnect: ssh <host> -t shellstudio\nCopy from this host (run locally):\n  scp -- <host>:" + Quote(path) + " .\nForward a port (run locally):\n  ssh -N -L 8080:127.0.0.1:8080 <host>\n\nUse F10 to leave consoles without stopping programs.\nMouse: select panes and drag tmux borders.\nClipboard: Ctrl+B [ enters tmux copy mode; Enter copies via OSC 52.\nYour local terminal must permit OSC 52; no SSH agent forwarding is needed.\nWSL: use /mnt/c/... paths, or Windows Terminal paste (Ctrl+Shift+V).\nNever paste untrusted commands without reviewing them."
}
