package envelope

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

const (
	Version  = 1
	SeedLen  = 32
	NonceLen = 12
)

var enc = base64.RawURLEncoding

type Wire struct {
	Version int    `json:"v"`
	Kind    string `json:"kind"`
	Expires int64  `json:"exp"`
	Nonce   string `json:"nonce"`
	Cipher  string `json:"ct"`
}

type Plain struct {
	Version int    `json:"v"`
	Kind    string `json:"kind"`
	ID      string `json:"id,omitempty"`
	Title   string `json:"title,omitempty"`
	Risk    string `json:"risk,omitempty"`
	Body    string `json:"body"`
}

func GenerateSeed() (string, error) {
	b := make([]byte, SeedLen)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return enc.EncodeToString(b), nil
}

func Encrypt(seedB64 string, plain Plain, expires time.Time) (string, error) {
	seed, err := parseSeed(seedB64)
	if err != nil {
		return "", err
	}
	plain.Version = Version
	pt, err := json.Marshal(plain)
	if err != nil {
		return "", err
	}
	key, err := deriveKey(seed)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, NonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	wire := Wire{
		Version: Version,
		Kind:    plain.Kind,
		Expires: expires.Unix(),
		Nonce:   enc.EncodeToString(nonce),
		Cipher:  enc.EncodeToString(gcm.Seal(nil, nonce, pt, aad(plain.Kind, expires))),
	}
	wb, err := json.Marshal(wire)
	if err != nil {
		return "", err
	}
	return enc.EncodeToString(wb), nil
}

func Decrypt(seedB64, token string, now time.Time) (Plain, error) {
	var zero Plain
	seed, err := parseSeed(seedB64)
	if err != nil {
		return zero, err
	}
	wb, err := enc.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return zero, fmt.Errorf("decode envelope: %w", err)
	}
	var wire Wire
	if err := json.Unmarshal(wb, &wire); err != nil {
		return zero, err
	}
	if wire.Version != Version {
		return zero, fmt.Errorf("unsupported envelope version %d", wire.Version)
	}
	if wire.Expires > 0 && !now.Before(time.Unix(wire.Expires, 0)) {
		return zero, errors.New("envelope expired")
	}
	nonce, err := enc.DecodeString(wire.Nonce)
	if err != nil {
		return zero, fmt.Errorf("decode nonce: %w", err)
	}
	ct, err := enc.DecodeString(wire.Cipher)
	if err != nil {
		return zero, fmt.Errorf("decode ciphertext: %w", err)
	}
	key, err := deriveKey(seed)
	if err != nil {
		return zero, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return zero, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return zero, err
	}
	pt, err := gcm.Open(nil, nonce, ct, aad(wire.Kind, time.Unix(wire.Expires, 0)))
	if err != nil {
		return zero, err
	}
	var plain Plain
	if err := json.Unmarshal(pt, &plain); err != nil {
		return zero, err
	}
	if plain.Version != Version || plain.Kind != wire.Kind {
		return zero, errors.New("envelope metadata mismatch")
	}
	return plain, nil
}

func BuildMiniAppURL(base, token string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", errors.New("mini app URL required")
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" {
		return "", errors.New("mini app URL must use https")
	}
	u.Fragment = "onibi=" + url.QueryEscape(token)
	return u.String(), nil
}

func BuildSeedURL(base, seed string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", errors.New("mini app URL required")
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" {
		return "", errors.New("mini app URL must use https")
	}
	u.Fragment = "seed=" + url.QueryEscape(seed)
	return u.String(), nil
}

func parseSeed(seedB64 string) ([]byte, error) {
	seed, err := enc.DecodeString(strings.TrimSpace(seedB64))
	if err != nil {
		return nil, fmt.Errorf("decode envelope seed: %w", err)
	}
	if len(seed) != SeedLen {
		return nil, fmt.Errorf("envelope seed must decode to %d bytes", SeedLen)
	}
	return seed, nil
}

func deriveKey(seed []byte) ([]byte, error) {
	return hkdf.Key(sha256.New, seed, []byte("onibi-envelope-v1"), "telegram-mini-app", 32)
}

func aad(kind string, expires time.Time) []byte {
	return []byte(fmt.Sprintf("v=%d;kind=%s;exp=%d", Version, kind, expires.Unix()))
}
