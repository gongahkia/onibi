package web

import (
	"encoding/json"
	"errors"
	"net/http"

	approvals "github.com/gongahkia/onibi/internal/approval"
)

type approvalDecisionRequest struct {
	Verdict     string `json:"verdict"`
	EditedInput string `json:"edited_input"`
	Reason      string `json:"reason"`
}

func (s *Server) handleApproval(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.requireHTTPAuth(w, r); !ok {
		return
	}
	if s.approvalQueue == nil {
		http.Error(w, "approval queue unavailable", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "approval id required", http.StatusBadRequest)
		return
	}
	var req approvalDecisionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	verdict, err := mapApprovalVerdict(req.Verdict)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a, err := s.approvalQueue.Get(r.Context(), id)
	if err != nil {
		writeApprovalError(w, err)
		return
	}
	if verdict == approvals.VerdictEdit {
		if req.EditedInput == "" {
			http.Error(w, "edited_input required", http.StatusBadRequest)
			return
		}
		if err := approvals.ValidateEditedInput(a.Tool, a.InputJSON, req.EditedInput); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	err = s.approvalQueue.Decide(r.Context(), id, verdict, req.EditedInput, req.Reason, 0)
	if err != nil {
		writeApprovalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
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
