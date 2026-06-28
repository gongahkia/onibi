package pushover

import (
	"os"
	"testing"

	"github.com/gongahkia/onibi/internal/liveartifact"
)

func TestLivePushover(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_PUSHOVER") != "1" {
		t.Skip("set ONIBI_LIVE_PUSHOVER=1")
	}
	envs := []string{"ONIBI_PUSHOVER_TOKEN", "ONIBI_PUSHOVER_USER_KEY"}
	rec, err := liveartifact.New("pushover", envs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rec.Close(envs...); err != nil {
			t.Errorf("artifact: %v", err)
		}
		t.Logf("artifact: %s", rec.Path())
	})
	resp, err := New(os.Getenv("ONIBI_PUSHOVER_TOKEN"), os.Getenv("ONIBI_PUSHOVER_USER_KEY")).Send(t.Context(), MessageOptions{Title: "Onibi", Message: "live pushover smoke"})
	if err != nil {
		rec.Error("send", err)
		t.Fatal(err)
	}
	rec.Record("send", map[string]any{"status": resp.Status, "receipt": resp.Receipt != ""})
}
