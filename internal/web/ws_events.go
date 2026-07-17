package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/gongahkia/onibi/internal/approval"
)

type eventEnvelope struct {
	Seq     uint64 `json:"seq"`
	Type    string `json:"type"`
	TS      string `json:"ts"`
	Payload any    `json:"payload"`
}

type eventsAttachFrame struct {
	Type        string `json:"type"`
	LastSeq     uint64 `json:"last_seq"`
	VerifyToken string `json:"verify_token,omitempty"`
}

type eventsRecoveryFrame struct {
	Type        string `json:"type"`
	Mode        string `json:"mode"`
	Seq         uint64 `json:"seq"`
	ReplayCount int    `json:"replay_count"`
}

const (
	sessionsStatusEvent       = "sessions.status"
	sessionsStatusMinInterval = 500 * time.Millisecond
)

func (s *Server) handleWSEvents(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sessionID, ok := s.requireWSAuth(w, r)
	if !ok {
		return
	}
	sessionKey, err := s.e2eSessionKey(sessionID)
	if err != nil {
		s.log.Warn("web events e2e unavailable", "request_id", requestID(r), "err", err, "remote", remoteHost(r.RemoteAddr))
		http.Error(w, "relay e2e unavailable", http.StatusUnauthorized)
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{eventsSubprotocol}})
	if err != nil {
		s.log.Warn("web events ws accept failed", "request_id", requestID(r), "err", err, "remote", remoteHost(r.RemoteAddr))
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)
	reqID := requestID(r)
	eventsSent := 0
	s.log.Info("web events ws accepted", "request_id", reqID, "remote", remoteHost(r.RemoteAddr), "session_id", sessionID)
	defer func() {
		s.log.Info("web events ws closed",
			"request_id", reqID,
			"session_id", sessionID,
			"remote", remoteHost(r.RemoteAddr),
			"duration_ms", time.Since(started).Milliseconds(),
			"events_sent", eventsSent,
		)
	}()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go s.pingLoop(ctx, c)
	go s.watchWebSession(ctx, c, sessionID, cancel, reqID)
	var writeMu sync.Mutex
	wsE2E := newSeqWSCodec(sessionKey, sessionID, e2eInfoEvents, e2eDirC2S, e2eDirS2C)
	attach, err := readEventsAttach(ctx, c, wsE2E)
	if err != nil {
		s.log.Warn("web events attach failed", "request_id", reqID, "err", err, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
		_ = c.Close(websocket.StatusPolicyViolation, "attach required")
		return
	}
	if wsE2E != nil {
		if err := s.verifyRelayAttach(ctx, sessionID, attach.VerifyToken); err != nil {
			s.log.Warn("web events attach failed", "request_id", reqID, "reason", "bad_relay_verifier", "err", err, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
			_ = c.Close(websocket.StatusPolicyViolation, "bad relay verifier")
			return
		}
	}
	var approvalEvents <-chan approval.Event
	var unsubApprovals func()
	if s.approvalQueue != nil {
		var err error
		approvalEvents, unsubApprovals, err = s.approvalQueue.Subscribe()
		if err != nil {
			s.log.Warn("web events approval subscribe failed", "request_id", reqID, "err", err, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
			_ = c.Close(websocket.StatusTryAgainLater, "approval subscribers full")
			return
		}
		defer unsubApprovals()
	}
	var appEvents <-chan Event
	var unsubAppEvents func()
	appReplay := EventReplay{Snapshot: attach.LastSeq > 0}
	if s.eventBus != nil {
		appReplay, appEvents, unsubAppEvents = s.eventBus.SubscribeFrom(attach.LastSeq)
		defer unsubAppEvents()
	}
	sentApprovalRequests := map[string]bool{}
	lastSessionsStatus := time.Time{}
	writeSessionsStatus := func(force bool) error {
		if s.sessionList == nil {
			return nil
		}
		now := time.Now()
		if !force && now.Sub(lastSessionsStatus) < sessionsStatusMinInterval {
			return nil
		}
		status, err := s.sessionsStatus(ctx, SessionListOptions{IncludeRemote: true}, now)
		if err != nil {
			s.log.Warn("web sessions status failed", "request_id", reqID, "session_id", sessionID, "err", err)
			return nil
		}
		if err := writeEvent(ctx, c, &writeMu, wsE2E, sessionsStatusEvent, status); err != nil {
			s.log.Warn("web events write failed", "request_id", reqID, "session_id", sessionID, "event_type", sessionsStatusEvent, "err", err)
			return err
		}
		lastSessionsStatus = now
		eventsSent++
		return nil
	}
	writeSnapshot := func() error {
		if err := writeEvent(ctx, c, &writeMu, wsE2E, "server.hello", map[string]any{
			"endpoint":   "events",
			"session_id": sessionID,
		}); err != nil {
			s.log.Warn("web events write failed", "request_id", reqID, "session_id", sessionID, "event_type", "server.hello", "err", err)
			return err
		}
		eventsSent++
		for _, id := range s.activeSessionIDs() {
			if err := writeEvent(ctx, c, &writeMu, wsE2E, "session.started", map[string]any{"session_id": id}); err != nil {
				s.log.Warn("web events write failed", "request_id", reqID, "session_id", sessionID, "event_type", "session.started", "err", err)
				return err
			}
			eventsSent++
		}
		if err := writeSessionsStatus(true); err != nil {
			return err
		}
		return nil
	}
	if attach.LastSeq == 0 || appReplay.Snapshot {
		if err := writeSnapshot(); err != nil {
			return
		}
	}
	if s.approvalQueue != nil {
		pending, err := s.approvalQueue.Pending(ctx)
		if err != nil {
			s.log.Warn("web pending approval replay failed", "request_id", reqID, "session_id", sessionID, "err", err)
		}
		for _, a := range pending {
			ev := approval.Event{Type: approval.EventRequested, Approval: *a, At: time.Now()}
			if err := writeEvent(ctx, c, &writeMu, wsE2E, ev.Type, approvalEventPayload(ev)); err != nil {
				s.log.Warn("web events write failed", "request_id", reqID, "session_id", sessionID, "event_type", ev.Type, "approval_id", ev.Approval.ID, "err", err)
				return
			}
			sentApprovalRequests[a.ID] = true
			eventsSent++
		}
	}
	for _, ev := range appReplay.Events {
		if err := writeSequencedEvent(ctx, c, &writeMu, wsE2E, ev); err != nil {
			s.log.Warn("web events replay failed", "request_id", reqID, "session_id", sessionID, "event_type", ev.Type, "seq", ev.Seq, "err", err)
			return
		}
		eventsSent++
	}
	mode := "replay"
	if attach.LastSeq == 0 || appReplay.Snapshot {
		mode = "snapshot"
	}
	if err := writeEvent(ctx, c, &writeMu, wsE2E, "events.recovery", eventsRecoveryFrame{
		Type:        "events.recovery",
		Mode:        mode,
		Seq:         appReplay.Seq,
		ReplayCount: len(appReplay.Events),
	}); err != nil {
		s.log.Warn("web events recovery feedback failed", "request_id", reqID, "session_id", sessionID, "err", err)
		return
	}
	go s.watchWSEventsFrames(ctx, c, wsE2E, cancel, reqID, sessionID)
	if approvalEvents == nil && appEvents == nil {
		s.log.Info("web events ws waiting without event sources", "request_id", reqID, "session_id", sessionID)
		<-ctx.Done()
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-approvalEvents:
			if !ok {
				approvalEvents = nil
				if appEvents == nil {
					return
				}
				continue
			}
			if ev.Type == approval.EventRequested && sentApprovalRequests[ev.Approval.ID] {
				continue
			}
			if err := writeEvent(ctx, c, &writeMu, wsE2E, ev.Type, approvalEventPayload(ev)); err != nil {
				s.log.Warn("web events write failed", "request_id", reqID, "session_id", sessionID, "event_type", ev.Type, "approval_id", ev.Approval.ID, "err", err)
				return
			}
			if ev.Type == approval.EventRequested {
				sentApprovalRequests[ev.Approval.ID] = true
			}
			eventsSent++
			if err := writeSessionsStatus(true); err != nil {
				return
			}
			s.log.Debug("web event sent", "request_id", reqID, "session_id", sessionID, "event_type", ev.Type, "approval_id", ev.Approval.ID, "events_sent", eventsSent)
		case ev, ok := <-appEvents:
			if !ok {
				appEvents = nil
				if approvalEvents == nil {
					return
				}
				continue
			}
			if err := writeSequencedEvent(ctx, c, &writeMu, wsE2E, ev); err != nil {
				s.log.Warn("web events write failed", "request_id", reqID, "session_id", sessionID, "event_type", ev.Type, "seq", ev.Seq, "err", err)
				return
			}
			eventsSent++
			if sessionsStatusRefreshEvent(ev.Type) {
				if err := writeSessionsStatus(false); err != nil {
					return
				}
			}
			s.log.Debug("web event sent", "request_id", reqID, "session_id", sessionID, "event_type", ev.Type, "events_sent", eventsSent)
		}
	}
}

func sessionsStatusRefreshEvent(typ string) bool {
	switch typ {
	case "session.started", "session.ended", "session.activity":
		return true
	default:
		return false
	}
}

func readEventsAttach(ctx context.Context, c *websocket.Conn, codec wsCodec) (eventsAttachFrame, error) {
	readCtx, cancel := context.WithTimeout(ctx, websocketAttachTimeout())
	defer cancel()
	typ, p, err := c.Read(readCtx)
	if err != nil {
		return eventsAttachFrame{}, err
	}
	if codec != nil {
		typ, p, err = codec.decrypt(typ, p)
		if err != nil {
			return eventsAttachFrame{}, err
		}
	}
	if typ != websocket.MessageText {
		return eventsAttachFrame{}, errors.New("web: events attach must be text")
	}
	var attach eventsAttachFrame
	if err := json.Unmarshal(p, &attach); err != nil {
		return eventsAttachFrame{}, err
	}
	if attach.Type != "attach" {
		return eventsAttachFrame{}, errors.New("web: invalid events attach")
	}
	return attach, nil
}

func (s *Server) watchWSEventsFrames(ctx context.Context, c *websocket.Conn, codec wsCodec, cancel context.CancelFunc, reqID, sessionID string) {
	for {
		typ, p, err := c.Read(ctx)
		if err != nil {
			cancel()
			return
		}
		if codec != nil {
			typ, p, err = codec.decrypt(typ, p)
			if err != nil {
				s.log.Warn("web events client frame rejected", "request_id", reqID, "session_id", sessionID, "err", err)
				_ = c.Close(websocket.StatusPolicyViolation, "invalid event frame")
				cancel()
				return
			}
		}
		reason := "unexpected event frame"
		if typ == websocket.MessageText {
			var frame eventsAttachFrame
			if json.Unmarshal(p, &frame) == nil && frame.Type == "attach" {
				reason = "duplicate event attach"
			}
		}
		s.log.Warn("web events client frame rejected", "request_id", reqID, "session_id", sessionID, "reason", reason)
		_ = c.Close(websocket.StatusPolicyViolation, reason)
		cancel()
		return
	}
}

func writeEvent(ctx context.Context, c *websocket.Conn, mu *sync.Mutex, codec wsCodec, typ string, payload any) error {
	return writeEventEnvelope(ctx, c, mu, codec, 0, typ, payload)
}

func writeSequencedEvent(ctx context.Context, c *websocket.Conn, mu *sync.Mutex, codec wsCodec, ev Event) error {
	return writeEventEnvelope(ctx, c, mu, codec, ev.Seq, ev.Type, ev.Payload)
}

func writeEventEnvelope(ctx context.Context, c *websocket.Conn, mu *sync.Mutex, codec wsCodec, seq uint64, typ string, payload any) error {
	return writeWSJSON(ctx, c, mu, eventEnvelope{
		Seq:     seq,
		Type:    typ,
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		Payload: payload,
	}, codec)
}

func approvalEventPayload(ev approval.Event) map[string]any {
	a := ev.Approval
	model, err := approval.PayloadForApproval(a)
	if err != nil {
		return map[string]any{"id": a.ID, "session_id": a.SessionID, "state": a.State, "error": "invalid approval payload"}
	}
	switch ev.Type {
	case approval.EventRequested:
		payload := map[string]any{
			"id":             a.ID,
			"session_id":     a.SessionID,
			"agent":          a.Agent,
			"tool":           a.Tool,
			"scrubbed_input": model.ScrubbedInput,
			"risk_level":     model.Risk.Level,
			"risk_reasons":   model.Risk.Reasons,
			"approval":       model,
			"expires_at":     a.ExpiresAt.UTC().Format(time.RFC3339Nano),
		}
		if a.UnifiedDiff != "" {
			payload["unified_diff"] = a.UnifiedDiff
		}
		if model.Details.FilePath != "" {
			payload["file_path"] = model.Details.FilePath
		}
		return payload
	case approval.EventExpired:
		return map[string]any{
			"id":         a.ID,
			"session_id": a.SessionID,
			"verdict":    ev.Decision.Verdict,
			"reason":     ev.Decision.Reason,
			"expires_at": a.ExpiresAt.UTC().Format(time.RFC3339Nano),
		}
	default:
		return map[string]any{
			"id":         a.ID,
			"session_id": a.SessionID,
			"verdict":    ev.Decision.Verdict,
			"reason":     ev.Decision.Reason,
			"decided_at": ev.Decision.DecidedAt,
		}
	}
}
