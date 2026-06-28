package matrix

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSyncUsesSinceTokenAndParsesMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_matrix/client/v3/sync" || r.URL.Query().Get("since") != "s1" || r.URL.Query().Get("timeout") != "1000" {
			t.Fatalf("request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		writeJSON(t, w, map[string]any{
			"next_batch": "s2",
			"rooms": map[string]any{"join": map[string]any{"!room:example": map[string]any{
				"timeline": map[string]any{"events": []any{map[string]any{
					"type": "m.room.message", "sender": "@owner:example", "content": map[string]any{"msgtype": "m.text", "body": "ls"},
				}}},
			}}},
		})
	}))
	defer srv.Close()
	got, err := New(srv.URL, "tok").Sync(t.Context(), "s1", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ev := got.Rooms.Join["!room:example"].Timeline.Events[0]
	if got.NextBatch != "s2" || MessageBody(ev) != "ls" {
		t.Fatalf("sync = %#v body=%q", got, MessageBody(ev))
	}
}

func TestSendTextEscapesRoomAndTxn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.Contains(r.URL.Path, "/send/m.room.message/txn-1") {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var body RoomMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.MsgType != "m.text" || body.Body != "hello" {
			t.Fatalf("body = %#v", body)
		}
		writeJSON(t, w, map[string]any{"event_id": "$1"})
	}))
	defer srv.Close()
	c := New(srv.URL, "tok")
	c.TxnID = func() string { return "txn-1" }
	if err := c.SendText(t.Context(), "!room:example", "hello"); err != nil {
		t.Fatal(err)
	}
}

func TestSendTextChunksAndRetriesRateLimit(t *testing.T) {
	var bodies []RoomMessage
	var slept time.Duration
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/send/m.room.message/") {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			writeJSON(t, w, map[string]any{"errcode": "M_LIMIT_EXCEEDED", "error": "limited", "retry_after_ms": 125})
			return
		}
		var body RoomMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, body)
		writeJSON(t, w, map[string]any{"event_id": "$1"})
	}))
	defer srv.Close()
	txn := 0
	c := New(srv.URL, "tok")
	c.TxnID = func() string {
		txn++
		return "txn-" + string(rune('0'+txn))
	}
	c.Sleep = func(_ context.Context, d time.Duration) error {
		slept += d
		return nil
	}
	if err := c.SendText(t.Context(), "!room:example", strings.Repeat("x", MessageChunkLimit+7)); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || len(bodies[0].Body) != MessageChunkLimit || len(bodies[1].Body) != 7 {
		t.Fatalf("bodies = %#v", bodies)
	}
	if slept != 125*time.Millisecond {
		t.Fatalf("slept = %s", slept)
	}
}

func TestCheckRoomOwnerUsesPowerLevels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/account/whoami"):
			writeJSON(t, w, WhoAmI{UserID: "@owner:example"})
		case strings.Contains(r.URL.Path, "/state/m.room.power_levels"):
			writeJSON(t, w, PowerLevels{Users: map[string]int{"@owner:example": 100}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	who, err := New(srv.URL, "tok").CheckRoomOwner(t.Context(), "!room:example", 50)
	if err != nil {
		t.Fatal(err)
	}
	if who.UserID != "@owner:example" {
		t.Fatalf("who = %#v", who)
	}
}

func TestHomeserverErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		writeJSON(t, w, map[string]any{"errcode": "M_FORBIDDEN", "error": "room denied"})
	}))
	defer srv.Close()
	_, err := New(srv.URL, "tok").Sync(t.Context(), "", 0)
	if err == nil || !strings.Contains(err.Error(), "room denied") {
		t.Fatalf("err = %v", err)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatal(err)
	}
}
