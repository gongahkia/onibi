package daemon

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/trust"
)

func TestTrustWatchEventAuditsReloadAndPublishesErrorToast(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	events, unsub := d.Events.Subscribe()
	defer unsub()
	d.handleTrustWatchEvent(t.Context(), trust.WatchEvent{
		Root: "/repo",
		Path: "/repo/.onibi/trust.toml",
		Policy: trust.Policy{Rules: []trust.Rule{{
			Effect: trust.EffectDeny,
		}}},
	})
	audit, err := db.AuditRecent(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || audit[0].Action != "trust.policy.reload" || !strings.Contains(audit[0].Detail, "rules=0->1") {
		t.Fatalf("audit = %#v", audit)
	}
	d.handleTrustWatchEvent(t.Context(), trust.WatchEvent{
		Root: "/repo",
		Path: "/repo/.onibi/trust.toml",
		Err:  errors.New("bad TOML"),
	})
	select {
	case ev := <-events:
		if ev.Type != "toast" {
			t.Fatalf("event = %#v", ev)
		}
		payload, ok := ev.Payload.(map[string]any)
		if !ok || !strings.Contains(payload["message"].(string), "bad TOML") {
			t.Fatalf("payload = %#v", ev.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("toast not published")
	}
}
