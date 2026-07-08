package ntfy

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestValidateTopicSecret(t *testing.T) {
	for _, bad := range []string{"short", "onibi-public-topic-1234567890", "aaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
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

func TestActions(t *testing.T) {
	topic := "A1b2C3d4E5f6G7h8I9j0"
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Actions")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := New(srv.URL, topic, "")
	err := c.Publish(t.Context(), Message{
		Title: "Approval",
		Body:  "approve a1",
		Actions: []Action{
			{Type: "http", Label: "Approve", URL: "https://onibi.example/ntfy/approval/a1/approve?sig=s1", Method: http.MethodPost, Clear: true},
			{Type: "http", Label: "Deny", URL: "https://onibi.example/ntfy/approval/a1/deny?sig=s2", Method: http.MethodPost, Clear: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "http, Approve, https://onibi.example/ntfy/approval/a1/approve?sig=s1, method=POST, clear=true; http, Deny, https://onibi.example/ntfy/approval/a1/deny?sig=s2, method=POST, clear=true"
	if got != want {
		t.Fatalf("actions = %q", got)
	}
}

func TestStreamJSONChunking(t *testing.T) {
	topic := "A1b2C3d4E5f6G7h8I9j0"
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+topic+"/json" || r.URL.Query().Get("since") != "all" {
			t.Fatalf("url = %s", r.URL.String())
		}
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, `{"id":"m1","event":"message",`)
		flusher.Flush()
		fmt.Fprint(w, `"topic":"`+topic+`","message":"hi"}`)
	}))
	defer srv.Close()
	srv.Listener = ln
	srv.Start()
	c := New(srv.URL, topic, "")
	var got StreamMessage
	err = c.StreamJSON(t.Context(), "all", func(msg StreamMessage) error {
		got = msg
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "EOF") {
		t.Fatalf("err = %v", err)
	}
	if got.ID != "m1" || got.Message != "hi" {
		t.Fatalf("message = %#v", got)
	}
}

func TestTailJSONReconnectsWithLastID(t *testing.T) {
	topic := "A1b2C3d4E5f6G7h8I9j0"
	calls := 0
	var sinces []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		sinces = append(sinces, r.URL.Query().Get("since"))
		if calls == 1 {
			fmt.Fprint(w, `{"id":"m1","event":"message","message":"one"}`)
			return
		}
		fmt.Fprint(w, `{"id":"m2","event":"message","message":"two"}`)
	}))
	defer srv.Close()
	c := New(srv.URL, topic, "")
	var messages []string
	err := c.TailJSON(t.Context(), TailOptions{Since: "all", RetryMin: time.Millisecond, RetryMax: time.Millisecond, MaxReconnects: 1}, func(msg StreamMessage) error {
		messages = append(messages, msg.Message)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(messages, ",") != "one,two" {
		t.Fatalf("messages = %#v", messages)
	}
	if len(sinces) != 2 || sinces[0] != "all" || sinces[1] != "m1" {
		t.Fatalf("sinces = %#v", sinces)
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

func TestSubscribeWSSince(t *testing.T) {
	topic := "A1b2C3d4E5f6G7h8I9j0"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/ws") || r.URL.Query().Get("since") != "all" {
			t.Fatalf("url = %s", r.URL.String())
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.CloseNow()
	}))
	defer srv.Close()
	conn, err := New(srv.URL, topic, "").SubscribeWSSince(t.Context(), "all")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
}
