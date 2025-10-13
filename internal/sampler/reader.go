package sampler

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	drmClassPath          = "class/drm"
	gpuBusyFilename       = "gpu_busy_percent"
	memBusyFilename       = "mem_busy_percent"
	ppDpmSclkFilename     = "pp_dpm_sclk"
	ppDpmMclkFilename     = "pp_dpm_mclk"
	debugPmInfoFilename   = "amdgpu_pm_info"
	hwmonTempFile         = "temp1_input"
	hwmonFanFile          = "fan1_input"
	hwmonPowerAverageFile = "power1_average"
	hwmonPowerInputFile   = "power1_input"
)

// Reader fetches telemetry metrics for a single GPU.
type Reader struct {
	cardID        string
	cardIndex     int
	logger        *slog.Logger
	deviceRoot    *os.Root
	debugCardRoot *os.Root
	hwmonRoot     *os.Root
}

// NewReader constructs a Reader for the provided card identifier (e.g. "card0").
func NewReader(cardID, sysfsRoot, debugfsRoot string, logger *slog.Logger) (*Reader, error) {
	if logger == nil {
		logger = slog.Default()
	}

	cardIndex, err := parseCardIndex(cardID)
	if err != nil {
		return nil, err
	}

	sysRoot, err := os.OpenRoot(sysfsRoot)
	if err != nil {
		return nil, fmt.Errorf("open sysfs root: %w", err)
	}
	defer sysRoot.Close()

	deviceRoot, err := sysRoot.OpenRoot(filepath.Join(drmClassPath, cardID, "device"))
	if err != nil {
		return nil, fmt.Errorf("open device root: %w", err)
	}

	var debugCardRoot *os.Root
	if debugfsRoot != "" {
		if dbgRoot, err := os.OpenRoot(debugfsRoot); err == nil {
			defer dbgRoot.Close()
			if sub, err := dbgRoot.OpenRoot(filepath.Join("dri", strconv.Itoa(cardIndex))); err == nil {
				debugCardRoot = sub
			}
		}
	}

	reader := &Reader{
		cardID:        cardID,
		cardIndex:     cardIndex,
		logger:        logger.With("card", cardID),
		deviceRoot:    deviceRoot,
		debugCardRoot: debugCardRoot,
		hwmonRoot:     detectHwmon(deviceRoot),
	}

	return reader, nil
}

// Sample collects metrics for the GPU. Non-fatal read errors result in nil fields.
func (r *Reader) Sample() Sample {
	now := time.Now().UTC()
	metrics := Metrics{}

	metrics.GPUBusyPct = r.readPercent(gpuBusyFilename)
	metrics.MemBusyPct = r.readPercent(memBusyFilename)

	metrics.SCLKMHz = r.readCurrentClock(ppDpmSclkFilename)
	metrics.MCLKMHz = r.readCurrentClock(ppDpmMclkFilename)

	metrics.VRAMUsedBytes = r.readUint("mem_info_vram_used")
	metrics.VRAMTotalBytes = r.readUint("mem_info_vram_total")
	metrics.GTTUsedBytes = r.readUint("mem_info_gtt_used")
	metrics.GTTTotalBytes = r.readUint("mem_info_gtt_total")

	if r.hwmonRoot != nil {
		metrics.TempC = r.readScaledFloat(r.hwmonRoot, hwmonTempFile, 1000)
		metrics.FanRPM = r.readFloat(r.hwmonRoot, hwmonFanFile)
		metrics.PowerW = r.readScaledFloat(r.hwmonRoot, hwmonPowerAverageFile, 1_000_000)
		if metrics.PowerW == nil {
			metrics.PowerW = r.readScaledFloat(r.hwmonRoot, hwmonPowerInputFile, 1_000_000)
		}
	}

	// Optional debugfs fallback for select metrics.
	if metrics.GPUBusyPct == nil || metrics.SCLKMHz == nil || metrics.MCLKMHz == nil || metrics.PowerW == nil || metrics.TempC == nil {
		info := r.readDebugFSInfo()
		if metrics.GPUBusyPct == nil && info.gpuLoad != nil {
			metrics.GPUBusyPct = info.gpuLoad
		}
		if metrics.SCLKMHz == nil && info.sclkMHz != nil {
			metrics.SCLKMHz = info.sclkMHz
		}
		if metrics.MCLKMHz == nil && info.mclkMHz != nil {
			metrics.MCLKMHz = info.mclkMHz
		}
		if metrics.PowerW == nil && info.powerW != nil {
			metrics.PowerW = info.powerW
		}
		if metrics.TempC == nil && info.tempC != nil {
			metrics.TempC = info.tempC
		}
	}

	return Sample{
		GPUId:     r.cardID,
		Timestamp: now,
		Metrics:   metrics,
	}
}

func (r *Reader) readPercent(name string) *float64 {
	value, err := r.readFloatValue(r.deviceRoot, name)
	if err != nil {
		return nil
	}
	if value < 0 {
		return nil
	}
	if value > 100 {
		// Some kernels report busy % scaled by 100.
		value = clamp(value/100, 0, 100)
	}
	return float64Ptr(value)
}

func (r *Reader) readCurrentClock(filename string) *float64 {
	raw, err := r.deviceRoot.ReadFile(filename)
	if err != nil {
		return nil
	}

	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "*") {
			continue
		}
		if clock, ok := extractClockMHz(line); ok {
			return float64Ptr(clock)
		}
	}

	return nil
}

func (r *Reader) readUint(path string) *uint64 {
	data, err := r.deviceRoot.ReadFile(path)
	if err != nil {
		return nil
	}
	valueStr := strings.TrimSpace(string(data))
	if valueStr == "" {
		return nil
	}
	value, err := strconv.ParseUint(valueStr, 10, 64)
	if err != nil {
		r.logger.Debug("failed to parse uint value", "path", path, "value", valueStr, "err", err)
		return nil
	}
	return uint64Ptr(value)
}

func (r *Reader) readScaledFloat(root *os.Root, name string, divisor float64) *float64 {
	value, err := r.readFloatValue(root, name)
	if err != nil {
		return nil
	}
	return float64Ptr(value / divisor)
}

func (r *Reader) readFloat(root *os.Root, name string) *float64 {
	value, err := r.readFloatValue(root, name)
	if err != nil {
		return nil
	}
	return float64Ptr(value)
}

func (r *Reader) readFloatValue(root *os.Root, name string) (float64, error) {
	if root == nil {
		return 0, fs.ErrNotExist
	}
	data, err := root.ReadFile(name)
	if err != nil {
		return 0, err
	}
	valueStr := strings.TrimSpace(string(data))
	if valueStr == "" {
		return 0, fmt.Errorf("empty value")
	}
	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return 0, fmt.Errorf("parse float: %w", err)
	}
	return value, nil
}

func (r *Reader) readDebugFSInfo() debugInfo {
	if r.debugCardRoot == nil {
		return debugInfo{}
	}
	data, err := r.debugCardRoot.ReadFile(debugPmInfoFilename)
	if err != nil {
		return debugInfo{}
	}

	info := debugInfo{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)

		switch {
		case strings.HasPrefix(lower, "gpu load"):
			if val, ok := extractFirstFloat(line); ok {
				info.gpuLoad = float64Ptr(val)
			}
		case strings.HasPrefix(lower, "sclk"):
			if val, ok := extractFirstFloat(line); ok {
				info.sclkMHz = float64Ptr(val)
			}
		case strings.HasPrefix(lower, "mclk"):
			if val, ok := extractFirstFloat(line); ok {
				info.mclkMHz = float64Ptr(val)
			}
		case strings.HasPrefix(lower, "gpu temperature"):
			if val, ok := extractFirstFloat(line); ok {
				info.tempC = float64Ptr(val)
			}
		case strings.HasPrefix(lower, "gpu power"):
			if val, ok := extractFirstFloat(line); ok {
				info.powerW = float64Ptr(val)
			}
		case strings.HasPrefix(lower, "power:"):
			if val, ok := extractFirstFloat(line); ok {
				info.powerW = float64Ptr(val)
			}
		case strings.HasPrefix(lower, "average gfxclk"):
			if val, ok := extractFirstFloat(line); ok {
				info.sclkMHz = float64Ptr(val)
			}
		case strings.HasPrefix(lower, "average memclk"):
			if val, ok := extractFirstFloat(line); ok {
				info.mclkMHz = float64Ptr(val)
			}
		case strings.Contains(lower, "gpu load"):
			if val, ok := extractFirstFloat(line); ok && info.gpuLoad == nil {
				info.gpuLoad = float64Ptr(val)
			}
		}
	}

	return info
}

type debugInfo struct {
	gpuLoad *float64
	sclkMHz *float64
	mclkMHz *float64
	tempC   *float64
	powerW  *float64
}

func detectHwmon(deviceRoot *os.Root) *os.Root {
	if deviceRoot == nil {
		return nil
	}

	hwmonRoot, err := deviceRoot.OpenRoot("hwmon")
	if err != nil {
		return nil
	}
	defer hwmonRoot.Close()

	entries, err := fs.ReadDir(hwmonRoot.FS(), ".")
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 {
			subRoot, err := hwmonRoot.OpenRoot(entry.Name())
			if err == nil {
				return subRoot
			}
		}
	}
	return nil
}

func parseCardIndex(cardID string) (int, error) {
	if !strings.HasPrefix(cardID, "card") {
		return 0, fmt.Errorf("invalid card id %q", cardID)
	}
	indexStr := cardID[len("card"):]
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		return 0, fmt.Errorf("parse card index: %w", err)
	}
	return index, nil
}

func extractClockMHz(line string) (float64, bool) {
	line = strings.TrimSpace(strings.TrimSuffix(line, "*"))
	fields := strings.Fields(line)
	for _, field := range fields {
		field = strings.TrimSuffix(field, "*")
		if strings.HasSuffix(strings.ToLower(field), "mhz") {
			valueStr := strings.TrimSuffix(strings.ToLower(field), "mhz")
			value, err := strconv.ParseFloat(valueStr, 64)
			if err != nil {
				continue
			}
			return value, true
		}
	}
	return 0, false
}

func extractFirstFloat(line string) (float64, bool) {
	var buf strings.Builder
	var seen bool
	for i, r := range line {
		if unicode.IsDigit(r) || r == '.' || (r == '-' && !seen) {
			buf.WriteRune(r)
			seen = true
			continue
		}
		if seen {
			// Allow decimal separators like '.' or continue for thousands separators.
			if r == ',' {
				continue
			}
			break
		}
		// Special case: skip spaces in prefix.
		_ = i
	}
	if !seen {
		return 0, false
	}
	value, err := strconv.ParseFloat(buf.String(), 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func clamp(value, min, max float64) float64 {
	return math.Max(min, math.Min(max, value))
}

func float64Ptr(value float64) *float64 {
	v := value
	return &v
}

func uint64Ptr(value uint64) *uint64 {
	v := value
	return &v
}
