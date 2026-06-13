// Package daemon manages SSH tunnel processes and their pid/log state.
package daemon

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/northwang-lucky/ssh-tunnel-daemon/internal/config"
	"github.com/northwang-lucky/ssh-tunnel-daemon/internal/logger"
)

// TunnelStatus represents the runtime status of a tunnel.
type TunnelStatus struct {
	Name    string
	Target  string
	Mode    string
	Ports   []int
	PID     int
	Running bool
}

// StartTunnel launches a new SSH tunnel for t. It returns the child PID and
// the path to the log file used for stdout/stderr.
func StartTunnel(stateDir string, t config.Tunnel) (int, string, error) {
	if err := validateTunnel(t); err != nil {
		return 0, "", err
	}

	status, err := GetStatus(stateDir, t)
	if err != nil {
		return 0, "", err
	}
	if status.Running {
		return 0, "", fmt.Errorf("tunnel %q is already running (PID %d)", t.Name, status.PID)
	}

	if _, err := exec.LookPath("ssh"); err != nil {
		return 0, "", fmt.Errorf("ssh command not found in PATH: %w", err)
	}

	logFile, logPath, err := logger.NewLogFile(config.DefaultLogDir(), t.Name)
	if err != nil {
		return 0, "", err
	}
	defer logFile.Close()

	cmd := exec.Command("ssh", buildSSHArgs(t)...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return 0, "", fmt.Errorf("start ssh: %w", err)
	}

	if err := writePID(stateDir, t.Name, cmd.Process.Pid); err != nil {
		_ = syscall.Kill(cmd.Process.Pid, syscall.SIGKILL)
		return 0, "", fmt.Errorf("write pid file: %w", err)
	}

	return cmd.Process.Pid, logPath, nil
}

// StopTunnel terminates the SSH process for name, first with SIGTERM then
// SIGKILL if it does not exit within 5 seconds. The pid file is always removed.
func StopTunnel(stateDir, name string) error {
	pid, err := readPID(stateDir, name)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("tunnel %q is not running", name)
	}
	if err != nil {
		return err
	}

	if isProcessAlive(pid) {
		_ = syscall.Kill(pid, syscall.SIGTERM)
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if !isProcessAlive(pid) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if isProcessAlive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}

	_ = os.Remove(pidPath(stateDir, name))
	return nil
}

// RestartTunnel stops and then starts the tunnel t.
func RestartTunnel(stateDir string, t config.Tunnel) (int, string, error) {
	_ = StopTunnel(stateDir, t.Name)
	return StartTunnel(stateDir, t)
}



// GetStatus returns the runtime status of t based on its pid file.
func GetStatus(stateDir string, t config.Tunnel) (TunnelStatus, error) {
	pid, err := readPID(stateDir, t.Name)
	if err != nil {
		// Missing or corrupt pid file is treated as not running.
		return TunnelStatus{
			Name:    t.Name,
			Target:  t.Target,
			Mode:    t.Mode,
			Ports:   t.Ports,
			PID:     0,
			Running: false,
		}, nil
	}

	running := pid != 0 && isProcessAlive(pid)
	return TunnelStatus{
		Name:    t.Name,
		Target:  t.Target,
		Mode:    t.Mode,
		Ports:   t.Ports,
		PID:     pid,
		Running: running,
	}, nil
}

// ListRunning returns the status of every tunnel that has a pid file under
// stateDir. Missing config fields are left empty.
func ListRunning(stateDir string, cfg *config.Config) ([]TunnelStatus, error) {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read state dir: %w", err)
	}

	var statuses []TunnelStatus
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".pid" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".pid")
		t, _ := cfg.FindTunnel(name)
		status, err := GetStatus(stateDir, t)
		if err != nil {
			continue
		}
		statuses = append(statuses, status)
	}

	return statuses, nil
}

func buildSSHArgs(t config.Tunnel) []string {
	args := []string{
		"-N",
		"-o", "ServerAliveInterval=60",
		"-o", "ExitOnForwardFailure=yes",
	}
	for _, port := range t.Ports {
		flag := "-L"
		if t.Mode == "remote" {
			flag = "-R"
		}
		args = append(args, flag, fmt.Sprintf("%d:localhost:%d", port, port))
	}
	args = append(args, t.Target)
	return args
}

func validateTunnel(t config.Tunnel) error {
	if strings.TrimSpace(t.Name) == "" {
		return errors.New("tunnel name is required")
	}
	if strings.TrimSpace(t.Target) == "" {
		return fmt.Errorf("tunnel %q: target is required", t.Name)
	}
	if len(t.Ports) == 0 {
		return fmt.Errorf("tunnel %q: at least one port is required", t.Name)
	}
	if t.Mode != "local" && t.Mode != "remote" {
		return fmt.Errorf("tunnel %q: mode must be local or remote", t.Name)
	}
	return nil
}

func pidPath(stateDir, name string) string {
	return filepath.Join(stateDir, fmt.Sprintf("%s.pid", name))
}

func writePID(stateDir, name string, pid int) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	path := pidPath(stateDir, name)
	return os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o644)
}

func readPID(stateDir, name string) (int, error) {
	data, err := os.ReadFile(pidPath(stateDir, name))
	if err != nil {
		return 0, err
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid pid file: %w", err)
	}
	return v, nil
}

func isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
