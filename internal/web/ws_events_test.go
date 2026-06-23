package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/store"
)

func TestWSEventsStreamsApprovalRequestAndDecision(t *testing.T) {
	srv, q, cleanup := testEventServer(t)
	defer cleanup()
	c := dialEventsForTest(t, srv)
	_ = readEventEnvelope(t, c) // server.hello

	id, _, err := q.Request(context.Background(), "s1", "claude", "Bash", `{"command":"rm -rf /tmp/x","token":"ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
	if err != nil {
		t.Fatal(err)
	}
	env := readEventEnvelope(t, c)
	if env.Type != approval.EventRequested {
		t.Fatalf("type = %q", env.Type)
	}
	var requested map[string]any
	if err := json.Unmarshal(env.Payload, &requested); err != nil {
		t.Fatal(err)
	}
	if requested["id"] != id || requested["session_id"] != "s1" || requested["tool"] != "Bash" || requested["risk_level"] != "high" {
		t.Fatalf("requested = %#v", requested)
	}
	if got := requested["scrubbed_input"].(string); strings.Contains(got, "ghp_") || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("scrubbed_input = %q", got)
	}

	if err := q.Decide(context.Background(), id, approval.VerdictDeny, "", "no", 1); err != nil {
		t.Fatal(err)
	}
	env = readEventEnvelope(t, c)
	if env.Type != approval.EventDecided {
		t.Fatalf("type = %q", env.Type)
	}
	var decided map[string]any
	if err := json.Unmarshal(env.Payload, &decided); err != nil {
		t.Fatal(err)
	}
	if decided["id"] != id || decided["verdict"] != string(approval.VerdictDeny) || decided["reason"] != "no" {
		t.Fatalf("decided = %#v", decided)
	}
}

func TestApprovalPostRejectsUnauthenticated(t *testing.T) {
	srv, _, cleanup := testEventServer(t)
	defer cleanup()
	req := httptest.NewRequest(http.MethodPost, "/approval/abc", strings.NewReader(`{"verdict":"approve"}`))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestApprovalPostDecidesQueue(t *testing.T) {
	srv, q, cleanup := testEventServer(t)
	defer cleanup()
	id, ch, err := q.Request(context.Background(), "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/approval/"+id, strings.NewReader(`{"verdict":"approve"}`))
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	select {
	case got := <-ch:
		if got.Verdict != approval.VerdictApprove {
			t.Fatalf("verdict = %s", got.Verdict)
		}
	case <-time.After(time.Second):
		t.Fatal("decision not delivered")
	}
}

func testEventServer(t *testing.T) (*Server, *approval.Queue, func()) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	q := approval.New(db, approval.DefaultTTL)
	srv := New(Options{DB: db, ApprovalQueue: q})
	return srv, q, func() { _ = db.Close() }
}

func dialEventsForTest(t *testing.T, srv *Server) *websocket.Conn {
	t.Helper()
	rr := httptest.NewRecorder()
	ownerSessionID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/events?token=" + ownerSessionID
	header := http.Header{}
	header.Add("Cookie", rr.Result().Cookies()[0].String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
		Subprotocols: []string{eventsSubprotocol},
		HTTPHeader:   header,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

type rawEventEnvelope struct {
	Type    string          `json:"type"`
	TS      string          `json:"ts"`
	Payload json.RawMessage `json:"payload"`
}

func readEventEnvelope(t *testing.T, c *websocket.Conn) rawEventEnvelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var env rawEventEnvelope
	if err := wsjson.Read(ctx, c, &env); err != nil {
		t.Fatal(err)
	}
	if env.Type == "" || env.TS == "" {
		t.Fatalf("bad envelope = %#v", env)
	}
	return env
}
