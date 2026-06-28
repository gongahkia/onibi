package discord

import (
	"os"
	"testing"

	"github.com/gongahkia/onibi/internal/liveartifact"
)

func TestLiveDiscord(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_DISCORD") != "1" {
		t.Skip("set ONIBI_LIVE_DISCORD=1")
	}
	envs := []string{"ONIBI_DISCORD_TOKEN", "ONIBI_DISCORD_CHANNEL_ID", "ONIBI_DISCORD_APPLICATION_ID", "ONIBI_DISCORD_GUILD_ID"}
	rec, err := liveartifact.New("discord", envs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rec.Close(envs...); err != nil {
			t.Errorf("artifact: %v", err)
		}
		t.Logf("artifact: %s", rec.Path())
	})
	channel := os.Getenv("ONIBI_DISCORD_CHANNEL_ID")
	if channel == "" {
		t.Fatal("ONIBI_DISCORD_CHANNEL_ID required")
	}
	c := New(os.Getenv("ONIBI_DISCORD_TOKEN"))
	if _, err := c.CurrentApplication(t.Context()); err != nil {
		rec.Error("current-application", err)
		t.Fatal(err)
	}
	rec.Record("current-application", map[string]any{"ok": true})
	if _, err := c.Channel(t.Context(), channel); err != nil {
		rec.Error("channel", err)
		t.Fatal(err)
	}
	rec.Record("channel", map[string]any{"channel_id": channel})
	if err := c.CreateMessage(t.Context(), channel, "onibi live discord smoke"); err != nil {
		rec.Error("create-message", err)
		t.Fatal(err)
	}
	rec.Record("create-message", map[string]any{"ok": true})
}
