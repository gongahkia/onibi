package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type Snapshot struct {
	Name             string `json:"name"`
	SessionID        string `json:"session_id"`
	CreatedAt        string `json:"created_at"`
	CWD              string `json:"cwd"`
	TranscriptOffset int64  `json:"transcript_offset"`
}

type SnapshotListResponse struct {
	Snapshots []Snapshot `json:"snapshots"`
}

type SnapshotRestoreRequest struct {
	Name      string `json:"name,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

type SnapshotForkRequest struct {
	SessionID string `json:"session_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Turn      int    `json:"turn"`
	NewPrompt string `json:"new_prompt"`
}

type SnapshotActionResult struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

func (s *Server) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.requireHTTPAuth(w, r); !ok {
		return
	}
	if s.snapshots == nil {
		http.Error(w, "snapshots unavailable", http.StatusServiceUnavailable)
		return
	}
	rows, err := s.snapshots(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(SnapshotListResponse{Snapshots: rows})
}

func (s *Server) handleSnapshotRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerSessionID, ok := s.requireHTTPAuth(w, r)
	if !ok {
		return
	}
	if !s.requireCSRF(w, r, ownerSessionID) {
		return
	}
	if s.snapshotRestore == nil {
		http.Error(w, "snapshot restore unavailable", http.StatusServiceUnavailable)
		return
	}
	var req SnapshotRestoreRequest
	if !s.readJSONBody(w, r, ownerSessionID, &req) {
		return
	}
	name, err := snapshotName(r, req.Name)
	if err != nil {
		http.Error(w, "snapshot name required", http.StatusBadRequest)
		return
	}
	result, err := s.snapshotRestore(r.Context(), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) handleSnapshotFork(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerSessionID, ok := s.requireHTTPAuth(w, r)
	if !ok {
		return
	}
	if !s.requireCSRF(w, r, ownerSessionID) {
		return
	}
	if s.snapshotFork == nil {
		http.Error(w, "snapshot fork unavailable", http.StatusServiceUnavailable)
		return
	}
	var req SnapshotForkRequest
	if !s.readJSONBody(w, r, ownerSessionID, &req) {
		return
	}
	name, err := snapshotName(r, req.Name)
	if err != nil {
		http.Error(w, "snapshot name required", http.StatusBadRequest)
		return
	}
	req.Name = name
	result, err := s.snapshotFork(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func snapshotName(r *http.Request, bodyName string) (string, error) {
	if name := strings.TrimSpace(bodyName); name != "" {
		return name, nil
	}
	return snapshotNameFromPath(r)
}

func snapshotNameFromPath(r *http.Request) (string, error) {
	name, err := url.PathUnescape(r.PathValue("name"))
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("snapshot name required")
	}
	return name, nil
}
