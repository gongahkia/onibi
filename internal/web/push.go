package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/webpush"
)

const (
	notifyPollInterval = 5 * time.Second
	notifyCooldown     = 30 * time.Second
	notifyTTL          = 10 * time.Minute
)

// notification is everything a push message carries. It names the session
// and the generic reason only; terminal text never leaves the Mac this way.
type notification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Tag   string `json:"tag"`
	URL   string `json:"url"`
}

type pushSender func(context.Context, webpush.Subscription, []byte) error

func (s *Server) preparePush(ctx context.Context) error {
	if _, err := s.db.SQL().ExecContext(ctx, `CREATE TABLE IF NOT EXISTS web_push_key (id INTEGER PRIMARY KEY CHECK (id = 1), der BLOB NOT NULL)`); err != nil {
		return err
	}
	if _, err := s.db.SQL().ExecContext(ctx, `CREATE TABLE IF NOT EXISTS web_push_subscriptions (
 device_id TEXT PRIMARY KEY, origin TEXT NOT NULL, endpoint TEXT NOT NULL,
 p256dh TEXT NOT NULL, auth TEXT NOT NULL, created_at INTEGER NOT NULL
 )`); err != nil {
		return err
	}
	if _, err := s.db.SQL().ExecContext(ctx, `DELETE FROM web_push_subscriptions WHERE device_id NOT IN (SELECT id FROM web_devices)`); err != nil {
		return err
	}
	var der []byte
	err := s.db.SQL().QueryRowContext(ctx, `SELECT der FROM web_push_key WHERE id=1`).Scan(&der)
	if errors.Is(err, sql.ErrNoRows) {
		key, err := webpush.GenerateKey()
		if err != nil {
			return err
		}
		if der, err = key.Marshal(); err != nil {
			return err
		}
		if _, err := s.db.SQL().ExecContext(ctx, `INSERT INTO web_push_key(id,der) VALUES(1,?)`, der); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	key, err := webpush.ParseKey(der)
	if err != nil {
		return err
	}
	s.pushKey = key
	subject := s.origin
	if !s.secure {
		subject = "mailto:onibi@localhost"
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	s.pushSend = func(ctx context.Context, sub webpush.Subscription, payload []byte) error {
		return key.Send(ctx, client, sub, payload, subject, notifyTTL)
	}
	return nil
}

func (s *Server) pushStatus(w http.ResponseWriter, r *http.Request, session browserSession) {
	var exists int
	err := s.db.SQL().QueryRowContext(r.Context(), `SELECT 1 FROM web_push_subscriptions WHERE device_id=? AND origin=?`, session.Device.ID, s.origin).Scan(&exists)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusServiceUnavailable, "push_unavailable", "Notification settings could not be read.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"publicKey": s.pushKey.PublicKey(), "subscribed": err == nil, "sessions": s.managed != nil})
}

func (s *Server) pushSubscribe(w http.ResponseWriter, r *http.Request, session browserSession) {
	var sub webpush.Subscription
	if !readJSON(w, r, &sub) {
		return
	}
	if err := sub.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_subscription", "This browser's push subscription was not accepted: "+err.Error()+".")
		return
	}
	_, err := s.db.SQL().ExecContext(r.Context(), `INSERT INTO web_push_subscriptions(device_id,origin,endpoint,p256dh,auth,created_at) VALUES(?,?,?,?,?,?)
 ON CONFLICT(device_id) DO UPDATE SET endpoint=excluded.endpoint,p256dh=excluded.p256dh,auth=excluded.auth,created_at=excluded.created_at`,
		session.Device.ID, s.origin, sub.Endpoint, sub.Keys.P256dh, sub.Keys.Auth, s.now().Unix())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "push_unavailable", "Notifications could not be turned on.")
		return
	}
	s.log.Info("web.push_subscribed")
	writeJSON(w, http.StatusOK, map[string]bool{"subscribed": true})
}

func (s *Server) pushUnsubscribe(w http.ResponseWriter, r *http.Request, session browserSession) {
	if _, err := s.db.SQL().ExecContext(r.Context(), `DELETE FROM web_push_subscriptions WHERE device_id=? AND origin=?`, session.Device.ID, s.origin); err != nil {
		writeError(w, http.StatusServiceUnavailable, "push_unavailable", "Notifications could not be turned off.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"subscribed": false})
}

func (s *Server) pushTest(w http.ResponseWriter, r *http.Request, session browserSession) {
	subs, err := s.pushSubscriptions(r.Context(), session.Device.ID)
	if err != nil || len(subs) == 0 {
		writeError(w, http.StatusConflict, "not_subscribed", "Turn on notifications for this browser first.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := s.deliver(ctx, subs[0], notification{Title: "Onibi notifications are on", Body: "You'll hear from your Mac when a session needs you.", Tag: "onibi-test", URL: "/"}); err != nil {
		writeError(w, http.StatusBadGateway, "push_failed", "The push service did not accept the test notification. Turn notifications off and on again.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"sent": true})
}

type storedSubscription struct {
	deviceID string
	sub      webpush.Subscription
}

// pushSubscriptions returns subscriptions of devices whose grant is still
// valid. An empty deviceID means every device.
func (s *Server) pushSubscriptions(ctx context.Context, deviceID string) ([]storedSubscription, error) {
	rows, err := s.db.SQL().QueryContext(ctx, `SELECT p.device_id,p.endpoint,p.p256dh,p.auth FROM web_push_subscriptions p
 JOIN web_devices d ON d.id=p.device_id AND d.origin=p.origin
 WHERE p.origin=? AND d.expires_at>? AND (?='' OR p.device_id=?)`, s.origin, s.now().Unix(), deviceID, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storedSubscription
	for rows.Next() {
		var st storedSubscription
		if err := rows.Scan(&st.deviceID, &st.sub.Endpoint, &st.sub.Keys.P256dh, &st.sub.Keys.Auth); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Server) deliver(ctx context.Context, st storedSubscription, n notification) error {
	payload, err := json.Marshal(n)
	if err != nil {
		return err
	}
	err = s.pushSend(ctx, st.sub, payload)
	if errors.Is(err, webpush.ErrGone) {
		_, _ = s.db.SQL().ExecContext(context.WithoutCancel(ctx), `DELETE FROM web_push_subscriptions WHERE device_id=? AND endpoint=?`, st.deviceID, st.sub.Endpoint)
		s.log.Info("web.push_subscription_gone")
	} else if err != nil {
		s.log.Warn("web.push_failed", "err", err)
	}
	return err
}

type attentionMark struct {
	state string
	since time.Time
}

// notifier remembers what it last saw so only a change into input or bell
// is announced. Its first poll is a baseline: states that were already
// waiting when it started are not announced.
type notifier struct {
	baseline bool
	seen     map[string]attentionMark
	lastSent map[string]time.Time
}

// RunNotifications watches managed sessions while any browser is subscribed.
func (s *Server) RunNotifications(ctx context.Context) {
	if s.managed == nil {
		return
	}
	n := &notifier{seen: map[string]attentionMark{}, lastSent: map[string]time.Time{}}
	ticker := time.NewTicker(notifyPollInterval)
	defer ticker.Stop()
	for {
		s.pollNotifications(ctx, n)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) pollNotifications(ctx context.Context, n *notifier) {
	subs, err := s.pushSubscriptions(ctx, "")
	if err != nil || len(subs) == 0 {
		n.baseline = false
		return
	}
	listCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	sessions, err := s.managed.Sessions(listCtx)
	cancel()
	if err != nil {
		return
	}
	now := s.now()
	live := map[string]bool{}
	for _, session := range sessions {
		live[session.ID] = true
		mark := attentionMark{session.Attention.State, session.Attention.Since}
		previous := n.seen[session.ID]
		n.seen[session.ID] = mark
		if !n.baseline || mark == previous || (mark.state != "input" && mark.state != "bell") {
			continue
		}
		if now.Sub(n.lastSent[session.ID]) < notifyCooldown {
			continue
		}
		n.lastSent[session.ID] = now
		name := strings.TrimSpace(session.Name)
		if name == "" {
			name = "A session"
		}
		msg := notification{Title: name + " needs input", Body: session.Attention.Reason, Tag: "onibi-session-" + session.ID, URL: "/?session=" + session.ID}
		if mark.state == "bell" {
			msg.Title, msg.Body = name+" rang the bell", "Terminal bell"
		}
		for _, st := range subs {
			sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			_ = s.deliver(sendCtx, st, msg)
			cancel()
		}
	}
	for id := range n.seen {
		if !live[id] {
			delete(n.seen, id)
			delete(n.lastSent, id)
		}
	}
	n.baseline = true
}
