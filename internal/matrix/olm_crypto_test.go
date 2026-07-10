package matrix

import (
	"bytes"
	"sort"
	"testing"
)

func TestOlmDeviceEncryptDecryptRoundTrip(t *testing.T) {
	pickleKey := []byte("pickle-key")
	alice, err := NewOlmAccountState("@alice:example", "ALICE", pickleKey, 0)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := NewOlmAccountState("@bob:example", "BOB", pickleKey, 3)
	if err != nil {
		t.Fatal(err)
	}
	bobKeys, err := OlmAccountOneTimeKeys(bob, pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	bobOneTimeKey := firstOneTimeKey(t, bobKeys)
	bobCurve := bob.DeviceKeys.Keys["curve25519:BOB"]
	aliceCurve := alice.DeviceKeys.Keys["curve25519:ALICE"]
	alice, outbound, content, err := EncryptOlmForDevice(alice, pickleKey, bob.UserID, bob.DeviceID, bobCurve, bobOneTimeKey, []byte("room-key"))
	if err != nil {
		t.Fatal(err)
	}
	info := content.Ciphertext[bobCurve]
	if content.Algorithm != AlgorithmOlmV1 || content.SenderKey != aliceCurve || info.Type != OlmMessageTypePreKey || info.Body == "" {
		t.Fatalf("content=%#v", content)
	}
	if outbound.UserID != bob.UserID || outbound.DeviceID != bob.DeviceID || outbound.SenderKey != bobCurve || outbound.SessionID == "" || outbound.Pickle == "" {
		t.Fatalf("outbound=%#v", outbound)
	}
	bob, inbound, plaintext, err := DecryptOlmFromDevice(bob, pickleKey, content)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plaintext, []byte("room-key")) {
		t.Fatalf("plaintext=%q", plaintext)
	}
	if inbound.UserID != bob.UserID || inbound.DeviceID != bob.DeviceID || inbound.SenderKey != aliceCurve || inbound.SessionID != outbound.SessionID || inbound.Pickle == "" {
		t.Fatalf("inbound=%#v outbound=%#v", inbound, outbound)
	}
	if bob.OneTimeKeyCounts[KeyAlgorithmSignedCurve255] != 2 {
		t.Fatalf("one time key counts=%#v", bob.OneTimeKeyCounts)
	}
}

func TestOlmDeviceDecryptRejectsMismatchedContent(t *testing.T) {
	pickleKey := []byte("pickle-key")
	alice, err := NewOlmAccountState("@alice:example", "ALICE", pickleKey, 0)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := NewOlmAccountState("@bob:example", "BOB", pickleKey, 1)
	if err != nil {
		t.Fatal(err)
	}
	bobKeys, err := OlmAccountOneTimeKeys(bob, pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	bobCurve := bob.DeviceKeys.Keys["curve25519:BOB"]
	_, _, content, err := EncryptOlmForDevice(alice, pickleKey, bob.UserID, bob.DeviceID, bobCurve, firstOneTimeKey(t, bobKeys), []byte("room-key"))
	if err != nil {
		t.Fatal(err)
	}
	content.Algorithm = AlgorithmMegolmV1
	if _, _, _, err := DecryptOlmFromDevice(bob, pickleKey, content); err == nil {
		t.Fatal("expected algorithm mismatch")
	}
	content.Algorithm = AlgorithmOlmV1
	content.Ciphertext[bobCurve] = OlmCiphertextInfo{Body: content.Ciphertext[bobCurve].Body, Type: OlmMessageTypeMessage}
	if _, _, _, err := DecryptOlmFromDevice(bob, pickleKey, content); err == nil {
		t.Fatal("expected pre-key message")
	}
	content.Ciphertext[bobCurve] = OlmCiphertextInfo{Body: content.Ciphertext[bobCurve].Body, Type: OlmMessageTypePreKey}
	info := content.Ciphertext[bobCurve]
	delete(content.Ciphertext, bobCurve)
	content.Ciphertext["other"] = info
	if _, _, _, err := DecryptOlmFromDevice(bob, pickleKey, content); err == nil {
		t.Fatal("expected missing device ciphertext")
	}
}

func TestOlmSessionEncryptDecryptRoundTrip(t *testing.T) {
	pickleKey := []byte("pickle-key")
	alice, err := NewOlmAccountState("@alice:example", "ALICE", pickleKey, 0)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := NewOlmAccountState("@bob:example", "BOB", pickleKey, 1)
	if err != nil {
		t.Fatal(err)
	}
	bobKeys, err := OlmAccountOneTimeKeys(bob, pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	aliceCurve := alice.DeviceKeys.Keys["curve25519:ALICE"]
	bobCurve := bob.DeviceKeys.Keys["curve25519:BOB"]
	_, aliceSession, first, err := EncryptOlmForDevice(alice, pickleKey, bob.UserID, bob.DeviceID, bobCurve, firstOneTimeKey(t, bobKeys), []byte("open"))
	if err != nil {
		t.Fatal(err)
	}
	bob, bobSession, plaintext, err := DecryptOlmFromDevice(bob, pickleKey, first)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plaintext, []byte("open")) {
		t.Fatalf("plaintext=%q", plaintext)
	}
	bobSession, reply, err := EncryptOlmWithSession(bobSession, pickleKey, bobCurve, []byte("ack"))
	if err != nil {
		t.Fatal(err)
	}
	if reply.Ciphertext[aliceCurve].Type != OlmMessageTypeMessage {
		t.Fatalf("reply=%#v", reply)
	}
	aliceSession, plaintext, err = DecryptOlmWithSession(aliceSession, pickleKey, reply, aliceCurve)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plaintext, []byte("ack")) {
		t.Fatalf("plaintext=%q", plaintext)
	}
	aliceSession, second, err := EncryptOlmWithSession(aliceSession, pickleKey, aliceCurve, []byte("again"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Ciphertext[bobCurve].Type != OlmMessageTypeMessage {
		t.Fatalf("second=%#v", second)
	}
	_, plaintext, err = DecryptOlmWithSession(bobSession, pickleKey, second, bobCurve)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plaintext, []byte("again")) {
		t.Fatalf("plaintext=%q", plaintext)
	}
}

func TestOlmSessionDecryptRejectsMismatchedContent(t *testing.T) {
	pickleKey := []byte("pickle-key")
	alice, err := NewOlmAccountState("@alice:example", "ALICE", pickleKey, 0)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := NewOlmAccountState("@bob:example", "BOB", pickleKey, 1)
	if err != nil {
		t.Fatal(err)
	}
	bobKeys, err := OlmAccountOneTimeKeys(bob, pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	bobCurve := bob.DeviceKeys.Keys["curve25519:BOB"]
	_, aliceSession, first, err := EncryptOlmForDevice(alice, pickleKey, bob.UserID, bob.DeviceID, bobCurve, firstOneTimeKey(t, bobKeys), []byte("open"))
	if err != nil {
		t.Fatal(err)
	}
	_, bobSession, _, err := DecryptOlmFromDevice(bob, pickleKey, first)
	if err != nil {
		t.Fatal(err)
	}
	_, reply, err := EncryptOlmWithSession(bobSession, pickleKey, bobCurve, []byte("ack"))
	if err != nil {
		t.Fatal(err)
	}
	reply.SenderKey = "other"
	if _, _, err := DecryptOlmWithSession(aliceSession, pickleKey, reply, alice.DeviceKeys.Keys["curve25519:ALICE"]); err == nil {
		t.Fatal("expected sender mismatch")
	}
}

func firstOneTimeKey(t *testing.T, keys map[string]string) string {
	t.Helper()
	ids := make([]string, 0, len(keys))
	for keyID := range keys {
		ids = append(ids, keyID)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		t.Fatal("expected one time key")
	}
	return keys[ids[0]]
}
