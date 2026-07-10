package gotify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/gongahkia/onibi/internal/chatout"
)

func TestProviderSendsApprovalWithClickURL(t *testing.T) {
	var got Message
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/message" || r.URL.Query().Get("token") != "app" {
			t.Fatalf("url = %s", r.URL.String())
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(MessageResponse{ID: 42})
	}))
	defer srv.Close()
	p := NewProvider(New(srv.URL, "app", "client"), func(req chatout.ApprovalRequest) (string, error) {
		if req.ID != "apr_1" {
			t.Fatalf("approval req = %#v", req)
		}
		return "https://onibi.example/gotify/approval/apr_1", nil
	})
	var audit []chatout.AuditInteraction
	p.Audit = func(_ context.Context, item chatout.AuditInteraction) error {
		audit = append(audit, item)
		return nil
	}
	msgID, err := p.SendApproval(t.Context(), chatout.ApprovalRequest{ID: "apr_1", SessionID: "s1", Agent: "codex", Tool: "Bash", InputJSON: `{"cmd":"pwd"}`})
	if err != nil {
		t.Fatal(err)
	}
	if msgID != "42" || !strings.Contains(got.Message, "Approval apr_1") || got.Priority != 8 {
		t.Fatalf("msgID=%q got=%#v", msgID, got)
	}
	notification := got.Extras["client::notification"].(map[string]any)
	click := notification["click"].(map[string]any)
	if click["url"] != "https://onibi.example/gotify/approval/apr_1" {
		t.Fatalf("click = %#v", click)
	}
	if len(audit) != 1 || audit[0].Kind != "provider.gotify.send" || audit[0].MessageID != "42" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestProviderRequiresApprovalURL(t *testing.T) {
	_, err := NewProvider(New("https://gotify.local", "app", "client"), nil).SendApproval(t.Context(), chatout.ApprovalRequest{ID: "apr_1"})
	if err == nil || !strings.Contains(err.Error(), "approval url") {
		t.Fatalf("err = %v", err)
	}
}

func TestProviderTailStreamAuditsChunks(t *testing.T) {
	var messages []Message
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg Message
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, msg)
		_ = json.NewEncoder(w).Encode(MessageResponse{ID: len(messages)})
	}))
	defer srv.Close()
	p := NewProvider(New(srv.URL, "app", "client"), nil)
	var audit []chatout.AuditInteraction
	p.Audit = func(_ context.Context, item chatout.AuditInteraction) error {
		audit = append(audit, item)
		return nil
	}
	ch := make(chan []byte, 1)
	ch <- []byte(strings.Repeat("x", ProviderMessageChunkLimit+2))
	close(ch)
	if err := p.TailStream(t.Context(), "s1", ch); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || len(messages[0].Message) != ProviderMessageChunkLimit || len(messages[1].Message) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	if len(audit) != 2 || audit[0].Kind != "provider.gotify.tail_chunk" || audit[0].SessionID != "s1" || audit[0].MessageID != "1" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestProviderConnectAuditsStream(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/message":
			if r.Method != http.MethodGet || r.URL.Query().Get("token") != "client" {
				t.Fatalf("validate = %s %s", r.Method, r.URL.String())
			}
			w.WriteHeader(http.StatusOK)
		case "/stream":
			if r.URL.Query().Get("token") != "client" {
				t.Fatalf("stream url = %s", r.URL.String())
			}
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"id":7,"appid":3,"title":"Admin","message":"status","priority":4}`))
			<-ctx.Done()
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	p := NewProvider(New(srv.URL, "app", "client"), nil)
	streamed := make(chan chatout.AuditInteraction, 1)
	p.Audit = func(_ context.Context, item chatout.AuditInteraction) error {
		if item.Kind == "provider.gotify.stream" {
			streamed <- item
			cancel()
		}
		return nil
	}
	if err := p.Connect(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	select {
	case got := <-streamed:
		if got.MessageID != "7" || got.Payload != "status" || got.Meta["app_id"] != 3 {
			t.Fatalf("stream = %#v", got)
		}
	default:
		t.Fatal("missing stream audit")
	}
}

func TestProviderConnectRequiresClientToken(t *testing.T) {
	err := NewProvider(New("https://gotify.local", "app", ""), nil).Connect(t.Context())
	if err == nil || !strings.Contains(err.Error(), "client token") {
		t.Fatalf("err = %v", err)
	}
}

func TestProviderCapabilitiesUnsupportedAndRateLimit(t *testing.T) {
	p := NewProvider(nil, nil)
	if p.Name() != "gotify" || len(p.Capabilities()) != 4 {
		t.Fatalf("provider = %s %#v", p.Name(), p.Capabilities())
	}
	if err := p.OnDecision("apr_1", func(chatout.Decision) {}); err == nil {
		t.Fatal("missing decision unsupported error")
	}
	if err := p.OnInboundText(func(string, chatout.Sender) {}); err == nil {
		t.Fatal("missing inbound unsupported error")
	}
	rl := p.RateLimit()
	if rl.PerSecond.Window != time.Second || rl.PerMinute.Window != time.Minute {
		t.Fatalf("rate limit = %#v", rl)
	}
}
