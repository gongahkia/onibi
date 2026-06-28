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
	sessionID, err := s.CreateOwnerSession(r.Context(), w, r.UserAgent())
	if err != nil {
		s.log.Error("web pair session create failed", "request_id", requestID(r), "err", err, "remote", remoteHost(r.RemoteAddr))
		http.Error(w, "pair failed", http.StatusInternalServerError)
		return
	}
	if s.relayKeys != nil {
		bound, err := s.relayKeys.BindSession(r.Context(), s.db, token, sessionID)
		if err != nil {
			s.log.Warn("web relay key bind failed", "request_id", requestID(r), "err", err, "remote", remoteHost(r.RemoteAddr))
		}
		if s.requireE2E && !bound {
			s.log.Warn("web relay key missing", "request_id", requestID(r), "remote", remoteHost(r.RemoteAddr))
			http.Error(w, "relay key missing", http.StatusUnauthorized)
			return
		}
	}
	s.log.Info("web pair accepted", "request_id", requestID(r), "remote", remoteHost(r.RemoteAddr), "user_agent", trimForLog(r.UserAgent(), 160))
	pairAccepted(w)
}

func pairFailed(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprint(w, "pair token expired or already used")
}

func pairAccepted(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, `<!doctype html><title>Onibi paired</title><body><p>Paired. Opening Onibi...</p><script>location.replace("/"+location.hash)</script><p><a href="/">Open Onibi</a></p></body>`)
}
