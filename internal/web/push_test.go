package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/envelope"
	"github.com/gongahkia/onibi/internal/store"
)

func TestPushVAPIDKeysSurviveRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "onibi.db")
	key, err := envelope.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(dbPath, store.WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	first, err := EnsureVAPIDKeys(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if first.PrivateKey == "" || first.PublicKey == "" {
		t.Fatalf("empty keys = %#v", first)
	}
	_ = db.Close()

	db, err = store.Open(dbPath, store.WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	second, err := EnsureVAPIDKeys(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("keys changed after restart: %#v != %#v", second, first)
	}
}

func TestPushVAPIDPublicKeyEndpoint(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/push/vapid-public-key", nil)
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	var resp struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Key == "" {
		t.Fatalf("response = %q", w.Body.String())
	}
	stored, ok, err := srv.db.KVGetString(context.Background(), pushVAPIDPublicKey)
	if err != nil || !ok || stored != resp.Key {
		t.Fatalf("stored key = %q ok=%v err=%v", stored, ok, err)
	}
}

func TestPushVAPIDPublicKeyRequiresOwner(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	req := httptest.NewRequest(http.MethodGet, "/push/vapid-public-key", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
}

func TestPushSubscribeStoresSubscription(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	body := `{"endpoint":"https://push.example.invalid/sub/1","keys":{"p256dh":"p-key","auth":"a-key"}}`
	req := httptest.NewRequest(http.MethodPost, "/push/subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(rr.Result().Cookies()[0])
	addCSRF(req, sessionID)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	subs, err := srv.db.PushSubscriptions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || subs[0].Endpoint != "https://push.example.invalid/sub/1" || subs[0].P256dh != "p-key" || subs[0].Auth != "a-key" {
		t.Fatalf("subscriptions = %#v", subs)
	}
}

func TestSendApprovalPushNotificationsDeletesGoneSubscription(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := EnsureVAPIDKeys(ctx, db); err != nil {
		t.Fatal(err)
	}
	endpoint := "https://push.example.invalid/sub/1"
	if err := db.PutPushSubscription(ctx, endpoint, "p-key", "a-key", testNow()); err != nil {
		t.Fatal(err)
	}
	oldSend := sendWebPushNotification
	t.Cleanup(func() { sendWebPushNotification = oldSend })
	var gotPayload map[string]any
	var gotSub *webpush.Subscription
	var gotOptions *webpush.Options
	sendWebPushNotification = func(_ context.Context, message []byte, sub *webpush.Subscription, opts *webpush.Options) (*http.Response, error) {
		gotSub = sub
		gotOptions = opts
		if err := json.Unmarshal(message, &gotPayload); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: http.StatusGone, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	SendApprovalPushNotifications(ctx, db, &approval.Approval{
		ID:        "ap1",
		SessionID: "s1",
		Agent:     "claude",
		Tool:      "Write",
		InputJSON: `{"file_path":"README.md","content":"x"}`,
	}, nil)
	if gotSub == nil || gotSub.Endpoint != endpoint || gotSub.Keys.P256dh != "p-key" || gotSub.Keys.Auth != "a-key" {
		t.Fatalf("subscription = %#v", gotSub)
	}
	if gotOptions == nil || gotOptions.Subscriber != "mailto:owner@onibi.local" || gotOptions.TTL != 30 || gotOptions.Urgency != webpush.UrgencyHigh || gotOptions.VAPIDPublicKey == "" || gotOptions.VAPIDPrivateKey == "" {
		t.Fatalf("options = %#v", gotOptions)
	}
	if gotPayload["approval_id"] != "ap1" || gotPayload["session_id"] != "s1" || gotPayload["tool"] != "Write" {
		t.Fatalf("payload = %#v", gotPayload)
	}
	subs, err := db.PushSubscriptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 0 {
		t.Fatalf("subscriptions after gone = %#v", subs)
	}
}

func testNow() time.Time {
	return time.Unix(100, 0)
}
