// SPDX-License-Identifier: GPL-3.0-only
package platform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Profile struct {
	Executable string            `json:"executable"`
	Args       []string          `json:"args"`
	Env        map[string]string `json:"env,omitempty"`
}
type ExtensionConfig struct {
	Enabled  bool               `json:"enabled"`
	Default  string             `json:"default_profile"`
	Profiles map[string]Profile `json:"profiles,omitempty"`
}
type Config struct {
	Version    int                        `json:"version"`
	Extensions map[string]ExtensionConfig `json:"extensions"`
	Recent     []string                   `json:"recent_folders"`
}

func LoadConfig(dir string) (Config, error) {
	c := Config{Version: 1, Extensions: map[string]ExtensionConfig{}}
	b, e := os.ReadFile(filepath.Join(dir, "config.json"))
	if os.IsNotExist(e) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, fmt.Errorf("config.json: %w", e)
	}
	if c.Version != 1 {
		return c, fmt.Errorf("unsupported config version %d", c.Version)
	}
	if c.Extensions == nil {
		c.Extensions = map[string]ExtensionConfig{}
	}
	return c, nil
}
func UpdateConfig(dir string, update func(*Config) error) error {
	f, e := Lock(filepath.Join(dir, "config.lock"))
	if e != nil {
		return e
	}
	defer Unlock(f)
	c, e := LoadConfig(dir)
	if e != nil {
		return e
	}
	if e = update(&c); e != nil {
		return e
	}
	return WriteJSON(filepath.Join(dir, "config.json"), c)
}
func Remember(dir, path string) error {
	return UpdateConfig(dir, func(c *Config) error {
		a := []string{path}
		for _, p := range c.Recent {
			if p != path && len(a) < 20 {
				a = append(a, p)
			}
		}
		c.Recent = a
		return nil
	})
}
