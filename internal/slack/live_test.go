package slack

import (
	"os"
	"testing"

	"github.com/gongahkia/onibi/internal/liveartifact"
)

func TestLiveSlack(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_SLACK") != "1" {
		t.Skip("set ONIBI_LIVE_SLACK=1")
	}
	envs := []string{"ONIBI_SLACK_APP_TOKEN", "ONIBI_SLACK_BOT_TOKEN", "ONIBI_SLACK_CHANNEL_ID", "ONIBI_SLACK_ALLOWED_CHANNELS", "ONIBI_SLACK_ALLOWED_DM_USERS"}
	rec, err := liveartifact.New("slack", envs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rec.Close(envs...); err != nil {
			t.Errorf("artifact: %v", err)
		}
		t.Logf("artifact: %s", rec.Path())
	})
	c := New(os.Getenv("ONIBI_SLACK_APP_TOKEN"), os.Getenv("ONIBI_SLACK_BOT_TOKEN"))
	url, err := c.OpenSocket(t.Context())
	if err != nil {
		rec.Error("open-socket", err)
		t.Fatal(err)
	}
	if url == "" {
		t.Fatal("empty socket url")
	}
	rec.Record("open-socket", map[string]any{"url": url})
	auth, err := c.AuthTest(t.Context())
	if err != nil {
		rec.Error("auth-test", err)
		t.Fatal(err)
	}
	rec.Record("auth-test", map[string]any{"bot_id": auth.BotID, "user_id": auth.UserID, "team_id": auth.TeamID})
	if channel := os.Getenv("ONIBI_SLACK_CHANNEL_ID"); channel != "" {
		info, err := c.ConversationInfo(t.Context(), channel)
		if err != nil {
			rec.Error("conversation-info", err)
			t.Fatal(err)
		}
		rec.Record("conversation-info", map[string]any{"channel": info.Channel.ID, "is_member": info.Channel.IsMember})
	}
}
