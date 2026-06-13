package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPaths(t *testing.T) {
	t.Parallel()

	if DefaultConfigDir() == "" {
		t.Error("DefaultConfigDir returned empty")
	}
	if DefaultConfigPath() == "" {
		t.Error("DefaultConfigPath returned empty")
	}
	if DefaultStateDir() == "" {
		t.Error("DefaultStateDir returned empty")
	}
	if DefaultLogDir() == "" {
		t.Error("DefaultLogDir returned empty")
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Tunnels) != 0 {
		t.Fatalf("expected empty config, got %d tunnels", len(cfg.Tunnels))
	}
}

func TestConfigRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := &Config{
		Tunnels: []Tunnel{
			{Name: "web", Target: "user@example.com", Ports: []int{8080, 8080, 9090}, Mode: "local"},
		},
	}

	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if len(loaded.Tunnels) != 1 {
		t.Fatalf("expected 1 tunnel, got %d", len(loaded.Tunnels))
	}
	tunnel := loaded.Tunnels[0]
	if tunnel.Name != "web" || tunnel.Target != "user@example.com" || tunnel.Mode != "local" {
		t.Errorf("unexpected tunnel fields: %+v", tunnel)
	}
	wantPorts := []int{8080, 9090}
	if len(tunnel.Ports) != len(wantPorts) {
		t.Fatalf("expected ports %v, got %v", wantPorts, tunnel.Ports)
	}
	for i := range wantPorts {
		if tunnel.Ports[i] != wantPorts[i] {
			t.Errorf("port mismatch at %d: want %d got %d", i, wantPorts[i], tunnel.Ports[i])
		}
	}
}

func TestFindAndUpsertTunnel(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Tunnels: []Tunnel{{Name: "a", Target: "t", Ports: []int{80}, Mode: "local"}},
	}

	if _, ok := cfg.FindTunnel("missing"); ok {
		t.Error("expected missing tunnel not found")
	}

	tun, ok := cfg.FindTunnel("a")
	if !ok || tun.Name != "a" {
		t.Error("expected to find tunnel a")
	}

	replaced := cfg.UpsertTunnel(Tunnel{Name: "a", Target: "t2", Ports: []int{443}, Mode: "remote"})
	if !replaced {
		t.Error("expected UpsertTunnel to replace existing")
	}

	cfg.UpsertTunnel(Tunnel{Name: "b", Target: "t3", Ports: []int{8080}, Mode: "local"})
	if len(cfg.Tunnels) != 2 {
		t.Fatalf("expected 2 tunnels, got %d", len(cfg.Tunnels))
	}
}

func TestParsePorts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  []int
	}{
		{"8080", []int{8080}},
		{"8080,9090", []int{8080, 9090}},
		{"9090,8080,8080", []int{8080, 9090}},
		{" 8080 , 9090 ", []int{8080, 9090}},
	}

	for _, tc := range cases {
		got, err := ParsePorts(tc.input)
		if err != nil {
			t.Errorf("ParsePorts(%q) unexpected error: %v", tc.input, err)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("ParsePorts(%q) = %v, want %v", tc.input, got, tc.want)
			continue
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Errorf("ParsePorts(%q)[%d] = %d, want %d", tc.input, i, got[i], tc.want[i])
			}
		}
	}
}

func TestParsePortsErrors(t *testing.T) {
	t.Parallel()

	cases := []string{"", "abc", "0", "65536", "8080,,9090", "80,abc"}
	for _, input := range cases {
		if _, err := ParsePorts(input); err == nil {
			t.Errorf("ParsePorts(%q) expected error, got nil", input)
		}
	}
}

func TestFormatPorts(t *testing.T) {
	t.Parallel()

	if got := FormatPorts([]int{8080, 9090}); got != "8080,9090" {
		t.Errorf("FormatPorts = %q, want 8080,9090", got)
	}
}

func TestSaveConfigCreatesDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.yaml")

	if err := SaveConfig(path, &Config{}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
}

func TestLoadConfigInvalidFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("not: [valid yaml"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if _, err := LoadConfig(path); err == nil {
		t.Error("expected error loading invalid YAML")
	}
}
