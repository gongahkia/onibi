package cli

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/store"
)

func TestStoreRekeyCommandKeepsDevicesReadable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, db, err := openCLIStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutWebSession(context.Background(), "cookie-value", "iPhone", time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	out, _ := executeRoot(t, "store", "rekey", "--json", "--color", "never")
	var rekeyed struct {
		Rekeyed bool `json:"rekeyed"`
	}
	if err := json.Unmarshal(out.Bytes(), &rekeyed); err != nil {
		t.Fatal(err)
	}
	if !rekeyed.Rekeyed {
		t.Fatalf("rekey output = %q", out.String())
	}
	_, db, err = openCLIStore()
	if err != nil {
		t.Fatal(err)
	}
	session, ok, err := db.WebSession(context.Background(), "cookie-value")
	_ = db.Close()
	if err != nil || !ok || !session.Revoked || session.RevokedReason != store.WebSessionReasonStoreRekey {
		t.Fatalf("session ok=%v err=%v session=%#v", ok, err, session)
	}
	out, _ = executeRoot(t, "devices", "--json", "--color", "never")
	if !json.Valid(out.Bytes()) {
		t.Fatalf("devices output is not JSON: %q", out.String())
	}
}

func TestStoreRekeyDryRunReportsImpact(t *testing.T) {
	withDefaultState(t)
	_, db, err := openCLIStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutPairingToken(context.Background(), "pair-token", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := db.PutWebSession(context.Background(), "owner-cookie", "Mac", time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	out, _ := executeRoot(t, "store", "rekey", "--dry-run", "--json", "--color", "never")
	var got struct {
		DryRun                 bool   `json:"dry_run"`
		ActiveWebSessions      int    `json:"active_web_sessions"`
		WebSessionsToRevoke    int    `json:"web_sessions_to_revoke"`
		WebSessionsToReseal    int    `json:"web_sessions_to_reseal"`
		PairingTokensToReseal  int    `json:"pairing_tokens_to_reseal"`
		WebsocketCloseReason   string `json:"websocket_close_reason"`
		StoreKeyWouldBeRotated bool   `json:"store_key_would_be_rotated"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.DryRun || got.ActiveWebSessions != 1 || got.WebSessionsToRevoke != 1 || got.WebSessionsToReseal != 1 || got.PairingTokensToReseal != 1 || got.WebsocketCloseReason != store.WebSessionReasonStoreRekey || !got.StoreKeyWouldBeRotated {
		t.Fatalf("dry-run output = %#v", got)
	}
	_, db, err = openCLIStore()
	if err != nil {
		t.Fatal(err)
	}
	session, ok, err := db.WebSession(context.Background(), "owner-cookie")
	_ = db.Close()
	if err != nil || !ok || session.Revoked {
		t.Fatalf("dry-run mutated session ok=%v err=%v session=%#v", ok, err, session)
	}
}

func TestPushRotateCommandInvalidatesSubscriptions(t *testing.T) {
	withDefaultState(t)
	_, db, err := openCLIStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutPushSubscription(context.Background(), "https://push.example.invalid/sub/1", "p-key", "a-key", time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	out, _ := executeRoot(t, "push", "rotate", "--json", "--color", "never")
	var got struct {
		Rotated                  bool   `json:"rotated"`
		PublicKey                string `json:"public_key"`
		SubscriptionsInvalidated int64  `json:"subscriptions_invalidated"`
		ResubscribeCommand       string `json:"resubscribe_command"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Rotated || got.PublicKey == "" || got.SubscriptionsInvalidated != 1 || got.ResubscribeCommand == "" {
		t.Fatalf("push rotate output = %#v", got)
	}
	_, db, err = openCLIStore()
	if err != nil {
		t.Fatal(err)
	}
	subs, err := db.PushSubscriptions(context.Background())
	_ = db.Close()
	if err != nil || len(subs) != 0 {
		t.Fatalf("subscriptions after rotate = %#v err=%v", subs, err)
	}
}
