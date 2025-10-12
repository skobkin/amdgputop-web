package procscan

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/skobkin/amdgputop-web/internal/config"
	"github.com/skobkin/amdgputop-web/internal/gpu"
)

func TestManagerSnapshotsAndSubscriptions(t *testing.T) {
	root := t.TempDir()
	procDir := setupProcEntry(t, root, 1234)

	fdinfoPath := procDir.fdinfo("5")
	writeFile(t, fdinfoPath, string(readTestdata(t, "fdinfo_mem_engine.txt")))

	if err := procDir.linkFD("5", "/dev/dri/renderD128"); err != nil {
		t.Fatalf("symlink fd: %v", err)
	}

	cfg := config.ProcConfig{
		Enable:       true,
		ScanInterval: 2 * time.Second,
		MaxPIDs:      10,
		MaxFDsPerPID: 16,
	}

	gpus := []gpu.Info{{ID: "card0", RenderNode: "/dev/dri/renderD128"}}

	manager, err := NewManager(cfg, root, gpus, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	manager.collector.userCache[1000] = "alice"

	first := time.Unix(0, 0)
	manager.performScan(first)

	snap, ok := manager.Latest("card0")
	if !ok {
		t.Fatalf("expected snapshot for card0")
	}
	if len(snap.Processes) != 1 {
		t.Fatalf("expected single process, got %d", len(snap.Processes))
	}
	p := snap.Processes[0]
	if p.GPUTimeMSPerS != nil {
		t.Fatalf("expected nil gpu time on first sample")
	}
	if p.VRAMBytes == nil || *p.VRAMBytes != 268435456 {
		t.Fatalf("unexpected VRAM %+v", p.VRAMBytes)
	}
	if p.GTTBytes == nil || *p.GTTBytes != 104857600 {
		t.Fatalf("unexpected GTT %+v", p.GTTBytes)
	}
	if !snap.Capabilities.VRAMGTTFromFDInfo {
		t.Fatalf("expected VRAM capability flag")
	}
	if !snap.Capabilities.EngineTimeFromFDInfo {
		t.Fatalf("expected engine capability flag")
	}

	ch, cancel, err := manager.Subscribe("card0")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(cancel)

	select {
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for initial snapshot")
	case s := <-ch:
		if len(s.Processes) != 1 {
			t.Fatalf("expected initial snapshot with process data")
		}
	}

	writeFile(t, fdinfoPath, `pos:	0
flags:	02
mnt_id:	28
ino:	123456
drm-memory:
	gtt: 32 bo (104857600 bytes)
	vram: 16 bo (268435456 bytes)
drm-engine:
	gfx: 400000000 ns
	media: 550000000 ns
`)

	second := first.Add(2 * time.Second)
	manager.performScan(second)

	select {
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for update snapshot")
	case s := <-ch:
		if len(s.Processes) != 1 {
			t.Fatalf("expected updated process list")
		}
		latest := s.Processes[0]
		if latest.GPUTimeMSPerS == nil {
			t.Fatalf("expected gpu time delta")
		}
		want := 300.0
		if diff := *latest.GPUTimeMSPerS - want; diff < -0.001 || diff > 0.001 {
			t.Fatalf("unexpected gpu time %.3f", *latest.GPUTimeMSPerS)
		}
	}
}

type procFixture struct {
	root string
	pid  int
}

func setupProcEntry(t *testing.T, root string, pid int) procFixture {
	t.Helper()
	procDir := procFixture{
		root: filepath.Join(root, strconv.Itoa(pid)),
		pid:  pid,
	}
	mustMkdir(t, procDir.root)
	mustMkdir(t, filepath.Join(procDir.root, "fd"))
	mustMkdir(t, filepath.Join(procDir.root, "fdinfo"))

	writeFile(t, filepath.Join(procDir.root, "comm"), "proc\n")
	writeFile(t, filepath.Join(procDir.root, "cmdline"), "proc\x00--gpu\x00")
	writeFile(t, filepath.Join(procDir.root, "status"), "Name:\tproc\nUid:\t1000\t1000\t1000\t1000\n")

	return procDir
}

func (p procFixture) fdinfo(fd string) string {
	return filepath.Join(p.root, "fdinfo", fd)
}

func (p procFixture) linkFD(fd, target string) error {
	return os.Symlink(target, filepath.Join(p.root, "fd", fd))
}
