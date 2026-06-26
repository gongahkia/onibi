package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	cpty "github.com/creack/pty"

	"github.com/gongahkia/onibi/internal/pty"
)

func TestWSPTYAttachValidLastSeqReturnsDelta(t *testing.T) {
	host := spawnPTYForTest(t, "printf delta-output; sleep 2")
	waitForReplay(t, host, 0, func(r pty.Replay) bool {
		return bytes.Contains(r.Data, []byte("delta-output"))
	})
	c := dialPTYForTest(t, host)
	writeWSJSONForTest(t, c, ptyAttachFrame{Type: "attach", SessionID: "s1", LastSeq: 0})

	typ, p := readWSForTest(t, c)
	if typ != websocket.MessageBinary {
		t.Fatalf("message type = %v", typ)
	}
	if !bytes.Contains(p, []byte("delta-output")) {
		t.Fatalf("delta = %q", p)
	}
}

func TestWSPTYAttachStaleLastSeqReturnsSnapshot(t *testing.T) {
	host := spawnPTYForTest(t, "yes snapshot-line | head -c 300000; sleep 2")
	waitForReplay(t, host, 1, func(r pty.Replay) bool {
		return r.Snapshot && len(r.Data) >= pty.DefaultRingSize
	})
	c := dialPTYForTest(t, host)
	writeWSJSONForTest(t, c, ptyAttachFrame{Type: "attach", SessionID: "s1", LastSeq: 1})

	typ, p := readWSForTest(t, c)
	if typ != websocket.MessageText {
		t.Fatalf("message type = %v", typ)
	}
	var frame ptySnapshotFrame
	if err := json.Unmarshal(p, &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Type != "snapshot" || frame.Seq == 0 || frame.Base64Data == "" {
		t.Fatalf("snapshot frame = %#v", frame)
	}
	data, err := base64.StdEncoding.DecodeString(frame.Base64Data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("snapshot-line")) {
		t.Fatalf("snapshot data missing marker")
	}
}

func TestWSPTYResizeFrameResizesHost(t *testing.T) {
	host := spawnPTYForTest(t, "cat")
	c := dialPTYForTest(t, host)
	writeWSJSONForTest(t, c, ptyAttachFrame{Type: "attach", SessionID: "s1", LastSeq: 0})
	writeWSJSONForTest(t, c, ptyControlFrame{Type: "resize", Rows: 22, Cols: 77})

	typ, p := readWSForTest(t, c)
	if typ != websocket.MessageText {
		t.Fatalf("resize echo message type = %v payload=%q", typ, p)
	}
	var frame ptyResizeFrame
	if err := json.Unmarshal(p, &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Type != "resize" || frame.Rows != 22 || frame.Cols != 77 {
		t.Fatalf("resize frame = %#v", frame)
	}
	if bytes.Contains(p, []byte("ONIBI-RESIZE")) {
		t.Fatalf("resize marker leaked to web client: %q", p)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows, cols, err := cpty.Getsize(host.Master)
		if err != nil {
			t.Fatal(err)
		}
		if rows == 22 && cols == 77 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("resize did not reach PTY")
}

func spawnPTYForTest(t *testing.T, script string) *pty.Host {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	host, err := pty.Spawn(ctx, pty.SpawnOptions{Name: "/bin/sh", Args: []string{"-c", script}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	return host
}

func dialPTYForTest(t *testing.T, host *pty.Host) *websocket.Conn {
	t.Helper()
	srv, cleanup := testServer(t)
	t.Cleanup(cleanup)
	srv.ptyHosts = func() map[string]*pty.Host {
		return map[string]*pty.Host{"s1": host}
	}
	rr := httptest.NewRecorder()
	ownerSessionID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/pty?token=" + ownerSessionID
	header := http.Header{}
	header.Add("Cookie", rr.Result().Cookies()[0].String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
		Subprotocols: []string{ptySubprotocol},
		HTTPHeader:   header,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadLimit(1 << 20)
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

func waitForReplay(t *testing.T, host *pty.Host, seq uint64, ok func(pty.Replay) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if r := host.ReplaySince(seq); ok(r) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for replay")
}

func writeWSJSONForTest(t *testing.T, c *websocket.Conn, v any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, c, v); err != nil {
		t.Fatal(err)
	}
}

func readWSForTest(t *testing.T, c *websocket.Conn) (websocket.MessageType, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, p, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return typ, p
}
