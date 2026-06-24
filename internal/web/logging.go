package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type requestIDKey struct{}

var requestSeq atomic.Uint64

func (s *Server) loggedHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := fmt.Sprintf("req-%06x", requestSeq.Add(1))
		w.Header().Set("X-Onibi-Request-ID", id)
		rec := &statusRecorder{ResponseWriter: w}
		start := time.Now()
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
		s.log.Info("web request",
			"request_id", id,
			"method", r.Method,
			"path", safeRequestPath(r.URL.Path),
			"status", rec.statusCode(),
			"bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", remoteHost(r.RemoteAddr),
			"user_agent", trimForLog(r.UserAgent(), 160),
		)
	})
}

func requestID(r *http.Request) string {
	id, _ := r.Context().Value(requestIDKey{}).(string)
	return id
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += int64(n)
	return n, err
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (r *statusRecorder) statusCode() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

type slogWriter struct {
	server *Server
}

func (w slogWriter) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	if msg != "" {
		w.server.log.Warn("web server", "message", msg)
	}
	return len(p), nil
}

func safeRequestPath(path string) string {
	if strings.HasPrefix(path, "/pair/") {
		return "/pair/<redacted>"
	}
	return path
}

func remoteHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func trimForLog(value string, max int) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	if len(value) <= max {
		return value
	}
	return value[:max] + "..."
}
