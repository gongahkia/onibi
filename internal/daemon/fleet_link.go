package daemon

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
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/gongahkia/onibi/internal/fleet"
)

const (
	defaultFleetLinkHeartbeatInterval = 30 * time.Second
	defaultFleetLinkReconnectMin      = 250 * time.Millisecond
	defaultFleetLinkReconnectMax      = 10 * time.Second
	fleetLinkHandshakeTimeout         = 10 * time.Second
	fleetLinkMaxSkew                  = 2 * time.Minute
	fleetLinkMaxFrameSize             = 128 << 10
	fleetLinkSubprotocol              = "onibi.fleet.link.v1"
)

type FleetLinkDial func(context.Context, string) (*websocket.Conn, error)

type FleetLinkOptions struct {
	HubURL            string
	OwnerID           string
	HostID            string
	PrivateKey        ed25519.PrivateKey
	HubPublic         ed25519.PublicKey
	BinaryVersion     string
	Capabilities      []string
	HeartbeatInterval time.Duration
	ReconnectMin      time.Duration
	ReconnectMax      time.Duration
	HTTPClient        *http.Client
	Dial              FleetLinkDial
	OnControl         func(context.Context, fleet.Control) error
	OnControlResult   func(context.Context, fleet.Control) fleet.ControlResult
	BudgetReport      func() fleet.BudgetReport
}

type FleetLink struct {
	hubURL            string
	ownerID           string
	hostID            string
	privateKey        ed25519.PrivateKey
	hubPublic         ed25519.PublicKey
	binaryVersion     string
	capabilities      []string
	heartbeatInterval time.Duration
	reconnectMin      time.Duration
	reconnectMax      time.Duration
	dial              FleetLinkDial
	onControl         func(context.Context, fleet.Control) error
	onControlResult   func(context.Context, fleet.Control) fleet.ControlResult
	budgetReport      func() fleet.BudgetReport
}

func NewFleetLink(opts FleetLinkOptions) (*FleetLink, error) {
	hubURL, err := normalizeFleetLinkURL(opts.HubURL)
	if err != nil {
		return nil, err
	}
	hostID := strings.TrimSpace(opts.HostID)
	ownerID := strings.TrimSpace(opts.OwnerID)
	binaryVersion := strings.TrimSpace(opts.BinaryVersion)
	if len(opts.PrivateKey) != ed25519.PrivateKeySize || len(opts.HubPublic) != ed25519.PublicKeySize {
		return nil, errors.New("invalid fleet link configuration")
	}
	if err := (fleet.Heartbeat{Version: fleet.ProtocolVersion, OwnerID: ownerID, HostID: hostID, SentAt: time.Now().UTC(), BinaryVersion: binaryVersion, Capabilities: opts.Capabilities, Signature: "configured"}).Validate(); err != nil {
		return nil, err
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = defaultFleetLinkHeartbeatInterval
	}
	if opts.ReconnectMin <= 0 {
		opts.ReconnectMin = defaultFleetLinkReconnectMin
	}
	if opts.ReconnectMax <= 0 {
		opts.ReconnectMax = defaultFleetLinkReconnectMax
	}
	if opts.ReconnectMin > opts.ReconnectMax {
		return nil, errors.New("fleet link reconnect minimum exceeds maximum")
	}
	dial := opts.Dial
	if dial == nil {
		dial = func(ctx context.Context, endpoint string) (*websocket.Conn, error) {
			conn, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPClient: opts.HTTPClient, Subprotocols: []string{fleetLinkSubprotocol}})
			return conn, err
		}
	}
	return &FleetLink{
		hubURL:            hubURL,
		ownerID:           ownerID,
		hostID:            hostID,
		privateKey:        append(ed25519.PrivateKey(nil), opts.PrivateKey...),
		hubPublic:         append(ed25519.PublicKey(nil), opts.HubPublic...),
		binaryVersion:     binaryVersion,
		capabilities:      append([]string(nil), opts.Capabilities...),
		heartbeatInterval: opts.HeartbeatInterval,
		reconnectMin:      opts.ReconnectMin,
		reconnectMax:      opts.ReconnectMax,
		dial:              dial,
		onControl:         opts.OnControl,
		onControlResult:   opts.OnControlResult,
		budgetReport:      opts.BudgetReport,
	}, nil
}

func (l *FleetLink) Run(ctx context.Context) error {
	delay := l.reconnectMin
	for {
		err := l.runOnce(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			delay = l.reconnectMin
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
		delay = fleetLinkNextReconnectDelay(delay, l.reconnectMax)
	}
}

func (l *FleetLink) RunOnce(ctx context.Context) error {
	return l.runOnce(ctx)
}

func (l *FleetLink) runOnce(ctx context.Context) error {
	conn, err := l.dial(ctx, l.hubURL)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(fleetLinkMaxFrameSize)
	var writeMu sync.Mutex
	write := func(ctx context.Context, value any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return writeFleetLinkFrame(ctx, conn, value)
	}
	handshakeCtx, cancel := context.WithTimeout(ctx, fleetLinkHandshakeTimeout)
	defer cancel()
	var challenge fleet.LinkChallenge
	if err := wsjson.Read(handshakeCtx, conn, &challenge); err != nil {
		return err
	}
	if err := challenge.Validate(); err != nil {
		return err
	}
	if !challenge.ExpiresAt.After(time.Now().UTC()) {
		return errors.New("fleet link challenge expired")
	}
	auth := fleet.LinkAuthenticate{
		Version:     fleet.ProtocolVersion,
		OwnerID:     l.ownerID,
		HostID:      l.hostID,
		ChallengeID: challenge.ID,
		Nonce:       challenge.Nonce,
		SentAt:      time.Now().UTC(),
	}
	auth.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(l.privateKey, fleet.LinkAuthenticateSigningPayload(challenge, auth)))
	if err := write(handshakeCtx, auth); err != nil {
		return err
	}
	var accepted fleet.LinkAccepted
	if err := wsjson.Read(handshakeCtx, conn, &accepted); err != nil {
		return err
	}
	if err := accepted.Validate(); err != nil {
		return err
	}
	if accepted.OwnerID != l.ownerID || accepted.HostID != l.hostID || accepted.ChallengeID != challenge.ID || len(accepted.Nonce) != len(challenge.Nonce) || subtle.ConstantTimeCompare([]byte(accepted.Nonce), []byte(challenge.Nonce)) != 1 {
		return errors.New("fleet link acceptance mismatch")
	}
	if skew := time.Now().UTC().Sub(accepted.SentAt.UTC()); skew > fleetLinkMaxSkew || skew < -fleetLinkMaxSkew {
		return errors.New("fleet link acceptance timestamp outside allowed skew")
	}
	acceptedSignature, err := base64.RawURLEncoding.DecodeString(accepted.Signature)
	if err != nil || !ed25519.Verify(l.hubPublic, fleet.LinkAcceptedSigningPayload(challenge, accepted), acceptedSignature) {
		return errors.New("invalid fleet link acceptance proof")
	}
	if err := l.writeHeartbeat(ctx, write); err != nil {
		return err
	}
	readErr := make(chan error, 1)
	go func() { readErr <- l.readControls(ctx, conn, write) }()
	ticker := time.NewTicker(l.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readErr:
			return err
		case <-ticker.C:
			if err := l.writeHeartbeat(ctx, write); err != nil {
				return err
			}
		}
	}
}

func (l *FleetLink) writeHeartbeat(ctx context.Context, write func(context.Context, any) error) error {
	now := time.Now().UTC()
	heartbeat := fleet.Heartbeat{
		Version:       fleet.ProtocolVersion,
		OwnerID:       l.ownerID,
		HostID:        l.hostID,
		SentAt:        now,
		BinaryVersion: l.binaryVersion,
		Capabilities:  append([]string(nil), l.capabilities...),
	}
	if l.budgetReport != nil {
		heartbeat.Budget = l.budgetReport().Normalized()
	}
	heartbeat.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(l.privateKey, fleet.HeartbeatSigningPayload(heartbeat)))
	body, err := json.Marshal(heartbeat)
	if err != nil {
		return err
	}
	requestID, err := newFleetLinkRequestID()
	if err != nil {
		return err
	}
	envelope := fleet.Envelope{Version: fleet.ProtocolVersion, Type: fleet.MessageHeartbeat, RequestID: requestID, SentAt: now, Body: body}
	frame := fleet.LinkFrame{Envelope: envelope, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(l.privateKey, fleet.LinkFrameSigningPayload(envelope)))}
	return write(ctx, frame)
}

func (l *FleetLink) readControls(ctx context.Context, conn *websocket.Conn, write func(context.Context, any) error) error {
	for {
		var frame fleet.LinkFrame
		if err := wsjson.Read(ctx, conn, &frame); err != nil {
			return err
		}
		if err := frame.Validate(); err != nil {
			return err
		}
		if frame.Envelope.Type != fleet.MessageControl {
			return errors.New("unsupported fleet link message")
		}
		signature, err := base64.RawURLEncoding.DecodeString(frame.Signature)
		if err != nil || !ed25519.Verify(l.hubPublic, fleet.LinkFrameSigningPayload(frame.Envelope), signature) {
			return errors.New("invalid fleet control signature")
		}
		var control fleet.Control
		if err := json.Unmarshal(frame.Envelope.Body, &control); err != nil {
			return err
		}
		if err := control.Validate(); err != nil {
			return err
		}
		if control.OwnerID != l.ownerID || control.HostID != l.hostID || !control.ExpiresAt.After(time.Now().UTC()) {
			return errors.New("invalid fleet control target")
		}
		result := fleet.ControlResult{Version: fleet.ProtocolVersion, ID: control.ID, OwnerID: control.OwnerID, HostID: control.HostID, State: fleet.CommandSucceeded, CompletedAt: time.Now().UTC()}
		if l.onControlResult != nil {
			result = l.onControlResult(ctx, control)
			result.Version = fleet.ProtocolVersion
			result.ID = control.ID
			result.OwnerID = control.OwnerID
			result.HostID = control.HostID
			if result.CompletedAt.IsZero() {
				result.CompletedAt = time.Now().UTC()
			}
		} else if l.onControl == nil {
			result.State = fleet.CommandFailed
			result.Error = "control handler unavailable"
		} else if err := l.onControl(ctx, control); err != nil {
			result.State = fleet.CommandFailed
			result.Error = fleetControlError(err)
		}
		if err := result.Validate(); err != nil {
			result = fleet.ControlResult{Version: fleet.ProtocolVersion, ID: control.ID, OwnerID: control.OwnerID, HostID: control.HostID, State: fleet.CommandFailed, Error: fleetControlError(err), CompletedAt: time.Now().UTC()}
		}
		if err := l.writeControlResult(ctx, write, result); err != nil {
			return err
		}
	}
}

func (l *FleetLink) SetControlResultHandler(handler func(context.Context, fleet.Control) fleet.ControlResult) {
	if l == nil {
		return
	}
	l.onControlResult = handler
}

func (l *FleetLink) SetBudgetReportProvider(provider func() fleet.BudgetReport) {
	if l == nil {
		return
	}
	l.budgetReport = provider
}

func (l *FleetLink) writeControlResult(ctx context.Context, write func(context.Context, any) error, result fleet.ControlResult) error {
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	envelope := fleet.Envelope{Version: fleet.ProtocolVersion, Type: fleet.MessageControlResult, RequestID: result.ID, SentAt: result.CompletedAt, Body: body}
	frame := fleet.LinkFrame{Envelope: envelope, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(l.privateKey, fleet.LinkFrameSigningPayload(envelope)))}
	return write(ctx, frame)
}

func fleetControlError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > 512 {
		return message[:512]
	}
	if message == "" {
		return "control failed"
	}
	return message
}

func normalizeFleetLinkURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("fleet hub URL must be an HTTPS origin without credentials, query, or fragment")
	}
	u.Scheme = "wss"
	u.Path = strings.TrimRight(u.Path, "/") + "/fleet/link"
	return u.String(), nil
}

func fleetLinkNextReconnectDelay(current, max time.Duration) time.Duration {
	if current >= max/2 {
		return max
	}
	return current * 2
}

func newFleetLinkRequestID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "link-" + hex.EncodeToString(b), nil
}

func writeFleetLinkFrame(ctx context.Context, conn *websocket.Conn, value any) error {
	writeCtx, cancel := context.WithTimeout(ctx, fleetLinkHandshakeTimeout)
	defer cancel()
	return wsjson.Write(writeCtx, conn, value)
}
