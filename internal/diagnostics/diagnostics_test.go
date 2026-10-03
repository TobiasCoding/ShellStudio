// SPDX-License-Identifier: GPL-3.0-only
package diagnostics

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"shellstudio/internal/platform"
	"shellstudio/internal/store"
)

func isolated(t *testing.T) platform.Paths {
	t.Helper()
	root := t.TempDir()
	for key, dir := range map[string]string{"XDG_CONFIG_HOME": "c", "XDG_DATA_HOME": "d", "XDG_STATE_HOME": "s", "XDG_RUNTIME_DIR": "r"} {
		t.Setenv(key, filepath.Join(root, dir))
	}
	p, err := platform.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReportWithoutStateDoesNotInitializeApplication(t *testing.T) {
	p := isolated(t)
	r := Collect("test")
	if r.Sections["database"].Status != "not-found" || r.Sections["views"].Status != "server-absent" {
		t.Fatal(r.Sections)
	}
	for _, dir := range []string{p.Config, p.Data, p.State, p.Runtime} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("report created %s", dir)
		}
	}
}

func TestReportPrivateDataAndBrokenStartup(t *testing.T) {
	p := isolated(t)
	s, err := store.Open(filepath.Join(p.Data, "shellstudio.db"))
	if err != nil {
		t.Fatal(err)
	}
	secret := "PRIVATE-DIAGNOSTIC-FIXTURE"
	v, err := s.NewView(secret, "/"+secret)
	if err != nil {
		t.Fatal(err)
	}
	c := store.Console{ID: store.ID(), Name: secret, Folder: "/" + secret, Extension: "terminal", Argv: []string{"sh", secret}, Env: map[string]string{"PASSWORD": secret}}
	if err = s.AddConsole(v.ID, c); err != nil {
		t.Fatal(err)
	}
	s.Event("error", secret)
	s.Close()
	platform.PrivateDir(p.Config)
	os.WriteFile(filepath.Join(p.Config, "config.json"), []byte(secret), 0600)
	r := Collect("test")
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) || strings.Contains(string(b), p.Data) {
		t.Fatal("report leaked private data")
	}
	if r.Sections["database"].Status != "ok" || len(r.Sections["consoles"].Rows) != 1 || r.Sections["configuration"].Status == "ok" {
		t.Fatal(r.Sections)
	}
	os.WriteFile(filepath.Join(p.Data, "shellstudio.db"), []byte("broken database"), 0600)
	r = Collect("test")
	if r.Sections["database"].Status == "ok" {
		t.Fatal("corruption not reported")
	}
	if _, ok := r.Sections["logs"]; !ok {
		t.Fatal("corruption stopped collection")
	}
}

func TestLogRotationPermissionsAndUnavailableStorage(t *testing.T) {
	dir := t.TempDir()
	l := &Logger{dir, "test"}
	path := filepath.Join(dir, LogName)
	for i := 0; i < 4; i++ {
		if err := os.WriteFile(path, []byte(strings.Repeat("x", MaxLogBytes)), 0644); err != nil {
			t.Fatal(err)
		}
		if err := l.Write(Entry{Event: "rotation"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, suffix := range []string{"", ".1", ".2"} {
		fi, err := os.Stat(path + suffix)
		if err != nil || fi.Size() > MaxLogBytes || fi.Mode().Perm() != 0600 {
			t.Fatal(fi, err)
		}
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatal("unbounded rotation")
	}
	lock, err := privateFile(filepath.Join(dir, "diagnostics.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err = l.Write(Entry{Event: "busy"}); err == nil {
		t.Fatal("ignored contention")
	}
	syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	os.Remove(path)
	target := filepath.Join(dir, "private")
	os.WriteFile(target, []byte("unchanged"), 0600)
	os.Symlink(target, path)
	if err = l.Write(Entry{Event: "symlink"}); err == nil {
		t.Fatal("followed log symlink")
	}
	b, _ := os.ReadFile(target)
	if string(b) != "unchanged" {
		t.Fatal("modified symlink target")
	}
}

func TestOperationalLogsDoNotRecordArgumentsOrErrors(t *testing.T) {
	p := isolated(t)
	if err := Start("test"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { active.Store(nil) })
	secret := "PRIVATE-DIAGNOSTIC-FIXTURE"
	Record(Entry{Event: "command-end", Operation: Command([]string{"_view", secret}), Reference: secret}, errors.New("permission denied: "+secret), time.Now())
	r := Collect("test")
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), secret) {
		t.Fatal("secret leaked")
	}
	if len(r.Logs) != 1 || r.Logs[0].Error != "permission-denied" || len(r.Logs[0].Trace) == 0 {
		t.Fatal(r.Logs)
	}
	if _, err := os.Stat(filepath.Join(p.State, LogName)); err != nil {
		t.Fatal(err)
	}
}
