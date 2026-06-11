package intake

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestApprovalRPCRoundtrip(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "onibi.sock")

	approver := func(_ context.Context, ev Event) (Response, error) {
		if ev.Tool != "Bash" {
			t.Errorf("got tool %q", ev.Tool)
		}
		return Response{
			Decision:  "approve",
			DecidedBy: 42,
		}, nil
	}

	srv := New(sock, func(context.Context, Event) error { return nil }, nil)
	srv.SetApprovalHandler(approver)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Serve(ctx) }()

	// wait for bind
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := pingSock(sock); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	resp, err := Request(sock, Event{
		Type:      TypeApprovalRequest,
		Tool:      "Bash",
		InputJSON: `{"command":"ls"}`,
	}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Decision != "approve" {
		t.Fatalf("decision = %q", resp.Decision)
	}
	if resp.DecidedBy != 42 {
		t.Fatalf("decided_by = %d", resp.DecidedBy)
	}
}

func TestApprovalNoHandlerCancels(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "onibi.sock")
	srv := New(sock, func(context.Context, Event) error { return nil }, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Serve(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := pingSock(sock); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	resp, err := Request(sock, Event{Type: TypeApprovalRequest, Tool: "Bash"}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Decision != "cancelled" {
		t.Fatalf("expected cancelled, got %q", resp.Decision)
	}
}

func TestGenericRPCRoundtrip(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "onibi.sock")
	srv := New(sock, func(context.Context, Event) error { return nil }, nil)
	srv.SetRPCHandler(func(_ context.Context, ev Event) (Response, error) {
		if ev.Type != TypeSessionPeek || ev.Session != "s1" {
			t.Fatalf("bad event: %#v", ev)
		}
		return Response{Text: "tail"}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Serve(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := pingSock(sock); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	resp, err := Request(sock, Event{Type: TypeSessionPeek, Session: "s1"}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "tail" {
		t.Fatalf("text = %q", resp.Text)
	}
}
