package irc

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
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
		if err := expectTestLine(r, "CAP REQ :sasl"); err != nil {
			done <- err
			return
		}
		if err := expectTestLine(r, "NICK onibi"); err != nil {
			done <- err
			return
		}
		if err := expectTestLine(r, "USER acct 0 * :Onibi"); err != nil {
			done <- err
			return
		}
		if err := writeTestLine(serverConn, ":irc.test CAP onibi ACK :sasl"); err != nil {
			done <- err
			return
		}
		if err := expectTestLine(r, "AUTHENTICATE PLAIN"); err != nil {
			done <- err
			return
		}
		if err := writeTestLine(serverConn, "AUTHENTICATE +"); err != nil {
			done <- err
			return
		}
		line, err := readTestLine(r)
		if err != nil {
			done <- err
			return
		}
		got := strings.TrimPrefix(line, "AUTHENTICATE ")
		decoded, err := base64.StdEncoding.DecodeString(got)
		if err != nil {
			done <- err
			return
		}
		if string(decoded) != "\x00acct\x00secret" {
			done <- fmt.Errorf("sasl payload = %q", decoded)
			return
		}
		if err := writeTestLine(serverConn, ":irc.test 903 onibi :SASL authentication successful"); err != nil {
			done <- err
			return
		}
		if err := expectTestLine(r, "CAP END"); err != nil {
			done <- err
			return
		}
		if err := writeTestLine(serverConn, ":irc.test 001 onibi :Welcome"); err != nil {
			done <- err
			return
		}
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
	done := make(chan lineResult, 1)
	go func() {
		r := bufio.NewReader(serverConn)
		first, err := readTestLine(r)
		if err != nil {
			done <- lineResult{err: err}
			return
		}
		second, err := readTestLine(r)
		if err != nil {
			done <- lineResult{err: err}
			return
		}
		done <- lineResult{lines: []string{first, second}}
	}()
	if err := c.SendPrivmsg(t.Context(), "owner", strings.Repeat("x", MessageChunkLimit+3)); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	lines := result.lines
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

type lineResult struct {
	lines []string
	err   error
}

func readTestLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func expectTestLine(r *bufio.Reader, want string) error {
	got, err := readTestLine(r)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("line = %q, want %q", got, want)
	}
	return nil
}

func writeTestLine(c net.Conn, line string) error {
	_, err := c.Write([]byte(line + "\r\n"))
	return err
}
