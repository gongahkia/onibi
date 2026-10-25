package dap

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func wireSend(t *testing.T, conn net.Conn, m Message) {
	t.Helper()
	data, _ := json.Marshal(m)
	conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if _, err := fmt.Fprintf(conn, "Content-Length: %d\r\n\r\n%s", len(data), data); err != nil {
		t.Error(err)
	}
}
func wireRead(t *testing.T, r *bufio.Reader) Message {
	t.Helper()
	data, err := readMessage(r)
	if err != nil {
		t.Fatal(err)
	}
	var m Message
	if err = json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
func testClient(t *testing.T) (*Client, net.Conn, *bufio.Reader) {
	t.Helper()
	left, right := net.Pipe()
	c := connect(context.Background(), left, left, func() {})
	right.SetDeadline(time.Now().Add(5 * time.Second))
	t.Cleanup(func() { c.Close(); right.Close() })
	return c, right, bufio.NewReader(right)
}
func TestConcurrentRepliesReverseRequestsAndTimeout(t *testing.T) {
	c, conn, reader := testClient(t)
	type result struct {
		value string
		err   error
	}
	one, two := make(chan result, 1), make(chan result, 1)
	go func() { v, e := c.Request(context.Background(), "one", nil); one <- result{string(v), e} }()
	first := wireRead(t, reader)
	go func() { v, e := c.Request(context.Background(), "two", nil); two <- result{string(v), e} }()
	second := wireRead(t, reader)
	wireSend(t, conn, Message{Type: "response", RequestSeq: second.Seq, Success: true, Body: json.RawMessage(`{"value":2}`)})
	wireSend(t, conn, Message{Type: "response", RequestSeq: first.Seq, Success: true, Body: json.RawMessage(`{"value":1}`)})
	if r := <-one; r.err != nil || !strings.Contains(r.value, ":1") {
		t.Fatal(r)
	}
	if r := <-two; r.err != nil || !strings.Contains(r.value, ":2") {
		t.Fatal(r)
	}
	wireSend(t, conn, Message{Seq: 42, Type: "request", Command: "runInTerminal", Arguments: json.RawMessage(`{"args":["should-not-run"]}`)})
	rejected := wireRead(t, reader)
	if rejected.Type != "response" || rejected.RequestSeq != 42 || rejected.Success {
		t.Fatal(rejected)
	}
	c.SupportsCancel.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	go func() { _, e := c.Request(ctx, "slow", nil); one <- result{err: e} }()
	slow := wireRead(t, reader)
	cancellation := wireRead(t, reader)
	if cancellation.Command != "cancel" || !strings.Contains(string(cancellation.Arguments), fmt.Sprint(slow.Seq)) {
		t.Fatal(cancellation)
	}
	if r := <-one; r.err != context.DeadlineExceeded {
		t.Fatal(r.err)
	}
	wireSend(t, conn, Message{Type: "response", RequestSeq: slow.Seq, Success: true})
	c.mu.Lock()
	pending := len(c.pending)
	c.mu.Unlock()
	if pending != 0 {
		t.Fatal("timed out request retained")
	}
}
func TestOutputBoundAndFraming(t *testing.T) {
	c, conn, _ := testClient(t)
	for i := 0; i < 8; i++ {
		body, _ := json.Marshal(map[string]string{"output": strings.Repeat("你好", 100000)})
		wireSend(t, conn, Message{Type: "event", Event: "output", Body: body})
	}
	// A following event proves that all previous output has been processed.
	wireSend(t, conn, Message{Type: "event", Event: "initialized"})
	select {
	case <-c.Events:
	case <-time.After(time.Second):
		t.Fatal("output blocked protocol events")
	}
	value := c.Output()
	if len(value) > maxOutput || len(value) < maxOutput-6 || !utf8.ValidString(value) {
		t.Fatal("output ring bound/UTF8", len(value))
	}
	for _, input := range []string{"Content-Length: -1\r\n\r\n", "Content-Length: 9000000\r\n\r\n", "Content-Length: 1\r\nContent-Length: 1\r\n\r\nx"} {
		if _, e := readMessage(bufio.NewReader(strings.NewReader(input))); e == nil {
			t.Fatal("invalid framing accepted")
		}
	}
}
func TestCloseUnblocksStalledWriteAndPendingRequest(t *testing.T) {
	c, _, _ := testClient(t)
	result := make(chan error, 1)
	go func() { _, e := c.Request(context.Background(), "blocked", strings.Repeat("x", 65536)); result <- e }()
	time.Sleep(20 * time.Millisecond)
	c.Close()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("missing close error")
		}
	case <-time.After(time.Second):
		t.Fatal("close blocked on adapter write")
	}
}
