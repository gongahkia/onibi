package gotify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

func TestSendUsesRESTAndAppToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/message" || r.Header.Get("X-Gotify-Key") != "app" {
			t.Fatalf("path=%s key=%q", r.URL.Path, r.Header.Get("X-Gotify-Key"))
		}
		var msg Message
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Fatal(err)
		}
		if msg.Message != "approve a1" || msg.Priority != 8 {
			t.Fatalf("msg = %#v", msg)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	if err := New(srv.URL, "app", "client").Send(t.Context(), Message{Title: "Approval", Message: "approve a1", Priority: 8}); err != nil {
		t.Fatal(err)
	}
}

func TestSubscribeWSUsesClientToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stream" || r.URL.Query().Get("token") != "client" {
			t.Fatalf("url = %s", r.URL.String())
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.CloseNow()
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"message":"hi"}`))
	}))
	defer srv.Close()
	conn, err := New(srv.URL, "app", "client").SubscribeWS(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	_, p, err := conn.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(p), `"hi"`) {
		t.Fatalf("payload = %s", p)
	}
}
