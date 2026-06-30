package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/store"
)

const (
	pushVAPIDPrivateKey = "push_vapid_priv_enc"
	pushVAPIDPublicKey  = "push_vapid_pub"
)

var pushVAPIDMu sync.Mutex

var sendWebPushNotification = webpush.SendNotificationWithContext

type VAPIDKeys struct {
	PrivateKey string
	PublicKey  string
}

type pushSubscribeRequest struct {
	Endpoint string               `json:"endpoint"`
	Keys     pushSubscriptionKeys `json:"keys"`
}

type pushSubscriptionKeys struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}

// EnsureVAPIDKeys returns the stored VAPID keypair, creating it once if absent.
func EnsureVAPIDKeys(ctx context.Context, db *store.DB) (VAPIDKeys, error) {
	if db == nil {
		return VAPIDKeys{}, errors.New("db unavailable")
	}
	pushVAPIDMu.Lock()
	defer pushVAPIDMu.Unlock()
	privateKey, privateOK, err := db.KVGetEncryptedString(ctx, pushVAPIDPrivateKey)
	if err != nil {
		return VAPIDKeys{}, err
	}
	publicKey, publicOK, err := db.KVGetString(ctx, pushVAPIDPublicKey)
	if err != nil {
		return VAPIDKeys{}, err
	}
	if privateOK && publicOK && strings.TrimSpace(privateKey) != "" && strings.TrimSpace(publicKey) != "" {
		return VAPIDKeys{PrivateKey: privateKey, PublicKey: publicKey}, nil
	}
	privateKey, publicKey, err = webpush.GenerateVAPIDKeys()
	if err != nil {
		return VAPIDKeys{}, err
	}
	if err := db.KVSetEncryptedString(ctx, pushVAPIDPrivateKey, privateKey); err != nil {
		return VAPIDKeys{}, err
	}
	if err := db.KVSetString(ctx, pushVAPIDPublicKey, publicKey); err != nil {
		return VAPIDKeys{}, err
	}
	return VAPIDKeys{PrivateKey: privateKey, PublicKey: publicKey}, nil
}

func (s *Server) handlePushVAPIDPublicKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.requireHTTPAuth(w, r); !ok {
		return
	}
	keys, err := EnsureVAPIDKeys(r.Context(), s.db)
	if err != nil {
		s.log.Warn("web push vapid key failed", "request_id", requestID(r), "err", err)
		http.Error(w, "push key unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"key": keys.PublicKey})
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerSessionID, ok := s.requireHTTPAuth(w, r)
	if !ok {
		return
	}
	if s.db == nil {
		http.Error(w, "db unavailable", http.StatusServiceUnavailable)
		return
	}
	var req pushSubscribeRequest
	if !s.readJSONBodyLimit(w, r, ownerSessionID, &req, 16<<10) {
		return
	}
	if err := s.db.PutPushSubscription(r.Context(), req.Endpoint, req.Keys.P256dh, req.Keys.Auth, time.Now()); err != nil {
		s.log.Warn("web push subscribe failed", "request_id", requestID(r), "err", err)
		http.Error(w, "bad push subscription", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func SendApprovalPushNotifications(ctx context.Context, db *store.DB, a *approval.Approval, log *slog.Logger) {
	if db == nil || a == nil {
		return
	}
	subs, err := db.PushSubscriptions(ctx)
	if err != nil {
		logPushWarn(log, "web push subscriptions unavailable", "err", err)
		return
	}
	if len(subs) == 0 {
		return
	}
	keys, err := EnsureVAPIDKeys(ctx, db)
	if err != nil {
		logPushWarn(log, "web push vapid unavailable", "err", err)
		return
	}
	payload := approvalEventPayload(approval.Event{Type: approval.EventRequested, Approval: *a})
	payload["approval_id"] = a.ID
	payload["title"] = "Onibi approval"
	payload["body"] = fmt.Sprintf("%s requests %s", a.Agent, a.Tool)
	body, err := json.Marshal(payload)
	if err != nil {
		logPushWarn(log, "web push payload failed", "approval_id", a.ID, "err", err)
		return
	}
	for _, sub := range subs {
		resp, err := sendWebPushNotification(ctx, body, &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys: webpush.Keys{
				P256dh: sub.P256dh,
				Auth:   sub.Auth,
			},
		}, &webpush.Options{
			Subscriber:      "mailto:owner@onibi.local",
			VAPIDPublicKey:  keys.PublicKey,
			VAPIDPrivateKey: keys.PrivateKey,
			TTL:             30,
			Urgency:         webpush.UrgencyHigh,
		})
		if err != nil {
			logPushWarn(log, "web push send failed", "approval_id", a.ID, "endpoint", sub.Endpoint, "err", err)
			continue
		}
		closePushResponse(resp)
		if resp == nil {
			continue
		}
		if resp.StatusCode == http.StatusGone {
			if err := db.DeletePushSubscription(ctx, sub.Endpoint); err != nil {
				logPushWarn(log, "web push subscription delete failed", "approval_id", a.ID, "endpoint", sub.Endpoint, "err", err)
			}
		}
	}
}

func closePushResponse(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func logPushWarn(log *slog.Logger, msg string, args ...any) {
	if log != nil {
		log.Warn(msg, args...)
	}
}
