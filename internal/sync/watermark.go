package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// DefaultWatermarkPath returns the default sync-watermark file path for a
// bank: ~/.cache/paperless-bank/<bank>-sync-state.json.
func DefaultWatermarkPath(bankName string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "paperless-bank", bankName+"-sync-state.json")
}

// watermarkState is the on-disk record of the newest document date an
// Orchestrator run has fully accounted for.
type watermarkState struct {
	LatestDate time.Time `json:"latest_date"`
}

// loadWatermark returns the zero time (and no error) if path is empty or the
// file doesn't exist yet - both just mean "no watermark set, check everything".
func loadWatermark(path string) (time.Time, error) {
	if path == "" {
		return time.Time{}, nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is a locally configured cache file path, not user/network input
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	var st watermarkState
	if err := json.Unmarshal(data, &st); err != nil {
		return time.Time{}, err
	}
	return st.LatestDate, nil
}

func saveWatermark(path string, latest time.Time) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(watermarkState{LatestDate: latest}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
