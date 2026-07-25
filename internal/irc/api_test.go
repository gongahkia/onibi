package irc

import (
	"bufio"
	"context"
	"encoding/base64"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/chatout"
)

func TestClientSASLHandshakeAndPrivmsg(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	lines := make(chan string, 16)
	go func() {
		r := bufio.NewReader(serverConn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			lines <- strings.TrimSpace(line)
		}
	}()
	c := NewClient(Config{Nick: "bot", Account: "account", Password: "password", Dial: func(context.Context, string, string) (net.Conn, error) { return clientConn, nil }})
	done := make(chan error, 1)
	go func() { done <- c.Connect(context.Background()) }()
	mustLine(t, lines, "CAP LS 302")
	mustLine(t, lines, "NICK bot")
	mustLine(t, lines, "USER bot 0 * :Onibi IRC cockpit")
	mustWrite(t, serverConn, ":server CAP * LS :sasl\r\n")
	mustLine(t, lines, "CAP REQ :sasl")
	mustWrite(t, serverConn, ":server CAP * ACK :sasl\r\n")
	mustLine(t, lines, "AUTHENTICATE PLAIN")
	mustWrite(t, serverConn, "AUTHENTICATE +\r\n")
	auth := mustLinePrefix(t, lines, "AUTHENTICATE ")
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "AUTHENTICATE "))
	if err != nil || string(decoded) != "account\x00account\x00password" {
		t.Fatalf("sasl = %q, %v", decoded, err)
	}
	mustWrite(t, serverConn, ":server 903 bot :SASL successful\r\n")
	mustLine(t, lines, "CAP END")
	mustWrite(t, serverConn, ":server 001 bot :welcome\r\n")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := c.SendPrivmsg(context.Background(), "owner", "hello"); err != nil {
		t.Fatal(err)
	}
	mustLine(t, lines, "PRIVMSG owner :hello")
	_ = c.Close()
}

func TestClientRejectsMissingSASL(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	lines := make(chan string, 4)
	go func() {
		r := bufio.NewReader(serverConn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			lines <- strings.TrimSpace(line)
		}
	}()
	c := NewClient(Config{Nick: "bot", Account: "account", Password: "password", Dial: func(context.Context, string, string) (net.Conn, error) { return clientConn, nil }})
	done := make(chan error, 1)
	go func() { done <- c.Connect(context.Background()) }()
	mustLine(t, lines, "CAP LS 302")
	mustLine(t, lines, "NICK bot")
	mustLine(t, lines, "USER bot 0 * :Onibi IRC cockpit")
	mustWrite(t, serverConn, ":server CAP * LS :multi-prefix\r\n")
	if err := <-done; err == nil || !strings.Contains(err.Error(), "does not offer sasl") {
		t.Fatalf("err = %v", err)
	}
}

func TestProviderRejectsBadTokenAndRoutesDecisions(t *testing.T) {
	p := NewProvider(&Client{Config: Config{Nick: "bot"}}, "owner", strings.Repeat("a", 32))
	var texts []string
	if err := p.OnInboundText(func(text string, _ chatout.Sender) { texts = append(texts, text) }); err != nil {
		t.Fatal(err)
	}
	decisions := make(chan chatout.Decision, 1)
	if err := p.OnDecision("*", func(d chatout.Decision) { decisions <- d }); err != nil {
		t.Fatal(err)
	}
	if err := p.route(Message{Prefix: "owner!u@h", Command: "PRIVMSG", Params: []string{"bot"}, Trailing: "!onibi wrong hello"}); err != nil {
		t.Fatal(err)
	}
	if len(texts) != 0 {
		t.Fatalf("unauthorized texts = %#v", texts)
	}
	if err := p.route(Message{Prefix: "owner!u@h", Command: "PRIVMSG", Params: []string{"bot"}, Trailing: "!onibi " + strings.Repeat("a", 32) + " hello world"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(texts, ""); got != "hello world" {
		t.Fatalf("texts = %q", got)
	}
	if err := p.route(Message{Prefix: "owner!u@h", Command: "PRIVMSG", Params: []string{"bot"}, Trailing: "!onibi " + strings.Repeat("a", 32) + " /approve a1"}); err != nil {
		t.Fatal(err)
	}
	select {
	case d := <-decisions:
		if d.ApprovalID != "a1" || d.Verdict != "approve" || d.Sender.ID != "owner" {
			t.Fatalf("decision = %#v", d)
		}
	case <-time.After(time.Second):
		t.Fatal("decision not routed")
	}
}

func TestChunkTextAndParseLine(t *testing.T) {
	chunks := ChunkText(strings.Repeat("界", 401), 400)
	if len(chunks) != 2 || len([]rune(chunks[0])) != 400 || len([]rune(chunks[1])) != 1 {
		t.Fatalf("chunks = %#v", chunks)
	}
	m, err := ParseLine("@account=owner :owner!u@h PRIVMSG bot :hello\r\n")
	if err != nil || m.Nick() != "owner" || m.Tags["account"] != "owner" || m.Trailing != "hello" {
		t.Fatalf("message = %#v, %v", m, err)
	}
}

func mustWrite(t *testing.T, conn net.Conn, line string) {
	t.Helper()
	if _, err := conn.Write([]byte(line)); err != nil {
		t.Fatal(err)
	}
}

func mustLine(t *testing.T, lines <-chan string, want string) {
	t.Helper()
	if got := mustLinePrefix(t, lines, want); got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
}

func mustLinePrefix(t *testing.T, lines <-chan string, prefix string) string {
	t.Helper()
	select {
	case got := <-lines:
		if !strings.HasPrefix(got, prefix) {
			t.Fatalf("line = %q, want prefix %q", got, prefix)
		}
		return got
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %q", prefix)
		return ""
	}
}
