package gpu

import (
	"bufio"
	"errors"
	"fmt"
	"io"
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

	drmPath := filepath.Join(root, drmClassPath)
	entries, err := os.ReadDir(drmPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logger.Warn("drm class path missing", "path", drmPath)
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

		cardPath := filepath.Join(drmPath, name)
		info, err := loadCardInfo(cardPath, root)
		if err != nil {
			logger.Warn("failed to load card info", "card", name, "err", err)
			continue
		}
		infos = append(infos, info)
	}

	return infos, nil
}

func loadCardInfo(cardPath, root string) (Info, error) {
	id := filepath.Base(cardPath)

	devicePath := filepath.Join(cardPath, "device")
	ueventPath := filepath.Join(devicePath, "uevent")

	var (
		pciSlot string
		pciID   string
		name    string
	)

	if data, err := os.ReadFile(ueventPath); err == nil {
		pciSlot = parseKeyValue(string(data), "PCI_SLOT_NAME")
		pciID = parseKeyValue(string(data), "PCI_ID")
		name = parseKeyValue(string(data), "PCI_ID_NAME")
		if name == "" {
			name = parseKeyValue(string(data), "DRIVER")
		}
	}

	if pciID == "" {
		if vendor, err := readTrim(filepath.Join(devicePath, "vendor")); err == nil {
			if device, err := readTrim(filepath.Join(devicePath, "device")); err == nil {
				pciID = formatHexPair(vendor, device)
			}
		}
	}

	if name == "" {
		name, _ = readTrim(filepath.Join(devicePath, "product_name"))
	}

	renderNode := findRenderNode(devicePath)

	return Info{
		ID:         id,
		PCI:        pciSlot,
		PCIID:      pciID,
		Name:       name,
		RenderNode: renderNode,
	}, nil
}

func findRenderNode(devicePath string) string {
	drmSubdir := filepath.Join(devicePath, "drm")
	entries, err := os.ReadDir(drmSubdir)
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

func readTrim(path string) (string, error) {
	data, err := os.ReadFile(path)
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
