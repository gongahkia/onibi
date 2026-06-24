package web

import (
	"fmt"
	"net/http"

	"github.com/gongahkia/onibi/internal/setup"
)

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	token := r.PathValue("token")
	if token == "" || s.db == nil {
		s.log.Warn("web pair failed", "request_id", requestID(r), "reason", "missing_token_or_db", "remote", remoteHost(r.RemoteAddr), "user_agent", trimForLog(r.UserAgent(), 160))
		pairFailed(w)
		return
	}
	if err := setup.Consume(r.Context(), s.db, token); err != nil {
		s.log.Warn("web pair failed", "request_id", requestID(r), "reason", err.Error(), "remote", remoteHost(r.RemoteAddr), "user_agent", trimForLog(r.UserAgent(), 160))
		pairFailed(w)
		return
	}
	if _, err := s.CreateOwnerSession(r.Context(), w, r.UserAgent()); err != nil {
		s.log.Error("web pair session create failed", "request_id", requestID(r), "err", err, "remote", remoteHost(r.RemoteAddr))
		http.Error(w, "pair failed", http.StatusInternalServerError)
		return
	}
	s.log.Info("web pair accepted", "request_id", requestID(r), "remote", remoteHost(r.RemoteAddr), "user_agent", trimForLog(r.UserAgent(), 160))
	http.Redirect(w, r, "/", http.StatusFound)
}

func pairFailed(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprint(w, "pair token expired or already used")
}
