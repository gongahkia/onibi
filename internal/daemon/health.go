package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/store"
)

const healthStateKey = "daemon.health.v1"

type daemonHealthState struct {
	Sessions map[string]sessionHealth `json:"sessions,omitempty"`
}

type sessionHealth struct {
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
	Error     string    `json:"error,omitempty"`
}

type healthEvent struct {
	Dedupe string
	Text   string
}

type healthTracker struct {
	db    *store.DB
	log   logger
	mu    sync.RWMutex
	state daemonHealthState
}

type logger interface{ Warn(string, ...any) }

func newHealthTracker(db *store.DB, log logger) *healthTracker {
	return &healthTracker{db: db, log: log, state: daemonHealthState{Sessions: map[string]sessionHealth{}}}
}

func (h *healthTracker) load(ctx context.Context) {
	if h == nil || h.db == nil {
		return
	}
	raw, ok, err := h.db.KVGet(ctx, healthStateKey)
	if err != nil {
		h.log.Warn("load daemon health", "err", err)
		return
	}
	if !ok || json.Unmarshal(raw, &h.state) != nil {
		return
	}
	if h.state.Sessions == nil {
		h.state.Sessions = map[string]sessionHealth{}
	}
}

func (h *healthTracker) persistLocked(ctx context.Context) {
	if h == nil || h.db == nil {
		return
	}
	raw, err := json.Marshal(h.state)
	if err == nil {
		err = h.db.KVSet(ctx, healthStateKey, raw, 0)
	}
	if err != nil {
		h.log.Warn("persist daemon health", "err", err)
	}
}

func (h *healthTracker) tmuxResult(ctx context.Context, s *Session, live bool, err error) *healthEvent {
	if h == nil || s == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state.Sessions == nil {
		h.state.Sessions = map[string]sessionHealth{}
	}
	now := time.Now()
	previous, known := h.state.Sessions[s.ID]
	status, detail := "healthy", ""
	if err != nil {
		status, detail = "unavailable", healthError(err)
	} else if !live {
		status = "ended"
	}
	h.state.Sessions[s.ID] = sessionHealth{Name: s.Name, Status: status, UpdatedAt: now, Error: detail}
	h.persistLocked(ctx)
	if !known || previous.Status == status {
		return nil
	}
	switch {
	case status == "healthy":
		return &healthEvent{Dedupe: "health:tmux:" + s.ID + ":recovered", Text: "Onibi alert: " + s.Transport + " session " + s.Name + " recovered."}
	case status == "unavailable":
		return &healthEvent{Dedupe: "health:tmux:" + s.ID + ":unavailable", Text: "Onibi alert: " + s.Transport + " health check failed for " + s.Name + "."}
	}
	return nil
}

func (h *healthTracker) sessionEnded(ctx context.Context, s *Session) {
	if h == nil || s == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state.Sessions == nil {
		h.state.Sessions = map[string]sessionHealth{}
	}
	h.state.Sessions[s.ID] = sessionHealth{Name: s.Name, Status: "ended", UpdatedAt: time.Now()}
	h.persistLocked(ctx)
}

func (h *healthTracker) snapshot() daemonHealthState {
	if h == nil {
		return daemonHealthState{Sessions: map[string]sessionHealth{}}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	copy := h.state
	copy.Sessions = make(map[string]sessionHealth, len(h.state.Sessions))
	for id, state := range h.state.Sessions {
		copy.Sessions[id] = state
	}
	return copy
}

func healthError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", " ")
	if len(text) > 240 {
		return text[:240] + "…"
	}
	return text
}

func (d *Daemon) reportHealthEvent(event *healthEvent) {
	if event == nil {
		return
	}
	d.Log.Warn("health alert", "key", event.Dedupe, "text", event.Text)
}

func (d *Daemon) pingText(_ context.Context) string {
	state := d.health.snapshot()
	sessions := d.liveSessions()
	var out strings.Builder
	fmt.Fprintf(&out, "onibi\nuptime=%s\nsessions=%d", time.Since(d.started).Truncate(time.Second), len(sessions))
	if len(sessions) > 0 {
		out.WriteString("\nmultiplexers:")
		for _, s := range sessions {
			health := state.Sessions[s.ID]
			status := health.Status
			if status == "" {
				status = "unknown"
			}
			fmt.Fprintf(&out, "\n- %s (%s)=%s", s.Name, s.Transport, status)
		}
	}
	return out.String()
}
