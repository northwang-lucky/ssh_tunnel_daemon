// Package config defines the configuration model, XDG path helpers, and YAML I/O.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/viper"
)

// Tunnel describes a single SSH tunnel definition.
type Tunnel struct {
	Name   string `mapstructure:"name"`
	Target string `mapstructure:"target"`
	Ports  []int  `mapstructure:"ports"`
	Mode   string `mapstructure:"mode"`
}

// Config is the top-level configuration file.
type Config struct {
	Tunnels []Tunnel `mapstructure:"tunnels"`
}

// DefaultConfigDir returns the XDG config directory for this application.
func DefaultConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "~/.config"
	}
	return filepath.Join(dir, "ssh-tunnel-daemon")
}

// DefaultConfigPath returns the default configuration file path.
func DefaultConfigPath() string {
	return filepath.Join(DefaultConfigDir(), "config.yaml")
}

// DefaultStateDir returns the XDG state directory for this application.
func DefaultStateDir() string {
	dir := xdgStateHome()
	return filepath.Join(dir, "ssh-tunnel-daemon")
}

func xdgStateHome() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".local", "state")
}

// DefaultLogDir returns the directory used for per-tunnel log files.
func DefaultLogDir() string {
	return filepath.Join(DefaultStateDir(), "logs")
}

// LoadConfig reads the YAML configuration from path. If the file does not
// exist, an empty config is returned without error.
func LoadConfig(path string) (*Config, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("stat config: %w", err)
	}

	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	for i := range cfg.Tunnels {
		cfg.Tunnels[i].Ports = uniqueSortedPorts(cfg.Tunnels[i].Ports)
	}

	return &cfg, nil
}

// SaveConfig writes cfg to path as YAML, creating the parent directory if needed.
func SaveConfig(path string, cfg *Config) error {
	if cfg == nil {
		return errors.New("config is nil")
	}

	for i := range cfg.Tunnels {
		cfg.Tunnels[i].Ports = uniqueSortedPorts(cfg.Tunnels[i].Ports)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	v := viper.New()
	v.Set("tunnels", cfg.Tunnels)
	if err := v.WriteConfigAs(path); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

// FindTunnel returns the tunnel with the given name, if present.
func (c *Config) FindTunnel(name string) (Tunnel, bool) {
	if c == nil {
		return Tunnel{}, false
	}
	for _, t := range c.Tunnels {
		if t.Name == name {
			return t, true
		}
	}
	return Tunnel{}, false
}

// UpsertTunnel adds or replaces a tunnel by name and returns whether an
// existing entry was replaced.
func (c *Config) UpsertTunnel(t Tunnel) bool {
	if c == nil {
		return false
	}
	t.Ports = uniqueSortedPorts(t.Ports)
	for i, existing := range c.Tunnels {
		if existing.Name == t.Name {
			c.Tunnels[i] = t
			return true
		}
	}
	c.Tunnels = append(c.Tunnels, t)
	return false
}

func uniqueSortedPorts(ports []int) []int {
	seen := make(map[int]struct{}, len(ports))
	uniq := make([]int, 0, len(ports))
	for _, p := range ports {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		uniq = append(uniq, p)
	}
	sort.Ints(uniq)
	return uniq
}
