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
			case strings.Contains(lower, "vram"):
				if value, ok := parseBytesValue(trimmed); ok {
					metrics.VRAMBytes += value
					metrics.HasMemory = true
				}
			case strings.Contains(lower, "gtt"):
				if value, ok := parseBytesValue(trimmed); ok {
					metrics.GTTBytes += value
					metrics.HasMemory = true
				}
			}
		case sectionEngine:
			if value, ok := parseEngineValue(trimmed); ok {
				metrics.EngineTotal += value
				metrics.HasEngine = true
			}
		}
	}

	return metrics
}

func parseBytesValue(line string) (uint64, bool) {
	fields := strings.Fields(line)
	for i := len(fields) - 1; i >= 0; i-- {
		token := strings.Trim(fields[i], "(),")
		if token == "" {
			continue
		}
		lower := strings.ToLower(token)
		multiplier := uint64(1)

		switch {
		case strings.HasSuffix(lower, "bytes"):
			token = token[:len(token)-5]
		case strings.HasSuffix(lower, "byte"):
			token = token[:len(token)-4]
		case strings.HasSuffix(lower, "kb"):
			token = token[:len(token)-2]
			multiplier = 1024
		case strings.HasSuffix(lower, "kib"):
			token = token[:len(token)-3]
			multiplier = 1024
		case strings.HasSuffix(lower, "mb"):
			token = token[:len(token)-2]
			multiplier = 1024 * 1024
		case strings.HasSuffix(lower, "mib"):
			token = token[:len(token)-3]
			multiplier = 1024 * 1024
		case strings.HasSuffix(lower, "gb"):
			token = token[:len(token)-2]
			multiplier = 1024 * 1024 * 1024
		case strings.HasSuffix(lower, "gib"):
			token = token[:len(token)-3]
			multiplier = 1024 * 1024 * 1024
		case strings.HasSuffix(lower, "b"):
			token = token[:len(token)-1]
		}

		token = strings.Trim(token, "()")
		if token == "" {
			continue
		}

		if value, err := strconv.ParseFloat(token, 64); err == nil {
			return uint64(value * float64(multiplier)), true
		}
		if value, err := strconv.ParseUint(token, 10, 64); err == nil {
			return value * multiplier, true
		}
	}
	return 0, false
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
