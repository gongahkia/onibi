package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type recordingsResponse struct {
	Recordings []RecordingSummary `json:"recordings"`
}

func (s *Server) handleRecordings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.requireHTTPAuth(w, r); !ok {
		return
	}
	if s.recordingList == nil {
		http.Error(w, "recordings unavailable", http.StatusServiceUnavailable)
		return
	}
	items, err := s.recordingList(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for i := range items {
		items[i].URL = "/recordings/" + url.PathEscape(items[i].ID) + ".cast"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(recordingsResponse{Recordings: items})
}

func (s *Server) handleRecordingCast(w http.ResponseWriter, r *http.Request) {
	s.serveRecording(w, r, strings.TrimSuffix(r.PathValue("id"), ".cast"))
}

func (s *Server) handleSessionRecording(w http.ResponseWriter, r *http.Request) {
	s.serveRecording(w, r, r.PathValue("id"))
}

func (s *Server) serveRecording(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.requireHTTPAuth(w, r); !ok {
		return
	}
	if s.recordingPath == nil {
		http.Error(w, "recording unavailable", http.StatusServiceUnavailable)
		return
	}
	path, ok, err := s.recordingPath(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "recording not found", http.StatusNotFound)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "recording not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-asciicast")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeRecordingName(id)+`.cast"`)
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}
