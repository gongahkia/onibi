package ssh

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

type tcpDialer struct{}

func (tcpDialer) Dial(network, addr string) (net.Conn, error) {
	return net.Dial(network, addr)
}

func TestStartTunnelProxiesLocalToRemote(t *testing.T) {
	remote := startEchoServer(t)
	tun, err := startTunnel(context.Background(), tcpDialer{}, TunnelOptions{RemoteAddr: remote.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	defer tun.Close()
	c, err := net.Dial("tcp", tun.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("ping\n")); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if line != "ping\n" {
		t.Fatalf("line = %q", line)
	}
	if !strings.HasPrefix(tun.URL(), "https://127.0.0.1:") {
		t.Fatalf("url = %q", tun.URL())
	}
}

func TestNormalizeTunnelOptions(t *testing.T) {
	got := normalizeTunnelOptions(TunnelOptions{})
	if got.LocalAddr != defaultTunnelLocalAddr || got.RemoteAddr != defaultTunnelRemoteAddr || got.URLScheme != defaultTunnelScheme {
		t.Fatalf("opts = %#v", got)
	}
}

func TestTunnelURLUsesLoopbackForUnspecifiedAddr(t *testing.T) {
	addr := &net.TCPAddr{IP: net.IPv4zero, Port: 49152}
	if got := tunnelURL(addr, "https"); got != "https://127.0.0.1:49152" {
		t.Fatalf("url = %q", got)
	}
}

func TestStartTunnelRetriesFailedForwardAfterReconnect(t *testing.T) {
	remote := startEchoServer(t)
	dialer := &flakyTunnelDialer{}
	tun, err := startTunnel(context.Background(), dialer, TunnelOptions{RemoteAddr: remote.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	defer tun.Close()
	c, err := net.Dial("tcp", tun.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("reconnect\n")); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if line, err := bufio.NewReader(c).ReadString('\n'); err != nil || line != "reconnect\n" {
		t.Fatalf("line=%q err=%v", line, err)
	}
	if got, want := dialer.reconnects, 1; got != want {
		t.Fatalf("reconnects=%d, want %d", got, want)
	}
}

type flakyTunnelDialer struct {
	mu         sync.Mutex
	attempts   int
	reconnects int
}

func (d *flakyTunnelDialer) Dial(network, addr string) (net.Conn, error) {
	d.mu.Lock()
	d.attempts++
	attempt := d.attempts
	d.mu.Unlock()
	if attempt == 1 {
		return nil, errors.New("temporary ssh forward failure")
	}
	return net.Dial(network, addr)
}

func (d *flakyTunnelDialer) ReconnectTunnel() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.reconnects++
	return nil
}

func startEchoServer(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	return ln
}
