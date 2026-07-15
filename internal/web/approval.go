package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	approvals "github.com/gongahkia/onibi/internal/approval"
)

type approvalDecisionRequest struct {
	Verdict     string `json:"verdict"`
	EditedInput string `json:"edited_input"`
	Reason      string `json:"reason"`
}

func (s *Server) handleApproval(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if r.Method == http.MethodGet {
		s.handleApprovalGet(w, r, started)
		return
	}
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
	if s.approvalQueue == nil {
		s.log.Warn("web approval failed", "request_id", requestID(r), "reason", "queue_unavailable", "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
		http.Error(w, "approval queue unavailable", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		s.log.Warn("web approval failed", "request_id", requestID(r), "reason", "missing_id", "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
		http.Error(w, "approval id required", http.StatusBadRequest)
		return
	}
	var req approvalDecisionRequest
	if !s.readJSONBody(w, r, ownerSessionID, &req) {
		return
	}
	verdict, err := mapApprovalVerdict(req.Verdict)
	if err != nil {
		s.log.Warn("web approval failed", "request_id", requestID(r), "approval_id", id, "reason", "bad_verdict", "verdict", req.Verdict, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a, err := s.approvalQueue.Get(r.Context(), id)
	if err != nil {
		s.log.Warn("web approval lookup failed", "request_id", requestID(r), "approval_id", id, "err", err, "verdict", req.Verdict, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
		writeApprovalError(w, err)
		return
	}
	if verdict == approvals.VerdictEdit {
		if req.EditedInput == "" {
			s.log.Warn("web approval failed", "request_id", requestID(r), "approval_id", id, "session_id", a.SessionID, "agent", a.Agent, "tool", a.Tool, "reason", "empty_edit", "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
			http.Error(w, "edited_input required", http.StatusBadRequest)
			return
		}
		if err := approvals.ValidateEditedInput(a.Tool, a.InputJSON, req.EditedInput); err != nil {
			s.log.Warn("web approval failed", "request_id", requestID(r), "approval_id", id, "session_id", a.SessionID, "agent", a.Agent, "tool", a.Tool, "reason", "invalid_edit", "err", err, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	_, err = s.approvalQueue.DecideIdempotently(r.Context(), id, verdict, req.EditedInput, req.Reason, 0)
	if err != nil {
		s.log.Warn("web approval decide failed", "request_id", requestID(r), "approval_id", id, "session_id", a.SessionID, "agent", a.Agent, "tool", a.Tool, "verdict", verdict, "err", err, "age_ms", time.Since(a.CreatedAt).Milliseconds(), "ttl_remaining_ms", time.Until(a.ExpiresAt).Milliseconds(), "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
		writeApprovalError(w, err)
		return
	}
	s.log.Info("web approval decided",
		"request_id", requestID(r),
		"approval_id", id,
		"session_id", a.SessionID,
		"agent", a.Agent,
		"tool", a.Tool,
		"verdict", verdict,
		"edited", req.EditedInput != "",
		"age_ms", time.Since(a.CreatedAt).Milliseconds(),
		"ttl_remaining_ms", time.Until(a.ExpiresAt).Milliseconds(),
		"remote", remoteHost(r.RemoteAddr),
		"duration_ms", time.Since(started).Milliseconds(),
	)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handleApprovalGet(w http.ResponseWriter, r *http.Request, started time.Time) {
	if _, ok := s.requireOwnerHTTPAuth(w, r); !ok {
		return
	}
	if s.approvalQueue == nil {
		s.log.Warn("web approval status failed", "request_id", requestID(r), "reason", "queue_unavailable", "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
		http.Error(w, "approval queue unavailable", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "approval id required", http.StatusBadRequest)
		return
	}
	a, err := s.approvalQueue.Get(r.Context(), id)
	if err != nil {
		s.log.Warn("web approval status lookup failed", "request_id", requestID(r), "approval_id", id, "err", err, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
		writeApprovalError(w, err)
		return
	}
	model, err := approvals.PayloadForApproval(*a)
	if err != nil {
		s.log.Warn("web approval status invalid payload", "request_id", requestID(r), "approval_id", id, "err", err)
		http.Error(w, "invalid approval payload", http.StatusInternalServerError)
		return
	}
	payload := map[string]any{
		"id":         model.ID,
		"session_id": model.SessionID,
		"agent":      model.Agent,
		"tool":       model.Tool,
		"state":      model.State,
		"approval":   model,
		"expires_at": a.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}
	if a.Reason != "" {
		payload["reason"] = a.Reason
	}
	if !a.DecidedAt.IsZero() {
		payload["decided_at"] = a.DecidedAt.Unix()
	}
	if model.Details.FilePath != "" {
		payload["file_path"] = model.Details.FilePath
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func mapApprovalVerdict(v string) (approvals.Verdict, error) {
	switch v {
	case "approve":
		return approvals.VerdictApprove, nil
	case "deny":
		return approvals.VerdictDeny, nil
	case "edit":
		return approvals.VerdictEdit, nil
	default:
		return "", errors.New("bad verdict")
	}
}

func writeApprovalError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, approvals.ErrUnknownApproval):
		http.Error(w, "approval not found", http.StatusNotFound)
	case errors.Is(err, approvals.ErrAlreadyDecided), errors.Is(err, approvals.ErrExpired):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
