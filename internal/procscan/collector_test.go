package procscan

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectorCollectsProcessMemoryAndEngine(t *testing.T) {
	root := t.TempDir()
	procDir := filepath.Join(root, "1234")
	mustMkdir(t, filepath.Join(procDir, "fd"))
	mustMkdir(t, filepath.Join(procDir, "fdinfo"))

	writeFile(t, filepath.Join(procDir, "comm"), "testproc\n")
	writeFile(t, filepath.Join(procDir, "cmdline"), "test\x00--flag\x00")
	writeFile(t, filepath.Join(procDir, "status"), "Name:\ttestproc\nUid:\t1000\t1000\t1000\t1000\n")

	fdinfoData := readTestdata(t, "fdinfo_mem_engine.txt")
	writeFile(t, filepath.Join(procDir, "fdinfo", "5"), string(fdinfoData))

	if err := os.Symlink("/dev/dri/renderD128", filepath.Join(procDir, "fd", "5")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	gpus := []string{"card0"}
	renderNodes := map[string]string{"card0": "/dev/dri/renderD128"}
	lookup := newGPULookup(gpus, renderNodes)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	coll := newCollector(root, 10, 16, lookup, logger)
	coll.userCache[1000] = "alice"

	result, err := coll.collect()
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	col, ok := result["card0"]
	if !ok {
		t.Fatalf("expected gpu card0 in result")
	}
	if len(col.processes) != 1 {
		t.Fatalf("expected 1 process, got %d", len(col.processes))
	}

	proc := col.processes[0]
	if proc.pid != 1234 {
		t.Fatalf("unexpected pid %d", proc.pid)
	}
	if proc.uid != 1000 {
		t.Fatalf("unexpected uid %d", proc.uid)
	}
	if proc.user != "alice" {
		t.Fatalf("unexpected user %q", proc.user)
	}
	if proc.name != "testproc" {
		t.Fatalf("unexpected name %q", proc.name)
	}
	if proc.command != "test --flag" {
		t.Fatalf("unexpected cmd %q", proc.command)
	}
	if proc.renderNode != "renderD128" {
		t.Fatalf("unexpected render node %q", proc.renderNode)
	}
	if !proc.hasMemory {
		t.Fatalf("expected memory flag")
	}
	if proc.vramBytes != 268435456 {
		t.Fatalf("unexpected vram %d", proc.vramBytes)
	}
	if proc.gttBytes != 104857600 {
		t.Fatalf("unexpected gtt %d", proc.gttBytes)
	}
	if !proc.hasEngine {
		t.Fatalf("expected engine flag")
	}
	if proc.engineTotal != 350000000 {
		t.Fatalf("unexpected engine total %d", proc.engineTotal)
	}
	if !col.hasMemory {
		t.Fatalf("collection should mark memory capability")
	}
	if !col.hasEngine {
		t.Fatalf("collection should mark engine capability")
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}
