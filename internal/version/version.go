// Package version provides the current version of ssh-tunnel-daemon.
// The Version variable is set at build time via ldflags (e.g., -X
// github.com/northwang-lucky/ssh-tunnel-daemon/internal/version.Version=v0.1.0)
// and defaults to "dev" for local development builds.
package version

// Version holds the current ssh-tunnel-daemon version string.
// Override with ldflags during release builds.
var Version = "dev"
