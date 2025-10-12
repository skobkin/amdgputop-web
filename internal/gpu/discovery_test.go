package gpu

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestDiscover(t *testing.T) {
	t.Parallel()

	root := filepath.Join("testdata", "sysfs")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	infos, err := Discover(root, logger)
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}

	if len(infos) != 2 {
		t.Fatalf("expected 2 GPUs, got %d", len(infos))
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].ID < infos[j].ID
	})

	card0 := infos[0]
	if card0.ID != "card0" {
		t.Fatalf("expected first GPU id 'card0', got %q", card0.ID)
	}
	if card0.PCI != "0000:0a:00.0" {
		t.Errorf("unexpected PCI slot: %q", card0.PCI)
	}
	if card0.PCIID != "1002:73df" {
		t.Errorf("unexpected PCI ID: %q", card0.PCIID)
	}
	if card0.Name != "AMD Radeon RX 6800" {
		t.Errorf("unexpected name: %q", card0.Name)
	}
	if card0.RenderNode != "/dev/dri/renderD128" {
		t.Errorf("unexpected render node: %q", card0.RenderNode)
	}

	card1 := infos[1]
	if card1.ID != "card1" {
		t.Fatalf("expected second GPU id 'card1', got %q", card1.ID)
	}
	if card1.PCIID != "1002:731f" {
		t.Errorf("expected PCI ID fallback to vendor/device, got %q", card1.PCIID)
	}
	if card1.Name != "AMD Radeon Pro Test" {
		t.Errorf("unexpected name for card1: %q", card1.Name)
	}
	if card1.RenderNode != "/dev/dri/renderD129" {
		t.Errorf("unexpected render node for card1: %q", card1.RenderNode)
	}
}

func TestDiscoverMissingDRMClass(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	infos, err := Discover(root, logger)
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}

	if len(infos) != 0 {
		t.Fatalf("expected 0 GPUs, got %d", len(infos))
	}
}

func TestDiscoverFollowsSymlinks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	classPath := filepath.Join(root, "class", "drm")
	if err := os.MkdirAll(classPath, 0o755); err != nil {
		t.Fatalf("mkdir class: %v", err)
	}

	target := filepath.Join(root, "devices", "pci0000:00", "0000:00:01.0", "drm", "card0")
	deviceDir := filepath.Join(target, "device")
	if err := os.MkdirAll(filepath.Join(deviceDir, "drm"), 0o755); err != nil {
		t.Fatalf("mkdir device: %v", err)
	}

	writeFile(t, filepath.Join(deviceDir, "uevent"), "PCI_SLOT_NAME=0000:00:01.0\nPCI_ID=1002:73df\n")
	writeFile(t, filepath.Join(deviceDir, "vendor"), "0x1002\n")
	writeFile(t, filepath.Join(deviceDir, "device"), "0x73df\n")
	if err := os.MkdirAll(filepath.Join(deviceDir, "drm", "renderD128"), 0o755); err != nil {
		t.Fatalf("mkdir render node: %v", err)
	}

	linkPath := filepath.Join(classPath, "card0")
	if err := os.Symlink(target, linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	infos, err := Discover(root, logger)
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	if len(infos) != 1 || infos[0].ID != "card0" {
		t.Fatalf("expected symlinked gpu, got %+v", infos)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
