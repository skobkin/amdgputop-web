package gpu

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	drmClassPath = "class/drm"
)

// Info describes a single GPU device discovered via sysfs.
type Info struct {
	ID         string `json:"id"`
	PCI        string `json:"pci"`
	PCIID      string `json:"pci_id"`
	Name       string `json:"name"`
	RenderNode string `json:"render_node"`
}

// Discover enumerates DRM cards exposed via sysfs under the provided root.
func Discover(root string, logger *slog.Logger) ([]Info, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	sysRoot, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open sysfs root: %w", err)
	}
	defer sysRoot.Close()

	entries, err := fs.ReadDir(sysRoot.FS(), drmClassPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
			logger.Warn("drm class path missing", "path", filepath.Join(root, drmClassPath))
			return nil, nil
		}
		return nil, fmt.Errorf("read drm class dir: %w", err)
	}

	var infos []Info
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "card") {
			continue
		}
		if strings.ContainsRune(name, '-') {
			continue
		}
		if !allDigits(name[4:]) {
			continue
		}

		if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			continue
		}

		cardRoot, err := sysRoot.OpenRoot(filepath.Join(drmClassPath, name))
		if err != nil {
			logger.Warn("failed to open card root", "card", name, "err", err)
			continue
		}

		info, err := loadCardInfo(name, cardRoot)
		if err := cardRoot.Close(); err != nil {
			logger.Debug("failed to close card root", "card", name, "err", err)
		}
		if err != nil {
			logger.Warn("failed to load card info", "card", name, "err", err)
			continue
		}
		infos = append(infos, info)
	}

	return infos, nil
}

func loadCardInfo(cardID string, cardRoot *os.Root) (Info, error) {
	deviceRoot, err := cardRoot.OpenRoot("device")
	if err != nil {
		return Info{}, fmt.Errorf("open device root: %w", err)
	}
	defer deviceRoot.Close()

	var (
		pciSlot   string
		pciID     string
		name      string
		subVendor string
		subDevice string
	)

	if data, err := deviceRoot.ReadFile("uevent"); err == nil {
		text := string(data)
		pciSlot = parseKeyValue(text, "PCI_SLOT_NAME")
		pciID = parseKeyValue(text, "PCI_ID")
		subsys := parseKeyValue(text, "PCI_SUBSYS_ID")
		if subsys != "" {
			parts := strings.SplitN(subsys, ":", 2)
			if len(parts) == 2 {
				subVendor = parts[0]
				subDevice = parts[1]
			}
		}
		name = parseKeyValue(text, "PCI_ID_NAME")
		if name == "" {
			name = parseKeyValue(text, "DRIVER")
		}
	}

	if pciID == "" {
		if vendor, err := readTrim(deviceRoot, "vendor"); err == nil {
			if device, err := readTrim(deviceRoot, "device"); err == nil {
				pciID = formatHexPair(vendor, device)
			}
		}
	}

	if name == "" {
		name, _ = readTrim(deviceRoot, "product_name")
	}

	if subVendor == "" {
		subVendor, _ = readTrim(deviceRoot, "subsystem_vendor")
	}
	if subDevice == "" {
		subDevice, _ = readTrim(deviceRoot, "subsystem_device")
	}

	vendorID, deviceID := splitPCIIdentifier(pciID)
	resolved := lookupGPUName(vendorID, deviceID, subVendor, subDevice)
	if shouldUseResolvedName(name, resolved) {
		name = resolved
	}

	renderNode := findRenderNode(deviceRoot)

	return Info{
		ID:         cardID,
		PCI:        pciSlot,
		PCIID:      pciID,
		Name:       name,
		RenderNode: renderNode,
	}, nil
}

func findRenderNode(deviceRoot *os.Root) string {
	drmRoot, err := deviceRoot.OpenRoot("drm")
	if err != nil {
		return ""
	}
	defer drmRoot.Close()

	entries, err := fs.ReadDir(drmRoot.FS(), ".")
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "renderD") {
			return filepath.Join("/dev/dri", name)
		}
	}
	return ""
}

func parseKeyValue(data, key string) string {
	prefix := key + "="
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func readTrim(root *os.Root, name string) (string, error) {
	data, err := root.ReadFile(name)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func formatHexPair(vendor, device string) string {
	return strings.TrimPrefix(vendor, "0x") + ":" + strings.TrimPrefix(device, "0x")
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
