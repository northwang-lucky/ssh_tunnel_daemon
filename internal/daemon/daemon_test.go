package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/northwang-lucky/ssh-tunnel-daemon/internal/config"
)

func TestBuildSSHArgsLocal(t *testing.T) {
	t.Parallel()

	args := buildSSHArgs(config.Tunnel{Name: "web", Target: "user@host", Ports: []int{8080, 9090}, Mode: "local"})
	want := []string{"-N", "-o", "ServerAliveInterval=60", "-o", "ExitOnForwardFailure=yes", "-L", "8080:localhost:8080", "-L", "9090:localhost:9090", "user@host"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("local args = %v, want %v", args, want)
	}
}

func TestBuildSSHArgsRemote(t *testing.T) {
	t.Parallel()

	args := buildSSHArgs(config.Tunnel{Name: "web", Target: "user@host", Ports: []int{8080}, Mode: "remote"})
	want := []string{"-N", "-o", "ServerAliveInterval=60", "-o", "ExitOnForwardFailure=yes", "-R", "8080:localhost:8080", "user@host"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("remote args = %v, want %v", args, want)
	}
}

func TestValidateTunnel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		tunnel config.Tunnel
		want   string
	}{
		{"empty name", config.Tunnel{Target: "t", Ports: []int{80}, Mode: "local"}, "name is required"},
		{"empty target", config.Tunnel{Name: "x", Ports: []int{80}, Mode: "local"}, "target is required"},
		{"no ports", config.Tunnel{Name: "x", Target: "t", Mode: "local"}, "at least one port"},
		{"bad mode", config.Tunnel{Name: "x", Target: "t", Ports: []int{80}, Mode: "x"}, "mode must be local or remote"},
	}

	for _, tc := range cases {
		err := validateTunnel(tc.tunnel)
		if err == nil || !contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want containing %q", tc.name, err, tc.want)
		}
	}
}

func TestPIDFileRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := writePID(dir, "web", 12345); err != nil {
		t.Fatalf("writePID: %v", err)
	}

	pid, err := readPID(dir, "web")
	if err != nil {
		t.Fatalf("readPID: %v", err)
	}
	if pid != 12345 {
		t.Errorf("pid = %d, want 12345", pid)
	}
}

func TestGetStatusNotRunning(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	status, err := GetStatus(dir, config.Tunnel{Name: "web"})
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if status.Running {
		t.Error("expected not running")
	}
}

func TestGetStatusCorruptPID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "web.pid"), []byte("not-a-number"), 0o644); err != nil {
		t.Fatalf("write pid: %v", err)
	}

	status, err := GetStatus(dir, config.Tunnel{Name: "web"})
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if status.Running {
		t.Error("expected not running with corrupt pid")
	}
}

func TestStopTunnelMissingPID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := StopTunnel(dir, "missing")
	if err == nil || !contains(err.Error(), "not running") {
		t.Errorf("expected not running error, got %v", err)
	}
}

func TestListRunning(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := writePID(dir, "web", 1); err != nil {
		t.Fatalf("writePID: %v", err)
	}
	if err := writeSupervisorPID(dir, "web", 2); err != nil {
		t.Fatalf("writeSupervisorPID: %v", err)
	}

	cfg := &config.Config{Tunnels: []config.Tunnel{{Name: "web", Target: "u@h", Ports: []int{80}, Mode: "local"}}}
	statuses, err := ListRunning(dir, cfg)
	if err != nil {
		t.Fatalf("ListRunning: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].Name != "web" {
		t.Errorf("unexpected name %q", statuses[0].Name)
	}
}

func TestWaitForTunnelPID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Write a live PID and verify it is picked up immediately.
	if err := writePID(dir, "live", os.Getpid()); err != nil {
		t.Fatalf("writePID: %v", err)
	}
	pid, err := WaitForTunnelPID(dir, "live", time.Second)
	if err != nil {
		t.Fatalf("WaitForTunnelPID: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("expected pid %d, got %d", os.Getpid(), pid)
	}
}

func TestStartTunnelRealSleep(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("ssh not found in PATH")
	}

	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	t.Setenv("XDG_STATE_HOME", dir)

	// Override default log dir resolution for this test by writing into our own state tree.
	tunnel := config.Tunnel{Name: "sleepy", Target: "localhost", Ports: []int{29090}, Mode: "local"}
	pid, logPath, err := StartTunnel(dir, tunnel)
	if err != nil {
		t.Fatalf("StartTunnel: %v", err)
	}
	if pid == 0 {
		t.Error("expected non-zero pid")
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Errorf("log file should exist: %v", err)
	}

	status, err := GetStatus(dir, tunnel)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if !status.Running {
		t.Error("expected tunnel to be running")
	}

	if err := StopTunnel(dir, "sleepy"); err != nil {
		t.Fatalf("StopTunnel: %v", err)
	}

	// Give the process a moment to exit.
	time.Sleep(200 * time.Millisecond)

	status, _ = GetStatus(dir, tunnel)
	if status.Running {
		t.Error("expected tunnel to be stopped")
	}

	// Log file should be preserved after stop.
	if _, err := os.Stat(logPath); err != nil {
		t.Errorf("log file should be preserved: %v", err)
	}

	// Clean up logs directory helper.
	_ = os.RemoveAll(logDir)
}


func contains(s, substr string) bool {
	return len(substr) > 0 && len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (findSubstr(s, substr)))
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Ensure a real running process is detected by isProcessAlive.
func TestIsProcessAlive(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("sleep", "10")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	defer syscall.Kill(cmd.Process.Pid, syscall.SIGKILL)

	if !isProcessAlive(cmd.Process.Pid) {
		t.Error("expected sleep process to be alive")
	}
}
