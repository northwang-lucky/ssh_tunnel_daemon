package logger

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewLogFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	f, path, err := NewLogFile(dir, "web")
	if err != nil {
		t.Fatalf("NewLogFile: %v", err)
	}
	defer f.Close()

	base := filepath.Base(path)
	if !strings.HasPrefix(base, "web_") || !strings.HasSuffix(base, ".log") {
		t.Errorf("unexpected log file name: %s", base)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file to exist: %v", err)
	}
}

func TestNewLogFileCreatesDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logDir := filepath.Join(dir, "nested", "logs")

	_, path, err := NewLogFile(logDir, "api")
	if err != nil {
		t.Fatalf("NewLogFile: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file to exist: %v", err)
	}
}

func TestCleanupOldLogs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	fresh := filepath.Join(dir, "fresh.log")
	old := filepath.Join(dir, "old.log")
	other := filepath.Join(dir, "other.txt")

	if err := os.WriteFile(fresh, []byte("fresh"), 0o644); err != nil {
		t.Fatalf("write fresh: %v", err)
	}
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatalf("write old: %v", err)
	}
	if err := os.WriteFile(other, []byte("other"), 0o644); err != nil {
		t.Fatalf("write other: %v", err)
	}

	fourDaysAgo := time.Now().Add(-4 * 24 * time.Hour)
	if err := os.Chtimes(old, fourDaysAgo, fourDaysAgo); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	if err := CleanupOldLogs(dir, 3*24*time.Hour); err != nil {
		t.Fatalf("CleanupOldLogs: %v", err)
	}

	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh.log should be kept: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("old.log should be removed")
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("other.txt should be kept: %v", err)
	}
}

func TestCleanupOldLogsMissingDir(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "missing")
	if err := CleanupOldLogs(dir, 3*24*time.Hour); err != nil {
		t.Errorf("unexpected error for missing dir: %v", err)
	}
}

func TestSanitizeName(t *testing.T) {
	t.Parallel()

	if got := sanitizeName("a/b"); got != "a-b" {
		t.Errorf("sanitizeName(a/b) = %q, want a-b", got)
	}
}

func TestLineWriterRotatesByLineCount(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	w, first, err := NewLineWriter(dir, "web/api", "session-1", 2)
	if err != nil {
		t.Fatalf("NewLineWriter: %v", err)
	}
	for _, line := range []string{"one", "two", "three"} {
		if err := w.WriteLine(line); err != nil {
			t.Fatalf("WriteLine: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if first != SessionSegmentPath(dir, "web/api", "session-1", 1) {
		t.Errorf("first path = %q", first)
	}
	firstData, err := os.ReadFile(SessionSegmentPath(dir, "web/api", "session-1", 1))
	if err != nil {
		t.Fatalf("read first segment: %v", err)
	}
	if string(firstData) != "one\ntwo\n" {
		t.Errorf("first segment = %q", string(firstData))
	}
	secondData, err := os.ReadFile(SessionSegmentPath(dir, "web/api", "session-1", 2))
	if err != nil {
		t.Fatalf("read second segment: %v", err)
	}
	if string(secondData) != "three\n" {
		t.Errorf("second segment = %q", string(secondData))
	}
}

func TestStreamSessionOrdersSegments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	w, _, err := NewLineWriter(dir, "web", "s1", 1)
	if err != nil {
		t.Fatalf("NewLineWriter: %v", err)
	}
	for _, line := range []string{"a", "b", "c"} {
		if err := w.WriteLine(line); err != nil {
			t.Fatalf("WriteLine: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var out bytes.Buffer
	if err := StreamSession(&out, dir, "web", "s1"); err != nil {
		t.Fatalf("StreamSession: %v", err)
	}
	if out.String() != "a\nb\nc\n" {
		t.Errorf("stream = %q", out.String())
	}
}

func TestSafeName(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"web/api":       "web-api",
		" user@host:22": "user-host-22",
		"***":           "tunnel",
	}
	for in, want := range cases {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
	}
}
