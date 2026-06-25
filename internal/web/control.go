package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"syscall"
	"time"

	"github.com/gongahkia/onibi/internal/pty"
)

type controlRequest struct {
	SessionID string `json:"session_id"`
	Action    string `json:"action"`
}

func (s *Server) handleControl(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.requireHTTPAuth(w, r); !ok {
		return
	}
	var req controlRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		s.log.Warn("web control bad request", "request_id", requestID(r), "err", err, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	host, ok := s.hostForSession(req.SessionID)
	if !ok {
		s.log.Warn("web control failed", "request_id", requestID(r), "reason", "session_not_found", "session_id", req.SessionID, "action", req.Action, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	var sig syscall.Signal
	switch req.Action {
	case "interrupt":
		sig = syscall.SIGINT
	case "kill":
		sig = syscall.SIGKILL
	default:
		s.log.Warn("web control failed", "request_id", requestID(r), "reason", "bad_action", "session_id", req.SessionID, "action", req.Action, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
		http.Error(w, "bad action", http.StatusBadRequest)
		return
	}
	if err := signalHost(host, sig); err != nil {
		s.log.Warn("web control failed", "request_id", requestID(r), "reason", "signal_failed", "session_id", req.SessionID, "action", req.Action, "signal", sig.String(), "err", err, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.log.Info("web control accepted", "request_id", requestID(r), "session_id", req.SessionID, "action", req.Action, "signal", sig.String(), "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func signalHost(host *pty.Host, sig syscall.Signal) error {
	if host == nil {
		return errors.New("session has no host")
	}
	if sig == syscall.SIGINT {
		_, err := host.Write([]byte{3})
		return err
	}
	if host.Cmd != nil && host.Cmd.Process != nil {
		return signalPID(host.Cmd.Process.Pid, sig)
	}
	switch sig {
	case syscall.SIGKILL:
		return host.Close()
	default:
		return errors.New("unsupported signal")
	}
}

func signalPID(pid int, sig syscall.Signal) error {
	if pid <= 0 {
		return errors.New("process not started")
	}
	if pgid, err := syscall.Getpgid(pid); err == nil {
		return syscall.Kill(-pgid, sig)
	}
	return syscall.Kill(pid, sig)
}
