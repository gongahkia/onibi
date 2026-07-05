//go:build !onibi_remote

package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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
		if ack["envelope_id"] != "e1" || !strings.Contains(payload["text"].(string), "approved") {
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

func TestSlackInteractiveUpdatesApprovalMessage(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	id, _, err := d.Queue.Request(t.Context(), "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	updateCh := make(chan map[string]any, 1)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat.update") {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		updateCh <- body
		writeSlackAPIOK(t, w, map[string]any{"channel": "C1", "ts": "111.222"})
	}))
	defer api.Close()
	ackCh := make(chan map[string]any, 1)
	ws := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.CloseNow()
		value := slackApprovalValue(&approval.Approval{ID: id, SessionID: "s1", Agent: "claude", Tool: "Bash"}, approval.VerdictApprove)
		payload := `{"type":"block_actions","channel":{"id":"C1"},"message":{"ts":"111.222"},"actions":[{"action_id":"approve","value":` + strconv.Quote(value) + `}]}`
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
	defer ws.Close()
	conn, err := slack.Dial(t.Context(), "ws"+strings.TrimPrefix(ws.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	c := slack.New("", "xoxb-token")
	c.BaseURL = api.URL
	err = d.runSlackSocket(t.Context(), c, conn, slack.Allowlist{})
	if err == nil {
		t.Fatal("expected socket close error")
	}
	select {
	case body := <-updateCh:
		if body["channel"] != "C1" || body["ts"] != "111.222" || !strings.Contains(body["text"].(string), "approved") {
			t.Fatalf("update = %#v", body)
		}
	case <-time.After(time.Second):
		t.Fatal("missing update")
	}
	select {
	case ack := <-ackCh:
		payload := ack["payload"].(map[string]any)
		if !strings.Contains(payload["text"].(string), "approved") {
			t.Fatalf("ack = %#v", ack)
		}
	case <-time.After(time.Second):
		t.Fatal("missing ack")
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

func writeSlackAPIOK(t *testing.T, w http.ResponseWriter, extra map[string]any) {
	t.Helper()
	body := map[string]any{"ok": true}
	for k, v := range extra {
		body[k] = v
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatal(err)
	}
}
