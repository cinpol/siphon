// Package config loads Argonaut's own configuration.
//
// This is Argonaut's application config (preferences, how to reach the
// cluster) — distinct from Ceph's own ceph.conf, which librados reads. The
// file lives under the XDG config directory as YAML, e.g.
// ~/.config/argonaut/config.yaml. When no file exists, built-in defaults are
// used so the application runs out of the box.
//
// Persistence of richer state (connection profiles for kubectl-style context
// switching, saved layouts, etc.) may be added here later; for now the config
// is intentionally small.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root of Argonaut's application configuration.
type Config struct {
	Ceph CephConfig `yaml:"ceph"`
	UI   UIConfig   `yaml:"ui"`
}

// CephConfig describes how to reach the cluster. Both fields are optional: an
// empty ConfigPath means "let librados find ceph.conf on its default search
// path", matching the ceph CLI's behaviour on an admin/MON node.
type CephConfig struct {
	ConfigPath string `yaml:"config_path"`
	User       string `yaml:"user"`
}

// UIConfig holds presentation and refresh preferences.
type UIConfig struct {
	// RefreshSeconds is how often the dashboard re-polls the cluster. Stored as
	// an integer for a simple, unambiguous config file.
	RefreshSeconds int `yaml:"refresh_seconds"`
}

// RefreshInterval returns the poll interval as a duration, falling back to a
// sane default if the configured value is missing or invalid.
func (u UIConfig) RefreshInterval() time.Duration {
	if u.RefreshSeconds <= 0 {
		return 5 * time.Second
	}
	return time.Duration(u.RefreshSeconds) * time.Second
}

// Default returns the built-in configuration used when no file is present.
func Default() Config {
	return Config{
		Ceph: CephConfig{ConfigPath: "", User: "client.admin"},
		UI:   UIConfig{RefreshSeconds: 5},
	}
}

// Dir returns Argonaut's config directory, honouring XDG_CONFIG_HOME and
// falling back to ~/.config/argonaut.
func Dir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "argonaut")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// Last resort: current directory. Better than panicking on a headless
		// or misconfigured host.
		return filepath.Join(".", ".config", "argonaut")
	}
	return filepath.Join(home, ".config", "argonaut")
}

// Path returns the full path to the config file.
func Path() string {
	return filepath.Join(Dir(), "config.yaml")
}

// Load reads the config file, returning built-in defaults when it does not
// exist. A malformed file is a hard error so misconfiguration is not silently
// ignored.
func Load() (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(Path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config %s: %w", Path(), err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", Path(), err)
	}
	return cfg, nil
}
