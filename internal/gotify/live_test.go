package gotify

import (
	"os"
	"testing"

	"github.com/gongahkia/onibi/internal/liveartifact"
)

func TestLiveGotify(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_GOTIFY") != "1" {
		t.Skip("set ONIBI_LIVE_GOTIFY=1")
	}
	envs := []string{"ONIBI_GOTIFY_URL", "ONIBI_GOTIFY_APP_TOKEN", "ONIBI_GOTIFY_CLIENT_TOKEN"}
	rec, err := liveartifact.New("gotify", envs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rec.Close(envs...); err != nil {
			t.Errorf("artifact: %v", err)
		}
		t.Logf("artifact: %s", rec.Path())
	})
	c := New(os.Getenv("ONIBI_GOTIFY_URL"), os.Getenv("ONIBI_GOTIFY_APP_TOKEN"), os.Getenv("ONIBI_GOTIFY_CLIENT_TOKEN"))
	if err := c.Validate(t.Context()); err != nil {
		rec.Error("validate", err)
		t.Fatal(err)
	}
	rec.Record("validate", map[string]any{"ok": true})
	if err := c.Send(t.Context(), Message{Title: "Onibi", Message: "live gotify smoke"}); err != nil {
		rec.Error("send", err)
		t.Fatal(err)
	}
	rec.Record("send", map[string]any{"ok": true})
}
