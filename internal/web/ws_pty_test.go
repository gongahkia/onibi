package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	cpty "github.com/creack/pty"

	e2ecrypto "github.com/gongahkia/onibi/internal/e2e"
	"github.com/gongahkia/onibi/internal/envelope"
	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/store"
	webtransport "github.com/gongahkia/onibi/internal/web/transport"
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

func TestWSPTYViewerInputFramesAreDropped(t *testing.T) {
	host := spawnPTYForTest(t, "cat")
	c := dialPTYForTestWithRole(t, host, store.PairRoleViewer)
	writeWSJSONForTest(t, c, ptyAttachFrame{Type: "attach", SessionID: "s1", LastSeq: 0})
	writeWSBinaryForTest(t, c, []byte("viewer-blocked\n"))

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	_, p, err := c.Read(ctx)
	if err == nil {
		t.Fatalf("viewer input reached pty: %q", p)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("viewer ws closed instead of dropping input: %v", err)
	}
}

func TestWSPTYViewerAttachDetachAudited(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	host := spawnPTYForTest(t, "cat")
	srv.ptyHosts = func() map[string]*pty.Host {
		return map[string]*pty.Host{"s1": host}
	}
	rr := httptest.NewRecorder()
	viewerID, err := srv.CreateWebSession(context.Background(), rr, "viewer", store.PairRoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/pty?token=" + viewerID
	header := http.Header{}
	header.Add("Cookie", rr.Result().Cookies()[0].String())
	header.Set("User-Agent", "ViewerAgent/1.0")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
		Subprotocols: []string{ptySubprotocol},
		HTTPHeader:   header,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	writeWSJSONForTest(t, c, ptyAttachFrame{Type: "attach", SessionID: "s1", LastSeq: 0})

	attach := waitForAuditAction(t, srv.db, "viewer.attach")
	assertViewerAuditEntry(t, attach, "s1", viewerID)
	if err := c.Close(websocket.StatusNormalClosure, "done"); err != nil {
		t.Fatal(err)
	}
	detach := waitForAuditAction(t, srv.db, "viewer.detach")
	assertViewerAuditEntry(t, detach, "s1", viewerID)
}

func TestWSPTYE2EReplayClosesPolicyViolation(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	keys := NewRelayKeys()
	key, err := envelope.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := keys.RegisterPair(context.Background(), db, "tok", key, time.Minute); err != nil {
		t.Fatal(err)
	}
	srv := New(Options{DB: db, RelayKeys: keys, RequireE2E: true})
	host := spawnPTYForTest(t, "cat")
	srv.ptyHosts = func() map[string]*pty.Host {
		return map[string]*pty.Host{"s1": host}
	}
	rr := httptest.NewRecorder()
	ownerSessionID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := keys.BindSession(context.Background(), db, "tok", ownerSessionID); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

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
	defer c.CloseNow()

	sessionKey := e2ecrypto.DeriveSessionKey(key, []byte(ownerSessionID))
	base, err := envelope.NewCodec(sessionKey, e2eInfoPTY)
	if err != nil {
		t.Fatal(err)
	}
	client := newSeqWSCodec(base, ownerSessionID, e2eInfoPTY, e2eDirS2C, e2eDirC2S)
	verifyToken, err := relayVerifyToken(key, ownerSessionID)
	if err != nil {
		t.Fatal(err)
	}
	attach, err := json.Marshal(ptyAttachFrame{
		Type:        "attach",
		SessionID:   "s1",
		LastSeq:     0,
		VerifyToken: base64.RawURLEncoding.EncodeToString(verifyToken),
	})
	if err != nil {
		t.Fatal(err)
	}
	typ, sealed, err := client.encrypt(websocket.MessageText, attach)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Write(ctx, typ, sealed); err != nil {
		t.Fatal(err)
	}
	if err := c.Write(ctx, typ, sealed); err != nil {
		t.Fatal(err)
	}
	_, _, err = c.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("close status = %v err=%v", websocket.CloseStatus(err), err)
	}
}

func TestWSPTYE2EIdlePingKeepsConnectionOpen(t *testing.T) {
	withWSPingConfig(t, 50*time.Millisecond, 500*time.Millisecond)
	srv, ownerSessionID, cookie, key, cleanup := e2ePTYServerForTest(t)
	defer cleanup()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	c, client := dialE2EPTYAt(t, ts.URL, ownerSessionID, cookie, key)
	defer c.CloseNow()
	messages, errs := readE2EPTYForTest(t, c, client)
	select {
	case err := <-errs:
		t.Fatalf("idle ws closed early: %v", err)
	case <-time.After(250 * time.Millisecond):
	}
	writeE2EWSBinaryForTest(t, c, client, []byte("idle-ok\n"))
	waitForE2EPTYPayload(t, messages, errs, []byte("idle-ok"))
}

func TestLiveCloudflareQuickWSPTYE2EIdleFourMinutes(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_CLOUDFLARE_QUICK_IDLE") != "1" {
		t.Skip("set ONIBI_LIVE_CLOUDFLARE_QUICK_IDLE=1")
	}
	srv, ownerSessionID, cookie, key, cleanup := e2ePTYServerForTest(t)
	defer cleanup()
	ts := httptest.NewTLSServer(srv.Handler())
	defer ts.Close()
	_, portText, err := net.SplitHostPort(ts.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := net.LookupPort("tcp", portText)
	if err != nil {
		t.Fatal(err)
	}
	cf := webtransport.NewCloudflareQuick()
	procCtx, procCancel := context.WithCancel(context.Background())
	defer procCancel()
	enableDone := make(chan error, 1)
	go func() { enableDone <- cf.Enable(procCtx, port) }()
	select {
	case err := <-enableDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(45 * time.Second):
		procCancel()
		t.Fatal("cloudflare quick tunnel activation timed out")
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = cf.Disable(ctx)
	}()
	publicURL, err := cf.URL(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("cloudflare url: %s", publicURL)
	waitForTryCloudflareDNSForTest(t, publicURL, 30*time.Second)
	c, client := dialE2EPTYAtWithin(t, publicURL, ownerSessionID, cookie, key, 2*time.Minute)
	defer c.CloseNow()
	messages, errs := readE2EPTYForTest(t, c, client)
	select {
	case err := <-errs:
		t.Fatalf("cloudflare idle ws closed early: %v", err)
	case <-time.After(4 * time.Minute):
	}
	writeE2EWSBinaryForTest(t, c, client, []byte("cloudflare-idle-ok\n"))
	waitForE2EPTYPayload(t, messages, errs, []byte("cloudflare-idle-ok"))
}

func spawnPTYForTest(t *testing.T, script string) *pty.Host {
	t.Helper()
	return spawnPTYForTestWithTimeout(t, script, 10*time.Second)
}

func spawnPTYForTestWithTimeout(t *testing.T, script string, timeout time.Duration) *pty.Host {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	host, err := pty.Spawn(ctx, pty.SpawnOptions{Name: "/bin/sh", Args: []string{"-c", script}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	return host
}

func dialPTYForTest(t *testing.T, host *pty.Host) *websocket.Conn {
	return dialPTYForTestWithRole(t, host, store.PairRoleOwner)
}

func dialPTYForTestWithRole(t *testing.T, host *pty.Host, role string) *websocket.Conn {
	t.Helper()
	srv, cleanup := testServer(t)
	t.Cleanup(cleanup)
	srv.ptyHosts = func() map[string]*pty.Host {
		return map[string]*pty.Host{"s1": host}
	}
	rr := httptest.NewRecorder()
	ownerSessionID, err := srv.CreateWebSession(context.Background(), rr, "test device", role)
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

func writeWSBinaryForTest(t *testing.T, c *websocket.Conn, p []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageBinary, p); err != nil {
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

func waitForAuditAction(t *testing.T, db *store.DB, action string) store.AuditEntry {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := db.AuditRecent(context.Background(), 20)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.Action == action {
				return row
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for audit action %s", action)
	return store.AuditEntry{}
}

func assertViewerAuditEntry(t *testing.T, entry store.AuditEntry, sessionID, viewerID string) {
	t.Helper()
	if entry.SessionID != sessionID {
		t.Fatalf("session_id = %q", entry.SessionID)
	}
	for _, want := range []string{"viewer_id=" + viewerID, "remote=", `user_agent="ViewerAgent/1.0"`} {
		if !strings.Contains(entry.Detail, want) {
			t.Fatalf("audit detail %q missing %q", entry.Detail, want)
		}
	}
}

func withWSPingConfig(t *testing.T, interval, timeout time.Duration) {
	t.Helper()
	oldInterval, oldTimeout := wsPingInterval, wsPingTimeout
	wsPingInterval, wsPingTimeout = interval, timeout
	t.Cleanup(func() {
		wsPingInterval, wsPingTimeout = oldInterval, oldTimeout
	})
}

func e2ePTYServerForTest(t *testing.T) (*Server, string, *http.Cookie, []byte, func()) {
	t.Helper()
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	keys := NewRelayKeys()
	key, err := envelope.NewKey()
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := keys.RegisterPair(context.Background(), db, "tok", key, time.Minute); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	srv := New(Options{DB: db, RelayKeys: keys, RequireE2E: true})
	host := spawnPTYForTestWithTimeout(t, "cat", 6*time.Minute)
	srv.ptyHosts = func() map[string]*pty.Host {
		return map[string]*pty.Host{"s1": host}
	}
	rr := httptest.NewRecorder()
	ownerSessionID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := keys.BindSession(context.Background(), db, "tok", ownerSessionID); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	return srv, ownerSessionID, rr.Result().Cookies()[0], key, func() { _ = db.Close() }
}

func dialE2EPTYAt(t *testing.T, baseURL, ownerSessionID string, cookie *http.Cookie, key []byte) (*websocket.Conn, wsCodec) {
	t.Helper()
	return dialE2EPTYAtWithin(t, baseURL, ownerSessionID, cookie, key, 10*time.Second)
}

func dialE2EPTYAtWithin(t *testing.T, baseURL, ownerSessionID string, cookie *http.Cookie, key []byte, timeout time.Duration) (*websocket.Conn, wsCodec) {
	t.Helper()
	u := "ws" + strings.TrimPrefix(strings.TrimRight(baseURL, "/"), "http") + "/ws/pty?token=" + ownerSessionID
	header := http.Header{}
	header.Add("Cookie", cookie.String())
	var c *websocket.Conn
	var err error
	deadline := time.Now().Add(timeout)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		c, _, err = websocket.Dial(ctx, u, &websocket.DialOptions{
			Subprotocols: []string{ptySubprotocol},
			HTTPHeader:   header,
		})
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(500 * time.Millisecond)
	}
	c.SetReadLimit(1 << 20)
	sessionKey := e2ecrypto.DeriveSessionKey(key, []byte(ownerSessionID))
	base, err := envelope.NewCodec(sessionKey, e2eInfoPTY)
	if err != nil {
		c.CloseNow()
		t.Fatal(err)
	}
	client := newSeqWSCodec(base, ownerSessionID, e2eInfoPTY, e2eDirS2C, e2eDirC2S)
	verifyToken, err := relayVerifyToken(key, ownerSessionID)
	if err != nil {
		c.CloseNow()
		t.Fatal(err)
	}
	writeE2EWSJSONForTest(t, c, client, ptyAttachFrame{
		Type:        "attach",
		SessionID:   "s1",
		LastSeq:     0,
		VerifyToken: base64.RawURLEncoding.EncodeToString(verifyToken),
	})
	return c, client
}

func writeE2EWSJSONForTest(t *testing.T, c *websocket.Conn, client wsCodec, v any) {
	t.Helper()
	p, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	typ, sealed, err := client.encrypt(websocket.MessageText, p)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, typ, sealed); err != nil {
		t.Fatal(err)
	}
}

func writeE2EWSBinaryForTest(t *testing.T, c *websocket.Conn, client wsCodec, p []byte) {
	t.Helper()
	typ, sealed, err := client.encrypt(websocket.MessageBinary, p)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, typ, sealed); err != nil {
		t.Fatal(err)
	}
}

func readE2EPTYForTest(t *testing.T, c *websocket.Conn, client wsCodec) (<-chan []byte, <-chan error) {
	t.Helper()
	messages := make(chan []byte, 16)
	errs := make(chan error, 1)
	go func() {
		for {
			typ, p, err := c.Read(context.Background())
			if err != nil {
				errs <- err
				return
			}
			typ, p, err = client.decrypt(typ, p)
			if err != nil {
				errs <- err
				return
			}
			if typ == websocket.MessageBinary || typ == websocket.MessageText {
				messages <- p
			}
		}
	}()
	return messages, errs
}

func waitForE2EPTYPayload(t *testing.T, messages <-chan []byte, errs <-chan error, want []byte) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case p := <-messages:
			if bytes.Contains(p, want) {
				return
			}
		case err := <-errs:
			t.Fatalf("ws closed before payload: %v", err)
		case <-deadline:
			t.Fatalf("timed out waiting for %q", want)
		}
	}
}

func waitForTryCloudflareDNSForTest(t *testing.T, rawurl string, timeout time.Duration) {
	t.Helper()
	u, err := url.Parse(rawurl)
	if err != nil {
		t.Fatal(err)
	}
	host := u.Hostname()
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "udp", "1.1.1.1:53")
		},
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		addrs, err := resolver.LookupHost(ctx, host)
		cancel()
		if err == nil && len(addrs) > 0 {
			return
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("cloudflare dns not ready for %s: %v", host, lastErr)
}
