package discord

import (
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

func TestSlashCommandFallbackResponse(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		if r.Method != http.MethodPost || !strings.Contains(r.URL.Path, "/interactions/i1/t1/callback") {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
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
}

func contextWithTimeout(t *testing.T, d time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(t.Context(), d)
}
