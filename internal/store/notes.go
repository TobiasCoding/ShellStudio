// SPDX-License-Identifier: GPL-3.0-only
package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Note struct {
	ID, Title, Body string
	Revision        int64
	Trashed         bool
	Updated         string
}

func (s *Store) Notes(search string, trash bool) ([]Note, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r, e := s.DB.QueryContext(ctx, "SELECT id,substr(title,1,200),'',revision,trashed,updated FROM notes WHERE trashed=? AND (instr(lower(title),lower(?))>0 OR instr(lower(body),lower(?))>0) ORDER BY updated DESC LIMIT 200", trash, search, search)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	var ns []Note
	for r.Next() {
		var n Note
		if e = r.Scan(&n.ID, &n.Title, &n.Body, &n.Revision, &n.Trashed, &n.Updated); e != nil {
			return nil, e
		}
		ns = append(ns, n)
	}
	return ns, r.Err()
}
func (s *Store) Note(id string) (Note, error) {
	var n Note
	e := s.DB.QueryRow("SELECT id,title,body,revision,trashed,updated FROM notes WHERE id=?", id).Scan(&n.ID, &n.Title, &n.Body, &n.Revision, &n.Trashed, &n.Updated)
	return n, e
}
func (s *Store) NewNote(title string) (Note, error) {
	n := Note{ID: ID(), Title: title, Revision: 1, Updated: time.Now().UTC().Format(time.RFC3339Nano)}
	_, e := s.DB.Exec("INSERT INTO notes(id,title,body,updated) VALUES(?,?,'',?)", n.ID, n.Title, n.Updated)
	return n, e
}

// SaveNote is compare-and-swap. A stale editor creates a sibling; neither edit is lost.
// The returned revision is valid only after Commit succeeds.
func (s *Store) SaveNote(n Note) (saved Note, conflict bool, err error) {
	if len(n.Body) > 4<<20 {
		return n, false, errors.New("note exceeds 4 MiB")
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return n, false, e
	}
	defer tx.Rollback()
	n.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	r, e := tx.Exec("UPDATE notes SET title=?,body=?,revision=revision+1,updated=? WHERE id=? AND revision=? AND trashed=0", n.Title, n.Body, n.Updated, n.ID, n.Revision)
	if e != nil {
		return n, false, e
	}
	count, e := r.RowsAffected()
	if e != nil {
		return n, false, e
	}
	if count == 0 {
		var found int
		e = tx.QueryRow("SELECT 1 FROM notes WHERE id=?", n.ID).Scan(&found)
		if e != nil && e != sql.ErrNoRows {
			return n, false, e
		}
		n.ID = ID()
		n.Revision = 1
		n.Title += " (conflict copy)"
		n.Trashed = false
		conflict = true
		_, e = tx.Exec("INSERT INTO notes(id,title,body,revision,updated) VALUES(?,?,?,?,?)", n.ID, n.Title, n.Body, n.Revision, n.Updated)
		if e != nil {
			return n, false, e
		}
	} else {
		n.Revision++
	}
	if e = tx.Commit(); e != nil {
		return n, false, e
	}
	return n, conflict, nil
}
func (s *Store) Trash(id string, trash bool) error {
	_, e := s.DB.Exec("UPDATE notes SET trashed=?,revision=revision+1,updated=? WHERE id=?", trash, time.Now().UTC().Format(time.RFC3339Nano), id)
	return e
}
