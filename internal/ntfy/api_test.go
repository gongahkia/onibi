package ntfy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

func TestValidateTopicSecret(t *testing.T) {
	for _, bad := range []string{"short", "onibi-public-topic-1234567890"} {
		if err := ValidateTopicSecret(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if err := ValidateTopicSecret("A1b2C3d4E5f6G7h8I9j0"); err != nil {
		t.Fatal(err)
	}
}

func TestPublishSendsHeaders(t *testing.T) {
	topic := "A1b2C3d4E5f6G7h8I9j0"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+topic || r.Header.Get("Title") != "Approval" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Fatalf("path=%s title=%q auth=%q", r.URL.Path, r.Header.Get("Title"), r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := New(srv.URL, topic, "tok")
	if err := c.Publish(t.Context(), Message{Title: "Approval", Body: "approve a1"}); err != nil {
		t.Fatal(err)
	}
}

func TestSubscribeWS(t *testing.T) {
	topic := "A1b2C3d4E5f6G7h8I9j0"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/ws") {
			t.Fatalf("path = %s", r.URL.Path)
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.CloseNow()
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"event":"message","message":"hi"}`))
	}))
	defer srv.Close()
	c := New(srv.URL, topic, "")
	conn, err := c.SubscribeWS(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	_, p, err := conn.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(p), `"message":"hi"`) {
		t.Fatalf("payload = %s", p)
	}
}
