package ssh

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const defaultPort = "22"

type Client struct {
	*xssh.Client
	mu        sync.Mutex
	reconnect func() (*xssh.Client, error)
	closed    bool
}

type Options struct {
	KnownHostsPath string
	In             io.Reader
	Out            io.Writer
	Timeout        time.Duration
}

type HostKeyPrompt func(host string, remote net.Addr, key xssh.PublicKey) (bool, error)

func Connect(host, user string, key []byte) (*Client, error) {
	return ConnectWithOptions(host, user, key, Options{})
}

func ConnectWithOptions(host, user string, key []byte, opts Options) (*Client, error) {
	host = strings.TrimSpace(host)
	user = strings.TrimSpace(user)
	if host == "" {
		return nil, errors.New("ssh: host required")
	}
	if user == "" {
		return nil, errors.New("ssh: user required")
	}
	signer, err := xssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("ssh: parse private key: %w", err)
	}
	knownHostsPath, err := resolveKnownHostsPath(opts.KnownHostsPath)
	if err != nil {
		return nil, err
	}
	if err := ensureKnownHostsFile(knownHostsPath); err != nil {
		return nil, err
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	cfg := &xssh.ClientConfig{
		User:            user,
		Auth:            []xssh.AuthMethod{xssh.PublicKeys(signer)},
		HostKeyCallback: hostKeyCallback(knownHostsPath, defaultPrompt(opts.In, opts.Out)),
		Timeout:         timeout,
	}
	dial := func() (*xssh.Client, error) {
		return xssh.Dial("tcp", normalizeDialAddress(host), cfg)
	}
	client, err := dial()
	if err != nil {
		return nil, err
	}
	return &Client{Client: client, reconnect: dial}, nil
}

func (c *Client) Dial(network, addr string) (net.Conn, error) {
	c.mu.Lock()
	client := c.Client
	closed := c.closed
	c.mu.Unlock()
	if closed || client == nil {
		return nil, net.ErrClosed
	}
	return client.Dial(network, addr)
}

func (c *Client) ReconnectTunnel() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return net.ErrClosed
	}
	if c.reconnect == nil {
		return errors.New("ssh: tunnel reconnect unavailable")
	}
	next, err := c.reconnect()
	if err != nil {
		return fmt.Errorf("ssh: reconnect tunnel: %w", err)
	}
	previous := c.Client
	c.Client = next
	if previous != nil {
		_ = previous.Close()
	}
	return nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	client := c.Client
	c.mu.Unlock()
	if client == nil {
		return nil
	}
	return client.Close()
}

func hostKeyCallback(path string, prompt HostKeyPrompt) xssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key xssh.PublicKey) error {
		if err := ensureKnownHostsFile(path); err != nil {
			return err
		}
		check, err := knownhosts.New(path)
		if err != nil {
			return err
		}
		if err := check(hostname, remote, key); err == nil {
			return nil
		} else {
			var keyErr *knownhosts.KeyError
			if !errors.As(err, &keyErr) {
				return err
			}
			if len(keyErr.Want) > 0 {
				return fmt.Errorf("ssh: host key mismatch for %s (got %s); refusing automatic replacement; confirm reprovisioning then update %s: %w", knownHostAddress(hostname, remote), xssh.FingerprintSHA256(key), path, err)
			}
			ok, promptErr := prompt(hostname, remote, key)
			if promptErr != nil {
				return promptErr
			}
			if !ok {
				return fmt.Errorf("ssh: unknown host key rejected for %s", hostname)
			}
			return appendKnownHost(path, knownHostAddress(hostname, remote), key)
		}
	}
}

func appendKnownHost(path, host string, key xssh.PublicKey) error {
	if err := ensureKnownHostsFile(path); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, knownhosts.Line([]string{host}, key))
	return err
}

func ensureKnownHostsFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	return f.Close()
}

func resolveKnownHostsPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path != "" {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("ssh: user home: %w", err)
	}
	return filepath.Join(home, ".ssh", "known_hosts"), nil
}

func normalizeDialAddress(host string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	return net.JoinHostPort(host, defaultPort)
}

func knownHostAddress(hostname string, remote net.Addr) string {
	if hostname = strings.TrimSpace(hostname); hostname != "" {
		return hostname
	}
	if remote != nil {
		return remote.String()
	}
	return ""
}

func defaultPrompt(in io.Reader, out io.Writer) HostKeyPrompt {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	return func(host string, remote net.Addr, key xssh.PublicKey) (bool, error) {
		addr := knownHostAddress(host, remote)
		fmt.Fprintf(out, "Unknown SSH host key for %s\n", addr)
		fmt.Fprintf(out, "%s %s\n", key.Type(), xssh.FingerprintSHA256(key))
		fmt.Fprint(out, "Type yes to trust this host key: ")
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}
		return strings.TrimSpace(strings.ToLower(line)) == "yes", nil
	}
}
