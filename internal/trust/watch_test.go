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
