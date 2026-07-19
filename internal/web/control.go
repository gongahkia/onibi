package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/store"
)

type controlRequest struct {
	CommandID string `json:"command_id"`
	HostID    string `json:"host_id"`
	SessionID string `json:"session_id"`
	Action    string `json:"action"`
	Input     string `json:"input"`
	Target    string `json:"target"`
}

type controlResponse struct {
	OK        bool               `json:"ok"`
	CommandID string             `json:"command_id"`
	State     fleet.CommandState `json:"state"`
	Result    string             `json:"result,omitempty"`
}

const controlCommandTTL = 30 * time.Second

func (s *Server) handleControl(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if r.Method == http.MethodGet {
		s.handleControlStatus(w, r)
		return
	}
	if r.Method != http.MethodPost {
		writeControlError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ownerSessionID, ok := s.requireOwnerHTTPAuth(w, r)
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
	command, err := s.submitControl(r.Context(), req)
	if err != nil {
		s.log.Warn("web control failed", "request_id", requestID(r), "session_id", req.SessionID, "action", req.Action, "err", err, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
		writeControlError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if command.State == fleet.CommandFailed || command.State == fleet.CommandTimedOut {
		status := http.StatusInternalServerError
		if command.Result == "session not found" {
			status = http.StatusNotFound
		}
		writeControlError(w, command.Result, status)
		return
	}
	s.log.Info("web control accepted", "request_id", requestID(r), "command_id", command.ID, "session_id", req.SessionID, "action", req.Action, "state", command.State, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
	writeControlResponse(w, command)
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

func (s *Server) handleControlStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireOwnerHTTPAuth(w, r); !ok {
		return
	}
	if s.db == nil {
		writeControlError(w, "command store unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, err := s.db.ControlCommandsExpire(r.Context(), time.Now().UTC()); err != nil {
		writeControlError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	command, err := s.db.ControlCommand(r.Context(), r.PathValue("id"))
	if err != nil {
		writeControlError(w, err.Error(), http.StatusNotFound)
		return
	}
	writeControlResponse(w, command)
}

func (s *Server) submitControl(ctx context.Context, req controlRequest) (store.ControlCommand, error) {
	if s.db == nil {
		return store.ControlCommand{}, errors.New("command store unavailable")
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action != "interrupt" && action != "kill" && action != "input" && action != "handover" {
		return store.ControlCommand{}, errors.New("bad action")
	}
	if strings.TrimSpace(req.SessionID) == "" {
		return store.ControlCommand{}, errors.New("session id required")
	}
	id, err := controlCommandID(req.CommandID)
	if err != nil {
		return store.ControlCommand{}, err
	}
	hostID := strings.TrimSpace(req.HostID)
	if hostID == "" {
		hostID = "local"
	}
	payload, err := json.Marshal(fleet.ControlPayload{SessionID: req.SessionID, Input: req.Input, Target: req.Target})
	if err != nil {
		return store.ControlCommand{}, err
	}
	now := time.Now().UTC()
	if _, err := s.db.ControlCommandsExpire(ctx, now); err != nil {
		return store.ControlCommand{}, err
	}
	command, created, err := s.db.ControlCommandCreate(ctx, store.ControlCommand{ID: id, HostID: hostID, SessionID: req.SessionID, Action: action, Payload: payload, State: fleet.CommandPending, CreatedAt: now, ExpiresAt: now.Add(controlCommandTTL)})
	if err != nil || !created {
		return command, err
	}
	if hostID != "local" {
		if err := s.dispatchFleetControl(ctx, command); err == nil {
			command, err = s.db.ControlCommand(ctx, command.ID)
		}
		return command, nil
	}
	result, err := s.executeLocalControl(ctx, command, req)
	state := fleet.CommandSucceeded
	if err != nil {
		state = fleet.CommandFailed
		result = controlError(err)
	}
	if _, err := s.db.ControlCommandComplete(ctx, command.ID, state, result, time.Now().UTC()); err != nil {
		return store.ControlCommand{}, err
	}
	return s.db.ControlCommand(ctx, command.ID)
}

func (s *Server) executeLocalControl(ctx context.Context, command store.ControlCommand, req controlRequest) (string, error) {
	if command.Action == "handover" {
		if s.handover == nil {
			return "", errors.New("handover unavailable")
		}
		return s.handover(ctx, command.SessionID, req.Target)
	}
	host, ok := s.hostForSession(ctx, command.SessionID)
	if !ok {
		return "", errors.New("session not found")
	}
	switch command.Action {
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

func controlCommandID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return newFleetLinkID()
	}
	if len(value) < 3 || len(value) > 64 {
		return "", errors.New("invalid command id")
	}
	for i, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '_' || (i == 0 && (r == '-' || r == '_')) {
			return "", errors.New("invalid command id")
		}
	}
	return value, nil
}

func controlError(err error) string {
	message := strings.TrimSpace(err.Error())
	if len(message) > 512 {
		return message[:512]
	}
	if message == "" {
		return "control failed"
	}
	return message
}

func writeControlResponse(w http.ResponseWriter, command store.ControlCommand) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(controlResponse{OK: command.State != fleet.CommandFailed && command.State != fleet.CommandTimedOut, CommandID: command.ID, State: command.State, Result: command.Result})
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
