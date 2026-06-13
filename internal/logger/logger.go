// Package logger manages per-tunnel log files under XDG state home.
package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const logTimeFormat = "20060102_150405"

// NewLogFile creates a new log file for tunnelName under logDir. The file name
// embeds the tunnel name and the current local timestamp, matching the pattern
// {tunnel_name}_{yyyyMMdd_HHmmss}.log. The returned file handle should be
// closed by the caller.
func NewLogFile(logDir, tunnelName string) (*os.File, string, error) {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, "", fmt.Errorf("create log dir: %w", err)
	}

	safeName := sanitizeName(tunnelName)
	filename := fmt.Sprintf("%s_%s.log", safeName, time.Now().Local().Format(logTimeFormat))
	path := filepath.Join(logDir, filename)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, "", fmt.Errorf("open log file: %w", err)
	}

	return f, path, nil
}

// CleanupOldLogs removes .log files in logDir whose modification time is older
// than retention relative to now. It returns the first error encountered while
// iterating, but attempts to remove every candidate file regardless.
func CleanupOldLogs(logDir string, retention time.Duration) error {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read log dir: %w", err)
	}

	cutoff := time.Now().Add(-retention)
	var firstErr error

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".log" {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("stat %s: %w", entry.Name(), err)
			}
			continue
		}

		if info.ModTime().Before(cutoff) {
			path := filepath.Join(logDir, entry.Name())
			if err := os.Remove(path); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("remove %s: %w", entry.Name(), err)
			}
		}
	}

	return firstErr
}

func sanitizeName(name string) string {
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, string(filepath.Separator), "-")
	return name
}
