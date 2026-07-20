package irc

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/liveartifact"
)

func TestLiveLiberaChatSASLPlain(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_IRC") != "1" {
		t.Skip("set ONIBI_LIVE_IRC=1 with registered Libera.Chat credentials")
	}
	values := map[string]string{
		"nick":     strings.TrimSpace(os.Getenv("ONIBI_LIVE_IRC_NICK")),
		"username": strings.TrimSpace(os.Getenv("ONIBI_LIVE_IRC_USERNAME")),
		"password": strings.TrimSpace(os.Getenv("ONIBI_LIVE_IRC_PASSWORD")),
		"target":   strings.TrimSpace(os.Getenv("ONIBI_LIVE_IRC_TARGET")),
	}
	for key, value := range values {
		if value == "" {
			t.Skip("set ONIBI_LIVE_IRC_" + strings.ToUpper(key))
		}
	}
	envs := []string{"ONIBI_LIVE_IRC", "ONIBI_LIVE_IRC_NICK", "ONIBI_LIVE_IRC_USERNAME", "ONIBI_LIVE_IRC_PASSWORD", "ONIBI_LIVE_IRC_TARGET"}
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
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	c := NewClient(Config{Nick: values["nick"], Username: values["username"], Password: values["password"]})
	if err := c.Connect(ctx); err != nil {
		rec.Error("sasl-connect", err)
		t.Fatal(err)
	}
	defer c.Close()
	rec.Record("sasl-connect", map[string]any{"ok": true})
	if err := c.SendPrivmsg(ctx, values["target"], "onibi live IRC smoke"); err != nil {
		rec.Error("send-dm", err)
		t.Fatal(err)
	}
	rec.Record("send-dm", map[string]any{"ok": true})
}
