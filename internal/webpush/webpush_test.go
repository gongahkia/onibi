package webpush

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"
)

func mustDecode(t *testing.T, s string) []byte {
	t.Helper()
	raw, err := b64.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// RFC 8291 Appendix A.
func TestEncryptMatchesRFC8291Example(t *testing.T) {
	asPrivate, err := ecdh.P256().NewPrivateKey(mustDecode(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	if got := b64.EncodeToString(asPrivate.PublicKey().Bytes()); got != "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8" {
		t.Fatalf("as_public=%s", got)
	}
	uaPublic := mustDecode(t, "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4")
	body, err := encrypt([]byte("When I grow up, I want to be a watermelon"), uaPublic, mustDecode(t, "BTBZMqHH6r4Tts7J_aSIgg"), asPrivate, mustDecode(t, "DGv6ra1nlYgDCS1FRnbzlw"))
	if err != nil {
		t.Fatal(err)
	}
	want := "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
	if got := b64.EncodeToString(body); got != want {
		t.Fatalf("body\n got %s\nwant %s", got, want)
	}
}

func TestAuthorizationIsAValidES256Token(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	der, _ := key.Marshal()
	if parsed, err := ParseKey(der); err != nil || parsed.PublicKey() != key.PublicKey() {
		t.Fatalf("round trip err=%v", err)
	}
	now := time.Unix(1_800_000_000, 0)
	header, err := key.authorization("https://web.push.apple.com/QGuQyavXutnMH...", "https://mac.example.ts.net", now)
	if err != nil {
		t.Fatal(err)
	}
	token, public, ok := strings.Cut(strings.TrimPrefix(header, "vapid t="), ", k=")
	if !ok || public != key.PublicKey() {
		t.Fatalf("header=%s", header)
	}
	parts := strings.Split(token, ".")
	var claims struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}
	if len(parts) != 3 || json.Unmarshal(mustDecode(t, parts[1]), &claims) != nil {
		t.Fatalf("token=%s", token)
	}
	if claims.Aud != "https://web.push.apple.com" || claims.Exp != now.Add(time.Hour).Unix() || claims.Sub != "https://mac.example.ts.net" {
		t.Fatalf("claims=%+v", claims)
	}
	sig := mustDecode(t, parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if len(sig) != 64 || !ecdsa.Verify(&key.private.PublicKey, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("signature does not verify")
	}
}

func TestValidateRefusesEndpointsOutsideBrowserPushServices(t *testing.T) {
	sub := Subscription{}
	sub.Keys.P256dh = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	sub.Keys.Auth = "BTBZMqHH6r4Tts7J_aSIgg"
	for endpoint, ok := range map[string]bool{
		"https://web.push.apple.com/abc":                  true,
		"https://fcm.googleapis.com/fcm/send/abc":         true,
		"https://updates.push.services.mozilla.com/wpush": true,
		"https://wns2-par02p.notify.windows.com/w/?token": true,
		"http://fcm.googleapis.com/fcm/send/abc":          false,
		"https://127.0.0.1/push":                          false,
		"https://evilpush.apple.com.example/abc":          false,
		"https://fcm.googleapis.com:8443/abc":             false,
		"https://user@fcm.googleapis.com/abc":             false,
	} {
		sub.Endpoint = endpoint
		if err := sub.Validate(); (err == nil) != ok {
			t.Errorf("%s: err=%v", endpoint, err)
		}
	}
	sub.Endpoint = "https://web.push.apple.com/abc"
	sub.Keys.Auth = "c2hvcnQ"
	if sub.Validate() == nil {
		t.Error("short auth secret accepted")
	}
}
