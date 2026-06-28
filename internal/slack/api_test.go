package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestOpenSocketAndPostMessage(t *testing.T) {
	var posted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
			t.Fatalf("auth = %q", got)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/apps.connections.open"):
			_ = json.NewEncoder(w).Encode(SocketOpenResponse{OK: true, URL: "wss://socket.example"})
		case strings.HasSuffix(r.URL.Path, "/chat.postMessage"):
			posted = true
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["channel"] != "C1" || body["text"] != "hello" {
				t.Fatalf("body = %#v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	c := New("xapp-token", "xoxb-token")
	c.BaseURL = srv.URL
	url, err := c.OpenSocket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if url != "wss://socket.example" {
		t.Fatalf("url = %q", url)
	}
	if err := c.PostMessage(t.Context(), "C1", "hello"); err != nil {
		t.Fatal(err)
	}
	if !posted {
		t.Fatal("message not posted")
	}
}

func TestPostMessageChunksAndRetriesRateLimit(t *testing.T) {
	var texts []string
	var slept time.Duration
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat.postMessage") {
			writeSlackOK(t, w, map[string]any{"url": "wss://socket.example"})
			return
		}
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0.25")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"error":"ratelimited"}`))
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		texts = append(texts, body["text"].(string))
		writeSlackOK(t, w, nil)
	}))
	defer srv.Close()
	c := New("xapp-token", "xoxb-token")
	c.BaseURL = srv.URL
	c.PostPace = -1
	c.Sleep = func(_ context.Context, d time.Duration) error {
		slept += d
		return nil
	}
	if err := c.PostMessage(t.Context(), "C1", strings.Repeat("x", MessageChunkLimit+10)); err != nil {
		t.Fatal(err)
	}
	if len(texts) != 2 || len(texts[0]) != MessageChunkLimit || len(texts[1]) != 10 {
		t.Fatalf("texts = %d %v", len(texts), chunkLens(texts))
	}
	if slept != 250*time.Millisecond {
		t.Fatalf("slept = %s", slept)
	}
}

func TestSocketReadAckAndInteractionParse(t *testing.T) {
	ackCh := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.CloseNow()
		payload := `{"type":"block_actions","user":{"id":"U1"},"channel":{"id":"D1"},"actions":[{"action_id":"approve","value":"a1"}]}`
		env := `{"envelope_id":"e1","type":"interactive","payload":` + payload + `}`
		if err := conn.Write(r.Context(), websocket.MessageText, []byte(env)); err != nil {
			t.Fatal(err)
		}
		_, p, err := conn.Read(r.Context())
		if err != nil {
			t.Fatal(err)
		}
		var ack map[string]any
		if err := json.Unmarshal(p, &ack); err != nil {
			t.Fatal(err)
		}
		ackCh <- ack
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, err := Dial(ctx, wsURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	env, err := ReadEnvelope(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	action, err := ParseInteraction(env)
	if err != nil {
		t.Fatal(err)
	}
	if action.User.ID != "U1" || action.Actions[0].ActionID != "approve" {
		t.Fatalf("action = %#v", action)
	}
	if err := Ack(ctx, conn, env.EnvelopeID, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case ack := <-ackCh:
		if ack["envelope_id"] != "e1" {
			t.Fatalf("ack = %#v", ack)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestDisconnectShouldReconnect(t *testing.T) {
	env := Envelope{Type: "disconnect", Payload: json.RawMessage(`{"reason":"refresh_requested"}`)}
	got, err := ParseDisconnect(env)
	if err != nil {
		t.Fatal(err)
	}
	if got.Reason != "refresh_requested" || !ShouldReconnect(env) {
		t.Fatalf("disconnect = %#v reconnect=%v", got, ShouldReconnect(env))
	}
}

func writeSlackOK(t *testing.T, w http.ResponseWriter, extra map[string]any) {
	t.Helper()
	body := map[string]any{"ok": true}
	for k, v := range extra {
		body[k] = v
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatal(err)
	}
}

func chunkLens(chunks []string) []int {
	out := make([]int, 0, len(chunks))
	for _, chunk := range chunks {
		out = append(out, len(chunk))
	}
	return out
}

func TestAllowlistDMAndChannel(t *testing.T) {
	a := Allowlist{Channels: map[string]bool{"C1": true}, DMUsers: map[string]bool{"U1": true}}
	if !a.Allows("C1", "U2", "channel") || !a.Allows("D1", "U1", "im") {
		t.Fatal("expected allowed")
	}
	if a.Allows("C2", "U2", "channel") {
		t.Fatal("unexpected allowed")
	}
}
