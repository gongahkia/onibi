package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gongahkia/onibi/internal/buildinfo"
)

type counters struct{ requests, active, errors, completed, rejected, uncertain atomic.Uint64 }
type RuntimeStatus struct {
	PID            int         `json:"pid"`
	Executable     string      `json:"executable"`
	Version        string      `json:"version"`
	Commit         string      `json:"commit"`
	Origin         string      `json:"origin"`
	StartedAt      time.Time   `json:"startedAt"`
	UptimeSeconds  int64       `json:"uptimeSeconds"`
	Requests       uint64      `json:"requests"`
	ActiveRequests uint64      `json:"activeRequests"`
	Errors         uint64      `json:"errors"`
	Completed      uint64      `json:"completed"`
	Rejected       uint64      `json:"rejected"`
	Uncertain      uint64      `json:"uncertain"`
	Inventory      Observation `json:"inventory"`
	Capture        Observation `json:"capture"`
}

// RuntimeStatus uses cached observations only; it never invokes Ghostty.
func (s *Server) RuntimeStatus() RuntimeStatus {
	s.mu.Lock()
	inventory, capture := s.inventoryObservation, s.captureObservation
	s.mu.Unlock()
	// Local status needs health, not pane identities.
	inventory.TargetID, capture.TargetID = "", ""
	if inventory.State == "" {
		inventory.State = "unknown"
	}
	if capture.State == "" {
		capture.State = "unknown"
	}
	executable, _ := os.Executable()
	return RuntimeStatus{PID: os.Getpid(), Executable: executable, Version: buildinfo.Version, Commit: buildinfo.Commit, Origin: s.origin, StartedAt: s.started, UptimeSeconds: int64(time.Since(s.started).Seconds()), Requests: s.metrics.requests.Load(), ActiveRequests: s.metrics.active.Load(), Errors: s.metrics.errors.Load(), Completed: s.metrics.completed.Load(), Rejected: s.metrics.rejected.Load(), Uncertain: s.metrics.uncertain.Load(), Inventory: inventory, Capture: capture}
}

type requestRecorder struct {
	http.ResponseWriter
	status int
	id     string
}

func (w *requestRecorder) WriteHeader(status int) {
	if status < 200 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *requestRecorder) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
func (w *requestRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func safeRoute(path string) string {
	switch path {
	case "/api/session", "/api/pair", "/api/logout", "/api/targets", "/api/snapshot", "/api/actions", "/api/health", "/api/capabilities", "/api/devices", "/api/devices/revoke":
		return path
	}
	if strings.HasPrefix(path, "/api/actions/") {
		return "/api/actions/:receipt"
	}
	if strings.HasPrefix(path, "/api/") {
		return "/api/unknown"
	}
	return "asset"
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	id := s.metrics.requests.Add(1)
	s.metrics.active.Add(1)
	recorder := &requestRecorder{ResponseWriter: w, id: fmt.Sprintf("%x-%x", s.started.Unix(), id)}
	w.Header().Set("X-Onibi-Request-ID", recorder.id)
	defer func() {
		s.metrics.active.Add(^uint64(0))
		status := recorder.status
		if status == 0 {
			status = 200
		}
		level := slog.LevelDebug
		if r.Method == "POST" {
			level = slog.LevelInfo
		}
		if status >= 400 {
			level = slog.LevelWarn
			s.metrics.errors.Add(1)
		}
		if status >= 500 {
			level = slog.LevelError
		}
		if status == 401 && r.URL.Path == "/api/session" {
			level = slog.LevelInfo
		}
		method := r.Method
		if method != "GET" && method != "POST" && method != "HEAD" {
			method = "OTHER"
		}
		s.log.Log(r.Context(), level, "web.request", "request_id", recorder.id, "method", method, "route", safeRoute(r.URL.Path), "status", status, "duration_ms", time.Since(started).Milliseconds())
	}()
	s.serveHTTP(recorder, r)
}
func (s *Server) recordReceipt(w http.ResponseWriter, receipt Receipt, kind string) {
	switch receipt.State {
	case "completed":
		s.metrics.completed.Add(1)
	case "rejected":
		s.metrics.rejected.Add(1)
	case "uncertain":
		s.metrics.uncertain.Add(1)
	}
	id := ""
	if recorder, ok := w.(*requestRecorder); ok {
		id = recorder.id
	}
	level := slog.LevelInfo
	if receipt.State == "uncertain" {
		level = slog.LevelWarn
	}
	s.log.Log(context.Background(), level, "web.action", "request_id", id, "kind", kind, "state", receipt.State)
}
