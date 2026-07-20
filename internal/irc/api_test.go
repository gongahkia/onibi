package irc

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestParseMessage(t *testing.T) {
	msg, err := ParseMessage("@time=1 :owner!user@host PRIVMSG onibi :hello world\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if msg.Prefix != "owner!user@host" || msg.Command != "PRIVMSG" || len(msg.Params) != 2 || msg.Params[0] != "onibi" || msg.Params[1] != "hello world" {
		t.Fatalf("message = %#v", msg)
	}
	for _, line := range []string{"", ":bad", "@tag"} {
		if _, err := ParseMessage(line); err == nil {
			t.Fatalf("ParseMessage(%q) succeeded", line)
		}
	}
}

func TestClientConnectsWithSASLPlainAndSendsDM(t *testing.T) {
	server, clientConn := net.Pipe()
	serverDone := make(chan error, 1)
	go func() {
		defer server.Close()
		r := bufio.NewReader(server)
		for _, want := range []string{"CAP LS 302", "NICK onibi", "USER owner 0 * :onibi"} {
			if got, err := readIRCLine(r); err != nil || got != want {
				serverDone <- fmt.Errorf("got %q err=%v want %q", got, err, want)
				return
			}
		}
		if _, err := server.Write([]byte(":irc.example CAP * LS :multi-prefix sasl\r\n")); err != nil {
			serverDone <- err
			return
		}
		if got, err := readIRCLine(r); err != nil || got != "CAP REQ :sasl" {
			serverDone <- fmt.Errorf("cap req got %q err=%v", got, err)
			return
		}
		if _, err := server.Write([]byte(":irc.example CAP * ACK :sasl\r\n")); err != nil {
			serverDone <- err
			return
		}
		if got, err := readIRCLine(r); err != nil || got != "AUTHENTICATE PLAIN" {
			serverDone <- fmt.Errorf("auth mechanism got %q err=%v", got, err)
			return
		}
		if _, err := server.Write([]byte("AUTHENTICATE +\r\n")); err != nil {
			serverDone <- err
			return
		}
		encoded, err := readIRCLine(r)
		if err != nil || !strings.HasPrefix(encoded, "AUTHENTICATE ") {
			serverDone <- fmt.Errorf("auth payload got %q err=%v", encoded, err)
			return
		}
		payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, "AUTHENTICATE "))
		if err != nil || string(payload) != "\x00owner\x00password" {
			serverDone <- fmt.Errorf("auth payload=%q err=%v", payload, err)
			return
		}
		if _, err := server.Write([]byte(":irc.example 903 onibi :SASL successful\r\n")); err != nil {
			serverDone <- err
			return
		}
		if got, err := readIRCLine(r); err != nil || got != "CAP END" {
			serverDone <- fmt.Errorf("cap end got %q err=%v", got, err)
			return
		}
		if got, err := readIRCLine(r); err != nil || got != "PRIVMSG owner :hello" {
			serverDone <- fmt.Errorf("dm got %q err=%v", got, err)
			return
		}
		serverDone <- nil
	}()
	c := NewClient(Config{Nick: "onibi", Username: "owner", Password: "password"})
	c.DialTLS = func(context.Context, string, string, *tls.Config) (net.Conn, error) {
		return clientConn, nil
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.SendPrivmsg(ctx, "owner", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeConfigRequiresTLSDefaultsAndSASLValues(t *testing.T) {
	cfg, err := normalizeConfig(Config{Nick: "onibi", Username: "owner", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Address != DefaultAddress || cfg.TLS == nil || cfg.TLS.ServerName != "irc.libera.chat" || cfg.TLS.MinVersion < 0x0303 {
		t.Fatalf("config = %#v", cfg)
	}
	for _, invalid := range []Config{{}, {Nick: "onibi", Username: "owner", Password: ""}, {Nick: "bad nick", Username: "owner", Password: "password"}, {Address: "invalid", Nick: "onibi", Username: "owner", Password: "password"}, {Nick: "onibi", Username: "owner", Password: "password", TLS: &tls.Config{InsecureSkipVerify: true}}} {
		if _, err := normalizeConfig(invalid); err == nil {
			t.Fatalf("normalizeConfig(%#v) succeeded", invalid)
		}
	}
}

func TestSessionRejectsOversizeLinesAndHonorsCancellation(t *testing.T) {
	server, client := net.Pipe()
	s := &Session{conn: client, read: bufio.NewReaderSize(client, MaxLineBytes+2)}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	go func() {
		defer server.Close()
		_, _ = server.Write(append(bytes.Repeat([]byte{'x'}, MaxLineBytes+1), '\r', '\n'))
	}()
	if _, err := s.next(ctx); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversize err = %v", err)
	}
	_ = client.Close()

	server, client = net.Pipe()
	defer server.Close()
	defer client.Close()
	s = &Session{conn: client, read: bufio.NewReaderSize(client, MaxLineBytes+2)}
	ctx, cancel = context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() {
		_, err := s.next(ctx)
		errCh <- err
	}()
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel err = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("read did not honor cancellation")
	}
}

func readIRCLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), err
}
