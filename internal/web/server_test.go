package web

import (
	"bytes"
	"context"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/store"
)

func TestGenerateOrLoadCertRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cert1, err := GenerateOrLoadCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	cert2, err := GenerateOrLoadCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cert1.Certificate) == 0 || len(cert2.Certificate) == 0 {
		t.Fatal("missing certificate DER")
	}
	if !bytes.Equal(cert1.Certificate[0], cert2.Certificate[0]) {
		t.Fatal("loaded certificate differs from generated certificate")
	}
	leaf, err := x509.ParseCertificate(cert1.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if !leaf.NotAfter.After(time.Now().AddDate(0, 11, 0)) {
		t.Fatalf("unexpected NotAfter: %s", leaf.NotAfter)
	}
	if !containsString(leaf.DNSNames, "localhost") {
		t.Fatalf("DNS SANs = %#v", leaf.DNSNames)
	}
	if !hasIP(leaf, "127.0.0.1") || !hasIP(leaf, "::1") {
		t.Fatalf("IP SANs = %#v", leaf.IPAddresses)
	}
	for _, name := range []string{"server.crt", "server.key"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("%s mode = %o", name, got)
		}
	}
}

func TestOwnerCookieAttributesAndAuth(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	cookies := rr.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v", cookies)
	}
	c := cookies[0]
	if c.Name != OwnerCookieName || c.Value != sessionID || c.Path != "/" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.MaxAge <= 0 {
		t.Fatalf("cookie = %#v", c)
	}
	req := httptest.NewRequest(http.MethodPost, "/control", strings.NewReader(`{"session_id":"missing","action":"interrupt"}`))
	req.AddCookie(c)
	w := httptest.NewRecorder()
	srv.handleControl(w, req)
	if w.Code == http.StatusUnauthorized {
		t.Fatal("valid owner cookie was rejected")
	}
}

func TestWSPTYRejectsMissingCookie(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/ws/pty?token=missing")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestWSPTYAcceptsCookieAndToken(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/pty?token=" + sessionID
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
	if got := c.Subprotocol(); got != ptySubprotocol {
		t.Fatalf("subprotocol = %q", got)
	}
	var hello map[string]any
	if err := wsjson.Read(ctx, c, &hello); err != nil {
		t.Fatal(err)
	}
	if hello["type"] != "server-hello" || hello["endpoint"] != "pty" || hello["session_id"] != sessionID {
		t.Fatalf("hello = %#v", hello)
	}
}

func TestWSEventsAcceptsCookieAndToken(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/events?token=" + sessionID
	header := http.Header{}
	header.Add("Cookie", rr.Result().Cookies()[0].String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
		Subprotocols: []string{eventsSubprotocol},
		HTTPHeader:   header,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if got := c.Subprotocol(); got != eventsSubprotocol {
		t.Fatalf("subprotocol = %q", got)
	}
	var hello map[string]any
	if err := wsjson.Read(ctx, c, &hello); err != nil {
		t.Fatal(err)
	}
	if hello["type"] != "server-hello" || hello["endpoint"] != "events" || hello["session_id"] != sessionID {
		t.Fatalf("hello = %#v", hello)
	}
}

func TestControlInterruptUsesHostResolver(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	writes := make(chan []byte, 1)
	host := pty.NewVirtualHost(func(p []byte) (int, error) {
		writes <- append([]byte(nil), p...)
		return len(p), nil
	}, nil, nil)
	srv.ptyHosts = func() map[string]*pty.Host {
		return map[string]*pty.Host{"s1": host}
	}
	rr := httptest.NewRecorder()
	_, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/control", strings.NewReader(`{"session_id":"s1","action":"interrupt"}`))
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.handleControl(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	select {
	case got := <-writes:
		if !bytes.Equal(got, []byte{3}) {
			t.Fatalf("write = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("host did not receive interrupt")
	}
}

func testServer(t *testing.T) (*Server, func()) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Options{DB: db})
	return srv, func() { _ = db.Close() }
}

func containsString(vals []string, want string) bool {
	for _, v := range vals {
		if v == want {
			return true
		}
	}
	return false
}

func hasIP(cert *x509.Certificate, want string) bool {
	for _, ip := range cert.IPAddresses {
		if ip.String() == want {
			return true
		}
	}
	return false
}
