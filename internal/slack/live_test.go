package slack

import (
	"os"
	"testing"
)

func TestLiveSlack(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_SLACK") != "1" {
		t.Skip("set ONIBI_LIVE_SLACK=1")
	}
	c := New(os.Getenv("ONIBI_SLACK_APP_TOKEN"), os.Getenv("ONIBI_SLACK_BOT_TOKEN"))
	url, err := c.OpenSocket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if url == "" {
		t.Fatal("empty socket url")
	}
}
