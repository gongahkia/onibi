package gotify

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestSendUsesRESTAndAppToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/message" || r.URL.Query().Get("token") != "app" || r.Header.Get("X-Gotify-Key") != "app" {
			t.Fatalf("url=%s key=%q", r.URL.String(), r.Header.Get("X-Gotify-Key"))
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

func TestValidateChecksClientToken(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		if r.Method != http.MethodGet || r.URL.Path != "/message" || r.URL.Query().Get("token") != "client" || r.Header.Get("X-Gotify-Key") != "client" {
			t.Fatalf("request = %s %s key=%q", r.Method, r.URL.String(), r.Header.Get("X-Gotify-Key"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	if err := New(srv.URL, "app", "client").Validate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !hit {
		t.Fatal("validation did not call server")
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

func TestParityAxes(t *testing.T) {
	t.Run("extras", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var msg Message
			if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
				t.Fatal(err)
			}
			notification, ok := msg.Extras["client::notification"].(map[string]any)
			if !ok {
				t.Fatalf("extras = %#v", msg.Extras)
			}
			click := notification["click"].(map[string]any)
			if click["url"] != "https://onibi.example/gotify/approval/a1" {
				t.Fatalf("click = %#v", click)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		msg := Message{Title: "Approval", Message: "Open: https://onibi.example/gotify/approval/a1", Priority: 8, Extras: ApprovalExtras("https://onibi.example/gotify/approval/a1")}
		if err := New(srv.URL, "app", "client").Send(t.Context(), msg); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("tail reconnect", func(t *testing.T) {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/stream" || r.URL.Query().Get("token") != "client" {
				t.Fatalf("url = %s", r.URL.String())
			}
			calls++
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(fmt.Sprintf(`{"id":%d,"message":"m%d"}`, calls, calls)))
		}))
		defer srv.Close()
		var got []string
		errStop := errors.New("stop")
		err := New(srv.URL, "app", "client").Tail(t.Context(), TailOptions{RetryMin: time.Millisecond, RetryMax: time.Millisecond, MaxReconnects: 2}, func(msg StreamMessage) error {
			got = append(got, msg.Message)
			if len(got) == 2 {
				return errStop
			}
			return nil
		})
		if !errors.Is(err, errStop) {
			t.Fatalf("err = %v", err)
		}
		if strings.Join(got, ",") != "m1,m2" {
			t.Fatalf("got = %#v", got)
		}
	})
}
