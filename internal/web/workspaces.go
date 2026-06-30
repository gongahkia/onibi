package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gongahkia/onibi/internal/workspace"
)

type WorkspaceState struct {
	Current    string             `json:"current"`
	Workspaces []WorkspaceSummary `json:"workspaces"`
}

type WorkspaceSummary struct {
	Name             string `json:"name"`
	Path             string `json:"path"`
	LastSeen         string `json:"last_seen"`
	DefaultTransport string `json:"default_transport,omitempty"`
	Current          bool   `json:"current"`
}

type workspaceUseRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleWorkspaces(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.requireHTTPAuth(w, r); !ok {
			return
		}
		state, err := s.workspaceState(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state)
	case http.MethodPost:
		ownerSessionID, ok := s.requireHTTPAuth(w, r)
		if !ok {
			return
		}
		var req workspaceUseRequest
		if !s.readJSONBody(w, r, ownerSessionID, &req) {
			return
		}
		if err := s.setWorkspaceDefault(r, req.Name); err != nil {
			if errors.Is(err, errWorkspaceNotFound) {
				http.Error(w, "workspace not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		state, err := s.workspaceState(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

var errWorkspaceNotFound = errors.New("workspace not found")

func (s *Server) setWorkspaceDefault(r *http.Request, name string) error {
	if s.db == nil {
		return errors.New("workspaces unavailable")
	}
	wsStore, err := workspace.NewDBStore(s.db)
	if err != nil {
		return err
	}
	if _, ok, err := wsStore.Get(r.Context(), name); err != nil {
		return err
	} else if !ok {
		return errWorkspaceNotFound
	}
	return workspace.SetDefaultName(r.Context(), s.db, name)
}

func (s *Server) workspaceState(r *http.Request) (WorkspaceState, error) {
	if s.db == nil {
		return WorkspaceState{}, errors.New("workspaces unavailable")
	}
	wsStore, err := workspace.NewDBStore(s.db)
	if err != nil {
		return WorkspaceState{}, err
	}
	current, _, err := workspace.DefaultName(r.Context(), s.db)
	if err != nil {
		return WorkspaceState{}, err
	}
	entries, err := wsStore.List(r.Context())
	if err != nil {
		return WorkspaceState{}, err
	}
	state := WorkspaceState{Current: current}
	for _, entry := range entries {
		row := WorkspaceSummary{
			Name:     entry.Name,
			Path:     entry.Path,
			LastSeen: formatWorkspaceTime(entry.LastSeen),
			Current:  entry.Name == current,
		}
		if indexEntry, ok := workspaceIndex(entry.Name); ok {
			row.DefaultTransport = indexEntry.DefaultTransport
		}
		state.Workspaces = append(state.Workspaces, row)
	}
	return state, nil
}

func workspaceIndex(name string) (workspace.IndexEntry, bool) {
	dir, err := workspace.DefaultIndexDir()
	if err != nil {
		return workspace.IndexEntry{}, false
	}
	entry, err := workspace.LoadIndexEntry(dir, name)
	return entry, err == nil
}

func formatWorkspaceTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
