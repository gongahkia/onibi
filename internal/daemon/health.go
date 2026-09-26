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
	PollSuccess      time.Time                `json:"poll_success,omitempty"`
	PollFailure      time.Time                `json:"poll_failure,omitempty"`
	PollFailures     int                      `json:"poll_failures,omitempty"`
	PollError        string                   `json:"poll_error,omitempty"`
	PollAlerted      bool                     `json:"poll_alerted,omitempty"`
	DeliverySuccess  time.Time                `json:"delivery_success,omitempty"`
	DeliveryFailure  time.Time                `json:"delivery_failure,omitempty"`
	DeliveryFailures int                      `json:"delivery_failures,omitempty"`
	DeliveryError    string                   `json:"delivery_error,omitempty"`
	DeliveryAlerted  bool                     `json:"delivery_alerted,omitempty"`
	Sessions         map[string]sessionHealth `json:"sessions,omitempty"`
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

func (h *healthTracker) pollResult(ctx context.Context, err error) *healthEvent {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	var event *healthEvent
	if err == nil {
		h.state.PollSuccess = now
		h.state.PollFailures = 0
		h.state.PollError = ""
		if h.state.PollAlerted {
			h.state.PollAlerted = false
			event = &healthEvent{Dedupe: "health:telegram-poll:recovered", Text: "Onibi alert: Telegram polling recovered."}
		}
	} else {
		h.state.PollFailure = now
		h.state.PollFailures++
		h.state.PollError = healthError(err)
		if h.state.PollFailures >= 3 && !h.state.PollAlerted {
			h.state.PollAlerted = true
			event = &healthEvent{Dedupe: "health:telegram-poll:failed", Text: "Onibi alert: Telegram polling has failed three consecutive times; commands may be delayed."}
		}
	}
	h.persistLocked(ctx)
	return event
}

func (h *healthTracker) deliveryResult(ctx context.Context, err error, permanent bool) *healthEvent {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	var event *healthEvent
	if err == nil {
		h.state.DeliverySuccess = now
		h.state.DeliveryFailures = 0
		h.state.DeliveryError = ""
		if h.state.DeliveryAlerted {
			h.state.DeliveryAlerted = false
			event = &healthEvent{Dedupe: "health:telegram-delivery:recovered", Text: "Onibi alert: Telegram delivery recovered."}
		}
	} else {
		h.state.DeliveryFailure = now
		h.state.DeliveryFailures++
		h.state.DeliveryError = healthError(err)
		if permanent && !h.state.DeliveryAlerted {
			h.state.DeliveryAlerted = true
			event = &healthEvent{Dedupe: "health:telegram-delivery:failed", Text: "Onibi alert: Telegram permanently rejected an outbound message; inspect /status and bot permissions."}
		}
	}
	h.persistLocked(ctx)
	return event
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
		return &healthEvent{Dedupe: "health:tmux:" + s.ID + ":recovered", Text: "Onibi alert: tmux session " + s.Name + " recovered."}
	case status == "unavailable":
		return &healthEvent{Dedupe: "health:tmux:" + s.ID + ":unavailable", Text: "Onibi alert: tmux health check failed for " + s.Name + "."}
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

func healthAge(now, value time.Time) string {
	if value.IsZero() {
		return "never"
	}
	return now.Sub(value).Truncate(time.Second).String() + " ago"
}

func (d *Daemon) queueHealthEvent(ctx context.Context, event *healthEvent) {
	if event == nil || d.DB == nil || d.TelegramOwnerID == 0 {
		return
	}
	if err := d.DB.TelegramOutboxUpsert(ctx, store.TelegramOutboxIntent{ID: NewID(), DedupeKey: event.Dedupe, Kind: "notice", ChatID: d.TelegramOwnerID, Title: event.Text}); err != nil {
		d.Log.Warn("queue health alert", "err", err)
	}
}

func (d *Daemon) pingText(ctx context.Context) string {
	state := d.health.snapshot()
	now := time.Now()
	stats := store.TelegramOutboxStats{}
	if d.DB != nil {
		var err error
		stats, err = d.DB.TelegramOutboxStats(ctx)
		if err != nil {
			d.Log.Warn("Telegram outbox stats", "err", err)
		}
	}
	codexSessions := 0
	sessions := d.liveSessions()
	for _, s := range sessions {
		if s.Transport == "codex" {
			codexSessions++
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "onibi\nuptime=%s\nsessions=%d\ncodex_sessions=%d\npoll=%s failures=%d\ndelivery=%s failures=%d\noutbox=pending:%d running:%d expired:%d", time.Since(d.started).Truncate(time.Second), len(sessions), codexSessions, healthAge(now, state.PollSuccess), state.PollFailures, healthAge(now, state.DeliverySuccess), state.DeliveryFailures, stats.Pending, stats.Running, stats.Expired)
	if len(sessions) > 0 {
		out.WriteString("\ntmux:")
		for _, s := range sessions {
			health := state.Sessions[s.ID]
			status := health.Status
			if status == "" {
				status = "unknown"
			}
			fmt.Fprintf(&out, "\n- %s=%s", s.Name, status)
		}
	}
	return out.String()
}
