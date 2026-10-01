// SPDX-License-Identifier: GPL-3.0-only
package extensions

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"shellstudio/internal/platform"
)

//go:embed catalog/*.json bundles/*/* schema.json
var Assets embed.FS

type Detection struct {
	Executable string   `json:"executable"`
	Args       []string `json:"args"`
}
type Artifact struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Member string `json:"member,omitempty"`
}
type Install struct {
	Kind      string              `json:"kind"`
	Artifacts map[string]Artifact `json:"artifacts,omitempty"`
}
type MCP struct {
	Name    string           `json:"name"`
	Command platform.Profile `json:"command"`
}
type Manifest struct {
	Schema        int                         `json:"schema_version"`
	ID            string                      `json:"id"`
	Version       string                      `json:"version"`
	Description   string                      `json:"description"`
	Compatibility string                      `json:"compatibility"`
	Origin        string                      `json:"origin"`
	License       string                      `json:"license"`
	Dependencies  []string                    `json:"dependencies"`
	Detection     Detection                   `json:"detection"`
	Install       Install                     `json:"installation"`
	Commands      map[string]platform.Profile `json:"commands"`
	Profiles      map[string]platform.Profile `json:"profiles"`
	Default       string                      `json:"default_profile"`
	MCP           *MCP                        `json:"mcp,omitempty"`
}

var identifier = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)
var variable = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ValidateProfile(p platform.Profile) error {
	if p.Executable == "" || strings.ContainsAny(p.Executable, "\x00\n\r") {
		return errors.New("profile needs a valid executable")
	}
	if len(p.Args) > 128 {
		return errors.New("too many arguments")
	}
	for _, a := range p.Args {
		if strings.ContainsRune(a, 0) {
			return errors.New("NUL in argument")
		}
	}
	for k, v := range p.Env {
		if !variable.MatchString(k) || strings.ContainsRune(v, 0) {
			return errors.New("invalid environment entry")
		}
	}
	return nil
}
func Parse(b []byte) (Manifest, error) {
	var m Manifest
	if len(b) > 256<<10 {
		return m, errors.New("manifest exceeds 256 KiB")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(&m); e != nil {
		return m, e
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return m, errors.New("trailing manifest data")
	}
	var fields map[string]json.RawMessage
	if e := json.Unmarshal(b, &fields); e != nil {
		return m, e
	}
	for _, key := range []string{"schema_version", "id", "version", "description", "compatibility", "origin", "license", "dependencies", "detection", "installation", "commands", "profiles", "default_profile"} {
		if raw, ok := fields[key]; !ok || string(raw) == "null" {
			return m, fmt.Errorf("missing field %s", key)
		}
	}
	if m.Schema != 1 || !identifier.MatchString(m.ID) || m.ID == "terminal" || m.Compatibility != "shellstudio-v1" || m.Version == "" || m.Description == "" || m.Origin == "" || m.License == "" {
		return m, errors.New("invalid manifest identity, compatibility, origin or license")
	}
	if len(m.Profiles) == 0 {
		return m, errors.New("manifest has no profiles")
	}
	if _, ok := m.Profiles[m.Default]; !ok {
		return m, errors.New("default profile does not exist")
	}
	for k, p := range m.Profiles {
		if !identifier.MatchString(k) {
			return m, errors.New("invalid profile name")
		}
		if e := ValidateProfile(p); e != nil {
			return m, e
		}
	}
	for _, p := range m.Commands {
		if e := ValidateProfile(p); e != nil {
			return m, e
		}
	}
	if m.Detection.Executable == "" {
		return m, errors.New("missing detection executable")
	}
	for _, dep := range m.Dependencies {
		if dep == "" || strings.ContainsAny(dep, "\x00\n\r") {
			return m, errors.New("invalid dependency")
		}
	}
	switch m.Install.Kind {
	case "builtin", "existing", "python-bundle":
	case "artifact":
		if len(m.Install.Artifacts) == 0 {
			return m, errors.New("missing artifacts")
		}
		for _, a := range m.Install.Artifacts {
			u, e := url.Parse(a.URL)
			if e != nil || u.Scheme != "https" || u.Host == "" || !digest.MatchString(a.SHA256) {
				return m, errors.New("artifact requires HTTPS and SHA-256")
			}
			if a.Member != "" && (strings.HasPrefix(a.Member, "/") || strings.Contains(a.Member, "..")) {
				return m, errors.New("invalid archive member")
			}
		}
	default:
		return m, errors.New("unsupported installation kind")
	}
	if m.MCP != nil {
		if !identifier.MatchString(m.MCP.Name) {
			return m, errors.New("invalid MCP name")
		}
		if e := ValidateProfile(m.MCP.Command); e != nil {
			return m, e
		}
	}
	return m, nil
}

type Entry struct {
	Manifest Manifest
	Source   string
	Error    string
}

func Catalog(dir string) []Entry {
	byID := map[string]Entry{}
	fs, _ := Assets.ReadDir("catalog")
	for _, f := range fs {
		b, _ := Assets.ReadFile("catalog/" + f.Name())
		m, e := Parse(b)
		if e != nil {
			byID[f.Name()] = Entry{Source: "bundled:" + f.Name(), Error: e.Error()}
		} else {
			byID[m.ID] = Entry{Manifest: m, Source: "bundled:" + f.Name()}
		}
	}
	fs2, _ := os.ReadDir(filepath.Join(dir, "extensions"))
	for _, f := range fs2 {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		p := filepath.Join(dir, "extensions", f.Name())
		b, e := ReadBounded(p, 256<<10)
		if e != nil {
			byID[f.Name()] = Entry{Source: p, Error: e.Error()}
			continue
		}
		m, e := Parse(b)
		if e != nil {
			byID[f.Name()] = Entry{Source: p, Error: e.Error()}
			continue
		}
		if _, exists := byID[m.ID]; exists {
			byID[f.Name()+"-error"] = Entry{Source: p, Error: "duplicate extension ID: " + m.ID}
			continue
		}
		byID[m.ID] = Entry{Manifest: m, Source: p}
	}
	out := []Entry{}
	for _, v := range byID {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.ID < out[j].Manifest.ID })
	return out
}
func ReadBounded(path string, n int64) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	fi, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("expected regular file")
	}
	b, e := io.ReadAll(io.LimitReader(f, n+1))
	if int64(len(b)) > n {
		return nil, errors.New("file too large")
	}
	return b, e
}
func HTTPS(raw string, limit int64) ([]byte, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return nil, errors.New("an HTTPS URL without credentials is required")
	}
	c := http.Client{Timeout: 90 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if r.URL.Scheme != "https" || len(via) > 5 {
			return errors.New("unsafe or excessive redirect")
		}
		return nil
	}}
	r, e := c.Get(raw)
	if e != nil {
		return nil, e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("download: HTTP %d", r.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if int64(len(b)) > limit {
		return nil, errors.New("download too large")
	}
	return b, e
}
func ImportPreview(source string) (Manifest, error) {
	var b []byte
	var e error
	if strings.HasPrefix(source, "https://") {
		b, e = HTTPS(source, 256<<10)
	} else {
		b, e = ReadBounded(source, 256<<10)
	}
	if e != nil {
		return Manifest{}, e
	}
	return Parse(b)
}
func Import(dir string, m Manifest) error {
	for _, entry := range Catalog(dir) {
		if entry.Manifest.ID == m.ID {
			return errors.New("extension ID already exists")
		}
	}
	if m.Install.Kind == "builtin" || m.Install.Kind == "python-bundle" {
		return errors.New("reserved installation kind")
	}
	p := filepath.Join(dir, "extensions")
	if e := platform.PrivateDir(p); e != nil {
		return e
	}
	return platform.WriteJSON(filepath.Join(p, m.ID+".json"), m)
}
