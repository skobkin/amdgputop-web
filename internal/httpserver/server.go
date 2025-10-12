package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/skobkin/amdgputop-web/internal/config"
	"github.com/skobkin/amdgputop-web/internal/gpu"
	"github.com/skobkin/amdgputop-web/internal/procscan"
	"github.com/skobkin/amdgputop-web/internal/sampler"
	"github.com/skobkin/amdgputop-web/internal/version"
	"nhooyr.io/websocket"
)

const (
	readHeaderTimeout = 5 * time.Second
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
	mux.HandleFunc("/api/gpus", s.handleAPIGPUs)
	mux.HandleFunc("/api/gpus/", s.handleAPIGPUSubresource)
	mux.HandleFunc("/ws", s.handleWS)
	mux.Handle("/", s.staticHandler())

	s.httpServer = &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
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

	statusCode := http.StatusOK
	if info.Status != "ok" {
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(info); err != nil {
		s.logger.Error("failed to encode readyz response", "err", err)
	}
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	info := version.Current()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(info); err != nil {
		s.logger.Error("failed to encode version response", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleAPIGPUs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(s.gpus); err != nil {
		s.logger.Error("failed to encode gpu list", "err", err)
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(sample); err != nil {
		s.logger.Error("failed to encode gpu metrics", "gpu_id", gpuID, "err", err)
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(snapshot); err != nil {
		s.logger.Error("failed to encode gpu process data", "gpu_id", gpuID, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	opts := &websocket.AcceptOptions{
		OriginPatterns: originPatterns(s.cfg.AllowedOrigins),
	}

	conn, err := websocket.Accept(w, r, opts)
	if err != nil {
		s.logger.Warn("websocket accept failed", "err", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	hello := helloMessage{
		Type:       "hello",
		IntervalMS: int(s.cfg.SampleInterval / time.Millisecond),
		GPUs:       s.gpus,
		Features: map[string]bool{
			"procs": s.proc != nil,
		},
	}

	if err := s.writeJSON(r.Context(), conn, hello); err != nil {
		s.logger.Warn("failed to send hello", "err", err)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	messageCh := make(chan []byte, 8)
	readErrCh := make(chan error, 1)
	go s.readMessages(ctx, conn, messageCh, readErrCh)

	defaultGPU := s.defaultGPU()

	var (
		subCh           <-chan sampler.Sample
		unsubscribe     func()
		procCh          <-chan procscan.Snapshot
		procUnsubscribe func()
		currentGPU      string
	)

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
				s.logger.Warn("failed to subscribe proc scanner", "gpu_id", target, "err", err)
			} else {
				procCh = procStream
				procUnsubscribe = procCancel
			}
		}
		currentGPU = target
		s.logger.Info("ws subscribed", "gpu_id", target)
		return nil
	}

	defer func() {
		if unsubscribe != nil {
			unsubscribe()
		}
		if procUnsubscribe != nil {
			procUnsubscribe()
		}
	}()

	if defaultGPU != "" {
		if err := switchSubscription(defaultGPU); err != nil {
			s.logger.Warn("failed to subscribe default gpu", "gpu_id", defaultGPU, "err", err)
			_ = s.sendError(ctx, conn, fmt.Sprintf("failed to subscribe default gpu: %v", err))
		}
	} else if len(s.gpus) == 0 {
		_ = s.sendError(ctx, conn, "no GPUs detected")
	}

	for {
		select {
		case sample, ok := <-subCh:
			if !ok {
				subCh = nil
				currentGPU = ""
				continue
			}
			if err := s.writeJSON(ctx, conn, statsMessage{Type: "stats", Sample: sample}); err != nil {
				s.logger.Warn("failed to write stats message", "err", err)
				return
			}
		case snapshot, ok := <-procCh:
			if !ok {
				procCh = nil
				continue
			}
			if err := s.writeJSON(ctx, conn, procsMessage{Type: "procs", Snapshot: snapshot}); err != nil {
				s.logger.Warn("failed to write procs message", "err", err)
				return
			}
		case data, ok := <-messageCh:
			if !ok {
				messageCh = nil
				continue
			}
			if err := s.handleClientMessage(ctx, conn, data, switchSubscription, defaultGPU); err != nil {
				if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
					return
				}
				s.logger.Warn("client message handling error", "err", err)
				return
			}
		case err := <-readErrCh:
			if err != nil && websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				s.logger.Warn("websocket read error", "err", err)
			}
			return
		case <-ctx.Done():
			return
		}
	}
}

type helloMessage struct {
	Type       string          `json:"type"`
	IntervalMS int             `json:"interval_ms"`
	GPUs       []gpu.Info      `json:"gpus"`
	Features   map[string]bool `json:"features"`
}

type statsMessage struct {
	Type string `json:"type"`
	sampler.Sample
}

type procsMessage struct {
	Type string `json:"type"`
	procscan.Snapshot
}

type errorMessage struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type clientMessage struct {
	Type string `json:"type"`
}

type subscribeMessage struct {
	Type  string `json:"type"`
	GPUId string `json:"gpu_id"`
}

type pongMessage struct {
	Type string `json:"type"`
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
		readCtx, cancel := context.WithTimeout(ctx, s.cfg.WS.ReadTimeout)
		msgType, data, err := conn.Read(readCtx)
		cancel()
		if err != nil {
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

func (s *Server) handleClientMessage(ctx context.Context, conn *websocket.Conn, data []byte, switchSubscription func(string) error, defaultGPU string) error {
	var envelope clientMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		s.logger.Debug("invalid client message", "err", err)
		return nil
	}

	switch envelope.Type {
	case "subscribe":
		var msg subscribeMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return s.sendError(ctx, conn, "invalid subscribe payload")
		}
		target := msg.GPUId
		if target == "" {
			target = defaultGPU
		}
		if target == "" {
			return s.sendError(ctx, conn, "no gpu_id provided and no default available")
		}
		if err := switchSubscription(target); err != nil {
			return s.sendError(ctx, conn, err.Error())
		}
	case "ping":
		return s.writeJSON(ctx, conn, pongMessage{Type: "pong"})
	default:
		s.logger.Debug("unknown message type", "type", envelope.Type)
	}
	return nil
}

func (s *Server) writeJSON(ctx context.Context, conn *websocket.Conn, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, s.cfg.WS.WriteTimeout)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, data)
}

func (s *Server) sendError(ctx context.Context, conn *websocket.Conn, msg string) error {
	return s.writeJSON(ctx, conn, errorMessage{Type: "error", Message: msg})
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
