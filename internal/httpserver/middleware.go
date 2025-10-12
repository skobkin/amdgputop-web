package httpserver

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

type contextKey string

const requestLoggerKey contextKey = "httpserver.request.logger"

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (lrw *loggingResponseWriter) WriteHeader(status int) {
	lrw.status = status
	lrw.ResponseWriter.WriteHeader(status)
}

func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	if lrw.status == 0 {
		lrw.status = http.StatusOK
	}
	n, err := lrw.ResponseWriter.Write(b)
	lrw.bytes += int64(n)
	return n, err
}

func (lrw *loggingResponseWriter) Status() int {
	if lrw.status == 0 {
		return http.StatusOK
	}
	return lrw.status
}

func (lrw *loggingResponseWriter) Bytes() int64 {
	return lrw.bytes
}

func (lrw *loggingResponseWriter) Flush() {
	if f, ok := lrw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (lrw *loggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := lrw.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("httpserver: response writer does not support hijacking")
}

func (lrw *loggingResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := lrw.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (s *Server) withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := s.requestIDs.Add(1)
		logger := s.logger.With(
			"req_id", reqID,
			"method", r.Method,
			"path", r.URL.Path,
		)
		if remote := r.RemoteAddr; remote != "" {
			logger = logger.With("remote_addr", remote)
		}

		ctx := context.WithValue(r.Context(), requestLoggerKey, logger)
		lrw := &loggingResponseWriter{ResponseWriter: w}
		start := time.Now()

		next.ServeHTTP(lrw, r.WithContext(ctx))

		logger.Info("request complete",
			"status", lrw.Status(),
			"duration", time.Since(start),
			"bytes", lrw.Bytes(),
		)
	})
}

func (s *Server) loggerFromContext(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if logger, ok := ctx.Value(requestLoggerKey).(*slog.Logger); ok && logger != nil {
			return logger
		}
	}
	return s.logger
}
