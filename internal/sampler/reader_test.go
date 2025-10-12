package sampler

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"
)

func TestReaderSampleSysfs(t *testing.T) {
	t.Parallel()

	sysfsRoot := filepath.Join("testdata", "sysfs_full")
	debugfsRoot := filepath.Join("testdata", "debugfs_fallback")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	reader, err := NewReader("card0", sysfsRoot, debugfsRoot, logger)
	if err != nil {
		t.Fatalf("NewReader returned error: %v", err)
	}

	sample := reader.Sample()
	if sample.GPUId != "card0" {
		t.Fatalf("unexpected GPU id %q", sample.GPUId)
	}
	if sample.Timestamp.IsZero() {
		t.Fatalf("expected timestamp to be set")
	}

	assertFloatEqual(t, sample.Metrics.GPUBusyPct, 47)
	assertFloatEqual(t, sample.Metrics.MemBusyPct, 31)
	assertFloatEqual(t, sample.Metrics.SCLKMHz, 1000)
	assertFloatEqual(t, sample.Metrics.MCLKMHz, 900)
	assertFloatEqual(t, sample.Metrics.TempC, 65)
	assertFloatEqual(t, sample.Metrics.FanRPM, 1200)
	assertFloatEqual(t, sample.Metrics.PowerW, 120)

	assertUintEqual(t, sample.Metrics.VRAMUsedBytes, 104857600)
	assertUintEqual(t, sample.Metrics.VRAMTotalBytes, 2147483648)
	assertUintEqual(t, sample.Metrics.GTTUsedBytes, 52428800)
	assertUintEqual(t, sample.Metrics.GTTTotalBytes, 4294967296)
}

func TestReaderSampleDebugFallback(t *testing.T) {
	t.Parallel()

	sysfsRoot := filepath.Join("testdata", "sysfs_fallback")
	debugfsRoot := filepath.Join("testdata", "debugfs_fallback")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	reader, err := NewReader("card1", sysfsRoot, debugfsRoot, logger)
	if err != nil {
		t.Fatalf("NewReader returned error: %v", err)
	}

	sample := reader.Sample()

	assertFloatEqual(t, sample.Metrics.GPUBusyPct, 76)
	assertFloatEqual(t, sample.Metrics.SCLKMHz, 1200)
	assertFloatEqual(t, sample.Metrics.MCLKMHz, 1100)
	assertFloatEqual(t, sample.Metrics.TempC, 70)
	assertFloatEqual(t, sample.Metrics.PowerW, 100)

	if sample.Metrics.MemBusyPct != nil {
		t.Fatalf("expected MemBusyPct to be nil when sysfs metric missing")
	}
	if sample.Metrics.FanRPM != nil {
		t.Fatalf("expected FanRPM to be nil without hwmon data")
	}

	assertUintEqual(t, sample.Metrics.VRAMTotalBytes, 17179869184)
	assertUintEqual(t, sample.Metrics.GTTTotalBytes, 34359738368)
}

func assertFloatEqual(t *testing.T, value *float64, expected float64) {
	t.Helper()
	if value == nil {
		t.Fatalf("expected float value %.2f, got nil", expected)
	}
	if diff := *value - expected; diff < -0.0001 || diff > 0.0001 {
		t.Fatalf("expected %.2f, got %.4f", expected, *value)
	}
}

func assertUintEqual(t *testing.T, value *uint64, expected uint64) {
	t.Helper()
	if value == nil {
		t.Fatalf("expected uint value %d, got nil", expected)
	}
	if *value != expected {
		t.Fatalf("expected %d, got %d", expected, *value)
	}
}
