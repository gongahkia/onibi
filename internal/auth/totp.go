package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// TOTP secret length (RFC 6238 recommends 160-bit secrets).
const secretLen = 20

// stepSeconds is the TOTP time step (RFC 6238 default).
const stepSeconds = 30

// NewSecret returns 20 random bytes for use as a TOTP secret.
func NewSecret() ([]byte, error) {
	b := make([]byte, secretLen)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("totp secret rand: %w", err)
	}
	return b, nil
}

// EncodeHex turns a raw secret into hex for storage in the keystore.
func EncodeHex(secret []byte) string { return hex.EncodeToString(secret) }

// DecodeHex parses a hex secret.
func DecodeHex(s string) ([]byte, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("totp hex decode: %w", err)
	}
	if len(b) != secretLen {
		return nil, fmt.Errorf("totp secret must be %d bytes, got %d", secretLen, len(b))
	}
	return b, nil
}

// OTPAuthURI returns the otpauth URI for QR scanning by an authenticator
// app. label is typically "onibi@<hostname>"; issuer is "onibi".
func OTPAuthURI(secret []byte, label, issuer string) string {
	b32 := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
	return fmt.Sprintf("otpauth://totp/%s?secret=%s&issuer=%s", label, b32, issuer)
}

// Code computes the 6-digit TOTP code for the given Unix timestamp.
func Code(secret []byte, unixTime int64) uint32 {
	step := uint64(unixTime / stepSeconds)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], step)
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	hash := mac.Sum(nil)
	offset := hash[len(hash)-1] & 0x0f
	code := (uint32(hash[offset]&0x7f) << 24) |
		(uint32(hash[offset+1]) << 16) |
		(uint32(hash[offset+2]) << 8) |
		uint32(hash[offset+3])
	return code % 1_000_000
}

// Verify checks the supplied 6-digit code against the secret with ±1 time
// window slack (matches tgterm/bot.c). Constant-time compare on each window.
func Verify(secret []byte, code string) (bool, error) {
	if len(code) != 6 {
		return false, errors.New("totp code must be 6 digits")
	}
	want, err := strconv.ParseUint(code, 10, 32)
	if err != nil {
		return false, errors.New("totp code must be numeric")
	}
	now := time.Now().Unix()
	match := false
	// constant time over the three windows so timing doesn't leak which
	// window matched (defense-in-depth; matters less here than for keys
	// but no reason to be sloppy).
	for off := int64(-1); off <= 1; off++ {
		got := Code(secret, now+off*stepSeconds)
		if subtleEqualU32(uint32(want), got) {
			match = true
		}
	}
	return match, nil
}

func subtleEqualU32(a, b uint32) bool {
	// xor of the two values; non-zero means mismatch. Branch-free.
	return a^b == 0
}
