package main

import (
	"context"
	"errors"
	"fmt"
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
	Long:         "ssh-tunnel-daemon starts, stops, restarts and monitors SSH tunnels using the system ssh client.",
	SilenceUsage: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := logger.CleanupOldLogs(config.DefaultLogDir(), 3*24*time.Hour); err != nil {
			fmt.Fprintf(os.Stderr, "warning: log cleanup failed: %v\n", err)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd, startCmd, stopCmd, restartCmd, statusCmd, configCmd, supervisorCmd)
	configCmd.AddCommand(configShowCmd, configEditCmd)

	startCmd.Flags().StringP("target", "t", "", "SSH target (e.g. user@host)")
	startCmd.Flags().StringP("ports", "p", "", "Comma-separated ports")
	startCmd.Flags().StringP("mode", "m", "local", "Tunnel mode: local or remote")
	startCmd.Flags().Bool("save", false, "Persist the tunnel definition to the config file")
	startCmd.Flags().Bool("no-supervisor", false, "Start the SSH tunnel directly without a watchdog supervisor")
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "ssh-tunnel-daemon version %s\n", version.Version)
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
	}
	noSupervisor, _ := cmd.Flags().GetBool("no-supervisor")

	if noSupervisor {
		pid, logPath, err := daemon.StartTunnel(stateDir, tunnel)
		if err != nil {
			return err
		}
		fmt.Printf("Started tunnel %q (PID: %d, log: %s)\n", tunnel.Name, pid, logPath)
	} else {
		pid, logPath, err := daemon.StartSupervisor(stateDir, tunnel)
		if err != nil {
			return err
		}
		fmt.Printf("Started supervisor for tunnel %q (PID: %d, log: %s)\n", tunnel.Name, pid, logPath)
	}

	return nil
}

var stopCmd = &cobra.Command{
	Use:   "stop [tunnel_name...]",
	Short: "Stop SSH tunnel daemons",
	RunE:  runStopRestart(false),
}

var restartCmd = &cobra.Command{
	Use:   "restart [tunnel_name...]",
	Short: "Restart SSH tunnel daemons",
	RunE:  runStopRestart(true),
}

func runStopRestart(restart bool) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
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
			names, err = prompt.MultiSelectRunning(running, restart)
			if err != nil {
				return err
			}
		} else {
			names = args
		}

		var hadError bool
		for _, name := range names {
			if restart {
				t, ok := cfg.FindTunnel(name)
				if !ok {
					fmt.Fprintf(os.Stderr, "error: tunnel %q not found in config; cannot restart\n", name)
					hadError = true
					continue
				}
				pid, logPath, err := daemon.RestartTunnel(stateDir, t)
				if err != nil {
					fmt.Fprintf(os.Stderr, "error: failed to restart %q: %v\n", name, err)
					hadError = true
					continue
				}
				fmt.Printf("Restarted tunnel %q (PID: %d, log: %s)\n", name, pid, logPath)
			} else {
				// Stop supervisor first (ignore "not running" errors), then the tunnel.
				_ = daemon.StopSupervisor(stateDir, name)
				if err := daemon.StopTunnel(stateDir, name); err != nil {
					fmt.Fprintf(os.Stderr, "error: %v\n", err)
					hadError = true
					continue
				}
				fmt.Printf("Stopped tunnel %q\n", name)
			}
		}

		if hadError {
			return errors.New("one or more operations failed")
		}
		return nil
	}
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
			if !ok {
				t = config.Tunnel{Name: name}
			}
			status, err := daemon.GetStatus(stateDir, t)
			if err != nil {
				return err
			}
			printStatus(status)
			return nil
		}

		var statuses []daemon.TunnelStatus
		for _, t := range cfg.Tunnels {
			status, err := daemon.GetStatus(stateDir, t)
			if err != nil {
				continue
			}
			statuses = append(statuses, status)
		}

		if len(statuses) == 0 {
			fmt.Println("No tunnels configured.")
			return nil
		}

		fmt.Printf("%-12s %-10s %-8s %-12s %s\n", "NAME", "STATUS", "PID", "MODE", "PORTS")
		for _, s := range statuses {
			statusStr := "stopped"
			pidStr := "-"
			if s.Running {
				statusStr = "running"
				pidStr = fmt.Sprintf("%d", s.PID)
			}
			fmt.Printf("%-12s %-10s %-8s %-12s %s\n", s.Name, statusStr, pidStr, s.Mode, config.FormatPorts(s.Ports))
		}
		return nil
	},
}

func printStatus(s daemon.TunnelStatus) {
	statusStr := "stopped"
	if s.Running {
		statusStr = fmt.Sprintf("running (PID %d)", s.PID)
	}
	fmt.Printf("Tunnel: %s\nStatus: %s\nTarget: %s\nMode:   %s\nPorts:  %s\n",
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

	if name == "" || target == "" || portsFlag == "" || mode == "" {
		return errors.New("--name, --target, --ports, and --mode are all required")
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

	// Clean up our own PID file on exit.
	defer os.Remove(daemon.SupervisorPIDPath(stateDir, tunnel.Name))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return daemon.NewSupervisor(stateDir).WatchTunnel(ctx, tunnel)
}

func init() {
	supervisorCmd.Flags().String("name", "", "Tunnel name")
	supervisorCmd.Flags().String("target", "", "SSH target")
	supervisorCmd.Flags().String("ports", "", "Comma-separated ports")
	supervisorCmd.Flags().String("mode", "", "Tunnel mode: local or remote")
	_ = supervisorCmd.MarkFlagRequired("name")
	_ = supervisorCmd.MarkFlagRequired("target")
	_ = supervisorCmd.MarkFlagRequired("ports")
	_ = supervisorCmd.MarkFlagRequired("mode")
}
