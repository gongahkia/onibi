package telegram

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func writeTG(t *testing.T, w http.ResponseWriter, result any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result}); err != nil {
		t.Fatal(err)
	}
}
