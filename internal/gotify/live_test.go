package gotify

import (
	"os"
	"testing"
)

func TestLiveGotify(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_GOTIFY") != "1" {
		t.Skip("set ONIBI_LIVE_GOTIFY=1")
	}
	if err := New(os.Getenv("ONIBI_GOTIFY_URL"), os.Getenv("ONIBI_GOTIFY_APP_TOKEN"), os.Getenv("ONIBI_GOTIFY_CLIENT_TOKEN")).Send(t.Context(), Message{Title: "Onibi", Message: "live gotify smoke"}); err != nil {
		t.Fatal(err)
	}
}
