package sampler

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerSubscribeAndReady(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sysfsRoot := t.TempDir()
	debugfsRoot := t.TempDir()

	cardID := "card0"
	devicePath := createMinimalDevice(t, sysfsRoot, cardID)
	gpuBusyPath := filepath.Join(devicePath, gpuBusyFilename)

	writeFile(t, gpuBusyPath, "10\n")

	reader, err := NewReader(cardID, sysfsRoot, debugfsRoot, logger)
	if err != nil {
		t.Fatalf("NewReader returned error: %v", err)
	}

	manager, err := NewManager(15*time.Millisecond, map[string]*Reader{cardID: reader}, logger)
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = manager.Run(ctx)
	}()

	waitFor(t, 500*time.Millisecond, manager.Ready)

	ch, unsubscribe, err := manager.Subscribe(cardID)
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}
	defer unsubscribe()

	first := awaitSample(t, ch)
	assertFloatEqual(t, first.Metrics.GPUBusyPct, 10)

	writeFile(t, gpuBusyPath, "25\n")
	next := awaitSample(t, ch)
	assertFloatEqual(t, next.Metrics.GPUBusyPct, 25)

	if latest, ok := manager.Latest(cardID); !ok || latest.Metrics.GPUBusyPct == nil || *latest.Metrics.GPUBusyPct != 25 {
		t.Fatalf("Latest did not return expected sample: %+v", latest)
	}

	ids := manager.GPUIDs()
	if len(ids) != 1 || ids[0] != cardID {
		t.Fatalf("GPUIDs returned %v", ids)
	}

	if _, _, err := manager.Subscribe("unknown"); err == nil {
		t.Fatalf("Subscribe should fail for unknown gpu id")
	}

	// Validate backpressure behaviour by leaving one sample unconsumed.
	writeFile(t, gpuBusyPath, "55\n")
	time.Sleep(40 * time.Millisecond)
	writeFile(t, gpuBusyPath, "75\n")
	time.Sleep(40 * time.Millisecond)
	final := awaitSample(t, ch)
	assertFloatEqual(t, final.Metrics.GPUBusyPct, 75)
}

func TestManagerDropsOldestOnBackpressure(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sysfsRoot := t.TempDir()
	debugfsRoot := t.TempDir()

	cardID := "card0"
	devicePath := createMinimalDevice(t, sysfsRoot, cardID)
	gpuBusyPath := filepath.Join(devicePath, gpuBusyFilename)
	writeFile(t, gpuBusyPath, "5\n")

	reader, err := NewReader(cardID, sysfsRoot, debugfsRoot, logger)
	if err != nil {
		t.Fatalf("NewReader returned error: %v", err)
	}

	manager, err := NewManager(10*time.Millisecond, map[string]*Reader{cardID: reader}, logger)
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = manager.Run(ctx)
	}()

	waitFor(t, 500*time.Millisecond, manager.Ready)

	ch, unsubscribe, err := manager.Subscribe(cardID)
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}
	defer unsubscribe()

	// Consume initial sample.
	_ = awaitSample(t, ch)

	writeFile(t, gpuBusyPath, "15\n")
	time.Sleep(25 * time.Millisecond)
	writeFile(t, gpuBusyPath, "35\n")
	time.Sleep(25 * time.Millisecond)

	latest := awaitSample(t, ch)
	assertFloatEqual(t, latest.Metrics.GPUBusyPct, 35)
}

func createMinimalDevice(t *testing.T, root, cardID string) string {
	t.Helper()
	devicePath := filepath.Join(root, "class", "drm", cardID, "device")
	if err := os.MkdirAll(devicePath, 0o750); err != nil {
		t.Fatalf("failed to create device directory: %v", err)
	}
	return devicePath
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("failed to create directories for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

func awaitSample(t *testing.T, ch <-chan Sample) Sample {
	t.Helper()
	select {
	case sample, ok := <-ch:
		if !ok {
			t.Fatal("subscription channel closed unexpectedly")
		}
		return sample
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for sample")
		return Sample{}
	}
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}
