# Repository Guidelines

## Project Overview

`ssh-tunnel-daemon` is a Go CLI tool that starts, stops, monitors, and tails SSH tunnels (`-L` / `-R`) as daemon processes. Every tunnel start goes through the watchdog supervisor, which auto-reconnects on failure with exponential backoff. Each running tunnel is tracked with PID files plus a run metadata file, and writes session-scoped rotating logs. Tunnels are persisted in a YAML config file. The binary ships with two aliases: `sshtnl` and `s17n`.

## Architecture & Data Flow

```
cmd/ssh-tunnel-daemon    ← Cobra CLI (start/stop/status/list/log/config)
  │
  ├── internal/daemon    ← Process lifecycle, PID files, run metadata, supervisor watchdog
  ├── internal/config    ← YAML config I/O, XDG paths, port parsing
  ├── internal/prompt    ← Interactive TUI forms (charmbracelet/huh)
  ├── internal/logger    ← Session log rotation, streaming/following, cleanup
  └── internal/version   ← Version string injected via ldflags
```

**Flow**: CLI `start` → `daemon.StartSupervisor` → writes `{name}.run.json` and `{name}.supervisor.pid` under `$XDG_STATE_HOME/ssh-tunnel-daemon/` → starts hidden `ssh-tunnel-daemon supervisor --name … --session-id …` subprocess. The supervisor owns the session log writer, starts the `ssh` child process, writes `{name}.pid`, and wraps SSH in a retry loop with exponential backoff (2s base, 60s cap, 10 retries). `stop` kills supervisor and ssh, removes PID files and run metadata, and preserves logs. `status` and `list` read config + PID files for display. `log` reads current run metadata and streams/follows only the current start session logs.

## Key Directories

| Directory | Purpose |
|-----------|---------|
| `cmd/ssh-tunnel-daemon/` | CLI entry point, cobra commands, `main.go` + `main_test.go` |
| `internal/daemon/` | Tunnel lifecycle, supervisor watchdog, PID file and run metadata helpers |
| `internal/config/` | YAML config model, XDG path resolution, port parser |
| `internal/prompt/` | Interactive forms (select tunnel, create tunnel, multi-select for stop) |
| `internal/logger/` | Rotating session log writer, session streaming/following, old log cleanup |
| `internal/version/` | Single `var Version = "dev"`, overridden by ldflags |
| `.github/workflows/` | Release Please + GoReleaser CI on main push |

## Development Commands

All commands use [mise](https://mise.jdx.dev/) as task runner:

```bash
mise run build     # go build → bin/ssh-tunnel-daemon (with version ldflags)
mise run test      # go test ./... -v
mise run clean     # rm -rf bin/
mise run install   # cp to ~/.local/bin/ + sshtnl/s17n symlinks
mise run uninstall # rm binaries + symlinks
```

Go 1.26.3 is managed by mise (`mise.toml`). Dependencies: `spf13/cobra`, `spf13/viper`, `charmbracelet/huh`, `charmbracelet/bubbletea`, `catppuccin/go` (theme).

Tests involving file I/O MUST run inside a `bwrap` sandbox:
```bash
bwrap --dev-bind / / --new-session --proc /proc \
  --tmpfs /tmp --dir /tmp/config --dir /tmp/state --dir /tmp/cache --dir /tmp/gocache \
  --setenv XDG_CONFIG_HOME /tmp/config --setenv XDG_STATE_HOME /tmp/state \
  --setenv XDG_CACHE_HOME /tmp/cache --setenv GOCACHE /tmp/gocache \
  --setenv GOMODCACHE "$HOME/go/pkg/mod" --setenv PATH "$PATH" \
  --chdir "$PWD" go test ./...
```

## Code Conventions & Common Patterns

### Packages

- Public functions in `internal/daemon` are the CLI's only permitted interface to process management. No external package calls `exec.Command("ssh")` directly.
- `internal/config` owns `Tunnel` / `Config` structs and all XDG path resolution. Other packages reference these through the config package only.
- `internal/prompt` gates all interactive forms on `isTTY()`. Non-TTY → error instructing user to pass args explicitly.

### Structs & Types

```go
// config.Tunnel — the core type, used everywhere
type Tunnel struct {
    Name   string
    Target string // e.g. "user@host"
    Ports  []int
    Mode   string // "local" or "remote"
}

// daemon.TunnelStatus — runtime state
type TunnelStatus struct {
    Name    string
    Target  string
    Mode    string
    Ports   []int
    PID     int
    Running bool
}
```

### Error Handling

- Functions in `daemon` return `(result, error)`, never panic.
- `errors.New` for static messages, `fmt.Errorf("…: %w", err)` for wrapping.
- CLI RunE functions accumulate errors with a `hadError` bool and return `errors.New("one or more operations failed")` at the end.
- Guardian clauses (early return on error) preferred; no else after return.

### Concurrency

- Supervisor uses `sync.Mutex` to guard the current `*exec.Cmd`.
- Supervisor is the long-lived owner of log writing. It pipes ssh stdout/stderr into `logger.LineWriter`; supervisor event lines are written to the same session log with a `[supervisor]` prefix.
- `signal.NotifyContext` used for graceful shutdown on SIGINT/SIGTERM.
- Tests use `t.Parallel()` extensively except integration tests that spawn real child processes (supervisor retry/kill tests).

### PID Files

- Tunnel PID: `$XDG_STATE_HOME/ssh-tunnel-daemon/{name}.pid`
- Supervisor PID: `$XDG_STATE_HOME/ssh-tunnel-daemon/{name}.supervisor.pid`
- Current start session metadata: `$XDG_STATE_HOME/ssh-tunnel-daemon/{name}.run.json`
- PID file content: decimal PID as a string (no newline).
- `isProcessAlive(pid)` sends signal 0 via `syscall.Kill`.
- Corrupt PID files (garbage, out-of-range) are treated as "not running".
- `stop` removes the tunnel PID, supervisor PID, and run metadata. It does not remove logs.

### Logs

- Log directory: `$XDG_STATE_HOME/ssh-tunnel-daemon/logs`.
- Session logs are grouped by safe tunnel name: `logs/{safeTunnel}/session_{sessionID}_{000001...}.log`.
- Default rotation threshold: 1000 lines per segment (`logger.DefaultMaxLines`).
- `log [tunnel_name]` outputs current start session logs in forward order.
- `log -f/--follow [tunnel_name]` outputs existing current session logs, then follows appended data and new rotated segments.
- If no tunnel name is passed to `log`, use the interactive tunnel selector. Non-TTY behavior comes from `internal/prompt`.
- If run metadata is missing, `log` must fail explicitly rather than guessing historical logs.
- Supervisor logs are not separate files; write supervisor events into the tunnel session log with `[supervisor]`.

### Port Handling

- `config.ParsePorts` deduplicates, sorts, and validates ports in `[1, 65535]`.
- `config.FormatPorts` formats `[]int` back to comma-separated string.
- Config loading/saving always normalizes ports via `uniqueSortedPorts`.

## Important Files

| File | Role |
|------|------|
| `cmd/ssh-tunnel-daemon/main.go` | All CLI commands, flag definitions, output formatting |
| `internal/daemon/daemon.go` | Tunnel lifecycle (Start/Stop/GetStatus/ListRunning/WaitForTunnelPID) |
| `internal/daemon/supervisor.go` | Supervisor struct, WatchTunnel retry loop, StartSupervisor subprocess |
| `internal/daemon/run.go` | Current start session metadata helpers |
| `internal/config/config.go` | Config struct, XDG path helpers, LoadConfig/SaveConfig, FindTunnel |
| `internal/logger/logger.go` | Rotating line writer, session stream/follow helpers, cleanup |
| `internal/prompt/prompt.go` | SelectTunnel, CreateTunnel, MultiSelectRunning forms |
| `go.mod` | Module path `github.com/northwang-lucky/ssh-tunnel-daemon` |
| `mise.toml` | Go version, build/test/install/clean tasks |
| `.goreleaser.yaml` | Cross-compilation targets (darwin/linux, amd64/arm64), Homebrew formula |
| `.github/workflows/release-please.yml` | CI: Release Please → GoReleaser on main push |

## Runtime & Tooling

- **Go**: 1.26.3, managed by `mise`
- **Build**: `CGO_ENABLED=0` (static binaries), `-ldflags "-s -w -X …Version=…"` for release
- **Release automation**: Release Please (Google) → GoReleaser → Homebrew tap (`northwang-lucky/homebrew-tap`)
- **XDG paths**: `os.UserConfigDir()` + env vars, with `~/.config` / `~/.local/state` fallbacks
- **No runtime config**: Config file is YAML, run metadata is JSON, PID files are plain text, logs are plain text

## Testing & QA

- Framework: stdlib `testing`, no external test library.
- Conventions:
  - `t.Parallel()` on all non-integration tests.
  - `t.TempDir()` for disk isolation (auto-cleaned).
  - `t.Setenv("XDG_STATE_HOME", dir)` to isolate daemon state.
  - ssh-based integration tests skip with `t.Skip("ssh not found in PATH")`.
- Run: `mise run test` or `go test ./...`.
- Sandbox: file-mutating tests MUST run inside `bwrap` (see [Development Commands]).
