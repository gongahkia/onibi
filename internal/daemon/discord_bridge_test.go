//go:build !onibi_remote

package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/discord"
	"github.com/gongahkia/onibi/internal/tmux"
)

func TestDiscordApprovalButtonDecidesAndAudits(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	id, _, err := d.Queue.Request(t.Context(), "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	callbackCh := make(chan map[string]any, 1)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/interactions/i1/tok/callback") {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		callbackCh <- body
		w.WriteHeader(http.StatusNoContent)
	}))
	defer api.Close()
	ws := discordFrameServer(t, discord.GatewayFrame{Op: discord.OpDispatch, T: "INTERACTION_CREATE", D: mustDiscordJSON(t, map[string]any{
		"id": "i1", "token": "tok", "type": 3, "channel_id": "C1",
		"user": map[string]any{"id": "U1"},
		"data": map[string]any{"custom_id": discordApprovalPrefix + "approve:" + id, "component_type": 2},
	})})
	defer ws.Close()
	conn, err := discord.DialGateway(t.Context(), "ws"+strings.TrimPrefix(ws.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	c := discord.New("bot-token")
	c.BaseURL = api.URL
	err = d.runDiscordSocket(t.Context(), c, conn, map[string]bool{}, &discord.GatewayState{})
	if err == nil {
		t.Fatal("expected socket close error")
	}
	got, err := d.Queue.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != approval.StateApproved {
		t.Fatalf("state = %s", got.State)
	}
	select {
	case body := <-callbackCh:
		if body["type"].(float64) != 4 {
			t.Fatalf("callback = %#v", body)
		}
	default:
		t.Fatal("missing callback")
	}
	assertAuditActions(t, db, "provider.discord.button", "approval.decided")
}

func TestDiscordEditModalSubmitEditsApproval(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	id, _, err := d.Queue.Request(t.Context(), "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	callbacks := make(chan map[string]any, 2)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/callback") {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		callbacks <- body
		w.WriteHeader(http.StatusNoContent)
	}))
	defer api.Close()
	ws := discordFrameServer(t,
		discord.GatewayFrame{Op: discord.OpDispatch, T: "INTERACTION_CREATE", D: mustDiscordJSON(t, map[string]any{
			"id": "i1", "token": "tok", "type": 3, "channel_id": "C1",
			"user": map[string]any{"id": "U1"},
			"data": map[string]any{"custom_id": discordApprovalPrefix + "edit:" + id, "component_type": 2},
		})},
		discord.GatewayFrame{Op: discord.OpDispatch, T: "INTERACTION_CREATE", D: mustDiscordJSON(t, map[string]any{
			"id": "i2", "token": "tok2", "type": 5,
			"user": map[string]any{"id": "U1"},
			"data": map[string]any{
				"custom_id": discordEditModalPrefix + id,
				"components": []any{map[string]any{"type": 1, "components": []any{
					map[string]any{"type": 4, "custom_id": discordEditInputID, "value": `{"command":"pwd"}`},
				}}},
			},
		})},
	)
	defer ws.Close()
	conn, err := discord.DialGateway(t.Context(), "ws"+strings.TrimPrefix(ws.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	c := discord.New("bot-token")
	c.BaseURL = api.URL
	err = d.runDiscordSocket(t.Context(), c, conn, map[string]bool{}, &discord.GatewayState{})
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
	first := <-callbacks
	second := <-callbacks
	if first["type"].(float64) != 9 || second["type"].(float64) != 4 {
		t.Fatalf("callbacks = %#v %#v", first, second)
	}
	assertAuditActions(t, db, "provider.discord.edit_modal", "provider.discord.edit_submit", "approval.decided")
}

func TestDiscordTextInTailUsesThreadAndAudits(t *testing.T) {
	db := openDaemonTestDB(t)
	out := "$ printf big\n" + strings.Repeat("x", discord.MessageChunkLimit+5)
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
	var tailPosts []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/channels/C1/messages"):
			_ = json.NewEncoder(w).Encode(discord.Message{ID: "seed", ChannelID: "C1"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/channels/C1/messages/seed/threads"):
			_ = json.NewEncoder(w).Encode(discord.Channel{ID: "T1", GuildID: "G1", Name: "onibi-s1"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/channels/T1/messages"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			tailPosts = append(tailPosts, body["content"].(string))
			_ = json.NewEncoder(w).Encode(discord.Message{ID: "m-tail", ChannelID: "T1"})
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer api.Close()
	ws := discordFrameServer(t, discord.GatewayFrame{Op: discord.OpDispatch, T: "MESSAGE_CREATE", D: mustDiscordJSON(t, discord.MessageCreate{
		ChannelID: "C1",
		Content:   "printf big",
	})})
	defer ws.Close()
	conn, err := discord.DialGateway(t.Context(), "ws"+strings.TrimPrefix(ws.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	c := discord.New("bot-token")
	c.BaseURL = api.URL
	err = d.runDiscordSocket(t.Context(), c, conn, map[string]bool{}, &discord.GatewayState{})
	if err == nil {
		t.Fatal("expected socket close error")
	}
	if len(tailPosts) < 2 {
		t.Fatalf("tail posts = %d", len(tailPosts))
	}
	assertAuditActions(t, db, "provider.discord.text_in", "provider.discord.thread", "provider.discord.tail_chunk")
}

func mustDiscordJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func discordFrameServer(t *testing.T, frames ...discord.GatewayFrame) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.CloseNow()
		for _, frame := range frames {
			b, err := json.Marshal(frame)
			if err != nil {
				t.Fatal(err)
			}
			if err := conn.Write(r.Context(), websocket.MessageText, b); err != nil {
				t.Fatal(err)
			}
		}
	}))
}
