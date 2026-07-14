package web

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
	"github.com/gongahkia/onibi/internal/fleetnode"
)

func (s *Server) handleFleetMeshHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.db == nil {
		http.Error(w, "fleet unavailable", http.StatusServiceUnavailable)
		return
	}
	var request fleet.MeshHealthRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.Validate() != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	now := time.Now().UTC()
	if skew := now.Sub(request.SentAt.UTC()); skew > fleet.MeshHealthSkew || skew < -fleet.MeshHealthSkew {
		http.Error(w, "mesh health timestamp outside allowed skew", http.StatusBadRequest)
		return
	}
	config, configured, err := fleetnode.LoadConfig(r.Context(), s.db)
	if err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	if !configured {
		http.NotFound(w, r)
		return
	}
	identity, found, err := fleetnode.LoadIdentity(r.Context(), s.db)
	if err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	if !found || identity.HostID != config.HostID {
		http.NotFound(w, r)
		return
	}
	private, err := identity.PrivateKeyBytes()
	if err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	response := fleet.MeshHealthResponse{Version: fleet.ProtocolVersion, HostID: identity.HostID, Nonce: request.Nonce, SentAt: now}
	response.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.MeshHealthSigningPayload(request, response)))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}
