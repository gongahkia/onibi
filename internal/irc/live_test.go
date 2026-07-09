package irc

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveIRC(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_IRC") != "1" {
		t.Skip("set ONIBI_LIVE_IRC=1 with registered IRC bot env")
	}
	nick := strings.TrimSpace(os.Getenv("ONIBI_IRC_NICK"))
	username := strings.TrimSpace(os.Getenv("ONIBI_IRC_USERNAME"))
	password := strings.TrimSpace(os.Getenv("ONIBI_IRC_PASSWORD"))
	if nick == "" || username == "" || password == "" {
		t.Fatal("ONIBI_IRC_NICK, ONIBI_IRC_USERNAME, ONIBI_IRC_PASSWORD required")
	}
	ctx := t.Context()
	c := New(os.Getenv("ONIBI_IRC_ADDR"), nick, username, password)
	c.SendPace = 2 * time.Second
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if owner := strings.TrimSpace(os.Getenv("ONIBI_IRC_OWNER_NICK")); owner != "" {
		if err := c.SendPrivmsg(ctx, owner, "onibi live irc probe"); err != nil {
			t.Fatal(err)
		}
	}
}
