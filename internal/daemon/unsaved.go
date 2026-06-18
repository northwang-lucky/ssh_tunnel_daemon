package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/northwang-lucky/ssh-tunnel-daemon/internal/config"
)

// UnsavedTunnelPath returns the path to the temporary JSON file that stores
// metadata for a tunnel that is running but not persisted in the config file.
func UnsavedTunnelPath(stateDir, name string) string {
	return filepath.Join(stateDir, "unsaved", fmt.Sprintf("%s.json", name))
}

// WriteUnsavedTunnel writes tunnel metadata to the unsaved directory.
// It overwrites any existing file for the same tunnel name.
func WriteUnsavedTunnel(stateDir string, t config.Tunnel) error {
	path := UnsavedTunnelPath(stateDir, t.Name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create unsaved dir: %w", err)
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal unsaved tunnel: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write unsaved tunnel: %w", err)
	}
	return nil
}

// ReadUnsavedTunnel reads a previously written unsaved tunnel definition.
func ReadUnsavedTunnel(stateDir, name string) (config.Tunnel, error) {
	path := UnsavedTunnelPath(stateDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return config.Tunnel{}, err
	}
	var t config.Tunnel
	if err := json.Unmarshal(data, &t); err != nil {
		return config.Tunnel{}, fmt.Errorf("unmarshal unsaved tunnel: %w", err)
	}
	return t, nil
}

// RemoveUnsavedTunnel deletes the unsaved tunnel metadata file, if any.
// Errors are ignored so cleanup is best-effort.
func RemoveUnsavedTunnel(stateDir, name string) {
	_ = os.Remove(UnsavedTunnelPath(stateDir, name))
}

// ListUnsavedTunnels returns metadata for every tunnel currently stored in the
// unsaved directory. Files that cannot be parsed are skipped.
func ListUnsavedTunnels(stateDir string) ([]config.Tunnel, error) {
	dir := filepath.Join(stateDir, "unsaved")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read unsaved dir: %w", err)
	}

	var tunnels []config.Tunnel
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		tunnelName := strings.TrimSuffix(name, ".json")
		t, err := ReadUnsavedTunnel(stateDir, tunnelName)
		if err != nil {
			continue
		}
		tunnels = append(tunnels, t)
	}
	return tunnels, nil
}
