package irc

import (
	"bufio"
	"context"
	"encoding/base64"
	"net"
	"strings"
	"testing"
	"time"
)

func TestParseLine(t *testing.T) {
	msg, err := ParseLine(":owner!u@h PRIVMSG onibi :!onibi approve a1\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if msg.Command != "PRIVMSG" || msg.Nick() != "owner" || msg.Params[0] != "onibi" || msg.Trailing != "!onibi approve a1" {
		t.Fatalf("msg = %#v", msg)
	}
	msg, err = ParseLine("PING :server.example\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if msg.Command != "PING" || msg.Trailing != "server.example" {
		t.Fatalf("ping = %#v", msg)
	}
}

func TestSASLConnectShape(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	c := New("pipe", "onibi", "acct", "secret")
	c.Plaintext = true
	c.Dial = func(context.Context, string, string) (net.Conn, error) { return clientConn, nil }
	done := make(chan error, 1)
	go func() {
		r := bufio.NewReader(serverConn)
		if got := readTestLine(t, r); got != "CAP REQ :sasl" {
			t.Fatalf("cap = %q", got)
		}
		if got := readTestLine(t, r); got != "NICK onibi" {
			t.Fatalf("nick = %q", got)
		}
		if got := readTestLine(t, r); got != "USER acct 0 * :Onibi" {
			t.Fatalf("user = %q", got)
		}
		writeTestLine(t, serverConn, ":irc.test CAP onibi ACK :sasl")
		if got := readTestLine(t, r); got != "AUTHENTICATE PLAIN" {
			t.Fatalf("authenticate = %q", got)
		}
		writeTestLine(t, serverConn, "AUTHENTICATE +")
		got := strings.TrimPrefix(readTestLine(t, r), "AUTHENTICATE ")
		decoded, err := base64.StdEncoding.DecodeString(got)
		if err != nil {
			t.Fatal(err)
		}
		if string(decoded) != "\x00acct\x00secret" {
			t.Fatalf("sasl payload = %q", decoded)
		}
		writeTestLine(t, serverConn, ":irc.test 903 onibi :SASL authentication successful")
		if got := readTestLine(t, r); got != "CAP END" {
			t.Fatalf("cap end = %q", got)
		}
		writeTestLine(t, serverConn, ":irc.test 001 onibi :Welcome")
		done <- nil
	}()
	if err := c.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not finish")
	}
}

func TestSendPrivmsgChunksAndPaces(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	c := New("pipe", "onibi", "acct", "")
	c.conn = clientConn
	c.reader = bufio.NewReader(clientConn)
	var sleeps []time.Duration
	c.Sleep = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}
	c.SendPace = 10 * time.Millisecond
	done := make(chan []string, 1)
	go func() {
		r := bufio.NewReader(serverConn)
		done <- []string{readTestLine(t, r), readTestLine(t, r)}
	}()
	if err := c.SendPrivmsg(t.Context(), "owner", strings.Repeat("x", MessageChunkLimit+3)); err != nil {
		t.Fatal(err)
	}
	lines := <-done
	if !strings.HasPrefix(lines[0], "PRIVMSG owner :") || len(strings.TrimPrefix(lines[0], "PRIVMSG owner :")) != MessageChunkLimit {
		t.Fatalf("line0 = %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], "xxx") || len(sleeps) != 1 {
		t.Fatalf("line1=%q sleeps=%v", lines[1], sleeps)
	}
}

func TestParseOnibiCommand(t *testing.T) {
	action, id, ok := ParseOnibiCommand("!onibi deny abc")
	if !ok || action != "deny" || id != "abc" {
		t.Fatalf("action=%q id=%q ok=%t", action, id, ok)
	}
	if _, _, ok := ParseOnibiCommand("approve abc"); ok {
		t.Fatal("unexpected command parse")
	}
}

func readTestLine(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(line, "\r\n")
}

func writeTestLine(t *testing.T, c net.Conn, line string) {
	t.Helper()
	if _, err := c.Write([]byte(line + "\r\n")); err != nil {
		t.Fatal(err)
	}
}
