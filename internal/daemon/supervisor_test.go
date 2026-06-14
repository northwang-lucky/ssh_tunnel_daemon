package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/northwang-lucky/ssh-tunnel-daemon/internal/config"
)

// ---------------------------------------------------------------------------
// backoffPolicy tests
// ---------------------------------------------------------------------------

func TestBackoffDelay(t *testing.T) {
	t.Parallel()

	b := backoffPolicy{base: 2 * time.Second, max: 60 * time.Second}

	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{4, 16 * time.Second},
		{5, 32 * time.Second},
		{6, 60 * time.Second},  // capped
		{7, 60 * time.Second},  // capped
		{10, 60 * time.Second}, // capped
	}

	for _, tc := range cases {
		got := b.delay(tc.attempt)
		if got != tc.want {
			t.Errorf("delay(%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Supervisor PID helpers tests
// ---------------------------------------------------------------------------

func TestSupervisorPIDPath(t *testing.T) {
	t.Parallel()

	got := SupervisorPIDPath("/state", "web")
	want := filepath.Join("/state", "web.supervisor.pid")
	if got != want {
		t.Errorf("SupervisorPIDPath = %q, want %q", got, want)
	}
}

func TestWriteReadSupervisorPID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := writeSupervisorPID(dir, "web", 12345); err != nil {
		t.Fatalf("writeSupervisorPID: %v", err)
	}

	pid, err := readSupervisorPID(dir, "web")
	if err != nil {
		t.Fatalf("readSupervisorPID: %v", err)
	}
	if pid != 12345 {
		t.Errorf("pid = %d, want 12345", pid)
	}
}

func TestStopSupervisorRemovesRunMetadata(t *testing.T) {
	dir := t.TempDir()

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	if err := writeSupervisorPID(dir, "web", cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("writeSupervisorPID: %v", err)
	}
	if err := writeRunMetadata(dir, RunMetadata{Name: "web", SessionID: "s1"}); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("writeRunMetadata: %v", err)
	}

	if err := StopSupervisor(dir, "web"); err != nil {
		t.Fatalf("StopSupervisor: %v", err)
	}
	_ = cmd.Wait()
	if _, err := os.Stat(RunMetadataPath(dir, "web")); !os.IsNotExist(err) {
		t.Fatalf("run metadata should be removed")
	}
}

func TestStopSupervisorMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := StopSupervisor(dir, "missing")
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Errorf("expected not running error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Supervisor retry tests
// ---------------------------------------------------------------------------

func TestSupervisorExhaustsRetries(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	starter := func(tunnel config.Tunnel) (*exec.Cmd, string, error) {
		cmd := exec.Command("sh", "-c", "exit 1")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			return nil, "", err
		}
		return cmd, "fake-log-path", nil
	}

	s := NewSupervisor(dir,
		WithTunnelStarter(starter),
	)
	s.backoff = backoffPolicy{
		base:       10 * time.Millisecond,
		max:        50 * time.Millisecond,
		maxRetries: 3,
	}

	tunnel := config.Tunnel{Name: "retry-test", Target: "user@host", Ports: []int{8080}, Mode: "local"}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := s.WatchTunnel(ctx, tunnel)
	if err == nil {
		t.Fatal("expected error after max retries")
	}
	if !strings.Contains(err.Error(), "max retries") {
		t.Errorf("expected 'max retries' in error, got: %v", err)
	}

	if _, err := os.Stat(pidPath(dir, tunnel.Name)); !os.IsNotExist(err) {
		t.Error("expected ssh PID file to be removed after max retries")
	}
}

// ---------------------------------------------------------------------------
// Supervisor ctx cancellation test
// ---------------------------------------------------------------------------

func TestSupervisorCtxCancelKillsSSH(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	starter := func(tunnel config.Tunnel) (*exec.Cmd, string, error) {
		cmd := exec.Command("sleep", "5")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			return nil, "", err
		}
		return cmd, "fake-log-path", nil
	}

	s := NewSupervisor(dir,
		WithTunnelStarter(starter),
	)
	s.backoff = backoffPolicy{
		base:       100 * time.Millisecond,
		max:        500 * time.Millisecond,
		maxRetries: 3,
	}

	tunnel := config.Tunnel{Name: "cancel-test", Target: "user@host", Ports: []int{8080}, Mode: "local"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err := s.WatchTunnel(ctx, tunnel)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if err != context.Canceled && err != context.DeadlineExceeded {
		t.Errorf("expected context error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// StopSupervisor integration test with a real process
// ---------------------------------------------------------------------------

func TestStopSupervisorKillsProcess(t *testing.T) {
	dir := t.TempDir()

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}

	if err := writeSupervisorPID(dir, "test", cmd.Process.Pid); err != nil {
		cmd.Process.Kill()
		t.Fatalf("writeSupervisorPID: %v", err)
	}

	if !isProcessAlive(cmd.Process.Pid) {
		t.Fatal("expected sleep process to be alive before StopSupervisor")
	}

	if err := StopSupervisor(dir, "test"); err != nil {
		t.Fatalf("StopSupervisor: %v", err)
	}

	// Reap the zombie so the PID leaves the process table.
	_ = cmd.Wait()

	if isProcessAlive(cmd.Process.Pid) {
		t.Error("expected sleep process to be dead after StopSupervisor")
	}

	if _, err := os.Stat(SupervisorPIDPath(dir, "test")); !os.IsNotExist(err) {
		t.Error("expected supervisor PID file to be removed")
	}
}

// ---------------------------------------------------------------------------
// readSupervisorPID corrupt file
// ---------------------------------------------------------------------------

func TestReadSupervisorPIDCorrupt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(SupervisorPIDPath(dir, "bad"), []byte("not-a-number"), 0o644); err != nil {
		t.Fatalf("write corrupt pid: %v", err)
	}

	_, err := readSupervisorPID(dir, "bad")
	if err == nil {
		t.Error("expected error for corrupt supervisor PID file")
	}
	if !strings.Contains(err.Error(), "invalid supervisor pid file") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// File path helpers consistency
// ---------------------------------------------------------------------------

func TestPIDPathConsistency(t *testing.T) {
	t.Parallel()

	dir := "/state/base"
	name := "my-tunnel"

	pid := pidPath(dir, name)
	if pid != filepath.Join(dir, name+".pid") {
		t.Errorf("pidPath = %q, want %q", pid, filepath.Join(dir, name+".pid"))
	}

	spid := SupervisorPIDPath(dir, name)
	if spid != filepath.Join(dir, name+".supervisor.pid") {
		t.Errorf("SupervisorPIDPath = %q, want %q", spid, filepath.Join(dir, name+".supervisor.pid"))
	}
}

func TestWriteSupervisorPIDFormat(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := writeSupervisorPID(dir, "fmt-test", 42); err != nil {
		t.Fatalf("writeSupervisorPID: %v", err)
	}

	data, err := os.ReadFile(SupervisorPIDPath(dir, "fmt-test"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	v, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v != 42 {
		t.Errorf("pid = %d, want 42", v)
	}
}
