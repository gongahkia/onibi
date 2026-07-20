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
)

const (
	DefaultAddress = "irc.libera.chat:6697"
	MaxLineBytes   = 510
)

type Message struct {
	Prefix  string
	Command string
	Params  []string
}

type Config struct {
	Address  string
	Nick     string
	Username string
	Password string
	TLS      *tls.Config
}

type DialTLSFunc func(context.Context, string, string, *tls.Config) (net.Conn, error)

type Client struct {
	Config  Config
	DialTLS DialTLSFunc

	mu      sync.Mutex
	session *Session
}

type Session struct {
	conn net.Conn
	read *bufio.Reader

	writeMu sync.Mutex
}

func NewClient(cfg Config) *Client {
	return &Client{Config: cfg}
}

func (c *Client) Connect(ctx context.Context) error {
	if c == nil {
		return errors.New("irc client nil")
	}
	cfg, err := normalizeConfig(c.Config)
	if err != nil {
		return err
	}
	dial := c.DialTLS
	if dial == nil {
		dial = defaultDialTLS
	}
	conn, err := dial(ctx, "tcp", cfg.Address, cfg.TLS)
	if err != nil {
		return err
	}
	s := &Session{conn: conn, read: bufio.NewReaderSize(conn, MaxLineBytes+2)}
	if err := s.authenticate(ctx, cfg); err != nil {
		_ = conn.Close()
		return err
	}
	c.mu.Lock()
	old := c.session
	c.session = s
	c.mu.Unlock()
	if old != nil {
		_ = old.conn.Close()
	}
	return nil
}

func (c *Client) Next(ctx context.Context) (Message, error) {
	s, err := c.currentSession()
	if err != nil {
		return Message{}, err
	}
	return s.next(ctx)
}

func (c *Client) SendPrivmsg(ctx context.Context, target, text string) error {
	s, err := c.currentSession()
	if err != nil {
		return err
	}
	target = strings.TrimSpace(target)
	if target == "" || strings.ContainsAny(target, " \r\n") {
		return errors.New("irc target required")
	}
	text = sanitizeText(text)
	if text == "" {
		text = "(empty)"
	}
	return s.write(ctx, "PRIVMSG "+target+" :"+text)
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	s := c.session
	c.session = nil
	c.mu.Unlock()
	if s == nil {
		return nil
	}
	return s.conn.Close()
}

func (c *Client) currentSession() (*Session, error) {
	if c == nil {
		return nil, errors.New("irc client nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == nil {
		return nil, errors.New("irc client not connected")
	}
	return c.session, nil
}

func normalizeConfig(cfg Config) (Config, error) {
	if strings.TrimSpace(cfg.Address) == "" {
		cfg.Address = DefaultAddress
	}
	host, _, err := net.SplitHostPort(cfg.Address)
	if err != nil || strings.TrimSpace(host) == "" {
		return Config{}, fmt.Errorf("invalid IRC address %q", cfg.Address)
	}
	cfg.Nick = strings.TrimSpace(cfg.Nick)
	cfg.Username = strings.TrimSpace(cfg.Username)
	cfg.Password = strings.TrimSpace(cfg.Password)
	if cfg.Nick == "" || cfg.Username == "" || cfg.Password == "" {
		return Config{}, errors.New("irc SASL nick, username, and password are required")
	}
	for _, value := range []string{cfg.Nick, cfg.Username, cfg.Password} {
		if strings.ContainsAny(value, "\x00\r\n") {
			return Config{}, errors.New("irc SASL values contain a control character")
		}
	}
	if strings.ContainsAny(cfg.Nick, " ") || strings.ContainsAny(cfg.Username, " ") {
		return Config{}, errors.New("irc nick and username cannot contain spaces")
	}
	if cfg.TLS == nil {
		cfg.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
	} else {
		cfg.TLS = cfg.TLS.Clone()
		if cfg.TLS.InsecureSkipVerify {
			return Config{}, errors.New("irc TLS verification is required")
		}
		if cfg.TLS.ServerName == "" {
			cfg.TLS.ServerName = host
		}
		if cfg.TLS.MinVersion < tls.VersionTLS12 {
			cfg.TLS.MinVersion = tls.VersionTLS12
		}
	}
	return cfg, nil
}

func defaultDialTLS(ctx context.Context, network, address string, cfg *tls.Config) (net.Conn, error) {
	raw, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	conn := tls.Client(raw, cfg)
	if err := conn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return conn, nil
}

func (s *Session) authenticate(ctx context.Context, cfg Config) error {
	for _, line := range []string{
		"CAP LS 302",
		"NICK " + cfg.Nick,
		"USER " + cfg.Username + " 0 * :onibi",
	} {
		if err := s.write(ctx, line); err != nil {
			return err
		}
	}
	requested := false
	started := false
	for {
		msg, err := s.next(ctx)
		if err != nil {
			return err
		}
		if msg.Command == "PING" {
			if err := s.pong(ctx, msg); err != nil {
				return err
			}
			continue
		}
		switch msg.Command {
		case "CAP":
			if len(msg.Params) < 2 {
				continue
			}
			subcommand := strings.ToUpper(msg.Params[1])
			switch subcommand {
			case "LS":
				if requested {
					continue
				}
				if !hasCapability(msg.Params[2:], "sasl") {
					return errors.New("irc server does not advertise SASL")
				}
				if err := s.write(ctx, "CAP REQ :sasl"); err != nil {
					return err
				}
				requested = true
			case "ACK":
				if !requested || started || !hasCapability(msg.Params[2:], "sasl") {
					continue
				}
				if err := s.write(ctx, "AUTHENTICATE PLAIN"); err != nil {
					return err
				}
				started = true
			case "NAK":
				return errors.New("irc server rejected SASL capability")
			}
		case "AUTHENTICATE":
			if !started || len(msg.Params) == 0 || msg.Params[0] != "+" {
				continue
			}
			if err := s.sendSASLPlain(ctx, cfg.Username, cfg.Password); err != nil {
				return err
			}
		case "903":
			if err := s.write(ctx, "CAP END"); err != nil {
				return err
			}
			return nil
		case "904", "905", "906", "907":
			return errors.New("irc SASL authentication failed")
		case "ERROR":
			return errors.New("irc server error: " + strings.Join(msg.Params, " "))
		}
	}
}

func (s *Session) sendSASLPlain(ctx context.Context, username, password string) error {
	encoded := base64.StdEncoding.EncodeToString([]byte("\x00" + username + "\x00" + password))
	for len(encoded) > 400 {
		if err := s.write(ctx, "AUTHENTICATE "+encoded[:400]); err != nil {
			return err
		}
		encoded = encoded[400:]
	}
	if err := s.write(ctx, "AUTHENTICATE "+encoded); err != nil {
		return err
	}
	if len(encoded) == 400 {
		return s.write(ctx, "AUTHENTICATE +")
	}
	return nil
}

func (s *Session) next(ctx context.Context) (Message, error) {
	if s == nil || s.conn == nil || s.read == nil {
		return Message{}, errors.New("irc session unavailable")
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := s.conn.SetReadDeadline(deadline); err != nil {
			return Message{}, err
		}
	}
	defer s.conn.SetReadDeadline(timeZero)
	stop := context.AfterFunc(ctx, func() {
		_ = s.conn.SetReadDeadline(time.Now())
	})
	defer stop()
	line, err := s.read.ReadSlice('\n')
	if err != nil {
		if errors.Is(err, bufio.ErrBufferFull) {
			return Message{}, errors.New("irc line exceeds limit")
		}
		if ctx.Err() != nil {
			return Message{}, ctx.Err()
		}
		return Message{}, err
	}
	if len(line) > MaxLineBytes+2 {
		return Message{}, errors.New("irc line exceeds limit")
	}
	return ParseMessage(string(line))
}

var timeZero = time.Time{}

func (s *Session) pong(ctx context.Context, msg Message) error {
	if len(msg.Params) == 0 {
		return s.write(ctx, "PONG")
	}
	return s.write(ctx, "PONG :"+sanitizeText(msg.Params[len(msg.Params)-1]))
}

func (s *Session) write(ctx context.Context, line string) error {
	if s == nil || s.conn == nil {
		return errors.New("irc session unavailable")
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if strings.ContainsAny(line, "\r\n") || line == "" || len(line) > MaxLineBytes {
		return errors.New("invalid IRC line")
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := s.conn.SetWriteDeadline(deadline); err != nil {
			return err
		}
	}
	defer s.conn.SetWriteDeadline(timeZero)
	stop := context.AfterFunc(ctx, func() {
		_ = s.conn.SetWriteDeadline(time.Now())
	})
	defer stop()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := s.conn.Write([]byte(line + "\r\n"))
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func ParseMessage(line string) (Message, error) {
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if len(line) > MaxLineBytes || strings.ContainsAny(line, "\r\n") {
		return Message{}, errors.New("invalid IRC line")
	}
	if line == "" {
		return Message{}, errors.New("empty IRC line")
	}
	if strings.HasPrefix(line, "@") {
		_, rest, ok := strings.Cut(line, " ")
		if !ok {
			return Message{}, errors.New("invalid IRC tags")
		}
		line = rest
	}
	msg := Message{}
	if strings.HasPrefix(line, ":") {
		var ok bool
		msg.Prefix, line, ok = strings.Cut(strings.TrimPrefix(line, ":"), " ")
		if !ok || msg.Prefix == "" {
			return Message{}, errors.New("invalid IRC prefix")
		}
	}
	command, rest, found := strings.Cut(line, " ")
	msg.Command = strings.ToUpper(strings.TrimSpace(command))
	if msg.Command == "" {
		return Message{}, errors.New("IRC command required")
	}
	if !found {
		return msg, nil
	}
	rest = strings.TrimLeft(rest, " ")
	for rest != "" {
		if strings.HasPrefix(rest, ":") {
			msg.Params = append(msg.Params, strings.TrimPrefix(rest, ":"))
			break
		}
		param, tail, ok := strings.Cut(rest, " ")
		msg.Params = append(msg.Params, param)
		if !ok {
			break
		}
		rest = strings.TrimLeft(tail, " ")
	}
	return msg, nil
}

func hasCapability(params []string, want string) bool {
	for _, param := range params {
		for _, capability := range strings.Fields(param) {
			name, _, _ := strings.Cut(strings.ToLower(capability), "=")
			if name == want {
				return true
			}
		}
	}
	return false
}

func sanitizeText(text string) string {
	return strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(text))
}
