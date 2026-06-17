package cli

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/intake"
)

func TestPingSocketReturnsDaemonHealth(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "onibi-ping-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := dir + "/onibi.sock"
	srv := intake.New(sock, nil, nil)
	srv.SetRPCHandler(func(_ context.Context, ev intake.Event) (intake.Response, error) {
		if ev.Type != intake.TypePing {
			t.Fatalf("type = %s", ev.Type)
		}
		return intake.Response{Text: "pong\nuptime=1s\nsessions=0"}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Serve(ctx) }()
	if !waitForSocket(ctx, sock, 2*time.Second) {
		t.Fatal("socket not ready")
	}
	resp, err := pingSocket(context.Background(), sock, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Text, "pong") || !strings.Contains(resp.Text, "uptime=1s") {
		t.Fatalf("resp = %#v", resp)
	}
}

func TestPingCommandRejectsInvalidCount(t *testing.T) {
	cmd := pingCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--count", "0"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--count must be > 0") {
		t.Fatalf("err = %v", err)
	}
}
