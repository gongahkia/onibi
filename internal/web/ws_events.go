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
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sessionID, ok := s.requireWSAuth(w, r)
	if !ok {
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{eventsSubprotocol}})
	if err != nil {
		s.log.Warn("web events ws accept failed", "request_id", requestID(r), "err", err, "remote", remoteHost(r.RemoteAddr))
		return
	}
	defer c.CloseNow()
	s.log.Info("web events ws accepted", "request_id", requestID(r), "remote", remoteHost(r.RemoteAddr))

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go s.pingLoop(ctx, c)
	var writeMu sync.Mutex
	if err := writeEvent(ctx, c, &writeMu, "server.hello", map[string]any{
		"endpoint":   "events",
		"session_id": sessionID,
	}); err != nil {
		return
	}
	for id := range s.currentPTYHosts() {
		if err := writeEvent(ctx, c, &writeMu, "session.started", map[string]any{"session_id": id}); err != nil {
			return
		}
	}
	if s.approvalQueue == nil {
		s.log.Info("web events ws waiting without approval queue", "request_id", requestID(r), "session_id", sessionID)
		<-ctx.Done()
		return
	}
	events, unsub := s.approvalQueue.Subscribe()
	defer unsub()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if err := writeEvent(ctx, c, &writeMu, ev.Type, approvalEventPayload(ev)); err != nil {
				return
			}
		}
	}
}

func writeEvent(ctx context.Context, c *websocket.Conn, mu *sync.Mutex, typ string, payload any) error {
	return writeWSJSON(ctx, c, mu, eventEnvelope{
		Type:    typ,
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		Payload: payload,
	})
}

func approvalEventPayload(ev approval.Event) map[string]any {
	a := ev.Approval
	switch ev.Type {
	case approval.EventRequested:
		risk := approval.ClassifyRisk(a.Tool, a.InputJSON)
		return map[string]any{
			"id":             a.ID,
			"session_id":     a.SessionID,
			"agent":          a.Agent,
			"tool":           a.Tool,
			"scrubbed_input": approval.Scrub(a.InputJSON),
			"risk_level":     risk.Level,
			"risk_reasons":   risk.Reasons,
			"expires_at":     a.ExpiresAt.UTC().Format(time.RFC3339Nano),
		}
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

func (s *Server) currentPTYHosts() map[string]any {
	out := map[string]any{}
	if s.ptyHosts == nil {
		return out
	}
	for id, h := range s.ptyHosts() {
		if h != nil {
			out[id] = true
		}
	}
	return out
}
