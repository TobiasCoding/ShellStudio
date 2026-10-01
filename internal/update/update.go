// SPDX-License-Identifier: GPL-3.0-only
// Package update installs only reviewed, checksum-verified GitHub release assets.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"shellstudio/internal/platform"
	"shellstudio/internal/runner"
)

const Repository = "TobiasCoding/ShellStudio"
const maxBinary = 128 << 20

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}
type Release struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}
type Client struct {
	HTTP           *http.Client
	API, Downloads string
}

func NewClient() *Client {
	return &Client{
		HTTP: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || len(via) > 5 {
				return errors.New("unsafe or excessive update redirect")
			}
			return nil
		}},
		API:       "https://api.github.com/repos/" + Repository + "/releases/latest",
		Downloads: "https://github.com/" + Repository + "/releases/download/",
	}
}

var stable = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func versionParts(s string) ([3]uint64, error) {
	var out [3]uint64
	if !stable.MatchString(s) {
		return out, fmt.Errorf("not a stable version: %q", s)
	}
	for i, p := range strings.Split(strings.TrimPrefix(s, "v"), ".") {
		n, e := strconv.ParseUint(p, 10, 64)
		if e != nil {
			return out, e
		}
		out[i] = n
	}
	return out, nil
}
func Newer(candidate, current string) bool {
	a, e := versionParts(candidate)
	if e != nil {
		return false
	}
	b, e := versionParts(current)
	if e != nil {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}
func (c *Client) get(ctx context.Context, raw string, limit int64) ([]byte, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil {
		return nil, errors.New("update URL must use HTTPS without credentials")
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("User-Agent", "ShellStudio-updater")
	req.Header.Set("Accept", "application/vnd.github+json")
	res, e := c.HTTP.Do(req)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		return nil, errors.New("no public release found; the repository may be private or have no published release")
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned HTTP %d", res.StatusCode)
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if e != nil {
		return nil, e
	}
	if int64(len(data)) > limit {
		return nil, errors.New("release response exceeds size limit")
	}
	return data, nil
}
func (c *Client) Check(ctx context.Context) (Release, error) {
	var r Release
	b, e := c.get(ctx, c.API, 1<<20)
	if e != nil {
		return r, e
	}
	if e = json.Unmarshal(b, &r); e != nil {
		return r, e
	}
	if _, e = versionParts(r.Tag); e != nil {
		return r, e
	}
	if r.Draft || r.Prerelease {
		return r, errors.New("release is not stable")
	}
	if _, _, e = c.urls(r); e != nil {
		return r, e
	}
	return r, nil
}
func (c *Client) urls(r Release) (binary, checksums string, err error) {
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return "", "", errors.New("updates support Linux amd64/arm64")
	}
	if _, e := versionParts(r.Tag); e != nil {
		return "", "", e
	}
	name := "shellstudio-linux-" + runtime.GOARCH
	for _, a := range r.Assets {
		if a.Name != name && a.Name != "SHA256SUMS" {
			continue
		}
		if a.URL != c.Downloads+r.Tag+"/"+a.Name {
			return "", "", errors.New("release asset is outside the expected repository/tag")
		}
		if a.Name == name {
			if binary != "" {
				return "", "", errors.New("duplicate binary asset")
			}
			binary = a.URL
		} else {
			if checksums != "" {
				return "", "", errors.New("duplicate checksum asset")
			}
			checksums = a.URL
		}
	}
	if binary == "" || checksums == "" {
		return "", "", errors.New("release is missing this platform's binary or SHA256SUMS")
	}
	return binary, checksums, nil
}
func ExpectedDigest(data []byte, name string) (string, error) {
	found := ""
	s := bufio.NewScanner(strings.NewReader(string(data)))
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		if found != "" {
			return "", errors.New("duplicate checksum entry")
		}
		d, e := hex.DecodeString(fields[0])
		if e != nil || len(d) != sha256.Size {
			return "", errors.New("invalid SHA-256 entry")
		}
		found = strings.ToLower(fields[0])
	}
	if e := s.Err(); e != nil {
		return "", e
	}
	if found == "" {
		return "", errors.New("binary checksum is missing")
	}
	return found, nil
}

// Install uses a same-directory replacement. Running UIs and tmux processes keep
// their old inode; a failed download/verification never removes the current binary.
func (c *Client) Install(ctx context.Context, r Release, destination string) error {
	info, e := os.Lstat(destination)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() {
		return errors.New("update destination must be a regular executable")
	}
	lock, e := platform.Lock(filepath.Join(filepath.Dir(destination), ".shellstudio-install.lock"))
	if e != nil {
		return fmt.Errorf("cannot update this installation: %w", e)
	}
	defer platform.Unlock(lock)
	binaryURL, sumURL, e := c.urls(r)
	if e != nil {
		return e
	}
	sums, e := c.get(ctx, sumURL, 1<<20)
	if e != nil {
		return e
	}
	digest, e := ExpectedDigest(sums, "shellstudio-linux-"+runtime.GOARCH)
	if e != nil {
		return e
	}
	data, e := c.get(ctx, binaryURL, maxBinary)
	if e != nil {
		return e
	}
	h := sha256.Sum256(data)
	if hex.EncodeToString(h[:]) != digest {
		return errors.New("update checksum mismatch; current installation kept")
	}
	if len(data) < 20 || string(data[:4]) != "\x7fELF" {
		return errors.New("release asset is not a Linux executable")
	}
	f, e := os.CreateTemp(filepath.Dir(destination), ".shellstudio-update-")
	if e != nil {
		return e
	}
	defer func() { f.Close(); os.Remove(f.Name()) }()
	if _, e = f.Write(data); e != nil {
		return e
	}
	if e = f.Chmod(0755); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	actual, e := runner.Run(5*time.Second, "", nil, f.Name(), "--version")
	if e != nil {
		return fmt.Errorf("new executable failed verification: %w", e)
	}
	if strings.TrimSpace(actual) != "ShellStudio "+strings.TrimPrefix(r.Tag, "v") {
		return errors.New("new executable does not match the release version")
	}
	if e = os.Rename(f.Name(), destination); e != nil {
		return e
	}
	dir, e := os.Open(filepath.Dir(destination))
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
}

// Prompt deliberately reads from the provided terminal, never an install pipe.
func Prompt(in io.Reader, out io.Writer, current, candidate string) bool {
	fmt.Fprintf(out, "ShellStudio %s is available (installed: %s). Install now? [y/N] ", strings.TrimPrefix(candidate, "v"), current)
	line, e := bufio.NewReader(io.LimitReader(in, 1024)).ReadString('\n')
	if e != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}
