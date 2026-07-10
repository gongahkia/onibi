package matrix

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMegolmRoomEventEncryptDecryptRoundTrip(t *testing.T) {
	pickleKey := []byte("pickle-key")
	outbound, roomKey, err := NewMegolmOutboundState("!room:example", pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	inbound, err := NewMegolmInboundState(roomKey, "sender-key", pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	outbound, encrypted, err := EncryptMegolmRoomEvent(outbound, pickleKey, "sender-key", "ONIBI", "!room:example", "m.room.message", RoomMessage{MsgType: "m.text", Body: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if outbound.MessageIndex != 1 || encrypted.Algorithm != AlgorithmMegolmV1 || encrypted.SessionID != outbound.SessionID {
		t.Fatalf("outbound=%#v encrypted=%#v", outbound, encrypted)
	}
	inbound, payload, index, err := DecryptMegolmRoomEvent(inbound, pickleKey, encrypted, "!room:example")
	if err != nil {
		t.Fatal(err)
	}
	if index != 0 || payload.Type != "m.room.message" || payload.RoomID != "!room:example" {
		t.Fatalf("index=%d payload=%#v inbound=%#v", index, payload, inbound)
	}
	var msg RoomMessage
	if err := json.Unmarshal(payload.Content, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.MsgType != "m.text" || msg.Body != "hello" {
		t.Fatalf("message=%#v", msg)
	}
	if _, _, _, err := DecryptMegolmRoomEvent(inbound, pickleKey, encrypted, "!other:example"); err == nil {
		t.Fatal("expected requested room mismatch")
	}
}

func TestSendMegolmEncryptedEvent(t *testing.T) {
	var body MegolmEncryptedContent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.Contains(r.URL.Path, "/send/m.room.encrypted/txn-1") {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		writeJSON(t, w, map[string]any{"event_id": "$encrypted"})
	}))
	defer srv.Close()
	c := New(srv.URL, "tok")
	c.TxnID = func() string { return "txn-1" }
	eventID, err := c.SendMegolmEncryptedEvent(t.Context(), "!room:example", MegolmEncryptedContent{
		Algorithm:  AlgorithmMegolmV1,
		Ciphertext: "ciphertext",
		DeviceID:   "ONIBI",
		SenderKey:  "sender-key",
		SessionID:  "session",
	})
	if err != nil {
		t.Fatal(err)
	}
	if eventID != "$encrypted" || body.Algorithm != AlgorithmMegolmV1 || body.Ciphertext != "ciphertext" {
		t.Fatalf("eventID=%q body=%#v", eventID, body)
	}
}
