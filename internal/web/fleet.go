package web

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
)

const fleetHubPrivateKey = "fleet_hub_ed25519_private"

const fleetHeartbeatMaxSkew = 2 * time.Minute

type fleetEnrollmentChallengeRequest struct {
	Host fleet.Host `json:"host"`
}

type fleetEnrollmentProofResponse struct {
	Host     fleet.Host `json:"host"`
	HubProof string     `json:"hub_proof"`
}

func (s *Server) handleFleetHosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.db == nil {
		http.Error(w, "fleet unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, ok := s.requireOwnerHTTPAuth(w, r); !ok {
		return
	}
	ownerID, err := s.db.FleetOwnerID(r.Context())
	if err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	hosts, err := s.db.FleetHostList(r.Context())
	if err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	out := make([]fleet.Host, 0, len(hosts))
	for _, host := range hosts {
		if host.OwnerID == ownerID {
			out = append(out, host)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"version": fleet.ProtocolVersion, "hosts": out})
}

func (s *Server) handleFleetEnrollmentChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.db == nil {
		http.Error(w, "fleet unavailable", http.StatusServiceUnavailable)
		return
	}
	ownerSessionID, ok := s.requireOwnerHTTPAuth(w, r)
	if !ok {
		return
	}
	if !s.requireCSRF(w, r, ownerSessionID) {
		return
	}
	var req fleetEnrollmentChallengeRequest
	if !s.readJSONBodyLimit(w, r, ownerSessionID, &req, 64<<10) {
		return
	}
	ownerID, err := s.db.FleetOwnerID(r.Context())
	if err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	privateKey, err := s.fleetHubKey(r.Context())
	if err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	now := time.Now().UTC()
	host := req.Host.Normalized()
	host.OwnerID = ownerID
	host.State = fleet.HostStatePending
	host.RegisteredAt = now
	host.LastSeenAt = time.Time{}
	host.RevokedAt = nil
	if err := host.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	challengeID, err := newFleetEnrollmentID()
	if err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	nonce, err := newFleetNonce()
	if err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	challenge := fleet.EnrollmentChallenge{
		Version:   fleet.ProtocolVersion,
		ID:        challengeID,
		OwnerID:   ownerID,
		Nonce:     nonce,
		HubPublic: base64.RawURLEncoding.EncodeToString(privateKey.Public().(ed25519.PublicKey)),
		ExpiresAt: now.Add(fleet.EnrollmentTTL).Truncate(time.Second),
	}
	if err := s.db.FleetEnrollmentIssue(r.Context(), challenge, host); err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(challenge)
}

func (s *Server) handleFleetEnrollmentProof(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.db == nil {
		http.Error(w, "fleet unavailable", http.StatusServiceUnavailable)
		return
	}
	var proof fleet.EnrollmentProof
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proof); err != nil || proof.Validate() != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	enrollment, ok, err := s.db.FleetEnrollmentGet(r.Context(), proof.ChallengeID)
	if err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "enrollment unavailable", http.StatusNotFound)
		return
	}
	privateKey, err := s.fleetHubKey(r.Context())
	if err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	challenge := enrollment.Challenge
	challenge.Nonce = proof.Nonce
	challenge.HubPublic = base64.RawURLEncoding.EncodeToString(privateKey.Public().(ed25519.PublicKey))
	publicKey, err := decodeEd25519Public(enrollment.Host.IdentityPublic)
	if err != nil {
		http.Error(w, "invalid host identity", http.StatusBadRequest)
		return
	}
	signature, err := base64.RawURLEncoding.DecodeString(proof.Signature)
	if err != nil || !ed25519.Verify(publicKey, fleet.EnrollmentSigningPayload(challenge, enrollment.Host), signature) {
		http.Error(w, "invalid enrollment proof", http.StatusUnauthorized)
		return
	}
	consumed, err := s.db.FleetEnrollmentConsume(r.Context(), proof.ChallengeID, proof.Nonce)
	if err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	if !consumed {
		http.Error(w, "enrollment unavailable", http.StatusConflict)
		return
	}
	host := enrollment.Host
	host.State = fleet.HostStateActive
	host.LastSeenAt = time.Now().UTC()
	if err := s.db.FleetHostUpsert(r.Context(), host); err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	hubProof := ed25519.Sign(privateKey, fleet.EnrollmentSigningPayload(challenge, host))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(fleetEnrollmentProofResponse{Host: host, HubProof: base64.RawURLEncoding.EncodeToString(hubProof)})
}

func (s *Server) handleFleetHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.db == nil {
		http.Error(w, "fleet unavailable", http.StatusServiceUnavailable)
		return
	}
	var heartbeat fleet.Heartbeat
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&heartbeat); err != nil || heartbeat.Validate() != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	now := time.Now().UTC()
	if skew := now.Sub(heartbeat.SentAt.UTC()); skew > fleetHeartbeatMaxSkew || skew < -fleetHeartbeatMaxSkew {
		http.Error(w, "heartbeat timestamp outside allowed skew", http.StatusBadRequest)
		return
	}
	host, ok, err := s.db.FleetHostGet(r.Context(), heartbeat.HostID)
	if err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	if !ok || host.State != fleet.HostStateActive {
		http.Error(w, "unknown host", http.StatusNotFound)
		return
	}
	publicKey, err := decodeEd25519Public(host.IdentityPublic)
	if err != nil {
		http.Error(w, "invalid host identity", http.StatusInternalServerError)
		return
	}
	signature, err := base64.RawURLEncoding.DecodeString(heartbeat.Signature)
	if err != nil || !ed25519.Verify(publicKey, fleet.HeartbeatSigningPayload(heartbeat), signature) {
		http.Error(w, "invalid heartbeat proof", http.StatusUnauthorized)
		return
	}
	updated, applied, err := s.db.FleetHostRecordHeartbeat(r.Context(), heartbeat)
	if err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	if !applied {
		http.Error(w, "stale heartbeat", http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"host": updated})
}

func (s *Server) fleetHubKey(ctx context.Context) (ed25519.PrivateKey, error) {
	if s.db == nil {
		return nil, errors.New("fleet unavailable")
	}
	if encoded, ok, err := s.db.KVGetEncryptedString(ctx, fleetHubPrivateKey); err != nil {
		return nil, err
	} else if ok {
		return decodeEd25519Private(encoded)
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(privateKey)
	inserted, err := s.db.KVSetEncryptedStringIfAbsent(ctx, fleetHubPrivateKey, encoded)
	if err != nil {
		return nil, err
	}
	if inserted {
		return privateKey, nil
	}
	encoded, ok, err := s.db.KVGetEncryptedString(ctx, fleetHubPrivateKey)
	if err != nil || !ok {
		return nil, errors.New("persisted fleet hub key missing")
	}
	return decodeEd25519Private(encoded)
}

func decodeEd25519Public(value string) (ed25519.PublicKey, error) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("invalid Ed25519 public key")
	}
	return ed25519.PublicKey(b), nil
}

func decodeEd25519Private(value string) (ed25519.PrivateKey, error) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(b) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid Ed25519 private key")
	}
	return ed25519.PrivateKey(b), nil
}

func newFleetEnrollmentID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "enroll-" + hex.EncodeToString(b), nil
}

func newFleetNonce() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
