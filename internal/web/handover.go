package web

import (
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
	ownerSessionID, ok := s.requireOwnerHTTPAuth(w, r)
	if !ok {
		return
	}
	if !s.requireCSRF(w, r, ownerSessionID) {
		return
	}
	var req handoverRequest
	if !s.readJSONBody(w, r, ownerSessionID, &req) {
		return
	}
	result, err := s.executeLocalControl(r.Context(), controlRequest{SessionID: req.SessionID, Action: "handover", Target: req.Target})
	if err != nil {
		s.log.Warn("web handover failed", "request_id", requestID(r), "session_id", req.SessionID, "target", req.Target, "err", err, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.log.Info("web handover accepted", "request_id", requestID(r), "session_id", req.SessionID, "target", req.Target, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
	writeControlResponse(w, result)
}
