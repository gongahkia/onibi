package matrix

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEncryptedContentShapes(t *testing.T) {
	megolm, err := NewMegolmEncryptedContent("sender-key", "ONIBI", "sess-1", "cipher")
	if err != nil {
		t.Fatal(err)
	}
	if megolm.Algorithm != AlgorithmMegolmV1 || megolm.SessionID != "sess-1" || megolm.Ciphertext != "cipher" {
		t.Fatalf("megolm = %#v", megolm)
	}
	olm, err := NewOlmEncryptedContent("sender-key", "recipient-key", "body", OlmMessageTypePreKey)
	if err != nil {
		t.Fatal(err)
	}
	if olm.Algorithm != AlgorithmOlmV1 || olm.Ciphertext["recipient-key"].Type != OlmMessageTypePreKey || olm.Ciphertext["recipient-key"].Body != "body" {
		t.Fatalf("olm = %#v", olm)
	}
}

func TestRoomKeyContentShape(t *testing.T) {
	key, err := NewRoomKeyContent("!room:example", "sess-1", "session-key", false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	if key.Algorithm != AlgorithmMegolmV1 || key.RoomID != "!room:example" || !strings.Contains(string(b), `"shared_history":false`) {
		t.Fatalf("key=%#v json=%s", key, b)
	}
}

func TestRoomKeyRequestShapes(t *testing.T) {
	req, err := NewRoomKeyRequest("ONIBI", "req-1", RequestedKeyInfo{RoomID: "!room:example", SessionID: "sess-1", SenderKey: "sender-key"})
	if err != nil {
		t.Fatal(err)
	}
	if req.Action != RoomKeyActionRequest || req.Body == nil || req.Body.Algorithm != AlgorithmMegolmV1 || req.Body.SenderKey != "sender-key" {
		t.Fatalf("request = %#v", req)
	}
	cancel, err := NewRoomKeyRequestCancellation("ONIBI", "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if cancel.Action != RoomKeyActionCancellation || cancel.Body != nil || cancel.RequestingDeviceID != "ONIBI" {
		t.Fatalf("cancel = %#v", cancel)
	}
}

func TestEncryptionShapeValidation(t *testing.T) {
	if _, err := NewMegolmEncryptedContent("", "", "", "cipher"); err == nil {
		t.Fatal("expected megolm validation error")
	}
	if _, err := NewOlmEncryptedContent("", "recipient", "body", OlmMessageTypeMessage); err == nil {
		t.Fatal("expected olm validation error")
	}
	if _, err := NewRoomKeyContent("!room:example", "", "key", false); err == nil {
		t.Fatal("expected room key validation error")
	}
	if _, err := NewRoomKeyRequest("ONIBI", "req-1", RequestedKeyInfo{RoomID: "!room:example"}); err == nil {
		t.Fatal("expected room key request validation error")
	}
}
