// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"path/filepath"
	"shellstudio/internal/platform"
	"shellstudio/internal/store"
)

// Workspace selects a folder's existing view, or creates one without launching
// any program. The lock prevents duplicate views on simultaneous first launches.
func (a *App) Workspace(path string) (store.View, error) {
	dir, e := platform.Directory(path, "", a.CWD)
	if e != nil {
		return store.View{}, e
	}
	lock, e := platform.Lock(filepath.Join(a.Paths.Config, "workspace.lock"))
	if e != nil {
		return store.View{}, e
	}
	defer platform.Unlock(lock)
	views, e := a.Store.Views()
	if e != nil {
		return store.View{}, e
	}
	for _, v := range views {
		if v.Folder == "" {
			continue
		}
		canonical, err := platform.Directory(v.Folder, "", a.CWD)
		if err == nil && canonical == dir {
			a.CWD = dir
			return v, nil
		}
	}
	v, e := a.Store.NewView(filepath.Base(dir), dir)
	if e != nil {
		return v, e
	}
	a.CWD = dir
	return v, nil
}
