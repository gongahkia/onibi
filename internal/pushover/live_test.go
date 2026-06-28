package pushover

import (
	"os"
	"testing"
)

func TestLivePushover(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_PUSHOVER") != "1" {
		t.Skip("set ONIBI_LIVE_PUSHOVER=1")
	}
	if _, err := New(os.Getenv("ONIBI_PUSHOVER_TOKEN"), os.Getenv("ONIBI_PUSHOVER_USER_KEY")).Send(t.Context(), MessageOptions{Title: "Onibi", Message: "live pushover smoke"}); err != nil {
		t.Fatal(err)
	}
}
