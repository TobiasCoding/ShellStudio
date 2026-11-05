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
	if v > SchemaVersion {
		return fmt.Errorf("database schema %d is newer than supported schema %d", v, SchemaVersion)
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
	// Schema 4: user tags and descriptions on consoles, minimized members
	// (kept with their position so restoring returns them to their place) and
	// persistent sizes of the left column. The seeded tags are ordinary rows:
	// no code depends on their names.
	if v < 4 {
		if _, e = tx.Exec(`CREATE TABLE tags (id TEXT PRIMARY KEY,name TEXT NOT NULL UNIQUE,color INTEGER NOT NULL,position INTEGER NOT NULL);
ALTER TABLE consoles ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE consoles ADD COLUMN tag_id TEXT REFERENCES tags(id) ON DELETE SET NULL;
ALTER TABLE members ADD COLUMN minimized INTEGER NOT NULL DEFAULT 0;
ALTER TABLE members ADD COLUMN minimized_at TEXT;
ALTER TABLE views ADD COLUMN explorer_width INTEGER NOT NULL DEFAULT 0;
ALTER TABLE views ADD COLUMN minimized_height INTEGER NOT NULL DEFAULT 0;
ALTER TABLE views ADD COLUMN minimized_panel INTEGER NOT NULL DEFAULT 1;`); e != nil {
			return e
		}
		for i, t := range SeedTags {
			if _, e = tx.Exec("INSERT INTO tags(id,name,color,position) VALUES(?,?,?,?)", ID(), t.Name, t.Color, i); e != nil {
				return e
			}
		}
		if _, e = tx.Exec("PRAGMA user_version=4"); e != nil {
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

// SchemaVersion is the newest metadata schema this build reads and writes.
const SchemaVersion = 4

// SeedTags are created once by the schema 4 migration. They are ordinary,
// editable rows; Color is an index of the 256-color palette.
var SeedTags = []Tag{
	{Name: "Por revisar", Color: 179},
	{Name: "En curso", Color: 33},
	{Name: "Pendiente", Color: 245},
	{Name: "Listo", Color: 71},
}

type View struct {
	ID, Name, Folder, Layout string
	Explorer                 bool
	Tree                     string `json:"-"`
	// ExplorerWidth and MinimizedHeight are the saved sizes of the left
	// column and of the Minimized panel; 0 means automatic.
	ExplorerWidth, MinimizedHeight int
	// MinimizedPanel shows the Minimized panel under the explorer.
	MinimizedPanel bool
}
type Console struct {
	ID, Name, Extension, Profile, Folder string
	Argv                                 []string
	Env                                  map[string]string
	Created                              time.Time
	Description                          string
	// TagID is empty when the console has no tag.
	TagID string
	// Minimized and MinimizedAt describe the membership in the view the
	// console was listed for; they are zero when listed without a view.
	Minimized   bool
	MinimizedAt time.Time
}

// Tag is a user label for consoles. Color is an index of the 256-color palette.
type Tag struct {
	ID, Name string
	Color    int
	Position int
}

const viewColumns = "id,name,folder,layout,explorer,tree,explorer_width,minimized_height,minimized_panel"

type scanner interface{ Scan(...any) error }

func scanView(r scanner) (View, error) {
	var v View
	e := r.Scan(&v.ID, &v.Name, &v.Folder, &v.Layout, &v.Explorer, &v.Tree, &v.ExplorerWidth, &v.MinimizedHeight, &v.MinimizedPanel)
	return v, e
}

func (s *Store) Views() ([]View, error) {
	r, e := s.DB.Query("SELECT " + viewColumns + " FROM views ORDER BY rowid")
	if e != nil {
		return nil, e
	}
	defer r.Close()
	var vs []View
	for r.Next() {
		v, e := scanView(r)
		if e != nil {
			return nil, e
		}
		vs = append(vs, v)
	}
	return vs, r.Err()
}
func (s *Store) View(id string) (View, error) {
	return scanView(s.DB.QueryRow("SELECT "+viewColumns+" FROM views WHERE id=?", id))
}
func (s *Store) NewView(name, folder string) (View, error) {
	v := View{ID: ID(), Name: name, Folder: folder, Layout: "tiled", Explorer: true, Tree: "{}", MinimizedPanel: true}
	_, e := s.DB.Exec("INSERT INTO views(id,name,folder,explorer) VALUES(?,?,?,1)", v.ID, v.Name, v.Folder)
	return v, e
}
func (s *Store) SetTree(view, tree string) error {
	_, e := s.DB.Exec("UPDATE views SET tree=? WHERE id=?", tree, view)
	return e
}

// UpdateView saves the name, folder, layout and panel visibility of a view.
// Panel sizes are saved by SetPanelSizes.
func (s *Store) UpdateView(v View) error {
	_, e := s.DB.Exec("UPDATE views SET name=?,folder=?,layout=?,explorer=?,minimized_panel=? WHERE id=?", v.Name, v.Folder, v.Layout, v.Explorer, v.MinimizedPanel, v.ID)
	return e
}

// SetPanelSizes saves the width of the left column and the height of the
// Minimized panel of a view. Negative values keep the stored size.
func (s *Store) SetPanelSizes(view string, explorerWidth, minimizedHeight int) error {
	_, e := s.DB.Exec(`UPDATE views SET explorer_width=CASE WHEN ?<0 THEN explorer_width ELSE ? END,
minimized_height=CASE WHEN ?<0 THEN minimized_height ELSE ? END WHERE id=?`, explorerWidth, explorerWidth, minimizedHeight, minimizedHeight, view)
	return e
}
func (s *Store) DeleteView(id string) error {
	_, e := s.DB.Exec("DELETE FROM views WHERE id=?", id)
	return e
}

const consoleColumns = "c.id,c.name,c.extension,c.profile,c.folder,c.argv,c.env,c.created,c.description,coalesce(c.tag_id,'')"

func (s *Store) queryConsoles(q string, member bool, args ...any) ([]Console, error) {
	r, e := s.DB.Query(q, args...)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	var cs []Console
	for r.Next() {
		var c Console
		var a, b, created string
		dest := []any{&c.ID, &c.Name, &c.Extension, &c.Profile, &c.Folder, &a, &b, &created, &c.Description, &c.TagID}
		var minimizedAt sql.NullString
		if member {
			dest = append(dest, &c.Minimized, &minimizedAt)
		}
		if e = r.Scan(dest...); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(a), &c.Argv); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &c.Env); e != nil {
			return nil, e
		}
		c.Created, _ = time.Parse(time.RFC3339Nano, created)
		if minimizedAt.Valid {
			c.MinimizedAt, _ = time.Parse(time.RFC3339Nano, minimizedAt.String)
		}
		cs = append(cs, c)
	}
	return cs, r.Err()
}

// Consoles lists every console when view is empty, ordered by creation, or
// the visible (not minimized) members of a view in their order.
func (s *Store) Consoles(view string) ([]Console, error) {
	if view == "" {
		return s.queryConsoles("SELECT "+consoleColumns+" FROM consoles c ORDER BY c.created", false)
	}
	return s.queryConsoles("SELECT "+consoleColumns+",m.minimized,m.minimized_at FROM consoles c JOIN members m ON m.console_id=c.id WHERE m.view_id=? AND m.minimized=0 ORDER BY m.position", true, view)
}

// Minimized lists the minimized members of a view, the most recently
// minimized first.
func (s *Store) Minimized(view string) ([]Console, error) {
	return s.queryConsoles("SELECT "+consoleColumns+",m.minimized,m.minimized_at FROM consoles c JOIN members m ON m.console_id=c.id WHERE m.view_id=? AND m.minimized=1 ORDER BY m.minimized_at DESC, m.position", true, view)
}

// Members lists every member of a view, visible or minimized, in order.
func (s *Store) Members(view string) ([]Console, error) {
	return s.queryConsoles("SELECT "+consoleColumns+",m.minimized,m.minimized_at FROM consoles c JOIN members m ON m.console_id=c.id WHERE m.view_id=? ORDER BY m.position", true, view)
}

// SetMinimized hides or shows a member of a view. Its position is kept, so a
// restored console returns to its place among the visible ones.
func (s *Store) SetMinimized(view, id string, minimized bool) error {
	var at any
	if minimized {
		at = time.Now().UTC().Format(time.RFC3339Nano)
	}
	r, e := s.DB.Exec("UPDATE members SET minimized=?,minimized_at=? WHERE view_id=? AND console_id=?", minimized, at, view, id)
	if e != nil {
		return e
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetDescription stores the free text shown under a console's name.
func (s *Store) SetDescription(id, description string) error {
	r, e := s.DB.Exec("UPDATE consoles SET description=? WHERE id=?", description, id)
	if e != nil {
		return e
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetConsoleTag tags a console; an empty tag removes it.
func (s *Store) SetConsoleTag(id, tag string) error {
	var t any
	if tag != "" {
		t = tag
	}
	r, e := s.DB.Exec("UPDATE consoles SET tag_id=? WHERE id=?", t, id)
	if e != nil {
		return e
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Tags lists the tags in their order.
func (s *Store) Tags() ([]Tag, error) {
	r, e := s.DB.Query("SELECT id,name,color,position FROM tags ORDER BY position, name")
	if e != nil {
		return nil, e
	}
	defer r.Close()
	var out []Tag
	for r.Next() {
		var t Tag
		if e = r.Scan(&t.ID, &t.Name, &t.Color, &t.Position); e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, r.Err()
}

// Tag returns one tag.
func (s *Store) Tag(id string) (Tag, error) {
	var t Tag
	e := s.DB.QueryRow("SELECT id,name,color,position FROM tags WHERE id=?", id).Scan(&t.ID, &t.Name, &t.Color, &t.Position)
	return t, e
}

// AddTag appends a tag. Names are unique.
func (s *Store) AddTag(name string, color int) (Tag, error) {
	t := Tag{ID: ID(), Name: name, Color: color}
	e := s.DB.QueryRow("INSERT INTO tags(id,name,color,position) VALUES(?,?,?,coalesce((SELECT max(position)+1 FROM tags),0)) RETURNING position", t.ID, name, color).Scan(&t.Position)
	return t, e
}

// UpdateTag renames or recolors a tag.
func (s *Store) UpdateTag(t Tag) error {
	r, e := s.DB.Exec("UPDATE tags SET name=?,color=? WHERE id=?", t.Name, t.Color, t.ID)
	if e != nil {
		return e
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteTag removes a tag; consoles that used it are left untagged.
func (s *Store) DeleteTag(id string) error {
	_, e := s.DB.Exec("DELETE FROM tags WHERE id=?", id)
	return e
}

// TagUses counts the consoles that have a tag.
func (s *Store) TagUses(id string) (int, error) {
	var n int
	e := s.DB.QueryRow("SELECT count(*) FROM consoles WHERE tag_id=?", id).Scan(&n)
	return n, e
}

// ReorderTags stores the given tags first, in that order, keeping the rest after.
func (s *Store) ReorderTags(ids []string) error {
	tags, e := s.Tags()
	if e != nil {
		return e
	}
	order := append([]string{}, ids...)
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	for _, t := range tags {
		if !seen[t.ID] {
			order = append(order, t.ID)
		}
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for i, id := range order {
		if _, e = tx.Exec("UPDATE tags SET position=? WHERE id=?", i, id); e != nil {
			return e
		}
	}
	return tx.Commit()
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
	var tag any
	if c.TagID != "" {
		tag = c.TagID
	}
	_, e = tx.Exec("INSERT INTO consoles(id,name,extension,profile,folder,argv,env,created,description,tag_id) VALUES(?,?,?,?,?,?,?,?,?,?)", c.ID, c.Name, c.Extension, c.Profile, c.Folder, string(a), string(b), time.Now().UTC().Format(time.RFC3339Nano), c.Description, tag)
	if e != nil {
		return e
	}
	_, e = tx.Exec("INSERT INTO members(view_id,console_id,position) VALUES(?,?,coalesce((SELECT max(position)+1 FROM members WHERE view_id=?),0))", view, c.ID, view)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Link(view, id string) error {
	_, e := s.DB.Exec("INSERT OR IGNORE INTO members(view_id,console_id,position) VALUES(?,?,coalesce((SELECT max(position)+1 FROM members WHERE view_id=?),0))", view, id, view)
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
	ids := make([]string, len(cs))
	for i, c := range cs {
		ids[i] = c.ID
	}
	return s.placeVisible(view, ids)
}

// placeVisible stores a new order of the visible members of a view in the
// positions they already hold, so minimized members keep their places.
func (s *Store) placeVisible(view string, order []string) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	r, e := tx.Query("SELECT position FROM members WHERE view_id=? AND minimized=0 ORDER BY position", view)
	if e != nil {
		return e
	}
	var slots []int
	for r.Next() {
		var p int
		if e = r.Scan(&p); e != nil {
			r.Close()
			return e
		}
		slots = append(slots, p)
	}
	r.Close()
	if e = r.Err(); e != nil {
		return e
	}
	// Distinct slots: older databases may repeat a position.
	for i := 1; i < len(slots); i++ {
		if slots[i] <= slots[i-1] {
			slots[i] = slots[i-1] + 1
		}
	}
	if len(slots) != len(order) {
		return errors.New("the view changed; try again")
	}
	// Move the visible members out of the way first: a minimized member may
	// hold a position after the shifted slots.
	if _, e = tx.Exec("UPDATE members SET position=-1-position WHERE view_id=? AND minimized=0", view); e != nil {
		return e
	}
	for i, id := range order {
		if _, e = tx.Exec("UPDATE members SET position=? WHERE view_id=? AND console_id=?", slots[i], view, id); e != nil {
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
	return s.placeVisible(view, order)
}

// Replace puts a new console in the place of an old one, in every view, and
// removes the old console's metadata. The tag, the description and each
// membership (position and minimized state) carry over.
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
	r, e := tx.Exec(`INSERT INTO consoles(id,name,extension,profile,folder,argv,env,created,description,tag_id)
SELECT ?,?,?,?,?,?,?,?,description,tag_id FROM consoles WHERE id=?`, c.ID, c.Name, c.Extension, c.Profile, c.Folder, string(a), string(b), time.Now().UTC().Format(time.RFC3339Nano), old)
	if e != nil {
		return e
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	if _, e = tx.Exec("UPDATE members SET console_id=? WHERE console_id=?", c.ID, old); e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM consoles WHERE id=?", old); e != nil {
		return e
	}
	return tx.Commit()
}
