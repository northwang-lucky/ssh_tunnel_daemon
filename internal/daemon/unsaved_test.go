package daemon

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/northwang-lucky/ssh-tunnel-daemon/internal/config"
)

func TestUnsavedTunnelRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tunnel := config.Tunnel{Name: "tmp", Target: "u@h", Ports: []int{8080, 9090}, Mode: "local"}

	if err := WriteUnsavedTunnel(dir, tunnel); err != nil {
		t.Fatalf("WriteUnsavedTunnel: %v", err)
	}

	got, err := ReadUnsavedTunnel(dir, "tmp")
	if err != nil {
		t.Fatalf("ReadUnsavedTunnel: %v", err)
	}
	if !reflect.DeepEqual(got, tunnel) {
		t.Errorf("round-trip mismatch: got %+v want %+v", got, tunnel)
	}

	RemoveUnsavedTunnel(dir, "tmp")
	if _, err := os.Stat(UnsavedTunnelPath(dir, "tmp")); !os.IsNotExist(err) {
		t.Fatalf("unsaved tunnel file should be removed")
	}
}

func TestListUnsavedTunnelsSkipsInvalid(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tunnel := config.Tunnel{Name: "good", Target: "u@h", Ports: []int{80}, Mode: "remote"}
	if err := WriteUnsavedTunnel(dir, tunnel); err != nil {
		t.Fatalf("WriteUnsavedTunnel: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unsaved", "bad.json"), []byte("not json"), 0o644); err != nil {
		t.Fatalf("write bad file: %v", err)
	}

	got, err := ListUnsavedTunnels(dir)
	if err != nil {
		t.Fatalf("ListUnsavedTunnels: %v", err)
	}
	if len(got) != 1 || got[0].Name != "good" {
		t.Errorf("expected only valid tunnel, got %+v", got)
	}
}

func TestListRunningEnrichesUnsaved(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tunnel := config.Tunnel{Name: "orphan", Target: "u@h", Ports: []int{8080}, Mode: "local"}
	if err := WriteUnsavedTunnel(dir, tunnel); err != nil {
		t.Fatalf("WriteUnsavedTunnel: %v", err)
	}
	if err := writePID(dir, "orphan", 1); err != nil {
		t.Fatalf("writePID: %v", err)
	}

	statuses, err := ListRunning(dir, &config.Config{})
	if err != nil {
		t.Fatalf("ListRunning: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	s := statuses[0]
	if s.Name != "orphan" {
		t.Errorf("name = %q, want orphan", s.Name)
	}
	if !s.Unsaved {
		t.Error("expected Unsaved=true")
	}
	if s.Target != "u@h" || s.Mode != "local" || !reflect.DeepEqual(s.Ports, []int{8080}) {
		t.Errorf("unexpected metadata: %+v", s)
	}
}
