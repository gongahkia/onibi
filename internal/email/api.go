package email

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/smtp"
	"strings"
)

type SendMailFunc func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error

type Client struct {
	Addr     string
	Host     string
	Username string
	Password string
	From     string
	SendMail SendMailFunc
}

type Message struct {
	To      string
	Subject string
	Body    string
}

func New(addr, host, username, password, from string) *Client {
	return &Client{Addr: addr, Host: host, Username: username, Password: password, From: from}
}

func (c *Client) Send(ctx context.Context, msg Message) error {
	if c == nil {
		return errors.New("email client nil")
	}
	addr := strings.TrimSpace(c.Addr)
	from := sanitizeHeader(c.From)
	to := sanitizeHeader(msg.To)
	subject := sanitizeHeader(msg.Subject)
	switch {
	case addr == "":
		return errors.New("smtp addr required")
	case from == "":
		return errors.New("email from required")
	case to == "":
		return errors.New("email recipient required")
	case subject == "":
		return errors.New("email subject required")
	}
	host := strings.TrimSpace(c.Host)
	if host == "" {
		host = smtpHost(addr)
	}
	var auth smtp.Auth
	if strings.TrimSpace(c.Username) != "" || strings.TrimSpace(c.Password) != "" {
		auth = smtp.PlainAuth("", strings.TrimSpace(c.Username), c.Password, host)
	}
	raw := buildMessage(from, to, subject, msg.Body)
	sendMail := c.SendMail
	if sendMail == nil {
		sendMail = smtp.SendMail
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	errCh := make(chan error, 1)
	go func() { errCh <- sendMail(addr, auth, from, []string{to}, raw) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func buildMessage(from, to, subject, body string) []byte {
	var b bytes.Buffer
	writeHeader := func(k, v string) {
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\r\n")
	}
	writeHeader("From", from)
	writeHeader("To", to)
	writeHeader("Subject", subject)
	writeHeader("MIME-Version", "1.0")
	writeHeader("Content-Type", "text/plain; charset=utf-8")
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.Bytes()
}

func sanitizeHeader(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Join(strings.Fields(s), " ")
}

func smtpHost(addr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err == nil {
		return host
	}
	if i := strings.LastIndex(addr, ":"); i > 0 {
		return addr[:i]
	}
	return addr
}
