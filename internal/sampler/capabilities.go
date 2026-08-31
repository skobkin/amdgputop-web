package sampler

import (
	"bufio"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"strings"

	"github.com/skobkin/amdgputop-web/internal/gpu"
)

// fileExists reports whether name is reachable under root. Existence —
// not readability or parseability — is deliberate: a present source must
// stay "supported" so transient read failures cannot flip capabilities.
func fileExists(root *os.Root, name string) bool {
	if root == nil {
		return false
	}
	_, err := root.Stat(name)

	return err == nil
}

// detectCapabilities probes the same sources Sample() reads, once at
// reader construction, so a false flag means "known unsupported" and
// never "temporarily unavailable". Supported-but-empty metrics keep
// sampling and surface as null sample values.
func (r *Reader) detectCapabilities() *gpu.Capabilities {
	caps := &gpu.Capabilities{
		GPUBusyPct: fileExists(r.deviceRoot, gpuBusyFilename),
		MemBusyPct: fileExists(r.deviceRoot, memBusyFilename),
		SCLKMHz:    fileExists(r.deviceRoot, ppDpmSclkFilename),
		MCLKMHz:    fileExists(r.deviceRoot, ppDpmMclkFilename),
		VRAM:       fileExists(r.deviceRoot, memInfoVRAMUsedFile),
		GTT:        fileExists(r.deviceRoot, memInfoGTTUsedFile),
		TempC:      fileExists(r.hwmonRoot, hwmonTempFile),
		FanRPM:     fileExists(r.hwmonRoot, hwmonFanFile) || fileExists(r.fanRoot, hwmonFanFile),
		PowerW:     fileExists(r.hwmonRoot, hwmonPowerAverageFile) || fileExists(r.hwmonRoot, hwmonPowerInputFile),
	}

	r.applyDebugFSCapabilities(caps)

	return caps
}

// applyDebugFSCapabilities folds amdgpu_pm_info into caps. The file has
// three possible states and each maps differently:
//   - absent: contributes nothing;
//   - readable: contributes a capability per recognized field line, even
//     when the current value fails to parse;
//   - present but unreadable (e.g. root-only debugfs): support is unknown,
//     and unknown support must not hide metrics, so every field counts.
func (r *Reader) applyDebugFSCapabilities(caps *gpu.Capabilities) {
	if r.debugCardRoot == nil {
		return
	}

	data, err := r.debugCardRoot.ReadFile(debugPmInfoFilename)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return
		}

		caps.GPUBusyPct = true
		caps.SCLKMHz = true
		caps.MCLKMHz = true
		caps.TempC = true
		caps.PowerW = true

		return
	}

	for _, field := range debugFSFieldsPresent(data) {
		switch field {
		case debugFieldGPULoad:
			caps.GPUBusyPct = true
		case debugFieldSCLK:
			caps.SCLKMHz = true
		case debugFieldMCLK:
			caps.MCLKMHz = true
		case debugFieldTempC:
			caps.TempC = true
		case debugFieldPowerW:
			caps.PowerW = true
		}
	}
}

// debugFSFieldsPresent reports which metric fields amdgpu_pm_info exposes,
// independent of whether their current values parse.
func debugFSFieldsPresent(data []byte) []debugField {
	var (
		seen  = make(map[debugField]struct{})
		found []debugField
	)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		field := classifyDebugFSLine(strings.ToLower(line))
		if field == debugFieldNone {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		found = append(found, field)
	}

	return found
}
