package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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
	for _, want := range []string{"sshtnl", "s17n", "start", "stop", "restart", "status", "config"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected help output to contain %q, got: %s", want, out)
		}
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
