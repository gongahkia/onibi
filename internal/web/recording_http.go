package web

import (
	"net/http"
	"os"
)

func (s *Server) handleSessionRecording(w http.ResponseWriter, r *http.Request) {
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
	id := r.PathValue("id")
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
