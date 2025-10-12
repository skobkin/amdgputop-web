package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/skobkin/amdgputop-web/internal/config"
	"github.com/skobkin/amdgputop-web/internal/gpu"
	"github.com/skobkin/amdgputop-web/internal/procscan"
	"github.com/skobkin/amdgputop-web/internal/sampler"
	"github.com/skobkin/amdgputop-web/internal/version"
	"nhooyr.io/websocket"
)

func TestHealthzOK(t *testing.T) {
	t.Parallel()

	_, ts := newTestHTTPServer(t, config.Config{}, nil, nil, nil)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if strings.TrimSpace(string(body)) != `{"status":"ok"}` {
		t.Fatalf("unexpected body %q", string(body))
	}

	// Ensure legacy path also works.
	respAPI, err := http.Get(ts.URL + "/api/healthz")
	if err != nil {
		t.Fatalf("GET /api/healthz failed: %v", err)
	}
	respAPI.Body.Close()
	if respAPI.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for /api/healthz, got %d", respAPI.StatusCode)
	}

}

func TestReadyzStates(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	cfg := defaultTestConfig()
	gpus := []gpu.Info{{ID: "card0"}}

	// Sampler not configured -> degraded.
	_, ts := newTestHTTPServer(t, cfg, gpus, nil, nil)
	defer ts.Close()

	assertReadyz(t, ts.URL+"/readyz", http.StatusServiceUnavailable, "degraded", "sampler_not_configured")
	assertReadyz(t, ts.URL+"/api/readyz", http.StatusServiceUnavailable, "degraded", "sampler_not_configured")

	// Sampler configured but not ready -> initializing.
	sysfsRoot := t.TempDir()
	debugRoot := t.TempDir()
	devicePath := createDeviceTree(t, sysfsRoot, "card0")
	writeFile(t, filepath.Join(devicePath, "gpu_busy_percent"), "12\n")

	reader, err := sampler.NewReader("card0", sysfsRoot, debugRoot, logger)
	if err != nil {
		t.Fatalf("NewReader error: %v", err)
	}

	manager, err := sampler.NewManager(10*time.Millisecond, map[string]*sampler.Reader{"card0": reader}, logger)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	_, tsInit := newTestHTTPServer(t, cfg, gpus, manager, nil)
	defer tsInit.Close()

	assertReadyz(t, tsInit.URL+"/readyz", http.StatusServiceUnavailable, "initializing", "waiting_for_samples")

	// Now run the sampler and expect ready.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		_ = manager.Run(ctx)
	}()

	waitFor(t, 2*time.Second, manager.Ready)
	assertReadyz(t, tsInit.URL+"/readyz", http.StatusOK, "ok", "")

}

func TestVersionEndpoint(t *testing.T) {
	t.Parallel()

	version.Set(version.Info{Version: "v0.0.1", Commit: "abc123", BuildTime: "now"})

	cfg := defaultTestConfig()
	_, ts := newTestHTTPServer(t, cfg, nil, nil, nil)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/version")
	if err != nil {
		t.Fatalf("GET /api/version failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var info version.Info
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if info.Version != "v0.0.1" || info.Commit != "abc123" || info.BuildTime != "now" {
		t.Fatalf("unexpected version payload %+v", info)
	}

}

func TestStaticIndexServed(t *testing.T) {
	t.Parallel()

	cfg := defaultTestConfig()
	_, ts := newTestHTTPServer(t, cfg, nil, nil, nil)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "Frontend build is not yet available") {
		t.Fatalf("placeholder text missing from response body")
	}

}

func TestAPIGPUs(t *testing.T) {
	t.Parallel()

	cfg := defaultTestConfig()
	gpus := []gpu.Info{
		{ID: "card0", PCI: "0000:01:00.0", PCIID: "1002:73df", RenderNode: "/dev/dri/renderD128"},
	}

	_, ts := newTestHTTPServer(t, cfg, gpus, nil, nil)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/gpus")
	if err != nil {
		t.Fatalf("GET /api/gpus failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var payload []gpu.Info
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(payload) != 1 || payload[0].ID != "card0" {
		t.Fatalf("unexpected gpu payload %+v", payload)
	}

}

func TestAPIGPUMetrics(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	sysfsRoot := t.TempDir()
	debugRoot := t.TempDir()
	devicePath := createDeviceTree(t, sysfsRoot, "card0")
	writeFile(t, filepath.Join(devicePath, "gpu_busy_percent"), "9\n")

	reader, err := sampler.NewReader("card0", sysfsRoot, debugRoot, logger)
	if err != nil {
		t.Fatalf("NewReader error: %v", err)
	}

	manager, err := sampler.NewManager(5*time.Millisecond, map[string]*sampler.Reader{"card0": reader}, logger)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = manager.Run(ctx) }()

	waitFor(t, 2*time.Second, manager.Ready)

	cfg := defaultTestConfig()
	gpus := []gpu.Info{{ID: "card0"}}

	_, ts := newTestHTTPServer(t, cfg, gpus, manager, nil)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/gpus/card0/metrics")
	if err != nil {
		t.Fatalf("GET metrics failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var sample sampler.Sample
	if err := json.NewDecoder(resp.Body).Decode(&sample); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}

	if sample.GPUId != "card0" {
		t.Fatalf("unexpected gpu id %q", sample.GPUId)
	}
	if sample.Metrics.GPUBusyPct == nil {
		t.Fatalf("expected gpu_busy_pct in metrics")
	}

	resp2, err := http.Get(ts.URL + "/api/gpus/unknown/metrics")
	if err != nil {
		t.Fatalf("GET unknown metrics failed: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown gpu, got %d", resp2.StatusCode)
	}
}

func TestAPIGPUProcs(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	sysfsRoot := t.TempDir()
	debugRoot := t.TempDir()
	devicePath := createDeviceTree(t, sysfsRoot, "card0")
	writeFile(t, filepath.Join(devicePath, "gpu_busy_percent"), "9\n")

	reader, err := sampler.NewReader("card0", sysfsRoot, debugRoot, logger)
	if err != nil {
		t.Fatalf("NewReader error: %v", err)
	}

	samplerManager, err := sampler.NewManager(5*time.Millisecond, map[string]*sampler.Reader{"card0": reader}, logger)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	samplerCtx, samplerCancel := context.WithCancel(context.Background())
	t.Cleanup(samplerCancel)
	go func() { _ = samplerManager.Run(samplerCtx) }()

	waitFor(t, 2*time.Second, samplerManager.Ready)

	procRoot := t.TempDir()
	pidDir := filepath.Join(procRoot, "3100")
	if err := os.MkdirAll(filepath.Join(pidDir, "fdinfo"), 0o755); err != nil {
		t.Fatalf("mkdir fdinfo: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(pidDir, "fd"), 0o755); err != nil {
		t.Fatalf("mkdir fd: %v", err)
	}
	writeFile(t, filepath.Join(pidDir, "comm"), "proc\n")
	writeFile(t, filepath.Join(pidDir, "cmdline"), "proc\x00--gpu\x00")
	writeFile(t, filepath.Join(pidDir, "status"), "Name:\tproc\nUid:\t0\t0\t0\t0\n")
	fdinfoData, err := os.ReadFile(filepath.Join("..", "procscan", "testdata", "fdinfo_mem_engine.txt"))
	if err != nil {
		t.Fatalf("read fdinfo fixture: %v", err)
	}
	writeFile(t, filepath.Join(pidDir, "fdinfo", "5"), string(fdinfoData))
	if err := os.Symlink("/dev/dri/renderD128", filepath.Join(pidDir, "fd", "5")); err != nil {
		t.Fatalf("symlink fd: %v", err)
	}

	procCfg := config.ProcConfig{
		Enable:       true,
		ScanInterval: 25 * time.Millisecond,
		MaxPIDs:      16,
		MaxFDsPerPID: 16,
	}

	gpus := []gpu.Info{{ID: "card0", RenderNode: "/dev/dri/renderD128"}}

	procManager, err := procscan.NewManager(procCfg, procRoot, gpus, logger)
	if err != nil {
		t.Fatalf("NewProcManager error: %v", err)
	}

	procCtx, procCancel := context.WithCancel(context.Background())
	t.Cleanup(procCancel)
	go func() { _ = procManager.Run(procCtx) }()

	waitFor(t, 2*time.Second, procManager.Ready)

	cfg := defaultTestConfig()
	cfg.Proc = procCfg
	cfg.ProcRoot = procRoot

	_, ts := newTestHTTPServer(t, cfg, gpus, samplerManager, procManager)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/gpus/card0/procs")
	if err != nil {
		t.Fatalf("GET procs failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var payload procscan.Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode procs: %v", err)
	}

	if payload.GPUId != "card0" {
		t.Fatalf("unexpected gpu id %q", payload.GPUId)
	}
	if !payload.Capabilities.VRAMGTTFromFDInfo {
		t.Fatalf("expected memory capability")
	}
	if len(payload.Processes) == 0 {
		t.Fatalf("expected processes in snapshot")
	}

	// Requesting procs when manager is nil should yield 503.
	_, tsNoProc := newTestHTTPServer(t, cfg, gpus, samplerManager, nil)
	defer tsNoProc.Close()

	resp2, err := http.Get(tsNoProc.URL + "/api/gpus/card0/procs")
	if err != nil {
		t.Fatalf("GET procs without manager failed: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when proc manager unavailable, got %d", resp2.StatusCode)
	}
}

func TestWebSocketHelloAndStats(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	sysfsRoot := t.TempDir()
	debugRoot := t.TempDir()
	devicePath := createDeviceTree(t, sysfsRoot, "card0")
	busyPath := filepath.Join(devicePath, "gpu_busy_percent")
	writeFile(t, busyPath, "5\n")

	reader, err := sampler.NewReader("card0", sysfsRoot, debugRoot, logger)
	if err != nil {
		t.Fatalf("NewReader error: %v", err)
	}

	manager, err := sampler.NewManager(5*time.Millisecond, map[string]*sampler.Reader{"card0": reader}, logger)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = manager.Run(ctx) }()

	waitFor(t, 2*time.Second, manager.Ready)

	cfg := defaultTestConfig()
	cfg.SampleInterval = 5 * time.Millisecond
	gpus := []gpu.Info{{ID: "card0"}}

	_, ts := newTestHTTPServer(t, cfg, gpus, manager, nil)
	defer ts.Close()

	wsURL := toWebsocketURL(ts.URL + "/ws")
	cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer ccancel()

	conn, _, err := websocket.Dial(cctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	helloType, helloData, err := conn.Read(cctx)
	if err != nil {
		t.Fatalf("read hello: %v", err)
	}
	if helloType != websocket.MessageText {
		t.Fatalf("unexpected hello type %v", helloType)
	}

	var helloMsg map[string]interface{}
	if err := json.Unmarshal(helloData, &helloMsg); err != nil {
		t.Fatalf("decode hello: %v", err)
	}
	if helloMsg["type"] != "hello" {
		t.Fatalf("expected hello message, got %q", helloMsg["type"])
	}

	// Next message should be stats broadcast.
	statsType, statsData, err := conn.Read(cctx)
	if err != nil {
		t.Fatalf("read stats: %v", err)
	}
	if statsType != websocket.MessageText {
		t.Fatalf("unexpected stats type %v", statsType)
	}

	var statsMsg map[string]interface{}
	if err := json.Unmarshal(statsData, &statsMsg); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if statsMsg["type"] != "stats" {
		t.Fatalf("expected stats message, got %q", statsMsg["type"])
	}

	metrics, ok := statsMsg["metrics"].(map[string]interface{})
	if !ok {
		t.Fatalf("metrics payload missing or wrong type")
	}
	if _, ok := metrics["gpu_busy_pct"]; !ok {
		t.Fatalf("expected gpu_busy_pct value in stats")
	}
}

func TestWebSocketStatsAndProcs(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	sysfsRoot := t.TempDir()
	debugRoot := t.TempDir()
	devicePath := createDeviceTree(t, sysfsRoot, "card0")
	busyPath := filepath.Join(devicePath, "gpu_busy_percent")
	writeFile(t, busyPath, "7\n")

	reader, err := sampler.NewReader("card0", sysfsRoot, debugRoot, logger)
	if err != nil {
		t.Fatalf("NewReader error: %v", err)
	}

	samplerManager, err := sampler.NewManager(5*time.Millisecond, map[string]*sampler.Reader{"card0": reader}, logger)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	samplerCtx, samplerCancel := context.WithCancel(context.Background())
	t.Cleanup(samplerCancel)
	go func() { _ = samplerManager.Run(samplerCtx) }()

	waitFor(t, 2*time.Second, samplerManager.Ready)

	procRoot := t.TempDir()
	pidDir := filepath.Join(procRoot, "2200")
	if err := os.MkdirAll(filepath.Join(pidDir, "fdinfo"), 0o755); err != nil {
		t.Fatalf("mkdir proc fdinfo: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(pidDir, "fd"), 0o755); err != nil {
		t.Fatalf("mkdir proc fd: %v", err)
	}
	writeFile(t, filepath.Join(pidDir, "comm"), "proc\n")
	writeFile(t, filepath.Join(pidDir, "cmdline"), "proc\x00--gpu\x00")
	writeFile(t, filepath.Join(pidDir, "status"), "Name:\tproc\nUid:\t0\t0\t0\t0\n")
	fdinfoData, err := os.ReadFile(filepath.Join("..", "procscan", "testdata", "fdinfo_mem_engine.txt"))
	if err != nil {
		t.Fatalf("read fdinfo fixture: %v", err)
	}
	writeFile(t, filepath.Join(pidDir, "fdinfo", "5"), string(fdinfoData))
	if err := os.Symlink("/dev/dri/renderD128", filepath.Join(pidDir, "fd", "5")); err != nil {
		t.Fatalf("symlink fd: %v", err)
	}

	procCfg := config.ProcConfig{
		Enable:       true,
		ScanInterval: 25 * time.Millisecond,
		MaxPIDs:      16,
		MaxFDsPerPID: 16,
	}

	gpus := []gpu.Info{{ID: "card0", RenderNode: "/dev/dri/renderD128"}}

	procManager, err := procscan.NewManager(procCfg, procRoot, gpus, logger)
	if err != nil {
		t.Fatalf("NewProcManager error: %v", err)
	}

	procCtx, procCancel := context.WithCancel(context.Background())
	t.Cleanup(procCancel)
	go func() { _ = procManager.Run(procCtx) }()

	waitFor(t, 2*time.Second, procManager.Ready)

	cfg := defaultTestConfig()
	cfg.SampleInterval = 5 * time.Millisecond
	cfg.Proc = procCfg
	cfg.ProcRoot = procRoot

	_, ts := newTestHTTPServer(t, cfg, gpus, samplerManager, procManager)
	defer ts.Close()

	wsURL := toWebsocketURL(ts.URL + "/ws")
	cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer ccancel()

	conn, _, err := websocket.Dial(cctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	helloMsg, err := expectHelloMessage(cctx, conn)
	if err != nil {
		t.Fatalf("hello message error: %v", err)
	}
	features, ok := helloMsg["features"].(map[string]any)
	if !ok {
		t.Fatalf("hello features missing")
	}
	if procs, ok := features["procs"].(bool); !ok || !procs {
		t.Fatalf("expected procs feature true")
	}

	gotStats := false
	gotProcs := false
	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) && (!gotStats || !gotProcs) {
		timeout := time.Until(deadline)
		if timeout <= 0 {
			break
		}
		readCtx, cancel := context.WithTimeout(context.Background(), timeout)
		msgType, data, err := conn.Read(readCtx)
		cancel()
		if err != nil {
			t.Fatalf("read message: %v", err)
		}
		if msgType != websocket.MessageText {
			continue
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		switch envelope.Type {
		case "stats":
			gotStats = true
		case "procs":
			var msg struct {
				Type         string                `json:"type"`
				GPUId        string                `json:"gpu_id"`
				Capabilities procscan.Capabilities `json:"capabilities"`
				Processes    []procscan.Process    `json:"processes"`
			}
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("decode procs message: %v", err)
			}
			if msg.GPUId != "card0" {
				t.Fatalf("unexpected gpu id %q", msg.GPUId)
			}
			if !msg.Capabilities.VRAMGTTFromFDInfo {
				t.Fatalf("expected vram capability true")
			}
			if !msg.Capabilities.EngineTimeFromFDInfo {
				t.Fatalf("expected engine capability true")
			}
			if len(msg.Processes) == 0 {
				t.Fatalf("expected at least one process")
			}
			proc := msg.Processes[0]
			if proc.RenderNode != "renderD128" {
				t.Fatalf("unexpected render node %q", proc.RenderNode)
			}
			if proc.VRAMBytes == nil || *proc.VRAMBytes == 0 {
				t.Fatalf("expected vram metric")
			}
			gotProcs = true
		}
	}

	if !gotStats {
		t.Fatalf("did not receive stats message")
	}
	if !gotProcs {
		t.Fatalf("did not receive procs message")
	}
}

func newTestHTTPServer(t *testing.T, cfg config.Config, gpus []gpu.Info, samplerManager *sampler.Manager, procManager *procscan.Manager) (*Server, *httptest.Server) {
	t.Helper()

	if cfg.ListenAddr == "" {
		cfg = defaultTestConfig()
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New(cfg, logger, gpus, samplerManager, procManager)
	ts := httptest.NewServer(srv.httpServer.Handler)
	t.Cleanup(ts.Close)
	return srv, ts
}

func assertReadyz(t *testing.T, url string, expectedStatus int, expected string, reason string) {
	t.Helper()

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != expectedStatus {
		t.Fatalf("expected status %d for %s, got %d", expectedStatus, url, resp.StatusCode)
	}

	var payload readyResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode readyz response: %v", err)
	}

	if payload.Status != expected {
		t.Fatalf("expected status %q, got %q", expected, payload.Status)
	}
	if reason == "" {
		if payload.Reason != "" {
			t.Fatalf("expected empty reason, got %q", payload.Reason)
		}
	} else if payload.Reason != reason {
		t.Fatalf("expected reason %q, got %q", reason, payload.Reason)
	}
}

func createDeviceTree(t *testing.T, root, cardID string) string {
	t.Helper()
	devicePath := filepath.Join(root, "class", "drm", cardID, "device")
	if err := mkdirAll(devicePath); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return devicePath
}

func mkdirAll(path string) error {
	return os.MkdirAll(path, 0o755)
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func expectHelloMessage(ctx context.Context, conn *websocket.Conn) (map[string]any, error) {
	msgType, data, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	if msgType != websocket.MessageText {
		return nil, fmt.Errorf("expected text message, got %v", msgType)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	if payload["type"] != "hello" {
		return nil, fmt.Errorf("expected hello message, got %v", payload["type"])
	}
	return payload, nil
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
	t.Fatalf("condition not satisfied within %s", timeout)
}

func defaultTestConfig() config.Config {
	return config.Config{
		ListenAddr:     ":0",
		SampleInterval: 250 * time.Millisecond,
		AllowedOrigins: []string{"*"},
		DefaultGPU:     "auto",
		ProcRoot:       "/proc",
		WS: config.WebsocketConfig{
			MaxClients:   1024,
			WriteTimeout: 3 * time.Second,
			ReadTimeout:  30 * time.Second,
		},
		Proc: config.ProcConfig{
			Enable:       true,
			ScanInterval: 2 * time.Second,
			MaxPIDs:      5000,
			MaxFDsPerPID: 64,
		},
	}
}

func toWebsocketURL(httpURL string) string {
	u, err := url.Parse(httpURL)
	if err != nil {
		return httpURL
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	return u.String()
}
