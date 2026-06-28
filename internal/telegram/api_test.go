package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testToken = "123456:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghi"

func TestClientGetMeAndSendMessage(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			writeTG(t, w, User{ID: 7, IsBot: true, Username: "onibi_test_bot"})
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["text"] != "hello" {
				t.Fatalf("body = %#v", body)
			}
			writeTG(t, w, Message{MessageID: 9, Chat: Chat{ID: 42}, Text: "hello"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := NewClient(testToken)
	c.BaseURL = srv.URL
	u, err := c.GetMe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "onibi_test_bot" {
		t.Fatalf("user = %#v", u)
	}
	msg, err := c.SendMessage(t.Context(), 42, "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.MessageID != 9 || len(seen) != 2 {
		t.Fatalf("msg=%#v seen=%#v", msg, seen)
	}
}

func TestValidBotToken(t *testing.T) {
	if !ValidBotToken(testToken) {
		t.Fatal("expected valid token")
	}
	for _, token := range []string{"", "abc", "123:short", "123456:bad token"} {
		if ValidBotToken(token) {
			t.Fatalf("expected invalid token %q", token)
		}
	}
}

func TestClientGetUpdatesPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getUpdates") {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["offset"] != float64(99) || body["timeout"] != float64(7) {
			t.Fatalf("body = %#v", body)
		}
		updates, ok := body["allowed_updates"].([]any)
		if !ok || len(updates) != 2 || updates[0] != "message" || updates[1] != "callback_query" {
			t.Fatalf("allowed_updates = %#v", body["allowed_updates"])
		}
		writeTG(t, w, []Update{{UpdateID: 99, Message: &Message{Chat: Chat{ID: 42}, Text: "/ping"}}})
	}))
	defer srv.Close()

	c := NewClient(testToken)
	c.BaseURL = srv.URL
	got, err := c.GetUpdates(t.Context(), 99, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].UpdateID != 99 || got[0].Message.Text != "/ping" {
		t.Fatalf("updates = %#v", got)
	}
}

func TestClientSendPhotoMultipart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/sendPhoto") {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
			t.Fatalf("content-type = %q", r.Header.Get("Content-Type"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("chat_id") != "42" || r.FormValue("caption") != "screen" {
			t.Fatalf("form chat=%q caption=%q", r.FormValue("chat_id"), r.FormValue("caption"))
		}
		f, hdr, err := r.FormFile("photo")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		b, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Filename != "onibi.png" || string(b) != "png-bytes" {
			t.Fatalf("file=%q bytes=%q", hdr.Filename, string(b))
		}
		writeTG(t, w, true)
	}))
	defer srv.Close()

	c := NewClient(testToken)
	c.BaseURL = srv.URL
	if err := c.SendPhoto(t.Context(), 42, []byte("png-bytes"), "screen"); err != nil {
		t.Fatal(err)
	}
	if err := c.SendPhoto(t.Context(), 42, nil, "screen"); err == nil || !strings.Contains(err.Error(), "photo bytes required") {
		t.Fatalf("expected photo bytes error, got %v", err)
	}
}

func TestClientAPIAndValidationErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(map[string]any{"ok": false, "description": "Bad Request: chat not found", "error_code": 400}); err != nil {
			t.Fatal(err)
		}
	}))
	defer srv.Close()

	c := NewClient(testToken)
	c.BaseURL = srv.URL
	if _, err := c.SendMessage(t.Context(), 42, "hello", nil); err == nil || !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("expected api error, got %v", err)
	}
	c = NewClient("bad")
	c.BaseURL = srv.URL
	if _, err := c.GetMe(t.Context()); err == nil || !strings.Contains(err.Error(), "invalid Telegram bot token") {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestClientRetriesRetryAfter429(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":          false,
				"error_code":  429,
				"description": "Too Many Requests",
				"parameters":  map[string]any{"retry_after": 1},
			})
			return
		}
		writeTG(t, w, Message{MessageID: 10, Chat: Chat{ID: 42}, Text: "hello"})
	}))
	defer srv.Close()
	var slept time.Duration
	c := NewClient(testToken)
	c.BaseURL = srv.URL
	c.RetrySleep = func(_ context.Context, d time.Duration) error {
		slept = d
		return nil
	}
	msg, err := c.SendMessage(t.Context(), 42, "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || slept != time.Second || msg.MessageID != 10 {
		t.Fatalf("calls=%d slept=%s msg=%#v", calls, slept, msg)
	}
}

func TestChunkTextSplitsOnNewline(t *testing.T) {
	got := ChunkText("one\ntwo\nthree", 8)
	want := []string{"one\ntwo", "three"}
	if len(got) != len(want) {
		t.Fatalf("chunks = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chunks = %#v", got)
		}
	}
}

func TestChunkTextNearTelegramLimit(t *testing.T) {
	text := strings.Repeat("x", 4095) + "\n" + strings.Repeat("y", 4095)
	chunks := ChunkText(text, 4096)
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	for _, chunk := range chunks {
		if len(chunk) > 4096 {
			t.Fatalf("chunk too long: %d", len(chunk))
		}
	}
}

func writeTG(t *testing.T, w http.ResponseWriter, result any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result}); err != nil {
		t.Fatal(err)
	}
}
