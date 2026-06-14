package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type RunMetadata struct {
	Name          string    `json:"name"`
	SessionID     string    `json:"session_id"`
	StartedAt     time.Time `json:"started_at"`
	SupervisorPID int       `json:"supervisor_pid"`
	CurrentPID    int       `json:"current_pid"`
	MaxLines      int       `json:"max_lines"`
	LogPath       string    `json:"log_path"`
}

func RunMetadataPath(stateDir, name string) string {
	return filepath.Join(stateDir, fmt.Sprintf("%s.run.json", name))
}

func writeRunMetadata(stateDir string, meta RunMetadata) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal run metadata: %w", err)
	}
	if err := os.WriteFile(RunMetadataPath(stateDir, meta.Name), data, 0o644); err != nil {
		return fmt.Errorf("write run metadata: %w", err)
	}
	return nil
}

func ReadRunMetadata(stateDir, name string) (RunMetadata, error) {
	data, err := os.ReadFile(RunMetadataPath(stateDir, name))
	if err != nil {
		return RunMetadata{}, err
	}
	var meta RunMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return RunMetadata{}, fmt.Errorf("unmarshal run metadata: %w", err)
	}
	return meta, nil
}

func RemoveRunMetadata(stateDir, name string) {
	_ = os.Remove(RunMetadataPath(stateDir, name))
}

func newSessionID(now time.Time, pid int) string {
	return fmt.Sprintf("%s_%d", now.Local().Format("20060102_150405"), pid)
}
