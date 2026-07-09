package irc

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/chatout"
)

const (
	DefaultAddr       = "irc.libera.chat:6697"
	DefaultRealName   = "Onibi"
	MessageChunkLimit = 400
	DefaultSendPace   = 2 * time.Second
)

type DialFunc func(context.Context, string, string) (net.Conn, error)

type Client struct {
	Addr       string
	Nick       string
	Username   string
	RealName   string
	Password   string
	Plaintext  bool
	Dial       DialFunc
	Sleep      func(context.Context, time.Duration) error
	SendPace   time.Duration
	RetryMin   time.Duration
	RetryMax   time.Duration
	AfterError func(error, time.Duration, int)

	mu       sync.Mutex
	writeMu  sync.Mutex
	paceMu   sync.Mutex
	conn     net.Conn
	reader   *bufio.Reader
	lastSend time.Time
}

type Message struct {
	Raw      string
	Prefix   string
	Command  string
	Params   []string
	Trailing string
}

func (m Message) Nick() string {
	prefix := strings.TrimSpace(m.Prefix)
	if prefix == "" {
		return ""
	}
	for _, sep := range []string{"!", "@"} {
		if i := strings.Index(prefix, sep); i >= 0 {
			return prefix[:i]
		}
	}
	return prefix
}

func New(addr, nick, username, password string) *Client {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = DefaultAddr
	}
	username = strings.TrimSpace(username)
	if username == "" {
		username = strings.TrimSpace(nick)
	}
	return &Client{
		Addr:     addr,
		Nick:     strings.TrimSpace(nick),
		Username: username,
		RealName: DefaultRealName,
		Password: strings.TrimSpace(password),
	}
}

func (c *Client) Connect(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return err
	}
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.conn = conn
	c.reader = bufio.NewReader(conn)
	c.mu.Unlock()
	if c.Password != "" {
		if err := c.writeRaw(ctx, "CAP REQ :sasl"); err != nil {
			return err
		}
	}
	if err := c.writeRaw(ctx, "NICK "+c.Nick); err != nil {
		return err
	}
	real := strings.TrimSpace(c.RealName)
	if real == "" {
		real = DefaultRealName
	}
	if err := c.writeRaw(ctx, "USER "+c.Username+" 0 * :"+real); err != nil {
		return err
	}
	saslDone := c.Password == ""
	capEnded := c.Password == ""
	for {
		msg, err := c.ReadMessage(ctx)
		if err != nil {
			return err
		}
		switch msg.Command {
		case "PING":
			if err := c.pong(ctx, msg); err != nil {
				return err
			}
		case "CAP":
			if c.Password == "" {
				continue
			}
			if len(msg.Params) >= 2 && strings.EqualFold(msg.Params[1], "ACK") && strings.Contains(strings.ToLower(msg.Trailing), "sasl") {
				if err := c.writeRaw(ctx, "AUTHENTICATE PLAIN"); err != nil {
					return err
				}
			}
			if len(msg.Params) >= 2 && strings.EqualFold(msg.Params[1], "NAK") {
				return errors.New("irc server rejected sasl capability")
			}
		case "AUTHENTICATE":
			if c.Password != "" && len(msg.Params) > 0 && msg.Params[0] == "+" {
				if err := c.writeRaw(ctx, "AUTHENTICATE "+c.saslPlain()); err != nil {
					return err
				}
			}
		case "903":
			saslDone = true
			if !capEnded {
				if err := c.writeRaw(ctx, "CAP END"); err != nil {
					return err
				}
				capEnded = true
			}
		case "904", "905", "906", "907":
			return fmt.Errorf("irc sasl failed: %s", strings.TrimSpace(msg.Trailing))
		case "001":
			if !saslDone {
				return errors.New("irc registered before sasl completed")
			}
			return nil
		}
	}
}

func (c *Client) RunWithReconnect(ctx context.Context, handle func(Message) error) error {
	attempt := 0
	for {
		err := c.Connect(ctx)
		if err == nil {
			attempt = 0
			err = c.ReadLoop(ctx, handle)
		}
		_ = c.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		delay := reconnectDelay(attempt, c.RetryMin, c.RetryMax)
		attempt++
		if c.AfterError != nil {
			c.AfterError(err, delay, attempt)
		}
		if err := chatout.Sleep(ctx, delay, c.Sleep); err != nil {
			return err
		}
	}
}

func (c *Client) ReadLoop(ctx context.Context, handle func(Message) error) error {
	for {
		msg, err := c.ReadMessage(ctx)
		if err != nil {
			return err
		}
		switch msg.Command {
		case "PING":
			if err := c.pong(ctx, msg); err != nil {
				return err
			}
		case "PRIVMSG":
			if handle != nil {
				if err := handle(msg); err != nil {
					return err
				}
			}
		}
	}
}

func (c *Client) ReadMessage(ctx context.Context) (Message, error) {
	for {
		c.mu.Lock()
		conn := c.conn
		reader := c.reader
		c.mu.Unlock()
		if conn == nil || reader == nil {
			return Message{}, errors.New("irc not connected")
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		line, err := reader.ReadString('\n')
		if err == nil {
			return ParseLine(line)
		}
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			if ctx.Err() != nil {
				return Message{}, ctx.Err()
			}
			continue
		}
		if ctx.Err() != nil {
			return Message{}, ctx.Err()
		}
		return Message{}, err
	}
}

func ParseLine(line string) (Message, error) {
	raw := strings.TrimRight(line, "\r\n")
	rest := raw
	if strings.HasPrefix(rest, "@") {
		if i := strings.IndexByte(rest, ' '); i >= 0 {
			rest = strings.TrimSpace(rest[i+1:])
		}
	}
	msg := Message{Raw: raw}
	if strings.HasPrefix(rest, ":") {
		i := strings.IndexByte(rest, ' ')
		if i < 0 {
			return Message{}, errors.New("irc prefix without command")
		}
		msg.Prefix = rest[1:i]
		rest = strings.TrimSpace(rest[i+1:])
	}
	if i := strings.Index(rest, " :"); i >= 0 {
		msg.Trailing = rest[i+2:]
		rest = strings.TrimSpace(rest[:i])
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return Message{}, errors.New("irc command missing")
	}
	msg.Command = strings.ToUpper(fields[0])
	if len(fields) > 1 {
		msg.Params = append([]string(nil), fields[1:]...)
	}
	if msg.Trailing == "" && (msg.Command == "PING" || msg.Command == "AUTHENTICATE") && len(msg.Params) > 0 {
		msg.Trailing = msg.Params[len(msg.Params)-1]
	}
	return msg, nil
}

func (c *Client) SendPrivmsg(ctx context.Context, target, text string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return errors.New("irc privmsg target required")
	}
	for _, chunk := range chatout.Chunks(sanitizeText(text), MessageChunkLimit) {
		if err := c.waitSendPace(ctx); err != nil {
			return err
		}
		if err := c.writeRaw(ctx, "PRIVMSG "+target+" :"+chunk); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	c.reader = nil
	return err
}

func (c *Client) SetConnForTest(conn net.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn = conn
	if conn == nil {
		c.reader = nil
		return
	}
	c.reader = bufio.NewReader(conn)
}

func (c *Client) validate() error {
	if c == nil {
		return errors.New("irc client nil")
	}
	if strings.TrimSpace(c.Addr) == "" {
		c.Addr = DefaultAddr
	}
	c.Nick = strings.TrimSpace(c.Nick)
	c.Username = strings.TrimSpace(c.Username)
	if c.Username == "" {
		c.Username = c.Nick
	}
	if c.Nick == "" || c.Username == "" {
		return errors.New("irc nick and username required")
	}
	return nil
}

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	if c.Dial != nil {
		return c.Dial(ctx, "tcp", c.Addr)
	}
	d := net.Dialer{Timeout: 10 * time.Second}
	if c.Plaintext {
		return d.DialContext(ctx, "tcp", c.Addr)
	}
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil {
		host = c.Addr
	}
	return tls.DialWithDialer(&d, "tcp", c.Addr, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host})
}

func (c *Client) saslPlain() string {
	authzid := ""
	payload := authzid + "\x00" + c.Username + "\x00" + c.Password
	return base64.StdEncoding.EncodeToString([]byte(payload))
}

func (c *Client) pong(ctx context.Context, msg Message) error {
	token := strings.TrimSpace(msg.Trailing)
	if token == "" && len(msg.Params) > 0 {
		token = msg.Params[0]
	}
	if token == "" {
		return c.writeRaw(ctx, "PONG")
	}
	return c.writeRaw(ctx, "PONG :"+token)
}

func (c *Client) writeRaw(ctx context.Context, line string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return errors.New("irc not connected")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := io.WriteString(conn, line+"\r\n")
	return err
}

func (c *Client) waitSendPace(ctx context.Context) error {
	pace := c.SendPace
	if pace < 0 {
		return nil
	}
	if pace == 0 {
		pace = DefaultSendPace
	}
	c.paceMu.Lock()
	defer c.paceMu.Unlock()
	if !c.lastSend.IsZero() {
		if wait := time.Until(c.lastSend.Add(pace)); wait > 0 {
			if err := chatout.Sleep(ctx, wait, c.Sleep); err != nil {
				return err
			}
		}
	}
	c.lastSend = time.Now()
	return nil
}

func reconnectDelay(attempt int, minBackoff, maxBackoff time.Duration) time.Duration {
	if minBackoff <= 0 {
		minBackoff = time.Second
	}
	if maxBackoff <= 0 || maxBackoff < minBackoff {
		maxBackoff = 30 * time.Second
	}
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 5 {
		attempt = 5
	}
	base := minBackoff << attempt
	if base > maxBackoff {
		base = maxBackoff
	}
	jitter := time.Duration(rand.Int63n(int64(base/2) + 1))
	return base + jitter
}

func sanitizeText(text string) string {
	text = strings.NewReplacer("\r", " ", "\n", " ").Replace(strings.TrimSpace(text))
	if text == "" {
		return "(empty)"
	}
	return text
}

func ParseOnibiCommand(text string) (string, string, bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) < 3 || strings.ToLower(fields[0]) != "!onibi" {
		return "", "", false
	}
	action := strings.ToLower(fields[1])
	switch action {
	case "approve", "ap", "deny", "dn":
	default:
		return "", "", false
	}
	return action, fields[2], true
}

func FormatOnibiCommand(action, id string) string {
	return "!onibi " + strings.ToLower(strings.TrimSpace(action)) + " " + strings.TrimSpace(id)
}
