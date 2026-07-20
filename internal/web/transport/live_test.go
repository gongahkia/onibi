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

	"github.com/gongahkia/onibi/internal/liveartifact"
)

func TestLiveTailscale(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_TAILSCALE") != "1" {
		t.Skip("set ONIBI_LIVE_TAILSCALE=1")
	}
	envs := []string{"ONIBI_LIVE_TAILSCALE"}
	rec, err := liveartifact.New("tailscale", envs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rec.Close(envs...); err != nil {
			t.Errorf("artifact: %v", err)
		}
		t.Logf("artifact: %s", rec.Path())
	})
	port, cleanup := liveHTTPSServer(t)
	defer cleanup()
	rec.Record("local-https", map[string]any{"port": port})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ts := NewTailscale()
	if err := ts.Enable(ctx, port); err != nil {
		rec.Error("enable", err)
		t.Fatal(err)
	}
	defer ts.Disable(context.Background())
	if url, err := ts.URL(ctx); err != nil || url == "" {
		rec.Error("url", err)
		t.Fatalf("url=%q err=%v", url, err)
	} else {
		rec.Record("url", map[string]any{"url": url})
	}
}

func TestLiveWireGuard(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_WIREGUARD") != "1" {
		t.Skip("set ONIBI_LIVE_WIREGUARD=1")
	}
	envs := []string{"ONIBI_LIVE_WIREGUARD", "ONIBI_WIREGUARD_BIN", "ONIBI_WIREGUARD_INTERFACE"}
	rec, err := liveartifact.New("wireguard", envs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rec.Close(envs...); err != nil {
			t.Errorf("artifact: %v", err)
		}
		t.Logf("artifact: %s", rec.Path())
	})
	port, cleanup := liveHTTPSServer(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wg := NewWireGuardFromEnv()
	if err := wg.Enable(ctx, port); err != nil {
		rec.Error("enable", err)
		t.Fatal(err)
	}
	if url, err := wg.URL(ctx); err != nil || url == "" {
		rec.Error("url", err)
		t.Fatalf("url=%q err=%v", url, err)
	} else {
		rec.Record("url", map[string]any{"url": url, "interface": wg.InterfaceName()})
	}
}

func TestLiveZeroTier(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_ZEROTIER") != "1" {
		t.Skip("set ONIBI_LIVE_ZEROTIER=1")
	}
	envs := []string{"ONIBI_LIVE_ZEROTIER", "ONIBI_ZEROTIER_BIN", "ONIBI_ZEROTIER_NETWORK"}
	rec, err := liveartifact.New("zerotier", envs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rec.Close(envs...); err != nil {
			t.Errorf("artifact: %v", err)
		}
		t.Logf("artifact: %s", rec.Path())
	})
	port, cleanup := liveHTTPSServer(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	zt := NewZeroTierFromEnv()
	if err := zt.Enable(ctx, port); err != nil {
		rec.Error("enable", err)
		t.Fatal(err)
	}
	if url, err := zt.URL(ctx); err != nil || url == "" {
		rec.Error("url", err)
		t.Fatalf("url=%q err=%v", url, err)
	} else {
		rec.Record("url", map[string]any{"url": url, "network": zt.NetworkID(), "interface": zt.InterfaceName()})
	}
}

func TestLiveCloudflareQuick(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_CLOUDFLARE_QUICK") != "1" {
		t.Skip("set ONIBI_LIVE_CLOUDFLARE_QUICK=1")
	}
	envs := []string{"ONIBI_LIVE_CLOUDFLARE_QUICK", "ONIBI_CLOUDFLARED_BIN"}
	rec, err := liveartifact.New("cloudflare-quick", envs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rec.Close(envs...); err != nil {
			t.Errorf("artifact: %v", err)
		}
		t.Logf("artifact: %s", rec.Path())
	})
	port, cleanup := liveHTTPSServer(t)
	defer cleanup()
	rec.Record("local-https", map[string]any{"port": port})
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cf := NewCloudflareQuick()
	if err := cf.Enable(ctx, port); err != nil {
		rec.Error("enable", err)
		t.Fatal(err)
	}
	defer cf.Disable(context.Background())
	if url, err := cf.URL(ctx); err != nil || url == "" {
		rec.Error("url", err)
		t.Fatalf("url=%q err=%v", url, err)
	} else {
		rec.Record("url", map[string]any{"url": url})
	}
}

func TestLiveNgrok(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_NGROK") != "1" {
		t.Skip("set ONIBI_LIVE_NGROK=1")
	}
	envs := []string{"ONIBI_LIVE_NGROK", "ONIBI_NGROK_BIN", "ONIBI_NGROK_AUTHTOKEN", "ONIBI_NGROK_DOMAIN"}
	rec, err := liveartifact.New("ngrok", envs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rec.Close(envs...); err != nil {
			t.Errorf("artifact: %v", err)
		}
		t.Logf("artifact: %s", rec.Path())
	})
	port, cleanup := liveHTTPSServer(t)
	defer cleanup()
	rec.Record("local-https", map[string]any{"port": port})
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	ng := NewNgrokFromEnv()
	if err := ng.Enable(ctx, port); err != nil {
		rec.Error("enable", err)
		t.Fatal(err)
	}
	defer ng.Disable(context.Background())
	if url, err := ng.URL(ctx); err != nil || url == "" {
		rec.Error("url", err)
		t.Fatalf("url=%q err=%v", url, err)
	} else {
		rec.Record("url", map[string]any{"url": url})
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
