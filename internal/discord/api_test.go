package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestGatewayIdentifyResumeAndReconnect(t *testing.T) {
	seen := make(chan GatewayFrame, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.CloseNow()
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"op":10,"d":{"heartbeat_interval":45000}}`))
		for i := 0; i < 2; i++ {
			_, p, err := conn.Read(r.Context())
			if err != nil {
				t.Fatal(err)
			}
			var frame GatewayFrame
			if err := json.Unmarshal(p, &frame); err != nil {
				t.Fatal(err)
			}
			seen <- frame
		}
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"op":7}`))
	}))
	defer srv.Close()
	ctx := t.Context()
	conn, err := DialGateway(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	hello, err := ReadFrame(ctx, conn)
	if err != nil || hello.Op != OpHello {
		t.Fatalf("hello=%#v err=%v", hello, err)
	}
	if err := SendIdentify(ctx, conn, "bot", 1<<9); err != nil {
		t.Fatal(err)
	}
	if err := SendResume(ctx, conn, "bot", "sess", 9); err != nil {
		t.Fatal(err)
	}
	if (<-seen).Op != OpIdentify || (<-seen).Op != OpResume {
		t.Fatal("missing identify/resume")
	}
	reconnect, err := ReadFrame(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	if !HandleReconnect(reconnect) {
		t.Fatalf("not reconnect: %#v", reconnect)
	}
}

func TestMessageContentIntentAndDMGuild(t *testing.T) {
	frame := GatewayFrame{Op: OpDispatch, T: "MESSAGE_CREATE", D: mustJSON(MessageCreate{ChannelID: "D1", Content: ""})}
	msg, ok, err := ParseMessage(frame)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if !MissingMessageContent(msg) || !IsDM(msg) {
		t.Fatalf("msg = %#v", msg)
	}
	frame = GatewayFrame{Op: OpDispatch, T: "MESSAGE_CREATE", D: mustJSON(MessageCreate{ChannelID: "C1", GuildID: "G1", Content: "ls"})}
	msg, ok, err = ParseMessage(frame)
	if err != nil || !ok || IsDM(msg) || MissingMessageContent(msg) {
		t.Fatalf("msg=%#v ok=%v err=%v", msg, ok, err)
	}
}

func TestGatewayStateReadyHeartbeatAckAndInvalidSession(t *testing.T) {
	st := &GatewayState{}
	seq := int64(42)
	st.Observe(GatewayFrame{
		Op: OpDispatch,
		T:  "READY",
		S:  &seq,
		D:  mustJSON(Ready{SessionID: "sess-1", ResumeGatewayURL: "wss://resume.example"}),
	})
	u, sessionID, gotSeq, ok := st.Resume("wss://default.example")
	if !ok || u != "wss://resume.example" || sessionID != "sess-1" || gotSeq != 42 {
		t.Fatalf("resume = %q %q %d %v", u, sessionID, gotSeq, ok)
	}
	if hb := st.HeartbeatSeq(); hb == nil || *hb != 42 {
		t.Fatalf("heartbeat seq = %v", hb)
	}
	st.MarkHeartbeatSent()
	if !st.AckOverdue(-time.Nanosecond) {
		t.Fatal("expected heartbeat overdue")
	}
	st.Observe(GatewayFrame{Op: OpHeartbeatACK})
	if st.AckOverdue(-time.Nanosecond) {
		t.Fatal("ack should clear heartbeat overdue")
	}
	st.Observe(GatewayFrame{Op: OpInvalidSession, D: mustJSON(false)})
	if _, _, _, ok := st.Resume("wss://default.example"); ok {
		t.Fatal("non-resumable invalid session should clear state")
	}
}

func TestSlashCommandFallbackResponse(t *testing.T) {
	var hit, modalHit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.Contains(r.URL.Path, "/interactions/i1/t1/callback") {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		switch body["type"].(float64) {
		case 4:
			hit = true
		case 9:
			modalHit = true
		default:
			t.Fatalf("body = %#v", body)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := New("bot-token")
	c.BaseURL = srv.URL
	ctx, cancel := contextWithTimeout(t, time.Second)
	defer cancel()
	if err := c.RespondInteraction(ctx, "i1", "t1", "message content intent missing; use /onibi <text>"); err != nil {
		t.Fatal(err)
	}
	if !hit {
		t.Fatal("interaction not posted")
	}
	if err := c.RespondInteractionModal(ctx, "i1", "t1", map[string]any{"custom_id": "m1", "title": "Edit"}); err != nil {
		t.Fatal(err)
	}
	if !modalHit {
		t.Fatal("modal interaction not posted")
	}
}

func TestRegisterOnibiCommandAndProbes(t *testing.T) {
	var registeredPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/oauth2/applications/@me"):
			_ = json.NewEncoder(w).Encode(Application{ID: "app1", Name: "onibi"})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/channels/C1"):
			_ = json.NewEncoder(w).Encode(Channel{ID: "C1", GuildID: "G1", Name: "ops"})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/applications/app1/guilds/G1/commands"):
			registeredPath = r.URL.Path
			var body ApplicationCommand
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Name != "onibi" || len(body.Options) != 1 || body.Options[0].Name != "text" {
				t.Fatalf("body = %#v", body)
			}
			_ = json.NewEncoder(w).Encode(ApplicationCommand{ID: "cmd1", Name: "onibi", Description: body.Description})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/applications/app1/guilds/G1/commands"):
			_ = json.NewEncoder(w).Encode([]ApplicationCommand{{ID: "cmd1", Name: "onibi"}})
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	c := New("bot-token")
	c.BaseURL = srv.URL
	app, err := c.CurrentApplication(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if app.ID != "app1" {
		t.Fatalf("app = %#v", app)
	}
	ch, err := c.Channel(t.Context(), "C1")
	if err != nil {
		t.Fatal(err)
	}
	if ch.GuildID != "G1" {
		t.Fatalf("channel = %#v", ch)
	}
	cmd, err := c.RegisterOnibiCommand(t.Context(), "", "G1")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "onibi" || registeredPath == "" {
		t.Fatalf("cmd=%#v path=%q", cmd, registeredPath)
	}
	cmds, err := c.ApplicationCommands(t.Context(), "app1", "G1")
	if err != nil {
		t.Fatal(err)
	}
	if !HasOnibiCommand(cmds) {
		t.Fatalf("commands = %#v", cmds)
	}
}

func TestAPIErrorIncludesStatusAndCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 50013, "message": "Missing Permissions"})
	}))
	defer srv.Close()
	c := New("bot-token")
	c.BaseURL = srv.URL
	err := c.CreateMessage(t.Context(), "C1", "hello")
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusForbidden || apiErr.Code != 50013 || !strings.Contains(apiErr.Error(), "Missing Permissions") {
		t.Fatalf("err = %#v %v", err, err)
	}
}

func TestInteractionText(t *testing.T) {
	frame := GatewayFrame{Op: OpDispatch, T: "INTERACTION_CREATE", D: mustJSON(map[string]any{
		"id": "i1", "token": "t1", "type": 2,
		"data": map[string]any{"name": "onibi", "options": []any{map[string]any{"name": "text", "type": 3, "value": "ls"}}},
	})}
	in, ok, err := ParseInteraction(frame)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if got := InteractionText(in); got != "ls" {
		t.Fatalf("text = %q", got)
	}
}

func TestCreateMessageChunksNoMentionsAndRetriesRateLimit(t *testing.T) {
	var bodies []map[string]any
	var slept time.Duration
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/channels/C1/messages") {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"retry_after":0.2}`))
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"m1"}`))
	}))
	defer srv.Close()
	c := New("bot-token")
	c.BaseURL = srv.URL
	c.Sleep = func(_ context.Context, d time.Duration) error {
		slept += d
		return nil
	}
	if err := c.CreateMessage(t.Context(), "C1", strings.Repeat("x", MessageChunkLimit+5)); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || len(bodies[0]["content"].(string)) != MessageChunkLimit || len(bodies[1]["content"].(string)) != 5 {
		t.Fatalf("bodies = %#v", bodies)
	}
	mentions := bodies[0]["allowed_mentions"].(map[string]any)
	if len(mentions["parse"].([]any)) != 0 {
		t.Fatalf("mentions = %#v", mentions)
	}
	if slept != 200*time.Millisecond {
		t.Fatalf("slept = %s", slept)
	}
}

func TestComponentsThreadAndParityAxes(t *testing.T) {
	var componentBody map[string]any
	var threadBody map[string]any
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if _, ok := body["components"]; ok {
				componentBody = body
			}
			_ = json.NewEncoder(w).Encode(Message{ID: "m1", ChannelID: "C1"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages/m1/threads"):
			if err := json.NewDecoder(r.Body).Decode(&threadBody); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(Channel{ID: "T1", GuildID: "G1", Name: "onibi-s1"})
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	c := New("bot-token")
	c.BaseURL = srv.URL
	msg, err := c.CreateComponentsMessage(t.Context(), "C1", []any{
		map[string]any{"type": 10, "content": "approval"},
		map[string]any{"type": 1, "components": []any{map[string]any{"type": 2, "custom_id": "onibi:approval:approve:a1"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.ID != "m1" || int(componentBody["flags"].(float64)) != ComponentsV2Flag {
		t.Fatalf("msg=%#v body=%#v", msg, componentBody)
	}
	ch, err := c.StartThreadFromMessage(t.Context(), "C1", "m1", "onibi-s1")
	if err != nil {
		t.Fatal(err)
	}
	if ch.ID != "T1" || threadBody["name"] != "onibi-s1" {
		t.Fatalf("thread=%#v body=%#v paths=%v", ch, threadBody, paths)
	}
	frame := GatewayFrame{Op: OpDispatch, T: "INTERACTION_CREATE", D: mustJSON(map[string]any{
		"id": "i1", "token": "tok", "type": 3, "channel_id": "C1",
		"user": map[string]any{"id": "U1"},
		"data": map[string]any{"custom_id": "onibi:approval:approve:a1", "component_type": 2},
	})}
	in, ok, err := ParseInteraction(frame)
	if err != nil || !ok || in.Data.CustomID != "onibi:approval:approve:a1" || InteractionUserID(in) != "U1" {
		t.Fatalf("interaction=%#v ok=%v err=%v", in, ok, err)
	}
	frame = GatewayFrame{Op: OpDispatch, T: "INTERACTION_CREATE", D: mustJSON(map[string]any{
		"id": "i2", "token": "tok", "type": 5,
		"data": map[string]any{
			"custom_id": "onibi:approval_edit:a1",
			"components": []any{map[string]any{"type": 1, "components": []any{
				map[string]any{"type": 4, "custom_id": "json", "value": `{"command":"pwd"}`},
			}}},
		},
	})}
	in, ok, err = ParseInteraction(frame)
	if err != nil || !ok || InteractionModalValue(in, "json") != `{"command":"pwd"}` {
		t.Fatalf("modal=%#v ok=%v err=%v", in, ok, err)
	}
}

func TestParityAxes(t *testing.T) {
	var chunks []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/channels/C1/messages"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			chunks = append(chunks, body["content"].(string))
			_ = json.NewEncoder(w).Encode(Message{ID: "m1", ChannelID: "C1"})
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	c := New("bot-token")
	c.BaseURL = srv.URL
	c.Sleep = func(context.Context, time.Duration) error { return nil }
	if err := c.CreateMessageChunks(t.Context(), "C1", strings.Repeat("x", MessageChunkLimit+1), nil); err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 || len(chunks[0]) != MessageChunkLimit || len(chunks[1]) != 1 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	msg, ok, err := ParseMessage(GatewayFrame{Op: OpDispatch, T: "MESSAGE_CREATE", D: mustJSON(MessageCreate{ChannelID: "C1", Content: "pwd"})})
	if err != nil || !ok || msg.Content != "pwd" {
		t.Fatalf("msg=%#v ok=%v err=%v", msg, ok, err)
	}
	if !HandleReconnect(GatewayFrame{Op: OpReconnect}) {
		t.Fatal("gateway reconnect not handled")
	}
}

func contextWithTimeout(t *testing.T, d time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(t.Context(), d)
}
