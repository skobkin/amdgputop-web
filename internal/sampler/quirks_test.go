package sampler

import (
	"os"
	"path/filepath"
	"testing"
)

// createDeckLikeSysfs writes a minimal Deck-like sysfs tree: DMI identity
// for the given vendor and product name, a card0 device with the given PCI
// device id plus busy and memory sources, an amdgpu hwmon without a fan,
// and — when withPlatformHwmon is set — a platform steamdeck_hwmon with a
// fan1_input. It returns the sysfs root path.
func createDeckLikeSysfs(t *testing.T, vendor, productName, pciDevice string, withPlatformHwmon bool) string {
	t.Helper()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "class", "dmi", "id", "sys_vendor"), vendor+"\n")
	writeFile(t, filepath.Join(root, "class", "dmi", "id", "product_name"), productName+"\n")

	deviceDir := filepath.Join(root, "class", "drm", "card0", "device")
	writeFile(t, filepath.Join(deviceDir, "device"), pciDevice+"\n")
	writeFile(t, filepath.Join(deviceDir, "gpu_busy_percent"), "40\n")
	writeFile(t, filepath.Join(deviceDir, "mem_info_vram_used"), "1048576\n")
	writeFile(t, filepath.Join(deviceDir, "hwmon", "hwmon0", "name"), "amdgpu\n")
	writeFile(t, filepath.Join(deviceDir, "hwmon", "hwmon0", "temp1_input"), "59000\n")

	if withPlatformHwmon {
		writeFile(t, filepath.Join(root, "class", "hwmon", "hwmon3", "name"), "steamdeck_hwmon\n")
		writeFile(t, filepath.Join(root, "class", "hwmon", "hwmon3", "fan1_input"), "4290\n")
	}

	return root
}

func newDeckTestReader(t *testing.T, sysfsRoot string) *Reader {
	t.Helper()

	reader, err := NewReader("card0", sysfsRoot, "", capabilitiesLogger())
	if err != nil {
		t.Fatalf("NewReader returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = reader.Close()
	})

	return reader
}

func TestSteamdeckFanFromPlatformHwmon(t *testing.T) {
	t.Parallel()

	reader := newDeckTestReader(t, filepath.Join("testdata", "sysfs_steamdeck"))

	caps := reader.Capabilities()
	if caps.MemBusyPct {
		t.Fatalf("expected MemBusyPct to be unsupported on Deck APU, got %+v", caps)
	}
	if !caps.FanRPM {
		t.Fatalf("expected FanRPM capability from platform hwmon, got %+v", caps)
	}
	if !caps.GPUBusyPct || !caps.SCLKMHz || !caps.MCLKMHz || !caps.TempC || !caps.PowerW || !caps.VRAM || !caps.GTT {
		t.Fatalf("expected remaining capabilities to be supported, got %+v", caps)
	}

	sample := reader.Sample()
	assertFloatEqual(t, sample.Metrics.FanRPM, 4290)
	// The quirk must redirect the fan only; temp and power stay on the
	// GPU's amdgpu hwmon (steamdeck_hwmon also exposes a bogus temp).
	assertFloatEqual(t, sample.Metrics.TempC, 59)
	assertFloatEqual(t, sample.Metrics.PowerW, 16.161)
	assertFloatEqual(t, sample.Metrics.GPUBusyPct, 40)
	if sample.Metrics.MemBusyPct != nil {
		t.Fatalf("expected MemBusyPct to be nil on Deck APU")
	}
}

func TestSteamdeckLCDVariantUsesPlatformHwmon(t *testing.T) {
	t.Parallel()

	sysfsRoot := createDeckLikeSysfs(t, "Valve", "Jupiter", "0x163f", true)
	reader := newDeckTestReader(t, sysfsRoot)

	if caps := reader.Capabilities(); !caps.FanRPM {
		t.Fatalf("expected FanRPM capability on Deck LCD, got %+v", caps)
	}
	assertFloatEqual(t, reader.Sample().Metrics.FanRPM, 4290)
}

func TestSteamdeckQuirkRequiresValveDMI(t *testing.T) {
	t.Parallel()

	// Same platform hwmon layout, but the host is not Valve hardware.
	sysfsRoot := createDeckLikeSysfs(t, "ASUSTeK Computer Inc.", "Galileo", "0x1435", true)
	reader := newDeckTestReader(t, sysfsRoot)

	if caps := reader.Capabilities(); caps.FanRPM {
		t.Fatalf("expected FanRPM to be unsupported without Valve DMI, got %+v", caps)
	}
	if sample := reader.Sample(); sample.Metrics.FanRPM != nil {
		t.Fatalf("expected FanRPM to stay unread without Valve DMI")
	}
}

func TestSteamdeckQuirkRequiresDeckProductName(t *testing.T) {
	t.Parallel()

	// product_family alone (Sephiroth) is not Deck identity; the quirk
	// matches the established Jupiter/Galileo product names.
	sysfsRoot := createDeckLikeSysfs(t, "Valve", "Sephiroth", "0x1435", true)
	reader := newDeckTestReader(t, sysfsRoot)

	if caps := reader.Capabilities(); caps.FanRPM {
		t.Fatalf("expected FanRPM to be unsupported without a Deck product name, got %+v", caps)
	}
}

func TestSteamdeckQuirkSkipsDiscreteGPU(t *testing.T) {
	t.Parallel()

	// A discrete GPU docked to a Deck must not adopt the chassis fan.
	sysfsRoot := createDeckLikeSysfs(t, "Valve", "Galileo", "0x73df", true)
	reader := newDeckTestReader(t, sysfsRoot)

	if caps := reader.Capabilities(); caps.FanRPM {
		t.Fatalf("expected FanRPM to be unsupported for a non-Deck GPU, got %+v", caps)
	}
}

func TestSteamdeckQuirkIgnoresOtherHwmonDevices(t *testing.T) {
	t.Parallel()

	sysfsRoot := createDeckLikeSysfs(t, "Valve", "Galileo", "0x1435", false)
	// A non-platform device that happens to expose a fan input must not
	// satisfy the quirk; matching is by hwmon name, never by filename.
	writeFile(t, filepath.Join(sysfsRoot, "class", "hwmon", "hwmon4", "name"), "nvme\n")
	writeFile(t, filepath.Join(sysfsRoot, "class", "hwmon", "hwmon4", "fan1_input"), "2000\n")

	reader := newDeckTestReader(t, sysfsRoot)

	if caps := reader.Capabilities(); caps.FanRPM {
		t.Fatalf("expected FanRPM to be unsupported without steamdeck_hwmon, got %+v", caps)
	}
}

func TestSteamdeckQuirkWithoutPlatformHwmon(t *testing.T) {
	t.Parallel()

	sysfsRoot := createDeckLikeSysfs(t, "Valve", "Galileo", "0x1435", false)
	reader := newDeckTestReader(t, sysfsRoot)

	if caps := reader.Capabilities(); caps.FanRPM {
		t.Fatalf("expected FanRPM to be unsupported without platform hwmon, got %+v", caps)
	}
	assertNilFloat(t, reader.Sample().Metrics.FanRPM)
}

func TestSteamdeckQuirkDoesNotOverrideGenericFan(t *testing.T) {
	t.Parallel()

	sysfsRoot := createDeckLikeSysfs(t, "Valve", "Galileo", "0x1435", true)
	// The GPU's own hwmon also exposes a fan: the generic source must win.
	writeFile(t, filepath.Join(sysfsRoot, "class", "drm", "card0", "device", "hwmon", "hwmon0", "fan1_input"), "1200\n")

	reader := newDeckTestReader(t, sysfsRoot)

	assertFloatEqual(t, reader.Sample().Metrics.FanRPM, 1200)
}

func TestIsSteamDeckMissingDMI(t *testing.T) {
	t.Parallel()

	sysRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	defer sysRoot.Close()

	if isSteamDeck(sysRoot) {
		t.Fatalf("expected isSteamDeck to be false without DMI identity")
	}
}

// assertNilFloat asserts that a nullable float sample value is absent.
func assertNilFloat(t *testing.T, value *float64) {
	t.Helper()
	if value != nil {
		t.Fatalf("expected nil float value, got %.4f", *value)
	}
}
