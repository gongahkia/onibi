package web

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/gongahkia/onibi/internal/fleet"
)

const (
	fleetLinkSubprotocol  = "onibi.fleet.link.v1"
	fleetLinkAuthTimeout  = 10 * time.Second
	fleetLinkMaxFrameSize = 128 << 10
)

type fleetLinkConnection struct {
	hostID         string
	identityPublic string
	conn           *websocket.Conn
	mu             sync.Mutex
}

func (c *fleetLinkConnection) write(ctx context.Context, value any) error {
	writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	return wsjson.Write(writeCtx, c.conn, value)
}

func (s *Server) handleFleetLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.db == nil {
		http.Error(w, "fleet unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.TLS == nil {
		http.Error(w, "fleet link requires TLS", http.StatusUpgradeRequired)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{fleetLinkSubprotocol}})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	if conn.Subprotocol() != fleetLinkSubprotocol {
		_ = conn.Close(websocket.StatusPolicyViolation, "fleet link subprotocol required")
		return
	}
	conn.SetReadLimit(fleetLinkMaxFrameSize)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	challenge, err := newFleetLinkChallenge()
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "fleet link unavailable")
		return
	}
	if err := writeFleetLink(ctx, conn, challenge); err != nil {
		return
	}
	authCtx, authCancel := context.WithTimeout(ctx, fleetLinkAuthTimeout)
	defer authCancel()
	var auth fleet.LinkAuthenticate
	if err := wsjson.Read(authCtx, conn, &auth); err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "fleet link authentication required")
		return
	}
	if err := validateFleetLinkAuthentication(challenge, auth); err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "invalid fleet link authentication")
		return
	}
	host, ok, err := s.db.FleetHostGet(ctx, auth.HostID)
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "fleet link unavailable")
		return
	}
	if !ok || host.State != fleet.HostStateActive {
		_ = conn.Close(websocket.StatusPolicyViolation, "unknown fleet host")
		return
	}
	public, err := decodeEd25519Public(host.IdentityPublic)
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "invalid fleet host identity")
		return
	}
	signature, err := base64.RawURLEncoding.DecodeString(auth.Signature)
	if err != nil || !ed25519.Verify(public, fleet.LinkAuthenticateSigningPayload(challenge, auth), signature) {
		_ = conn.Close(websocket.StatusPolicyViolation, "invalid fleet link proof")
		return
	}
	private, err := s.fleetHubKey(ctx)
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "fleet link unavailable")
		return
	}
	accepted := fleet.LinkAccepted{
		Version:     fleet.ProtocolVersion,
		HostID:      host.ID,
		ChallengeID: challenge.ID,
		Nonce:       challenge.Nonce,
		SentAt:      time.Now().UTC(),
	}
	accepted.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.LinkAcceptedSigningPayload(challenge, accepted)))
	if err := writeFleetLink(ctx, conn, accepted); err != nil {
		return
	}
	link := &fleetLinkConnection{hostID: host.ID, identityPublic: host.IdentityPublic, conn: conn}
	previous := s.setFleetLink(link)
	if previous != nil {
		_ = previous.conn.Close(websocket.StatusPolicyViolation, "replaced by newer fleet link")
	}
	defer s.removeFleetLink(link)
	for {
		var frame fleet.LinkFrame
		if err := wsjson.Read(ctx, conn, &frame); err != nil {
			return
		}
		if err := s.applyFleetLinkFrame(ctx, host.ID, frame); err != nil {
			_ = conn.Close(websocket.StatusPolicyViolation, "invalid fleet link frame")
			return
		}
	}
}

func (s *Server) SendFleetControl(ctx context.Context, control fleet.Control) error {
	if s.db == nil {
		return errors.New("fleet unavailable")
	}
	if err := control.Validate(); err != nil {
		return err
	}
	if !control.ExpiresAt.After(time.Now().UTC()) {
		return errors.New("fleet control expired")
	}
	host, ok, err := s.db.FleetHostGet(ctx, control.HostID)
	if err != nil {
		return err
	}
	if !ok || host.State != fleet.HostStateActive {
		return errors.New("fleet host unavailable")
	}
	s.fleetLinkMu.Lock()
	link := s.fleetLinks[control.HostID]
	s.fleetLinkMu.Unlock()
	if link == nil {
		return errors.New("fleet host link unavailable")
	}
	if link.identityPublic != host.IdentityPublic {
		s.closeFleetLink(control.HostID)
		return errors.New("fleet host identity changed")
	}
	body, err := json.Marshal(control)
	if err != nil {
		return err
	}
	requestID, err := newFleetLinkID()
	if err != nil {
		return err
	}
	envelope := fleet.Envelope{Version: fleet.ProtocolVersion, Type: fleet.MessageControl, RequestID: requestID, SentAt: time.Now().UTC(), Body: body}
	private, err := s.fleetHubKey(ctx)
	if err != nil {
		return err
	}
	frame := fleet.LinkFrame{Envelope: envelope, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.LinkFrameSigningPayload(envelope)))}
	return link.write(ctx, frame)
}

func (s *Server) applyFleetLinkFrame(ctx context.Context, hostID string, frame fleet.LinkFrame) error {
	if err := frame.Validate(); err != nil {
		return err
	}
	if frame.Envelope.Type != fleet.MessageHeartbeat {
		return errors.New("unsupported fleet link message")
	}
	host, ok, err := s.db.FleetHostGet(ctx, hostID)
	if err != nil {
		return err
	}
	if !ok || host.State != fleet.HostStateActive {
		return errors.New("fleet host unavailable")
	}
	public, err := decodeEd25519Public(host.IdentityPublic)
	if err != nil {
		return err
	}
	signature, err := base64.RawURLEncoding.DecodeString(frame.Signature)
	if err != nil || !ed25519.Verify(public, fleet.LinkFrameSigningPayload(frame.Envelope), signature) {
		return errors.New("invalid fleet link signature")
	}
	var heartbeat fleet.Heartbeat
	if err := json.Unmarshal(frame.Envelope.Body, &heartbeat); err != nil {
		return err
	}
	if err := heartbeat.Validate(); err != nil {
		return err
	}
	if heartbeat.HostID != hostID || !heartbeat.SentAt.Equal(frame.Envelope.SentAt) {
		return errors.New("fleet heartbeat link mismatch")
	}
	now := time.Now().UTC()
	if skew := now.Sub(heartbeat.SentAt.UTC()); skew > fleetHeartbeatMaxSkew || skew < -fleetHeartbeatMaxSkew {
		return errors.New("fleet heartbeat timestamp outside allowed skew")
	}
	heartbeatSignature, err := base64.RawURLEncoding.DecodeString(heartbeat.Signature)
	if err != nil || !ed25519.Verify(public, fleet.HeartbeatSigningPayload(heartbeat), heartbeatSignature) {
		return errors.New("invalid fleet heartbeat proof")
	}
	_, applied, err := s.db.FleetHostRecordHeartbeat(ctx, heartbeat)
	if err != nil {
		return err
	}
	if !applied {
		return errors.New("stale fleet heartbeat")
	}
	return nil
}

func (s *Server) setFleetLink(link *fleetLinkConnection) *fleetLinkConnection {
	s.fleetLinkMu.Lock()
	defer s.fleetLinkMu.Unlock()
	previous := s.fleetLinks[link.hostID]
	s.fleetLinks[link.hostID] = link
	return previous
}

func (s *Server) removeFleetLink(link *fleetLinkConnection) {
	s.fleetLinkMu.Lock()
	defer s.fleetLinkMu.Unlock()
	if s.fleetLinks[link.hostID] == link {
		delete(s.fleetLinks, link.hostID)
	}
}

func (s *Server) closeFleetLink(hostID string) {
	s.fleetLinkMu.Lock()
	link := s.fleetLinks[hostID]
	delete(s.fleetLinks, hostID)
	s.fleetLinkMu.Unlock()
	if link != nil {
		_ = link.conn.Close(websocket.StatusPolicyViolation, "fleet host identity changed")
	}
}

func validateFleetLinkAuthentication(challenge fleet.LinkChallenge, auth fleet.LinkAuthenticate) error {
	if err := challenge.Validate(); err != nil {
		return err
	}
	if err := auth.Validate(); err != nil {
		return err
	}
	if auth.ChallengeID != challenge.ID || len(auth.Nonce) != len(challenge.Nonce) || subtle.ConstantTimeCompare([]byte(auth.Nonce), []byte(challenge.Nonce)) != 1 {
		return errors.New("fleet link challenge mismatch")
	}
	if !challenge.ExpiresAt.After(time.Now().UTC()) {
		return errors.New("fleet link challenge expired")
	}
	if skew := time.Now().UTC().Sub(auth.SentAt.UTC()); skew > fleetHeartbeatMaxSkew || skew < -fleetHeartbeatMaxSkew {
		return errors.New("fleet link authentication timestamp outside allowed skew")
	}
	return nil
}

func newFleetLinkChallenge() (fleet.LinkChallenge, error) {
	id, err := newFleetLinkID()
	if err != nil {
		return fleet.LinkChallenge{}, err
	}
	nonce, err := newFleetNonce()
	if err != nil {
		return fleet.LinkChallenge{}, err
	}
	return fleet.LinkChallenge{Version: fleet.ProtocolVersion, ID: id, Nonce: nonce, ExpiresAt: time.Now().UTC().Add(fleet.LinkChallengeTTL).Truncate(time.Second)}, nil
}

func newFleetLinkID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "link-" + hex.EncodeToString(b), nil
}

func writeFleetLink(ctx context.Context, conn *websocket.Conn, value any) error {
	writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	return wsjson.Write(writeCtx, conn, value)
}
