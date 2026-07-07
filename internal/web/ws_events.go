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
	Type    string `json:"type"`
	TS      string `json:"ts"`
	Payload any    `json:"payload"`
}

type eventsAttachFrame struct {
	Type        string `json:"type"`
	VerifyToken string `json:"verify_token,omitempty"`
}

const (
	timelineEntryEvent          = "timeline.entry"
	defaultTimelineReplayEvents = 200
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
	var writeMu sync.Mutex
	wsE2E := newSeqWSCodec(sessionKey, sessionID, e2eInfoEvents, e2eDirC2S, e2eDirS2C)
	if wsE2E != nil {
		attach, err := readEventsAttach(ctx, c, wsE2E)
		if err != nil {
			s.log.Warn("web events attach failed", "request_id", reqID, "err", err, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
			_ = c.Close(websocket.StatusPolicyViolation, "attach required")
			return
		}
		if err := s.verifyRelayAttach(ctx, sessionID, attach.VerifyToken); err != nil {
			s.log.Warn("web events attach failed", "request_id", reqID, "reason", "bad_relay_verifier", "err", err, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
			_ = c.Close(websocket.StatusPolicyViolation, "bad relay verifier")
			return
		}
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
	sentApprovalRequests := map[string]bool{}
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
	n, err := s.writeTimelineReplay(ctx, c, &writeMu, wsE2E, reqID, sessionID)
	if err != nil {
		return
	}
	eventsSent += n
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

func readEventsAttach(ctx context.Context, c *websocket.Conn, codec wsCodec) (eventsAttachFrame, error) {
	readCtx, cancel := context.WithTimeout(ctx, ptyAttachTimeout)
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

func (s *Server) writeTimelineReplay(ctx context.Context, c *websocket.Conn, writeMu *sync.Mutex, codec wsCodec, reqID, sessionID string) (int, error) {
	if s.timeline == nil {
		return 0, nil
	}
	entries, err := s.timeline(ctx, defaultTimelineReplayEvents)
	if err != nil {
		s.log.Warn("web timeline replay failed", "request_id", reqID, "session_id", sessionID, "err", err)
		return 0, nil
	}
	sent := 0
	for _, entry := range entries {
		if err := writeEvent(ctx, c, writeMu, codec, timelineEntryEvent, entry); err != nil {
			s.log.Warn("web events write failed", "request_id", reqID, "session_id", sessionID, "event_type", timelineEntryEvent, "err", err)
			return sent, err
		}
		sent++
	}
	return sent, nil
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
		details := approval.ExtractDetails(a.Tool, a.InputJSON)
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
		if a.BudgetWarn != nil {
			payload["budget_warning"] = map[string]any{
				"scope":            a.BudgetWarn.Scope,
				"current_tokens":   a.BudgetWarn.CurrentTokens,
				"predicted_tokens": a.BudgetWarn.PredictedTokens,
				"projected_tokens": a.BudgetWarn.ProjectedTokens,
				"limit_tokens":     a.BudgetWarn.LimitTokens,
				"remaining_tokens": a.BudgetWarn.RemainingTokens,
				"on_overrun":       a.BudgetWarn.OnOverrun,
				"message":          a.BudgetWarn.Message,
			}
		}
		if details.FilePath != "" {
			payload["file_path"] = details.FilePath
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
