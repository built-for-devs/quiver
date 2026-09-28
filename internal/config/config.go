// Package config loads and saves the CLI config file and resolves effective
// settings from file, environment, and flags.
//
// Precedence (highest first): flag > environment variable > config file.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	EnvToken     = "QUIVER_TOKEN"
	EnvAPIURL    = "QUIVER_API_URL"
	EnvWorkspace = "QUIVER_WORKSPACE"
	EnvConfig    = "QUIVER_CONFIG"

	TokenPrefix = "qvr_"
)

// Config is the on-disk config file (~/.quiver/config, JSON).
type Config struct {
	Token     string `json:"token,omitempty"`
	APIURL    string `json:"api_url,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

// SettableKeys are the keys `quiver config set` accepts. The token is set via
// `quiver auth login` so it never appears in `config set` shell history.
var SettableKeys = map[string]func(*Config) *string{
	"api_url":   func(c *Config) *string { return &c.APIURL },
	"workspace": func(c *Config) *string { return &c.Workspace },
}

// KeyNames returns the settable keys, sorted.
func KeyNames() []string {
	keys := make([]string, 0, len(SettableKeys))
	for k := range SettableKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Path returns the config file path: $QUIVER_CONFIG or ~/.quiver/config.
func Path() (string, error) {
	if p := os.Getenv(EnvConfig); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".quiver", "config"), nil
}

// Load reads the config file. A missing file yields an empty Config.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", p, err)
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", p, err)
	}
	return &c, nil
}

// Save writes the config file atomically with 0600 permissions, since it
// contains a credential.
func (c *Config) Save() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".config-*")
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// Source describes where an effective setting came from.
type Source string

const (
	SourceNone Source = ""
	SourceFlag Source = "flag"
	SourceEnv  Source = "env"
	SourceFile Source = "config"
)

// Setting is a resolved value and its origin.
type Setting struct {
	Value  string
	Source Source
}

// Resolve picks the effective value: flag, then env var, then file.
func Resolve(flagVal, envKey, fileVal string) Setting {
	if flagVal != "" {
		return Setting{flagVal, SourceFlag}
	}
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		return Setting{v, SourceEnv}
	}
	if fileVal != "" {
		return Setting{fileVal, SourceFile}
	}
	return Setting{}
}

// SourceWorkspace marks an API URL derived from the workspace slug.
const SourceWorkspace Source = "workspace"

var workspaceRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidWorkspace reports whether slug is usable as a DNS label.
func ValidWorkspace(slug string) bool { return workspaceRe.MatchString(slug) }

// WorkspaceURL returns the MCP endpoint for a workspace. Workspace scoping is
// by subdomain: https://<workspace>.quivergtm.dev/api/mcp
func WorkspaceURL(slug string) string {
	return "https://" + slug + ".quivergtm.dev/api/mcp"
}

// MaskToken hides all but the prefix and last four characters.
func MaskToken(t string) string {
	if t == "" {
		return ""
	}
	if len(t) <= len(TokenPrefix)+4 {
		return TokenPrefix + "****"
	}
	return t[:len(TokenPrefix)] + "****" + t[len(t)-4:]
}
