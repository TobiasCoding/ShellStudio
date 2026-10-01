// SPDX-License-Identifier: GPL-3.0-only
package viewer

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
	"shellstudio/internal/explorer"
)

// Read never creates or migrates the MCP database. At most 200 rows are loaded.
func Read(kind, path, filter string) (string, error) {
	fi, e := os.Stat(path)
	if e != nil {
		return "", e
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("viewer source must be a regular file")
	}
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("mode", "ro")
	q.Add("_pragma", "query_only(ON)")
	q.Add("_pragma", "busy_timeout(1000)")
	u.RawQuery = q.Encode()
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return "", e
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var out strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	switch kind {
	case "agent-chat":
		r, e := db.QueryContext(ctx, "SELECT substr(room,1,200),substr(author,1,200),substr(body,1,8192),created_at FROM messages WHERE instr(lower(room || ' ' || author || ' ' || body),lower(?))>0 ORDER BY id DESC LIMIT 200", filter)
		if e != nil {
			return "", e
		}
		defer r.Close()
		for r.Next() {
			var room, author, body, at string
			if e = r.Scan(&room, &author, &body, &at); e != nil {
				return "", e
			}
			fmt.Fprintf(&out, "[%s] %s · %s\n%s\n\n", room, author, at, body)
		}
		if e = r.Err(); e != nil {
			return "", e
		}
	case "agent-gantt":
		r, e := db.QueryContext(ctx, "SELECT t.id,substr(p.name,1,200),substr(t.title,1,200),t.status,substr(coalesce(t.assignee,''),1,200),coalesce(t.start_date,''),coalesce(t.due_date,''),t.progress,substr(coalesce(t.description,''),1,2048) FROM tasks t JOIN projects p ON p.id=t.project_id WHERE instr(lower(p.name || ' ' || t.title || ' ' || t.status || ' #' || t.id),lower(?))>0 ORDER BY p.id,t.id DESC LIMIT 200", filter)
		if e != nil {
			return "", e
		}
		defer r.Close()
		type task struct {
			id, progress                                              int
			project, title, status, assignee, start, end, description string
		}
		var tasks []task
		axisStart, axisEnd := "", ""
		for r.Next() {
			var t task
			if e = r.Scan(&t.id, &t.project, &t.title, &t.status, &t.assignee, &t.start, &t.end, &t.progress, &t.description); e != nil {
				return "", e
			}
			tasks = append(tasks, t)
			if _, e = time.Parse("2006-01-02", t.start); e == nil && (axisStart == "" || t.start < axisStart) {
				axisStart = t.start
			}
			if _, e = time.Parse("2006-01-02", t.end); e == nil && t.end > axisEnd {
				axisEnd = t.end
			}
		}
		if e = r.Err(); e != nil {
			return "", e
		}
		if len(tasks) > 0 {
			fmt.Fprintf(&out, "Calendar: %s → %s (40 columns)\n\n", axisStart, axisEnd)
		}
		for _, t := range tasks {
			fmt.Fprintf(&out, "#%d %s / %s\n  [%s]\n  %-12s %d%% · %s · %s → %s\n%s\n\n", t.id, t.project, t.title, Timeline(t.start, t.end, axisStart, axisEnd, 40), t.status, t.progress, t.assignee, t.start, t.end, t.description)
		}
	default:
		return "", fmt.Errorf("unknown viewer")
	}
	if out.Len() == 0 {
		return "No matching records. The MCP server creates data when used.", nil
	}
	return explorer.Clean(out.String()), nil
}
