// SPDX-License-Identifier: GPL-3.0-only
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReportCommandSurvivesCorruptionAndNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	for key, dir := range map[string]string{"XDG_CONFIG_HOME": "c", "XDG_DATA_HOME": "d", "XDG_STATE_HOME": "s", "XDG_RUNTIME_DIR": "r"} {
		t.Setenv(key, filepath.Join(root, dir))
	}
	dir := filepath.Join(root, "d", "shellstudio")
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, "shellstudio.db"), []byte("broken"), 0600)
	path := filepath.Join(root, "report.json")
	if err := run([]string{"report", path}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || !json.Valid(b) {
		t.Fatal("invalid report", err)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0600 {
		t.Fatal("report permissions")
	}
	if err := run([]string{"report", path}); err == nil {
		t.Fatal("overwrote report")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(b) {
		t.Fatal("changed existing report")
	}
	if !reservedCommand("report") {
		t.Fatal("report can be confused with folder")
	}
}
