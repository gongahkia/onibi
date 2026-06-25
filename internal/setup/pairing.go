package setup

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/gongahkia/onibi/internal/store"
)

// PairTokenTTL is how long a pairing token remains usable.
const PairTokenTTL = 5 * time.Minute

// pairPayloadBytes is the random payload length before base64url. 32 bytes
// gives a compact 43-char URL-safe payload for QR URLs.
const pairPayloadBytes = 32

// ErrPairExpired and ErrPairConsumed surface specific failure modes for the
// caller to render distinct messages. The bot must never disclose which
// error occurred to non-owners; for the user themselves it's helpful.
var (
	ErrPairExpired  = errors.New("pairing token expired or unknown")
	ErrPairConsumed = ErrPairExpired // single-use violation looks identical externally
)

// NewToken generates and persists a fresh single-use pairing token.
// Returns the bare token (no "pair_" prefix) so the wizard can format the
// deeplink + QR however it wants.
func NewToken(ctx context.Context, db *store.DB) (string, error) {
	raw := make([]byte, pairPayloadBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("pair token rand: %w", err)
	}
	tok := base64.RawURLEncoding.EncodeToString(raw)
	if err := db.PutPairingToken(ctx, tok, PairTokenTTL); err != nil {
		return "", fmt.Errorf("persist pair token: %w", err)
	}
	return tok, nil
}

func WebPairURL(scheme, host string, port int, token string) string {
	return fmt.Sprintf("%s://%s/pair/%s", scheme, net.JoinHostPort(host, strconv.Itoa(port)), token)
}

// Consume atomically marks the token consumed. Returns nil only on the
// single winning consume call; ErrPairExpired otherwise (covers expiry,
// double-spend, and unknown-token in one rejection path so we don't leak
// which case it was).
func Consume(ctx context.Context, db *store.DB, token string) error {
	ok, err := db.ConsumePairingToken(ctx, token)
	if err != nil {
		return err
	}
	if !ok {
		return ErrPairExpired
	}
	return nil
}
