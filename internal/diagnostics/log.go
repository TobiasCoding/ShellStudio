// SPDX-License-Identifier: GPL-3.0-only
// Package diagnostics records operational metadata, never terminal content,
// arbitrary arguments, environment values or error messages containing user data.
package diagnostics

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"shellstudio/internal/platform"
)

const MaxLogBytes = 2 * 1024 * 1024
const LogName = "diagnostics.jsonl"

type Entry struct {
	At         string   `json:"at"`
	Version    string   `json:"version"`
	PID        int      `json:"pid"`
	Event      string   `json:"event"`
	Operation  string   `json:"operation,omitempty"`
	Server     string   `json:"server,omitempty"`
	Reference  string   `json:"reference,omitempty"`
	Error      string   `json:"error,omitempty"`
	DurationMS int64    `json:"duration_ms"`
	Commands   int      `json:"commands,omitempty"`
	Site       string   `json:"site,omitempty"`
	Trace      []string `json:"trace,omitempty"`
}

type Logger struct{ Dir, Version string }

var active atomic.Pointer[Logger]

func Start(version string) error {
	p, err := platform.Resolve()
	if err != nil {
		return err
	}
	if err = platform.PrivateDir(p.State); err != nil {
		return err
	}
	active.Store(&Logger{p.State, version})
	return nil
}

// Classify deliberately discards the original message: it can contain a shell
// command, URL credentials, a filename or text pasted into a dialog.
func Classify(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, os.ErrPermission):
		return "permission-denied"
	case errors.Is(err, os.ErrNotExist):
		return "not-found"
	}
	s := strings.ToLower(err.Error())
	for _, rule := range [][2]string{
		{"no server running", "server-absent"}, {"error connecting to", "socket-connect"},
		{"can't find", "target-missing"}, {"no current", "target-missing"},
		{"timed out", "timeout"}, {"in time", "timeout"}, {"deadline exceeded", "timeout"},
		{"locked", "locked"}, {"busy", "busy"}, {"permission", "permission-denied"},
		{"no space left", "disk-full"}, {"malformed", "malformed"}, {"corrupt", "corrupt"},
		{"syntax", "syntax"}, {"unknown command", "unknown-command"},
		{"too small", "size-too-small"}, {"needs a terminal", "terminal-required"},
		{"newer than", "schema-too-new"}, {"not found", "not-found"},
	} {
		if strings.Contains(s, rule[0]) {
			return rule[1]
		}
	}
	return "operation-failed"
}

func Record(entry Entry, err error, started time.Time) {
	l := active.Load()
	if l == nil {
		return
	}
	entry.Error = Classify(err)
	entry.Reference = safeID(entry.Reference)
	if !started.IsZero() {
		entry.DurationMS = time.Since(started).Milliseconds()
	}
	if err != nil {
		if pc, _, line, ok := runtime.Caller(1); ok {
			entry.Site = fmt.Sprintf("%s:%d", runtime.FuncForPC(pc).Name(), line)
		}
		var pcs [12]uintptr
		frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs[:])])
		for {
			frame, more := frames.Next()
			entry.Trace = append(entry.Trace, fmt.Sprintf("%s:%d", frame.Function, frame.Line))
			if !more {
				break
			}
		}
	}
	_ = l.Write(entry) // A logging failure must never break a workspace.
}

// Write serializes rotation across the short-lived tmux helper processes. Locks
// are nonblocking: a busy or unavailable log must not stall input handling.
func (l *Logger) Write(entry Entry) error {
	lock, err := privateFile(filepath.Join(l.Dir, "diagnostics.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	entry.At, entry.Version, entry.PID = time.Now().UTC().Format(time.RFC3339Nano), l.Version, os.Getpid()
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if len(b) > 8192 {
		return errors.New("diagnostic entry too large")
	}
	path := filepath.Join(l.Dir, LogName)
	f, err := privateFile(path)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	if fi.Size()+int64(len(b)+1) > MaxLogBytes {
		f.Close()
		if err = os.Rename(path+".1", path+".2"); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err = os.Rename(path, path+".1"); err != nil {
			return err
		}
		f, err = privateFile(path)
		if err != nil {
			return err
		}
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

func privateFile(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_APPEND|syscall.O_WRONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	fi, err := f.Stat()
	if err == nil && (!fi.Mode().IsRegular() || fi.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid())) {
		err = errors.New("not an owned regular log file")
	}
	if err == nil {
		err = f.Chmod(0600)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// Command identifies the action without retaining arbitrary CLI arguments.
func Command(args []string) string {
	if len(args) == 0 {
		return "workspace"
	}
	known := " open --menu update version --version help --help -h schema validate doctor report backup views consoles new-view launch restart stop kill extensions notes viewer _exec _empty _disconnected _explorer _close _sync _view _dialog _action _resize _resize_window "
	if !strings.Contains(known, " "+args[0]+" ") {
		return "workspace-or-unknown"
	}
	cmd := args[0]
	if len(args) > 1 && strings.HasPrefix(cmd, "_") {
		actions := " next previous layout drop move-before move-after ports upload move rename explorer remove kill start drag end menu consoles new views arrange resize "
		if strings.Contains(actions, " "+args[1]+" ") {
			cmd += ":" + args[1]
		}
	}
	return cmd
}
