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
			"to_device": map[string]any{"events": []any{map[string]any{
				"type": "m.room.encrypted", "sender": "@owner:example", "content": map[string]any{"algorithm": "m.olm.v1.curve25519-aes-sha2"},
			}}},
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
	if got.NextBatch != "s2" || MessageBody(ev) != "ls" || len(got.ToDevice.Events) != 1 || got.ToDevice.Events[0].Sender != "@owner:example" {
		t.Fatalf("sync = %#v body=%q", got, MessageBody(ev))
	}
}

func TestSyncRoomUsesRoomFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_matrix/client/v3/sync" || r.URL.Query().Get("since") != "s1" {
			t.Fatalf("request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		var filter struct {
			Room struct {
				Rooms    []string `json:"rooms"`
				Timeline struct {
					Types []string `json:"types"`
				} `json:"timeline"`
			} `json:"room"`
		}
		if err := json.Unmarshal([]byte(r.URL.Query().Get("filter")), &filter); err != nil {
			t.Fatal(err)
		}
		if len(filter.Room.Rooms) != 1 || filter.Room.Rooms[0] != "!room:example" {
			t.Fatalf("filter = %#v", filter)
		}
		if !containsString(filter.Room.Timeline.Types, "m.reaction") {
			t.Fatalf("filter missing reactions: %#v", filter.Room.Timeline.Types)
		}
		writeJSON(t, w, map[string]any{"next_batch": "s2", "rooms": map[string]any{"join": map[string]any{}}})
	}))
	defer srv.Close()
	got, err := New(srv.URL, "tok").SyncRoom(t.Context(), "!room:example", "s1", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got.NextBatch != "s2" {
		t.Fatalf("sync = %#v", got)
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
	var eventIDs []string
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
		eventID := "$" + r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
		eventIDs = append(eventIDs, eventID)
		writeJSON(t, w, map[string]any{"event_id": eventID})
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
	gotEventIDs, err := c.SendTextEvents(t.Context(), "!room:example", strings.Repeat("x", MessageChunkLimit+7))
	if err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || len(bodies[0].Body) != MessageChunkLimit || len(bodies[1].Body) != 7 {
		t.Fatalf("bodies = %#v", bodies)
	}
	if strings.Join(gotEventIDs, ",") != strings.Join(eventIDs, ",") {
		t.Fatalf("event ids = %#v want %#v", gotEventIDs, eventIDs)
	}
	if slept != 125*time.Millisecond {
		t.Fatalf("slept = %s", slept)
	}
}

func TestReactionParsesAnnotation(t *testing.T) {
	ev := Event{Type: "m.reaction", Content: json.RawMessage(`{"m.relates_to":{"rel_type":"m.annotation","event_id":"$approval","key":"✅"}}`)}
	eventID, key, ok := Reaction(ev)
	if !ok || eventID != "$approval" || key != "✅" {
		t.Fatalf("reaction = %q %q %v", eventID, key, ok)
	}
	if _, _, ok := Reaction(Event{Type: "m.room.message"}); ok {
		t.Fatal("message parsed as reaction")
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

func TestJoinedRooms(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/joined_rooms") {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		writeJSON(t, w, JoinedRooms{JoinedRooms: []string{"!room:example"}})
	}))
	defer srv.Close()
	got, err := New(srv.URL, "tok").JoinedRooms(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.JoinedRooms) != 1 || got.JoinedRooms[0] != "!room:example" {
		t.Fatalf("rooms = %#v", got)
	}
}

func TestCheckRoomOwnerUsesUsersDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/account/whoami"):
			writeJSON(t, w, WhoAmI{UserID: "@owner:example"})
		case strings.Contains(r.URL.Path, "/state/m.room.power_levels"):
			writeJSON(t, w, PowerLevels{UsersDefault: 50})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	if _, err := New(srv.URL, "tok").CheckRoomOwner(t.Context(), "!room:example", 50); err != nil {
		t.Fatal(err)
	}
}

func TestUploadKeysUsesClientServerShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/_matrix/client/v3/keys/upload" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var req KeysUploadRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.DeviceKeys == nil || req.DeviceKeys.UserID != "@bot:example" || req.DeviceKeys.DeviceID != "ONIBI" {
			t.Fatalf("device keys = %#v", req.DeviceKeys)
		}
		if !containsString(req.DeviceKeys.Algorithms, AlgorithmOlmV1) || !containsString(req.DeviceKeys.Algorithms, AlgorithmMegolmV1) {
			t.Fatalf("algorithms = %#v", req.DeviceKeys.Algorithms)
		}
		if req.OneTimeKeys["signed_curve25519:AAAA"] == nil {
			t.Fatalf("one-time keys = %#v", req.OneTimeKeys)
		}
		writeJSON(t, w, map[string]any{"one_time_key_counts": map[string]int{KeyAlgorithmSignedCurve255: 1}})
	}))
	defer srv.Close()
	got, err := New(srv.URL, "tok").UploadKeys(t.Context(), KeysUploadRequest{
		DeviceKeys: &DeviceKeys{
			UserID:     "@bot:example",
			DeviceID:   "ONIBI",
			Algorithms: []string{AlgorithmOlmV1, AlgorithmMegolmV1},
			Keys:       map[string]string{"curve25519:ONIBI": "curve", "ed25519:ONIBI": "ed"},
		},
		OneTimeKeys: map[string]any{"signed_curve25519:AAAA": map[string]any{"key": "otk"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.OneTimeKeyCounts[KeyAlgorithmSignedCurve255] != 1 {
		t.Fatalf("counts = %#v", got.OneTimeKeyCounts)
	}
}

func TestQueryAndClaimKeysUseClientServerShape(t *testing.T) {
	var sawQuery, sawClaim bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_matrix/client/v3/keys/query":
			sawQuery = true
			if r.Method != http.MethodPost {
				t.Fatalf("query method = %s", r.Method)
			}
			var req struct {
				DeviceKeys map[string][]string `json:"device_keys"`
				Timeout    int                 `json:"timeout"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req.DeviceKeys["@alice:example"][0] != "ALICE" || req.Timeout != 2500 {
				t.Fatalf("query req = %#v", req)
			}
			writeJSON(t, w, map[string]any{"device_keys": map[string]any{"@alice:example": map[string]any{"ALICE": map[string]any{"device_id": "ALICE"}}}})
		case "/_matrix/client/v3/keys/claim":
			sawClaim = true
			if r.Method != http.MethodPost {
				t.Fatalf("claim method = %s", r.Method)
			}
			var req struct {
				OneTimeKeys map[string]map[string]string `json:"one_time_keys"`
				Timeout     int                          `json:"timeout"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req.OneTimeKeys["@alice:example"]["ALICE"] != KeyAlgorithmSignedCurve255 || req.Timeout != 1000 {
				t.Fatalf("claim req = %#v", req)
			}
			writeJSON(t, w, map[string]any{"one_time_keys": map[string]any{"@alice:example": map[string]any{"ALICE": map[string]any{"signed_curve25519:AAAA": map[string]any{"key": "otk"}}}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "tok")
	query, err := c.QueryKeys(t.Context(), map[string][]string{"@alice:example": {"ALICE"}}, 2500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if query.DeviceKeys["@alice:example"]["ALICE"] == nil {
		t.Fatalf("query = %#v", query)
	}
	claim, err := c.ClaimOneTimeKeys(t.Context(), map[string]map[string]string{"@alice:example": {"ALICE": KeyAlgorithmSignedCurve255}}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if claim.OneTimeKeys["@alice:example"]["ALICE"]["signed_curve25519:AAAA"] == nil {
		t.Fatalf("claim = %#v", claim)
	}
	if !sawQuery || !sawClaim {
		t.Fatalf("saw query=%v claim=%v", sawQuery, sawClaim)
	}
}

func TestSendToDeviceEscapesEventTypeAndTxn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.Contains(r.URL.Path, "/sendToDevice/m.room.encrypted/txn-1") {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var req ToDeviceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Messages["@alice:example"]["ALICE"] == nil {
			t.Fatalf("messages = %#v", req.Messages)
		}
		writeJSON(t, w, map[string]any{})
	}))
	defer srv.Close()
	c := New(srv.URL, "tok")
	c.TxnID = func() string { return "txn-1" }
	err := c.SendToDevice(t.Context(), "m.room.encrypted", map[string]map[string]any{"@alice:example": {"ALICE": map[string]any{"type": "m.room_key"}}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestE2EEEndpointValidation(t *testing.T) {
	c := New("https://matrix.example", "tok")
	if _, err := c.UploadKeys(t.Context(), KeysUploadRequest{}); err == nil {
		t.Fatal("expected empty upload error")
	}
	if _, err := c.QueryKeys(t.Context(), nil, 0); err == nil {
		t.Fatal("expected empty query error")
	}
	if _, err := c.ClaimOneTimeKeys(t.Context(), nil, 0); err == nil {
		t.Fatal("expected empty claim error")
	}
	if err := c.SendToDevice(t.Context(), "", nil); err == nil {
		t.Fatal("expected empty to-device error")
	}
}

func TestIsEncryptedRoom(t *testing.T) {
	var encrypted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/state/m.room.encryption") {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if !encrypted {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(t, w, map[string]any{"errcode": "M_NOT_FOUND", "error": "not found"})
			return
		}
		writeJSON(t, w, map[string]any{"algorithm": "m.megolm.v1.aes-sha2"})
	}))
	defer srv.Close()
	c := New(srv.URL, "tok")
	got, err := c.IsEncryptedRoom(t.Context(), "!room:example")
	if err != nil || got {
		t.Fatalf("encrypted=%v err=%v", got, err)
	}
	encrypted = true
	got, err = c.IsEncryptedRoom(t.Context(), "!room:example")
	if err != nil || !got {
		t.Fatalf("encrypted=%v err=%v", got, err)
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

func containsString(vals []string, want string) bool {
	for _, v := range vals {
		if v == want {
			return true
		}
	}
	return false
}
