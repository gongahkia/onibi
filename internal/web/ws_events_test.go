package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/gongahkia/onibi/internal/approval"
	e2ecrypto "github.com/gongahkia/onibi/internal/e2e"
	"github.com/gongahkia/onibi/internal/store"
)

func TestWSEventsStreamsApprovalRequestAndDecision(t *testing.T) {
	srv, q, cleanup := testEventServer(t)
	defer cleanup()
	c := dialEventsForTest(t, srv)
	_ = readEventEnvelope(t, c) // server.hello
	readEventsRecovery(t, c)

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
	model, ok := requested["approval"].(map[string]any)
	if !ok || model["version"] != approval.ApprovalSchemaV1 {
		t.Fatalf("approval model = %#v", requested["approval"])
	}
	if _, ok := model["input"]; ok {
		t.Fatalf("approval model leaked raw input: %#v", model)
	}
	risk, ok := model["risk"].(map[string]any)
	if !ok || risk["level"] != "high" {
		t.Fatalf("approval risk = %#v", model["risk"])
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

func TestApprovalEventPayloadIncludesUnifiedDiff(t *testing.T) {
	payload := approvalEventPayload(approval.Event{
		Type: approval.EventRequested,
		Approval: approval.Approval{
			ID:          "a1",
			SessionID:   "s1",
			Agent:       "claude",
			Tool:        "Write",
			InputJSON:   `{"file_path":"/tmp/x","content":"new"}`,
			UnifiedDiff: "--- old\n+++ new\n@@\n-old\n+new\n",
			BudgetWarn: &approval.BudgetWarning{
				Scope:           "session",
				CurrentTokens:   8,
				PredictedTokens: 5,
				ProjectedTokens: 13,
				LimitTokens:     10,
				RemainingTokens: 2,
				OnOverrun:       "interrupt",
				Message:         "Predicted session budget overrun",
			},
			ExpiresAt: time.Now(),
		},
	})
	if payload["unified_diff"] != "--- old\n+++ new\n@@\n-old\n+new\n" {
		t.Fatalf("payload = %#v", payload)
	}
	if payload["file_path"] != "/tmp/x" {
		t.Fatalf("payload = %#v", payload)
	}
	warn, ok := payload["budget_warning"].(map[string]any)
	if !ok || warn["scope"] != "session" || warn["projected_tokens"] != int64(13) {
		t.Fatalf("budget_warning = %#v", payload["budget_warning"])
	}
}

func TestWSEventsStreamsAppEvents(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bus := NewEventBus()
	srv := New(Options{DB: db, EventBus: bus})
	c := dialEventsForTest(t, srv)
	_ = readEventEnvelope(t, c) // server.hello
	readEventsRecovery(t, c)
	time.Sleep(20 * time.Millisecond)
	bus.Publish(Event{Type: "toast", Payload: map[string]any{"message": "Trust policy not reloaded"}})
	env := readEventEnvelope(t, c)
	if env.Type != "toast" {
		t.Fatalf("type = %q", env.Type)
	}
	var payload map[string]any
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["message"] != "Trust policy not reloaded" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestSessionsStatusWSEventReplay(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv := New(Options{
		DB: db,
		SessionList: func(context.Context, SessionListOptions) ([]SessionSummary, error) {
			return []SessionSummary{{
				ID:                    "s1",
				Agent:                 "claude",
				LastActivity:          time.Now().UTC().Format(time.RFC3339Nano),
				PendingApprovalsCount: 1,
				RoleRequired:          "owner",
			}}, nil
		},
	})
	c := dialEventsForTest(t, srv)
	_ = readEventEnvelope(t, c) // server.hello
	env := readEventEnvelope(t, c)
	if env.Type != sessionsStatusEvent {
		t.Fatalf("type = %q", env.Type)
	}
	var payload SessionsStatusResponse
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Sessions) != 1 || payload.Sessions[0].State != SessionStateAwaitingApproval || payload.Counts[SessionStateAwaitingApproval] != 1 {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestEventBusFanoutDoesNotBlockOnSlowConsumer(t *testing.T) {
	bus := NewEventBus()
	slow, unsubSlow := bus.Subscribe()
	defer unsubSlow()
	fast, unsubFast := bus.Subscribe()
	defer unsubFast()
	for i := 0; i < 70; i++ {
		typ := fmt.Sprintf("event.%02d", i)
		bus.Publish(Event{Type: typ})
		select {
		case ev := <-fast:
			if ev.Type != typ {
				t.Fatalf("fast event = %q want %q", ev.Type, typ)
			}
		case <-time.After(time.Second):
			t.Fatal("fast consumer blocked behind slow consumer")
		}
	}
	var slowEvents []Event
	for {
		select {
		case ev := <-slow:
			slowEvents = append(slowEvents, ev)
		default:
			if len(slowEvents) != 64 {
				t.Fatalf("slow events = %d", len(slowEvents))
			}
			if slowEvents[0].Type != "event.06" || slowEvents[len(slowEvents)-1].Type != "event.69" {
				t.Fatalf("slow window = %q..%q", slowEvents[0].Type, slowEvents[len(slowEvents)-1].Type)
			}
			return
		}
	}
}

func TestEventBusSubscribeFromReplaysAndContinuesMonotonically(t *testing.T) {
	bus := NewEventBus()
	bus.Publish(Event{Type: "event.one"})
	bus.Publish(Event{Type: "event.two"})
	replay, ch, unsub := bus.SubscribeFrom(1)
	defer unsub()
	if replay.Snapshot || replay.Seq != 2 || len(replay.Events) != 1 || replay.Events[0].Seq != 2 || replay.Events[0].Type != "event.two" {
		t.Fatalf("replay = %#v", replay)
	}
	bus.Publish(Event{Type: "event.three"})
	select {
	case ev := <-ch:
		if ev.Seq != 3 || ev.Type != "event.three" {
			t.Fatalf("live event = %#v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live event")
	}
}

func TestEventBusRejectsStaleAndFutureCheckpointsWithSnapshot(t *testing.T) {
	bus := NewEventBus()
	bus.replayLimit = 2
	for i := 0; i < 3; i++ {
		bus.Publish(Event{Type: fmt.Sprintf("event.%d", i)})
	}
	stale, _, unsubStale := bus.SubscribeFrom(0)
	unsubStale()
	if !stale.Snapshot || stale.Seq != 3 {
		t.Fatalf("stale replay = %#v", stale)
	}
	future, _, unsubFuture := bus.SubscribeFrom(4)
	unsubFuture()
	if !future.Snapshot || future.Seq != 3 {
		t.Fatalf("future replay = %#v", future)
	}
}

func TestWSEventsResumesEventBusFromCheckpoint(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bus := NewEventBus()
	bus.Publish(Event{Type: "event.one", Payload: map[string]any{"value": 1}})
	bus.Publish(Event{Type: "event.two", Payload: map[string]any{"value": 2}})
	srv := New(Options{DB: db, EventBus: bus})
	c := dialEventsForTestAt(t, srv, 1)
	env := readEventEnvelope(t, c)
	if env.Type != "event.two" || env.Seq != 2 {
		t.Fatalf("replay event = %#v", env)
	}
	recovery := readEventsRecovery(t, c)
	var checkpoint eventsRecoveryFrame
	if err := json.Unmarshal(recovery.Payload, &checkpoint); err != nil {
		t.Fatal(err)
	}
	if checkpoint.Mode != "replay" || checkpoint.Seq != 2 || checkpoint.ReplayCount != 1 {
		t.Fatalf("checkpoint = %#v", checkpoint)
	}
	bus.Publish(Event{Type: "event.three"})
	env = readEventEnvelope(t, c)
	if env.Type != "event.three" || env.Seq != 3 {
		t.Fatalf("live event = %#v", env)
	}
}

func TestWSEventsFutureCheckpointResetsSnapshot(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bus := NewEventBus()
	bus.Publish(Event{Type: "event.before-restart"})
	srv := New(Options{DB: db, EventBus: bus})
	c := dialEventsForTestAt(t, srv, 99)
	env := readEventEnvelope(t, c)
	if env.Type != "server.hello" || env.Seq != 0 {
		t.Fatalf("snapshot hello = %#v", env)
	}
	recovery := readEventsRecovery(t, c)
	var checkpoint eventsRecoveryFrame
	if err := json.Unmarshal(recovery.Payload, &checkpoint); err != nil {
		t.Fatal(err)
	}
	if checkpoint.Mode != "snapshot" || checkpoint.Seq != 1 || checkpoint.ReplayCount != 0 {
		t.Fatalf("checkpoint = %#v", checkpoint)
	}
}

func TestWSEventsE2EAttachAuthenticatesCheckpointReplay(t *testing.T) {
	srv, ownerSessionID, cookie, key, cleanup := e2ePTYServerForTest(t)
	defer cleanup()
	bus := NewEventBus()
	bus.Publish(Event{Type: "event.one"})
	bus.Publish(Event{Type: "event.two"})
	srv.eventBus = bus
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	c, client := dialE2EEventsForTest(t, ts.URL, ownerSessionID, cookie, key, 1)
	defer c.CloseNow()
	env := readE2EEventEnvelope(t, c, client)
	if env.Type != "event.two" || env.Seq != 2 {
		t.Fatalf("replay event = %#v", env)
	}
	recovery := readE2EEventEnvelope(t, c, client)
	if recovery.Type != "events.recovery" {
		t.Fatalf("recovery = %#v", recovery)
	}
}

func TestWSEventsResumeReplaysPendingApproval(t *testing.T) {
	srv, q, cleanup := testEventServer(t)
	defer cleanup()
	bus := NewEventBus()
	bus.Publish(Event{Type: "event.checkpoint"})
	srv.eventBus = bus
	id, _, err := q.Request(context.Background(), "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	c := dialEventsForTestAt(t, srv, 1)
	env := readEventEnvelope(t, c)
	if env.Type != approval.EventRequested || env.Seq != 0 {
		t.Fatalf("pending approval = %#v", env)
	}
	var payload map[string]any
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["id"] != id {
		t.Fatalf("pending approval payload = %#v", payload)
	}
	recovery := readEventsRecovery(t, c)
	var checkpoint eventsRecoveryFrame
	if err := json.Unmarshal(recovery.Payload, &checkpoint); err != nil {
		t.Fatal(err)
	}
	if checkpoint.Mode != "replay" || checkpoint.Seq != 1 {
		t.Fatalf("checkpoint = %#v", checkpoint)
	}
}

func TestWSEventsAttachTimeoutClosesConnection(t *testing.T) {
	withPTYAttachTimeout(t, 20*time.Millisecond)
	srv, _, cleanup := testEventServer(t)
	defer cleanup()
	c := dialEventsWithoutAttachForTest(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err := c.Read(ctx)
	if err == nil {
		t.Fatal("attach timeout did not close connection")
	}
}

func TestWSEventsDuplicateAttachClosesConnection(t *testing.T) {
	srv, _, cleanup := testEventServer(t)
	defer cleanup()
	c := dialEventsForTest(t, srv)
	_ = readEventEnvelope(t, c) // server.hello
	readEventsRecovery(t, c)
	writeEventsAttachForTest(t, c, 0)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err := c.Read(ctx)
	if err == nil {
		t.Fatal("duplicate attach did not close connection")
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
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/approval/"+id, strings.NewReader(`{"verdict":"approve"}`))
	req.AddCookie(rr.Result().Cookies()[0])
	addCSRF(req, sessionID)
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
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	q := approval.New(db, approval.DefaultTTL)
	srv := New(Options{DB: db, ApprovalQueue: q})
	return srv, q, func() { _ = db.Close() }
}

func dialEventsForTest(t *testing.T, srv *Server) *websocket.Conn {
	return dialEventsForTestAt(t, srv, 0)
}

func dialEventsForTestAt(t *testing.T, srv *Server, lastSeq uint64) *websocket.Conn {
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
	if err := wsjson.Write(ctx, c, eventsAttachFrame{Type: "attach", LastSeq: lastSeq}); err != nil {
		_ = c.CloseNow()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

func dialEventsWithoutAttachForTest(t *testing.T, srv *Server) *websocket.Conn {
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

func writeEventsAttachForTest(t *testing.T, c *websocket.Conn, lastSeq uint64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, c, eventsAttachFrame{Type: "attach", LastSeq: lastSeq}); err != nil {
		t.Fatal(err)
	}
}

func dialE2EEventsForTest(t *testing.T, baseURL, ownerSessionID string, cookie *http.Cookie, key []byte, lastSeq uint64) (*websocket.Conn, wsCodec) {
	t.Helper()
	u := "ws" + strings.TrimPrefix(baseURL, "http") + "/ws/events?token=" + ownerSessionID
	header := http.Header{}
	header.Add("Cookie", cookie.String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
		Subprotocols: []string{eventsSubprotocol},
		HTTPHeader:   header,
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionKey := e2ecrypto.DeriveSessionKey(key, []byte(ownerSessionID))
	client, err := newSeqWSClientCodec(sessionKey, ownerSessionID, e2eInfoEvents, e2eDirS2C, e2eDirC2S)
	if err != nil {
		_ = c.CloseNow()
		t.Fatal(err)
	}
	verifyToken, err := relayVerifyToken(key, ownerSessionID)
	if err != nil {
		_ = c.CloseNow()
		t.Fatal(err)
	}
	attach, err := json.Marshal(eventsAttachFrame{
		Type:        "attach",
		LastSeq:     lastSeq,
		VerifyToken: base64.RawURLEncoding.EncodeToString(verifyToken),
	})
	if err != nil {
		_ = c.CloseNow()
		t.Fatal(err)
	}
	typ, sealed, err := client.encrypt(websocket.MessageText, attach)
	if err != nil {
		_ = c.CloseNow()
		t.Fatal(err)
	}
	if err := c.Write(ctx, typ, sealed); err != nil {
		_ = c.CloseNow()
		t.Fatal(err)
	}
	return c, client
}

func readE2EEventEnvelope(t *testing.T, c *websocket.Conn, client wsCodec) rawEventEnvelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, p, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	typ, p, err = client.decrypt(typ, p)
	if err != nil {
		t.Fatal(err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("message type = %v", typ)
	}
	var env rawEventEnvelope
	if err := json.Unmarshal(p, &env); err != nil {
		t.Fatal(err)
	}
	return env
}

type rawEventEnvelope struct {
	Seq     uint64          `json:"seq"`
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

func readEventsRecovery(t *testing.T, c *websocket.Conn) rawEventEnvelope {
	t.Helper()
	env := readEventEnvelope(t, c)
	if env.Type != "events.recovery" {
		t.Fatalf("recovery = %#v", env)
	}
	return env
}
