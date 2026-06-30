package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/web"
)

func TestAddAnomalyAllowlistRuleAppendsFile(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	root := t.TempDir()
	s := NewSession("s1", "claude", "claude", nil, 0)
	s.CWD = root
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	toasts, unsubToasts := d.Events.Subscribe()
	defer unsubToasts()
	evidence := "fork bomb pattern\nsecret=abc123"
	msg, err := d.AddAnomalyAllowlistRule(t.Context(), web.AnomalyAllowlistRequest{
		SessionID: "s1",
		RuleName:  "fork-bomb",
		Evidence:  evidence,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "fork-bomb") {
		t.Fatalf("message = %q", msg)
	}
	data, err := os.ReadFile(filepath.Join(root, ".onibi", "anomaly-allowlist.toml"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(evidence))
	body := string(data)
	if !strings.Contains(body, "[[allow]]") || !strings.Contains(body, `rule_name = "fork-bomb"`) || !strings.Contains(body, `session_id = "s1"`) {
		t.Fatalf("allowlist = %q", body)
	}
	if !strings.Contains(body, `evidence_sha256 = "`+hex.EncodeToString(sum[:])+`"`) || strings.Contains(body, "secret=abc123") {
		t.Fatalf("allowlist evidence = %q", body)
	}
	select {
	case ev := <-toasts:
		if ev.Type != "toast" {
			t.Fatalf("toast = %#v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("toast not published")
	}
	audit, err := db.AuditRecent(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || audit[0].Action != "anomaly.allowlist.add" || !strings.Contains(audit[0].Detail, "rule=fork-bomb") {
		t.Fatalf("audit = %#v", audit)
	}
}
