package sampler

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Steam Deck platform quirk: the chassis fan is not exposed by the GPU's
// amdgpu hwmon device; it belongs to the platform steamdeck_hwmon device.
// The quirk is deliberately narrow — it applies only on Valve hardware
// whose DMI product name identifies a Deck revision, and only to the
// Deck's own APU, never to another GPU attached to a Deck.

const (
	dmiIDPath     = "class/dmi/id"
	hwmonClassDir = "class/hwmon"
	hwmonNameFile = "name"
	pciDeviceFile = "device"

	dmiValveVendor = "valve"
	// DMI product names of the two Deck revisions.
	dmiJupiterName = "jupiter" // Steam Deck LCD
	dmiGalileoName = "galileo" // Steam Deck OLED

	steamdeckHwmonName = "steamdeck_hwmon"

	// PCI device ids of the Steam Deck APUs (vendor 0x1002).
	deckAPUDeviceVanGogh   = "163f" // LCD
	deckAPUDeviceSephiroth = "1435" // OLED
)

// isSteamDeck reports whether the host is a Valve Steam Deck, based on
// DMI identity. Missing or unreadable DMI files yield false.
func isSteamDeck(sysRoot *os.Root) bool {
	vendor, err := readRootTrimmed(sysRoot, filepath.Join(dmiIDPath, "sys_vendor"))
	if err != nil || !strings.EqualFold(vendor, dmiValveVendor) {
		return false
	}

	product, err := readRootTrimmed(sysRoot, filepath.Join(dmiIDPath, "product_name"))

	return err == nil && isDeckProductName(product)
}

func isDeckProductName(product string) bool {
	switch strings.ToLower(product) {
	case dmiJupiterName, dmiGalileoName:
		return true
	}

	return false
}

// isDeckAPU reports whether the GPU is one of the integrated AMD APUs
// used by the Steam Deck revisions.
func isDeckAPU(deviceRoot *os.Root) bool {
	device, err := readRootTrimmed(deviceRoot, pciDeviceFile)
	if err != nil {
		return false
	}

	switch normalizePCIID(device) {
	case deckAPUDeviceVanGogh, deckAPUDeviceSephiroth:
		return true
	}

	return false
}

// findHwmonByName returns a root for the first class/hwmon device whose
// name file matches and which exposes the requested input file, or nil.
// Non-matching candidates are closed again.
func findHwmonByName(sysRoot *os.Root, name, file string) *os.Root {
	if sysRoot == nil {
		return nil
	}

	entries, err := fs.ReadDir(sysRoot.FS(), hwmonClassDir)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() && entry.Type()&fs.ModeSymlink == 0 {
			continue
		}

		sub, err := sysRoot.OpenRoot(filepath.Join(hwmonClassDir, entry.Name()))
		if err != nil {
			continue
		}

		deviceName, err := readRootTrimmed(sub, hwmonNameFile)
		if err != nil || !strings.EqualFold(deviceName, name) || !fileExists(sub, file) {
			_ = sub.Close()

			continue
		}

		return sub
	}

	return nil
}

func readRootTrimmed(root *os.Root, name string) (string, error) {
	if root == nil {
		return "", fs.ErrNotExist
	}
	data, err := root.ReadFile(name)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(data)), nil
}

func normalizePCIID(id string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(id), "0x"))
}
