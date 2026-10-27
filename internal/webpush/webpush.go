// Package webpush sends Web Push messages: VAPID (RFC 8292) authorization
// and aes128gcm payload encryption (RFC 8291), using only the standard
// library.
package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	recordSize    = 4096
	maxPayload    = 3072
	tokenLifetime = time.Hour
)

var b64 = base64.RawURLEncoding

// ErrGone means the push service no longer accepts this subscription; the
// caller should delete it.
var ErrGone = errors.New("push subscription expired or was removed")

// Key is the application server's VAPID signing key.
type Key struct{ private *ecdsa.PrivateKey }

func GenerateKey() (Key, error) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	return Key{private}, err
}

func ParseKey(der []byte) (Key, error) {
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return Key{}, err
	}
	private, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || private.Curve != elliptic.P256() {
		return Key{}, errors.New("VAPID key must be a P-256 ECDSA key")
	}
	return Key{private}, nil
}

func (k Key) Marshal() ([]byte, error) { return x509.MarshalPKCS8PrivateKey(k.private) }

// PublicKey is the uncompressed point, base64url-encoded, that browsers take
// as applicationServerKey.
func (k Key) PublicKey() string {
	public, err := k.private.PublicKey.ECDH()
	if err != nil {
		return ""
	}
	return b64.EncodeToString(public.Bytes())
}

// Subscription is what PushSubscription.toJSON() returns in the browser.
type Subscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// pushHosts are the browser vendors' push services. Endpoints come from the
// browser, so anything else is refused rather than fetched.
var pushHosts = []string{"fcm.googleapis.com", "android.googleapis.com", "updates.push.services.mozilla.com", ".push.apple.com", ".notify.windows.com"}

// Validate checks the endpoint and keys without contacting the service.
func (s Subscription) Validate() error {
	u, err := url.Parse(s.Endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") || len(s.Endpoint) > 2048 {
		return errors.New("push endpoint must be an HTTPS URL")
	}
	host := strings.ToLower(u.Hostname())
	allowed := false
	for _, h := range pushHosts {
		if host == h || (strings.HasPrefix(h, ".") && strings.HasSuffix(host, h)) {
			allowed = true
		}
	}
	if !allowed {
		return fmt.Errorf("push endpoint host %q is not a known browser push service", host)
	}
	public, err := b64.DecodeString(strings.TrimRight(s.Keys.P256dh, "="))
	if err != nil {
		return errors.New("push key is not base64url")
	}
	if _, err := ecdh.P256().NewPublicKey(public); err != nil {
		return errors.New("push key is not a P-256 public key")
	}
	auth, err := b64.DecodeString(strings.TrimRight(s.Keys.Auth, "="))
	if err != nil || len(auth) != 16 {
		return errors.New("push auth secret must be 16 bytes")
	}
	return nil
}

// Send encrypts payload for the subscription and posts it. subject is the
// VAPID contact, an https: or mailto: URL.
func (k Key) Send(ctx context.Context, client *http.Client, sub Subscription, payload []byte, subject string, ttl time.Duration) error {
	if err := sub.Validate(); err != nil {
		return err
	}
	public, _ := b64.DecodeString(strings.TrimRight(sub.Keys.P256dh, "="))
	auth, _ := b64.DecodeString(strings.TrimRight(sub.Keys.Auth, "="))
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	body, err := encrypt(payload, public, auth, ephemeral, salt)
	if err != nil {
		return err
	}
	authorization, err := k.authorization(sub.Endpoint, subject, time.Now())
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", strconv.Itoa(int(ttl.Seconds())))
	req.Header.Set("Urgency", "high")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return ErrGone
	default:
		return fmt.Errorf("push service answered %d", resp.StatusCode)
	}
}

func (k Key) authorization(endpoint, subject string, now time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	header := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, err := json.Marshal(map[string]any{"aud": u.Scheme + "://" + u.Host, "exp": now.Add(tokenLifetime).Unix(), "sub": subject})
	if err != nil {
		return "", err
	}
	signing := header + "." + b64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, k.private, digest[:])
	if err != nil {
		return "", err
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return "vapid t=" + signing + "." + b64.EncodeToString(signature) + ", k=" + k.PublicKey(), nil
}

// encrypt produces a single aes128gcm record as RFC 8291 section 3.4 and
// RFC 8188 describe.
func encrypt(plaintext, uaPublic, authSecret []byte, ephemeral *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	if len(plaintext) > maxPayload {
		return nil, errors.New("push payload is too large")
	}
	peer, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, err
	}
	secret, err := ephemeral.ECDH(peer)
	if err != nil {
		return nil, err
	}
	asPublic := ephemeral.PublicKey().Bytes()
	keyInfo := append(append([]byte("WebPush: info\x00"), uaPublic...), asPublic...)
	ikm, err := hkdf.Key(sha256.New, secret, authSecret, string(keyInfo), 32)
	if err != nil {
		return nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	header := make([]byte, 0, 16+4+1+len(asPublic))
	header = append(header, salt...)
	header = binary.BigEndian.AppendUint32(header, recordSize)
	header = append(header, byte(len(asPublic)))
	header = append(header, asPublic...)
	// 0x02 marks the last (and only) record.
	return gcm.Seal(header, nonce, append(append([]byte{}, plaintext...), 0x02), nil), nil
}
