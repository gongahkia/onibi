package fleet

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

const LinkChallengeTTL = 30 * time.Second

type LinkChallenge struct {
	Version   uint16    `json:"version"`
	ID        string    `json:"id"`
	Nonce     string    `json:"nonce"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (c LinkChallenge) Validate() error {
	if c.Version != ProtocolVersion {
		return fmt.Errorf("fleet link version %d is incompatible with %d", c.Version, ProtocolVersion)
	}
	if !validID(c.ID) || strings.TrimSpace(c.Nonce) == "" || c.ExpiresAt.IsZero() {
		return errors.New("invalid fleet link challenge")
	}
	return nil
}

type LinkAuthenticate struct {
	Version     uint16    `json:"version"`
	HostID      string    `json:"host_id"`
	ChallengeID string    `json:"challenge_id"`
	Nonce       string    `json:"nonce"`
	SentAt      time.Time `json:"sent_at"`
	Signature   string    `json:"signature"`
}

func (a LinkAuthenticate) Validate() error {
	if a.Version != ProtocolVersion {
		return fmt.Errorf("fleet link version %d is incompatible with %d", a.Version, ProtocolVersion)
	}
	if !validID(a.HostID) || !validID(a.ChallengeID) || strings.TrimSpace(a.Nonce) == "" || a.SentAt.IsZero() || strings.TrimSpace(a.Signature) == "" {
		return errors.New("invalid fleet link authentication")
	}
	return nil
}

type LinkAccepted struct {
	Version     uint16    `json:"version"`
	HostID      string    `json:"host_id"`
	ChallengeID string    `json:"challenge_id"`
	Nonce       string    `json:"nonce"`
	SentAt      time.Time `json:"sent_at"`
	Signature   string    `json:"signature"`
}

func (a LinkAccepted) Validate() error {
	if a.Version != ProtocolVersion {
		return fmt.Errorf("fleet link version %d is incompatible with %d", a.Version, ProtocolVersion)
	}
	if !validID(a.HostID) || !validID(a.ChallengeID) || strings.TrimSpace(a.Nonce) == "" || a.SentAt.IsZero() || strings.TrimSpace(a.Signature) == "" {
		return errors.New("invalid fleet link acceptance")
	}
	return nil
}

type LinkFrame struct {
	Envelope  Envelope `json:"envelope"`
	Signature string   `json:"signature"`
}

func (f LinkFrame) Validate() error {
	if err := f.Envelope.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(f.Signature) == "" {
		return errors.New("fleet link signature required")
	}
	return nil
}

type Control struct {
	Version   uint16    `json:"version"`
	ID        string    `json:"id"`
	HostID    string    `json:"host_id"`
	Command   string    `json:"command"`
	Payload   []byte    `json:"payload,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (c Control) Validate() error {
	if c.Version != ProtocolVersion {
		return fmt.Errorf("fleet control version %d is incompatible with %d", c.Version, ProtocolVersion)
	}
	if !validID(c.ID) || !validID(c.HostID) || strings.TrimSpace(c.Command) == "" || len(c.Command) > 128 || c.ExpiresAt.IsZero() {
		return errors.New("invalid fleet control")
	}
	return nil
}

func LinkAuthenticateSigningPayload(challenge LinkChallenge, auth LinkAuthenticate) []byte {
	return linkSigningPayload("onibi-fleet-link-auth-v1", challenge.ID, challenge.Nonce, challenge.ExpiresAt, auth.HostID, auth.SentAt)
}

func LinkAcceptedSigningPayload(challenge LinkChallenge, accepted LinkAccepted) []byte {
	return linkSigningPayload("onibi-fleet-link-accept-v1", challenge.ID, challenge.Nonce, challenge.ExpiresAt, accepted.HostID, accepted.SentAt)
}

func LinkFrameSigningPayload(envelope Envelope) []byte {
	return []byte(strings.Join([]string{
		"onibi-fleet-link-frame-v1",
		fmt.Sprintf("%d", envelope.Version),
		string(envelope.Type),
		envelope.RequestID,
		envelope.SentAt.UTC().Format(time.RFC3339Nano),
		base64.RawURLEncoding.EncodeToString(envelope.Body),
	}, "\n"))
}

func linkSigningPayload(domain, challengeID, nonce string, expiresAt time.Time, hostID string, sentAt time.Time) []byte {
	return []byte(strings.Join([]string{
		domain,
		challengeID,
		nonce,
		expiresAt.UTC().Format(time.RFC3339Nano),
		hostID,
		sentAt.UTC().Format(time.RFC3339Nano),
	}, "\n"))
}
