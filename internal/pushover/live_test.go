package pushover

import (
	"os"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/liveartifact"
)

func TestLivePushover(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_PUSHOVER") != "1" {
		t.Skip("set ONIBI_LIVE_PUSHOVER=1")
	}
	envs := []string{"ONIBI_PUSHOVER_TOKEN", "ONIBI_PUSHOVER_USER_KEY", "ONIBI_PUSHOVER_EMERGENCY", "ONIBI_PUSHOVER_POLL_RECEIPT"}
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
	client := New(os.Getenv("ONIBI_PUSHOVER_TOKEN"), os.Getenv("ONIBI_PUSHOVER_USER_KEY"))
	opts := MessageOptions{Title: "Onibi", Message: "live pushover smoke"}
	if os.Getenv("ONIBI_PUSHOVER_EMERGENCY") == "1" {
		opts.Priority = 2
		opts.Retry = 30 * time.Second
		opts.Expire = 2 * time.Minute
		opts.Message = "live pushover emergency smoke; acknowledge on device"
	}
	resp, err := client.Send(t.Context(), opts)
	if err != nil {
		rec.Error("send", err)
		t.Fatal(err)
	}
	rec.Record("send", map[string]any{"status": resp.Status, "receipt": resp.Receipt != ""})
	if os.Getenv("ONIBI_PUSHOVER_POLL_RECEIPT") == "1" && resp.Receipt != "" {
		got, err := client.PollReceipt(t.Context(), resp.Receipt, 5*time.Second)
		if err != nil {
			rec.Error("receipt", err)
			t.Fatal(err)
		}
		rec.Record("receipt", map[string]any{"acknowledged": got.Acknowledged, "expired": got.Expired})
	}
}
