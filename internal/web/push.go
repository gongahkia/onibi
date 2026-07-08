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
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
)

const (
	PushVAPIDSecretName = "onibi.push.vapid.v1"
	pushVAPIDPrivateKey = "push_vapid_priv_enc"
	pushVAPIDPublicKey  = "push_vapid_pub"
)

var pushVAPIDMu sync.Mutex

var sendWebPushNotification = webpush.SendNotificationWithContext
var openPushSecretStore = secrets.OpenDefault

type VAPIDKeys struct {
	PrivateKey string
	PublicKey  string
}

type VAPIDDiagnostics struct {
	SecretPresent        bool
	SQLitePublicPresent  bool
	PublicKeyMatches     bool
	LegacyPrivatePresent bool
	SubscriptionCount    int
}

type vapidSecret struct {
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
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
	keys, ok, err := loadVAPIDKeys(ctx)
	if err != nil {
		return VAPIDKeys{}, err
	}
	if ok {
		if err := syncVAPIDPublicKey(ctx, db, keys); err != nil {
			return VAPIDKeys{}, err
		}
		return keys, nil
	}
	keys, ok, err = loadLegacyVAPIDKeys(ctx, db)
	if err != nil {
		return VAPIDKeys{}, err
	}
	if ok {
		if err := saveVAPIDKeys(ctx, keys); err != nil {
			return VAPIDKeys{}, err
		}
		if err := syncVAPIDPublicKey(ctx, db, keys); err != nil {
			return VAPIDKeys{}, err
		}
		_ = db.KVDel(ctx, pushVAPIDPrivateKey)
		return keys, nil
	}
	keys, _, err = rotateVAPIDKeysLocked(ctx, db)
	return keys, err
}

func RotateVAPIDKeys(ctx context.Context, db *store.DB) (VAPIDKeys, int64, error) {
	if db == nil {
		return VAPIDKeys{}, 0, errors.New("db unavailable")
	}
	pushVAPIDMu.Lock()
	defer pushVAPIDMu.Unlock()
	return rotateVAPIDKeysLocked(ctx, db)
}

func VAPIDDiagnosticsForDB(ctx context.Context, db *store.DB) (VAPIDDiagnostics, error) {
	if db == nil {
		return VAPIDDiagnostics{}, errors.New("db unavailable")
	}
	keys, secretOK, err := loadVAPIDKeys(ctx)
	if err != nil {
		return VAPIDDiagnostics{}, err
	}
	publicKey, publicOK, err := db.KVGetString(ctx, pushVAPIDPublicKey)
	if err != nil {
		return VAPIDDiagnostics{}, err
	}
	_, legacyOK, err := db.KVGetEncryptedString(ctx, pushVAPIDPrivateKey)
	if err != nil {
		return VAPIDDiagnostics{}, err
	}
	subs, err := db.PushSubscriptions(ctx)
	if err != nil {
		return VAPIDDiagnostics{}, err
	}
	return VAPIDDiagnostics{
		SecretPresent:        secretOK,
		SQLitePublicPresent:  publicOK && strings.TrimSpace(publicKey) != "",
		PublicKeyMatches:     secretOK && publicOK && strings.TrimSpace(publicKey) == keys.PublicKey,
		LegacyPrivatePresent: legacyOK,
		SubscriptionCount:    len(subs),
	}, nil
}

func rotateVAPIDKeysLocked(ctx context.Context, db *store.DB) (VAPIDKeys, int64, error) {
	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return VAPIDKeys{}, 0, err
	}
	keys := VAPIDKeys{PrivateKey: privateKey, PublicKey: publicKey}
	if err := saveVAPIDKeys(ctx, keys); err != nil {
		return VAPIDKeys{}, 0, err
	}
	if err := syncVAPIDPublicKey(ctx, db, keys); err != nil {
		return VAPIDKeys{}, 0, err
	}
	_ = db.KVDel(ctx, pushVAPIDPrivateKey)
	n, err := db.DeletePushSubscriptions(ctx)
	if err != nil {
		return VAPIDKeys{}, 0, err
	}
	return keys, n, nil
}

func loadVAPIDKeys(ctx context.Context) (VAPIDKeys, bool, error) {
	if err := ctx.Err(); err != nil {
		return VAPIDKeys{}, false, err
	}
	st, err := openPushSecretStore()
	if err != nil {
		return VAPIDKeys{}, false, err
	}
	raw, ok, err := st.Get(PushVAPIDSecretName)
	if err != nil || !ok {
		return VAPIDKeys{}, ok, err
	}
	var secret vapidSecret
	if err := json.Unmarshal([]byte(raw), &secret); err != nil {
		return VAPIDKeys{}, true, err
	}
	keys := VAPIDKeys{PrivateKey: strings.TrimSpace(secret.PrivateKey), PublicKey: strings.TrimSpace(secret.PublicKey)}
	if keys.PrivateKey == "" || keys.PublicKey == "" {
		return VAPIDKeys{}, true, errors.New("stored VAPID keypair is incomplete")
	}
	return keys, true, ctx.Err()
}

func loadLegacyVAPIDKeys(ctx context.Context, db *store.DB) (VAPIDKeys, bool, error) {
	privateKey, privateOK, err := db.KVGetEncryptedString(ctx, pushVAPIDPrivateKey)
	if err != nil {
		return VAPIDKeys{}, false, err
	}
	publicKey, publicOK, err := db.KVGetString(ctx, pushVAPIDPublicKey)
	if err != nil {
		return VAPIDKeys{}, false, err
	}
	keys := VAPIDKeys{PrivateKey: strings.TrimSpace(privateKey), PublicKey: strings.TrimSpace(publicKey)}
	if !privateOK || !publicOK || keys.PrivateKey == "" || keys.PublicKey == "" {
		return VAPIDKeys{}, false, nil
	}
	return keys, true, nil
}

func saveVAPIDKeys(ctx context.Context, keys VAPIDKeys) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(keys.PrivateKey) == "" || strings.TrimSpace(keys.PublicKey) == "" {
		return errors.New("VAPID keypair is incomplete")
	}
	st, err := openPushSecretStore()
	if err != nil {
		return err
	}
	body, err := json.Marshal(vapidSecret{PrivateKey: keys.PrivateKey, PublicKey: keys.PublicKey})
	if err != nil {
		return err
	}
	if err := st.Set(PushVAPIDSecretName, string(body)); err != nil {
		return err
	}
	return ctx.Err()
}

func syncVAPIDPublicKey(ctx context.Context, db *store.DB, keys VAPIDKeys) error {
	if strings.TrimSpace(keys.PublicKey) == "" {
		return errors.New("VAPID public key is empty")
	}
	return db.KVSetString(ctx, pushVAPIDPublicKey, keys.PublicKey)
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
	if !s.requireCSRF(w, r, ownerSessionID) {
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
