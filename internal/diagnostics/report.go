// SPDX-License-Identifier: GPL-3.0-only
package diagnostics

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	_ "modernc.org/sqlite"
	"shellstudio/internal/platform"
	"shellstudio/internal/runner"
)

type Section struct {
	Status    string              `json:"status"`
	Rows      []map[string]string `json:"rows,omitempty"`
	Truncated bool                `json:"truncated,omitempty"`
}

type Report struct {
	Schema   int                `json:"schema"`
	At       string             `json:"at"`
	Version  string             `json:"version"`
	System   map[string]any     `json:"system"`
	Sections map[string]Section `json:"sections"`
	Logs     []Entry            `json:"logs"`
	Privacy  string             `json:"privacy"`
}

var identifier = regexp.MustCompile(`^(?:[a-f0-9]{24}|[cv]-[a-f0-9]{24}|[$%@]?[0-9]+|[a-f0-9]{16})$`)
var numeric = regexp.MustCompile(`^[0-9]+(?:,[0-9]+)*$`)

func safeID(s string) string {
	if s == "" || identifier.MatchString(s) {
		return s
	}
	return "[omitted]"
}

func safeValue(key, value string) string {
	switch key {
	case "id", "view_id", "console_id", "session", "pane", "window", "config":
		return safeID(value)
	case "layout":
		for _, v := range []string{"tiled", "even-horizontal", "even-vertical", "main-horizontal", "main-vertical"} {
			if value == v {
				return value
			}
		}
	case "kind", "extension":
		for _, v := range []string{"terminal", "console", "explorer", "empty", "notes", "claude", "codex", "agent-chat", "agent-gantt"} {
			if value == v {
				return value
			}
		}
	case "state":
		for _, v := range []string{"live", "stopped", "finished", "disconnected"} {
			if value == v {
				return value
			}
		}
	default:
		if value == "" || numeric.MatchString(value) {
			return value
		}
	}
	return "[omitted]"
}

// Collect never calls app.Open: failed migrations, bad config, and dead tmux
// servers must remain inspectable. It neither repairs nor starts anything.
func Collect(version string) Report {
	r := Report{Schema: 1, At: time.Now().UTC().Format(time.RFC3339Nano), Version: version,
		System: map[string]any{"os": runtime.GOOS, "arch": runtime.GOARCH, "go": runtime.Version(),
			"ssh": os.Getenv("SSH_CONNECTION") != "", "inside_tmux": os.Getenv("TMUX") != "",
			"stdin_terminal": term.IsTerminal(os.Stdin.Fd()), "stdout_terminal": term.IsTerminal(os.Stdout.Fd())},
		Sections: map[string]Section{}, Logs: []Entry{},
		Privacy: "Operational metadata only. No terminal content, names, folder paths, arguments, environment dump, notes, configuration contents or raw error messages. Error categories intentionally omit free text. Logs are best-effort and may drop entries under contention; this is a live, non-atomic snapshot."}
	if b, ok := debug.ReadBuildInfo(); ok {
		for _, s := range b.Settings {
			if s.Key == "vcs.revision" || s.Key == "vcs.modified" {
				r.System[s.Key] = s.Value
			}
		}
	}
	for _, f := range []*os.File{os.Stdin, os.Stdout, os.Stderr} {
		if w, h, e := term.GetSize(f.Fd()); e == nil {
			r.System["terminal_width"], r.System["terminal_height"] = w, h
			break
		}
	}
	// These describe terminal capabilities, not the SSH address or environment.
	for _, key := range []string{"TERM", "COLORTERM"} {
		v := os.Getenv(key)
		if regexp.MustCompile(`^[a-zA-Z0-9_.+-]{0,64}$`).MatchString(v) {
			r.System[key] = v
		}
	}
	p, err := platform.Resolve()
	if err != nil {
		r.Sections["paths"] = Section{Status: Classify(err)}
		return r
	}
	for key, path := range map[string]string{"config": p.Config, "data": p.Data, "state": p.State, "runtime": p.Runtime} {
		s := Section{Status: "ok"}
		if fi, e := os.Lstat(path); e != nil {
			s.Status = Classify(e)
		} else {
			s.Rows = []map[string]string{{"permissions": fmt.Sprintf("%04o", fi.Mode().Perm()), "directory": fmt.Sprint(fi.IsDir()), "symlink": fmt.Sprint(fi.Mode()&os.ModeSymlink != 0)}}
		}
		r.Sections["directory_"+key] = s
	}
	_, err = platform.LoadConfig(p.Config)
	r.Sections["configuration"] = Section{Status: status(err)}
	r.database(filepath.Join(p.Data, "shellstudio.db"))
	r.tmux(p.Runtime)
	r.readLogs(p.State)
	return r
}

func status(err error) string {
	if err == nil {
		return "ok"
	}
	return Classify(err)
}

func (r *Report) database(path string) {
	fi, err := os.Lstat(path)
	if err != nil {
		r.Sections["database"] = Section{Status: Classify(err)}
		return
	}
	if !fi.Mode().IsRegular() {
		r.Sections["database"] = Section{Status: "not-regular"}
		return
	}
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("mode", "ro")
	q.Add("_pragma", "busy_timeout(1000)")
	q.Add("_pragma", "query_only(ON)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		r.Sections["database"] = Section{Status: Classify(err)}
		return
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var check string
	err = db.QueryRowContext(ctx, "PRAGMA quick_check(1)").Scan(&check)
	s := Section{Status: status(err)}
	if err == nil && check != "ok" {
		s.Status = "integrity-failed"
	}
	r.Sections["database"] = s
	for _, query := range []struct{ name, sql string }{
		{"database_schema", "PRAGMA user_version"},
		{"views", "SELECT id,layout,explorer FROM views ORDER BY id LIMIT 501"},
		{"consoles", "SELECT id,extension FROM consoles ORDER BY id LIMIT 501"},
		{"members", "SELECT view_id,console_id,position FROM members ORDER BY view_id,position LIMIT 501"},
		{"counts", "SELECT (SELECT count(*) FROM views) AS views, (SELECT count(*) FROM consoles) AS consoles, (SELECT count(*) FROM events) AS events"},
	} {
		s := Section{Status: "ok"}
		rows, e := db.QueryContext(ctx, query.sql)
		if e != nil {
			s.Status = Classify(e)
			r.Sections[query.name] = s
			continue
		}
		cols, e := rows.Columns()
		if e != nil {
			rows.Close()
			s.Status = Classify(e)
			r.Sections[query.name] = s
			continue
		}
		for rows.Next() {
			if len(s.Rows) == 500 {
				s.Truncated = true
				break
			}
			values, ptrs := make([]string, len(cols)), make([]any, len(cols))
			for i := range ptrs {
				ptrs[i] = &values[i]
			}
			if e = rows.Scan(ptrs...); e != nil {
				s.Status = Classify(e)
				break
			}
			row := map[string]string{}
			for i, key := range cols {
				row[key] = safeValue(key, values[i])
			}
			s.Rows = append(s.Rows, row)
		}
		if e = rows.Err(); e != nil {
			s.Status = Classify(e)
		}
		rows.Close()
		r.Sections[query.name] = s
	}
}

func (r *Report) tmux(dir string) {
	out, err := runner.Run(2*time.Second, "", nil, "tmux", "-V")
	if err == nil && regexp.MustCompile(`^tmux [0-9]+\.[0-9]+[a-z]?\s*$`).MatchString(out) {
		r.System["tmux"] = strings.TrimSpace(out)
	}
	r.Sections["tmux"] = Section{Status: status(err)}
	for _, server := range []string{"programs", "views"} {
		path := filepath.Join(dir, server+".sock")
		fi, e := os.Lstat(path)
		if os.IsNotExist(e) {
			r.Sections[server] = Section{Status: "server-absent"}
			continue
		}
		if e != nil {
			r.Sections[server] = Section{Status: Classify(e)}
			continue
		}
		if fi.Mode()&os.ModeSocket == 0 {
			r.Sections[server] = Section{Status: "not-socket"}
			continue
		}
		for _, query := range []struct {
			name, cmd, format string
			keys              []string
		}{
			{"panes", "list-panes", "#{session_name}\t#{window_id}\t#{pane_id}\t#{@ss_kind}\t#{@ss_console}\t#{pane_left}\t#{pane_top}\t#{pane_width}\t#{pane_height}\t#{pane_dead}\t#{pane_dead_status}\t#{pane_pid}\t#{pane_active}\t#{@ss_state}", []string{"session", "window", "pane", "kind", "console_id", "left", "top", "width", "height", "dead", "exit_status", "pid", "active", "state"}},
			{"windows", "list-windows", "#{session_name}\t#{window_id}\t#{window_width}\t#{window_height}\t#{window_zoomed_flag}\t#{@ss_layout}\t#{@ss_config}", []string{"session", "window", "width", "height", "zoomed", "layout", "config"}},
			{"clients", "list-clients", "#{client_pid}\t#{session_name}\t#{client_width}\t#{client_height}\t#{client_utf8}", []string{"pid", "session", "width", "height", "utf8"}},
		} {
			args := []string{"-u", "-N", "-S", path, "-f", "/dev/null", query.cmd}
			if query.cmd != "list-clients" {
				args = append(args, "-a")
			}
			args = append(args, "-F", query.format)
			out, e := runner.Run(time.Second, "", nil, "tmux", args...)
			s := Section{Status: status(e), Truncated: len(out) >= 65536}
			if e == nil {
				for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
					if line == "" {
						continue
					}
					if len(s.Rows) == 500 {
						s.Truncated = true
						break
					}
					fields := strings.Split(line, "\t")
					if len(fields) != len(query.keys) {
						s.Status = "invalid-row"
						continue
					}
					row := map[string]string{}
					for i, key := range query.keys {
						row[key] = safeValue(key, fields[i])
					}
					s.Rows = append(s.Rows, row)
				}
			}
			r.Sections[server+"_"+query.name] = s
		}
	}
}

func (r *Report) readLogs(dir string) {
	s := Section{Status: "ok"}
	for _, suffix := range []string{".2", ".1", ""} {
		path := filepath.Join(dir, LogName+suffix)
		fi, e := os.Lstat(path)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			s.Status = Classify(e)
			continue
		}
		if !fi.Mode().IsRegular() {
			s.Status = "not-regular"
			continue
		}
		f, e := os.Open(path)
		if e != nil {
			s.Status = Classify(e)
			continue
		}
		scan := bufio.NewScanner(io.LimitReader(f, MaxLogBytes))
		for scan.Scan() {
			var entry Entry
			if json.Unmarshal(scan.Bytes(), &entry) != nil {
				s.Status = "incomplete-entry"
				continue
			}
			r.Logs = append(r.Logs, entry)
		}
		if scan.Err() != nil {
			s.Status = "incomplete-entry"
		}
		f.Close()
	}
	if len(r.Logs) > 1000 {
		r.Logs = r.Logs[len(r.Logs)-1000:]
		s.Truncated = true
	}
	r.Sections["logs"] = s
}
