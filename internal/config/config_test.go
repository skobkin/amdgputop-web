package config

import (
	"log/slog"
	"reflect"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.ListenAddr != ":8080" {
		t.Fatalf("unexpected ListenAddr %q", cfg.ListenAddr)
	}
	if cfg.SampleInterval != 2*time.Second {
		t.Fatalf("unexpected SampleInterval %s", cfg.SampleInterval)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("unexpected LogLevel %v", cfg.LogLevel)
	}
	if cfg.SysfsRoot != "/sys" {
		t.Fatalf("unexpected SysfsRoot %q", cfg.SysfsRoot)
	}
	if cfg.DebugfsRoot != "/sys/kernel/debug" {
		t.Fatalf("unexpected DebugfsRoot %q", cfg.DebugfsRoot)
	}
	if cfg.ProcRoot != "/proc" {
		t.Fatalf("unexpected ProcRoot %q", cfg.ProcRoot)
	}
	if !cfg.Proc.Enable {
		t.Fatalf("expected process scanner enabled by default")
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("APP_LISTEN_ADDR", "127.0.0.1:9000")
	t.Setenv("APP_SAMPLE_INTERVAL", "500ms")
	t.Setenv("APP_ALLOWED_ORIGINS", "https://example.com, https://other.test")
	t.Setenv("APP_DEFAULT_GPU", "card42")
	t.Setenv("APP_ENABLE_PROMETHEUS", "true")
	t.Setenv("APP_ENABLE_PPROF", "true")
	t.Setenv("APP_LOG_LEVEL", "debug")
	t.Setenv("APP_SYSFS_ROOT", "/tmp/sys")
	t.Setenv("APP_DEBUGFS_ROOT", "/tmp/debug")
	t.Setenv("APP_PROC_ROOT", "/tmp/proc")
	t.Setenv("APP_WS_MAX_CLIENTS", "2048")
	t.Setenv("APP_WS_WRITE_TIMEOUT", "10s")
	t.Setenv("APP_WS_READ_TIMEOUT", "45s")
	t.Setenv("APP_PROC_ENABLE", "false")
	t.Setenv("APP_PROC_SCAN_INTERVAL", "5s")
	t.Setenv("APP_PROC_MAX_PIDS", "128")
	t.Setenv("APP_PROC_MAX_FDS_PER_PID", "32")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.ListenAddr != "127.0.0.1:9000" {
		t.Fatalf("ListenAddr override failed, got %q", cfg.ListenAddr)
	}
	if cfg.SampleInterval != 500*time.Millisecond {
		t.Fatalf("SampleInterval override failed, got %s", cfg.SampleInterval)
	}
	wantOrigins := []string{"https://example.com", "https://other.test"}
	if !reflect.DeepEqual(cfg.AllowedOrigins, wantOrigins) {
		t.Fatalf("AllowedOrigins mismatch: %+v", cfg.AllowedOrigins)
	}
	if cfg.DefaultGPU != "card42" {
		t.Fatalf("DefaultGPU override failed, got %q", cfg.DefaultGPU)
	}
	if !cfg.EnablePrometheus {
		t.Fatalf("EnablePrometheus override failed")
	}
	if !cfg.EnablePprof {
		t.Fatalf("EnablePprof override failed")
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Fatalf("LogLevel override failed, got %v", cfg.LogLevel)
	}
	if cfg.SysfsRoot != "/tmp/sys" {
		t.Fatalf("SysfsRoot override failed, got %q", cfg.SysfsRoot)
	}
	if cfg.DebugfsRoot != "/tmp/debug" {
		t.Fatalf("DebugfsRoot override failed, got %q", cfg.DebugfsRoot)
	}
	if cfg.ProcRoot != "/tmp/proc" {
		t.Fatalf("ProcRoot override failed, got %q", cfg.ProcRoot)
	}
	if cfg.WS.MaxClients != 2048 {
		t.Fatalf("WS.MaxClients override failed, got %d", cfg.WS.MaxClients)
	}
	if cfg.WS.WriteTimeout != 10*time.Second {
		t.Fatalf("WS.WriteTimeout override failed, got %s", cfg.WS.WriteTimeout)
	}
	if cfg.WS.ReadTimeout != 45*time.Second {
		t.Fatalf("WS.ReadTimeout override failed, got %s", cfg.WS.ReadTimeout)
	}
	if cfg.Proc.Enable {
		t.Fatalf("Proc.Enable override failed, expected false")
	}
	if cfg.Proc.ScanInterval != 5*time.Second {
		t.Fatalf("Proc.ScanInterval override failed, got %s", cfg.Proc.ScanInterval)
	}
	if cfg.Proc.MaxPIDs != 128 {
		t.Fatalf("Proc.MaxPIDs override failed, got %d", cfg.Proc.MaxPIDs)
	}
	if cfg.Proc.MaxFDsPerPID != 32 {
		t.Fatalf("Proc.MaxFDsPerPID override failed, got %d", cfg.Proc.MaxFDsPerPID)
	}
}

func TestLoadInvalidEnv(t *testing.T) {
	testCases := []struct {
		name string
		key  string
		val  string
	}{
		{"NegativeSampleInterval", "APP_SAMPLE_INTERVAL", "-1s"},
		{"InvalidOrigins", "APP_ALLOWED_ORIGINS", ","},
		{"InvalidPrometheusBool", "APP_ENABLE_PROMETHEUS", "maybe"},
		{"InvalidLogLevel", "APP_LOG_LEVEL", "loud"},
		{"InvalidWSMaxClients", "APP_WS_MAX_CLIENTS", "zero"},
		{"NonPositiveWSMaxClients", "APP_WS_MAX_CLIENTS", "0"},
		{"InvalidWSWriteTimeout", "APP_WS_WRITE_TIMEOUT", "nope"},
		{"NegativeWSWriteTimeout", "APP_WS_WRITE_TIMEOUT", "-1s"},
		{"InvalidProcEnable", "APP_PROC_ENABLE", "maybe"},
		{"InvalidProcInterval", "APP_PROC_SCAN_INTERVAL", "fast"},
		{"NonPositiveProcInterval", "APP_PROC_SCAN_INTERVAL", "0"},
		{"InvalidProcMaxPIDs", "APP_PROC_MAX_PIDS", "many"},
		{"NonPositiveProcMaxPIDs", "APP_PROC_MAX_PIDS", "0"},
		{"InvalidProcMaxFDs", "APP_PROC_MAX_FDS_PER_PID", "lots"},
		{"NonPositiveProcMaxFDs", "APP_PROC_MAX_FDS_PER_PID", "-1"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.key, tc.val)
			if _, err := Load(); err == nil {
				t.Fatalf("expected error for %s=%q", tc.key, tc.val)
			}
		})
	}
}
