// SPDX-License-Identifier: GPL-3.0-only
package extensions

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"shellstudio/internal/platform"
	"shellstudio/internal/runner"
)

type Manager struct {
	Paths  platform.Paths
	Binary string
	Log    func(string, string) error
}
type Receipt struct{ ID, Version, Root, ManifestHash, VerifiedAt string }

func (m Manager) receipt(id string) (Receipt, error) {
	var r Receipt
	b, e := ReadBounded(filepath.Join(m.Paths.Data, "extensions", id+".json"), 65536)
	if e != nil {
		return r, e
	}
	e = json.Unmarshal(b, &r)
	return r, e
}
func (m Manager) Root(id string) string {
	r, e := m.receipt(id)
	if e == nil {
		return r.Root
	}
	return filepath.Join(m.Paths.Data, "extensions", id, "not-installed")
}
func (m Manager) Expand(id string, p platform.Profile) platform.Profile {
	x := func(s string) string {
		return strings.NewReplacer("{extension}", m.Root(id), "{data}", filepath.Join(m.Paths.Data, "extension-data", id), "{shellstudio}", m.Binary).Replace(s)
	}
	out := platform.Profile{Executable: x(p.Executable), Env: map[string]string{}}
	for _, a := range p.Args {
		out.Args = append(out.Args, x(a))
	}
	for k, v := range p.Env {
		out.Env[k] = x(v)
	}
	return out
}
func (m Manager) Profile(man Manifest, name string) (platform.Profile, error) {
	c, e := platform.LoadConfig(m.Paths.Config)
	if e != nil {
		return platform.Profile{}, e
	}
	ec, configured := c.Extensions[man.ID]
	if name == "" {
		name = man.Default
		if ec.Default != "" {
			name = ec.Default
		}
	}
	p, ok := ec.Profiles[name]
	if !ok {
		p, ok = man.Profiles[name]
	}
	if !ok {
		return p, errors.New("profile not found")
	}
	if configured && !ec.Enabled {
		return p, errors.New("extension is disabled")
	}
	if !configured && man.ID != "notes" {
		return p, errors.New("extension must be installed or enabled first")
	}
	if e = ValidateProfile(p); e != nil {
		return p, e
	}
	return m.Expand(man.ID, p), nil
}
func (m Manager) Enabled(man Manifest) bool {
	c, e := platform.LoadConfig(m.Paths.Config)
	if e != nil {
		return false
	}
	v, ok := c.Extensions[man.ID]
	return (ok && v.Enabled) || (!ok && man.ID == "notes")
}
func (m Manager) Enable(man Manifest, enabled bool) error {
	if enabled {
		exe, e := m.Detect(man)
		if e != nil {
			return e
		}
		if len(man.Detection.Args) > 0 {
			if _, e = runner.Run(15*time.Second, "", nil, exe, man.Detection.Args...); e != nil {
				return e
			}
		}
	}
	return platform.UpdateConfig(m.Paths.Config, func(c *platform.Config) error {
		v := c.Extensions[man.ID]
		v.Enabled = enabled
		if v.Default == "" {
			v.Default = man.Default
		}
		c.Extensions[man.ID] = v
		return nil
	})
}
func (m Manager) Detect(man Manifest) (string, error) {
	exe := m.Expand(man.ID, platform.Profile{Executable: man.Detection.Executable}).Executable
	if r, e := m.receipt(man.ID); e == nil && man.Install.Kind == "artifact" {
		exe = filepath.Join(r.Root, "bin", man.ID)
	}
	if man.Install.Kind == "artifact" || man.Install.Kind == "existing" {
		if c, e := platform.LoadConfig(m.Paths.Config); e == nil {
			v := c.Extensions[man.ID]
			if p, ok := v.Profiles[v.Default]; ok {
				exe = m.Expand(man.ID, p).Executable
			}
		}
	}
	return exec.LookPath(exe)
}
func (m Manager) Preview(man Manifest) string {
	b, _ := json.MarshalIndent(man.Install, "", "  ")
	d, _ := json.MarshalIndent(man.Detection, "", "  ")
	profiles, _ := json.MarshalIndent(man.Profiles, "", "  ")
	return fmt.Sprintf("%s %s\nOrigin: %s\nLicense: %s\nDependencies: %s\nInstall: %s\nVerification command: %s\nLaunch profiles: %s\nPrivate destination: %s\nVerify executable before activation. No automatic updates.\n", man.ID, man.Version, man.Origin, man.License, strings.Join(man.Dependencies, ", "), b, d, profiles, filepath.Join(m.Paths.Data, "extensions"))
}
func (m Manager) Install(man Manifest) (err error) {
	defer func() {
		if m.Log != nil {
			state := "verified"
			if err != nil {
				state = err.Error()
			}
			m.Log("extension-install", man.ID+": "+state)
		}
	}()
	base := filepath.Join(m.Paths.Data, "extensions")
	if e := platform.PrivateDir(base); e != nil {
		return e
	}
	lock, e := platform.Lock(filepath.Join(base, "install.lock"))
	if e != nil {
		return e
	}
	defer platform.Unlock(lock)
	for _, dep := range man.Dependencies {
		if _, e := exec.LookPath(dep); e != nil {
			return fmt.Errorf("missing dependency %s", dep)
		}
	}
	if man.Install.Kind == "existing" || man.Install.Kind == "builtin" {
		return m.Enable(man, true)
	}
	b, _ := json.Marshal(man)
	sum := sha256.Sum256(b)
	hash := hex.EncodeToString(sum[:])
	parent := filepath.Join(base, man.ID)
	if e = platform.PrivateDir(parent); e != nil {
		return e
	}
	final := filepath.Join(parent, hash[:16])
	stage, e := os.MkdirTemp(parent, ".install-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	final += "-" + strings.TrimPrefix(filepath.Base(stage), ".install-")
	var executable string
	var verify []string
	switch man.Install.Kind {
	case "artifact":
		a, ok := man.Install.Artifacts[runtime.GOOS+"/"+runtime.GOARCH]
		if !ok {
			return errors.New("no artifact for this platform")
		}
		archive, e := HTTPS(a.URL, 256<<20)
		if e != nil {
			return e
		}
		h := sha256.Sum256(archive)
		if hex.EncodeToString(h[:]) != a.SHA256 {
			return errors.New("artifact SHA-256 mismatch")
		}
		data := archive
		if a.Member != "" {
			g, e := gzip.NewReader(bytes.NewReader(archive))
			if e != nil {
				return e
			}
			defer g.Close()
			tr := tar.NewReader(g)
			found := false
			for {
				hdr, e := tr.Next()
				if e == io.EOF {
					break
				}
				if e != nil {
					return e
				}
				if hdr.Name != a.Member {
					continue
				}
				if hdr.Typeflag != tar.TypeReg || hdr.Size > 512<<20 {
					return errors.New("invalid artifact member")
				}
				data, e = io.ReadAll(io.LimitReader(tr, 512<<20+1))
				if e != nil {
					return e
				}
				found = true
				break
			}
			if !found {
				return errors.New("artifact member not found")
			}
		}
		if e = platform.PrivateDir(filepath.Join(stage, "bin")); e != nil {
			return e
		}
		executable = filepath.Join(stage, "bin", man.ID)
		if e = platform.AtomicWrite(executable, data); e != nil {
			return e
		}
		if e = os.Chmod(executable, 0700); e != nil {
			return e
		}
		verify = man.Detection.Args
	case "python-bundle":
		if man.ID != "agent-chat" && man.ID != "agent-gantt" {
			return errors.New("unknown bundled Python package")
		}
		if _, e = runner.Run(60*time.Second, "", nil, "python3", "-I", "-m", "venv", filepath.Join(stage, "venv")); e != nil {
			return e
		}
		wheel, e := bundleWheel(man.ID)
		if e != nil {
			return e
		}
		wheelPath := filepath.Join(stage, strings.ReplaceAll(man.ID, "-", "_")+"_mcp-2.0.0-py3-none-any.whl")
		if e = platform.AtomicWrite(wheelPath, wheel); e != nil {
			return e
		}
		executable = filepath.Join(stage, "venv", "bin", "python")
		if _, e = runner.Run(60*time.Second, "", nil, executable, "-I", "-m", "pip", "install", "--disable-pip-version-check", "--no-index", "--no-deps", wheelPath); e != nil {
			return e
		}
		verify = []string{"-I", "-c", "import server; assert server.VERSION == '2.0.0'"}
	default:
		return errors.New("unsupported installer")
	}
	if _, e = runner.Run(15*time.Second, "", nil, executable, verify...); e != nil {
		return fmt.Errorf("installation verification: %w", e)
	}
	if _, e = os.Stat(final); e == nil {
		return errors.New("this version already exists; enable it or uninstall before reinstalling")
	}
	if e = os.Rename(stage, final); e != nil {
		return e
	}
	if e = platform.PrivateDir(filepath.Join(m.Paths.Data, "extension-data", man.ID)); e != nil {
		return e
	}
	r := Receipt{man.ID, man.Version, final, hash, time.Now().UTC().Format(time.RFC3339)}
	if e = platform.WriteJSON(filepath.Join(base, man.ID+".json"), r); e != nil {
		return e
	}
	return platform.UpdateConfig(m.Paths.Config, func(c *platform.Config) error {
		v := c.Extensions[man.ID]
		v.Enabled = true
		if v.Default == "" {
			v.Default = man.Default
		}
		if man.Install.Kind == "artifact" {
			if v.Profiles == nil {
				v.Profiles = map[string]platform.Profile{}
			}
			p := man.Profiles[man.Default]
			p.Executable = filepath.Join(final, "bin", man.ID)
			v.Profiles[man.Default] = p
		}
		c.Extensions[man.ID] = v
		return nil
	})
}
func bundleWheel(id string) ([]byte, error) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	dist := strings.ReplaceAll(id, "-", "_") + "_mcp-2.0.0.dist-info"
	files, e := Assets.ReadDir("bundles/" + id)
	if e != nil {
		return nil, e
	}
	record := ""
	add := func(name string, data []byte) error {
		f, e := z.Create(name)
		if e != nil {
			return e
		}
		_, e = f.Write(data)
		record += name + ",,\n"
		return e
	}
	for _, f := range files {
		data, e := Assets.ReadFile("bundles/" + id + "/" + f.Name())
		if e != nil {
			return nil, e
		}
		name := f.Name()
		if name == "LICENSE" {
			name = dist + "/LICENSE"
		}
		if e = add(name, data); e != nil {
			return nil, e
		}
	}
	if e = add(dist+"/METADATA", []byte("Metadata-Version: 2.1\nName: "+id+"-mcp\nVersion: 2.0.0\nLicense: MIT\nRequires-Python: >=3.9\n")); e != nil {
		return nil, e
	}
	if e = add(dist+"/WHEEL", []byte("Wheel-Version: 1.0\nGenerator: ShellStudio\nRoot-Is-Purelib: true\nTag: py3-none-any\n")); e != nil {
		return nil, e
	}
	if e = add(dist+"/RECORD", []byte(record+dist+"/RECORD,,\n")); e != nil {
		return nil, e
	}
	if e = z.Close(); e != nil {
		return nil, e
	}
	return b.Bytes(), nil
}
func (m Manager) Uninstall(man Manifest, purge bool) error {
	if e := m.Enable(man, false); e != nil {
		return e
	}
	// Keep executables while consoles may still use them. Disabling is always safe.
	if e := os.Remove(filepath.Join(m.Paths.Data, "extensions", man.ID+".json")); e != nil && !os.IsNotExist(e) {
		return e
	}
	if purge {
		if e := os.RemoveAll(filepath.Join(m.Paths.Data, "extension-data", man.ID)); e != nil {
			return e
		}
	}
	if m.Log != nil {
		return m.Log("extension-uninstall", man.ID+" disabled; immutable package retained for running consoles")
	}
	return nil
}
