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
	"time"

	_ "modernc.org/sqlite"
	"shellstudio/internal/platform"
)

type Store struct{ DB *sql.DB }

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
	for _, p := range []string{"journal_mode(WAL)", "synchronous(FULL)", "foreign_keys(ON)", "busy_timeout(3000)", "temp_store(MEMORY)"} {
		q.Add("_pragma", p)
	}
	u.RawQuery = q.Encode()
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	s := &Store{db}
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
	if v > 2 {
		return fmt.Errorf("database schema %d is newer than supported schema 2", v)
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
}
type Console struct {
	ID, Name, Extension, Profile, Folder string
	Argv                                 []string
	Env                                  map[string]string
}

func (s *Store) Views() ([]View, error) {
	r, e := s.DB.Query("SELECT id,name,folder,layout,explorer FROM views ORDER BY rowid")
	if e != nil {
		return nil, e
	}
	defer r.Close()
	var vs []View
	for r.Next() {
		var v View
		if e = r.Scan(&v.ID, &v.Name, &v.Folder, &v.Layout, &v.Explorer); e != nil {
			return nil, e
		}
		vs = append(vs, v)
	}
	return vs, r.Err()
}
func (s *Store) View(id string) (View, error) {
	var v View
	e := s.DB.QueryRow("SELECT id,name,folder,layout,explorer FROM views WHERE id=?", id).Scan(&v.ID, &v.Name, &v.Folder, &v.Layout, &v.Explorer)
	return v, e
}
func (s *Store) NewView(name, folder string) (View, error) {
	v := View{ID: ID(), Name: name, Folder: folder, Layout: "tiled"}
	_, e := s.DB.Exec("INSERT INTO views(id,name,folder) VALUES(?,?,?)", v.ID, v.Name, v.Folder)
	return v, e
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
