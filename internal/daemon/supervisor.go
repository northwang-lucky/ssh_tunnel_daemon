package daemon

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/northwang-lucky/ssh-tunnel-daemon/internal/config"
	"github.com/northwang-lucky/ssh-tunnel-daemon/internal/logger"
)

// ---------------------------------------------------------------------------
// Backoff policy
// ---------------------------------------------------------------------------

type backoffPolicy struct {
	base       time.Duration
	max        time.Duration
	maxRetries int
}

// delay returns the wait duration for the given attempt number (1-indexed).
func (b backoffPolicy) delay(attempt int) time.Duration {
	d := time.Duration(math.Pow(2, float64(attempt-1))) * b.base
	if d > b.max {
		d = b.max
	}
	return d
}

// ---------------------------------------------------------------------------
// Supervisor
// tunnelStarter is the function signature the supervisor uses to launch
// an ssh child process.
type tunnelStarter func(t config.Tunnel) (*exec.Cmd, string, error)

// Supervisor watches a single SSH tunnel and re-launches it on exit using
// exponential backoff.
type Supervisor struct {
	stateDir string
	backoff  backoffPolicy
	starter  tunnelStarter

	mu  sync.Mutex
	cmd *exec.Cmd // guarded by mu; set by each launch attempt
}

// SupervisorOption customises a Supervisor.
type SupervisorOption func(*Supervisor)

// WithTunnelStarter replaces the default starter (startTunnelCommand).
func WithTunnelStarter(starter tunnelStarter) SupervisorOption {
	return func(s *Supervisor) {
		s.starter = starter
	}
}

// NewSupervisor creates a Supervisor with sensible defaults.
func NewSupervisor(stateDir string, opts ...SupervisorOption) *Supervisor {
	s := &Supervisor{
		stateDir: stateDir,
		backoff: backoffPolicy{
			base:       2 * time.Second,
			max:        60 * time.Second,
			maxRetries: 10,
		},
		starter: startTunnelCommand,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// WatchTunnel starts the tunnel and re-launches it on exit with exponential
// backoff until maxRetries is reached or ctx is cancelled. On ctx cancellation
// it terminates the current ssh process group and returns ctx.Err().
func (s *Supervisor) WatchTunnel(ctx context.Context, t config.Tunnel) error {
	attempt := 1

	for {
		cmd, logPath, err := s.starter(t)
		if err != nil {
			logLaunchFailure(t.Name, attempt, err)
			if attempt >= s.backoff.maxRetries {
				return fmt.Errorf("tunnel %q: max retries (%d) exhausted after launch failure: %w", t.Name, s.backoff.maxRetries, err)
			}
			attempt++
			delay := s.backoff.delay(attempt - 1)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			continue
		}

		// Store cmd under mutex so the ctx-cancel goroutine can kill it.
		s.mu.Lock()
		s.cmd = cmd
		s.mu.Unlock()

		// Write the ssh PID file so CLI tools see the current process.
		if err := writePID(s.stateDir, t.Name, cmd.Process.Pid); err != nil {
			s.mu.Lock()
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			s.cmd = nil
			s.mu.Unlock()
			return fmt.Errorf("write ssh pid file: %w", err)
		}

		// Watch for ctx cancellation while ssh runs.
		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				s.killCurrent()
			case <-done:
			}
		}()

		waitErr := cmd.Wait()
		close(done)

		s.mu.Lock()
		s.cmd = nil
		s.mu.Unlock()

		// Log the exit.
		if waitErr != nil {
			fmt.Fprintf(os.Stderr, "[supervisor] ssh process %q exited: %v (attempt %d, log: %s)\n", t.Name, waitErr, attempt, logPath)
		} else {
			fmt.Fprintf(os.Stderr, "[supervisor] ssh process %q exited cleanly (attempt %d, log: %s)\n", t.Name, attempt, logPath)
		}

		// Check if ctx was cancelled while we were waiting.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if attempt >= s.backoff.maxRetries {
			_ = os.Remove(pidPath(s.stateDir, t.Name))
			return fmt.Errorf("tunnel %q: max retries (%d) exhausted", t.Name, s.backoff.maxRetries)
		}

		delay := s.backoff.delay(attempt)
		fmt.Fprintf(os.Stderr, "[supervisor] retrying %q in %v (attempt %d/%d)\n", t.Name, delay, attempt, s.backoff.maxRetries)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}

		attempt++
	}
}

// killCurrent terminates the process group of the currently running ssh
// child. It is safe to call from any goroutine.
func (s *Supervisor) killCurrent() {
	s.mu.Lock()
	cmd := s.cmd
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
}

// ---------------------------------------------------------------------------
// StartSupervisor – CLI-facing entry point
// ---------------------------------------------------------------------------

// StartSupervisor starts a supervisor subprocess that watches t. It returns
// the supervisor PID and path to the supervisor log file.
func StartSupervisor(stateDir string, t config.Tunnel) (int, string, error) {
	if err := validateTunnel(t); err != nil {
		return 0, "", err
	}

	// Check if a supervisor is already running for this tunnel.
	if pid, err := readSupervisorPID(stateDir, t.Name); err == nil && isProcessAlive(pid) {
		return 0, "", fmt.Errorf("tunnel %q supervisor already running (PID %d)", t.Name, pid)
	}

	logFile, logPath, err := logger.NewLogFile(config.DefaultLogDir(), "supervisor")
	if err != nil {
		return 0, "", err
	}
	defer logFile.Close()

	// Build ports argument.
	ports := make([]string, len(t.Ports))
	for i, p := range t.Ports {
		ports[i] = fmt.Sprintf("%d", p)
	}
	portsStr := ""
	for i, p := range ports {
		if i > 0 {
			portsStr += ","
		}
		portsStr += p
	}

	cmd := exec.Command(os.Args[0],
		"supervisor",
		"--name", t.Name,
		"--target", t.Target,
		"--ports", portsStr,
		"--mode", t.Mode,
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return 0, "", fmt.Errorf("start supervisor: %w", err)
	}

	if err := writeSupervisorPID(stateDir, t.Name, cmd.Process.Pid); err != nil {
		_ = syscall.Kill(cmd.Process.Pid, syscall.SIGKILL)
		return 0, "", fmt.Errorf("write supervisor pid file: %w", err)
	}

	return cmd.Process.Pid, logPath, nil
}

func logLaunchFailure(name string, attempt int, err error) {
	fmt.Fprintf(os.Stderr, "[supervisor] launch failed for %q (attempt %d): %v\n", name, attempt, err)
}
