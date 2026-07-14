package fleetnode

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
	"github.com/gongahkia/onibi/internal/store"
)

const (
	identityKey = "fleet_node_identity_v1"
	linkKey     = "fleet_node_link_v1"
)

type Identity struct {
	HostID     string `json:"host_id"`
	PrivateKey string `json:"private_key"`
}

type LinkConfig struct {
	HubURL        string   `json:"hub_url"`
	OwnerID       string   `json:"owner_id"`
	HostID        string   `json:"host_id"`
	HubPublic     string   `json:"hub_public"`
	BinaryVersion string   `json:"binary_version"`
	Capabilities  []string `json:"capabilities"`
}

type Enrollment struct {
	HubURL    string                    `json:"hub_url"`
	Challenge fleet.EnrollmentChallenge `json:"challenge"`
	Host      fleet.Host                `json:"host"`
	HubProof  string                    `json:"hub_proof"`
}

func LoadOrCreateIdentity(ctx context.Context, db *store.DB) (Identity, error) {
	if db == nil {
		return Identity{}, errors.New("fleet node store required")
	}
	raw, found, err := db.KVGetEncryptedString(ctx, identityKey)
	if err != nil {
		return Identity{}, err
	}
	if found {
		return parseIdentity(raw)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Identity{}, err
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return Identity{}, err
	}
	identity := Identity{HostID: "host-" + hex.EncodeToString(id), PrivateKey: base64.RawURLEncoding.EncodeToString(private)}
	if _, err := identity.PublicKey(); err != nil {
		return Identity{}, err
	}
	if len(public) != ed25519.PublicKeySize {
		return Identity{}, errors.New("invalid generated fleet node identity")
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return Identity{}, err
	}
	inserted, err := db.KVSetEncryptedStringIfAbsent(ctx, identityKey, string(encoded))
	if err != nil {
		return Identity{}, err
	}
	if inserted {
		return identity, nil
	}
	raw, found, err = db.KVGetEncryptedString(ctx, identityKey)
	if err != nil || !found {
		if err != nil {
			return Identity{}, err
		}
		return Identity{}, errors.New("fleet node identity missing after creation")
	}
	return parseIdentity(raw)
}

func LoadIdentity(ctx context.Context, db *store.DB) (Identity, bool, error) {
	if db == nil {
		return Identity{}, false, errors.New("fleet node store required")
	}
	raw, found, err := db.KVGetEncryptedString(ctx, identityKey)
	if err != nil || !found {
		return Identity{}, found, err
	}
	identity, err := parseIdentity(raw)
	return identity, true, err
}

func (i Identity) PublicKey() (ed25519.PublicKey, error) {
	if strings.TrimSpace(i.HostID) == "" || !strings.HasPrefix(i.HostID, "host-") || len(i.HostID) != len("host-")+32 {
		return nil, errors.New("invalid fleet node host id")
	}
	private, err := i.PrivateKeyBytes()
	if err != nil {
		return nil, err
	}
	return private.Public().(ed25519.PublicKey), nil
}

func (i Identity) PrivateKeyBytes() (ed25519.PrivateKey, error) {
	private, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(i.PrivateKey))
	if err != nil || len(private) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid fleet node private key")
	}
	return ed25519.PrivateKey(private), nil
}

func (i Identity) Sign(challenge fleet.EnrollmentChallenge, host fleet.Host) (fleet.EnrollmentProof, error) {
	if err := challenge.Validate(); err != nil {
		return fleet.EnrollmentProof{}, err
	}
	public, err := i.PublicKey()
	if err != nil {
		return fleet.EnrollmentProof{}, err
	}
	host = host.Normalized()
	if err := host.Validate(); err != nil || host.ID != i.HostID || host.IdentityPublic != base64.RawURLEncoding.EncodeToString(public) || host.OwnerID != challenge.OwnerID || host.State != fleet.HostStatePending {
		return fleet.EnrollmentProof{}, errors.New("fleet node enrollment host mismatch")
	}
	private, _ := i.PrivateKeyBytes()
	return fleet.EnrollmentProof{Version: fleet.ProtocolVersion, ChallengeID: challenge.ID, Nonce: challenge.Nonce, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.EnrollmentSigningPayload(challenge, host)))}, nil
}

func (i Identity) Host(displayName string, endpoint fleet.Endpoint, binaryVersion string, capabilities []string) (fleet.Host, error) {
	public, err := i.PublicKey()
	if err != nil {
		return fleet.Host{}, err
	}
	host := fleet.Host{ID: i.HostID, OwnerID: "owner-pending", DisplayName: strings.TrimSpace(displayName), IdentityPublic: base64.RawURLEncoding.EncodeToString(public), Endpoint: endpoint, ProtocolVersion: fleet.ProtocolVersion, BinaryVersion: strings.TrimSpace(binaryVersion), Capabilities: capabilities, State: fleet.HostStatePending}
	check := host
	check.RegisteredAt = time.Unix(1, 0).UTC()
	if err := check.Validate(); err != nil {
		return fleet.Host{}, err
	}
	return host, nil
}

func Configure(ctx context.Context, db *store.DB, identity Identity, enrollment Enrollment) (LinkConfig, error) {
	if db == nil {
		return LinkConfig{}, errors.New("fleet node store required")
	}
	config, err := enrollment.Validate(identity)
	if err != nil {
		return LinkConfig{}, err
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return LinkConfig{}, err
	}
	if err := db.KVSetEncryptedString(ctx, linkKey, string(encoded)); err != nil {
		return LinkConfig{}, err
	}
	return config, nil
}

func LoadConfig(ctx context.Context, db *store.DB) (LinkConfig, bool, error) {
	if db == nil {
		return LinkConfig{}, false, errors.New("fleet node store required")
	}
	raw, found, err := db.KVGetEncryptedString(ctx, linkKey)
	if err != nil || !found {
		return LinkConfig{}, found, err
	}
	var config LinkConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return LinkConfig{}, true, err
	}
	if err := config.Validate(); err != nil {
		return LinkConfig{}, true, err
	}
	return config, true, nil
}

func (e Enrollment) Validate(identity Identity) (LinkConfig, error) {
	if err := e.Challenge.Validate(); err != nil {
		return LinkConfig{}, err
	}
	if err := validHubURL(e.HubURL); err != nil {
		return LinkConfig{}, err
	}
	public, err := identity.PublicKey()
	if err != nil {
		return LinkConfig{}, err
	}
	host := e.Host.Normalized()
	if err := host.Validate(); err != nil || host.ID != identity.HostID || host.IdentityPublic != base64.RawURLEncoding.EncodeToString(public) || host.OwnerID != e.Challenge.OwnerID || host.State != fleet.HostStateActive {
		return LinkConfig{}, errors.New("fleet node enrollment host mismatch")
	}
	hubPublic, err := base64.RawURLEncoding.DecodeString(e.Challenge.HubPublic)
	if err != nil || len(hubPublic) != ed25519.PublicKeySize {
		return LinkConfig{}, errors.New("invalid fleet hub public key")
	}
	proof, err := base64.RawURLEncoding.DecodeString(e.HubProof)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(hubPublic), fleet.EnrollmentSigningPayload(e.Challenge, host), proof) {
		return LinkConfig{}, errors.New("invalid fleet hub enrollment proof")
	}
	config := LinkConfig{HubURL: strings.TrimSpace(e.HubURL), OwnerID: host.OwnerID, HostID: host.ID, HubPublic: base64.RawURLEncoding.EncodeToString(hubPublic), BinaryVersion: host.BinaryVersion, Capabilities: host.Capabilities}
	if err := config.Validate(); err != nil {
		return LinkConfig{}, err
	}
	return config, nil
}

func (c LinkConfig) Validate() error {
	if err := validHubURL(c.HubURL); err != nil {
		return err
	}
	hubPublic, err := base64.RawURLEncoding.DecodeString(c.HubPublic)
	if err != nil || len(hubPublic) != ed25519.PublicKeySize {
		return errors.New("invalid fleet node hub public key")
	}
	return (fleet.Heartbeat{Version: fleet.ProtocolVersion, OwnerID: c.OwnerID, HostID: c.HostID, SentAt: time.Unix(1, 0), BinaryVersion: c.BinaryVersion, Capabilities: c.Capabilities, Signature: "configured"}).Validate()
}

func parseIdentity(raw string) (Identity, error) {
	var identity Identity
	if err := json.Unmarshal([]byte(raw), &identity); err != nil {
		return Identity{}, err
	}
	if _, err := identity.PublicKey(); err != nil {
		return Identity{}, err
	}
	return identity, nil
}

func validHubURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || (u.Path != "" && u.Path != "/") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("invalid fleet hub URL")
	}
	return nil
}
