// SPDX-License-Identifier: GPL-3.0-only
package mcp

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/pelletier/go-toml/v2"
	"shellstudio/internal/platform"
)

type Plan struct {
	Client, Path, Name, Preview string
	OriginalHash                [32]byte
	Original, Updated           []byte
	Exists                      bool
}

func ClientPath(client string) (string, error) {
	home, e := os.UserHomeDir()
	if e != nil {
		return "", e
	}
	switch client {
	case "claude":
		if p := os.Getenv("CLAUDE_CONFIG_DIR"); p != "" {
			return filepath.Join(p, ".claude.json"), nil
		}
		return filepath.Join(home, ".claude.json"), nil
	case "codex":
		dir := os.Getenv("CODEX_HOME")
		if dir == "" {
			dir = filepath.Join(home, ".codex")
		}
		return filepath.Join(dir, "config.toml"), nil
	}
	return "", errors.New("unsupported MCP client")
}
func Prepare(client, path, name string, p platform.Profile) (Plan, error) {
	plan := Plan{Client: client, Path: path, Name: name}
	fi, e := os.Lstat(path)
	if e == nil {
		if !fi.Mode().IsRegular() {
			return plan, errors.New("MCP configuration must be a regular file")
		}
		if fi.Size() > 4<<20 {
			return plan, errors.New("MCP configuration exceeds 4 MiB")
		}
	} else if !os.IsNotExist(e) {
		return plan, e
	}
	old, e := os.ReadFile(path)
	if e != nil && !os.IsNotExist(e) {
		return plan, e
	}
	plan.Exists = e == nil
	plan.Original = old
	plan.OriginalHash = sha256.Sum256(old)
	entry := map[string]any{"command": p.Executable, "args": p.Args}
	if len(p.Env) > 0 {
		entry["env"] = p.Env
	}
	eb, _ := json.MarshalIndent(entry, "", "  ")
	plan.Preview = fmt.Sprintf("Client: %s\nFile: %s\nServer: %s (stdio)\n%s\nAn existing different entry will never be replaced. A private backup is required.\n", client, path, name, eb)
	root := map[string]any{}
	switch client {
	case "claude":
		if len(old) > 0 {
			d := json.NewDecoder(bytes.NewReader(old))
			d.UseNumber()
			if e = d.Decode(&root); e != nil {
				return plan, e
			}
		}
		if root == nil {
			return plan, errors.New("configuration root must be an object")
		}
		servers := map[string]any{}
		if x, ok := root["mcpServers"]; ok {
			var valid bool
			servers, valid = x.(map[string]any)
			if !valid {
				return plan, errors.New("mcpServers is not an object")
			}
		}
		if x, ok := servers[name]; ok {
			a, _ := json.Marshal(x)
			b, _ := json.Marshal(entry)
			if !bytes.Equal(a, b) {
				return plan, errors.New("MCP name conflict: " + name)
			}
			plan.Updated = old
			return plan, nil
		}
		servers[name] = entry
		root["mcpServers"] = servers
		plan.Updated, e = json.MarshalIndent(root, "", "  ")
		plan.Updated = append(plan.Updated, '\n')
		return plan, e
	case "codex":
		if len(old) > 0 {
			if e = toml.Unmarshal(old, &root); e != nil {
				return plan, e
			}
		}
		if x, ok := root["mcp_servers"]; ok {
			servers, valid := x.(map[string]any)
			if !valid {
				return plan, errors.New("mcp_servers is not a table")
			}
			if x, ok := servers[name]; ok {
				a, _ := json.Marshal(x)
				b, _ := json.Marshal(entry)
				if !bytes.Equal(a, b) {
					return plan, errors.New("MCP name conflict: " + name)
				}
				plan.Updated = old
				return plan, nil
			}
		}
		block, e := toml.Marshal(map[string]any{"mcp_servers": map[string]any{name: entry}})
		if e != nil {
			return plan, e
		}
		plan.Updated = append(append(append([]byte{}, old...), '\n'), block...)
		var after map[string]any
		if e = toml.Unmarshal(plan.Updated, &after); e != nil {
			return plan, e
		}
		delete(after["mcp_servers"].(map[string]any), name)
		if root["mcp_servers"] == nil {
			delete(after, "mcp_servers")
		}
		if !reflect.DeepEqual(root, after) {
			return plan, errors.New("MCP merge would change unrelated entries")
		}
		return plan, nil
	}
	return plan, errors.New("unsupported MCP client")
}
func Apply(p Plan) (string, error) {
	if e := os.MkdirAll(filepath.Dir(p.Path), 0700); e != nil {
		return "", e
	}
	lock, e := platform.Lock(p.Path + ".shellstudio.lock")
	if e != nil {
		return "", e
	}
	defer platform.Unlock(lock)
	old, e := os.ReadFile(p.Path)
	if e != nil && !os.IsNotExist(e) {
		return "", e
	}
	if (e == nil) != p.Exists || sha256.Sum256(old) != p.OriginalHash {
		return "", errors.New("configuration changed since preview; review again")
	}
	if bytes.Equal(old, p.Updated) {
		return "", nil
	}
	backup := ""
	if p.Exists {
		backup = p.Path + ".shellstudio-" + time.Now().UTC().Format("20060102T150405.000000000") + ".bak"
		if e = platform.AtomicWrite(backup, old); e != nil {
			return "", fmt.Errorf("backup failed: %w", e)
		}
	}
	if e = platform.AtomicWrite(p.Path, p.Updated); e != nil {
		return backup, e
	}
	return backup, nil
}
