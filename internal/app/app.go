// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"shellstudio/internal/extensions"
	"shellstudio/internal/mux"
	"shellstudio/internal/platform"
	"shellstudio/internal/store"
)

type App struct {
	Paths       platform.Paths
	Store       *store.Store
	Mux         *mux.Mux
	Extensions  extensions.Manager
	CWD, Binary string
}

func Open() (*App, error) {
	p, e := platform.Discover()
	if e != nil {
		return nil, e
	}
	s, e := store.Open(filepath.Join(p.Data, "shellstudio.db"))
	if e != nil {
		return nil, e
	}
	cwd, e := os.Getwd()
	if e != nil {
		s.Close()
		return nil, e
	}
	binary, e := os.Executable()
	if e != nil {
		s.Close()
		return nil, e
	}
	a := &App{Paths: p, Store: s, CWD: cwd, Binary: binary}
	a.Mux = mux.New(p, binary)
	a.Extensions = extensions.Manager{Paths: p, Binary: binary, Log: s.Event}
	return a, nil
}
func (a *App) Close() { a.Store.Close() }
func (a *App) Manifest(id string) (extensions.Manifest, error) {
	for _, v := range extensions.Catalog(a.Paths.Config) {
		if v.Error == "" && v.Manifest.ID == id {
			return v.Manifest, nil
		}
	}
	return extensions.Manifest{}, fmt.Errorf("extension %s not found", id)
}
func (a *App) Launch(view, name, id, profile, folder string) (store.Console, error) {
	v, e := a.Store.View(view)
	if e != nil {
		return store.Console{}, e
	}
	path, e := platform.Directory(folder, v.Folder, a.CWD)
	if e != nil {
		return store.Console{}, e
	}
	var p platform.Profile
	if id == "terminal" {
		p.Executable = os.Getenv("SHELL")
		if p.Executable == "" {
			p.Executable = "/bin/sh"
		}
		p.Args = []string{"-l"}
	} else {
		man, err := a.Manifest(id)
		if err != nil {
			return store.Console{}, err
		}
		p, e = a.Extensions.Profile(man, profile)
		if e != nil {
			return store.Console{}, e
		}
	}
	if name == "" {
		name = id
	}
	if len(name) > 80 {
		return store.Console{}, errors.New("console name exceeds 80 characters")
	}
	c := store.Console{ID: store.ID(), Name: name, Extension: id, Profile: profile, Folder: path, Argv: append([]string{p.Executable}, p.Args...), Env: p.Env}
	if e = a.Store.AddConsole(view, c); e != nil {
		return c, e
	}
	if e = a.Store.Event("console-start", c.ID+" "+id); e != nil {
		return c, e
	}
	if e = a.Mux.Start(c); e != nil {
		return c, fmt.Errorf("console saved as stopped: %w", e)
	}
	if e = platform.Remember(a.Paths.Config, path); e != nil {
		return c, fmt.Errorf("console running; recent folder could not be saved: %w", e)
	}
	return c, nil
}
func (a *App) Restart(id string) error {
	c, e := a.Store.Console(id)
	if e != nil {
		return e
	}
	if e = a.Store.Event("console-restart", id); e != nil {
		return e
	}
	return a.Mux.Start(c)
}
func (a *App) Stop(id string) error {
	if e := a.Store.Event("console-stop", id); e != nil {
		return e
	}
	return a.Mux.Stop(id)
}
