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

func TestCollectorAggregatesClientIDs(t *testing.T) {
	root := t.TempDir()
	procDir := filepath.Join(root, "4321")
	mustMkdir(t, filepath.Join(procDir, "fd"))
	mustMkdir(t, filepath.Join(procDir, "fdinfo"))

	writeFile(t, filepath.Join(procDir, "comm"), "delta\n")
	writeFile(t, filepath.Join(procDir, "cmdline"), "delta\x00")
	writeFile(t, filepath.Join(procDir, "status"), "Name:\tdelta\nUid:\t1000\t1000\t1000\t1000\n")

	fdinfoA := `drm-client-id: 7
drm-memory:
	vram: 128 MiB
	gtt: 16 MiB
`
	fdinfoB := `drm-client-id: 7
drm-memory:
	vram: 200 MiB
	gtt: 20 MiB
`

	writeFile(t, filepath.Join(procDir, "fdinfo", "5"), fdinfoA)
	writeFile(t, filepath.Join(procDir, "fdinfo", "6"), fdinfoB)

	target := "/dev/dri/renderD128"
	if err := os.Symlink(target, filepath.Join(procDir, "fd", "5")); err != nil {
		t.Fatalf("symlink fd5: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(procDir, "fd", "6")); err != nil {
		t.Fatalf("symlink fd6: %v", err)
	}

	gpus := []string{"card0"}
	renderNodes := map[string]string{"card0": target}
	lookup := newGPULookup(gpus, renderNodes)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	coll := newCollector(root, 10, 16, lookup, logger)
	coll.userCache[1000] = "bob"

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

	const miB = 1024 * 1024
	proc := col.processes[0]
	if proc.vramBytes != 200*miB {
		t.Fatalf("expected aggregated VRAM 200MiB, got %d", proc.vramBytes)
	}
	if proc.gttBytes != 20*miB {
		t.Fatalf("expected aggregated GTT 20MiB, got %d", proc.gttBytes)
	}
	if !col.hasMemory {
		t.Fatalf("expected memory capability flag")
	}
}

func TestCollectorParsesAmdRequestedMemory(t *testing.T) {
	root := t.TempDir()
	procDir := filepath.Join(root, "5678")
	mustMkdir(t, filepath.Join(procDir, "fd"))
	mustMkdir(t, filepath.Join(procDir, "fdinfo"))

	writeFile(t, filepath.Join(procDir, "comm"), "amdproc\n")
	writeFile(t, filepath.Join(procDir, "cmdline"), "amdproc\x00")
	writeFile(t, filepath.Join(procDir, "status"), "Name:\tamdproc\nUid:\t1000\t1000\t1000\t1000\n")

	fdinfo := `drm-client-id: 12
drm-memory:
	amd-requested-vram: 256 MiB
	amd-requested-gtt: 8 MiB
`

	writeFile(t, filepath.Join(procDir, "fdinfo", "3"), fdinfo)

	target := "/dev/dri/renderD129"
	if err := os.Symlink(target, filepath.Join(procDir, "fd", "3")); err != nil {
		t.Fatalf("symlink fd3: %v", err)
	}

	gpus := []string{"card1"}
	renderNodes := map[string]string{"card1": target}
	lookup := newGPULookup(gpus, renderNodes)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	coll := newCollector(root, 10, 16, lookup, logger)
	coll.userCache[1000] = "carol"

	result, err := coll.collect()
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	col, ok := result["card1"]
	if !ok {
		t.Fatalf("expected gpu card1 in result")
	}
	if len(col.processes) != 1 {
		t.Fatalf("expected single process, got %d", len(col.processes))
	}

	const miB = 1024 * 1024
	proc := col.processes[0]
	if proc.vramBytes != 256*miB {
		t.Fatalf("expected VRAM 256MiB, got %d", proc.vramBytes)
	}
	if proc.gttBytes != 8*miB {
		t.Fatalf("expected GTT 8MiB, got %d", proc.gttBytes)
	}
	if !proc.hasMemory {
		t.Fatalf("expected memory flag true")
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
