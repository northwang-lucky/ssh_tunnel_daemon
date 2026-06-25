package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/northwang-lucky/ssh-tunnel-daemon/internal/config"
	"github.com/northwang-lucky/ssh-tunnel-daemon/internal/daemon"
	"github.com/northwang-lucky/ssh-tunnel-daemon/internal/logger"
	"github.com/northwang-lucky/ssh-tunnel-daemon/internal/prompt"
	"github.com/northwang-lucky/ssh-tunnel-daemon/internal/version"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:          "ssh-tunnel-daemon",
	Aliases:      []string{"sshtnl", "s17n"},
	Short:        "Manage SSH tunnel daemons",
	Long:         "ssh-tunnel-daemon starts, stops and monitors SSH tunnels using the system ssh client.",
	SilenceUsage: true,
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: true,
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := logger.CleanupOldLogs(config.DefaultLogDir(), 3*24*time.Hour); err != nil {
			fmt.Fprintf(os.Stderr, "warning: log cleanup failed: %v\n", err)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd, startCmd, stopCmd, listCmd, statusCmd, logCmd, configCmd, supervisorCmd)
	configCmd.AddCommand(configShowCmd, configEditCmd)

	startCmd.Flags().StringP("target", "t", "", "SSH target (e.g. user@host)")
	startCmd.Flags().StringP("ports", "p", "", "Comma-separated ports")
	startCmd.Flags().StringP("mode", "m", "local", "Tunnel mode: local or remote")
	startCmd.Flags().Bool("save", false, "Persist the tunnel definition to the config file")
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "ssh-tunnel-daemon version %s\n", version.Version)
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all saved tunnels",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return errors.New("list takes no arguments")
		}

		cfgPath := config.DefaultConfigPath()
		cfg, err := config.LoadConfig(cfgPath)
		if err != nil {
			return err
		}

		if len(cfg.Tunnels) == 0 {
			fmt.Println("No tunnels configured.")
			return nil
		}

		rows := [][]string{{"NAME", "TARGET", "MODE", "PORTS"}}
		for _, t := range cfg.Tunnels {
			rows = append(rows, []string{t.Name, t.Target, t.Mode, config.FormatPorts(t.Ports)})
		}
		widths := make([]int, len(rows[0]))
		for _, row := range rows {
			for i, cell := range row {
				if len(cell) > widths[i] {
					widths[i] = len(cell)
				}
			}
		}
		for _, row := range rows {
			for i, cell := range row {
				if i == len(row)-1 {
					fmt.Printf("%s", cell)
				} else {
					fmt.Printf("%-*s  ", widths[i], cell)
				}
			}
			fmt.Println()
		}
		return nil
	},
}

var startCmd = &cobra.Command{
	Use:   "start [tunnel_name]",
	Short: "Start an SSH tunnel daemon",
	Long: `Start an SSH tunnel daemon.

If no tunnel_name is provided and stdin is a terminal, an interactive picker is shown.

Explicit mode:
  ssh-tunnel-daemon start web -t user@host -p 8080,9090 -m local --save

Config file mode:
  ssh-tunnel-daemon start web
`,
	RunE: runStart,
}

func runStart(cmd *cobra.Command, args []string) error {
	stateDir := config.DefaultStateDir()
	cfgPath := config.DefaultConfigPath()
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		return err
	}

	var tunnel config.Tunnel
	var save bool

	switch len(args) {
	case 0:
		if len(cfg.Tunnels) == 0 {
			t, s, err := prompt.CreateTunnel()
			if err != nil {
				return err
			}
			tunnel, save = *t, s
		} else {
			selected, create, err := prompt.SelectTunnel(cfg.Tunnels)
			if err != nil {
				return err
			}
			if create {
				t, s, err := prompt.CreateTunnel()
				if err != nil {
					return err
				}
				tunnel, save = *t, s
			} else {
				tunnel = *selected
			}
		}
	case 1:
		name := args[0]
		target, _ := cmd.Flags().GetString("target")
		portsFlag, _ := cmd.Flags().GetString("ports")
		mode, _ := cmd.Flags().GetString("mode")
		save, _ = cmd.Flags().GetBool("save")

		if target == "" {
			if t, ok := cfg.FindTunnel(name); ok {
				tunnel = t
			} else {
				return fmt.Errorf("tunnel %q not found in config and --target not provided", name)
			}
		} else {
			if portsFlag == "" {
				return errors.New("--ports is required when --target is provided")
			}
			ports, err := config.ParsePorts(portsFlag)
			if err != nil {
				return err
			}
			mode = strings.ToLower(strings.TrimSpace(mode))
			if mode != "local" && mode != "remote" {
				return errors.New("--mode must be local or remote")
			}
			tunnel = config.Tunnel{Name: name, Target: target, Ports: ports, Mode: mode}
		}
	default:
		return errors.New("too many arguments; provide at most one tunnel name")
	}

	if save {
		cfg.UpsertTunnel(tunnel)
		if err := config.SaveConfig(cfgPath, cfg); err != nil {
			return err
		}
		// Persisted tunnels are tracked in the config file; no unsaved file needed.
		daemon.RemoveUnsavedTunnel(stateDir, tunnel.Name)
	} else if _, ok := cfg.FindTunnel(tunnel.Name); !ok {
		// Track the running tunnel in the state dir so commands can resolve its
		// metadata even though it is not in the config file.
		if err := daemon.WriteUnsavedTunnel(stateDir, tunnel); err != nil {
			return err
		}
	}
	pid, logPath, err := daemon.StartSupervisor(stateDir, tunnel)
	if err != nil {
		return err
	}
	fmt.Printf("Started supervisor for tunnel %q (supervisor PID: %d, log: %s)\n", tunnel.Name, pid, logPath)
	// The supervisor starts the SSH child asynchronously; wait for its PID file to appear
	// and then verify the process stays alive through a stabilization period.
	_, err = daemon.WaitForTunnelPID(stateDir, tunnel.Name, 5*time.Second)
	if err != nil {
		if daemon.IsSupervisorRunning(stateDir, tunnel.Name) {
			return fmt.Errorf(
				"tunnel %q: SSH process started but failed to stabilize; the remote port may be in use\n"+
					"or authentication failed. Check logs: sshtnl log %s",
				tunnel.Name, tunnel.Name,
			)
		}
		return fmt.Errorf("tunnel %q did not start within 5s: %w", tunnel.Name, err)
	}
	fmt.Printf("Tunnel %q is running\n", tunnel.Name)
	return nil
}

var stopCmd = &cobra.Command{
	Use:   "stop [tunnel_name...]",
	Short: "Stop SSH tunnel daemons",
	RunE:  runStop,
}

func runStop(cmd *cobra.Command, args []string) error {
	stateDir := config.DefaultStateDir()
	cfgPath := config.DefaultConfigPath()
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		return err
	}

	var names []string
	if len(args) == 0 {
		running, err := daemon.ListRunning(stateDir, cfg)
		if err != nil {
			return err
		}
		names, err = prompt.MultiSelectRunning(running)
		if err != nil {
			return err
		}
	} else {
		names = args
	}

	var hadError bool
	for _, name := range names {
		// Stop supervisor first (ignore "not running" errors), then the tunnel.
		_ = daemon.StopSupervisor(stateDir, name)
		if err := daemon.StopTunnel(stateDir, name); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			hadError = true
			continue
		}
		daemon.RemoveUnsavedTunnel(stateDir, name)
		fmt.Printf("Stopped tunnel %q\n", name)
	}

	if hadError {
		return errors.New("one or more operations failed")
	}
	return nil
}

var statusCmd = &cobra.Command{
	Use:   "status [tunnel_name]",
	Short: "Show tunnel status",
	RunE: func(cmd *cobra.Command, args []string) error {
		stateDir := config.DefaultStateDir()
		cfgPath := config.DefaultConfigPath()
		cfg, err := config.LoadConfig(cfgPath)
		if err != nil {
			return err
		}

		if len(args) > 1 {
			return errors.New("too many arguments; provide at most one tunnel name")
		}

		if len(args) == 1 {
			name := args[0]
			t, ok := cfg.FindTunnel(name)
			unsaved := false
			if !ok {
				if u, err := daemon.ReadUnsavedTunnel(stateDir, name); err == nil {
					t = u
					unsaved = true
				} else {
					t = config.Tunnel{Name: name}
				}
			}
			status, err := daemon.GetStatus(stateDir, t, unsaved)
			if err != nil {
				return err
			}
			printStatus(cmd.OutOrStdout(), status)
			return nil
		}

		var statuses []daemon.TunnelStatus
		running, err := daemon.ListRunning(stateDir, cfg)
		if err != nil {
			return err
		}
		seen := make(map[string]bool)
		for _, s := range running {
			statuses = append(statuses, s)
			seen[s.Name] = true
		}
		for _, t := range cfg.Tunnels {
			if seen[t.Name] {
				continue
			}
			status, err := daemon.GetStatus(stateDir, t, false)
			if err != nil {
				continue
			}
			statuses = append(statuses, status)
		}
		// Also include unsaved tunnels that are not currently running but still
		// have metadata in the state dir.
		unsavedTunnels, err := daemon.ListUnsavedTunnels(stateDir)
		if err != nil {
			return err
		}
		for _, t := range unsavedTunnels {
			if seen[t.Name] {
				continue
			}
			status, err := daemon.GetStatus(stateDir, t, true)
			if err != nil {
				continue
			}
			statuses = append(statuses, status)
			seen[t.Name] = true
		}
		if len(statuses) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No tunnels configured.")
			return nil
		}

		// Compute dynamic column widths for strict alignment.
		rows := [][]string{
			{"NAME", "STATUS", "PID", "MODE", "PORTS"},
		}
		for _, s := range statuses {
			statusStr := "stopped"
			pidStr := "-"
			if s.Running {
				statusStr = "running"
				pidStr = fmt.Sprintf("%d", s.PID)
			} else if s.SupervisorAlive {
				statusStr = "retrying"
				pidStr = fmt.Sprintf("%d", s.PID)
			}
			name := s.Name
			if s.Unsaved {
				name = name + " *"
			}
			rows = append(rows, []string{name, statusStr, pidStr, s.Mode, config.FormatPorts(s.Ports)})
		}
		widths := make([]int, len(rows[0]))
		for _, row := range rows {
			for i, cell := range row {
				if len(cell) > widths[i] {
					widths[i] = len(cell)
				}
			}
		}

		out := cmd.OutOrStdout()
		for _, row := range rows {
			for i, cell := range row {
				if i == len(row)-1 {
					fmt.Fprintf(out, "%s", cell)
				} else {
					fmt.Fprintf(out, "%-*s  ", widths[i], cell)
				}
			}
			fmt.Fprintln(out)
		}
		return nil
	},
}

var logCmd = &cobra.Command{
	Use:   "log [tunnel_name]",
	Short: "Show the current tunnel session log",
	RunE:  runLog,
}

func init() {
	logCmd.Flags().BoolP("follow", "f", false, "Follow log output")
}

func runLog(cmd *cobra.Command, args []string) error {
	if len(args) > 1 {
		return errors.New("too many arguments; provide at most one tunnel name")
	}

	stateDir := config.DefaultStateDir()
	cfg, err := config.LoadConfig(config.DefaultConfigPath())
	if err != nil {
		return err
	}

	var name string
	if len(args) == 1 {
		name = args[0]
	} else {
		unsaved, err := daemon.ListUnsavedTunnels(stateDir)
		if err != nil {
			return err
		}
		// Deduplicate: unsaved tunnels already present in config are skipped.
		seen := make(map[string]bool, len(cfg.Tunnels))
		candidates := make([]config.Tunnel, 0, len(cfg.Tunnels)+len(unsaved))
		for _, t := range cfg.Tunnels {
			seen[t.Name] = true
			candidates = append(candidates, t)
		}
		for _, t := range unsaved {
			if seen[t.Name] {
				continue
			}
			candidates = append(candidates, t)
		}
		selected, create, err := prompt.SelectTunnel(candidates)
		if err != nil {
			return err
		}
		if create {
			return errors.New("log requires an existing tunnel")
		}
		name = selected.Name
	}

	// Validate the name belongs to a known tunnel (saved or unsaved) so the
	// error message is helpful rather than "no current session log".
	if _, ok := cfg.FindTunnel(name); !ok {
		if _, err := daemon.ReadUnsavedTunnel(stateDir, name); err != nil {
			return fmt.Errorf("tunnel %q not found", name)
		}
	}

	meta, err := daemon.ReadRunMetadata(stateDir, name)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("tunnel %q has no current session log; start it again to enable session logs", name)
	}
	if err != nil {
		return err
	}

	follow, _ := cmd.Flags().GetBool("follow")
	if follow {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		err := logger.FollowSession(ctx, cmd.OutOrStdout(), config.DefaultLogDir(), meta.Name, meta.SessionID, 500*time.Millisecond)
		if err == context.Canceled || err == context.DeadlineExceeded {
			return nil
		}
		return err
	}
	return logger.StreamSession(cmd.OutOrStdout(), config.DefaultLogDir(), meta.Name, meta.SessionID)
}

func printStatus(w io.Writer, s daemon.TunnelStatus) {
	statusStr := "stopped"
	if s.Running {
		statusStr = fmt.Sprintf("running (PID %d)", s.PID)
	} else if s.SupervisorAlive {
		statusStr = "retrying (supervisor alive)"
	}
	fmt.Fprintf(w, "Tunnel: %s\nStatus: %s\nTarget: %s\nMode:   %s\nPorts:  %s\n",
		s.Name, statusStr, s.Target, s.Mode, config.FormatPorts(s.Ports))
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage the configuration file",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display the current configuration file",
	RunE: func(cmd *cobra.Command, args []string) error {
		path := config.DefaultConfigPath()
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			fmt.Printf("Configuration file does not exist yet: %s\n", path)
			return nil
		}
		if err != nil {
			return err
		}
		fmt.Print(string(data))
		return nil
	},
}

var configEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit the configuration file with $EDITOR",
	RunE: func(cmd *cobra.Command, args []string) error {
		path := config.DefaultConfigPath()
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			if err := config.SaveConfig(path, &config.Config{}); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		editor := strings.TrimSpace(os.Getenv("EDITOR"))
		if editor == "" {
			editor = "vi"
		}

		editorCmd, err := exec.LookPath(editor)
		if err != nil {
			return fmt.Errorf("editor %q not found: %w", editor, err)
		}

		command := exec.Command(editorCmd, path)
		command.Stdin = os.Stdin
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		return command.Run()
	},
}

// supervisorCmd is an internal (hidden) sub-command that acts as the
// long-running watchdog for a single SSH tunnel. It is spawned automatically
// by the start command.
var supervisorCmd = &cobra.Command{
	Use:    "supervisor",
	Short:  "Watch a tunnel and restart on failures (internal)",
	Hidden: true,
	RunE:   runSupervisor,
}

func runSupervisor(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	target, _ := cmd.Flags().GetString("target")
	portsFlag, _ := cmd.Flags().GetString("ports")
	mode, _ := cmd.Flags().GetString("mode")
	sessionID, _ := cmd.Flags().GetString("session-id")

	if name == "" || target == "" || portsFlag == "" || mode == "" {
		return errors.New("--name, --target, --ports, and --mode are all required")
	}
	if sessionID == "" {
		return errors.New("--session-id is required")
	}

	ports, err := config.ParsePorts(portsFlag)
	if err != nil {
		return err
	}

	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "local" && mode != "remote" {
		return errors.New("--mode must be local or remote")
	}

	tunnel := config.Tunnel{Name: name, Target: target, Ports: ports, Mode: mode}
	stateDir := config.DefaultStateDir()
	logWriter, _, err := logger.NewLineWriter(config.DefaultLogDir(), tunnel.Name, sessionID, logger.DefaultMaxLines)
	if err != nil {
		return err
	}
	defer logWriter.Close()
	if err := logWriter.WriteLine(fmt.Sprintf("[supervisor] started supervisor for %q (PID %d)", tunnel.Name, os.Getpid())); err != nil {
		return err
	}

	// Clean up our own PID file on exit.
	defer os.Remove(daemon.SupervisorPIDPath(stateDir, tunnel.Name))

	signal.Ignore(syscall.SIGHUP, syscall.SIGPIPE)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return daemon.NewSupervisor(stateDir,
		daemon.WithLogWriter(logWriter),
		daemon.WithTunnelStarter(func(t config.Tunnel) (*exec.Cmd, string, error) {
			return daemon.StartTunnelCommandWithWriter(t, logWriter)
		}),
	).WatchTunnel(ctx, tunnel)
}

func init() {
	supervisorCmd.Flags().String("name", "", "Tunnel name")
	supervisorCmd.Flags().String("target", "", "SSH target")
	supervisorCmd.Flags().String("ports", "", "Comma-separated ports")
	supervisorCmd.Flags().String("mode", "", "Tunnel mode: local or remote")
	supervisorCmd.Flags().String("session-id", "", "Log session ID")
	_ = supervisorCmd.MarkFlagRequired("name")
	_ = supervisorCmd.MarkFlagRequired("target")
	_ = supervisorCmd.MarkFlagRequired("ports")
	_ = supervisorCmd.MarkFlagRequired("mode")
	_ = supervisorCmd.MarkFlagRequired("session-id")
}
