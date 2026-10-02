// SPDX-License-Identifier: GPL-3.0-only
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
	"shellstudio/internal/platform"
)

type Store struct {
	DB        *sql.DB
	durableMu sync.Mutex
}

func ID() string {
	var b [12]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}

func Open(path string) (*Store, error) {
	if err := platform.PrivateDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	if fi, e := os.Lstat(path); e == nil && (!fi.Mode().IsRegular() || fi.Mode()&os.ModeSymlink != 0) {
		return nil, errors.New("database must be a regular file")
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	if e = os.Chmod(path, 0600); e != nil {
		return nil, e
	}
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	for _, p := range []string{"busy_timeout(10000)", "journal_mode(WAL)", "synchronous(NORMAL)", "foreign_keys(ON)", "temp_store(MEMORY)"} {
		q.Add("_pragma", p)
	}
	u.RawQuery = q.Encode()
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	s := &Store{DB: db}
	if e = s.migrate(); e != nil {
		db.Close()
		return nil, e
	}
	if e = s.Check(); e != nil {
		db.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) Close() error { return s.DB.Close() }

// Durable runs a write whose success the user is told about (a note shown as
// Saved) with synchronous=FULL: it returns only after the WAL is on disk. View
// and console metadata use NORMAL: after a power cut the database is
// consistent but recent commits can be lost, while each commit stays fast
// on slow disks where a sync takes seconds.
func (s *Store) Durable(write func() error) error {
	// Concurrent note writers must not reset synchronous while another
	// writer is still committing its Saved revision on this connection.
	s.durableMu.Lock()
	defer s.durableMu.Unlock()
	if _, e := s.DB.Exec("PRAGMA synchronous=FULL"); e != nil {
		return e
	}
	defer s.DB.Exec("PRAGMA synchronous=NORMAL")
	return write()
}
func (s *Store) migrate() error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var v int
	if e = tx.QueryRow("PRAGMA user_version").Scan(&v); e != nil {
		return e
	}
	if v > 3 {
		return fmt.Errorf("database schema %d is newer than supported schema 3", v)
	}
	if v == 0 {
		_, e = tx.Exec(`
CREATE TABLE views (id TEXT PRIMARY KEY,name TEXT NOT NULL,folder TEXT NOT NULL,layout TEXT NOT NULL DEFAULT 'tiled');
CREATE TABLE consoles (id TEXT PRIMARY KEY,name TEXT NOT NULL,extension TEXT NOT NULL,profile TEXT NOT NULL,folder TEXT NOT NULL,argv TEXT NOT NULL,env TEXT NOT NULL,created TEXT NOT NULL);
CREATE TABLE members (view_id TEXT REFERENCES views(id) ON DELETE CASCADE,console_id TEXT REFERENCES consoles(id) ON DELETE CASCADE,position INTEGER NOT NULL,PRIMARY KEY(view_id,console_id));
CREATE TABLE notes (id TEXT PRIMARY KEY,title TEXT NOT NULL,body TEXT NOT NULL,revision INTEGER NOT NULL DEFAULT 1,trashed INTEGER NOT NULL DEFAULT 0,updated TEXT NOT NULL);
CREATE TABLE events (id INTEGER PRIMARY KEY,at TEXT NOT NULL,kind TEXT NOT NULL,detail TEXT NOT NULL);
PRAGMA user_version=1;`)
		if e != nil {
			return e
		}
	}
	if v < 2 {
		if _, e = tx.Exec("ALTER TABLE views ADD COLUMN explorer INTEGER NOT NULL DEFAULT 0; PRAGMA user_version=2;"); e != nil {
			return e
		}
	}
	// Schema 3: the file explorer is shown by default, keeps its tree state per
	// view, and layouts are named (a raw tmux layout no longer fits once the
	// explorer pane is joined on the left).
	if v < 3 {
		if _, e = tx.Exec(`ALTER TABLE views ADD COLUMN tree TEXT NOT NULL DEFAULT '{}';
UPDATE views SET explorer=1;
UPDATE views SET layout='tiled' WHERE layout NOT IN ('tiled','even-horizontal','even-vertical','main-horizontal','main-vertical');
PRAGMA user_version=3;`); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) Check() error {
	var v string
	if e := s.DB.QueryRow("PRAGMA quick_check").Scan(&v); e != nil {
		return e
	}
	if v != "ok" {
		return fmt.Errorf("database check failed: %s; preserve the file and recover from backup", v)
	}
	return nil
}
func (s *Store) Backup(path string) error {
	if _, e := os.Lstat(path); !os.IsNotExist(e) {
		return errors.New("backup destination must not exist")
	}
	_, e := s.DB.Exec("VACUUM INTO ?", path)
	if e != nil {
		return e
	}
	return os.Chmod(path, 0600)
}
func (s *Store) Event(kind, detail string) error {
	if len(detail) > 4096 {
		detail = detail[:4096]
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("INSERT INTO events(at,kind,detail) VALUES(?,?,?)", time.Now().UTC().Format(time.RFC3339), kind, detail); e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM events WHERE id < (SELECT coalesce(max(id),0)-999 FROM events)"); e != nil {
		return e
	}
	return tx.Commit()
}

type View struct {
	ID, Name, Folder, Layout string
	Explorer                 bool
	Tree                     string `json:"-"`
}
type Console struct {
	ID, Name, Extension, Profile, Folder string
	Argv                                 []string
	Env                                  map[string]string
}

func (s *Store) Views() ([]View, error) {
	r, e := s.DB.Query("SELECT id,name,folder,layout,explorer,tree FROM views ORDER BY rowid")
	if e != nil {
		return nil, e
	}
	defer r.Close()
	var vs []View
	for r.Next() {
		var v View
		if e = r.Scan(&v.ID, &v.Name, &v.Folder, &v.Layout, &v.Explorer, &v.Tree); e != nil {
			return nil, e
		}
		vs = append(vs, v)
	}
	return vs, r.Err()
}
func (s *Store) View(id string) (View, error) {
	var v View
	e := s.DB.QueryRow("SELECT id,name,folder,layout,explorer,tree FROM views WHERE id=?", id).Scan(&v.ID, &v.Name, &v.Folder, &v.Layout, &v.Explorer, &v.Tree)
	return v, e
}
func (s *Store) NewView(name, folder string) (View, error) {
	v := View{ID: ID(), Name: name, Folder: folder, Layout: "tiled", Explorer: true, Tree: "{}"}
	_, e := s.DB.Exec("INSERT INTO views(id,name,folder,explorer) VALUES(?,?,?,1)", v.ID, v.Name, v.Folder)
	return v, e
}
func (s *Store) SetTree(view, tree string) error {
	_, e := s.DB.Exec("UPDATE views SET tree=? WHERE id=?", tree, view)
	return e
}
func (s *Store) UpdateView(v View) error {
	_, e := s.DB.Exec("UPDATE views SET name=?,folder=?,layout=?,explorer=? WHERE id=?", v.Name, v.Folder, v.Layout, v.Explorer, v.ID)
	return e
}
func (s *Store) DeleteView(id string) error {
	_, e := s.DB.Exec("DELETE FROM views WHERE id=?", id)
	return e
}
func (s *Store) Consoles(view string) ([]Console, error) {
	q := "SELECT c.id,c.name,c.extension,c.profile,c.folder,c.argv,c.env FROM consoles c"
	var args []any
	if view != "" {
		q += " JOIN members m ON m.console_id=c.id WHERE m.view_id=? ORDER BY m.position"
		args = append(args, view)
	} else {
		q += " ORDER BY c.created"
	}
	r, e := s.DB.Query(q, args...)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	var cs []Console
	for r.Next() {
		var c Console
		var a, b string
		if e = r.Scan(&c.ID, &c.Name, &c.Extension, &c.Profile, &c.Folder, &a, &b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(a), &c.Argv); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &c.Env); e != nil {
			return nil, e
		}
		cs = append(cs, c)
	}
	return cs, r.Err()
}
func (s *Store) Console(id string) (Console, error) {
	cs, e := s.Consoles("")
	if e != nil {
		return Console{}, e
	}
	for _, c := range cs {
		if c.ID == id {
			return c, nil
		}
	}
	return Console{}, sql.ErrNoRows
}
func (s *Store) AddConsole(view string, c Console) error {
	a, e := json.Marshal(c.Argv)
	if e != nil {
		return e
	}
	b, e := json.Marshal(c.Env)
	if e != nil {
		return e
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = tx.Exec("INSERT INTO consoles VALUES(?,?,?,?,?,?,?,?)", c.ID, c.Name, c.Extension, c.Profile, c.Folder, string(a), string(b), time.Now().UTC().Format(time.RFC3339Nano))
	if e != nil {
		return e
	}
	_, e = tx.Exec("INSERT INTO members VALUES(?,?,coalesce((SELECT max(position)+1 FROM members WHERE view_id=?),0))", view, c.ID, view)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Link(view, id string) error {
	_, e := s.DB.Exec("INSERT OR IGNORE INTO members VALUES(?,?,coalesce((SELECT max(position)+1 FROM members WHERE view_id=?),0))", view, id, view)
	return e
}
func (s *Store) Unlink(view, id string) error {
	_, e := s.DB.Exec("DELETE FROM members WHERE view_id=? AND console_id=?", view, id)
	return e
}
func (s *Store) Move(view, id string, delta int) error {
	cs, e := s.Consoles(view)
	if e != nil {
		return e
	}
	for i := range cs {
		if cs[i].ID == id {
			j := i + delta
			if j < 0 || j >= len(cs) {
				return nil
			}
			cs[i], cs[j] = cs[j], cs[i]
			break
		}
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for i, c := range cs {
		if _, e = tx.Exec("UPDATE members SET position=? WHERE view_id=? AND console_id=?", i, view, c.ID); e != nil {
			return e
		}
	}
	return tx.Commit()
}

func (s *Store) RenameConsole(id, name string) error {
	r, e := s.DB.Exec("UPDATE consoles SET name=? WHERE id=?", name, id)
	if e != nil {
		return e
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteConsole removes the console and, by cascade, its place in every view.
func (s *Store) DeleteConsole(id string) error {
	_, e := s.DB.Exec("DELETE FROM consoles WHERE id=?", id)
	return e
}

// ConsoleViews lists the views that show a console.
func (s *Store) ConsoleViews(id string) ([]string, error) {
	r, e := s.DB.Query("SELECT view_id FROM members WHERE console_id=?", id)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	var out []string
	for r.Next() {
		var v string
		if e = r.Scan(&v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, r.Err()
}

// Swap exchanges two members of a view, keeping every other position.
func (s *Store) Swap(view, a, b string) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var pa, pb int
	if e = tx.QueryRow("SELECT position FROM members WHERE view_id=? AND console_id=?", view, a).Scan(&pa); e != nil {
		return e
	}
	if e = tx.QueryRow("SELECT position FROM members WHERE view_id=? AND console_id=?", view, b).Scan(&pb); e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE members SET position=? WHERE view_id=? AND console_id=?", pb, view, a); e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE members SET position=? WHERE view_id=? AND console_id=?", pa, view, b); e != nil {
		return e
	}
	return tx.Commit()
}

// SetOrder stores the given members first, in that order, keeping the rest after.
func (s *Store) SetOrder(view string, ids []string) error {
	cs, e := s.Consoles(view)
	if e != nil {
		return e
	}
	order := append([]string{}, ids...)
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	for _, c := range cs {
		if !seen[c.ID] {
			order = append(order, c.ID)
		}
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for i, id := range order {
		if _, e = tx.Exec("UPDATE members SET position=? WHERE view_id=? AND console_id=?", i, view, id); e != nil {
			return e
		}
	}
	return tx.Commit()
}

// Replace puts a new console in the place of an old one, in every view, and
// removes the old console's metadata.
func (s *Store) Replace(old string, c Console) error {
	a, e := json.Marshal(c.Argv)
	if e != nil {
		return e
	}
	b, e := json.Marshal(c.Env)
	if e != nil {
		return e
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("INSERT INTO consoles VALUES(?,?,?,?,?,?,?,?)", c.ID, c.Name, c.Extension, c.Profile, c.Folder, string(a), string(b), time.Now().UTC().Format(time.RFC3339Nano)); e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE members SET console_id=? WHERE console_id=?", c.ID, old); e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM consoles WHERE id=?", old); e != nil {
		return e
	}
	return tx.Commit()
}
