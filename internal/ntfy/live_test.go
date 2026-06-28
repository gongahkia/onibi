package ntfy

import (
	"os"
	"testing"
)

func TestLiveNtfy(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_NTFY") != "1" {
		t.Skip("set ONIBI_LIVE_NTFY=1")
	}
	c := New(os.Getenv("ONIBI_NTFY_BASE_URL"), os.Getenv("ONIBI_NTFY_TOPIC"), os.Getenv("ONIBI_NTFY_TOKEN"))
	if err := c.Publish(t.Context(), Message{Title: "Onibi", Body: "live ntfy smoke"}); err != nil {
		t.Fatal(err)
	}
}
