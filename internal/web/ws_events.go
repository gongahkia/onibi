package web

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/gongahkia/onibi/internal/approval"
)

type eventEnvelope struct {
	Type    string `json:"type"`
	TS      string `json:"ts"`
	Payload any    `json:"payload"`
}

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
	codec, err := s.e2eCodec(sessionID, e2eInfoEvents)
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
	var writeMu sync.Mutex
	wsE2E := newSeqWSCodec(codec, sessionID, e2eInfoEvents, e2eDirC2S, e2eDirS2C)
	if err := writeEvent(ctx, c, &writeMu, wsE2E, "server.hello", map[string]any{
		"endpoint":   "events",
		"session_id": sessionID,
	}); err != nil {
		s.log.Warn("web events write failed", "request_id", reqID, "session_id", sessionID, "event_type", "server.hello", "err", err)
		return
	}
	eventsSent++
	for _, id := range s.activeSessionIDs() {
		if err := writeEvent(ctx, c, &writeMu, wsE2E, "session.started", map[string]any{"session_id": id}); err != nil {
			s.log.Warn("web events write failed", "request_id", reqID, "session_id", sessionID, "event_type", "session.started", "err", err)
			return
		}
		eventsSent++
	}
	var approvalEvents <-chan approval.Event
	var unsubApprovals func()
	if s.approvalQueue != nil {
		approvalEvents, unsubApprovals = s.approvalQueue.Subscribe()
		defer unsubApprovals()
	}
	var appEvents <-chan Event
	var unsubAppEvents func()
	if s.eventBus != nil {
		appEvents, unsubAppEvents = s.eventBus.Subscribe()
		defer unsubAppEvents()
	}
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
			if err := writeEvent(ctx, c, &writeMu, wsE2E, ev.Type, approvalEventPayload(ev)); err != nil {
				s.log.Warn("web events write failed", "request_id", reqID, "session_id", sessionID, "event_type", ev.Type, "approval_id", ev.Approval.ID, "err", err)
				return
			}
			eventsSent++
			s.log.Debug("web event sent", "request_id", reqID, "session_id", sessionID, "event_type", ev.Type, "approval_id", ev.Approval.ID, "events_sent", eventsSent)
		case ev, ok := <-appEvents:
			if !ok {
				appEvents = nil
				if approvalEvents == nil {
					return
				}
				continue
			}
			if err := writeEvent(ctx, c, &writeMu, wsE2E, ev.Type, ev.Payload); err != nil {
				s.log.Warn("web events write failed", "request_id", reqID, "session_id", sessionID, "event_type", ev.Type, "err", err)
				return
			}
			eventsSent++
			s.log.Debug("web event sent", "request_id", reqID, "session_id", sessionID, "event_type", ev.Type, "events_sent", eventsSent)
		}
	}
}

func writeEvent(ctx context.Context, c *websocket.Conn, mu *sync.Mutex, codec wsCodec, typ string, payload any) error {
	return writeWSJSON(ctx, c, mu, eventEnvelope{
		Type:    typ,
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		Payload: payload,
	}, codec)
}

func approvalEventPayload(ev approval.Event) map[string]any {
	a := ev.Approval
	switch ev.Type {
	case approval.EventRequested:
		risk := approval.ClassifyRisk(a.Tool, a.InputJSON)
		payload := map[string]any{
			"id":             a.ID,
			"session_id":     a.SessionID,
			"agent":          a.Agent,
			"tool":           a.Tool,
			"scrubbed_input": approval.Scrub(a.InputJSON),
			"risk_level":     risk.Level,
			"risk_reasons":   risk.Reasons,
			"expires_at":     a.ExpiresAt.UTC().Format(time.RFC3339Nano),
		}
		if a.UnifiedDiff != "" {
			payload["unified_diff"] = a.UnifiedDiff
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
