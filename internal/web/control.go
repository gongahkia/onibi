package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/gongahkia/onibi/internal/pty"
)

type controlRequest struct {
	SessionID string `json:"session_id"`
	Action    string `json:"action"`
	Input     string `json:"input"`
	Target    string `json:"target"`
}

type controlResponse struct {
	OK     bool   `json:"ok"`
	Result string `json:"result,omitempty"`
}

var errControlSessionNotFound = errors.New("session not found")

func (s *Server) handleControl(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if r.Method != http.MethodPost {
		writeControlError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ownerSessionID, ok := s.requireHTTPAuth(w, r)
	if !ok {
		return
	}
	if !s.requireCSRF(w, r, ownerSessionID) {
		return
	}
	var req controlRequest
	if !s.readJSONBody(w, r, ownerSessionID, &req) {
		return
	}
	if req.Action == "page_up" || req.Action == "page_down" {
		s.handleScrollControl(w, r, req, started)
		return
	}
	result, err := s.executeLocalControl(r.Context(), req)
	if err != nil {
		s.log.Warn("web control failed", "request_id", requestID(r), "session_id", req.SessionID, "action", req.Action, "err", err, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
		status := http.StatusBadRequest
		if errors.Is(err, errControlSessionNotFound) {
			status = http.StatusNotFound
		}
		writeControlError(w, err.Error(), status)
		return
	}
	s.log.Info("web control accepted", "request_id", requestID(r), "session_id", req.SessionID, "action", req.Action, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
	writeControlResponse(w, result)
}

func (s *Server) handleScrollControl(w http.ResponseWriter, r *http.Request, req controlRequest, started time.Time) {
	if req.Action == "page_up" || req.Action == "page_down" {
		if s.scroll == nil {
			writeControlError(w, "scroll unavailable", http.StatusNotImplemented)
			return
		}
		if err := s.scroll(r.Context(), req.SessionID, req.Action); err != nil {
			s.log.Warn("web control failed", "request_id", requestID(r), "reason", "scroll_failed", "session_id", req.SessionID, "action", req.Action, "err", err, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
			writeControlError(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.log.Info("web control accepted", "request_id", requestID(r), "session_id", req.SessionID, "action", req.Action, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		return
	}
}

func (s *Server) executeLocalControl(ctx context.Context, req controlRequest) (string, error) {
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action != "interrupt" && action != "kill" && action != "input" && action != "handover" {
		return "", errors.New("bad action")
	}
	if strings.TrimSpace(req.SessionID) == "" {
		return "", errors.New("session id required")
	}
	if action == "handover" {
		if s.handover == nil {
			return "", errors.New("handover unavailable")
		}
		return s.handover(ctx, req.SessionID, req.Target)
	}
	host, ok := s.hostForSession(ctx, req.SessionID)
	if !ok {
		return "", errControlSessionNotFound
	}
	switch action {
	case "interrupt":
		return "", signalHost(host, syscall.SIGINT)
	case "kill":
		return "", signalHost(host, syscall.SIGKILL)
	case "input":
		if strings.TrimSpace(req.Input) == "" {
			return "", errors.New("control input required")
		}
		if _, err := host.Write([]byte(req.Input)); err != nil {
			return "", err
		}
		_, err := host.Write([]byte{'\n'})
		return "", err
	default:
		return "", errors.New("bad action")
	}
}

func writeControlResponse(w http.ResponseWriter, result string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(controlResponse{OK: true, Result: result})
}

func writeControlError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
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
