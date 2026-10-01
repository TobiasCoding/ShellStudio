// SPDX-License-Identifier: GPL-3.0-only
package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestVersionOrdering(t *testing.T) {
	for _, v := range []struct {
		a, b string
		want bool
	}{{"v0.10.0", "0.9.9", true}, {"1.0.0", "0.99.99", true}, {"0.2.0", "v0.2.0", false}, {"0.1.0", "0.2.0", false}, {"v1.0.0-beta", "0.2.0", false}, {"9.0.0", "dev", false}, {"01.0.0", "0.2.0", false}} {
		if Newer(v.a, v.b) != v.want {
			t.Fatal(v)
		}
	}
}
func TestConfirmationDefaultsToNo(t *testing.T) {
	for _, s := range []string{"\n", "n\n", "yes", ""} {
		if Prompt(strings.NewReader(s), &bytes.Buffer{}, "0.1.0", "v0.2.0") {
			t.Fatal("accepted:", s)
		}
	}
	for _, s := range []string{"y\n", "YES\n"} {
		if !Prompt(strings.NewReader(s), &bytes.Buffer{}, "0.1.0", "v0.2.0") {
			t.Fatal(s)
		}
	}
}
func TestChecksumsRejectMissingDuplicateAndBad(t *testing.T) {
	hash := strings.Repeat("a", 64)
	for _, s := range []string{"", hash + " other", hash + " app\n" + hash + " app", "invalid app"} {
		if _, e := ExpectedDigest([]byte(s), "app"); e == nil {
			t.Fatal(s)
		}
	}
	got, e := ExpectedDigest([]byte(hash+" *app\n"), "app")
	if e != nil || got != hash {
		t.Fatal(got, e)
	}
}
func fixture(t *testing.T, binary []byte, version string, badHash bool) (*Client, Release) {
	t.Helper()
	name := "shellstudio-linux-" + runtime.GOARCH
	var release Release
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			json.NewEncoder(w).Encode(release)
		case "/releases/download/" + version + "/SHA256SUMS":
			hash := sha256.Sum256(binary)
			if badHash {
				hash = [32]byte{}
			}
			fmt.Fprintf(w, "%x  %s\n", hash, name)
		case "/releases/download/" + version + "/" + name:
			w.Write(binary)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := &Client{HTTP: server.Client(), API: server.URL + "/latest", Downloads: server.URL + "/releases/download/"}
	release = Release{Tag: version, Assets: []Asset{{name, client.Downloads + version + "/" + name}, {"SHA256SUMS", client.Downloads + version + "/SHA256SUMS"}}}
	return client, release
}
func TestVerifiedAtomicReplacement(t *testing.T) {
	binaryPath := filepath.Join("..", "..", "bin", "shellstudio")
	data, e := os.ReadFile(binaryPath)
	if e != nil {
		t.Fatal("run make build before updater integration tests:", e)
	}
	cmd := exec.Command(binaryPath, "--version")
	v, e := cmd.Output()
	if e != nil {
		t.Fatal(e)
	}
	version := "v" + strings.TrimSpace(strings.TrimPrefix(string(v), "ShellStudio "))
	client, release := fixture(t, data, version, false)
	r, e := client.Check(context.Background())
	if e != nil || r.Tag != release.Tag {
		t.Fatal(r, e)
	}
	dest := filepath.Join(t.TempDir(), "shellstudio")
	old := []byte("original executable")
	os.WriteFile(dest, old, 0755)
	oldHandle, e := os.Open(dest)
	if e != nil {
		t.Fatal(e)
	}
	defer oldHandle.Close()
	if e = client.Install(context.Background(), r, dest); e != nil {
		t.Fatal(e)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatal("new binary not installed")
	}
	buf := make([]byte, len(old))
	oldHandle.Read(buf)
	if !bytes.Equal(buf, old) {
		t.Fatal("overwrote the running executable inode")
	}
	info, _ := os.Stat(dest)
	if info.Mode().Perm() != 0755 {
		t.Fatal(info.Mode())
	}
}
func TestBadArtifactKeepsCurrentBinary(t *testing.T) {
	client, r := fixture(t, []byte("bad download"), "v9.0.0", true)
	dest := filepath.Join(t.TempDir(), "shellstudio")
	os.WriteFile(dest, []byte("old"), 0755)
	if e := client.Install(context.Background(), r, dest); e == nil {
		t.Fatal("accepted bad checksum")
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "old" {
		t.Fatal("changed installation on failure")
	}
	r.Assets[0].URL = "https://another.example/binary"
	if _, _, e := client.urls(r); e == nil {
		t.Fatal("accepted another origin")
	}
}
func TestOfflineAndCancelledChecks(t *testing.T) {
	c, _ := fixture(t, nil, "v1.0.0", false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, e := c.Check(ctx); e == nil {
		t.Fatal("cancelled check succeeded")
	}
	if time.Since(start) > time.Second {
		t.Fatal("check blocked")
	}
	c.API = strings.Replace(c.API, "https:", "http:", 1)
	if _, e := c.Check(context.Background()); e == nil {
		t.Fatal("accepted HTTP")
	}
}

func TestWrongBinaryVersionPreservesCurrent(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "bin", "shellstudio"))
	if err != nil {
		t.Fatal(err)
	}
	client, release := fixture(t, data, "v987.654.321", false)
	dir := t.TempDir()
	dest := filepath.Join(dir, "shellstudio")
	if err = os.WriteFile(dest, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err = client.Install(context.Background(), release, dest); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "old" {
		t.Fatal("installation changed", err)
	}
	staging, err := filepath.Glob(filepath.Join(dir, ".shellstudio-update-*"))
	if err != nil || len(staging) != 0 {
		t.Fatal(staging, err)
	}
}

func TestIncompleteReleaseRejected(t *testing.T) {
	client, release := fixture(t, nil, "v1.0.0", false)
	for _, assets := range [][]Asset{nil, release.Assets[:1], append(release.Assets, release.Assets[0])} {
		release.Assets = assets
		if _, _, err := client.urls(release); err == nil {
			t.Fatal("accepted missing/duplicate asset", assets)
		}
	}
}
