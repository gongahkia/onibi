//go:build !onibi_remote

package daemon

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/irc"
)

func TestIRCMessageApprovesFromOwnerDM(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db, IRC: IRCOptions{Nick: "onibi", OwnerNick: "owner"}})
	id, _, err := d.Queue.Request(t.Context(), "s1", "claude", "Bash", `{"command":"pwd"}`)
	if err != nil {
		t.Fatal(err)
	}
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	c := irc.New("pipe", "onibi", "onibi", "")
	c.SendPace = -1
	c.SetConnForTest(clientConn)
	done := make(chan string, 1)
	go func() {
		r := bufio.NewReader(serverConn)
		line, _ := r.ReadString('\n')
		done <- strings.TrimRight(line, "\r\n")
	}()
	err = d.handleIRCMessage(t.Context(), c, irc.Message{
		Command:  "PRIVMSG",
		Prefix:   "owner!u@h",
		Params:   []string{"onibi"},
		Trailing: "!onibi approve " + id,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.Queue.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != approval.StateApproved {
		t.Fatalf("state = %s", got.State)
	}
	select {
	case line := <-done:
		if !strings.Contains(line, "Approval "+id+" approve.") {
			t.Fatalf("line = %q", line)
		}
	case <-time.After(time.Second):
		t.Fatal("missing IRC reply")
	}
}

func TestIRCTailChunksAtLimit(t *testing.T) {
	d := New(Options{IRC: IRCOptions{Nick: "onibi", OwnerNick: "owner"}})
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	c := irc.New("pipe", "onibi", "onibi", "")
	c.SendPace = -1
	c.SetConnForTest(clientConn)
	done := make(chan []string, 1)
	go func() {
		r := bufio.NewReader(serverConn)
		var lines []string
		for i := 0; i < 2; i++ {
			line, _ := r.ReadString('\n')
			lines = append(lines, strings.TrimRight(line, "\r\n"))
		}
		done <- lines
	}()
	d.postIRCTail(t.Context(), c, "owner", "s1", strings.Repeat("x", irc.MessageChunkLimit+5))
	select {
	case lines := <-done:
		if len(strings.TrimPrefix(lines[0], "PRIVMSG owner :")) != irc.MessageChunkLimit || !strings.HasSuffix(lines[1], "xxxxx") {
			t.Fatalf("lines = %#v", lines)
		}
	case <-time.After(time.Second):
		t.Fatal("missing tail chunks")
	}
}
