package httpserver

import (
	"context"
	"encoding/json"
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
	"github.com/skobkin/amdgputop-web/internal/sampler"
	"github.com/skobkin/amdgputop-web/internal/version"
	"nhooyr.io/websocket"
)

func TestHealthzOK(t *testing.T) {
	t.Parallel()

	_, ts := newTestHTTPServer(t, config.Config{}, nil, nil)
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
	_, ts := newTestHTTPServer(t, cfg, gpus, nil)
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

	_, tsInit := newTestHTTPServer(t, cfg, gpus, manager)
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
	_, ts := newTestHTTPServer(t, cfg, nil, nil)
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
	_, ts := newTestHTTPServer(t, cfg, nil, nil)
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

	_, ts := newTestHTTPServer(t, cfg, gpus, nil)
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

	_, ts := newTestHTTPServer(t, cfg, gpus, manager)
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

	_, ts := newTestHTTPServer(t, cfg, gpus, manager)
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

func newTestHTTPServer(t *testing.T, cfg config.Config, gpus []gpu.Info, samplerManager *sampler.Manager) (*Server, *httptest.Server) {
	t.Helper()

	if cfg.ListenAddr == "" {
		cfg = defaultTestConfig()
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New(cfg, logger, gpus, samplerManager)
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
