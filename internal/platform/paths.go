// SPDX-License-Identifier: GPL-3.0-only
package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Paths struct{ Config, Data, State, Runtime string }

func PrivateDir(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute: %s", path)
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("not a real directory: %s", path)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); !ok || st.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("directory is not owned by this user: %s", path)
	}
	return os.Chmod(path, 0700)
}

func Discover() (Paths, error) {
	p, err := Resolve()
	if err != nil {
		return p, err
	}
	if len(filepath.Join(p.Runtime, "programs.sock")) > 100 {
		return p, errors.New("runtime path is too long for Unix sockets; use a shorter XDG_RUNTIME_DIR")
	}
	for _, d := range []string{p.Config, p.Data, p.State, p.Runtime} {
		if err = PrivateDir(d); err != nil {
			return p, err
		}
	}
	return p, nil
}

// Resolve locates storage without creating directories or changing permissions.
// Reports use it even when normal application startup cannot open the database.
func Resolve() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	xdg := func(key, fallback string) (string, error) {
		v := os.Getenv(key)
		if v == "" {
			v = filepath.Join(home, fallback)
		}
		if !filepath.IsAbs(v) {
			return "", fmt.Errorf("%s must be absolute", key)
		}
		return filepath.Join(v, "shellstudio"), nil
	}
	var p Paths
	for _, v := range []struct {
		key, fallback string
		dest          *string
	}{
		{"XDG_CONFIG_HOME", ".config", &p.Config}, {"XDG_DATA_HOME", ".local/share", &p.Data}, {"XDG_STATE_HOME", ".local/state", &p.State},
	} {
		*v.dest, err = xdg(v.key, v.fallback)
		if err != nil {
			return p, err
		}
	}
	p.Runtime = filepath.Join(p.State, "run")
	if r := os.Getenv("XDG_RUNTIME_DIR"); r != "" {
		if !filepath.IsAbs(r) {
			return p, errors.New("XDG_RUNTIME_DIR must be absolute")
		}
		p.Runtime = filepath.Join(r, "shellstudio")
	}
	return p, nil
}

// AtomicWrite commits a same-directory replacement and syncs both file and directory.
func AtomicWrite(path string, data []byte) (err error) {
	if fi, e := os.Lstat(path); e == nil && (!fi.Mode().IsRegular() || fi.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("refusing non-regular destination: %s", path)
	} else if e != nil && !os.IsNotExist(e) {
		return e
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	defer func() { f.Close(); os.Remove(f.Name()) }()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// ReplaceFile swaps the file atomically without fsync: for preferences, where a
// lost write after a power cut costs nothing and a sync costs seconds on slow disks.
func ReplaceFile(path string, data []byte) error {
	if fi, e := os.Lstat(path); e == nil && (!fi.Mode().IsRegular() || fi.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("refusing non-regular destination: %s", path)
	} else if e != nil && !os.IsNotExist(e) {
		return e
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func WriteJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return AtomicWrite(path, append(b, '\n'))
}

func Directory(explicit, view, cwd string) (string, error) {
	p := explicit
	if p == "" {
		p = view
	}
	if p == "" {
		p = cwd
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		h, e := os.UserHomeDir()
		if e != nil {
			return "", e
		}
		p = filepath.Join(h, strings.TrimPrefix(p, "~"))
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	p, e := filepath.EvalSymlinks(p)
	if e != nil {
		return "", e
	}
	f, e := os.Open(p)
	if e != nil {
		return "", e
	}
	defer f.Close()
	s, e := f.Stat()
	if e != nil {
		return "", e
	}
	if !s.IsDir() {
		return "", fmt.Errorf("not a folder: %s", p)
	}
	if e = syscall.Access(p, 4|1); e != nil {
		return "", fmt.Errorf("folder is not readable/searchable: %w", e)
	}
	return p, nil
}

// Lock serializes cross-process configuration and view mutations, with a deadline.
func Lock(path string) (*os.File, error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, fmt.Errorf("another operation is active; retry: %w", e)
	}
	return f, nil
}

// LockWait retries until the deadline: view rendering and console starts queue
// behind each other instead of failing when two shortcuts arrive together.
func LockWait(path string, wait time.Duration) (*os.File, error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	deadline := time.Now().Add(wait)
	for {
		e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			return f, nil
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("another operation is active; retry: %w", e)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
func Unlock(f *os.File) {
	if f != nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
}
