package ssh

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"sync"
)

const (
	defaultTunnelLocalAddr  = "127.0.0.1:0"
	defaultTunnelRemoteAddr = "127.0.0.1:8443"
	defaultTunnelScheme     = "https"
)

type TunnelOptions struct {
	LocalAddr  string
	RemoteAddr string
	URLScheme  string
}

type Tunnel struct {
	ln     net.Listener
	cancel context.CancelFunc
	done   chan struct{}
	url    string
	remote string
}

type tunnelDialer interface {
	Dial(network, addr string) (net.Conn, error)
}

func (c *Client) StartTunnel(ctx context.Context, opts TunnelOptions) (*Tunnel, error) {
	return startTunnel(ctx, c, opts)
}

func (t *Tunnel) URL() string {
	return t.url
}

func (t *Tunnel) Addr() net.Addr {
	return t.ln.Addr()
}

func (t *Tunnel) Close() error {
	t.cancel()
	err := t.ln.Close()
	<-t.done
	return err
}

func startTunnel(ctx context.Context, dialer tunnelDialer, opts TunnelOptions) (*Tunnel, error) {
	opts = normalizeTunnelOptions(opts)
	ln, err := net.Listen("tcp", opts.LocalAddr)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	t := &Tunnel{
		ln:     ln,
		cancel: cancel,
		done:   make(chan struct{}),
		url:    tunnelURL(ln.Addr(), opts.URLScheme),
		remote: opts.RemoteAddr,
	}
	go t.accept(ctx, dialer, opts.RemoteAddr)
	return t, nil
}

func (t *Tunnel) accept(ctx context.Context, dialer tunnelDialer, remoteAddr string) {
	defer close(t.done)
	for {
		local, err := t.ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			return
		}
		go proxyTunnelConn(local, dialer, remoteAddr)
	}
}

func proxyTunnelConn(local net.Conn, dialer tunnelDialer, remoteAddr string) {
	remote, err := dialer.Dial("tcp", remoteAddr)
	if err != nil {
		_ = local.Close()
		return
	}
	var once sync.Once
	closeBoth := func() {
		_ = local.Close()
		_ = remote.Close()
	}
	go func() {
		_, _ = io.Copy(remote, local)
		once.Do(closeBoth)
	}()
	go func() {
		_, _ = io.Copy(local, remote)
		once.Do(closeBoth)
	}()
}

func normalizeTunnelOptions(opts TunnelOptions) TunnelOptions {
	if opts.LocalAddr == "" {
		opts.LocalAddr = defaultTunnelLocalAddr
	}
	if opts.RemoteAddr == "" {
		opts.RemoteAddr = defaultTunnelRemoteAddr
	}
	if opts.URLScheme == "" {
		opts.URLScheme = defaultTunnelScheme
	}
	return opts
}

func tunnelURL(addr net.Addr, scheme string) string {
	host := "127.0.0.1"
	port := ""
	if tcp, ok := addr.(*net.TCPAddr); ok {
		if tcp.IP != nil && !tcp.IP.IsUnspecified() {
			host = tcp.IP.String()
		}
		port = strconv.Itoa(tcp.Port)
	} else if addr != nil {
		if h, p, err := net.SplitHostPort(addr.String()); err == nil {
			if h != "" && h != "::" && h != "0.0.0.0" {
				host = h
			}
			port = p
		}
	}
	return (&url.URL{Scheme: scheme, Host: net.JoinHostPort(host, port)}).String()
}

func (t *Tunnel) String() string {
	return fmt.Sprintf("%s -> %s", t.URL(), t.remote)
}
