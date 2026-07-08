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
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/tmux"
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

func TestSlackApprovalBlocksIncludeEditButton(t *testing.T) {
	blocks := slackApprovalBlocks(&approval.Approval{ID: "a1", SessionID: "s1", Agent: "claude", Tool: "Bash"}, ProviderOutputPolicy{})
	actions := blocks[1].(map[string]any)["elements"].([]any)
	var ids []string
	for _, raw := range actions {
		ids = append(ids, raw.(map[string]any)["action_id"].(string))
	}
	if strings.Join(ids, ",") != "approve,deny,edit" {
		t.Fatalf("action ids = %v", ids)
	}
}

func TestSlackEditButtonOpensModalAndAudits(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	id, _, err := d.Queue.Request(t.Context(), "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	viewCh := make(chan map[string]any, 1)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/views.open") {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		viewCh <- body
		writeSlackAPIOK(t, w, map[string]any{"view": map[string]any{"id": "V1"}})
	}))
	defer api.Close()
	ackCh := make(chan map[string]any, 1)
	ws := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.CloseNow()
		value := slackApprovalValue(&approval.Approval{ID: id, SessionID: "s1", Agent: "claude", Tool: "Bash"}, approval.VerdictEdit)
		payload := `{"type":"block_actions","trigger_id":"TR1","user":{"id":"U1"},"channel":{"id":"C1"},"message":{"ts":"111.222"},"actions":[{"action_id":"edit","value":` + strconv.Quote(value) + `}]}`
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
	case body := <-viewCh:
		if body["trigger_id"] != "TR1" {
			t.Fatalf("view body = %#v", body)
		}
		view := body["view"].(map[string]any)
		if view["callback_id"] != slackEditCallback || view["private_metadata"] != id {
			t.Fatalf("view = %#v", view)
		}
	case <-time.After(time.Second):
		t.Fatal("missing view open")
	}
	select {
	case ack := <-ackCh:
		payload := ack["payload"].(map[string]any)
		if !strings.Contains(payload["text"].(string), "Edit modal opened") {
			t.Fatalf("ack = %#v", ack)
		}
	case <-time.After(time.Second):
		t.Fatal("missing ack")
	}
	assertAuditActions(t, db, "provider.slack.button", "provider.slack.edit_modal")
}

func TestSlackViewSubmissionEditsApprovalAndAudits(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	id, _, err := d.Queue.Request(t.Context(), "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	d.rememberSlackApproval(id, slackApprovalRef{Channel: "C1", TS: "111.222"})
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
		payload, err := json.Marshal(map[string]any{
			"type": "view_submission",
			"user": map[string]any{"id": "U1"},
			"view": map[string]any{
				"callback_id":      slackEditCallback,
				"private_metadata": id,
				"state": map[string]any{"values": map[string]any{
					slackEditInputBlock: map[string]any{
						slackEditInputAction: map[string]any{"type": "plain_text_input", "value": `{"command":"pwd"}`},
					},
				}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		env := `{"envelope_id":"e1","type":"interactive","accepts_response_payload":true,"payload":` + string(payload) + `}`
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
	got, err := d.Queue.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != approval.StateEdited || got.EditedJSON != `{"command":"pwd"}` {
		t.Fatalf("approval = %#v", got)
	}
	select {
	case body := <-updateCh:
		if body["channel"] != "C1" || body["ts"] != "111.222" || !strings.Contains(body["text"].(string), "edited") {
			t.Fatalf("update = %#v", body)
		}
	case <-time.After(time.Second):
		t.Fatal("missing update")
	}
	select {
	case ack := <-ackCh:
		if _, ok := ack["payload"]; ok {
			t.Fatalf("expected modal close ack, got %#v", ack)
		}
	case <-time.After(time.Second):
		t.Fatal("missing ack")
	}
	assertAuditActions(t, db, "provider.slack.edit_submit", "approval.decided")
}

func TestSlackTextInAndTailAudited(t *testing.T) {
	db := openDaemonTestDB(t)
	out := "$ printf big\n" + strings.Repeat("x", slack.MessageChunkLimit+5)
	r := &tmuxRunner{results: [][]byte{
		nil,
		nil,
		[]byte(out),
		[]byte(out),
		[]byte(out),
	}}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(r) }
	t.Cleanup(func() { newTmuxController = old })
	d := New(Options{DB: db})
	s := NewSession("s1", "shell", "shell", nil, 0)
	s.Transport = "tmux"
	s.TmuxTarget = "onibi-s1"
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	var posts []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat.postMessage") {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		posts = append(posts, body["text"].(string))
		writeSlackAPIOK(t, w, nil)
	}))
	defer api.Close()
	ackCh := make(chan map[string]any, 1)
	ws := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.CloseNow()
		payload := `{"event":{"type":"message","channel":"C1","user":"U1","text":"printf big","channel_type":"channel"}}`
		env := `{"envelope_id":"e1","type":"events_api","payload":` + payload + `}`
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
	c.PostPace = -1
	err = d.runSlackSocket(t.Context(), c, conn, slack.Allowlist{})
	if err == nil {
		t.Fatal("expected socket close error")
	}
	if len(posts) < 2 {
		t.Fatalf("posts = %d", len(posts))
	}
	select {
	case ack := <-ackCh:
		if ack["envelope_id"] != "e1" {
			t.Fatalf("ack = %#v", ack)
		}
	case <-time.After(time.Second):
		t.Fatal("missing ack")
	}
	rows, err := db.AuditAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	textIn, tailChunks := 0, 0
	for _, row := range rows {
		switch row.Action {
		case "provider.slack.text_in":
			textIn++
			if row.SessionID != "s1" || row.PayloadHash == "" || strings.Contains(row.Detail, "printf big") {
				t.Fatalf("text audit = %#v", row)
			}
		case "provider.slack.tail_chunk":
			tailChunks++
			if row.SessionID != "s1" || row.PayloadHash == "" || !strings.Contains(row.Detail, "channel=C1") {
				t.Fatalf("tail audit = %#v", row)
			}
		}
	}
	if textIn != 1 || tailChunks != len(posts) {
		t.Fatalf("textIn=%d tailChunks=%d posts=%d audit=%#v", textIn, tailChunks, len(posts), rows)
	}
}

func TestSlackReconnectDelayBounds(t *testing.T) {
	for _, attempt := range []int{-1, 0, 1, 4, 99} {
		got := slackReconnectDelay(attempt)
		normalized := attempt
		if normalized < 0 {
			normalized = 0
		}
		if normalized > 5 {
			normalized = 5
		}
		base := time.Second << normalized
		if base > slackReconnectMaxWait {
			base = slackReconnectMaxWait
		}
		if got < base || got > base+base/2 {
			t.Fatalf("attempt %d delay %s outside [%s,%s]", attempt, got, base, base+base/2)
		}
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

func assertAuditActions(t *testing.T, db *store.DB, want ...string) {
	t.Helper()
	rows, err := db.AuditAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.Action] = true
	}
	for _, action := range want {
		if !seen[action] {
			t.Fatalf("missing audit action %s in %#v", action, rows)
		}
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
