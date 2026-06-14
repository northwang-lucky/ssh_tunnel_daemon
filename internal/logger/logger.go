// Package logger manages per-tunnel log files under XDG state home.
package logger

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const logTimeFormat = "20060102_150405"
const segmentNameFormat = "session_%s_%06d.log"

// DefaultMaxLines is the default maximum number of lines written to a single
// session log segment before rotation.
const DefaultMaxLines = 1000

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

// SessionLogDir returns the directory used for one tunnel's session logs.
func SessionLogDir(logDir, tunnelName string) string {
	return filepath.Join(logDir, SafeName(tunnelName))
}

// SessionSegmentPath returns the path for a session log segment.
func SessionSegmentPath(logDir, tunnelName, sessionID string, index int) string {
	return filepath.Join(SessionLogDir(logDir, tunnelName), fmt.Sprintf(segmentNameFormat, sessionID, index))
}

// LineWriter writes newline-delimited log records and rotates files after a
// fixed number of lines.
type LineWriter struct {
	mu        sync.Mutex
	logDir    string
	tunnel    string
	sessionID string
	maxLines  int
	index     int
	lineCount int
	file      *os.File
}

// NewLineWriter creates a rotating line writer for a tunnel session. It returns
// the writer and the first segment path.
func NewLineWriter(logDir, tunnelName, sessionID string, maxLines int) (*LineWriter, string, error) {
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	w := &LineWriter{
		logDir:    logDir,
		tunnel:    tunnelName,
		sessionID: sessionID,
		maxLines:  maxLines,
		index:     1,
	}
	if err := w.openSegment(); err != nil {
		return nil, "", err
	}
	return w, SessionSegmentPath(logDir, tunnelName, sessionID, 1), nil
}

// WriteLine writes one logical log line, appending a newline if needed.
func (w *LineWriter) WriteLine(line string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return os.ErrClosed
	}
	if w.lineCount >= w.maxLines {
		if err := w.rotate(); err != nil {
			return err
		}
	}
	line = strings.TrimRight(line, "\r\n")
	if _, err := fmt.Fprintln(w.file, line); err != nil {
		return fmt.Errorf("write log line: %w", err)
	}
	w.lineCount++
	return nil
}

// Close closes the currently open segment.
func (w *LineWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *LineWriter) rotate() error {
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return fmt.Errorf("close log segment: %w", err)
		}
	}
	w.index++
	w.lineCount = 0
	return w.openSegment()
}

func (w *LineWriter) openSegment() error {
	dir := SessionLogDir(w.logDir, w.tunnel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	path := SessionSegmentPath(w.logDir, w.tunnel, w.sessionID, w.index)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log segment: %w", err)
	}
	w.file = f
	return nil
}

// SessionSegments returns all segment paths for a tunnel session in ascending
// segment order.
func SessionSegments(logDir, tunnelName, sessionID string) ([]string, error) {
	pattern := filepath.Join(SessionLogDir(logDir, tunnelName), fmt.Sprintf("session_%s_*.log", sessionID))
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob session logs: %w", err)
	}
	sort.Strings(paths)
	return paths, nil
}

// StreamSession writes every existing segment for a tunnel session in ascending
// order.
func StreamSession(out io.Writer, logDir, tunnelName, sessionID string) error {
	paths, err := SessionSegments(logDir, tunnelName, sessionID)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if _, err := copyFile(out, path, 0); err != nil {
			return err
		}
	}
	return nil
}

// FollowSession streams existing session logs and then polls for appended data
// or newly rotated segments until ctx is cancelled.
func FollowSession(ctx context.Context, out io.Writer, logDir, tunnelName, sessionID string, pollInterval time.Duration) error {
	if pollInterval <= 0 {
		pollInterval = 500 * time.Millisecond
	}

	index := 1
	var offset int64
	for {
		path := SessionSegmentPath(logDir, tunnelName, sessionID, index)
		info, err := os.Stat(path)
		if err == nil {
			if info.Size() < offset {
				offset = 0
			}
			n, err := copyFile(out, path, offset)
			if err != nil {
				return err
			}
			offset += n

			next := SessionSegmentPath(logDir, tunnelName, sessionID, index+1)
			if _, err := os.Stat(next); err == nil {
				index++
				offset = 0
				continue
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat log segment: %w", err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func copyFile(out io.Writer, path string, offset int64) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open log segment: %w", err)
	}
	defer f.Close()

	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return 0, fmt.Errorf("seek log segment: %w", err)
		}
	}
	n, err := io.Copy(out, f)
	if err != nil {
		return n, fmt.Errorf("copy log segment: %w", err)
	}
	return n, nil
}

// CleanupOldLogs removes .log files in logDir whose modification time is older
// than retention relative to now. It returns the first error encountered while
// iterating, but attempts to remove every candidate file regardless.
func CleanupOldLogs(logDir string, retention time.Duration) error {
	_, err := os.Stat(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat log dir: %w", err)
	}

	cutoff := time.Now().Add(-retention)
	var firstErr error

	err = filepath.WalkDir(logDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return nil
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".log" {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("stat %s: %w", entry.Name(), err)
			}
			return nil
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("remove %s: %w", entry.Name(), err)
			}
		}
		return nil
	})
	if err != nil && firstErr == nil {
		firstErr = fmt.Errorf("walk log dir: %w", err)
	}

	return firstErr
}

func sanitizeName(name string) string {
	return SafeName(name)
}

// SafeName converts a tunnel name into a stable single path component.
func SafeName(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.TrimSpace(name) {
		ok := unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-'
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	safe := strings.Trim(b.String(), "-")
	if safe == "" {
		return "tunnel"
	}
	return safe
}
