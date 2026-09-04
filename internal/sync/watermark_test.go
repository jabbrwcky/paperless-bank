package sync

import (
	"path/filepath"
	"testing"
	"time"
)

func TestWatermark_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "watermark.json")
	want := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	if err := saveWatermark(path, want); err != nil {
		t.Fatalf("saveWatermark: %v", err)
	}
	got, err := loadWatermark(path)
	if err != nil {
		t.Fatalf("loadWatermark: %v", err)
	}
	if !got.Equal(want) {
		t.Errorf("loadWatermark = %v, want %v", got, want)
	}
}

func TestWatermark_MissingFileReturnsZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	got, err := loadWatermark(path)
	if err != nil {
		t.Fatalf("loadWatermark: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("loadWatermark = %v, want zero time", got)
	}
}

func TestWatermark_EmptyPathIsANoop(t *testing.T) {
	got, err := loadWatermark("")
	if err != nil || !got.IsZero() {
		t.Fatalf("loadWatermark(\"\") = %v, %v, want zero time, nil", got, err)
	}
	if err := saveWatermark("", time.Now()); err != nil {
		t.Fatalf("saveWatermark(\"\", ...) = %v, want nil", err)
	}
}
