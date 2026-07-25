package irc

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/liveartifact"
)

func TestLiveIRC(t *testing.T) {
	envs := []string{"ONIBI_LIVE_IRC_NICK", "ONIBI_LIVE_IRC_ACCOUNT", "ONIBI_LIVE_IRC_PASSWORD", "ONIBI_LIVE_IRC_OWNER_NICK"}
	cfg := Config{Nick: os.Getenv(envs[0]), Account: os.Getenv(envs[1]), Password: os.Getenv(envs[2])}
	owner := os.Getenv(envs[3])
	if cfg.Nick == "" || cfg.Account == "" || cfg.Password == "" || owner == "" {
		t.Skip("set ONIBI_LIVE_IRC_NICK, ONIBI_LIVE_IRC_ACCOUNT, ONIBI_LIVE_IRC_PASSWORD, and ONIBI_LIVE_IRC_OWNER_NICK")
	}
	rec, err := liveartifact.New("irc", envs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rec.Close(envs...); err != nil {
			t.Errorf("artifact: %v", err)
		}
		t.Logf("artifact: %s", rec.Path())
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := NewClient(cfg)
	defer client.Close()
	if err := client.Connect(ctx); err != nil {
		rec.Error("connect", err)
		t.Fatal(err)
	}
	rec.Record("tls-sasl", map[string]any{"ok": true, "endpoint": DefaultHost + ":" + DefaultPort})
	if err := client.SendPrivmsg(ctx, owner, "onibi live irc smoke"); err != nil {
		rec.Error("send-private-message", err)
		t.Fatal(err)
	}
	rec.Record("send-private-message", map[string]any{"ok": true})
}
