package envelope

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	KeyBytes    = 32
	Version     = "onibi.e2e.v1"
	commitText  = "onibi relay key commitment v1"
	hkdfSalt    = "onibi relay e2e salt v1"
	aesKeyBytes = 32
	nonceBytes  = 12
)

type Frame struct {
	Version string `json:"v"`
	Type    string `json:"t"`
	Nonce   string `json:"n"`
	Data    string `json:"ct"`
}

type Codec struct {
	key  []byte
	info string
}

func NewKey() ([]byte, error) {
	key := make([]byte, KeyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

func EncodeKey(key []byte) string {
	return base64.RawURLEncoding.EncodeToString(key)
}

func DecodeKey(v string) ([]byte, error) {
	key, err := base64.RawURLEncoding.DecodeString(v)
	if err != nil {
		return nil, err
	}
	if len(key) != KeyBytes {
		return nil, fmt.Errorf("e2e key must be %d bytes", KeyBytes)
	}
	return key, nil
}

func Commitment(key []byte) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(commitText))
	return hex.EncodeToString(mac.Sum(nil))
}

func NewCodec(key []byte, info string) (*Codec, error) {
	if len(key) != KeyBytes {
		return nil, fmt.Errorf("e2e key must be %d bytes", KeyBytes)
	}
	if info == "" {
		return nil, errors.New("e2e info required")
	}
	return &Codec{key: append([]byte(nil), key...), info: info}, nil
}

func (c *Codec) Seal(typ string, plaintext, aad []byte) ([]byte, error) {
	if c == nil {
		return nil, errors.New("nil e2e codec")
	}
	aead, err := c.aead()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	frame := Frame{
		Version: Version,
		Type:    typ,
		Nonce:   base64.RawURLEncoding.EncodeToString(nonce),
		Data:    base64.RawURLEncoding.EncodeToString(aead.Seal(nil, nonce, plaintext, aad)),
	}
	return json.Marshal(frame)
}

func (c *Codec) Open(frameBytes, aad []byte) (string, []byte, error) {
	if c == nil {
		return "", nil, errors.New("nil e2e codec")
	}
	var frame Frame
	if err := json.Unmarshal(frameBytes, &frame); err != nil {
		return "", nil, err
	}
	if frame.Version != Version {
		return "", nil, errors.New("bad e2e frame version")
	}
	nonce, err := base64.RawURLEncoding.DecodeString(frame.Nonce)
	if err != nil {
		return "", nil, err
	}
	if len(nonce) != nonceBytes {
		return "", nil, errors.New("bad e2e nonce")
	}
	ct, err := base64.RawURLEncoding.DecodeString(frame.Data)
	if err != nil {
		return "", nil, err
	}
	aead, err := c.aead()
	if err != nil {
		return "", nil, err
	}
	plaintext, err := aead.Open(nil, nonce, ct, aad)
	if err != nil {
		return "", nil, err
	}
	return frame.Type, plaintext, nil
}

func (c *Codec) aead() (cipher.AEAD, error) {
	key := hkdfSHA256(c.key, []byte(hkdfSalt), []byte(c.info), aesKeyBytes)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func hkdfSHA256(secret, salt, info []byte, n int) []byte {
	if len(salt) == 0 {
		salt = make([]byte, sha256.Size)
	}
	prkMac := hmac.New(sha256.New, salt)
	_, _ = prkMac.Write(secret)
	prk := prkMac.Sum(nil)
	var out bytes.Buffer
	var prev []byte
	counter := byte(1)
	for out.Len() < n {
		mac := hmac.New(sha256.New, prk)
		_, _ = mac.Write(prev)
		_, _ = mac.Write(info)
		_, _ = mac.Write([]byte{counter})
		prev = mac.Sum(nil)
		_, _ = out.Write(prev)
		counter++
	}
	return out.Bytes()[:n]
}

func ReadAllLimited(r io.Reader, n int64) ([]byte, error) {
	if n <= 0 {
		n = 1 << 20
	}
	return io.ReadAll(io.LimitReader(r, n+1))
}
