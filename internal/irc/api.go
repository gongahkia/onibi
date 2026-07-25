package irc

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	DefaultHost       = "irc.libera.chat"
	DefaultPort       = "6697"
	MaxLineBytes      = 4096
	MaxMessageRunes   = 400
	defaultSendPeriod = time.Second
)

type Config struct {
	Host     string
	Port     string
	Nick     string
	Account  string
	Password string
	Dial     func(context.Context, string, string) (net.Conn, error)
}

func (c Config) Address() string {
	host := strings.TrimSpace(c.Host)
	if host == "" {
		host = DefaultHost
	}
	port := strings.TrimSpace(c.Port)
	if port == "" {
		port = DefaultPort
	}
	return net.JoinHostPort(host, port)
}

func (c Config) ServerName() string {
	if host := strings.TrimSpace(c.Host); host != "" {
		return host
	}
	return DefaultHost
}

func (c Config) Validate() error {
	for name, value := range map[string]string{"nick": c.Nick, "account": c.Account, "password": c.Password} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("irc %s required", name)
		}
	}
	if strings.ContainsAny(c.Nick+c.Account, " \r\n") {
		return errors.New("irc nick/account contains whitespace")
	}
	return nil
}

type Message struct {
	Tags     map[string]string
	Prefix   string
	Command  string
	Params   []string
	Trailing string
}

func (m Message) Nick() string {
	nick, _, _ := strings.Cut(m.Prefix, "!")
	return nick
}

type Client struct {
	Config Config

	mu       sync.Mutex
	conn     net.Conn
	r        *bufio.Reader
	w        *bufio.Writer
	nextSend time.Time
}

func NewClient(cfg Config) *Client { return &Client{Config: cfg} }

func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

func (c *Client) Connect(ctx context.Context) error {
	if c == nil {
		return errors.New("irc client nil")
	}
	if err := c.Config.Validate(); err != nil {
		return err
	}
	if c.Connected() {
		return nil
	}
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.conn = conn
	c.r = bufio.NewReaderSize(conn, MaxLineBytes)
	c.w = bufio.NewWriterSize(conn, MaxLineBytes)
	c.nextSend = time.Time{}
	c.mu.Unlock()
	if err := c.handshake(ctx); err != nil {
		_ = c.Close()
		return err
	}
	return nil
}

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	if c.Config.Dial != nil {
		return c.Config.Dial(ctx, "tcp", c.Config.Address())
	}
	d := &tls.Dialer{Config: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: c.Config.ServerName()}}
	return d.DialContext(ctx, "tcp", c.Config.Address())
}

func (c *Client) handshake(ctx context.Context) error {
	if err := c.writeLine(ctx, "CAP LS 302", false); err != nil {
		return err
	}
	if err := c.writeLine(ctx, "NICK "+c.Config.Nick, false); err != nil {
		return err
	}
	if err := c.writeLine(ctx, "USER "+c.Config.Nick+" 0 * :Onibi IRC cockpit", false); err != nil {
		return err
	}
	seenLS := false
	sentAuth := false
	sentEnd := false
	for {
		msg, err := c.readMessage(ctx)
		if err != nil {
			return err
		}
		if strings.EqualFold(msg.Command, "PING") {
			if err := c.writeLine(ctx, "PONG :"+msg.Trailing, false); err != nil {
				return err
			}
			continue
		}
		switch msg.Command {
		case "CAP":
			if len(msg.Params) < 2 {
				continue
			}
			kind := strings.ToUpper(msg.Params[1])
			caps := msg.Trailing
			if kind == "LS" {
				seenLS = true
				if !hasCap(caps, "sasl") {
					return errors.New("irc server does not offer sasl")
				}
				if err := c.writeLine(ctx, "CAP REQ :sasl", false); err != nil {
					return err
				}
			}
			if kind == "ACK" && hasCap(caps, "sasl") && !sentAuth {
				sentAuth = true
				if err := c.writeLine(ctx, "AUTHENTICATE PLAIN", false); err != nil {
					return err
				}
			}
		case "AUTHENTICATE":
			if sentAuth && strings.TrimSpace(msg.Params[0]) == "+" {
				blob := base64.StdEncoding.EncodeToString([]byte(c.Config.Account + "\x00" + c.Config.Account + "\x00" + c.Config.Password))
				for len(blob) > 400 {
					if err := c.writeLine(ctx, "AUTHENTICATE "+blob[:400], false); err != nil {
						return err
					}
					blob = blob[400:]
				}
				if err := c.writeLine(ctx, "AUTHENTICATE "+blob, false); err != nil {
					return err
				}
			}
		case "903":
			if !sentEnd {
				sentEnd = true
				if err := c.writeLine(ctx, "CAP END", false); err != nil {
					return err
				}
			}
		case "904", "905", "906", "907", "908":
			return fmt.Errorf("irc sasl authentication failed: %s", strings.TrimSpace(msg.Trailing))
		case "001":
			if !seenLS || !sentEnd {
				return errors.New("irc registered before sasl completed")
			}
			return nil
		case "ERROR":
			return fmt.Errorf("irc server error: %s", strings.TrimSpace(msg.Trailing))
		}
	}
}

func hasCap(caps, want string) bool {
	for _, cap := range strings.Fields(caps) {
		name, _, _ := strings.Cut(strings.TrimPrefix(cap, "-"), "=")
		if name == want {
			return true
		}
	}
	return false
}

func (c *Client) Run(ctx context.Context, handle func(Message) error) error {
	if c == nil || !c.Connected() {
		return errors.New("irc client not connected")
	}
	for {
		msg, err := c.readMessage(ctx)
		if err != nil {
			return err
		}
		if strings.EqualFold(msg.Command, "PING") {
			if err := c.writeLine(ctx, "PONG :"+msg.Trailing, false); err != nil {
				return err
			}
			continue
		}
		if handle != nil {
			if err := handle(msg); err != nil {
				return err
			}
		}
	}
}

func (c *Client) SendPrivmsg(ctx context.Context, target, text string) error {
	if strings.TrimSpace(target) == "" {
		return errors.New("irc target required")
	}
	for _, chunk := range ChunkText(text, MaxMessageRunes) {
		if err := c.writeLine(ctx, "PRIVMSG "+target+" :"+chunk, true); err != nil {
			return err
		}
	}
	return nil
}

func ChunkText(text string, limit int) []string {
	text = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "\r", ""), "\n", " "))
	if text == "" {
		return []string{"(empty)"}
	}
	if limit <= 0 {
		limit = MaxMessageRunes
	}
	if utf8.RuneCountInString(text) <= limit {
		return []string{text}
	}
	var out []string
	buf := make([]rune, 0, limit)
	for _, r := range text {
		buf = append(buf, r)
		if len(buf) == limit {
			out = append(out, string(buf))
			buf = buf[:0]
		}
	}
	if len(buf) > 0 {
		out = append(out, string(buf))
	}
	return out
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.r = nil
	c.w = nil
	c.mu.Unlock()
	if conn == nil {
		return nil
	}
	_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
	_, _ = conn.Write([]byte("QUIT :Onibi stopping\r\n"))
	return conn.Close()
}

func (c *Client) readMessage(ctx context.Context) (Message, error) {
	if err := contextDeadline(ctx, c.connection()); err != nil {
		return Message{}, err
	}
	c.mu.Lock()
	r := c.r
	c.mu.Unlock()
	if r == nil {
		return Message{}, errors.New("irc client not connected")
	}
	line, err := r.ReadString('\n')
	if err != nil {
		return Message{}, err
	}
	if len(line) > MaxLineBytes {
		return Message{}, errors.New("irc line exceeds limit")
	}
	return ParseLine(line)
}

func (c *Client) writeLine(ctx context.Context, line string, limited bool) error {
	if strings.ContainsAny(line, "\r\n") || len(line)+2 > 510 {
		return errors.New("invalid or oversized irc line")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil || c.w == nil {
		return errors.New("irc client not connected")
	}
	if limited {
		if wait := time.Until(c.nextSend); wait > 0 {
			c.mu.Unlock()
			if err := sleepContext(ctx, wait); err != nil {
				c.mu.Lock()
				return err
			}
			c.mu.Lock()
			if c.conn == nil || c.w == nil {
				return errors.New("irc client not connected")
			}
		}
		c.nextSend = time.Now().Add(defaultSendPeriod)
	}
	if err := contextDeadline(ctx, c.conn); err != nil {
		return err
	}
	if _, err := c.w.WriteString(line + "\r\n"); err != nil {
		return err
	}
	return c.w.Flush()
}

func (c *Client) connection() net.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn
}

func contextDeadline(ctx context.Context, conn net.Conn) error {
	if conn == nil {
		return errors.New("irc client not connected")
	}
	if deadline, ok := ctx.Deadline(); ok {
		return conn.SetDeadline(deadline)
	}
	return conn.SetDeadline(time.Time{})
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func ParseLine(line string) (Message, error) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return Message{}, errors.New("empty irc line")
	}
	m := Message{Tags: map[string]string{}}
	if strings.HasPrefix(line, "@") {
		part, rest, ok := strings.Cut(line, " ")
		if !ok {
			return Message{}, errors.New("malformed irc tags")
		}
		for _, tag := range strings.Split(strings.TrimPrefix(part, "@"), ";") {
			key, value, _ := strings.Cut(tag, "=")
			if key != "" {
				m.Tags[key] = value
			}
		}
		line = rest
	}
	if strings.HasPrefix(line, ":") {
		part, rest, ok := strings.Cut(line, " ")
		if !ok {
			return Message{}, errors.New("malformed irc prefix")
		}
		m.Prefix = strings.TrimPrefix(part, ":")
		line = rest
	}
	if before, after, ok := strings.Cut(line, " :"); ok {
		m.Trailing = after
		line = before
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return Message{}, errors.New("missing irc command")
	}
	m.Command = strings.ToUpper(fields[0])
	m.Params = fields[1:]
	return m, nil
}
