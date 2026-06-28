package transport

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestLiveTailscale(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_TAILSCALE") != "1" {
		t.Skip("set ONIBI_LIVE_TAILSCALE=1")
	}
	port, cleanup := liveHTTPSServer(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ts := NewTailscale()
	if err := ts.Enable(ctx, port); err != nil {
		t.Fatal(err)
	}
	defer ts.Disable(context.Background())
	if url, err := ts.URL(ctx); err != nil || url == "" {
		t.Fatalf("url=%q err=%v", url, err)
	}
}

func TestLiveCloudflareQuick(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_CLOUDFLARE_QUICK") != "1" {
		t.Skip("set ONIBI_LIVE_CLOUDFLARE_QUICK=1")
	}
	port, cleanup := liveHTTPSServer(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cf := NewCloudflareQuick()
	if err := cf.Enable(ctx, port); err != nil {
		t.Fatal(err)
	}
	defer cf.Disable(context.Background())
	if url, err := cf.URL(ctx); err != nil || url == "" {
		t.Fatalf("url=%q err=%v", url, err)
	}
}

func TestLiveCloudflareNamed(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_CLOUDFLARE_NAMED") != "1" {
		t.Skip("set ONIBI_LIVE_CLOUDFLARE_NAMED=1")
	}
	port, cleanup := liveHTTPSServer(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cf := NewCloudflareNamedFromEnv()
	if err := cf.Enable(ctx, port); err != nil {
		t.Fatal(err)
	}
	defer cf.Disable(context.Background())
	if url, err := cf.URL(ctx); err != nil || url == "" {
		t.Fatalf("url=%q err=%v", url, err)
	}
}

func TestLiveNgrok(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_NGROK") != "1" {
		t.Skip("set ONIBI_LIVE_NGROK=1")
	}
	port, cleanup := liveHTTPSServer(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	ng := NewNgrokFromEnv()
	if err := ng.Enable(ctx, port); err != nil {
		t.Fatal(err)
	}
	defer ng.Disable(context.Background())
	if url, err := ng.URL(ctx); err != nil || url == "" {
		t.Fatalf("url=%q err=%v", url, err)
	}
}

func liveHTTPSServer(t *testing.T) (int, func()) {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		srv.Close()
		t.Fatal(err)
	}
	p, err := net.LookupPort("tcp", port)
	if err != nil {
		srv.Close()
		t.Fatal(err)
	}
	return p, srv.Close
}
