package matrix

import (
	"bytes"
	"testing"
)

func TestMegolmStateEncryptDecryptRoundTrip(t *testing.T) {
	pickleKey := []byte("pickle-key")
	outbound, roomKey, err := NewMegolmOutboundState("!room:example", pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	if outbound.SessionID == "" || roomKey.SessionID != outbound.SessionID || roomKey.SessionKey == "" {
		t.Fatalf("outbound=%#v roomKey=%#v", outbound, roomKey)
	}
	exported, err := RoomKeyFromMegolmOutboundState(outbound, pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	if exported.SessionID != roomKey.SessionID || exported.SessionKey != roomKey.SessionKey || exported.RoomID != roomKey.RoomID {
		t.Fatalf("exported=%#v roomKey=%#v", exported, roomKey)
	}
	inbound, err := NewMegolmInboundState(roomKey, "sender-key", pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	outbound, content, err := EncryptMegolmState(outbound, pickleKey, "sender-key", "ONIBI", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if outbound.MessageIndex != 1 || content.Algorithm != AlgorithmMegolmV1 || content.SessionID != outbound.SessionID || content.SenderKey != "sender-key" {
		t.Fatalf("outbound=%#v content=%#v", outbound, content)
	}
	inbound, plaintext, index, err := DecryptMegolmState(inbound, pickleKey, content)
	if err != nil {
		t.Fatal(err)
	}
	if index != 0 || !bytes.Equal(plaintext, []byte("hello")) {
		t.Fatalf("index=%d plaintext=%q inbound=%#v", index, plaintext, inbound)
	}
	outbound, content, err = EncryptMegolmState(outbound, pickleKey, "sender-key", "ONIBI", []byte("again"))
	if err != nil {
		t.Fatal(err)
	}
	_, plaintext, index, err = DecryptMegolmState(inbound, pickleKey, content)
	if err != nil {
		t.Fatal(err)
	}
	if index != 1 || !bytes.Equal(plaintext, []byte("again")) {
		t.Fatalf("index=%d plaintext=%q", index, plaintext)
	}
}

func TestMegolmStateRejectsMismatchedContent(t *testing.T) {
	pickleKey := []byte("pickle-key")
	outbound, roomKey, err := NewMegolmOutboundState("!room:example", pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	inbound, err := NewMegolmInboundState(roomKey, "sender-key", pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	_, content, err := EncryptMegolmState(outbound, pickleKey, "sender-key", "ONIBI", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	content.SessionID = "wrong"
	if _, _, _, err := DecryptMegolmState(inbound, pickleKey, content); err == nil {
		t.Fatal("expected session mismatch")
	}
	content.SessionID = inbound.SessionID
	content.SenderKey = "other"
	if _, _, _, err := DecryptMegolmState(inbound, pickleKey, content); err == nil {
		t.Fatal("expected sender mismatch")
	}
}
