package daemon

import (
	"bufio"
	"context"
	"fmt"
	"io"
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
// ---------------------------------------------------------------------------

// tunnelStarter is the function signature the supervisor uses to launch
// an ssh child process.
type tunnelStarter func(t config.Tunnel) (*exec.Cmd, string, error)

// Supervisor watches a single SSH tunnel and re-launches it on exit using
// exponential backoff.
type Supervisor struct {
	stateDir  string
	backoff   backoffPolicy
	starter   tunnelStarter
	logWriter *logger.LineWriter

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

func WithLogWriter(logWriter *logger.LineWriter) SupervisorOption {
	return func(s *Supervisor) {
		s.logWriter = logWriter
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
			s.logSupervisor("launch failed for %q (attempt %d): %v", t.Name, attempt, err)
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
		if meta, err := ReadRunMetadata(s.stateDir, t.Name); err == nil {
			meta.CurrentPID = cmd.Process.Pid
			_ = writeRunMetadata(s.stateDir, meta)
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
			s.logSupervisor("ssh process %q exited: %v (attempt %d, log: %s)", t.Name, waitErr, attempt, logPath)
		} else {
			s.logSupervisor("ssh process %q exited cleanly (attempt %d, log: %s)", t.Name, attempt, logPath)
		}

		// Check if ctx was cancelled while we were waiting.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if attempt >= s.backoff.maxRetries {
			_ = os.Remove(pidPath(s.stateDir, t.Name))
			RemoveRunMetadata(s.stateDir, t.Name)
			return fmt.Errorf("tunnel %q: max retries (%d) exhausted", t.Name, s.backoff.maxRetries)
		}

		delay := s.backoff.delay(attempt)
		s.logSupervisor("retrying %q in %v (attempt %d/%d)", t.Name, delay, attempt, s.backoff.maxRetries)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}

		attempt++
	}
}

func (s *Supervisor) logSupervisor(format string, args ...interface{}) {
	line := fmt.Sprintf("[supervisor] "+format, args...)
	if s.logWriter != nil {
		if err := s.logWriter.WriteLine(line); err == nil {
			return
		}
	}
	fmt.Fprintln(os.Stderr, line)
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
// the supervisor PID and path to the first session log segment.
func StartSupervisor(stateDir string, t config.Tunnel) (int, string, error) {
	if err := validateTunnel(t); err != nil {
		return 0, "", err
	}

	// Check if a supervisor is already running for this tunnel.
	if pid, err := readSupervisorPID(stateDir, t.Name); err == nil && isProcessAlive(pid) {
		return 0, "", fmt.Errorf("tunnel %q supervisor already running (PID %d)", t.Name, pid)
	}

	now := time.Now()
	sessionID := newSessionID(now, os.Getpid())
	logPath := logger.SessionSegmentPath(config.DefaultLogDir(), t.Name, sessionID, 1)
	meta := RunMetadata{
		Name:      t.Name,
		SessionID: sessionID,
		StartedAt: now,
		MaxLines:  logger.DefaultMaxLines,
		LogPath:   logPath,
	}
	if err := writeRunMetadata(stateDir, meta); err != nil {
		return 0, "", err
	}

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
		"--session-id", sessionID,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		RemoveRunMetadata(stateDir, t.Name)
		return 0, "", fmt.Errorf("start supervisor: %w", err)
	}

	if err := writeSupervisorPID(stateDir, t.Name, cmd.Process.Pid); err != nil {
		_ = syscall.Kill(cmd.Process.Pid, syscall.SIGKILL)
		RemoveRunMetadata(stateDir, t.Name)
		return 0, "", fmt.Errorf("write supervisor pid file: %w", err)
	}
	currentMeta, err := ReadRunMetadata(stateDir, t.Name)
	if err != nil {
		currentMeta = meta
	}
	currentMeta.SupervisorPID = cmd.Process.Pid
	if err := writeRunMetadata(stateDir, currentMeta); err != nil {
		_ = syscall.Kill(cmd.Process.Pid, syscall.SIGKILL)
		RemoveRunMetadata(stateDir, t.Name)
		return 0, "", err
	}

	return cmd.Process.Pid, logPath, nil
}

func StartTunnelCommandWithWriter(t config.Tunnel, logWriter *logger.LineWriter) (*exec.Cmd, string, error) {
	if logWriter == nil {
		return startTunnelCommand(t)
	}
	if err := validateTunnel(t); err != nil {
		return nil, "", err
	}
	if _, err := exec.LookPath("ssh"); err != nil {
		return nil, "", fmt.Errorf("ssh command not found in PATH: %w", err)
	}

	cmd := exec.Command("ssh", buildSSHArgs(t)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, "", fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, "", fmt.Errorf("start ssh: %w", err)
	}
	go streamLines(logWriter, stdout)
	go streamLines(logWriter, stderr)
	return cmd, "session log", nil
}

func streamLines(logWriter *logger.LineWriter, r io.Reader) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		_ = logWriter.WriteLine(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		_ = logWriter.WriteLine(fmt.Sprintf("[supervisor] log stream error: %v", err))
	}
}
