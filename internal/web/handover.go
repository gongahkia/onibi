package web

import (
	"encoding/json"
	"net/http"
	"time"
)

type handoverRequest struct {
	SessionID string `json:"session_id"`
	Target    string `json:"target"`
}

func (s *Server) handleHandover(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.requireHTTPAuth(w, r); !ok {
		return
	}
	if s.handover == nil {
		http.Error(w, "handover unavailable", http.StatusNotImplemented)
		return
	}
	var req handoverRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		s.log.Warn("web handover bad request", "request_id", requestID(r), "err", err, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	msg, err := s.handover(r.Context(), req.SessionID, req.Target)
	if err != nil {
		s.log.Warn("web handover failed", "request_id", requestID(r), "session_id", req.SessionID, "target", req.Target, "err", err, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.log.Info("web handover accepted", "request_id", requestID(r), "session_id", req.SessionID, "target", req.Target, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "message": msg})
}
