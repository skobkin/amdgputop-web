package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/skobkin/amdgputop-web/internal/config"
	"github.com/skobkin/amdgputop-web/internal/gpu"
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
}

// New assembles a Server with its handlers.
func New(cfg config.Config, logger *slog.Logger, gpus []gpu.Info) *Server {
	s := &Server{
		cfg:    cfg,
		logger: logger,
		gpus:   gpus,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/api/gpus", s.handleAPIGPUs)
	mux.HandleFunc("/ws", s.handleWS)

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
			"procs": s.cfg.Proc.Enable,
		},
	}

	payload, err := json.Marshal(hello)
	if err != nil {
		s.logger.Error("failed to marshal hello message", "err", err)
		conn.Close(websocket.StatusInternalError, "internal error")
		return
	}

	writeCtx, cancel := context.WithTimeout(r.Context(), s.cfg.WS.WriteTimeout)
	defer cancel()

	if err := conn.Write(writeCtx, websocket.MessageText, payload); err != nil {
		s.logger.Warn("websocket write failed", "err", err)
		return
	}
}

type helloMessage struct {
	Type       string          `json:"type"`
	IntervalMS int             `json:"interval_ms"`
	GPUs       []gpu.Info      `json:"gpus"`
	Features   map[string]bool `json:"features"`
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
