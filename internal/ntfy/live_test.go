package ntfy

import (
	"os"
	"testing"

	"github.com/gongahkia/onibi/internal/liveartifact"
)

func TestLiveNtfy(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_NTFY") != "1" {
		t.Skip("set ONIBI_LIVE_NTFY=1")
	}
	envs := []string{"ONIBI_NTFY_BASE_URL", "ONIBI_NTFY_TOPIC", "ONIBI_NTFY_TOKEN"}
	rec, err := liveartifact.New("ntfy", envs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rec.Close(envs...); err != nil {
			t.Errorf("artifact: %v", err)
		}
		t.Logf("artifact: %s", rec.Path())
	})
	c := New(os.Getenv("ONIBI_NTFY_BASE_URL"), os.Getenv("ONIBI_NTFY_TOPIC"), os.Getenv("ONIBI_NTFY_TOKEN"))
	if err := c.Publish(t.Context(), Message{Title: "Onibi", Body: "live ntfy smoke"}); err != nil {
		rec.Error("publish", err)
		t.Fatal(err)
	}
	rec.Record("publish", map[string]any{"ok": true})
}
