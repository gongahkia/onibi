package ntfy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/chatout"
)

const providerTestTopic = "A1b2C3d4E5f6G7h8I9j0"

func TestProviderSendsApprovalWithActions(t *testing.T) {
	var body string
	var actions string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+providerTestTopic || r.Header.Get("Authorization") != "Bearer tok" {
			t.Fatalf("request = %s auth=%q", r.URL.String(), r.Header.Get("Authorization"))
		}
		actions = r.Header.Get("X-Actions")
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		_ = json.NewEncoder(w).Encode(StreamMessage{ID: "m1", Event: "message", Topic: providerTestTopic})
	}))
	defer srv.Close()
	p := NewProvider(New(srv.URL, providerTestTopic, "tok"), func(req chatout.ApprovalRequest) ([]Action, error) {
		if req.ID != "apr_1" {
			t.Fatalf("request = %#v", req)
		}
		return []Action{
			{Type: "http", Label: "Approve", URL: "https://onibi.example/ntfy/approval/apr_1/approve", Method: http.MethodPost, Clear: true},
			{Type: "http", Label: "Deny", URL: "https://onibi.example/ntfy/approval/apr_1/deny", Method: http.MethodPost, Clear: true},
		}, nil
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
	if msgID != "m1" || !strings.Contains(body, "Approval apr_1") || !strings.Contains(actions, "Approve") || !strings.Contains(actions, "Deny") {
		t.Fatalf("msgID=%q body=%q actions=%q", msgID, body, actions)
	}
	if len(audit) != 1 || audit[0].Kind != "provider.ntfy.send" || audit[0].MessageID != "m1" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestProviderRequiresApprovalActions(t *testing.T) {
	_, err := NewProvider(New("https://ntfy.local", providerTestTopic, ""), nil).SendApproval(t.Context(), chatout.ApprovalRequest{ID: "apr_1"})
	if err == nil || !strings.Contains(err.Error(), "approval actions") {
		t.Fatalf("err = %v", err)
	}
}

func TestProviderTailStreamAuditsChunks(t *testing.T) {
	var messages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		messages = append(messages, string(data))
		_ = json.NewEncoder(w).Encode(StreamMessage{ID: fmt.Sprintf("m%d", len(messages)), Event: "message", Topic: providerTestTopic})
	}))
	defer srv.Close()
	p := NewProvider(New(srv.URL, providerTestTopic, ""), nil)
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
	if len(messages) != 2 || len(messages[0]) != ProviderMessageChunkLimit || len(messages[1]) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	if len(audit) != 2 || audit[0].Kind != "provider.ntfy.tail_chunk" || audit[0].SessionID != "s1" || audit[0].MessageID != "m1" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestProviderConnectAuditsStream(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+providerTestTopic+"/json" || r.URL.Query().Get("since") != "all" {
			t.Fatalf("url = %s", r.URL.String())
		}
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `{"id":"m7","event":"message","topic":"`+providerTestTopic+`","title":"Admin","message":"status","time":7}`)
		flusher.Flush()
		<-ctx.Done()
	}))
	defer srv.Close()
	p := NewProvider(New(srv.URL, providerTestTopic, ""), nil)
	streamed := make(chan chatout.AuditInteraction, 1)
	p.Audit = func(_ context.Context, item chatout.AuditInteraction) error {
		if item.Kind == "provider.ntfy.stream" {
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
		if got.MessageID != "m7" || got.Payload != "status" || got.Meta["topic"] != providerTestTopic {
			t.Fatalf("stream = %#v", got)
		}
	default:
		t.Fatal("missing stream audit")
	}
}

func TestProviderCapabilitiesUnsupportedAndRateLimit(t *testing.T) {
	p := NewProvider(nil, nil)
	if p.Name() != "ntfy" || len(p.Capabilities()) != 4 {
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

func TestTailJSONHandlerErrorStopsReconnect(t *testing.T) {
	errStop := errors.New("stop")
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprintln(w, `{"id":"m1","event":"message","message":"one"}`)
	}))
	defer srv.Close()
	err := New(srv.URL, providerTestTopic, "").TailJSON(t.Context(), TailOptions{RetryMin: time.Millisecond, RetryMax: time.Millisecond, MaxReconnects: 3}, func(StreamMessage) error {
		return errStop
	})
	if !errors.Is(err, errStop) || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
