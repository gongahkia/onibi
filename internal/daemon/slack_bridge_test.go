package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/slack"
)

func TestSlackInteractiveAckPayloadDecidesApproval(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	id, _, err := d.Queue.Request(t.Context(), "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	ackCh := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.CloseNow()
		payload := `{"type":"block_actions","actions":[{"action_id":"approve","value":"` + id + `"}]}`
		env := `{"envelope_id":"e1","type":"interactive","accepts_response_payload":true,"payload":` + payload + `}`
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
	conn, err := slack.Dial(t.Context(), "ws"+strings.TrimPrefix(srv.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	err = d.runSlackSocket(t.Context(), slack.New("", ""), conn, slack.Allowlist{})
	if err == nil {
		t.Fatal("expected socket close error")
	}
	select {
	case ack := <-ackCh:
		payload := ack["payload"].(map[string]any)
		if ack["envelope_id"] != "e1" || !strings.Contains(payload["text"].(string), "received") {
			t.Fatalf("ack = %#v", ack)
		}
	case <-time.After(time.Second):
		t.Fatal("missing ack")
	}
	got, err := d.Queue.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != approval.StateApproved {
		t.Fatalf("state = %s", got.State)
	}
}

func TestSlackDisconnectAcksAndReturnsForReconnect(t *testing.T) {
	ackCh := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.CloseNow()
		env := `{"envelope_id":"e2","type":"disconnect","payload":{"reason":"refresh_requested"}}`
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
	conn, err := slack.Dial(t.Context(), "ws"+strings.TrimPrefix(srv.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	if err := New(Options{}).runSlackSocket(t.Context(), slack.New("", ""), conn, slack.Allowlist{}); err != nil {
		t.Fatal(err)
	}
	select {
	case ack := <-ackCh:
		if ack["envelope_id"] != "e2" {
			t.Fatalf("ack = %#v", ack)
		}
	case <-time.After(time.Second):
		t.Fatal("missing ack")
	}
}
