package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/intake"
)

func TestSessionRequestReportsDaemonRefusal(t *testing.T) {
	// Unix socket paths are length-limited; t.TempDir can exceed it on macOS.
	dir, err := os.MkdirTemp("", "onibi-cli")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	if _, err := sessionRequest(sock, intake.Event{Type: intake.TypeSessionNew}); err == nil {
		t.Fatal("expected daemon unavailable")
	}
	srv := intake.New(sock, nil)
	srv.SetRPCHandler(func(_ context.Context, ev intake.Event) (intake.Response, error) {
		if ev.Agent == "missing" {
			return intake.Response{}, errors.New("missing not found in PATH")
		}
		return intake.Response{Text: "started x", SessionID: "abc"}, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for !intake.SocketActive(sock, 100*time.Millisecond) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := sessionRequest(sock, intake.Event{Type: intake.TypeSessionNew, Agent: "missing"}); err == nil || err.Error() != "missing not found in PATH" {
		t.Fatalf("refusal err=%v", err)
	}
	resp, err := sessionRequest(sock, intake.Event{Type: intake.TypeSessionNew, Agent: "shell"})
	if err != nil || resp.SessionID != "abc" {
		t.Fatalf("resp=%#v err=%v", resp, err)
	}
}
