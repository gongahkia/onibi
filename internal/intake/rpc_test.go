package intake

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestRetiredHookRequestsAreRefused(t *testing.T) {
	sock := testSocket(t)
	srv := New(sock, nil)
	srv.SetRPCHandler(func(context.Context, Event) (Response, error) {
		t.Error("retired request reached the rpc handler")
		return Response{}, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()
	waitSocket(t, sock)
	for _, typ := range []string{"approval_request", "claude_question", "agent_lifecycle"} {
		if _, err := Request(sock, Event{Type: typ}, time.Second); !IsRejected(err) {
			t.Fatalf("%s: client sent a retired request: %v", typ, err)
		}
		conn, err := net.Dial("unix", sock)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(conn).Encode(Event{Type: typ}); err != nil {
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if n, err := conn.Read(make([]byte, 1)); n != 0 || !errors.Is(err, io.EOF) {
			t.Fatalf("%s: server answered a retired request: n=%d err=%v", typ, n, err)
		}
		_ = conn.Close()
	}
}

func TestGenericRPCRoundtrip(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "onibi.sock")
	srv := New(sock, nil)
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

func TestRPCErrorsReportWhetherTheyPrecededSideEffects(t *testing.T) {
	sock := testSocket(t)
	srv := New(sock, nil)
	srv.SetRPCHandler(func(_ context.Context, ev Event) (Response, error) {
		if ev.Key == "reject" {
			return Response{}, Reject(errors.New("session not found"))
		}
		return Response{}, errors.New("send-keys failed")
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()
	waitSocket(t, sock)
	resp, err := Request(sock, Event{Type: TypeSessionKey, Session: "s1", Key: "reject", Exact: true}, 2*time.Second)
	if err != nil || !resp.Rejected || resp.Reason != "session not found" {
		t.Fatalf("resp=%#v err=%v", resp, err)
	}
	resp, err = Request(sock, Event{Type: TypeSessionKey, Session: "s1", Key: "up"}, 2*time.Second)
	if err != nil || resp.Rejected || resp.Reason != "send-keys failed" {
		t.Fatalf("resp=%#v err=%v", resp, err)
	}
}
