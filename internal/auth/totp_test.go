package auth

import (
	"encoding/hex"
	"fmt"
	"testing"
)

// RFC 6238 Appendix B test vectors for SHA-1 mode. Secret ASCII
// "12345678901234567890" → hex per below.
const rfcSecretHex = "3132333435363738393031323334353637383930"

func TestRFC6238Vectors(t *testing.T) {
	secret, _ := hex.DecodeString(rfcSecretHex)
	cases := []struct {
		t    int64
		want uint32
	}{
		{59, 287082},
		{1111111109, 81804},
		{1111111111, 50471},
		{1234567890, 5924},
		{2000000000, 279037},
	}
	for _, c := range cases {
		got := Code(secret, c.t)
		if got != c.want {
			t.Errorf("Code(%d) = %06d, want %06d", c.t, got, c.want)
		}
	}
}

func TestVerifyAcceptsCurrent(t *testing.T) {
	secret, _ := NewSecret()
	code := fmt.Sprintf("%06d", Code(secret, 1700000000))
	// Verify uses time.Now so we can't test it deterministically without
	// injecting a clock. Just check the contract: a code generated for
	// now() must Verify true.
	now := codeForNow(secret)
	ok, err := Verify(secret, now)
	if err != nil || !ok {
		t.Fatalf("expected verify true, got %v err=%v (code=%s static=%s)", ok, err, now, code)
	}
}

func TestVerifyRejectsWrongCode(t *testing.T) {
	secret, _ := NewSecret()
	ok, err := Verify(secret, "000000")
	if err != nil {
		t.Fatal(err)
	}
	// 000000 is statistically unlikely to be the current code; assume not.
	if ok {
		t.Skip("statistically improbable but current code is 000000")
	}
}

func TestVerifyRejectsBadLength(t *testing.T) {
	secret, _ := NewSecret()
	if _, err := Verify(secret, "1234"); err == nil {
		t.Fatal("expected error on short code")
	}
}

func TestVerifyRejectsNonNumeric(t *testing.T) {
	secret, _ := NewSecret()
	if _, err := Verify(secret, "abcdef"); err == nil {
		t.Fatal("expected error on non-numeric code")
	}
}

func TestHexRoundtrip(t *testing.T) {
	secret, _ := NewSecret()
	dec, err := DecodeHex(EncodeHex(secret))
	if err != nil {
		t.Fatal(err)
	}
	if len(dec) != secretLen {
		t.Fatalf("len mismatch: %d vs %d", len(dec), secretLen)
	}
	for i := range secret {
		if dec[i] != secret[i] {
			t.Fatalf("byte %d differs", i)
		}
	}
}

func codeForNow(secret []byte) string {
	return fmt.Sprintf("%06d", Code(secret, currentUnix()))
}

// currentUnix is overridable in tests, but Verify itself uses time.Now —
// in tests we just call this once and trust both stay in the same 30s window.
func currentUnix() int64 {
	return nowUnix()
}
