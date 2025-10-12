package procscan

import (
	"bufio"
	"bytes"
	"regexp"
	"strconv"
	"strings"
)

type fdMetrics struct {
	VRAMBytes   uint64
	GTTBytes    uint64
	HasMemory   bool
	EngineTotal uint64
	HasEngine   bool
	ClientID    int
}

func parseFDInfo(data []byte) fdMetrics {
	var metrics fdMetrics

	const (
		sectionNone = iota
		sectionMemory
		sectionEngine
	)
	section := sectionNone

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lower, "drm-memory"):
			section = sectionMemory
			trimmed = strings.TrimSpace(trimmed[len("drm-memory:"):])
			if trimmed == "" {
				continue
			}
			lower = strings.ToLower(trimmed)
		case strings.HasPrefix(lower, "drm-engine"):
			section = sectionEngine
			trimmed = strings.TrimSpace(trimmed[len("drm-engine:"):])
			if trimmed == "" {
				continue
			}
			lower = strings.ToLower(trimmed)
		}

		switch section {
		case sectionMemory:
			switch {
			case strings.HasPrefix(lower, "vram"),
				strings.HasPrefix(lower, "drm-memory-vram"),
				strings.HasPrefix(lower, "amd-requested-vram"):
				if value, ok := parseBytesValue(trimmed); ok {
					if value > metrics.VRAMBytes {
						metrics.VRAMBytes = value
					}
					metrics.HasMemory = true
				}
			case strings.HasPrefix(lower, "gtt"),
				strings.HasPrefix(lower, "drm-memory-gtt"),
				strings.HasPrefix(lower, "amd-requested-gtt"):
				if value, ok := parseBytesValue(trimmed); ok {
					if value > metrics.GTTBytes {
						metrics.GTTBytes = value
					}
					metrics.HasMemory = true
				}
			}
		case sectionEngine:
			if value, ok := parseEngineValue(trimmed); ok {
				metrics.EngineTotal += value
				metrics.HasEngine = true
			}
		default:
			if strings.HasPrefix(lower, "drm-client-id") {
				if value, ok := parseIntValue(trimmed); ok {
					metrics.ClientID = value
				}
			}
		}
	}

	return metrics
}

func parseBytesValue(line string) (uint64, bool) {
	matches := bytesValuePattern.FindAllStringSubmatch(strings.ToLower(line), -1)
	if len(matches) == 0 {
		return 0, false
	}

	match := matches[len(matches)-1]
	valueStr := match[1]
	unit := match[2]

	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return 0, false
	}

	multiplier := bytesUnitMultiplier(unit)
	return uint64(value * float64(multiplier)), true
}

func parseEngineValue(line string) (uint64, bool) {
	matches := engineValuePattern.FindAllStringSubmatch(strings.ToLower(line), -1)
	if len(matches) == 0 {
		return 0, false
	}

	best := matches[len(matches)-1]
	valueStr := best[1]
	unit := best[2]

	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return 0, false
	}
	return uint64(value * float64(engineUnitMultiplier(unit))), true
}

var bytesValuePattern = regexp.MustCompile(`([-+]?\d+(?:\.\d+)?)\s*(bytes?|byte|kib|kb|mib|mb|gib|gb|b)?`)

var engineValuePattern = regexp.MustCompile(`([-+]?\d+(?:\.\d+)?)\s*(ns|us|ms|s)`)

func engineUnitMultiplier(unit string) uint64 {
	switch unit {
	case "ns":
		return 1
	case "us":
		return 1000
	case "ms":
		return 1000 * 1000
	case "s":
		return 1000 * 1000 * 1000
	default:
		return 1
	}
}

func bytesUnitMultiplier(unit string) uint64 {
	switch unit {
	case "", "b", "byte", "bytes":
		return 1
	case "kb", "kib":
		return 1024
	case "mb", "mib":
		return 1024 * 1024
	case "gb", "gib":
		return 1024 * 1024 * 1024
	default:
		return 1
	}
}

func parseIntValue(line string) (int, bool) {
	fields := strings.Fields(line)
	for i := len(fields) - 1; i >= 0; i-- {
		token := strings.Trim(fields[i], "(),")
		if token == "" {
			continue
		}
		value, err := strconv.Atoi(token)
		if err == nil {
			return value, true
		}
	}
	return 0, false
}
