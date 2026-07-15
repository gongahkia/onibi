package trust

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherReloadsTrustTomlWithinOneSecond(t *testing.T) {
	root := t.TempDir()
	onibiDir := filepath.Join(root, ".onibi")
	if err := os.Mkdir(onibiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(onibiDir, "trust.toml"), []byte(policyText("deny")), 0o600); err != nil {
		t.Fatal(err)
	}
	events := make(chan WatchEvent, 8)
	w, err := NewWatcher(func(ev WatchEvent) {
		if !ev.Initial {
			events <- ev
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.Run(ctx)
	if err := w.AddRoot(root); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := os.WriteFile(filepath.Join(onibiDir, "trust.toml"), []byte(policyText("auto_approve")), 0o600); err != nil {
		t.Fatal(err)
	}
	ev := waitWatchEvent(t, events, func(ev WatchEvent) bool { return ev.Err == nil && len(ev.Policy.Rules) == 1 })
	if time.Since(started) > time.Second {
		t.Fatalf("reload took %s", time.Since(started))
	}
	if ev.Policy.Rules[0].Effect != EffectAutoApprove {
		t.Fatalf("effect = %s", ev.Policy.Rules[0].Effect)
	}
}

func TestWatcherKeepsOldPolicyOnBadToml(t *testing.T) {
	root := t.TempDir()
	onibiDir := filepath.Join(root, ".onibi")
	if err := os.Mkdir(onibiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(onibiDir, "trust.toml"), []byte(policyText("deny")), 0o600); err != nil {
		t.Fatal(err)
	}
	events := make(chan WatchEvent, 8)
	w, err := NewWatcher(func(ev WatchEvent) {
		if !ev.Initial {
			events <- ev
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.Run(ctx)
	if err := w.AddRoot(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(onibiDir, "trust.toml"), []byte(policyText("auto_approve")), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = waitWatchEvent(t, events, func(ev WatchEvent) bool { return ev.Err == nil && ev.Policy.Rules[0].Effect == EffectAutoApprove })
	if err := os.WriteFile(filepath.Join(onibiDir, "trust.toml"), []byte("[[rule]]\neffect ="), 0o600); err != nil {
		t.Fatal(err)
	}
	ev := waitWatchEvent(t, events, func(ev WatchEvent) bool { return ev.Err != nil })
	if ev.Policy.Rules[0].Effect != EffectAutoApprove {
		t.Fatalf("event policy = %s", ev.Policy.Rules[0].Effect)
	}
	got, ok := w.Policy(root)
	if !ok || got.Rules[0].Effect != EffectAutoApprove {
		t.Fatalf("stored policy = %#v ok=%v", got, ok)
	}
}

func TestWatcherRuntimeRuleWithRawExpiryDecays(t *testing.T) {
	root := t.TempDir()
	w, err := NewWatcher(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.AddRoot(root); err != nil {
		t.Fatal(err)
	}
	if err := w.AddRuntimeRule(root, Rule{
		Effect:     EffectAutoApprove,
		ExpiresRaw: "5m",
		Match:      Match{Tool: "Edit", Path: "src/**"},
	}); err != nil {
		t.Fatal(err)
	}
	p, ok := w.Policy(root)
	if !ok || len(p.Rules) != 1 || p.Rules[0].ExpiresAt.IsZero() {
		t.Fatalf("policy = %#v ok=%v", p, ok)
	}
	req := Request{Tool: "Edit", Path: "src/main.go"}
	if _, ok := p.EvaluateAt(req, p.Rules[0].ExpiresAt.Add(-time.Nanosecond)); !ok {
		t.Fatal("runtime rule did not match before expiry")
	}
	if _, ok := p.EvaluateAt(req, p.Rules[0].ExpiresAt); ok {
		t.Fatal("runtime rule matched at expiry")
	}
}

func TestPersistRuntimeRuleReloadsAsOneFileRule(t *testing.T) {
	root := t.TempDir()
	w, err := NewWatcher(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.AddRoot(root); err != nil {
		t.Fatal(err)
	}
	rule := RuntimeRule(Match{Tool: "Edit", Path: "src/**", Agent: "pi"}, EffectAutoApprove, 5*time.Minute, time.Now())
	if err := w.AddRuntimeRule(root, rule); err != nil {
		t.Fatal(err)
	}
	n, err := w.PersistRuntimeRules(root)
	if err != nil || n != 1 {
		t.Fatalf("persist n=%d err=%v", n, err)
	}
	p, ok := w.Policy(root)
	if !ok || len(p.Rules) != 1 || p.Rules[0].Runtime || p.Rules[0].ExpiresAt.IsZero() {
		t.Fatalf("policy = %#v ok=%v", p, ok)
	}
	evaluation := p.Explain(Request{Tool: "Edit", Path: "src/main.go", Agent: "pi"})
	if !evaluation.Matched || evaluation.Rule == nil || evaluation.Rule.ID != "file:1" {
		t.Fatalf("evaluation = %#v", evaluation)
	}
}

func TestWatcherRetainsExpiredFilePositionsInTrace(t *testing.T) {
	root := t.TempDir()
	onibiDir := filepath.Join(root, ".onibi")
	if err := os.Mkdir(onibiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(onibiDir, "trust.toml")
	if err := os.WriteFile(path, []byte(`[[rule]]
effect = "deny"
expires = "1ns"
[rule.match]
tool = "Bash"

[[rule]]
effect = "auto_approve"
expires = "never"
[rule.match]
tool = "Edit"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	w, err := NewWatcher(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.AddRoot(root); err != nil {
		t.Fatal(err)
	}
	p, ok := w.Policy(root)
	if !ok || len(p.Rules) != 2 {
		t.Fatalf("policy = %#v ok=%v", p, ok)
	}
	evaluation := p.Explain(Request{Tool: "Edit"})
	if !evaluation.Matched || evaluation.Rule == nil || evaluation.Rule.ID != "file:2" || len(evaluation.Trace) != 2 || evaluation.Trace[0].Outcome != "expired" {
		t.Fatalf("evaluation = %#v", evaluation)
	}
}

func waitWatchEvent(t *testing.T, events <-chan WatchEvent, want func(WatchEvent) bool) WatchEvent {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case ev := <-events:
			if want(ev) {
				return ev
			}
		case <-deadline:
			t.Fatal("watch event timed out")
		}
	}
}

func policyText(effect string) string {
	return `[[rule]]
effect = "` + effect + `"
expires = "never"
[rule.match]
tool = "Edit"
`
}
