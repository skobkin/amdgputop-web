package sampler

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func capabilitiesLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestCapabilitiesSysfsFull(t *testing.T) {
	t.Parallel()

	reader, err := NewReader("card0", filepath.Join("testdata", "sysfs_full"), filepath.Join("testdata", "debugfs_fallback"), capabilitiesLogger())
	if err != nil {
		t.Fatalf("NewReader returned error: %v", err)
	}

	caps := reader.Capabilities()
	if caps == nil {
		t.Fatalf("expected capabilities to be detected")
	}
	if !caps.GPUBusyPct || !caps.MemBusyPct {
		t.Fatalf("expected utilization capabilities, got %+v", caps)
	}
	if !caps.SCLKMHz || !caps.MCLKMHz {
		t.Fatalf("expected clock capabilities, got %+v", caps)
	}
	if !caps.TempC || !caps.FanRPM || !caps.PowerW {
		t.Fatalf("expected hwmon capabilities, got %+v", caps)
	}
	if !caps.VRAM || !caps.GTT {
		t.Fatalf("expected memory capabilities, got %+v", caps)
	}
}

func TestCapabilitiesDebugFSFallback(t *testing.T) {
	t.Parallel()

	reader, err := NewReader("card1", filepath.Join("testdata", "sysfs_fallback"), filepath.Join("testdata", "debugfs_fallback"), capabilitiesLogger())
	if err != nil {
		t.Fatalf("NewReader returned error: %v", err)
	}

	caps := reader.Capabilities()
	if caps == nil {
		t.Fatalf("expected capabilities to be detected")
	}
	// amdgpu_pm_info exposes all five fields; sysfs provides neither the
	// busy metrics, clocks, hwmon.
	if !caps.GPUBusyPct || !caps.SCLKMHz || !caps.MCLKMHz || !caps.TempC || !caps.PowerW {
		t.Fatalf("expected debugfs-backed capabilities, got %+v", caps)
	}
	if caps.MemBusyPct {
		t.Fatalf("expected MemBusyPct to be unsupported without mem_busy_percent")
	}
	if caps.FanRPM {
		t.Fatalf("expected FanRPM to be unsupported without hwmon fan input")
	}
	if !caps.VRAM || !caps.GTT {
		t.Fatalf("expected memory capabilities from mem_info files, got %+v", caps)
	}
}

func TestCapabilitiesPartialDebugFS(t *testing.T) {
	t.Parallel()

	sysfsRoot := filepath.Join("testdata", "sysfs_fallback")
	debugfsRoot := t.TempDir()
	writeFile(t, filepath.Join(debugfsRoot, "dri", "1", "amdgpu_pm_info"), "SCLK: 1200 MHz\n")

	reader, err := NewReader("card1", sysfsRoot, debugfsRoot, capabilitiesLogger())
	if err != nil {
		t.Fatalf("NewReader returned error: %v", err)
	}

	caps := reader.Capabilities()
	if !caps.SCLKMHz {
		t.Fatalf("expected sclk capability from exposed debugfs field, got %+v", caps)
	}
	if caps.GPUBusyPct || caps.MCLKMHz || caps.TempC || caps.PowerW {
		t.Fatalf("expected unexposed debugfs fields to stay unsupported, got %+v", caps)
	}
}

func TestCapabilitiesDebugFSPresenceIgnoresParseFailure(t *testing.T) {
	t.Parallel()

	sysfsRoot := filepath.Join("testdata", "sysfs_fallback")
	debugfsRoot := t.TempDir()
	writeFile(t, filepath.Join(debugfsRoot, "dri", "1", "amdgpu_pm_info"), "GPU Load: N/A\n")

	reader, err := NewReader("card1", sysfsRoot, debugfsRoot, capabilitiesLogger())
	if err != nil {
		t.Fatalf("NewReader returned error: %v", err)
	}

	if caps := reader.Capabilities(); !caps.GPUBusyPct {
		t.Fatalf("expected gpu_busy_pct capability for recognized field, got %+v", caps)
	}

	// The recognized but unparseable field still yields no sample value.
	if sample := reader.Sample(); sample.Metrics.GPUBusyPct != nil {
		t.Fatalf("expected nil GPUBusyPct value for unparseable field")
	}
}

func TestCapabilitiesUnreadableDebugFS(t *testing.T) {
	t.Parallel()

	sysfsRoot := filepath.Join("testdata", "sysfs_fallback")
	debugfsRoot := t.TempDir()
	// A directory at the file path makes ReadFile fail with a non-ErrNotExist
	// error regardless of the test user's privileges.
	if err := os.MkdirAll(filepath.Join(debugfsRoot, "dri", "1", "amdgpu_pm_info"), 0o750); err != nil {
		t.Fatalf("failed to create unreadable amdgpu_pm_info: %v", err)
	}

	reader, err := NewReader("card1", sysfsRoot, debugfsRoot, capabilitiesLogger())
	if err != nil {
		t.Fatalf("NewReader returned error: %v", err)
	}

	// Unknown support must not be reported as unsupported.
	caps := reader.Capabilities()
	if !caps.GPUBusyPct || !caps.SCLKMHz || !caps.MCLKMHz || !caps.TempC || !caps.PowerW {
		t.Fatalf("expected debugfs-backed metrics to stay visible, got %+v", caps)
	}
}

func TestCapabilitiesWithoutDebugFS(t *testing.T) {
	t.Parallel()

	reader, err := NewReader("card1", filepath.Join("testdata", "sysfs_fallback"), t.TempDir(), capabilitiesLogger())
	if err != nil {
		t.Fatalf("NewReader returned error: %v", err)
	}

	caps := reader.Capabilities()
	if caps.GPUBusyPct || caps.SCLKMHz || caps.MCLKMHz || caps.TempC || caps.PowerW {
		t.Fatalf("expected debugfs-backed metrics to be unsupported, got %+v", caps)
	}
	if caps.MemBusyPct || caps.FanRPM {
		t.Fatalf("expected utilization and fan to be unsupported, got %+v", caps)
	}
	if !caps.VRAM || !caps.GTT {
		t.Fatalf("expected memory capabilities from mem_info files, got %+v", caps)
	}
}

func TestCapabilitiesNilForZeroValueReader(t *testing.T) {
	t.Parallel()

	reader := &Reader{
		cardID: "card0",
		logger: capabilitiesLogger(),
	}

	if caps := reader.Capabilities(); caps != nil {
		t.Fatalf("expected nil capabilities for zero-value reader, got %+v", caps)
	}
}
