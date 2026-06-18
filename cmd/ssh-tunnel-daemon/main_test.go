package main

import (
	"bytes"
	"fmt"
	"github.com/northwang-lucky/ssh-tunnel-daemon/internal/config"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func execute(args ...string) (string, int) {
	buf := new(bytes.Buffer)
	cmd := rootCmd
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	// Reset the executed flag so repeated calls work in tests.
	cmd.SetHelpFunc(func(c *cobra.Command, s []string) {
		c.Println(c.UsageString())
	})
	if err := cmd.Execute(); err != nil {
		return buf.String(), 1
	}
	return buf.String(), 0
}

func TestVersionCommand(t *testing.T) {
	out, code := execute("version")
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; output: %s", code, out)
	}
	if !strings.Contains(out, "ssh-tunnel-daemon version") {
		t.Errorf("expected version output, got: %s", out)
	}
}

func TestHelpCommand(t *testing.T) {
	out, code := execute("--help")
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; output: %s", code, out)
	}
	for _, want := range []string{"sshtnl", "s17n", "start", "stop", "list", "status", "log", "config"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected help output to contain %q, got: %s", want, out)
		}
	}
	if strings.Contains(out, "completion") {
		t.Errorf("help output should not contain the default cobra completion command, got: %s", out)
	}
}

func TestStartNoSupervisorFlagRemoved(t *testing.T) {
	out, code := execute("start", "--help")
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; output: %s", code, out)
	}
	if strings.Contains(out, "no-supervisor") {
		t.Errorf("start help should not contain --no-supervisor, got: %s", out)
	}
}

func TestStatusListsUnsavedRunningTunnel(t *testing.T) {
	// Cannot use t.Parallel with t.Setenv.

	tmpConfig := t.TempDir()
	tmpState := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpConfig)
	t.Setenv("XDG_STATE_HOME", tmpState)

	stateDir := config.DefaultStateDir()
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	pidPath := filepath.Join(stateDir, "orphan.pid")
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", os.Getpid())), 0o644); err != nil {
		t.Fatalf("write pid: %v", err)
	}

	out, code := execute("status")
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; output: %s", code, out)
	}
	if !strings.Contains(out, "orphan") {
		t.Errorf("expected status output to contain unsaved tunnel 'orphan', got: %s", out)
	}
	if !strings.Contains(out, "running") {
		t.Errorf("expected status output to show orphan as running, got: %s", out)
	}
}

func TestAlias(t *testing.T) {
	if !containsAlias(rootCmd.Aliases, "sshtnl") {
		t.Error("expected sshtnl alias")
	}
	if !containsAlias(rootCmd.Aliases, "s17n") {
		t.Error("expected s17n alias")
	}
}

func containsAlias(aliases []string, target string) bool {
	for _, a := range aliases {
		if a == target {
			return true
		}
	}
	return false
}
