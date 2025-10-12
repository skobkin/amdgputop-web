package gpu

import (
	"strings"
	"sync"

	"github.com/jaypipes/pcidb"
)

var (
	pciOnce sync.Once
	pciDB   *pcidb.PCIDB
	pciErr  error
)

func lookupGPUName(vendorID, deviceID, subVendorID, subDeviceID string) string {
	vendorID = normalizePCIID(vendorID)
	deviceID = normalizePCIID(deviceID)
	if vendorID == "" || deviceID == "" {
		return ""
	}

	db := loadPCIDatabase()
	if db == nil {
		return ""
	}

	productKey := vendorID + deviceID
	product, ok := db.Products[productKey]
	if !ok || product == nil {
		return ""
	}

	subVendorID = normalizePCIID(subVendorID)
	subDeviceID = normalizePCIID(subDeviceID)
	if subVendorID != "" && subDeviceID != "" {
		for _, subsystem := range product.Subsystems {
			if subsystem == nil {
				continue
			}
			if strings.EqualFold(subsystem.VendorID, subVendorID) && strings.EqualFold(subsystem.ID, subDeviceID) {
				if subsystem.Name != "" {
					return subsystem.Name
				}
			}
		}
	}

	return product.Name
}

func loadPCIDatabase() *pcidb.PCIDB {
	pciOnce.Do(func() {
		pciDB, pciErr = pcidb.New()
	})
	if pciErr != nil || pciDB == nil {
		return nil
	}
	return pciDB
}

func normalizePCIID(raw string) string {
	if raw == "" {
		return ""
	}
	value := strings.TrimSpace(raw)
	value = strings.TrimPrefix(value, "0x")
	value = strings.TrimPrefix(value, "0X")
	if value == "" {
		return ""
	}
	value = strings.ToLower(value)
	if len(value) < 4 {
		value = strings.Repeat("0", 4-len(value)) + value
	}
	return value
}

func splitPCIIdentifier(pciID string) (vendorID string, deviceID string) {
	if pciID == "" {
		return "", ""
	}
	parts := strings.SplitN(pciID, ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func shouldUseResolvedName(current, resolved string) bool {
	if resolved == "" {
		return false
	}
	if current == "" {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(current))
	if lower == "" {
		return true
	}
	switch lower {
	case "amdgpu", "radeon", "unknown":
		return true
	}
	if strings.HasPrefix(lower, "pci device") {
		return true
	}
	if strings.HasPrefix(lower, "0x") {
		return true
	}
	return false
}
