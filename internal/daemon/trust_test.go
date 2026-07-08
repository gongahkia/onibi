package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/trust"
	"github.com/gongahkia/onibi/internal/web"
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

func TestTrustAutoApproveBypassesApprovalCard(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".onibi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".onibi", "trust.toml"), []byte(`[[rule]]
effect = "auto_approve"
expires = "never"
[rule.match]
tool = "Edit"
path = "src/**/*.go"
agent = "claude"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := trust.NewWatcher(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.AddRoot(root); err != nil {
		t.Fatal(err)
	}
	d.Trust = w
	s := NewSession("s1", "claude", "claude", nil, 0)
	s.CWD = root
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	events, unsub, err := d.Queue.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()
	toasts, unsubToasts := d.Events.Subscribe()
	defer unsubToasts()
	resp, err := d.handleApprovalRequest(t.Context(), intake.Event{
		Type:      intake.TypeApprovalRequest,
		Session:   "s1",
		Managed:   true,
		Agent:     "claude",
		Tool:      "Edit",
		InputJSON: `{"file_path":"src/main.go","old_string":"a","new_string":"b"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Decision != string(approval.VerdictApprove) {
		t.Fatalf("response = %#v", resp)
	}
	ev := readTrustApprovalEvent(t, events)
	if ev.Type == approval.EventRequested {
		t.Fatalf("unexpected requested event = %#v", ev)
	}
	if ev.Type != approval.EventDecided || ev.Decision.Verdict != approval.VerdictApprove {
		t.Fatalf("approval event = %#v", ev)
	}
	toast := readTrustWebEvent(t, toasts)
	if toast.Type != "toast" {
		t.Fatalf("toast = %#v", toast)
	}
	audit, err := db.AuditRecent(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || audit[0].Action != "trust.auto_approve" || !strings.Contains(audit[0].Detail, "rule=file:1") || !strings.Contains(audit[0].Detail, "path=src/main.go") {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestAddRuntimeTrustRuleAddsEphemeralRule(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	root := t.TempDir()
	w, err := trust.NewWatcher(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.AddRoot(root); err != nil {
		t.Fatal(err)
	}
	d.Trust = w
	s := NewSession("s1", "claude", "claude", nil, 0)
	s.CWD = root
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	toasts, unsubToasts := d.Events.Subscribe()
	defer unsubToasts()
	msg, err := d.AddRuntimeTrustRule(t.Context(), web.TrustRuntimeRequest{
		SessionID: "s1",
		Tool:      "Edit",
		Path:      filepath.Join(root, "src", "**"),
		Agent:     "claude",
		Expires:   "5m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "src/**") {
		t.Fatalf("message = %q", msg)
	}
	p, ok := w.Policy(root)
	if !ok || len(p.Rules) != 1 {
		t.Fatalf("policy = %#v ok=%v", p, ok)
	}
	got, ok := p.Evaluate(trust.Request{Tool: "Edit", Path: "src/main.go", Agent: "claude"})
	if !ok || !got.Runtime || got.Effect != trust.EffectAutoApprove || got.Expires != 5*time.Minute {
		t.Fatalf("rule = %#v ok=%v", got, ok)
	}
	toast := readTrustWebEvent(t, toasts)
	if toast.Type != "toast" {
		t.Fatalf("toast = %#v", toast)
	}
	audit, err := db.AuditRecent(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || audit[0].Action != "trust.runtime.add" || !strings.Contains(audit[0].Detail, "path=src/**") {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestTrustRPCRoundTripsRuntimeRules(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	root := t.TempDir()
	w, err := trust.NewWatcher(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	d.Trust = w
	add, err := d.handleTrustRPC(t.Context(), intake.Event{
		Type:        intake.TypeTrust,
		TrustAction: "add",
		TrustRoot:   root,
		Tool:        "Edit",
		FilePath:    "src/**",
		Agent:       "claude",
		Expires:     "5m",
	})
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(strings.TrimPrefix(add.Text, "added "))
	if !strings.HasPrefix(id, "runtime:") {
		t.Fatalf("add = %#v", add)
	}
	list, err := d.handleTrustRPC(t.Context(), intake.Event{Type: intake.TypeTrust, TrustAction: "list", TrustRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	view := decodeTrustView(t, list.Text)
	if len(view.Roots) != 1 || len(view.Roots[0].Rules) != 1 || view.Roots[0].Rules[0].ID != id || view.Roots[0].Rules[0].Source != "runtime" {
		t.Fatalf("view = %#v", view)
	}
	persist, err := d.handleTrustRPC(t.Context(), intake.Event{Type: intake.TypeTrust, TrustAction: "persist", TrustRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if persist.Text != "persisted 1 runtime rule(s)" {
		t.Fatalf("persist = %#v", persist)
	}
	disk, err := trust.Load(trust.PolicyPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(disk.Rules) != 1 || disk.Rules[0].Runtime || disk.Rules[0].Match.Path != "src/**" {
		t.Fatalf("disk = %#v", disk)
	}
	list, err = d.handleTrustRPC(t.Context(), intake.Event{Type: intake.TypeTrust, TrustAction: "list", TrustRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	view = decodeTrustView(t, list.Text)
	if len(view.Roots[0].Rules) != 1 || view.Roots[0].Rules[0].ID != "file:1" || view.Roots[0].Rules[0].Source != "file" {
		t.Fatalf("view after persist = %#v", view)
	}
	if _, err := d.handleTrustRPC(t.Context(), intake.Event{Type: intake.TypeTrust, TrustAction: "remove", TrustRoot: root, TrustRuleID: "file:1"}); err != nil {
		t.Fatal(err)
	}
	disk, err = trust.Load(trust.PolicyPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(disk.Rules) != 0 {
		t.Fatalf("disk after remove = %#v", disk)
	}
}

func decodeTrustView(t *testing.T, text string) trust.View {
	t.Helper()
	var view trust.View
	if err := json.Unmarshal([]byte(text), &view); err != nil {
		t.Fatal(err)
	}
	return view
}

func readTrustApprovalEvent(t *testing.T, events <-chan approval.Event) approval.Event {
	t.Helper()
	select {
	case ev := <-events:
		return ev
	case <-time.After(time.Second):
		t.Fatal("approval event not delivered")
		return approval.Event{}
	}
}

func readTrustWebEvent(t *testing.T, events <-chan web.Event) web.Event {
	t.Helper()
	timeout := time.After(time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Type == "session.activity" {
				continue
			}
			return ev
		case <-timeout:
			t.Fatal("web event not delivered")
			return web.Event{}
		}
	}
}
