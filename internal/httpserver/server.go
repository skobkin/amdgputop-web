package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/skobkin/amdgputop-web/internal/api"
	"github.com/skobkin/amdgputop-web/internal/config"
	"github.com/skobkin/amdgputop-web/internal/gpu"
	"github.com/skobkin/amdgputop-web/internal/procscan"
	"github.com/skobkin/amdgputop-web/internal/sampler"
	"github.com/skobkin/amdgputop-web/internal/version"
)

const (
	readHeaderTimeout = 5 * time.Second
	wsSendQueueSize   = 16
)

// Server wraps the HTTP surface area of the application.
type Server struct {
	cfg        config.Config
	logger     *slog.Logger
	httpServer *http.Server
	gpus       []gpu.Info
	gpuIndex   map[string]gpu.Info
	sampler    *sampler.Manager
	proc       *procscan.Manager

	maxWSClients int64
	wsActive     atomic.Int64
	wsTotal      atomic.Uint64
	wsRejected   atomic.Uint64
	wsSent       atomic.Uint64
	wsDropped    atomic.Uint64
	wsConnIDs    atomic.Uint64
	requestIDs   atomic.Uint64
}

// New assembles a Server with its handlers.
func New(cfg config.Config, logger *slog.Logger, gpus []gpu.Info, samplerManager *sampler.Manager, procManager *procscan.Manager) *Server {
	s := &Server{
		cfg:      cfg,
		logger:   logger,
		gpus:     gpus,
		gpuIndex: make(map[string]gpu.Info, len(gpus)),
		sampler:  samplerManager,
		proc:     procManager,
	}

	if cfg.WS.MaxClients > 0 {
		s.maxWSClients = int64(cfg.WS.MaxClients)
	}

	for _, info := range gpus {
		s.gpuIndex[info.ID] = info
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/api/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)
	mux.HandleFunc("/api/readyz", s.handleReadyz)
	mux.HandleFunc("/version", s.handleVersion)
	mux.HandleFunc("/api/version", s.handleVersion)
	mux.HandleFunc("/api", s.handleAPIDocs)
	mux.HandleFunc("/api/", s.handleAPIDocs)
	mux.HandleFunc("/api/gpus", s.handleAPIGPUs)
	mux.HandleFunc("/api/gpus/", s.handleAPIGPUSubresource)
	mux.HandleFunc("/ws", s.handleWS)
	mux.Handle("/", s.staticHandler())

	if cfg.EnablePrometheus {
		s.registerPrometheus(mux)
	}
	if cfg.EnablePprof {
		registerPprof(mux)
	}

	handler := s.withRequestLogging(mux)

	s.httpServer = &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	return s
}

// Start begins serving HTTP until shutdown is requested.
func (s *Server) Start() error {
	s.logger.Info("listening", "addr", s.httpServer.Addr)
	err := s.httpServer.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	s.logger.Info("listener stopped")
	return nil
}

// Shutdown attempts a graceful shutdown within the supplied context.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	info := s.readiness()
	logger := s.loggerFromContext(r.Context())

	statusCode := http.StatusOK
	if info.Status != "ok" {
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(info); err != nil {
		logger.Error("failed to encode readyz response", "err", err)
	}
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	info := version.Current()
	logger := s.loggerFromContext(r.Context())

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(info); err != nil {
		logger.Error("failed to encode version response", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleAPIDocs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.URL.Path != "/api" && r.URL.Path != "/api/" {
		http.NotFound(w, r)
		return
	}

	logger := s.loggerFromContext(r.Context())
	data, err := embeddedAssets.ReadFile("assets/api.html")
	if err != nil {
		logger.Error("failed to read api docs asset", "err", err)
		http.Error(w, "missing api docs", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write(data); err != nil {
		logger.Warn("failed to write api docs response", "err", err)
	}
}

func (s *Server) handleAPIGPUs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logger := s.loggerFromContext(r.Context())
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(s.gpus); err != nil {
		logger.Error("failed to encode gpu list", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleAPIGPUSubresource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	const prefix = "/api/gpus/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.NotFound(w, r)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	segments := strings.Split(rest, "/")
	if len(segments) != 2 || segments[0] == "" {
		http.NotFound(w, r)
		return
	}

	gpuID := segments[0]
	if _, ok := s.gpuIndex[gpuID]; !ok {
		http.NotFound(w, r)
		return
	}

	switch segments[1] {
	case "metrics":
		s.serveGPUMetrics(w, r, gpuID)
	case "procs":
		s.serveGPUProcs(w, r, gpuID)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) serveGPUMetrics(w http.ResponseWriter, r *http.Request, gpuID string) {
	if s.sampler == nil {
		http.Error(w, "metrics sampler unavailable", http.StatusServiceUnavailable)
		return
	}

	sample, ok := s.sampler.Latest(gpuID)
	if !ok {
		http.Error(w, "no sample available", http.StatusServiceUnavailable)
		return
	}

	logger := s.loggerFromContext(r.Context())
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(sample); err != nil {
		logger.Error("failed to encode gpu metrics", "gpu_id", gpuID, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
}

func (s *Server) serveGPUProcs(w http.ResponseWriter, r *http.Request, gpuID string) {
	if s.proc == nil {
		http.Error(w, "process scanner unavailable", http.StatusServiceUnavailable)
		return
	}

	snapshot, ok := s.proc.Latest(gpuID)
	if !ok {
		http.Error(w, "no process data available", http.StatusServiceUnavailable)
		return
	}

	logger := s.loggerFromContext(r.Context())
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(snapshot); err != nil {
		logger.Error("failed to encode gpu process data", "gpu_id", gpuID, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	reqLogger := s.loggerFromContext(r.Context())
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.reserveWS() {
		reqLogger.Warn("websocket rejected", "reason", "capacity")
		http.Error(w, "websocket capacity reached", http.StatusServiceUnavailable)
		return
	}
	defer s.releaseWS()

	opts := &websocket.AcceptOptions{
		OriginPatterns: originPatterns(s.cfg.AllowedOrigins),
	}

	conn, err := websocket.Accept(w, r, opts)
	if err != nil {
		reqLogger.Warn("websocket accept failed", "err", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	connID := s.wsConnIDs.Add(1)
	s.wsTotal.Add(1)
	logger := reqLogger.With("ws_id", connID)

	outbound := newWSOutbound(wsSendQueueSize, &s.wsDropped)

	features := map[string]bool{
		"procs":  s.proc != nil,
		"charts": s.cfg.Charts.Enable,
	}
	chartsMaxPoints := 0
	if s.cfg.Charts.Enable {
		chartsMaxPoints = s.cfg.Charts.MaxPoints
	}
	hello := api.NewHelloMessage(
		int(s.cfg.SampleInterval/time.Millisecond),
		s.gpus,
		features,
		chartsMaxPoints,
	)

	ctx, cancel := context.WithCancel(r.Context())

	writerDone := make(chan struct{})
	go s.wsWriter(ctx, conn, outbound, cancel, logger, writerDone)

	var (
		subCh           <-chan sampler.Sample
		unsubscribe     func()
		procCh          <-chan procscan.Snapshot
		procUnsubscribe func()
		currentGPU      string
	)

	defer func() {
		if unsubscribe != nil {
			unsubscribe()
		}
		if procUnsubscribe != nil {
			procUnsubscribe()
		}
		outbound.close()
		cancel()
		<-writerDone
	}()

	if !s.enqueueMessage(outbound, hello, logger) {
		return
	}

	messageCh := make(chan []byte, 8)
	readErrCh := make(chan error, 1)
	go s.readMessages(ctx, conn, messageCh, readErrCh)

	defaultGPU := s.defaultGPU()

	switchSubscription := func(target string) error {
		if target == "" {
			return fmt.Errorf("empty gpu id")
		}
		if _, ok := s.gpuIndex[target]; !ok {
			return fmt.Errorf("unknown gpu %q", target)
		}
		if s.sampler == nil {
			return fmt.Errorf("sampler unavailable")
		}
		if target == currentGPU {
			return nil
		}
		if unsubscribe != nil {
			unsubscribe()
			unsubscribe = nil
			subCh = nil
		}
		if procUnsubscribe != nil {
			procUnsubscribe()
			procUnsubscribe = nil
			procCh = nil
		}
		ch, cancel, err := s.sampler.Subscribe(target)
		if err != nil {
			return err
		}
		subCh = ch
		unsubscribe = cancel
		if s.proc != nil {
			procStream, procCancel, err := s.proc.Subscribe(target)
			if err != nil {
				logger.Warn("failed to subscribe proc scanner", "gpu_id", target, "err", err)
			} else {
				procCh = procStream
				procUnsubscribe = procCancel
			}
		}
		currentGPU = target
		logger.Info("ws subscribed", "gpu_id", target)
		return nil
	}

	if defaultGPU != "" {
		if err := switchSubscription(defaultGPU); err != nil {
			logger.Warn("failed to subscribe default gpu", "gpu_id", defaultGPU, "err", err)
			_ = s.enqueueError(outbound, fmt.Sprintf("failed to subscribe default gpu: %v", err), logger)
		}
	} else if len(s.gpus) == 0 {
		_ = s.enqueueError(outbound, "no GPUs detected", logger)
	}

	for {
		select {
		case sample, ok := <-subCh:
			if !ok {
				subCh = nil
				currentGPU = ""
				continue
			}
			if !s.enqueueMessage(outbound, api.NewStatsMessage(sample), logger) {
				return
			}
		case snapshot, ok := <-procCh:
			if !ok {
				procCh = nil
				continue
			}
			if !s.enqueueMessage(outbound, api.NewProcsMessage(snapshot), logger) {
				return
			}
		case data, ok := <-messageCh:
			if !ok {
				messageCh = nil
				continue
			}
			if err := s.handleClientMessage(outbound, data, switchSubscription, defaultGPU, logger); err != nil {
				if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
					return
				}
				logger.Warn("client message handling error", "err", err)
				return
			}
		case err := <-readErrCh:
			if err != nil && websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				logger.Warn("websocket read error", "err", err)
			}
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) defaultGPU() string {
	if s.cfg.DefaultGPU != "" && s.cfg.DefaultGPU != "auto" {
		if _, ok := s.gpuIndex[s.cfg.DefaultGPU]; ok {
			return s.cfg.DefaultGPU
		}
		s.logger.Warn("configured default gpu not found", "gpu_id", s.cfg.DefaultGPU)
	}
	if len(s.gpus) > 0 {
		return s.gpus[0].ID
	}
	return ""
}

func (s *Server) readMessages(ctx context.Context, conn *websocket.Conn, out chan<- []byte, errCh chan<- error) {
	defer close(out)
	for {
		readCtx := ctx
		var cancel context.CancelFunc
		if s.cfg.WS.ReadTimeout > 0 {
			readCtx, cancel = context.WithTimeout(ctx, s.cfg.WS.ReadTimeout)
		}
		msgType, data, err := conn.Read(readCtx)
		if cancel != nil {
			cancel()
		}
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				continue
			}
			errCh <- err
			return
		}
		if msgType != websocket.MessageText {
			continue
		}
		select {
		case out <- data:
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) handleClientMessage(outbound *wsOutbound, data []byte, switchSubscription func(string) error, defaultGPU string, logger *slog.Logger) error {
	var envelope api.ClientMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		logger.Debug("invalid client message", "err", err)
		return nil
	}

	switch envelope.Type {
	case "subscribe":
		var msg api.SubscribeMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			if !s.enqueueError(outbound, "invalid subscribe payload", logger) {
				return fmt.Errorf("failed to enqueue subscribe error")
			}
			return nil
		}
		target := msg.GPUId
		if target == "" {
			target = defaultGPU
		}
		if target == "" {
			if !s.enqueueError(outbound, "no gpu_id provided and no default available", logger) {
				return fmt.Errorf("failed to enqueue gpu missing error")
			}
			return nil
		}
		if err := switchSubscription(target); err != nil {
			if !s.enqueueError(outbound, err.Error(), logger) {
				return fmt.Errorf("failed to enqueue subscription error")
			}
			return nil
		}
	case "ping":
		if !s.enqueueMessage(outbound, api.PongMessage{Type: "pong"}, logger) {
			return fmt.Errorf("failed to enqueue pong response")
		}
	default:
		logger.Debug("unknown message type", "type", envelope.Type)
	}
	return nil
}

func (s *Server) wsWriter(ctx context.Context, conn *websocket.Conn, outbound *wsOutbound, cancel context.CancelFunc, logger *slog.Logger, done chan<- struct{}) {
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-outbound.channel():
			if !ok {
				return
			}
			if err := s.writeRaw(ctx, conn, msg); err != nil {
				if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
					logger.Warn("websocket write failed", "err", err)
				}
				cancel()
				return
			}
			s.wsSent.Add(1)
		}
	}
}

func (s *Server) writeRaw(ctx context.Context, conn *websocket.Conn, data []byte) error {
	writeCtx := ctx
	var cancel context.CancelFunc
	if s.cfg.WS.WriteTimeout > 0 {
		writeCtx, cancel = context.WithTimeout(ctx, s.cfg.WS.WriteTimeout)
	}
	if cancel != nil {
		defer cancel()
	}
	return conn.Write(writeCtx, websocket.MessageText, data)
}

func (s *Server) enqueueMessage(outbound *wsOutbound, payload any, logger *slog.Logger) bool {
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Error("failed to marshal websocket payload", "err", err)
		return false
	}
	if !outbound.enqueue(data) {
		logger.Warn("websocket outbound queue unavailable")
		return false
	}
	return true
}

func (s *Server) enqueueError(outbound *wsOutbound, msg string, logger *slog.Logger) bool {
	return s.enqueueMessage(outbound, api.ErrorMessage{Type: "error", Message: msg}, logger)
}

func (s *Server) reserveWS() bool {
	if s.maxWSClients <= 0 {
		s.wsActive.Add(1)
		return true
	}

	for {
		current := s.wsActive.Load()
		if current >= s.maxWSClients {
			s.wsRejected.Add(1)
			return false
		}
		if s.wsActive.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func (s *Server) releaseWS() {
	s.wsActive.Add(-1)
}

func (s *Server) registerPrometheus(mux *http.ServeMux) {
	registry := prometheus.NewRegistry()
	collectors := []prometheus.Collector{
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: "amdgputop",
			Subsystem: "ws",
			Name:      "active_connections",
			Help:      "Current number of active WebSocket clients.",
		}, func() float64 {
			return float64(s.wsActive.Load())
		}),
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Namespace: "amdgputop",
			Subsystem: "ws",
			Name:      "connections_total",
			Help:      "Total WebSocket connections accepted since start.",
		}, func() float64 {
			return float64(s.wsTotal.Load())
		}),
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Namespace: "amdgputop",
			Subsystem: "ws",
			Name:      "rejected_total",
			Help:      "Total WebSocket connection attempts rejected due to capacity.",
		}, func() float64 {
			return float64(s.wsRejected.Load())
		}),
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Namespace: "amdgputop",
			Subsystem: "ws",
			Name:      "messages_sent_total",
			Help:      "Total WebSocket messages sent to clients.",
		}, func() float64 {
			return float64(s.wsSent.Load())
		}),
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Namespace: "amdgputop",
			Subsystem: "ws",
			Name:      "messages_dropped_total",
			Help:      "Total WebSocket messages dropped due to backpressure.",
		}, func() float64 {
			return float64(s.wsDropped.Load())
		}),
	}

	if gpuCollector := newGPUMetricsCollector(s.gpus, s.sampler); gpuCollector != nil {
		collectors = append(collectors, gpuCollector)
	}

	for _, collector := range collectors {
		registry.MustRegister(collector)
	}

	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
}

func registerPprof(mux *http.ServeMux) {
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
}

func originPatterns(origins []string) []string {
	for _, origin := range origins {
		if origin == "*" {
			return nil
		}
	}
	dst := make([]string, len(origins))
	copy(dst, origins)
	return dst
}

func (s *Server) readiness() readyResponse {
	resp := readyResponse{
		GPUs: len(s.gpus),
	}

	if len(s.gpus) == 0 {
		resp.Status = "ok"
		return resp
	}

	if s.sampler == nil {
		resp.Status = "degraded"
		resp.Reason = "sampler_not_configured"
		return resp
	}

	readers := s.sampler.GPUIDs()
	resp.Readers = len(readers)
	if len(readers) == 0 {
		resp.Status = "degraded"
		resp.Reason = "no_metrics_readers"
		return resp
	}

	if s.sampler.Ready() {
		resp.Status = "ok"
		return resp
	}

	resp.Status = "initializing"
	resp.Reason = "waiting_for_samples"
	return resp
}

type readyResponse struct {
	Status  string `json:"status"`
	GPUs    int    `json:"gpus"`
	Readers int    `json:"metrics_readers"`
	Reason  string `json:"reason,omitempty"`
}

type wsOutbound struct {
	ch     chan []byte
	closed atomic.Bool
	drops  *atomic.Uint64
}

func newWSOutbound(size int, dropCounter *atomic.Uint64) *wsOutbound {
	if size <= 0 {
		size = 1
	}
	return &wsOutbound{
		ch:    make(chan []byte, size),
		drops: dropCounter,
	}
}

func (o *wsOutbound) enqueue(msg []byte) bool {
	if o.closed.Load() {
		o.countDrop()
		return false
	}

	select {
	case o.ch <- msg:
		return true
	default:
	}

	droppedOld := false
	select {
	case <-o.ch:
		droppedOld = true
	default:
	}
	if droppedOld {
		o.countDrop()
	}

	if o.closed.Load() {
		o.countDrop()
		return false
	}

	select {
	case o.ch <- msg:
		return true
	default:
		o.countDrop()
		return false
	}
}

func (o *wsOutbound) close() {
	if o.closed.CompareAndSwap(false, true) {
		close(o.ch)
	}
}

func (o *wsOutbound) channel() <-chan []byte {
	return o.ch
}

func (o *wsOutbound) countDrop() {
	if o.drops != nil {
		o.drops.Add(1)
	}
}
