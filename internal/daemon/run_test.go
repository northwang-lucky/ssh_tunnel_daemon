package daemon

import (
	"os"
	"testing"
	"time"
)

func TestRunMetadataRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	meta := RunMetadata{
		Name:          "web",
		SessionID:     "session-1",
		StartedAt:     time.Unix(100, 0).UTC(),
		SupervisorPID: 123,
		CurrentPID:    456,
		MaxLines:      1000,
		LogPath:       "/tmp/log",
	}
	if err := writeRunMetadata(dir, meta); err != nil {
		t.Fatalf("writeRunMetadata: %v", err)
	}
	got, err := ReadRunMetadata(dir, "web")
	if err != nil {
		t.Fatalf("ReadRunMetadata: %v", err)
	}
	if got.Name != meta.Name || got.SessionID != meta.SessionID || got.SupervisorPID != meta.SupervisorPID || got.CurrentPID != meta.CurrentPID || got.MaxLines != meta.MaxLines || got.LogPath != meta.LogPath {
		t.Fatalf("metadata mismatch: got %+v want %+v", got, meta)
	}

	RemoveRunMetadata(dir, "web")
	if _, err := os.Stat(RunMetadataPath(dir, "web")); !os.IsNotExist(err) {
		t.Fatalf("run metadata should be removed")
	}
}
