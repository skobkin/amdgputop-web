package procscan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFDInfoMemoryAndEngine(t *testing.T) {
	data := readTestdata(t, "fdinfo_mem_engine.txt")
	metrics := parseFDInfo(data)

	if !metrics.HasMemory {
		t.Fatalf("expected memory metrics")
	}
	if metrics.VRAMBytes != 268435456 {
		t.Fatalf("unexpected VRAM bytes %d", metrics.VRAMBytes)
	}
	if metrics.GTTBytes != 104857600 {
		t.Fatalf("unexpected GTT bytes %d", metrics.GTTBytes)
	}
	if !metrics.HasEngine {
		t.Fatalf("expected engine metrics")
	}
	if metrics.EngineTotal != 350000000 {
		t.Fatalf("unexpected engine total %d", metrics.EngineTotal)
	}
}

func TestParseFDInfoMemoryOnly(t *testing.T) {
	data := readTestdata(t, "fdinfo_mem_only.txt")
	metrics := parseFDInfo(data)

	if !metrics.HasMemory {
		t.Fatalf("expected memory metrics")
	}
	if metrics.VRAMBytes != 0 {
		t.Fatalf("unexpected VRAM bytes %d", metrics.VRAMBytes)
	}
	if metrics.GTTBytes != 2097152 {
		t.Fatalf("unexpected GTT bytes %d", metrics.GTTBytes)
	}
	if metrics.HasEngine {
		t.Fatalf("engine metrics should be absent")
	}
	if metrics.EngineTotal != 0 {
		t.Fatalf("expected zero engine total")
	}
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	// #nosec G304 -- reading controlled testdata fixtures.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}
