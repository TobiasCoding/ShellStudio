// SPDX-License-Identifier: GPL-3.0-only
package viewer

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTimeline(t *testing.T) {
	line := Timeline("2026-01-03", "2026-01-04", "2026-01-01", "2026-01-10", 10)
	if line != "  ━━      " {
		t.Fatal(line)
	}
	if Timeline("", "", "2026-01-01", "2026-01-10", 10) != "(unscheduled)" {
		t.Fatal("missing schedule")
	}
}
func TestReadOnlyAndBoundedChat(t *testing.T) {
	p := filepath.Join(t.TempDir(), "messages.db")
	if _, e := Read("agent-chat", p, ""); e == nil {
		t.Fatal("created absent database")
	}
	if _, e := os.Stat(p); !os.IsNotExist(e) {
		t.Fatal("created absent database")
	}
	db, e := sql.Open("sqlite", p)
	if e != nil {
		t.Fatal(e)
	}
	db.Exec("CREATE TABLE messages(id integer primary key,room text,author text,body text,created_at text)")
	db.Exec("INSERT INTO messages VALUES(1,'room','author',?,'today')", "\x1b[31mhello")
	db.Close()
	before, _ := os.ReadFile(p)
	s, e := Read("agent-chat", p, "hello")
	if e != nil || !strings.Contains(s, "hello") || strings.ContainsRune(s, 27) {
		t.Fatal(s, e)
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Fatal("viewer changed source")
	}
}
