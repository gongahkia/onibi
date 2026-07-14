// Package fleet defines the versioned personal-fleet boundary shared by a hub,
// enrolled hosts, and the phone cockpit.
package fleet

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const ProtocolVersion uint16 = 1

const (
	EnrollmentTTL  = 10 * time.Minute
	HostStaleAfter = 2 * time.Minute
)

type MessageType string

const (
	MessageEnrollmentChallenge  MessageType = "enrollment.challenge"
	MessageEnrollmentProof      MessageType = "enrollment.proof"
	MessageKeyRotationChallenge MessageType = "identity.rotate.challenge"
	MessageKeyRotationProof     MessageType = "identity.rotate.proof"
	MessageHeartbeat            MessageType = "host.heartbeat"
	MessageFleetSnapshot        MessageType = "fleet.snapshot"
	MessageControl              MessageType = "fleet.control"
	MessageRevoke               MessageType = "host.revoke"
)

type Envelope struct {
	Version   uint16      `json:"version"`
	Type      MessageType `json:"type"`
	RequestID string      `json:"request_id"`
	SentAt    time.Time   `json:"sent_at"`
	Body      []byte      `json:"body"`
}

func (e Envelope) Validate() error {
	if e.Version != ProtocolVersion {
		return fmt.Errorf("fleet protocol version %d is incompatible with %d", e.Version, ProtocolVersion)
	}
	if !validMessageType(e.Type) {
		return fmt.Errorf("unsupported fleet message type %q", e.Type)
	}
	if !validID(e.RequestID) {
		return errors.New("fleet request_id must be 3-64 lowercase alphanumeric, hyphen, or underscore characters")
	}
	if e.SentAt.IsZero() {
		return errors.New("fleet sent_at required")
	}
	if len(e.Body) == 0 {
		return errors.New("fleet body required")
	}
	return nil
}

type HostState string

const (
	HostStatePending HostState = "pending"
	HostStateActive  HostState = "active"
	HostStateStale   HostState = "stale"
	HostStateRevoked HostState = "revoked"
)

type EndpointKind string

const (
	EndpointMesh  EndpointKind = "mesh"
	EndpointSSH   EndpointKind = "ssh"
	EndpointRelay EndpointKind = "relay"
)

type Endpoint struct {
	Kind EndpointKind `json:"kind"`
	URL  string       `json:"url"`
}

type EnrollmentChallenge struct {
	Version   uint16    `json:"version"`
	ID        string    `json:"id"`
	OwnerID   string    `json:"owner_id"`
	Nonce     string    `json:"nonce"`
	HubPublic string    `json:"hub_public"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (c EnrollmentChallenge) Validate() error {
	if c.Version != ProtocolVersion {
		return fmt.Errorf("fleet enrollment version %d is incompatible with %d", c.Version, ProtocolVersion)
	}
	if !validID(c.ID) || !validID(c.OwnerID) || strings.TrimSpace(c.Nonce) == "" || strings.TrimSpace(c.HubPublic) == "" || c.ExpiresAt.IsZero() {
		return errors.New("invalid fleet enrollment challenge")
	}
	return nil
}

type EnrollmentProof struct {
	Version     uint16 `json:"version"`
	ChallengeID string `json:"challenge_id"`
	Nonce       string `json:"nonce"`
	Signature   string `json:"signature"`
}

type KeyRotationRequest struct {
	Version           uint16 `json:"version"`
	HostID            string `json:"host_id"`
	NewIdentityPublic string `json:"new_identity_public"`
}

type RevocationRequest struct {
	Version uint16 `json:"version"`
	HostID  string `json:"host_id"`
}

func (r RevocationRequest) Validate() error {
	if r.Version != ProtocolVersion {
		return fmt.Errorf("fleet revocation version %d is incompatible with %d", r.Version, ProtocolVersion)
	}
	if !validID(r.HostID) {
		return errors.New("invalid fleet revocation request")
	}
	return nil
}

func (r KeyRotationRequest) Validate() error {
	if r.Version != ProtocolVersion {
		return fmt.Errorf("fleet key rotation version %d is incompatible with %d", r.Version, ProtocolVersion)
	}
	if !validID(r.HostID) || !validIdentityPublic(r.NewIdentityPublic) {
		return errors.New("invalid fleet key rotation request")
	}
	return nil
}

type KeyRotationChallenge struct {
	Version               uint16    `json:"version"`
	ID                    string    `json:"id"`
	OwnerID               string    `json:"owner_id"`
	HostID                string    `json:"host_id"`
	CurrentIdentityPublic string    `json:"current_identity_public"`
	NewIdentityPublic     string    `json:"new_identity_public"`
	Nonce                 string    `json:"nonce"`
	HubPublic             string    `json:"hub_public"`
	ExpiresAt             time.Time `json:"expires_at"`
}

func (c KeyRotationChallenge) Validate() error {
	if c.Version != ProtocolVersion {
		return fmt.Errorf("fleet key rotation version %d is incompatible with %d", c.Version, ProtocolVersion)
	}
	if !validID(c.ID) || !validID(c.OwnerID) || !validID(c.HostID) || !validIdentityPublic(c.CurrentIdentityPublic) || !validIdentityPublic(c.NewIdentityPublic) || strings.TrimSpace(c.Nonce) == "" || strings.TrimSpace(c.HubPublic) == "" || c.ExpiresAt.IsZero() || c.CurrentIdentityPublic == c.NewIdentityPublic {
		return errors.New("invalid fleet key rotation challenge")
	}
	return nil
}

type KeyRotationProof struct {
	Version          uint16 `json:"version"`
	ChallengeID      string `json:"challenge_id"`
	Nonce            string `json:"nonce"`
	CurrentSignature string `json:"current_signature"`
	NewSignature     string `json:"new_signature"`
}

func (p KeyRotationProof) Validate() error {
	if p.Version != ProtocolVersion {
		return fmt.Errorf("fleet key rotation version %d is incompatible with %d", p.Version, ProtocolVersion)
	}
	if !validID(p.ChallengeID) || strings.TrimSpace(p.Nonce) == "" || strings.TrimSpace(p.CurrentSignature) == "" || strings.TrimSpace(p.NewSignature) == "" {
		return errors.New("invalid fleet key rotation proof")
	}
	return nil
}

func KeyRotationSigningPayload(challenge KeyRotationChallenge) []byte {
	return []byte(strings.Join([]string{
		"onibi-fleet-key-rotation-v1",
		challenge.ID,
		challenge.Nonce,
		challenge.ExpiresAt.UTC().Format(time.RFC3339Nano),
		challenge.OwnerID,
		challenge.HostID,
		challenge.CurrentIdentityPublic,
		challenge.NewIdentityPublic,
	}, "\n"))
}

type Heartbeat struct {
	Version       uint16    `json:"version"`
	OwnerID       string    `json:"owner_id"`
	HostID        string    `json:"host_id"`
	SentAt        time.Time `json:"sent_at"`
	BinaryVersion string    `json:"binary_version"`
	Capabilities  []string  `json:"capabilities"`
	Signature     string    `json:"signature"`
}

func (h Heartbeat) Validate() error {
	if h.Version != ProtocolVersion {
		return fmt.Errorf("fleet heartbeat version %d is incompatible with %d", h.Version, ProtocolVersion)
	}
	if !validID(h.OwnerID) || !validID(h.HostID) || h.SentAt.IsZero() || strings.TrimSpace(h.BinaryVersion) == "" || strings.TrimSpace(h.Signature) == "" {
		return errors.New("invalid fleet heartbeat")
	}
	for _, capability := range h.Capabilities {
		if !validCapability(strings.ToLower(strings.TrimSpace(capability))) {
			return fmt.Errorf("invalid fleet capability %q", capability)
		}
	}
	return nil
}

func HeartbeatSigningPayload(heartbeat Heartbeat) []byte {
	capabilities := normalizedCapabilities(heartbeat.Capabilities)
	return []byte(strings.Join([]string{
		"onibi-fleet-heartbeat-v1",
		heartbeat.OwnerID,
		heartbeat.HostID,
		heartbeat.SentAt.UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(heartbeat.BinaryVersion),
		strings.Join(capabilities, ","),
	}, "\n"))
}

func (p EnrollmentProof) Validate() error {
	if p.Version != ProtocolVersion {
		return fmt.Errorf("fleet enrollment version %d is incompatible with %d", p.Version, ProtocolVersion)
	}
	if !validID(p.ChallengeID) || strings.TrimSpace(p.Nonce) == "" || strings.TrimSpace(p.Signature) == "" {
		return errors.New("invalid fleet enrollment proof")
	}
	return nil
}

func EnrollmentSigningPayload(challenge EnrollmentChallenge, host Host) []byte {
	return []byte(strings.Join([]string{
		"onibi-fleet-enrollment-v1",
		challenge.ID,
		challenge.Nonce,
		challenge.ExpiresAt.UTC().Format(time.RFC3339Nano),
		challenge.OwnerID,
		host.ID,
		host.OwnerID,
		host.IdentityPublic,
	}, "\n"))
}

func (e Endpoint) Validate() error {
	switch e.Kind {
	case EndpointMesh:
		if err := validateHTTPSEndpoint(e.URL, false); err != nil {
			return fmt.Errorf("invalid %s endpoint", e.Kind)
		}
	case EndpointRelay:
		if err := validateHTTPSEndpoint(e.URL, true); err != nil {
			return fmt.Errorf("invalid %s endpoint", e.Kind)
		}
	case EndpointSSH:
		if err := validateSSHEndpoint(e.URL); err != nil {
			return errors.New("invalid ssh endpoint")
		}
	default:
		return fmt.Errorf("unsupported endpoint kind %q", e.Kind)
	}
	return nil
}

func validateHTTPSEndpoint(raw string, requirePublicHost bool) error {
	if raw != strings.TrimSpace(raw) || len(raw) == 0 || len(raw) > 2048 {
		return errors.New("invalid endpoint URL")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid endpoint URL")
	}
	if port := u.Port(); port != "" {
		v, err := strconv.Atoi(port)
		if err != nil || v < 1 || v > 65535 {
			return errors.New("invalid endpoint port")
		}
	}
	host := strings.ToLower(u.Hostname())
	if !validEndpointHost(host, requirePublicHost) {
		return errors.New("invalid endpoint host")
	}
	return nil
}

func validEndpointHost(host string, requirePublic bool) bool {
	if host == "" || strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".local") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return false
		}
		return !requirePublic || (!ip.IsPrivate() && !isCarrierGradeNAT(ip))
	}
	if len(host) > 253 || (requirePublic && !strings.Contains(host, ".")) {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

func isCarrierGradeNAT(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 64
}

func validateSSHEndpoint(raw string) error {
	if raw != strings.TrimSpace(raw) || len(raw) == 0 || len(raw) > 512 || strings.ContainsAny(raw, "\r\n\t /?#!") || strings.Count(raw, "@") != 1 {
		return errors.New("invalid ssh endpoint")
	}
	user, target, _ := strings.Cut(raw, "@")
	if !validSSHUser(user) {
		return errors.New("invalid ssh user")
	}
	host := target
	if parsedHost, port, err := net.SplitHostPort(target); err == nil {
		host = parsedHost
		v, err := strconv.Atoi(port)
		if err != nil || v < 1 || v > 65535 {
			return errors.New("invalid ssh port")
		}
	} else if strings.Contains(target, ":") || strings.ContainsAny(target, "[]") {
		return errors.New("invalid ssh host")
	}
	if !validEndpointHost(strings.ToLower(host), false) {
		return errors.New("invalid ssh host")
	}
	return nil
}

func validSSHUser(user string) bool {
	if len(user) == 0 || len(user) > 64 {
		return false
	}
	for _, r := range user {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

type Host struct {
	ID              string     `json:"id"`
	OwnerID         string     `json:"owner_id"`
	DisplayName     string     `json:"display_name"`
	IdentityPublic  string     `json:"identity_public"`
	Endpoint        Endpoint   `json:"endpoint"`
	ProtocolVersion uint16     `json:"protocol_version"`
	BinaryVersion   string     `json:"binary_version"`
	Capabilities    []string   `json:"capabilities"`
	State           HostState  `json:"state"`
	RegisteredAt    time.Time  `json:"registered_at"`
	LastSeenAt      time.Time  `json:"last_seen_at,omitempty"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
}

func (h Host) Validate() error {
	if !validID(h.ID) {
		return errors.New("fleet host id must be 3-64 lowercase alphanumeric, hyphen, or underscore characters")
	}
	if !validID(h.OwnerID) {
		return errors.New("fleet owner id must be 3-64 lowercase alphanumeric, hyphen, or underscore characters")
	}
	if strings.TrimSpace(h.DisplayName) == "" || len(h.DisplayName) > 128 {
		return errors.New("fleet host display_name must be 1-128 characters")
	}
	if strings.TrimSpace(h.IdentityPublic) == "" || len(h.IdentityPublic) > 4096 {
		return errors.New("fleet host identity_public required")
	}
	if err := h.Endpoint.Validate(); err != nil {
		return err
	}
	if h.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("fleet host protocol version %d is incompatible with %d", h.ProtocolVersion, ProtocolVersion)
	}
	if strings.TrimSpace(h.BinaryVersion) == "" || len(h.BinaryVersion) > 128 {
		return errors.New("fleet host binary_version required")
	}
	if !validHostState(h.State) {
		return fmt.Errorf("invalid fleet host state %q", h.State)
	}
	if h.RegisteredAt.IsZero() {
		return errors.New("fleet host registered_at required")
	}
	if h.State == HostStateRevoked && h.RevokedAt == nil {
		return errors.New("revoked fleet host requires revoked_at")
	}
	if h.State != HostStateRevoked && h.RevokedAt != nil {
		return errors.New("only revoked fleet host may have revoked_at")
	}
	if h.LastSeenAt.Before(h.RegisteredAt) && !h.LastSeenAt.IsZero() {
		return errors.New("fleet host last_seen_at precedes registered_at")
	}
	for _, capability := range h.Capabilities {
		if !validCapability(capability) {
			return fmt.Errorf("invalid fleet capability %q", capability)
		}
	}
	return nil
}

func (h Host) Normalized() Host {
	h.DisplayName = strings.TrimSpace(h.DisplayName)
	h.IdentityPublic = strings.TrimSpace(h.IdentityPublic)
	h.Endpoint.URL = strings.TrimSpace(h.Endpoint.URL)
	h.BinaryVersion = strings.TrimSpace(h.BinaryVersion)
	h.Capabilities = normalizedCapabilities(h.Capabilities)
	h.RegisteredAt = h.RegisteredAt.UTC()
	h.LastSeenAt = h.LastSeenAt.UTC()
	if h.RevokedAt != nil {
		v := h.RevokedAt.UTC()
		h.RevokedAt = &v
	}
	return h
}

type Session struct {
	ID            string    `json:"id"`
	HostID        string    `json:"host_id"`
	Agent         string    `json:"agent"`
	State         string    `json:"state"`
	LastActivity  time.Time `json:"last_activity"`
	ApprovalCount int       `json:"approval_count"`
}

type SessionRecoveryState string

const (
	SessionRecoveryHealthy      SessionRecoveryState = "healthy"
	SessionRecoveryReconnecting SessionRecoveryState = "reconnecting"
	SessionRecoveryRecovering   SessionRecoveryState = "recovering"
	SessionRecoveryOrphaned     SessionRecoveryState = "orphaned"
	SessionRecoveryFailed       SessionRecoveryState = "failed"
	SessionRecoveryTerminated   SessionRecoveryState = "terminated"
)

func (s SessionRecoveryState) Valid() bool {
	switch s {
	case SessionRecoveryHealthy, SessionRecoveryReconnecting, SessionRecoveryRecovering, SessionRecoveryOrphaned, SessionRecoveryFailed, SessionRecoveryTerminated:
		return true
	default:
		return false
	}
}

type Approval struct {
	ID        string    `json:"id"`
	HostID    string    `json:"host_id"`
	SessionID string    `json:"session_id"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Snapshot struct {
	Version     uint16     `json:"version"`
	GeneratedAt time.Time  `json:"generated_at"`
	Hosts       []Host     `json:"hosts"`
	Sessions    []Session  `json:"sessions"`
	Approvals   []Approval `json:"approvals"`
}

func (s Snapshot) Validate() error {
	if s.Version != ProtocolVersion {
		return fmt.Errorf("fleet snapshot version %d is incompatible with %d", s.Version, ProtocolVersion)
	}
	if s.GeneratedAt.IsZero() {
		return errors.New("fleet snapshot generated_at required")
	}
	hosts := make(map[string]Host, len(s.Hosts))
	for _, host := range s.Hosts {
		host = host.Normalized()
		if err := host.Validate(); err != nil {
			return err
		}
		if _, exists := hosts[host.ID]; exists {
			return fmt.Errorf("duplicate fleet host %q", host.ID)
		}
		hosts[host.ID] = host
	}
	for _, session := range s.Sessions {
		if !validID(session.ID) || !validID(session.HostID) || strings.TrimSpace(session.Agent) == "" || session.LastActivity.IsZero() || session.ApprovalCount < 0 {
			return errors.New("invalid fleet session")
		}
		if _, exists := hosts[session.HostID]; !exists {
			return fmt.Errorf("fleet session %q references unknown host %q", session.ID, session.HostID)
		}
	}
	for _, approval := range s.Approvals {
		if !validID(approval.ID) || !validID(approval.HostID) || !validID(approval.SessionID) || approval.CreatedAt.IsZero() || approval.ExpiresAt.Before(approval.CreatedAt) {
			return errors.New("invalid fleet approval")
		}
		if _, exists := hosts[approval.HostID]; !exists {
			return fmt.Errorf("fleet approval %q references unknown host %q", approval.ID, approval.HostID)
		}
	}
	return nil
}

func validMessageType(v MessageType) bool {
	switch v {
	case MessageEnrollmentChallenge, MessageEnrollmentProof, MessageKeyRotationChallenge, MessageKeyRotationProof, MessageHeartbeat, MessageFleetSnapshot, MessageControl, MessageRevoke:
		return true
	default:
		return false
	}
}

func validIdentityPublic(v string) bool {
	v = strings.TrimSpace(v)
	return v != "" && len(v) <= 4096
}

func validHostState(v HostState) bool {
	switch v {
	case HostStatePending, HostStateActive, HostStateStale, HostStateRevoked:
		return true
	default:
		return false
	}
}

func validID(v string) bool {
	if len(v) < 3 || len(v) > 64 {
		return false
	}
	for i, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			if i == 0 && (r == '-' || r == '_') {
				return false
			}
			continue
		}
		return false
	}
	return true
}

func validCapability(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > 64 {
		return false
	}
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func normalizedCapabilities(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
