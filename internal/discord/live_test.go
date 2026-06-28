package discord

import (
	"os"
	"testing"
)

func TestLiveDiscord(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_DISCORD") != "1" {
		t.Skip("set ONIBI_LIVE_DISCORD=1")
	}
	channel := os.Getenv("ONIBI_DISCORD_CHANNEL_ID")
	if channel == "" {
		t.Fatal("ONIBI_DISCORD_CHANNEL_ID required")
	}
	if err := New(os.Getenv("ONIBI_DISCORD_TOKEN")).CreateMessage(t.Context(), channel, "onibi live discord smoke"); err != nil {
		t.Fatal(err)
	}
}
