package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config represents runtime configuration sourced from environment variables.
type Config struct {
	ListenAddr       string
	SampleInterval   time.Duration
	AllowedOrigins   []string
	DefaultGPU       string
	EnablePrometheus bool
	EnablePprof      bool
	LogLevel         slog.Level
	SysfsRoot        string
	DebugfsRoot      string
	ProcRoot         string
	WS               WebsocketConfig
	Proc             ProcConfig
}

// WebsocketConfig captures tunables for WebSocket handling.
type WebsocketConfig struct {
	MaxClients   int
	WriteTimeout time.Duration
	ReadTimeout  time.Duration
}

// ProcConfig contains settings for the process scanner feature.
type ProcConfig struct {
	Enable       bool
	ScanInterval time.Duration
	MaxPIDs      int
	MaxFDsPerPID int
}

// Load parses configuration from environment variables, applying defaults.
func Load() (Config, error) {
	cfg := Config{
		ListenAddr:       ":8080",
		SampleInterval:   2 * time.Second,
		AllowedOrigins:   []string{"*"},
		DefaultGPU:       "auto",
		EnablePrometheus: false,
		EnablePprof:      false,
		LogLevel:         slog.LevelInfo,
		SysfsRoot:        "/sys",
		DebugfsRoot:      "/sys/kernel/debug",
		ProcRoot:         "/proc",
		WS: WebsocketConfig{
			MaxClients:   1024,
			WriteTimeout: 3 * time.Second,
			ReadTimeout:  30 * time.Second,
		},
		Proc: ProcConfig{
			Enable:       true,
			ScanInterval: 2 * time.Second,
			MaxPIDs:      5000,
			MaxFDsPerPID: 64,
		},
	}

	if value := strings.TrimSpace(os.Getenv("APP_LISTEN_ADDR")); value != "" {
		cfg.ListenAddr = value
	}

	if value := strings.TrimSpace(os.Getenv("APP_SAMPLE_INTERVAL")); value != "" {
		duration, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse APP_SAMPLE_INTERVAL: %w", err)
		}
		if duration <= 0 {
			return Config{}, fmt.Errorf("APP_SAMPLE_INTERVAL must be > 0")
		}
		cfg.SampleInterval = duration
	}

	if value := strings.TrimSpace(os.Getenv("APP_ALLOWED_ORIGINS")); value != "" {
		origins := splitAndTrim(value, ",")
		if len(origins) == 0 {
			return Config{}, fmt.Errorf("APP_ALLOWED_ORIGINS must not be empty")
		}
		cfg.AllowedOrigins = origins
	}

	if value := strings.TrimSpace(os.Getenv("APP_DEFAULT_GPU")); value != "" {
		cfg.DefaultGPU = value
	}

	if value := strings.TrimSpace(os.Getenv("APP_ENABLE_PROMETHEUS")); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse APP_ENABLE_PROMETHEUS: %w", err)
		}
		cfg.EnablePrometheus = enabled
	}

	if value := strings.TrimSpace(os.Getenv("APP_ENABLE_PPROF")); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse APP_ENABLE_PPROF: %w", err)
		}
		cfg.EnablePprof = enabled
	}

	if value := strings.TrimSpace(os.Getenv("APP_LOG_LEVEL")); value != "" {
		level, err := parseLogLevel(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse APP_LOG_LEVEL: %w", err)
		}
		cfg.LogLevel = level
	}

	if value := strings.TrimSpace(os.Getenv("APP_SYSFS_ROOT")); value != "" {
		cfg.SysfsRoot = value
	}

	if value := strings.TrimSpace(os.Getenv("APP_DEBUGFS_ROOT")); value != "" {
		cfg.DebugfsRoot = value
	}

	if value := strings.TrimSpace(os.Getenv("APP_PROC_ROOT")); value != "" {
		cfg.ProcRoot = value
	}

	if value := strings.TrimSpace(os.Getenv("APP_WS_MAX_CLIENTS")); value != "" {
		maxClients, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse APP_WS_MAX_CLIENTS: %w", err)
		}
		if maxClients <= 0 {
			return Config{}, fmt.Errorf("APP_WS_MAX_CLIENTS must be > 0")
		}
		cfg.WS.MaxClients = maxClients
	}

	if value := strings.TrimSpace(os.Getenv("APP_WS_WRITE_TIMEOUT")); value != "" {
		timeout, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse APP_WS_WRITE_TIMEOUT: %w", err)
		}
		if timeout <= 0 {
			return Config{}, fmt.Errorf("APP_WS_WRITE_TIMEOUT must be > 0")
		}
		cfg.WS.WriteTimeout = timeout
	}

	if value := strings.TrimSpace(os.Getenv("APP_WS_READ_TIMEOUT")); value != "" {
		timeout, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse APP_WS_READ_TIMEOUT: %w", err)
		}
		if timeout <= 0 {
			return Config{}, fmt.Errorf("APP_WS_READ_TIMEOUT must be > 0")
		}
		cfg.WS.ReadTimeout = timeout
	}

	if value := strings.TrimSpace(os.Getenv("APP_PROC_ENABLE")); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse APP_PROC_ENABLE: %w", err)
		}
		cfg.Proc.Enable = enabled
	}

	if value := strings.TrimSpace(os.Getenv("APP_PROC_SCAN_INTERVAL")); value != "" {
		dur, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse APP_PROC_SCAN_INTERVAL: %w", err)
		}
		if dur <= 0 {
			return Config{}, fmt.Errorf("APP_PROC_SCAN_INTERVAL must be > 0")
		}
		cfg.Proc.ScanInterval = dur
	}

	if value := strings.TrimSpace(os.Getenv("APP_PROC_MAX_PIDS")); value != "" {
		maxPIDs, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse APP_PROC_MAX_PIDS: %w", err)
		}
		if maxPIDs <= 0 {
			return Config{}, fmt.Errorf("APP_PROC_MAX_PIDS must be > 0")
		}
		cfg.Proc.MaxPIDs = maxPIDs
	}

	if value := strings.TrimSpace(os.Getenv("APP_PROC_MAX_FDS_PER_PID")); value != "" {
		maxFDs, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse APP_PROC_MAX_FDS_PER_PID: %w", err)
		}
		if maxFDs <= 0 {
			return Config{}, fmt.Errorf("APP_PROC_MAX_FDS_PER_PID must be > 0")
		}
		cfg.Proc.MaxFDsPerPID = maxFDs
	}

	return cfg, nil
}

func splitAndTrim(value, sep string) []string {
	raw := strings.Split(value, sep)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func parseLogLevel(input string) (slog.Level, error) {
	switch strings.ToUpper(strings.TrimSpace(input)) {
	case "DEBUG":
		return slog.LevelDebug, nil
	case "INFO":
		return slog.LevelInfo, nil
	case "WARN", "WARNING":
		return slog.LevelWarn, nil
	case "ERROR":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unsupported log level %q", input)
	}
}
